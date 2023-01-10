/*
Copyright 2020 The Kubernetes Authors.

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

package healthchecksl4

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/healthchecksprovider"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	// L4 Load Balancer parameters
	gceSharedHcCheckIntervalSeconds = int64(8) // Shared Health check Interval
	gceLocalHcCheckIntervalSeconds  = int64(3) // Local Health check Interval
	gceHcTimeoutSeconds             = int64(1)
	// Start sending requests as soon as one healthcheck succeeds.
	gceHcHealthyThreshold         = int64(1)
	gceSharedHcUnhealthyThreshold = int64(3) // 3  * 8 = 24 seconds before the LB will steer traffic away
	gceLocalHcUnhealthyThreshold  = int64(2) // 2  * 3 = 6 seconds before the LB will steer traffic away
	L4ILBIPv6HCRange              = "2600:2d00:1:b029::/64"
)

var (
	// sharedLock used to prevent race condition between shared health checks and firewalls.
	sharedLock = &sync.Mutex{}
)

type l4HealthChecks struct {
	// sharedResourceLock serializes operations on the healthcheck and firewall
	// resources shared across multiple Services.
	sharedResourcesLock *sync.Mutex
	hcProvider          healthChecksProvider
	cloud               *gce.Cloud
	recorder            record.EventRecorder
}

func NewL4HealthChecks(cloud *gce.Cloud, recorder record.EventRecorder) *l4HealthChecks {
	return &l4HealthChecks{
		sharedResourcesLock: sharedLock,
		cloud:               cloud,
		recorder:            recorder,
		hcProvider:          healthchecksprovider.NewHealthChecks(cloud, meta.VersionGA),
	}
}

// Fake creates instance of l4HealthChecks with independent lock. Use for test only.
func Fake(cloud *gce.Cloud, recorder record.EventRecorder) *l4HealthChecks {
	return &l4HealthChecks{
		sharedResourcesLock: &sync.Mutex{},
		cloud:               cloud,
		recorder:            recorder,
		hcProvider:          healthchecksprovider.NewHealthChecks(cloud, meta.VersionGA),
	}
}

// Returns an interval constant based of shared/local status
func healthcheckInterval(isShared bool) int64 {
	if isShared {
		return gceSharedHcCheckIntervalSeconds
	} else {
		return gceLocalHcCheckIntervalSeconds
	}
}

// Returns a threshold for unhealthy instance based of shared/local status
func healthcheckUnhealthyThreshold(isShared bool) int64 {
	if isShared {
		return gceSharedHcUnhealthyThreshold
	} else {
		return gceLocalHcUnhealthyThreshold
	}
}

// EnsureHealthCheckWithFirewall exist for the L4
// LoadBalancer Service.
//
// The healthcheck and firewall will be shared between different K8s
// Services for ExternalTrafficPolicy = Cluster, as the same
// configuration is used across all Services of this type.
//
// Firewall rules are always created at in the Global scope (vs
// Regional). This means that one Firewall rule is created for
// Services of different scope (Global vs Regional).
func (l4hc *l4HealthChecks) EnsureHealthCheckWithFirewall(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType, nodeNames []string) *EnsureHealthCheckResult {
	return l4hc.EnsureHealthCheckWithDualStackFirewalls(svc, namer, sharedHC, scope, l4Type, nodeNames /*create IPv4*/, true /*don't create IPv6*/, false)
}

