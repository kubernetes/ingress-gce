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
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	"k8s.io/ingress-gce/pkg/l4/resources"
	"k8s.io/ingress-gce/pkg/l4/servicestatus"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// usableFR is a forwarding rule whose IPs can be programmed in the service status.
type usableFR struct {
	ref  frRef
	vips []string
	// hcPort is the port of the HTTP health check of the backend service of
	// the forwarding rule, 0 if there is no usable HTTP health check.
	hcPort int64
}

// multipleActiveSvcNegsWarning is reported for services with multiple
// ServiceNetworkEndpointGroups that are not being deleted.
var multipleActiveSvcNegsWarning = syncWarning{
	reason: "MultipleServiceNetworkEndpointGroups",
	message: "Service has multiple ServiceNetworkEndpointGroups that are not being deleted. Only the NEGs of the current NEG name are synced, " +
		"the NEGs of a previous NEG name are no longer updated and are deleted once they are detached from the load balancer. " +
		"Make sure the backend services use the NEGs of the current NEG name.",
}

// unusableHealthCheck is a health check that can't be used to redirect the
// health check traffic, as it is not an HTTP health check with a fixed port.
type unusableHealthCheck struct {
	url    string
	bsName string
	reason string
}

// syncLoadBalancer programs the IPs of the forwarding rules referenced by the
// service in the service status, and updates the external health check
// annotation. It returns the load balancing schemes of the usable forwarding
// rules.
func (c *Controller) syncLoadBalancer(svc *v1.Service, svcLogger klog.Logger) (sets.Set[string], error) {
	refs, parseErrs := parseFRRefs(svc.Annotations[annotations.CustomForwardingRuleKey], c.ctx.Cloud.Region(), svcLogger)
	if len(refs) == 0 && len(parseErrs) == 0 {
		return nil, c.clearNoForwardingRuleRef(svc, svcLogger)
	}
	// errs are the errors making forwarding rules unusable. The first error determines the
	// reason of the ExternalIPProgrammed condition when none of the forwarding rules is usable.
	var errs []error
	for _, parseErr := range parseErrs {
		errs = append(errs, l4utils.NewUserError(parseErr))
	}
	refs = c.limitFRRefs(svc, refs)
	negs, err := c.listServiceNEGs(svc)
	if err != nil {
		// The NEG attachment can't be validated, the status is left as it is.
		return nil, err
	}
	// warnings are reported after the loop, once each, as backend services can be
	// shared by multiple forwarding rules.
	warnings := sets.New[syncWarning]()
	if negs.multipleActiveSvcNegs {
		warnings.Insert(multipleActiveSvcNegsWarning)
	}

	fetcher := newSyncFetcher(c.ctx.Cloud, svcLogger)
	schemes := sets.New[string]()
	var usable []usableFR
	// hcErrs are errors related to the external health check annotation, kept separately
	// from the forwarding rule errors as they do not make the forwarding rules unusable.
	var hcErrs []error
	// unusableHCs are reported after the loop, once per health check, as health checks
	// can be shared by multiple backend services.
	var unusableHCs []unusableHealthCheck
	for _, ref := range refs {
		fr, bs, frWarnings, err := c.resolveFR(fetcher, ref, negs, svcLogger)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(frAddresses(fr)) == 0 {
			errs = append(errs, l4utils.NewUserError(fmt.Errorf("forwarding rule %s has no IP address", ref.raw)))
			continue
		}
		warnings.Insert(frWarnings...)
		schemes.Insert(fr.LoadBalancingScheme)

		hcPort, notUsableReason, err := resolveHealthCheck(fetcher, bs)
		switch {
		case err != nil:
			c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ExternalHealthCheckFailed",
				"Failed to get the health check port of forwarding rule %s: %v", ref.raw, err)
			hcErrs = append(hcErrs, fmt.Errorf("failed to get the health check port of forwarding rule %s: %w", ref.raw, err))
		case notUsableReason != "":
			unusableHCs = append(unusableHCs, unusableHealthCheck{url: bs.HealthChecks[0], bsName: bs.Name, reason: notUsableReason})
		}
		usable = append(usable, usableFR{ref: ref, vips: forwardingRuleVIPs(fr), hcPort: hcPort})
	}

	c.reportUnusableHealthChecks(svc, unusableHCs, svcLogger)
	c.reportWarnings(svc, warnings, svcLogger)
	hcAddrs, collisions := externalHealthCheckAddrs(svc, usable)
	c.reportHealthCheckPortCollisions(svc, collisions)
	// Do not update the annotation if some of its entries could not be determined because
	// of non user errors (e.g. transient GCE API errors), not to drop entries that are still valid.
	if len(hcErrs) == 0 && !hasNonUserError(errs) {
		if err := c.patchExternalHealthCheckAnnotation(svc, hcAddrs, svcLogger); err != nil {
			hcErrs = append(hcErrs, err)
		}
	}

	ingresses := lbIngresses(usable)
	c.reportUnusableFRs(svc, errs, len(ingresses))
	if len(ingresses) == 0 {
		var firstErr error
		if len(errs) > 0 {
			firstErr = errs[0]
		}
		return schemes, c.clearLoadBalancerStatus(svc, classifyError(firstErr), append(errs, hcErrs...), svcLogger)
	}
	return schemes, c.programLoadBalancerStatus(svc, ingresses, append(errs, hcErrs...), svcLogger)
}

