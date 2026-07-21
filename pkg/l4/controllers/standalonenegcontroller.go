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
	"math/rand"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"golang.org/x/exp/slices"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/composite"
	ccontext "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	l4metrics "k8s.io/ingress-gce/pkg/l4/metrics"
	"k8s.io/ingress-gce/pkg/l4/resources"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
	"k8s.io/ingress-gce/pkg/utils"
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
	stopCh    <-chan struct{}
	hasSynced func() bool
}

// NewStandaloneNEGLBController creates a new instance of StandaloneNEGLBController.
func NewStandaloneNEGLBController(ctx *ccontext.ControllerContext, stopCh <-chan struct{}, logger klog.Logger) *StandaloneNEGLBController {
	logger = logger.WithName("StandaloneNEGLBController")
	lc := &StandaloneNEGLBController{
		ctx:       ctx,
		stopCh:    stopCh,
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
					logger.Error(nil, "unexpected object type in DeleteFunc", "type", fmt.Sprintf("%T", obj))
					return
				}
				svc, ok = tombstone.Obj.(*v1.Service)
				if !ok {
					logger.Error(nil, "unexpected object type in tombstone in DeleteFunc", "type", fmt.Sprintf("%T", tombstone.Obj))
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
	syncTrackingId := rand.Int31()
	svcLogger := lc.logger.WithValues("serviceKey", key, "syncId", syncTrackingId)

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
				svcLogger.Error(err, "failed to parse forwarding rule reference URL", "frRef", frName)
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

// IPAddress field is used for creating Regional NetLB forwarding rules, IPAddresses[] field is used for creating Global NetLB forwarding rules.
func frAddresses(fr *composite.ForwardingRule) []string {
	ipAddrs := fr.IPAddresses
	if len(ipAddrs) == 0 {
		ipAddrs = []string{fr.IPAddress}
	}
	return ipAddrs
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

func (lc *StandaloneNEGLBController) syncStandaloneNEGLB(svc *v1.Service, svcLogger klog.Logger) (lbSchemes []string, err error) {
	frNamesStr, ok := svc.Annotations[annotations.CustomForwardingRuleKey]
	if !ok || frNamesStr == "" {
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "NoForwardingRuleRef", "Service has no forwarding rule reference")
		svcLogger.V(4).Info("Service has no forwarding rule reference, skipping")
		cond := NewConditionExternalIPProgrammedFalse(NoForwardingRuleRef)
		err := updateServiceStatus(lc.ctx, svc, &v1.LoadBalancerStatus{Ingress: nil}, []metav1.Condition{cond}, nil, svcLogger)
		if err != nil {
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
		var err error
		if len(parseErrs) > 0 {
			reason = InvalidForwardingRule
		} else {
			reason = NoForwardingRuleRef
		}
		cond := NewConditionExternalIPProgrammedFalse(reason)
		err = updateServiceStatus(lc.ctx, svc, &v1.LoadBalancerStatus{Ingress: nil}, []metav1.Condition{cond}, nil, svcLogger)
		if err != nil {
			errs = append(errs, err)
		}
		if len(errs) > 0 {
			lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRuleUnusable", "Could not use any Forwarding Rule %s", errors.Join(errs...).Error())
			return nil, errors.Join(errs...)
		}
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "NoForwardingRuleRef", "Service has no forwarding rule reference")
		return nil, l4utils.NewUserError(fmt.Errorf("service has no valid forwarding rule reference in annotation"))
	}

	if len(parsedRules) > ForwardingRulesLimit {
		// Sort alphabetically forwarding rules so potentially skipped rules are consistent between resyncs.
		sortParsedFRs(parsedRules)
		skippedFrs := strings.Join(parsedFRNames(parsedRules[ForwardingRulesLimit:]), ", ")
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRuleUnusable", "Up to %d forwarding rules are supported. Skipping remaining forwarding rules (%s)", ForwardingRulesLimit, skippedFrs)
		parsedRules = parsedRules[:ForwardingRulesLimit]
	}

	var lbIngresses []v1.LoadBalancerIngress
	vipMode := v1.LoadBalancerIPModeVIP
	var schemes []string

	for _, parsed := range parsedRules {
		fr, err := composite.GetForwardingRule(lc.ctx.Cloud, parsed.key, meta.VersionBeta, svcLogger)
		if err != nil {
			svcLogger.Error(err, "failed to get forwarding rule", "frName", parsed.rawName)
			if utils.IsNotFoundError(err) {
				err = l4utils.NewUserError(err)
			}
			errs = append(errs, err)
			continue
		}

		schemes = append(schemes, fr.LoadBalancingScheme)

		if err := validateForwardingRule(fr, parsed.rawName); err != nil {
			svcLogger.Error(err, "invalid forwarding rule", "frName", parsed.rawName)
			errs = append(errs, l4utils.NewUserError(err))
			continue
		}

		addrs := frAddresses(fr)
		for _, a := range addrs {
			// GCP IPv6 forwarding rules provide the IP in CIDR form (e.g. /96 range). We must extract the base IP.
			trimmedIP := strings.Split(a, "/")[0]
			// And make it canonical to avoid warnings from k8s apiserver
			if ipAddr, err := netip.ParseAddr(trimmedIP); err == nil {
				trimmedIP = ipAddr.String()
			}
			lbIngresses = append(lbIngresses, v1.LoadBalancerIngress{IP: trimmedIP, IPMode: &vipMode})
		}
	}

	if len(errs) > 0 {
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRuleUnusable", "Could not use all Forwarding Rules %s", errors.Join(errs...).Error())
	}
	// if at least one FR was ok then we use it
	if len(lbIngresses) == 0 {
		// if none of the FRs is usable remove any that is possibly there
		cond := NewConditionExternalIPProgrammedFalse(classifyError(errs[0]))
		err := updateServiceStatus(lc.ctx, svc, &v1.LoadBalancerStatus{Ingress: nil}, []metav1.Condition{cond}, nil, svcLogger)
		if err != nil {
			errs = append(errs, err)
		}
		return schemes, errors.Join(errs...)
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

	if len(errs) > 0 {
		return schemes, errors.Join(errs...)
	}
	return schemes, nil
}

func (lc *StandaloneNEGLBController) clearStatusIngressIP(svc *v1.Service, svcLogger klog.Logger) error {
	newStatus := &v1.LoadBalancerStatus{
		Ingress: nil,
	}

	conditionsToRemove := []string{ExternalIPProgrammed}
	if err := updateServiceStatus(lc.ctx, svc, newStatus, nil, conditionsToRemove, svcLogger); err != nil {
		return err
	}
	return nil
}

func (lc *StandaloneNEGLBController) publishMetrics(key string, schemes []string, syncErr error, start time.Time) {
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

	state.LBSchemeExternal = slices.Contains(schemes, "EXTERNAL")
	state.LBSchemeExternalPassthrough = slices.Contains(schemes, "EXTERNAL_PASSTHROUGH")
	state.LBSchemeInternal = slices.Contains(schemes, "INTERNAL")

	lc.ctx.L4Metrics.SetL4StandaloneNEGService(key, state)
	l4metrics.PublishL4StandaloneNEGSyncLatency(syncErr == nil, start)
}

func classifyError(err error) lbConditionReason {
	if err == nil {
		return ProviderError
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
	case ProviderError:
		return "GCE provider error encountered while retrieving forwarding rules"
	default:
		return "An unexpected error occurred while programming external IPs"
	}
}