func (l4hc *l4HealthChecks) EnsureHealthCheckWithDualStackFirewalls(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType, nodeNames []string, needsIPv4 bool, needsIPv6 bool) *EnsureHealthCheckResult {
	namespacedName := types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}

	hcName := namer.L4HealthCheck(svc.Namespace, svc.Name, sharedHC)
	hcPath, hcPort := helpers.GetServiceHealthCheckPathPort(svc)
	klog.V(3).Infof("Ensuring L4 healthcheck %s with firewalls for service %s, shared: %v.", hcName, namespacedName.String(), sharedHC)

	if sharedHC {
		hcPath, hcPort = gce.GetNodesHealthCheckPath(), gce.GetNodesHealthCheckPort()
		// We need to acquire a controller-wide mutex to ensure that in the case of a healthcheck shared between loadbalancers that the sync of the GCE resources is not performed in parallel.
		l4hc.sharedResourcesLock.Lock()
		defer l4hc.sharedResourcesLock.Unlock()
	}
	klog.V(3).Infof("L4 Healthcheck %s, expected path: %q, expected port %d", hcName, hcPath, hcPort)

	hcLink, err := l4hc.ensureHealthCheck(hcName, namespacedName, sharedHC, hcPath, hcPort, scope, l4Type)
	if err != nil {
		klog.Errorf("Error while ensuring hc %s, error: %v", hcName, err)
		return &EnsureHealthCheckResult{
			GceResourceInError: annotations.HealthcheckResource,
			Err:                err,
		}
	}

	hcResult := &EnsureHealthCheckResult{
		HCName: hcName,
		HCLink: hcLink,
	}

	if needsIPv4 {
		klog.V(3).Infof("Ensuring IPv4 firewall rule for health check %s for service %s", hcName, namespacedName.String())
		l4hc.ensureIPv4Firewall(svc, namer, hcPort, sharedHC, nodeNames, hcResult)
	}

	if needsIPv6 {
		klog.V(3).Infof("Ensuring IPv6 firewall rule for health check %s for service %s", hcName, namespacedName.String())
		l4hc.ensureIPv6Firewall(svc, namer, hcPort, sharedHC, nodeNames, hcResult)
	}

	return hcResult
}

func (l4hc *l4HealthChecks) ensureHealthCheck(hcName string, svcName types.NamespacedName, shared bool, path string, port int32, scope meta.KeyType, l4Type utils.L4LBType) (string, error) {
	start := time.Now()
	klog.V(2).Infof("Ensuring healthcheck %s for service %s, shared = %v, path = %s, port = %d, scope = %s, l4Type = %s", hcName, svcName, shared, path, port, scope, l4Type.ToString())
	defer func() {
		klog.V(2).Infof("Finished ensuring healthcheck %s for service %s, time taken: %v", hcName, svcName, time.Since(start))
	}()

	hc, err := l4hc.hcProvider.Get(hcName, scope)
	if err != nil {
		return "", err
	}

	var region string
	if scope == meta.Regional {
		region = l4hc.cloud.Region()
	}
	expectedHC := newL4HealthCheck(hcName, svcName, shared, path, port, l4Type, scope, region)

	if hc == nil {
		// Create the healthcheck
		klog.V(2).Infof("Creating healthcheck %s for service %s, shared = %v. Expected healthcheck: %v", hcName, svcName, shared, expectedHC)
		err = l4hc.hcProvider.Create(expectedHC)
		if err != nil {
			return "", err
		}
		selfLink, err := l4hc.hcProvider.SelfLink(expectedHC.Name, scope)
		if err != nil {
			return "", err
		}
		return selfLink, nil
	}
	selfLink := hc.SelfLink
	if !needToUpdateHealthChecks(hc, expectedHC) {
		// nothing to do
		klog.V(3).Infof("Healthcheck %s already exists and does not require update", hcName)
		return selfLink, nil
	}
	mergeHealthChecks(hc, expectedHC)
	klog.V(2).Infof("Updating healthcheck %s for service %s, updated healthcheck: %v", hcName, svcName, expectedHC)
	err = l4hc.hcProvider.Update(expectedHC.Name, scope, expectedHC)
	if err != nil {
		return selfLink, err
	}
	return selfLink, err
}

// ensureIPv4Firewall rule for `svc`.
//
// L4 ILB and L4 NetLB Services with ExternalTrafficPolicy=Cluster use the same firewall
// rule at global scope.
func (l4hc *l4HealthChecks) ensureIPv4Firewall(svc *corev1.Service, namer namer.L4ResourcesNamer, hcPort int32, isSharedHC bool, nodeNames []string, hcResult *EnsureHealthCheckResult) {
	hcFwName := namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, isSharedHC)

	start := time.Now()
	klog.V(2).Infof("Ensuring IPv4 Firewall for health check %s for service %s/%s, health check port %d, shared health check: %t, len(nodeNames): %d", hcFwName, svc.Namespace, svc.Name, hcPort, isSharedHC, len(nodeNames))
	defer func() {
		klog.V(2).Infof("Finished ensuring IPv4 firewall for health check %s for service %s/%s, time taken %v", hcFwName, svc.Namespace, svc.Name, time.Since(start))
	}()

	hcFWRParams := firewalls.FirewallParams{
		PortRanges:   []string{strconv.Itoa(int(hcPort))},
		SourceRanges: gce.L4LoadBalancerSrcRanges(),
		Protocol:     string(corev1.ProtocolTCP),
		Name:         hcFwName,
		NodeNames:    nodeNames,
	}
	err := firewalls.EnsureL4LBFirewallForHc(svc, isSharedHC, &hcFWRParams, l4hc.cloud, l4hc.recorder)
	if err != nil {
		klog.Errorf("Error ensuring IPv4 Firewall %s for health check for service %s/%s, error %v", hcFwName, svc.Namespace, svc.Name, err)
		hcResult.GceResourceInError = annotations.FirewallForHealthcheckResource
		hcResult.Err = err
		return
	}
	hcResult.HCFirewallRuleName = hcFwName
}

