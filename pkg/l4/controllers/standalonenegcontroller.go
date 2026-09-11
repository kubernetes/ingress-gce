/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand"
	"net"
	"net/netip"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	ccontext "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	l4metrics "k8s.io/ingress-gce/pkg/l4/metrics"
	"k8s.io/ingress-gce/pkg/l4/resources"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/klog/v2"
)

const (
	StandaloneNEGLBControllerName = "standalone-neg-lb-controller"

	defaultNumWorkers = 1

	// ExternalIPProgrammed Condition Type
	ExternalIPProgrammed = "ExternalIPProgrammed"

	// IPProgrammed Reason
	IPProgrammed = lbConditionReason("IPProgrammed")
	// NoForwardingRuleRef Reason
	NoForwardingRuleRef = lbConditionReason("NoForwardingRuleRef")
	// UnsupportedLBType Reason
	UnsupportedLBType = lbConditionReason("UnsupportedLBType")
	// InvalidForwardingRule Reason
	InvalidForwardingRule = lbConditionReason("InvalidForwardingRule")
	// BackendNotAttached Reason
	BackendNotAttached = lbConditionReason("BackendNotAttached")
	// ProviderError Reason
	ProviderError = lbConditionReason("ProviderError")
	// Maximum number of forwarding rules
	ForwardingRulesLimit = 10
)

var (
	// supportedLBSchemes is a list of supported LB schemes. Only Forwarding rules
	// with these schemes can be referenced in a L4 standalone NEG service.
	supportedLBSchemes = []string{
		string(cloud.SchemeExternal),
		"EXTERNAL_PASSTHROUGH",
	}
	// supportedFRProtocols is a list of supported protocols. Only Forwarding
	// rules with these protocols can be referenced in a L4 standalone NEG
	// service.
	supportedFRProtocols = []string{
		"TCP",
		"UDP",
		"L3_DEFAULT",
	}
)

// lbConditionReason represents the reason for conditions created by the Standalone NEG Controller.
type lbConditionReason string

func isSchemeSupported(lbScheme string) bool {
	return slices.Contains(supportedLBSchemes, lbScheme)
}

func isFRProtocolSupported(protocol string) bool {
	return slices.Contains(supportedFRProtocols, protocol)
}

type parsedForwardingRule struct {
	key     *meta.Key
	rawName string
}

// StandaloneNEGLBController manages services with CustomNegLoadBalancerClass.
type StandaloneNEGLBController struct {
	ctx       *ccontext.ControllerContext
	svcQueue  utils.TaskQueue
	logger    klog.Logger
	namer     namer.L4ResourcesNamer
	stopCh    <-chan struct{}
	hasSynced func() bool
}

// NewStandaloneNEGLBController creates a new instance of StandaloneNEGLBController.
func NewStandaloneNEGLBController(ctx *ccontext.ControllerContext, stopCh <-chan struct{}, logger klog.Logger) *StandaloneNEGLBController {
	logger = logger.WithName("StandaloneNEGLBController")
	lc := &StandaloneNEGLBController{
		ctx:       ctx,
		stopCh:    stopCh,
		namer:     ctx.L4Namer,
		hasSynced: ctx.HasSynced,
		logger:    logger,
	}
	lc.svcQueue = utils.NewPeriodicTaskQueueWithMultipleWorkers("standalone-l4-neg-lb", "services", defaultNumWorkers, lc.syncWrapper, logger)

	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			svc, ok := obj.(*v1.Service)
			if !ok {
				return
			}
			if lc.shouldProcess(svc) {
				lc.enqueue(svc)
			}
		},
		UpdateFunc: func(old, cur interface{}) {
			curSvc, ok := cur.(*v1.Service)
			if !ok {
				return
			}
			oldSvc, okOld := old.(*v1.Service)
			if okOld && lc.shouldProcess(oldSvc) || lc.shouldProcess(curSvc) {
				lc.enqueue(curSvc)
			}
		},
		DeleteFunc: func(obj interface{}) {
			svc, ok := obj.(*v1.Service)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					logger.Error(nil, "Unexpected object type in DeleteFunc", "type", fmt.Sprintf("%T", obj))
					return
				}
				svc, ok = tombstone.Obj.(*v1.Service)
				if !ok {
					logger.Error(nil, "Unexpected object type in tombstone in DeleteFunc", "type", fmt.Sprintf("%T", tombstone.Obj))
					return
				}
			}
			if lc.shouldProcess(svc) {
				lc.enqueue(svc)
			}
		},
	})

	return lc
}