// resolveFR retrieves the forwarding rule and its backend service, and checks
// that they can be used for the service. A non-nil error means the forwarding
// rule can't be used, warnings are problems that don't make it unusable.
func (c *Controller) resolveFR(fetcher *syncFetcher, ref frRef, negs serviceNEGs, svcLogger klog.Logger) (*composite.ForwardingRule, *composite.BackendService, []syncWarning, error) {
	fr, err := fetcher.forwardingRule(ref)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := validateForwardingRule(fr, ref.raw); err != nil {
		svcLogger.Error(err, "invalid forwarding rule", "frName", ref.raw)
		return nil, nil, nil, l4utils.NewUserError(err)
	}

	var bs *composite.BackendService
	var warnings []syncWarning
	bsKey, err := backendServiceKey(fr)
	if err == nil {
		bs, err = fetcher.backendService(fr.BackendService, bsKey)
	}
	if err == nil {
		warnings, err = validateBackendServiceAttachment(bs, negs, svcLogger)
	}
	if err != nil {
		// Backend services can be shared by multiple forwarding rules, the forwarding
		// rule name is added to make the errors of each rule distinguishable.
		err = fmt.Errorf("forwarding rule %s: %w", ref.raw, err)
		svcLogger.Error(err, "invalid backend service for forwarding rule", "frName", ref.raw, "bsURL", fr.BackendService)
		return nil, nil, nil, err
	}
	return fr, bs, warnings, nil
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

// limitFRRefs sorts the references and keeps the first forwardingRulesLimit
// ones.
func (c *Controller) limitFRRefs(svc *v1.Service, refs []frRef) []frRef {
	sortFRRefs(refs)
	if len(refs) <= forwardingRulesLimit {
		return refs
	}
	skipped := strings.Join(frRefNames(refs[forwardingRulesLimit:]), ", ")
	c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRulesLimitExceeded",
		"Up to %d forwarding rules are supported. Skipping remaining forwarding rules (%s)", forwardingRulesLimit, skipped)
	return refs[:forwardingRulesLimit]
}