func (l4hc *l4HealthChecks) ensureIPv6Firewall(svc *corev1.Service, namer namer.L4ResourcesNamer, hcPort int32, isSharedHC bool, nodeNames []string, hcResult *EnsureHealthCheckResult) {
	ipv6HCFWName := namer.L4IPv6HealthCheckFirewall(svc.Namespace, svc.Name, isSharedHC)

	start := time.Now()
	klog.V(2).Infof("Ensuring IPv6 Firewall %s for health check for service %s/%s, health check port %s, shared health check: %t, len(nodeNames): %d", ipv6HCFWName, svc.Namespace, svc.Name, hcPort, isSharedHC, len(nodeNames))
	defer func() {
		klog.V(2).Infof("Finished ensuring IPv6 firewall %s for service %s/%s, time taken %v", ipv6HCFWName, svc.Namespace, svc.Name, time.Since(start))
	}()

	hcFWRParams := firewalls.FirewallParams{
		PortRanges:   []string{strconv.Itoa(int(hcPort))},
		SourceRanges: []string{L4ILBIPv6HCRange},
		Protocol:     string(corev1.ProtocolTCP),
		Name:         ipv6HCFWName,
		NodeNames:    nodeNames,
	}
	err := firewalls.EnsureL4LBFirewallForHc(svc, isSharedHC, &hcFWRParams, l4hc.cloud, l4hc.recorder)
	if err != nil {
		klog.Errorf("Error ensuring IPv6 Firewall %s for health check for service %s/%s, error %v", ipv6HCFWName, svc.Namespace, svc.Name, err)
		hcResult.GceResourceInError = annotations.FirewallForHealthcheckIPv6Resource
		hcResult.Err = err
		return
	}
	hcResult.HCFirewallRuleIPv6Name = ipv6HCFWName
}

func (l4hc *l4HealthChecks) DeleteHealthCheckWithFirewall(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType) (string, error) {
	return l4hc.deleteHealthCheckWithDualStackFirewalls(svc, namer, sharedHC, scope, l4Type /* don't delete ipv6 */, false)
}

// DeleteHealthCheckWithDualStackFirewalls deletes health check, ipv4 and ipv6 firewall rules for l4 service.
// Checks if shared resources are safe to delete.
func (l4hc *l4HealthChecks) DeleteHealthCheckWithDualStackFirewalls(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType) (string, error) {
	return l4hc.deleteHealthCheckWithDualStackFirewalls(svc, namer, sharedHC, scope, l4Type /* delete ipv6 */, true)
}

// deleteHealthCheckWithDualStackFirewalls deletes health check, ipv4  firewall rule
// and ipv6 firewall if running in dual-stack mode for l4 service.
// Checks if shared resources are safe to delete.
func (l4hc *l4HealthChecks) deleteHealthCheckWithDualStackFirewalls(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType, deleteIPv6 bool) (string, error) {
	if sharedHC {
		// We need to acquire a controller-wide mutex to ensure that in the case of a healthcheck shared between loadbalancers that the sync of the GCE resources is not performed in parallel.
		l4hc.sharedResourcesLock.Lock()
		defer l4hc.sharedResourcesLock.Unlock()
	}

	klog.V(3).Infof("Trying to delete L4 healthcheck and firewall rule for service %s/%s, shared: %v, scope: %v", svc.Namespace, svc.Name, sharedHC, scope)
	hcWasDeleted, err := l4hc.deleteHealthCheck(svc, namer, sharedHC, scope)
	if err != nil {
		return annotations.HealthcheckResource, err
	}
	if !hcWasDeleted {
		return "", nil
	}

	resourceInError, err := l4hc.deleteIPv4HealthCheckFirewall(svc, namer, sharedHC, l4Type)
	if err != nil {
		return resourceInError, err
	}
	if deleteIPv6 {
		resourceInError, err = l4hc.deleteIPv6HealthCheckFirewall(svc, namer, sharedHC, l4Type)
		if err != nil {
			return resourceInError, err
		}
	}
	return "", nil
}