func (lc *StandaloneNEGLBController) shouldProcess(svc *v1.Service) bool {
	if svc == nil {
		return false
	}
	if svc.Spec.Type != v1.ServiceTypeLoadBalancer {
		return false
	}
	return svc.Spec.LoadBalancerClass != nil && *svc.Spec.LoadBalancerClass == annotations.StandalonePassthroughNegLoadBalancerClass
}

func (lc *StandaloneNEGLBController) enqueue(svc *v1.Service) {
	lc.svcQueue.Enqueue(svc)
}

func (lc *StandaloneNEGLBController) Run() {
	lc.logger.Info("Starting StandaloneNEGLBController", "numWorkers", defaultNumWorkers)

	err := wait.PollUntilContextCancel(wait.ContextForChannel(lc.stopCh), 5*time.Second, true, func(ctx context.Context) (done bool, err error) {
		lc.logger.V(2).Info("Waiting for initial cache sync before starting L4 Standalone NEG controller")
		return lc.hasSynced(), nil
	})
	if err != nil {
		// This will fail if the context is canceled which means the channel was closed. We should return in that case.
		lc.logger.Error(err, "Failed to wait for initial cache sync")
		return
	}

	defer lc.svcQueue.Shutdown()
	lc.svcQueue.Run()
	<-lc.stopCh
}

func (lc *StandaloneNEGLBController) syncWrapper(key string) (err error) {
	syncTrackingID := rand.Int31()
	svcLogger := lc.logger.WithValues("serviceKey", key, "syncId", syncTrackingID)

	defer func() {
		if r := recover(); r != nil {
			errMessage := fmt.Sprintf("Panic in L4 Standalone NEG LB sync worker goroutine: %v", r)
			svcLogger.Error(nil, errMessage)
			l4metrics.PublishL4ControllerPanicCount(StandaloneNEGLBControllerName)
			err = fmt.Errorf("%s", errMessage)
		}
	}()
	syncErr := lc.sync(key, svcLogger)
	return syncErr
}

func (lc *StandaloneNEGLBController) sync(key string, svcLogger klog.Logger) error {
	start := time.Now()
	l4metrics.PublishL4controllerLastSyncTime(StandaloneNEGLBControllerName)

	obj, exists, err := lc.ctx.ServiceInformer.GetIndexer().GetByKey(key)
	if err != nil {
		return fmt.Errorf("failed to lookup service for key %s: %w", key, err)
	}
	if !exists || obj == nil {
		svcLogger.V(3).Info("Ignoring sync of non-existent service")
		lc.ctx.L4Metrics.DeleteL4StandaloneNEGService(key)
		return nil
	}
	svc := obj.(*v1.Service)

	if svc.DeletionTimestamp != nil {
		svcLogger.V(3).Info("Ignoring sync of service undergoing deletion")
		lc.ctx.L4Metrics.DeleteL4StandaloneNEGService(key)
		return nil
	}
	if !lc.shouldProcess(svc) {
		svcLogger.V(3).Info("Ignoring sync: service does not match standalone LB criteria")
		lc.ctx.L4Metrics.DeleteL4StandaloneNEGService(key)
		if err := lc.clearStatusIngressIP(svc, svcLogger); err != nil {
			return err
		}
		if _, err := lc.updateExternalHealthCheckAnnotation(svc, nil, svcLogger); err != nil {
			return err
		}
		return nil
	}

	schemes, err := lc.syncStandaloneNEGLB(svc, svcLogger)
	lc.publishMetrics(key, schemes, err, start)
	return err
}

