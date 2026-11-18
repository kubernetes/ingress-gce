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

package standaloneneg

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	"k8s.io/ingress-gce/pkg/l4/servicestatus"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// unusableHealthCheck is a health check that can't be used to redirect the
// health check traffic, as it is not an HTTP health check with a fixed port.
type unusableHealthCheck struct {
	url    string
	bsName string
	reason string
}

func (c *Controller) syncLoadBalancer(svc *v1.Service, svcLogger klog.Logger) (lbSchemes sets.Set[string], err error) {
	frNamesStr, ok := svc.Annotations[annotations.CustomForwardingRuleKey]
	if !ok || frNamesStr == "" {
		c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "NoForwardingRuleRef", "Service has no forwarding rule reference")
		svcLogger.V(4).Info("Service has no forwarding rule reference; skipping")
		cond := newConditionExternalIPProgrammedFalse(reasonNoForwardingRuleRef)
		err := servicestatus.Update(c.ctx.KubeClient.CoreV1(), svc, &v1.LoadBalancerStatus{Ingress: nil}, []metav1.Condition{cond}, nil, svcLogger)
		if err != nil {
			return nil, err
		}
		if _, err := c.updateExternalHealthCheckAnnotation(svc, nil, svcLogger); err != nil {
			return nil, err
		}
		return nil, l4utils.NewUserError(fmt.Errorf("service has no forwarding rule reference annotation %s", annotations.CustomForwardingRuleKey))
	}

	refs, parseErrs := parseFRRefs(frNamesStr, c.ctx.Cloud.Region(), svcLogger)
	var errs []error
	for _, parseErr := range parseErrs {
		errs = append(errs, l4utils.NewUserError(parseErr))
	}

	if len(refs) == 0 {
		var reason lbConditionReason
		if len(parseErrs) > 0 {
			reason = reasonInvalidForwardingRule
		} else {
			reason = reasonNoForwardingRuleRef
		}
		cond := newConditionExternalIPProgrammedFalse(reason)
		clearErr := servicestatus.Update(c.ctx.KubeClient.CoreV1(), svc, &v1.LoadBalancerStatus{Ingress: nil}, []metav1.Condition{cond}, nil, svcLogger)
		_, clearAnnotationErr := c.updateExternalHealthCheckAnnotation(svc, nil, svcLogger)
		if len(errs) > 0 {
			c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRuleUnusable", "Could not use any Forwarding Rule: %v", errors.Join(errs...))
			allErrs := append([]error{clearErr, clearAnnotationErr}, errs...)
			return nil, joinMaybeUserErrors(allErrs...)
		}
		c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "NoForwardingRuleRef", "Service has no forwarding rule reference")
		return nil, joinMaybeUserErrors(clearErr, clearAnnotationErr, l4utils.NewUserError(fmt.Errorf("service has no valid forwarding rule reference in annotation")))
	}

	sortFRRefs(refs)
	if len(refs) > forwardingRulesLimit {
		skippedFrs := strings.Join(frRefNames(refs[forwardingRulesLimit:]), ", ")
		c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRulesLimitExceeded", "Up to %d forwarding rules are supported. Skipping remaining forwarding rules (%s)", forwardingRulesLimit, skippedFrs)
		refs = refs[:forwardingRulesLimit]
	}

	var lbIngresses []v1.LoadBalancerIngress
	vipMode := v1.LoadBalancerIPModeVIP
	schemes := sets.New[string]()

	targetNEGs, err := c.serviceNEGLinks(svc)
	if err != nil {
		errs = append(errs, err)
	}

	fetcher := newSyncFetcher(c.ctx.Cloud, svcLogger)
	// healthCheckAddrs are the VIP:port entries of the external health check annotation.
	var healthCheckAddrs []string
	seenHealthCheckAddrs := sets.New[string]()
	// collisions maps health check ports colliding with the service TCP ports to the forwarding rules using them.
	collisions := make(map[int64][]string)
	// hcErrs are errors related to the external health check annotation, kept separately
	// from the forwarding rule errors as they do not make the forwarding rules unusable.
	var hcErrs []error
	// unusableHCs are reported after the loop, once per health check, as health checks
	// can be shared by multiple backend services.
	var unusableHCs []unusableHealthCheck

	for _, ref := range refs {
		fr, bs, err := c.resolveFR(fetcher, ref, targetNEGs, svcLogger)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(frAddresses(fr)) == 0 {
			errs = append(errs, l4utils.NewUserError(fmt.Errorf("forwarding rule %s has no IP address", ref.raw)))
			continue
		}
		schemes.Insert(fr.LoadBalancingScheme)

		vips := forwardingRuleVIPs(fr)
		for _, vip := range vips {
			lbIngresses = append(lbIngresses, v1.LoadBalancerIngress{IP: vip, IPMode: &vipMode})
		}

		hcPort, notUsableReason, err := resolveHealthCheck(fetcher, bs)
		switch {
		case err != nil:
			c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ExternalHealthCheckFailed",
				"Failed to get the health check port of forwarding rule %s: %v", ref.raw, err)
			hcErrs = append(hcErrs, fmt.Errorf("failed to get the health check port of forwarding rule %s: %w", ref.raw, err))
		case notUsableReason != "":
			unusableHCs = append(unusableHCs, unusableHealthCheck{url: bs.HealthChecks[0], bsName: bs.Name, reason: notUsableReason})
		case hcPort == 0:
			// No HTTP health check, nothing to redirect.
		case hcPort == int64(svc.Spec.HealthCheckNodePort):
			// The health check already targets the service health check node port, no redirection is needed.
		case serviceServesTCPPort(svc, hcPort):
			collisions[hcPort] = append(collisions[hcPort], ref.raw)
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

	c.reportUnusableHealthChecks(svc, unusableHCs, svcLogger)
	for _, port := range slices.Sorted(maps.Keys(collisions)) {
		c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "PortCollision",
			"Health check port %d collides with a TCP port of the service, skipping it in the %s annotation for forwarding rules: %s",
			port, annotations.ExternalHealthCheckKey, strings.Join(collisions[port], ", "))
	}

	// Do not update the annotation if some of its entries could not be determined because
	// of non user errors (e.g. transient GCE API errors), not to drop entries that are still valid.
	if len(hcErrs) == 0 && !hasNonUserError(errs) {
		if changed, err := c.updateExternalHealthCheckAnnotation(svc, healthCheckAddrs, svcLogger); err != nil {
			c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "UpdateExternalHealthCheckFailed",
				"Failed to update external health check annotation for load balancer, err: %v", err)
			hcErrs = append(hcErrs, fmt.Errorf("failed to update the %s annotation: %w", annotations.ExternalHealthCheckKey, err))
		} else if changed {
			c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeNormal, "UpdateExternalHealthCheckSuccessful",
				"Successfully updated external health check annotation for load balancer")
		}
	}

	if len(errs) > 0 {
		if len(lbIngresses) == 0 {
			c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRuleUnusable", "None of the Forwarding Rules could be used: %v", errors.Join(errs...))
		} else {
			c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRuleUnusable", "Some Forwarding Rules could not be used: %v", errors.Join(errs...))
		}
	}
	// if at least one FR was ok then we use it
	if len(lbIngresses) == 0 {
		// if none of the FRs is usable remove any that is possibly there
		var firstErr error
		if len(errs) > 0 {
			firstErr = errs[0]
		}
		cond := newConditionExternalIPProgrammedFalse(classifyError(firstErr))
		clearErr := servicestatus.Update(c.ctx.KubeClient.CoreV1(), svc, &v1.LoadBalancerStatus{Ingress: nil}, []metav1.Condition{cond}, nil, svcLogger)
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
	cond := newConditionExternalIPProgrammedTrue(ips)

	if err := servicestatus.Update(c.ctx.KubeClient.CoreV1(), svc, newStatus, []metav1.Condition{cond}, nil, svcLogger); err != nil {
		return schemes, err
	}

	if len(errs) > 0 || len(hcErrs) > 0 {
		return schemes, joinMaybeUserErrors(append(errs, hcErrs...)...)
	}
	c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeNormal, "SyncLoadBalancerSuccessful", "Successfully programmed Standalone NEG LoadBalancer IP(s)")
	return schemes, nil
}