func (l4hc *l4HealthChecks) deleteHealthCheck(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType) (bool, error) {
	hcName := namer.L4HealthCheck(svc.Namespace, svc.Name, sharedHC)

	start := time.Now()
	klog.V(3).Infof("Deleting L4 healthcheck %s for service %s/%s, shared: %v, scope: %v", hcName, svc.Namespace, svc.Name, sharedHC, scope)
	defer func() {
		klog.V(3).Infof("Finished deleting L4 healthcheck %s for service %s/%s, time taken: %v", hcName, svc.Namespace, svc.Name, time.Since(start))
	}()

	err := l4hc.hcProvider.Delete(hcName, scope)
	if err != nil {
		// Ignore deletion error due to health check in use by another resource.
		if !utils.IsInUsedByError(err) {
			klog.Errorf("Failed to delete healthcheck %s for service %s/%s - %v", hcName, svc.Namespace, svc.Name, err)
			return false, err
		}
		klog.V(2).Infof("Failed to delete healthcheck %s is in use by other resource. Health check is shared in GKE = %t", hcName, sharedHC)
		return false, nil
	}
	return true, nil
}

func (l4hc *l4HealthChecks) deleteIPv4HealthCheckFirewall(svc *corev1.Service, namer namer.L4ResourcesNamer, isSharedHC bool, l4type utils.L4LBType) (string, error) {
	hcName := namer.L4HealthCheck(svc.Namespace, svc.Name, isSharedHC)
	hcFwName := namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, isSharedHC)

	start := time.Now()
	klog.V(3).Infof("Deleting IPv4 Firewall %s for health check %s", hcFwName, hcName)
	defer func() {
		klog.V(3).Infof("Finished deleting IPv4 Firewall %s for health check %s, time taken: %v", hcFwName, hcName, time.Since(start))
	}()

	return l4hc.deleteHealthCheckFirewall(svc, hcName, hcFwName, isSharedHC, l4type)
}

func (l4hc *l4HealthChecks) deleteIPv6HealthCheckFirewall(svc *corev1.Service, namer namer.L4ResourcesNamer, isSharedHC bool, l4type utils.L4LBType) (string, error) {
	hcName := namer.L4HealthCheck(svc.Namespace, svc.Name, isSharedHC)
	ipv6hcFwName := namer.L4IPv6HealthCheckFirewall(svc.Namespace, svc.Name, isSharedHC)

	start := time.Now()
	klog.V(3).Infof("Deleting IPv6 Firewall %s for health check %s", ipv6hcFwName, hcName)
	defer func() {
		klog.V(3).Infof("Finished deleting IPv6 Firewall %s for health check %s, time taken: %v", ipv6hcFwName, hcName, time.Since(start))
	}()

	return l4hc.deleteHealthCheckFirewall(svc, hcName, ipv6hcFwName, isSharedHC, l4type)
}

func (l4hc *l4HealthChecks) deleteHealthCheckFirewall(svc *corev1.Service, hcName, hcFwName string, sharedHC bool, l4Type utils.L4LBType) (string, error) {
	namespacedName := types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}

	safeToDelete, err := l4hc.healthCheckFirewallSafeToDelete(hcName, sharedHC, l4Type)
	if err != nil {
		klog.Errorf("Failed to delete health check firewall rule %s for service %s - %v", hcFwName, namespacedName.String(), err)
		return annotations.HealthcheckResource, err
	}
	if !safeToDelete {
		klog.V(3).Infof("Failed to delete health check firewall rule %s: health check in use.", hcName)
		return "", nil
	}
	klog.V(3).Infof("Deleting healthcheck firewall rule named: %s", hcFwName)
	// Delete healthcheck firewall rule if no healthcheck uses the firewall rule.
	err = l4hc.deleteFirewall(hcFwName, svc)
	if err != nil {
		klog.Errorf("Failed to delete firewall rule %s for loadbalancer service %s, err %v", hcFwName, namespacedName.String(), err)
		return annotations.FirewallForHealthcheckResource, err
	}
	return "", nil
}