func (lc *StandaloneNEGLBController) parseForwardingRuleKeys(frNamesStr string, svcLogger klog.Logger) ([]parsedForwardingRule, []error) {
	if frNamesStr == "" {
		return nil, nil
	}
	var parsedRules []parsedForwardingRule
	var errs []error
	frNames := strings.Split(frNamesStr, ",")
	for _, frName := range frNames {
		frName = strings.TrimSpace(frName)
		if frName == "" {
			continue
		}
		var frKey *meta.Key
		if strings.Contains(frName, "/") {
			resourceID, err := cloud.ParseResourceURL(frName)
			if err != nil {
				svcLogger.Error(err, "Failed to parse forwarding rule reference URL", "frRef", frName)
				errs = append(errs, fmt.Errorf("failed to parse forwarding rule reference URL %s: %w", frName, err))
				continue
			}
			frKey = resourceID.Key
		} else {
			frKey = meta.RegionalKey(frName, lc.ctx.Cloud.Region())
		}
		parsedRules = append(parsedRules, parsedForwardingRule{key: frKey, rawName: frName})
	}
	return parsedRules, errs
}

func validateForwardingRule(fr *composite.ForwardingRule, frName string) error {
	var errs []error
	if !isSchemeSupported(fr.LoadBalancingScheme) {
		errs = append(errs, l4utils.NewUnsupportedLoadBalancingSchemeError(frName, fr.LoadBalancingScheme, supportedLBSchemes))
	}
	if !isFRProtocolSupported(fr.IPProtocol) {
		errs = append(errs, l4utils.NewUnsupportedProtocolError(frName, fr.IPProtocol, supportedFRProtocols))
	}
	if len(fr.IPAddresses) > 2 {
		errs = append(errs, fmt.Errorf("forwarding rule %s has more than 2 IP addresses", frName))
	}
	return errors.Join(errs...)
}

func (lc *StandaloneNEGLBController) forwardingRule(frRawName string, key *meta.Key, svcLogger klog.Logger) (*composite.ForwardingRule, error) {
	fr, err := composite.GetForwardingRule(lc.ctx.Cloud, key, meta.VersionBeta, svcLogger)
	if err != nil {
		if utils.IsNotFoundError(err) {
			svcLogger.Error(err, "failed to get forwarding rule", "frName", frRawName)
			err = l4utils.NewUserError(err)
		}
		return nil, err
	}
	return fr, nil
}

// validateLoadBalancer ensures that its forwarding rule and backend services are valid.
func (lc *StandaloneNEGLBController) validateLoadBalancer(parsedFR parsedForwardingRule, targetNEGs sets.Set[cloud.ResourceMapKey], bsValidationCache map[string]error, svcLogger klog.Logger) (addresses []string, scheme string, err error) {
	fr, err := lc.forwardingRule(parsedFR.rawName, parsedFR.key, svcLogger)
	if err != nil {
		return nil, "", err
	}

	if err := validateForwardingRule(fr, parsedFR.rawName); err != nil {
		svcLogger.Error(err, "invalid forwarding rule", "frName", parsedFR.rawName)
		return nil, fr.LoadBalancingScheme, l4utils.NewUserError(err)
	}

	bsURL := fr.BackendService
	var bsErr error
	if cachedErr, ok := bsValidationCache[bsURL]; ok {
		bsErr = cachedErr
	} else {
		bs, err := lc.backendService(fr, svcLogger)
		if err != nil {
			return nil, "", fmt.Errorf("forwarding rule %s: %w", parsedFR.rawName, err)
		}
		if !backendServiceHasNEGAttached(bs, targetNEGs) {
			bsErr = l4utils.NewUserError(l4utils.NewBackendNotAttachedError(bs.Name))
		}
		bsValidationCache[bsURL] = bsErr
	}
	if bsErr != nil {
		errWithContext := fmt.Errorf("forwarding rule %s: %w", parsedFR.rawName, bsErr)
		svcLogger.Error(errWithContext, "invalid backend service for forwarding rule", "frName", parsedFR.rawName, "bsURL", bsURL)
		return nil, fr.LoadBalancingScheme, errWithContext
	}

	return frAddresses(fr), fr.LoadBalancingScheme, nil
}

