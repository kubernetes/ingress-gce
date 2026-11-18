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
	"net/netip"
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
	"k8s.io/klog/v2"
)

// validateLoadBalancer ensures that its forwarding rule and backend services are valid.
// It returns the validated forwarding rule and its backend service.
func (c *Controller) validateLoadBalancer(parsedFR parsedForwardingRule, targetNEGs sets.Set[cloud.ResourceMapKey], resourceCache *lbResourceCache, svcLogger klog.Logger) (*composite.ForwardingRule, *composite.BackendService, error) {
	fr, err := c.forwardingRule(parsedFR.rawName, parsedFR.key, svcLogger)
	if err != nil {
		return nil, nil, err
	}

	if err := validateForwardingRule(fr, parsedFR.rawName); err != nil {
		svcLogger.Error(err, "invalid forwarding rule", "frName", parsedFR.rawName)
		return nil, nil, l4utils.NewUserError(err)
	}

	bsURL := fr.BackendService
	result, ok := resourceCache.backendServices[bsURL]
	if !ok {
		result = c.validatedBackendService(fr, targetNEGs, svcLogger)
		resourceCache.backendServices[bsURL] = result
	}
	if result.err != nil {
		// The cached error is shared by the forwarding rules using the backend service,
		// add the forwarding rule name to make the errors of each rule distinguishable.
		errWithContext := fmt.Errorf("forwarding rule %s: %w", parsedFR.rawName, result.err)
		svcLogger.Error(errWithContext, "invalid backend service for forwarding rule", "frName", parsedFR.rawName, "bsURL", bsURL)
		return nil, nil, errWithContext
	}

	return fr, result.bs, nil
}

// validatedBackendService retrieves the backend service of the forwarding rule
// and checks that it has the service NEGs attached.
func (c *Controller) validatedBackendService(fr *composite.ForwardingRule, targetNEGs sets.Set[cloud.ResourceMapKey], svcLogger klog.Logger) backendServiceResult {
	bs, err := c.backendService(fr, svcLogger)
	if err != nil {
		return backendServiceResult{err: err}
	}
	if !backendServiceHasNEGAttached(bs, targetNEGs) {
		return backendServiceResult{err: l4utils.NewUserError(l4utils.NewBackendNotAttachedError(bs.Name))}
	}
	return backendServiceResult{bs: bs}
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

	parsedRules, parseErrs := c.parseForwardingRuleKeys(frNamesStr, svcLogger)
	var errs []error
	for _, parseErr := range parseErrs {
		errs = append(errs, l4utils.NewUserError(parseErr))
	}

	if len(parsedRules) == 0 {
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

	// Sort alphabetically forwarding rules so rules and status ingress IPs are deterministic between resyncs.
	sortParsedFRs(parsedRules)
	if len(parsedRules) > forwardingRulesLimit {
		skippedFrs := strings.Join(parsedFRNames(parsedRules[forwardingRulesLimit:]), ", ")
		c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRulesLimitExceeded", "Up to %d forwarding rules are supported. Skipping remaining forwarding rules (%s)", forwardingRulesLimit, skippedFrs)
		parsedRules = parsedRules[:forwardingRulesLimit]
	}

	var lbIngresses []v1.LoadBalancerIngress
	vipMode := v1.LoadBalancerIPModeVIP
	schemes := sets.New[string]()

	targetNEGs, err := c.serviceNEGLinks(svc)
	if err != nil {
		errs = append(errs, err)
	}

	resourceCache := newLBResourceCache()
	// healthCheckAddrs are the VIP:port entries of the external health check annotation.
	var healthCheckAddrs []string
	seenHealthCheckAddrs := sets.New[string]()
	// collisions maps health check ports colliding with the service TCP ports to the forwarding rules using them.
	collisions := make(map[int64][]string)
	// hcErrs are errors related to the external health check annotation, kept separately
	// from the forwarding rule errors as they do not make the forwarding rules unusable.
	var hcErrs []error

	for _, parsed := range parsedRules {
		fr, bs, err := c.validateLoadBalancer(parsed, targetNEGs, resourceCache, svcLogger)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		addresses := frAddresses(fr)
		if len(addresses) == 0 {
			errs = append(errs, l4utils.NewUserError(fmt.Errorf("forwarding rule %s has no IP address", parsed.rawName)))
			continue
		}
		schemes.Insert(fr.LoadBalancingScheme)

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

		hcPort, ok, err := c.cachedHealthCheckPort(resourceCache, bs, svc, svcLogger)
		switch {
		case err != nil:
			c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ExternalHealthCheckFailed",
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