// resolveFR retrieves the forwarding rule and its backend service, and checks
// that they can be used for the service. A non-nil error means the forwarding
// rule can't be used.
func (c *Controller) resolveFR(fetcher *syncFetcher, ref frRef, serviceNEGs sets.Set[cloud.ResourceMapKey], svcLogger klog.Logger) (*composite.ForwardingRule, *composite.BackendService, error) {
	fr, err := fetcher.forwardingRule(ref)
	if err != nil {
		return nil, nil, err
	}
	if err := validateForwardingRule(fr, ref.raw); err != nil {
		svcLogger.Error(err, "invalid forwarding rule", "frName", ref.raw)
		return nil, nil, l4utils.NewUserError(err)
	}

	var bs *composite.BackendService
	bsKey, err := backendServiceKey(fr)
	if err == nil {
		bs, err = fetcher.backendService(fr.BackendService, bsKey)
	}
	if err == nil {
		err = validateBackendServiceAttachment(bs, serviceNEGs)
	}
	if err != nil {
		// Backend services can be shared by multiple forwarding rules, the forwarding
		// rule name is added to make the errors of each rule distinguishable.
		err = fmt.Errorf("forwarding rule %s: %w", ref.raw, err)
		svcLogger.Error(err, "invalid backend service for forwarding rule", "frName", ref.raw, "bsURL", fr.BackendService)
		return nil, nil, err
	}
	return fr, bs, nil
}

// resolveHealthCheck returns the port of the HTTP health check of the backend
// service. If the backend service has a health check that is not an HTTP
// health check with a fixed port, it returns the reason why it can't be used
// instead.
func resolveHealthCheck(fetcher *syncFetcher, bs *composite.BackendService) (port int64, notUsableReason string, err error) {
	// Currently, at most one health check can be specified for each backend service.
	if len(bs.HealthChecks) == 0 {
		return 0, "", nil
	}
	hcURL := bs.HealthChecks[0]
	hcKey, err := healthCheckKey(bs)
	if err != nil {
		return 0, "", err
	}
	hc, err := fetcher.healthCheck(hcURL, hcKey)
	switch {
	case utils.IsNotFoundError(err):
		return 0, fmt.Sprintf("health check %s not found", hcURL), nil
	case err != nil:
		return 0, "", fmt.Errorf("failed to get health check %s: %w", hcURL, err)
	}
	port, notUsableReason = httpHealthCheckPort(hc, hcURL)
	return port, notUsableReason, nil
}

// reportUnusableHealthChecks reports the health checks that can't be used to
// redirect the health check traffic, once per health check.
func (c *Controller) reportUnusableHealthChecks(svc *v1.Service, unusableHCs []unusableHealthCheck, svcLogger klog.Logger) {
	reported := sets.New[string]()
	for _, hc := range unusableHCs {
		if reported.Has(hc.url) {
			continue
		}
		reported.Insert(hc.url)
		c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "NoHTTPHealthChecks", "Backend service %s has no usable HTTP health check: %s", hc.bsName, hc.reason)
		svcLogger.V(4).Info("Backend service has no usable HTTP health check", "backendService", hc.bsName, "reason", hc.reason)
	}
}