// backendService retrieves the backend service referenced by the forwarding rule.
func (lc *StandaloneNEGLBController) backendService(fr *composite.ForwardingRule, svcLogger klog.Logger) (*composite.BackendService, error) {
	bsURL := fr.BackendService
	if bsURL == "" {
		return nil, l4utils.NewUserError(fmt.Errorf("the service NEGs are not attached to the load balancer, forwarding rule is missing the backend service reference"))
	}

	resourceID, err := cloud.ParseResourceURL(bsURL)
	if err != nil {
		return nil, l4utils.NewUserError(fmt.Errorf("failed to parse backend service URL %s: %w", bsURL, err))
	}
	if resourceID == nil || resourceID.Key == nil || resourceID.Key.Name == "" {
		return nil, l4utils.NewUserError(fmt.Errorf("invalid backend service URL %s: missing resource key", bsURL))
	}

	bs, err := composite.GetBackendService(lc.ctx.Cloud, resourceID.Key, meta.VersionBeta, svcLogger)
	if err != nil {
		wrappedError := fmt.Errorf("failed to get backend service %s: %w", bsURL, err)
		if utils.IsNotFoundError(err) {
			return nil, l4utils.NewUserError(wrappedError)
		}
		return nil, wrappedError
	}
	return bs, nil
}

// healthCheckPort returns the port of the HTTP health check of the backend service.
// ok is false if the backend service has no HTTP health check with a fixed port.
func (lc *StandaloneNEGLBController) healthCheckPort(bs *composite.BackendService, svc *v1.Service, svcLogger klog.Logger) (port int64, ok bool, err error) {
	hcs := bs.HealthChecks
	// Currently, at most one health check can be specified for each backend service.
	if len(hcs) == 0 {
		return 0, false, nil
	}
	hc, err := composite.GetHealthCheck(lc.ctx.Cloud, meta.RegionalKey(hcs[0], bs.Region), meta.VersionBeta, svcLogger)
	switch {
	case utils.IsNotFoundError(err):
		lc.recordNoHTTPHealthCheck(svc, bs, fmt.Sprintf("health check %s not found", hcs[0]), svcLogger)
		return 0, false, nil
	case err != nil:
		return 0, false, fmt.Errorf("failed to get health check %s: %w", hcs[0], err)
	case hc.Type != "HTTP" || hc.HttpHealthCheck == nil:
		lc.recordNoHTTPHealthCheck(svc, bs, fmt.Sprintf("health check %s is of type %s, not HTTP", hcs[0], hc.Type), svcLogger)
		return 0, false, nil
	case hc.HttpHealthCheck.Port == 0:
		lc.recordNoHTTPHealthCheck(svc, bs, fmt.Sprintf("HTTP health check %s has no fixed port (port specification %q)", hcs[0], hc.HttpHealthCheck.PortSpecification), svcLogger)
		return 0, false, nil
	}
	return hc.HttpHealthCheck.Port, true, nil
}

// recordNoHTTPHealthCheck reports that the backend service has no HTTP health check
// with a fixed port, so no health check redirection is configured for it.
func (lc *StandaloneNEGLBController) recordNoHTTPHealthCheck(svc *v1.Service, bs *composite.BackendService, reason string, svcLogger klog.Logger) {
	lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "NoHTTPHealthChecks", "Backend service %s has no usable HTTP health check: %s", bs.Name, reason)
	svcLogger.V(4).Info("Backend service has no usable HTTP health check", "backendService", bs.Name, "reason", reason)
}

// serviceServesTCPPort checks if the service exposes the given port over TCP.
// HTTP health check traffic to such port can't be redirected, as it can't be
// distinguished from the service traffic.
func serviceServesTCPPort(svc *v1.Service, port int64) bool {
	for _, sp := range svc.Spec.Ports {
		protocol := sp.Protocol
		if protocol == "" {
			protocol = v1.ProtocolTCP
		}
		if protocol == v1.ProtocolTCP && int64(sp.Port) == port {
			return true
		}
	}
	return false
}

// backendServiceHasNEGAttached checks if the backend service has any of
// the targetNEGs attached.
func backendServiceHasNEGAttached(bs *composite.BackendService, targetNEGs sets.Set[cloud.ResourceMapKey]) bool {
	hasMatchingNEG := false
	for _, be := range bs.Backends {
		if be == nil || be.Group == "" {
			continue
		}
		groupKey, err := cloud.ParseResourceURL(be.Group)

		if err == nil && groupKey != nil && targetNEGs.Has(groupKey.MapKey()) {
			hasMatchingNEG = true
			break
		}
	}
	return hasMatchingNEG
}