func (l4hc *l4HealthChecks) healthCheckFirewallSafeToDelete(hcName string, sharedHC bool, l4Type utils.L4LBType) (bool, error) {
	if !sharedHC {
		return true, nil
	}
	var scopeToCheck meta.KeyType
	scopeToCheck = meta.Regional
	if l4Type == utils.XLB {
		scopeToCheck = meta.Global
	}

	hc, err := l4hc.hcProvider.Get(hcName, scopeToCheck)
	if err != nil {
		return false, fmt.Errorf("l4hc.hcProvider.Get(%s, %s) returned error %w, want nil", hcName, scopeToCheck, err)
	}
	return hc == nil, nil
}

func (l4hc *l4HealthChecks) deleteFirewall(name string, svc *corev1.Service) error {
	err := firewalls.EnsureL4FirewallRuleDeleted(l4hc.cloud, name)
	if err == nil {
		return nil
	}
	// Suppress Firewall XPN error, as this is no retryable and requires action by security admin
	if fwErr, ok := err.(*firewalls.FirewallXPNError); ok {
		l4hc.recorder.Eventf(svc, corev1.EventTypeNormal, "XPN", fwErr.Message)
		return nil
	}
	return err
}

func newL4HealthCheck(name string, svcName types.NamespacedName, shared bool, path string, port int32, l4Type utils.L4LBType, scope meta.KeyType, region string) *composite.HealthCheck {
	httpSettings := composite.HTTPHealthCheck{
		Port:        int64(port),
		RequestPath: path,
	}

	desc, err := utils.MakeL4LBServiceDescription(svcName.String(), "", meta.VersionGA, shared, l4Type)
	if err != nil {
		klog.Warningf("Failed to generate description for L4HealthCheck %s, err %v", name, err)
	}
	// Get constant values based on shared/local status
	interval := healthcheckInterval(shared)
	unhealthyThreshold := healthcheckUnhealthyThreshold(shared)

	return &composite.HealthCheck{
		Name:               name,
		CheckIntervalSec:   interval,
		TimeoutSec:         gceHcTimeoutSeconds,
		HealthyThreshold:   gceHcHealthyThreshold,
		UnhealthyThreshold: unhealthyThreshold,
		HttpHealthCheck:    &httpSettings,
		Type:               "HTTP",
		Description:        desc,
		Scope:              scope,
		// Region will be omitted by GCP API if Scope is set to Global
		Region: region,
	}
}

// mergeHealthChecks reconciles HealthCheck config to be no smaller than
// the default values. newHC is assumed to have defaults,
// since it is created by the newL4HealthCheck call.
// E.g. old health check interval is 2s, new has the default of 8.
// The HC interval will be reconciled to 8 seconds.
// If the existing health check values are larger than the default interval,
// the existing configuration will be kept.
func mergeHealthChecks(hc, newHC *composite.HealthCheck) {
	if hc.CheckIntervalSec > newHC.CheckIntervalSec {
		newHC.CheckIntervalSec = hc.CheckIntervalSec
	}
	if hc.TimeoutSec > newHC.TimeoutSec {
		newHC.TimeoutSec = hc.TimeoutSec
	}
	if hc.UnhealthyThreshold > newHC.UnhealthyThreshold {
		newHC.UnhealthyThreshold = hc.UnhealthyThreshold
	}
	if hc.HealthyThreshold > newHC.HealthyThreshold {
		newHC.HealthyThreshold = hc.HealthyThreshold
	}
}

// needToUpdateHealthChecks checks whether the healthcheck needs to be updated.
func needToUpdateHealthChecks(hc, newHC *composite.HealthCheck) bool {
	return hc.HttpHealthCheck == nil ||
		newHC.HttpHealthCheck == nil ||
		hc.HttpHealthCheck.Port != newHC.HttpHealthCheck.Port ||
		hc.HttpHealthCheck.RequestPath != newHC.HttpHealthCheck.RequestPath ||
		hc.Description != newHC.Description ||
		hc.CheckIntervalSec < newHC.CheckIntervalSec ||
		hc.TimeoutSec < newHC.TimeoutSec ||
		hc.UnhealthyThreshold < newHC.UnhealthyThreshold ||
		hc.HealthyThreshold < newHC.HealthyThreshold
}