// reportUnusableFRs reports the errors of the forwarding rules that can't be used.
func (c *Controller) reportUnusableFRs(svc *v1.Service, errs []error, numIngresses int) {
	if len(errs) == 0 {
		return
	}
	if numIngresses == 0 {
		c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRuleUnusable", "None of the Forwarding Rules could be used: %v", errors.Join(errs...))
	} else {
		c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRuleUnusable", "Some Forwarding Rules could not be used: %v", errors.Join(errs...))
	}
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

// reportWarnings reports the warnings as events on the service.
func (c *Controller) reportWarnings(svc *v1.Service, warnings sets.Set[syncWarning], svcLogger klog.Logger) {
	for warning := range warnings {
		c.ctx.Recorder(svc.Namespace).Event(svc, v1.EventTypeWarning, warning.reason, warning.message)
		svcLogger.V(2).Info("Load balancer warning", "reason", warning.reason, "message", warning.message)
	}
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

// lbIngresses returns the load balancer ingresses of the service status: the
// IPs of the usable forwarding rules.
func lbIngresses(usable []usableFR) []v1.LoadBalancerIngress {
	vipMode := v1.LoadBalancerIPModeVIP
	var ingresses []v1.LoadBalancerIngress
	for _, u := range usable {
		for _, vip := range u.vips {
			ingresses = append(ingresses, v1.LoadBalancerIngress{IP: vip, IPMode: &vipMode})
		}
	}
	return ingresses
}

// clearNoForwardingRuleRef clears the status and the external health check
// annotation of a service that does not reference any forwarding rule.
func (c *Controller) clearNoForwardingRuleRef(svc *v1.Service, svcLogger klog.Logger) error {
	c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "NoForwardingRuleRef", "Service has no forwarding rule reference")
	svcLogger.V(4).Info("Service has no forwarding rule reference; skipping")
	cond := newConditionExternalIPProgrammedFalse(reasonNoForwardingRuleRef)
	statusErr := servicestatus.Update(c.ctx.KubeClient.CoreV1(), svc, &v1.LoadBalancerStatus{}, []metav1.Condition{cond}, nil, svcLogger)
	_, annotationErr := c.updateExternalHealthCheckAnnotation(svc, nil, svcLogger)
	if err := errors.Join(statusErr, annotationErr); err != nil {
		// The service could not be updated, the sync fails with a system error to be retried.
		return err
	}
	return l4utils.NewUserError(fmt.Errorf("service has no forwarding rule reference in annotation %s", annotations.CustomForwardingRuleKey))
}

// clearLoadBalancerStatus removes the IPs from the service status when none of
// the forwarding rules is usable, reporting the reason in the
// ExternalIPProgrammed condition. It returns the sync errors.
func (c *Controller) clearLoadBalancerStatus(svc *v1.Service, reason lbConditionReason, syncErrs []error, svcLogger klog.Logger) error {
	cond := newConditionExternalIPProgrammedFalse(reason)
	statusErr := servicestatus.Update(c.ctx.KubeClient.CoreV1(), svc, &v1.LoadBalancerStatus{}, []metav1.Condition{cond}, nil, svcLogger)
	return errors.Join(append([]error{statusErr}, syncErrs...)...)
}

// programLoadBalancerStatus writes the IPs of the usable forwarding rules to
// the service status. It returns the sync errors.
func (c *Controller) programLoadBalancerStatus(svc *v1.Service, ingresses []v1.LoadBalancerIngress, syncErrs []error, svcLogger klog.Logger) error {
	var ips []string
	for _, ingress := range ingresses {
		ips = append(ips, ingress.IP)
	}
	cond := newConditionExternalIPProgrammedTrue(ips)
	if err := servicestatus.Update(c.ctx.KubeClient.CoreV1(), svc, &v1.LoadBalancerStatus{Ingress: ingresses}, []metav1.Condition{cond}, nil, svcLogger); err != nil {
		return err
	}
	if len(syncErrs) > 0 {
		return errors.Join(syncErrs...)
	}
	c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeNormal, "SyncLoadBalancerSuccessful", "Successfully programmed Standalone NEG LoadBalancer IP(s)")
	return nil
}

// removeLoadBalancer removes the IPs and the ExternalIPProgrammed condition
// from the status of a service that no longer uses the Standalone NEG LB, and
// its external health check annotation. Unlike clearLoadBalancerStatus, the
// condition is removed and not set to False.
func (c *Controller) removeLoadBalancer(svc *v1.Service, svcLogger klog.Logger) error {
	if err := servicestatus.Update(c.ctx.KubeClient.CoreV1(), svc, &v1.LoadBalancerStatus{}, nil, []string{externalIPProgrammed}, svcLogger); err != nil {
		return err
	}
	_, err := c.updateExternalHealthCheckAnnotation(svc, nil, svcLogger)
	return err
}