func (lc *StandaloneNEGLBController) serviceNEGLinks(svc *v1.Service) (sets.Set[cloud.ResourceMapKey], error) {
	negLinks := sets.New[cloud.ResourceMapKey]()
	if svc == nil {
		return negLinks, nil
	}

	negName := lc.namer.L4Backend(svc.Namespace, svc.Name)
	if flags.F.EnableL4CustomStandaloneNEGNames {
		if customName, err := annotations.StandaloneNEGName(svc); err != nil {
			return nil, l4utils.NewUserError(err)
		} else if customName != "" {
			negName = customName
		}
	}
	svcNegKey := fmt.Sprintf("%s/%s", svc.Namespace, negName)

	if lc.ctx != nil && lc.ctx.SvcNegInformer != nil {
		obj, exists, err := lc.ctx.SvcNegInformer.GetIndexer().GetByKey(svcNegKey)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, nil
		}
		svcNeg, ok := obj.(*negv1beta1.ServiceNetworkEndpointGroup)
		if !ok || svcNeg == nil {
			return nil, fmt.Errorf("failed to retrieve ServiceNetworkEndpointGroup")
		}
		if !metav1.IsControlledBy(svcNeg, svc) {
			err := fmt.Errorf("retrieved ServiceNetworkEndpointGroup %s is not controller by the service %s", svcNeg.Name, svc.Name)
			return nil, l4utils.NewUserError(err)
		}

		for _, negRef := range svcNeg.Status.NetworkEndpointGroups {
			if negRef.SelfLink != "" {
				resourceKey, err := cloud.ParseResourceURL(negRef.SelfLink)
				if err != nil {
					continue
				}
				negLinks.Insert(resourceKey.MapKey())
			}
		}
	}
	return negLinks, nil
}

// IPAddress field is used for creating Regional NetLB forwarding rules, IPAddresses[] field is used for creating Global NetLB forwarding rules.
func frAddresses(fr *composite.ForwardingRule) []string {
	if len(fr.IPAddresses) > 0 {
		return fr.IPAddresses
	}
	if fr.IPAddress != "" {
		return []string{fr.IPAddress}
	}
	return nil
}

func parsedFRNames(frs []parsedForwardingRule) []string {
	names := make([]string, len(frs))
	for i, fr := range frs {
		names[i] = fr.rawName
	}
	return names
}

func sortParsedFRs(frs []parsedForwardingRule) {
	sort.Slice(frs, func(i, j int) bool {
		return frs[i].rawName < frs[j].rawName
	})
}

func (lc *StandaloneNEGLBController) syncStandaloneNEGLB(svc *v1.Service, svcLogger klog.Logger) (lbSchemes sets.Set[string], err error) {
	frNamesStr, ok := svc.Annotations[annotations.CustomForwardingRuleKey]
	if !ok || frNamesStr == "" {
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "NoForwardingRuleRef", "Service has no forwarding rule reference")
		svcLogger.V(4).Info("Service has no forwarding rule reference; skipping")
		cond := NewConditionExternalIPProgrammedFalse(NoForwardingRuleRef)
		err := updateServiceStatus(lc.ctx, svc, &v1.LoadBalancerStatus{Ingress: nil}, []metav1.Condition{cond}, nil, svcLogger)
		if err != nil {
			return nil, err
		}
		if _, err := lc.updateExternalHealthCheckAnnotation(svc, nil, svcLogger); err != nil {
			return nil, err
		}
		return nil, l4utils.NewUserError(fmt.Errorf("service has no forwarding rule reference annotation %s", annotations.CustomForwardingRuleKey))
	}

	parsedRules, parseErrs := lc.parseForwardingRuleKeys(frNamesStr, svcLogger)
	var errs []error
	for _, parseErr := range parseErrs {
		errs = append(errs, l4utils.NewUserError(parseErr))
	}

	if len(parsedRules) == 0 {
		var reason lbConditionReason
		if len(parseErrs) > 0 {
			reason = InvalidForwardingRule
		} else {
			reason = NoForwardingRuleRef
		}
		cond := NewConditionExternalIPProgrammedFalse(reason)
		clearErr := updateServiceStatus(lc.ctx, svc, &v1.LoadBalancerStatus{Ingress: nil}, []metav1.Condition{cond}, nil, svcLogger)
		_, clearAnnotationErr := lc.updateExternalHealthCheckAnnotation(svc, nil, svcLogger)
		if len(errs) > 0 {
			lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRuleUnusable", "Could not use any Forwarding Rule: %v", errors.Join(errs...))
			allErrs := append([]error{clearErr, clearAnnotationErr}, errs...)
			return nil, joinMaybeUserErrors(allErrs...)
		}
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "NoForwardingRuleRef", "Service has no forwarding rule reference")
		return nil, joinMaybeUserErrors(clearErr, clearAnnotationErr, l4utils.NewUserError(fmt.Errorf("service has no valid forwarding rule reference in annotation")))
	}

	// Sort alphabetically forwarding rules so rules and status ingress IPs are deterministic between resyncs.
	sortParsedFRs(parsedRules)
	if len(parsedRules) > ForwardingRulesLimit {
		skippedFrs := strings.Join(parsedFRNames(parsedRules[ForwardingRulesLimit:]), ", ")
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRulesLimitExceeded", "Up to %d forwarding rules are supported. Skipping remaining forwarding rules (%s)", ForwardingRulesLimit, skippedFrs)
		parsedRules = parsedRules[:ForwardingRulesLimit]
	}

	var lbIngresses []v1.LoadBalancerIngress
	vipMode := v1.LoadBalancerIPModeVIP
	schemes := sets.New[string]()

	targetNEGs, err := lc.serviceNEGLinks(svc)
	if err != nil {
		errs = append(errs, err)
	}

	bsValidationCache := make(map[string]error)
	// healthCheckAddrs are the VIP:port entries of the external health check annotation.
	var healthCheckAddrs []string
	seenHealthCheckAddrs := sets.New[string]()
	// collisions maps health check ports colliding with the service TCP ports to the forwarding rules using them.
	collisions := make(map[int64][]string)
	// hcErrs are errors related to the external health check annotation, kept separately
	// from the forwarding rule errors as they do not make the forwarding rules unusable.
	var hcErrs []error

	for _, parsed := range parsedRules {
		addresses, lbScheme, err := lc.validateLoadBalancer(parsed, targetNEGs, bsValidationCache, svcLogger)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(addresses) == 0 {
			errs = append(errs, l4utils.NewUserError(fmt.Errorf("forwarding rule %s has no IP address", parsed.rawName)))
			continue
		}
		schemes.Insert(lbScheme)

		fr, err := lc.forwardingRule(parsed.rawName, parsed.key, svcLogger)
		if err != nil {
			return nil, err
		}
		bs, _ := lc.backendService(fr, svcLogger)

		var vips []string
		for _, a := range addresses {
			if a == "" {
				continue
			}
			// GCP IPv6 forwarding rules provide the IP in CIDR form (e.g. /96 range). We must extract the base IP.
			trimmedIP := strings.Split(a, "/")[0]
			// And make it canonical to avoid warnings from k8s apiserver
			if ipAddr, err := netip.ParseAddr(trimmedIP); err == nil {
				trimmedIP = ipAddr.String()
			}
			if trimmedIP != "" {
				lbIngresses = append(lbIngresses, v1.LoadBalancerIngress{IP: trimmedIP, IPMode: &vipMode})
				vips = append(vips, trimmedIP)
			}
		}

		hcPort, ok, err := lc.healthCheckPort(bs, svc, svcLogger)
		switch {
		case err != nil:
			lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ExternalHealthCheckFailed",
				"Failed to get the health check port of forwarding rule %s: %v", parsed.rawName, err)
			hcErrs = append(hcErrs, fmt.Errorf("failed to get the health check port of forwarding rule %s: %w", parsed.rawName, err))
		case !ok:
			// No HTTP health check with a fixed port, nothing to redirect.
		case hcPort == int64(svc.Spec.HealthCheckNodePort):
			// The health check already targets the service health check node port, no redirection is needed.
		case serviceServesTCPPort(svc, hcPort):
			collisions[hcPort] = append(collisions[hcPort], parsed.rawName)
		default:
			for _, vip := range vips {
				addr := net.JoinHostPort(vip, strconv.FormatInt(hcPort, 10))
				if !seenHealthCheckAddrs.Has(addr) {
					seenHealthCheckAddrs.Insert(addr)
					healthCheckAddrs = append(healthCheckAddrs, addr)
				}
			}
		}
	}

	for _, port := range slices.Sorted(maps.Keys(collisions)) {
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "PortCollision",
			"Health check port %d collides with a TCP port of the service, skipping it in the %s annotation for forwarding rules: %s",
			port, annotations.ExternalHealthCheckKey, strings.Join(collisions[port], ", "))
	}

	// Do not update the annotation if some of its entries could not be determined because
	// of non user errors (e.g. transient GCE API errors), not to drop entries that are still valid.
	if len(hcErrs) == 0 && !hasNonUserError(errs) {
		if changed, err := lc.updateExternalHealthCheckAnnotation(svc, healthCheckAddrs, svcLogger); err != nil {
			lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "UpdateExternalHealthCheckFailed",
				"Failed to update external health check annotation for load balancer, err: %v", err)
			hcErrs = append(hcErrs, fmt.Errorf("failed to update the %s annotation: %w", annotations.ExternalHealthCheckKey, err))
		} else if changed {
			lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeNormal, "UpdateExternalHealthCheckSuccessful",
				"Successfully updated external health check annotation for load balancer")
		}
	}

	if len(errs) > 0 {
		if len(lbIngresses) == 0 {
			lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRuleUnusable", "None of the Forwarding Rules could be used: %v", errors.Join(errs...))
		} else {
			lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRuleUnusable", "Some Forwarding Rules could not be used: %v", errors.Join(errs...))
		}
	}
	// if at least one FR was ok then we use it
	if len(lbIngresses) == 0 {
		// if none of the FRs is usable remove any that is possibly there
		var firstErr error
		if len(errs) > 0 {
			firstErr = errs[0]
		}
		cond := NewConditionExternalIPProgrammedFalse(classifyError(firstErr))
		clearErr := updateServiceStatus(lc.ctx, svc, &v1.LoadBalancerStatus{Ingress: nil}, []metav1.Condition{cond}, nil, svcLogger)
		allErrs := append([]error{clearErr}, errs...)
		allErrs = append(allErrs, hcErrs...)
		return schemes, joinMaybeUserErrors(allErrs...)
	}

	newStatus := &v1.LoadBalancerStatus{
		Ingress: lbIngresses,
	}

	var ips []string
	for _, ing := range lbIngresses {
		if ing.IP != "" {
			ips = append(ips, ing.IP)
		} else if ing.Hostname != "" {
			ips = append(ips, ing.Hostname)
		}
	}
	cond := NewConditionExternalIPProgrammedTrue(ips)

	if err := updateServiceStatus(lc.ctx, svc, newStatus, []metav1.Condition{cond}, nil, svcLogger); err != nil {
		return schemes, err
	}

	if len(errs) > 0 || len(hcErrs) > 0 {
		return schemes, joinMaybeUserErrors(append(errs, hcErrs...)...)
	}
	lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeNormal, "SyncLoadBalancerSuccessful", "Successfully programmed Standalone NEG LoadBalancer IP(s)")
	return schemes, nil
}

// join errors but if all are UserErrors make the wrapping error a UserError as
// well. This is required to properly attribute sync status in metrics.
func joinMaybeUserErrors(errs ...error) error {
	var nonNilErrs []error
	for _, err := range errs {
		if err != nil {
			nonNilErrs = append(nonNilErrs, err)
		}
	}
	if len(nonNilErrs) == 0 {
		return nil
	}

	allUserErrors := true
	for _, err := range nonNilErrs {
		if !resources.IsUserError(err) {
			allUserErrors = false
			break
		}
	}
	joinedErr := errors.Join(nonNilErrs...)
	if allUserErrors {
		return l4utils.NewUserError(joinedErr)
	}
	return joinedErr
}

// hasNonUserError checks if any of the errors is not a user error.
func hasNonUserError(errs []error) bool {
	for _, err := range errs {
		if err != nil && !resources.IsUserError(err) {
			return true
		}
	}
	return false
}

// updateExternalHealthCheckAnnotation sets the external health check annotation of the service
// to the given addresses, or removes the annotation if there are none.
// It returns true if the annotation was changed.
func (lc *StandaloneNEGLBController) updateExternalHealthCheckAnnotation(svc *v1.Service, addrs []string, svcLogger klog.Logger) (bool, error) {
	newObjectMeta := svc.ObjectMeta.DeepCopy()
	oldValue, exists := newObjectMeta.Annotations[annotations.ExternalHealthCheckKey]
	if len(addrs) == 0 {
		if !exists {
			return false, nil
		}
		delete(newObjectMeta.Annotations, annotations.ExternalHealthCheckKey)
	} else {
		newValue := strings.Join(addrs, ",")
		if exists && oldValue == newValue {
			return false, nil
		}
		if newObjectMeta.Annotations == nil {
			newObjectMeta.Annotations = make(map[string]string)
		}
		newObjectMeta.Annotations[annotations.ExternalHealthCheckKey] = newValue
	}
	svcLogger.V(3).Info("Updating external health check annotation of service", "annotationKey", annotations.ExternalHealthCheckKey, "addresses", addrs)
	if err := patch.PatchServiceObjectMetadata(lc.ctx.KubeClient.CoreV1(), svc, *newObjectMeta); err != nil {
		return false, err
	}
	return true, nil
}

func (lc *StandaloneNEGLBController) clearStatusIngressIP(svc *v1.Service, svcLogger klog.Logger) error {
	newStatus := &v1.LoadBalancerStatus{
		Ingress: nil,
	}

	conditionsToRemove := []string{ExternalIPProgrammed}
	return updateServiceStatus(lc.ctx, svc, newStatus, nil, conditionsToRemove, svcLogger)
}

func (lc *StandaloneNEGLBController) publishMetrics(key string, schemes sets.Set[string], syncErr error, start time.Time) {
	state := l4metrics.L4StandaloneNEGServiceState{
		Status: l4metrics.StatusSuccess,
	}
	if syncErr != nil {
		if resources.IsUserError(syncErr) {
			state.Status = l4metrics.StatusUserError
		} else {
			state.Status = l4metrics.StatusError
			now := time.Now()
			state.FirstSyncErrorTime = &now
		}
	}

	state.LBSchemeExternal = schemes.Has("EXTERNAL")
	state.LBSchemeExternalPassthrough = schemes.Has("EXTERNAL_PASSTHROUGH")
	state.LBSchemeInternal = schemes.Has("INTERNAL")

	lc.ctx.L4Metrics.SetL4StandaloneNEGService(key, state)
	l4metrics.PublishL4StandaloneNEGSyncLatency(syncErr == nil, start)
}

func classifyError(err error) lbConditionReason {
	if err == nil {
		return ProviderError
	}
	if l4utils.IsBackendNotAttachedError(err) {
		return BackendNotAttached
	}
	if utils.IsNotFoundError(err) {
		return InvalidForwardingRule
	}
	if l4utils.IsUnsupportedLoadBalancingSchemeError(err) {
		return UnsupportedLBType
	}
	if l4utils.IsUnsupportedProtocolError(err) {
		return InvalidForwardingRule
	}
	if resources.IsUserError(err) {
		return InvalidForwardingRule
	}
	return ProviderError
}

func NewConditionExternalIPProgrammedTrue(ips []string) metav1.Condition {
	return metav1.Condition{
		Type:               ExternalIPProgrammed,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             string(IPProgrammed),
		Message:            fmt.Sprintf("IPs programmed: %s", strings.Join(ips, ", ")),
	}
}

func NewConditionExternalIPProgrammedFalse(reason lbConditionReason) metav1.Condition {
	return metav1.Condition{
		Type:               ExternalIPProgrammed,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(reason),
		Message:            messageForReason(reason),
	}
}

func messageForReason(reason lbConditionReason) string {
	switch reason {
	case NoForwardingRuleRef:
		return "Service is missing the custom forwarding rule reference"
	case UnsupportedLBType:
		return "The referenced forwarding rule has an unsupported load balancing scheme"
	case InvalidForwardingRule:
		return "The custom forwarding rule reference is invalid"
	case BackendNotAttached:
		return "The service NEGs are not attached to the load balancer backend service"
	case ProviderError:
		return "GCE provider error encountered while retrieving forwarding rules"
	default:
		return "An unexpected error occurred while programming external IPs"
	}
}
