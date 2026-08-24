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

package healthchecks

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/l4/address"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/validation"
	"k8s.io/klog/v2"
	netutils "k8s.io/utils/net"
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
	L4NetLBIPv6HCRange            = "2600:1901:8001::/48"
)

// sharedLock used to prevent race condition between shared health checks and firewalls.
var sharedLock = &sync.Mutex{}

type l4HealthChecks struct {
	// sharedResourceLock serializes operations on the healthcheck and firewall
	// resources shared across multiple Services.
	sharedResourcesLock *sync.Mutex
	hcProvider          healthChecksProvider
	cloud               *gce.Cloud
	recorder            record.EventRecorder
	useDenyFirewalls    bool
}

func NewL4HealthChecks(cloud *gce.Cloud, recorder record.EventRecorder, logger klog.Logger, useDenyFirewalls bool) *l4HealthChecks {
	logger = logger.WithName("L4HealthChecks")
	return &l4HealthChecks{
		sharedResourcesLock: sharedLock,
		cloud:               cloud,
		recorder:            recorder,
		hcProvider:          NewHealthChecks(cloud, meta.VersionGA, logger),
		useDenyFirewalls:    useDenyFirewalls,
	}
}

// Fake creates instance of l4HealthChecks with independent lock. Use for test only.
func Fake(cloud *gce.Cloud, recorder record.EventRecorder) *l4HealthChecks {
	return &l4HealthChecks{
		sharedResourcesLock: &sync.Mutex{},
		cloud:               cloud,
		recorder:            recorder,
		hcProvider:          NewHealthChecks(cloud, meta.VersionGA, klog.TODO()),
		useDenyFirewalls:    true,
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
func (l4hc *l4HealthChecks) EnsureHealthCheckWithFirewall(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType, nodeNames []string, svcNetwork network.NetworkInfo, svcLogger klog.Logger) *EnsureHealthCheckResult {
	return l4hc.EnsureHealthCheckWithDualStackFirewalls(svc, namer, sharedHC, scope, l4Type, nodeNames /*create IPv4*/, true /*don't create IPv6*/, false, svcNetwork, svcLogger)
}

func (l4hc *l4HealthChecks) EnsureHealthCheckWithDualStackFirewalls(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType, nodeNames []string, needsIPv4 bool, needsIPv6 bool, svcNetwork network.NetworkInfo, svcLogger klog.Logger) *EnsureHealthCheckResult {
	namespacedName := types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}

	hcName := namer.L4HealthCheck(svc.Namespace, svc.Name, sharedHC)
	hcPath, hcPort := helpers.GetServiceHealthCheckPathPort(svc)
	hcLogger := svcLogger.WithValues("healthcheckName", hcName)
	hcLogger.V(3).Info("Ensuring L4 healthcheck with firewalls for service", "shared", sharedHC)

	if sharedHC {
		hcPath, hcPort = gce.GetNodesHealthCheckPath(), gce.GetNodesHealthCheckPort()
		// We need to acquire a controller-wide mutex to ensure that in the case of a healthcheck shared between loadbalancers that the sync of the GCE resources is not performed in parallel.
		l4hc.sharedResourcesLock.Lock()
		defer l4hc.sharedResourcesLock.Unlock()
	}
	hcLogger.V(3).Info("L4 Healthcheck", "expectedPath", hcPath, "expectedPort", hcPort)

	hcLink, wasUpdate, err := l4hc.ensureHealthCheck(hcName, namespacedName, sharedHC, hcPath, hcPort, scope, l4Type, hcLogger)
	if err != nil {
		hcLogger.Error(err, "Error while ensuring hc")
		return &EnsureHealthCheckResult{
			GceResourceInError: annotations.HealthcheckResource,
			Err:                err,
			WasUpdated:         l4utils.ResourceResync,
		}
	}

	hcResult := &EnsureHealthCheckResult{
		HCName:     hcName,
		HCLink:     hcLink,
		WasUpdated: wasUpdate,
	}

	isSharedFirewall := sharedHC
	if !svcNetwork.IsDefault {
		isSharedFirewall = false
	}

	if needsIPv4 {
		hcLogger.V(3).Info("Ensuring IPv4 firewall rule for health check for service")
		l4hc.ensureFirewall(l4Type, address.IPv4Version, isSharedFirewall, svc, namer, hcPort, nodeNames, hcResult, svcNetwork, hcLogger)
	}

	if needsIPv6 {
		hcLogger.V(3).Info("Ensuring IPv6 firewall rule for health check for service")
		l4hc.ensureFirewall(l4Type, address.IPv6Version, isSharedFirewall, svc, namer, hcPort, nodeNames, hcResult, svcNetwork, hcLogger)
	}

	return hcResult
}

func (l4hc *l4HealthChecks) ensureHealthCheck(hcName string, svcName types.NamespacedName, shared bool, path string, port int32, scope meta.KeyType, l4Type utils.L4LBType, hcLogger klog.Logger) (string, l4utils.ResourceSyncStatus, error) {
	start := time.Now()
	hcLogger.V(2).Info("Ensuring healthcheck for service", "shared", shared, "path", path, "port", port, "scope", scope, "l4Type", l4Type.ToString())
	defer func() {
		hcLogger.V(2).Info("Finished ensuring healthcheck for service", "timeTaken", time.Since(start))
	}()

	hc, err := l4hc.hcProvider.Get(hcName, scope)
	if err != nil {
		return "", l4utils.ResourceResync, err
	}

	var region string
	if scope == meta.Regional {
		region = l4hc.cloud.Region()
	}
	expectedHC := newL4HealthCheck(hcName, svcName, shared, path, port, l4Type, scope, region, hcLogger)

	if hc == nil {
		// Create the healthcheck
		hcLogger.V(2).Info("Creating healthcheck for service", "shared", shared, "expectedHealthcheck", expectedHC)
		err = l4hc.hcProvider.Create(expectedHC)
		if err != nil {
			return "", l4utils.ResourceResync, err
		}
		selfLink, err := l4hc.hcProvider.SelfLink(expectedHC.Name, scope)
		if err != nil {
			return "", l4utils.ResourceResync, err
		}
		return selfLink, l4utils.ResourceUpdate, nil
	}
	selfLink := hc.SelfLink
	if !needToUpdateHealthChecks(hc, expectedHC) {
		// nothing to do
		hcLogger.V(3).Info("Healthcheck already exists and does not require update")
		return selfLink, l4utils.ResourceResync, nil
	}
	mergeHealthChecks(hc, expectedHC)
	hcLogger.V(2).Info("Updating healthcheck for service", "updatedHealthcheck", expectedHC)
	err = l4hc.hcProvider.Update(expectedHC.Name, scope, expectedHC)
	if err != nil {
		return selfLink, l4utils.ResourceUpdate, err
	}
	return selfLink, l4utils.ResourceUpdate, err
}

// ensureFirewall creates a firewall rule for `svc` allowing health check probes.
//
// L4 ILB and L4 NetLB Services with ExternalTrafficPolicy=Cluster use the same firewall
// rule at global scope.
func (l4hc *l4HealthChecks) ensureFirewall(l4Type utils.L4LBType, ipVersion address.IPVersion, isSharedHC bool, svc *corev1.Service, namer namer.L4ResourcesNamer, hcPort int32, nodeNames []string, hcResult *EnsureHealthCheckResult, svcNetwork network.NetworkInfo, svcLogger klog.Logger) {
	var hcFwName string
	var gceResourceInError string
	ipFamily := "IPv4"

	if ipVersion == address.IPv6Version {
		ipFamily = "IPv6"
		hcFwName = namer.L4IPv6HealthCheckFirewall(svc.Namespace, svc.Name, isSharedHC)
		gceResourceInError = annotations.FirewallForHealthcheckIPv6Resource
	} else {
		hcFwName = namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, isSharedHC)
		gceResourceInError = annotations.FirewallForHealthcheckResource
	}

	sourceRanges := getHCFirewallSourceRanges(l4Type, isSharedHC, ipVersion)

	start := time.Now()

	fwLogger := svcLogger.WithValues("healthcheckFirewallName", hcFwName)
	fwLogger.V(2).Info(fmt.Sprintf("Ensuring %s Firewall for health check for service", ipFamily), "healthcheckPort", hcPort, "shared", isSharedHC, "len(nodeNames)", len(nodeNames))
	defer func() {
		fwLogger.V(2).Info(fmt.Sprintf("Finished ensuring %s firewall for health check for service", ipFamily), "timeTaken", time.Since(start))
	}()

	hcFWRParams := firewalls.FirewallParams{
		Allowed: []*compute.FirewallAllowed{
			{
				IPProtocol: string(corev1.ProtocolTCP),
				Ports:      []string{strconv.Itoa(int(hcPort))},
			},
		},
		SourceRanges: sourceRanges,
		Name:         hcFwName,
		NodeNames:    nodeNames,
		Network:      svcNetwork,
		Priority:     l4hc.firewallPriority(),
	}

	wasUpdated, err := firewalls.EnsureL4LBFirewallForHc(svc, isSharedHC, &hcFWRParams, l4hc.cloud, l4hc.recorder, fwLogger)
	hcResult.WasFirewallUpdated = wasUpdated == l4utils.ResourceUpdate || hcResult.WasFirewallUpdated == l4utils.ResourceUpdate

	if err != nil {
		fwLogger.Error(err, fmt.Sprintf("Error ensuring %s Firewall for health check for service", ipFamily))
		hcResult.GceResourceInError = gceResourceInError
		hcResult.Err = err
		return
	}

	if ipVersion == address.IPv6Version {
		hcResult.HCFirewallRuleIPv6Name = hcFwName
	} else {
		hcResult.HCFirewallRuleName = hcFwName
	}
}

func (l4hc *l4HealthChecks) firewallPriority() *int {
	if l4hc.useDenyFirewalls {
		return firewalls.AllowTrafficPriority
	}
	return nil // when the field is unset the priority by default is 1000
}

func (l4hc *l4HealthChecks) DeleteHealthCheckWithFirewall(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType, svcLogger klog.Logger) (string, error) {
	return l4hc.deleteHealthCheckWithDualStackFirewalls(svc, namer, sharedHC, scope, l4Type /* don't delete ipv6 */, false, svcLogger)
}

// DeleteHealthCheckWithDualStackFirewalls deletes health check, ipv4 and ipv6 firewall rules for l4 service.
// Checks if shared resources are safe to delete.
func (l4hc *l4HealthChecks) DeleteHealthCheckWithDualStackFirewalls(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType, svcLogger klog.Logger) (string, error) {
	return l4hc.deleteHealthCheckWithDualStackFirewalls(svc, namer, sharedHC, scope, l4Type /* delete ipv6 */, true, svcLogger)
}

// deleteHealthCheckWithDualStackFirewalls deletes health check, ipv4  firewall rule
// and ipv6 firewall if running in dual-stack mode for l4 service.
// Checks if shared resources are safe to delete.
func (l4hc *l4HealthChecks) deleteHealthCheckWithDualStackFirewalls(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType, deleteIPv6 bool, svcLogger klog.Logger) (string, error) {
	if sharedHC {
		// We need to acquire a controller-wide mutex to ensure that in the case of a healthcheck shared between loadbalancers that the sync of the GCE resources is not performed in parallel.
		l4hc.sharedResourcesLock.Lock()
		defer l4hc.sharedResourcesLock.Unlock()
	}

	svcLogger.V(3).Info("Trying to delete L4 healthcheck and firewall rule for service", "shared", sharedHC, "scope", scope)
	hcWasDeleted, err := l4hc.deleteHealthCheck(svc, namer, sharedHC, scope, svcLogger)
	if err != nil {
		return annotations.HealthcheckResource, err
	}
	if !hcWasDeleted {
		return "", nil
	}

	resourceInError, err := l4hc.deleteIPv4HealthCheckFirewall(svc, namer, sharedHC, l4Type, svcLogger)
	if err != nil {
		return resourceInError, err
	}
	if deleteIPv6 {
		resourceInError, err = l4hc.deleteIPv6HealthCheckFirewall(svc, namer, sharedHC, l4Type, svcLogger)
		if err != nil {
			return resourceInError, err
		}
	}
	return "", nil
}

func (l4hc *l4HealthChecks) deleteHealthCheck(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, svcLogger klog.Logger) (bool, error) {
	start := time.Now()

	hcName := namer.L4HealthCheck(svc.Namespace, svc.Name, sharedHC)

	svcLogger.V(3).Info("Deleting L4 healthcheck for service", "shared", sharedHC, "scope", scope)
	defer func() {
		svcLogger.V(3).Info("Finished deleting L4 healthcheck for service", "timeTaken", time.Since(start))
	}()

	err := l4hc.hcProvider.Delete(hcName, scope)
	if err != nil {
		// Ignore deletion error due to health check in use by another resource.
		if !utils.IsInUsedByError(err) {
			svcLogger.Error(err, "Failed to delete healthcheck for service")
			return false, err
		}
		svcLogger.V(2).Info("Failed to delete healthcheck, it's in use by other resource", "sharedInGke", sharedHC)
		return false, nil
	}
	return true, nil
}

func (l4hc *l4HealthChecks) deleteIPv4HealthCheckFirewall(svc *corev1.Service, namer namer.L4ResourcesNamer, isSharedHC bool, l4type utils.L4LBType, svcLogger klog.Logger) (string, error) {
	hcName := namer.L4HealthCheck(svc.Namespace, svc.Name, isSharedHC)
	hcFwName := namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, isSharedHC)

	start := time.Now()
	fwLogger := svcLogger.WithValues("healthcheckFirewallName", hcFwName)
	fwLogger.V(3).Info("Deleting IPv4 Firewall for a healthcheck")
	defer func() {
		fwLogger.V(3).Info("Finished deleting IPv4 Firewall for a healthcheck", "timeTaken", time.Since(start))
	}()

	return l4hc.deleteHealthCheckFirewall(svc, hcName, hcFwName, isSharedHC, l4type, fwLogger)
}

func (l4hc *l4HealthChecks) deleteIPv6HealthCheckFirewall(svc *corev1.Service, namer namer.L4ResourcesNamer, isSharedHC bool, l4type utils.L4LBType, svcLogger klog.Logger) (string, error) {
	start := time.Now()

	hcName := namer.L4HealthCheck(svc.Namespace, svc.Name, isSharedHC)
	ipv6hcFwName := namer.L4IPv6HealthCheckFirewall(svc.Namespace, svc.Name, isSharedHC)

	fwLogger := svcLogger.WithValues("healthcheckFirewallName", ipv6hcFwName)
	fwLogger.V(3).Info("Deleting IPv6 Firewall for a healthcheck")
	defer func() {
		fwLogger.V(3).Info("Finished deleting IPv6 Firewall for a healthcheck", "timeTaken", time.Since(start))
	}()

	return l4hc.deleteHealthCheckFirewall(svc, hcName, ipv6hcFwName, isSharedHC, l4type, fwLogger)
}

func (l4hc *l4HealthChecks) deleteHealthCheckFirewall(svc *corev1.Service, hcName, hcFwName string, sharedHC bool, l4Type utils.L4LBType, fwLogger klog.Logger) (string, error) {
	safeToDelete, err := l4hc.healthCheckFirewallSafeToDelete(hcName, sharedHC, l4Type)
	if err != nil {
		fwLogger.Error(err, "Failed to delete healthcheck firewall rule for service")
		return annotations.HealthcheckResource, err
	}
	if !safeToDelete {
		fwLogger.V(3).Info("Failed to delete healthcheck firewall rule: health check in use")
		return "", nil
	}
	fwLogger.V(3).Info("Deleting healthcheck firewall rule")
	// Delete healthcheck firewall rule if no healthcheck uses the firewall rule.
	err = l4hc.deleteFirewall(hcFwName, svc, fwLogger)
	if err != nil {
		fwLogger.Error(err, "Failed to delete firewall rule for loadbalancer service")
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

func (l4hc *l4HealthChecks) deleteFirewall(name string, svc *corev1.Service, fwLogger klog.Logger) error {
	err := firewalls.EnsureL4FirewallRuleDeleted(l4hc.cloud, name, fwLogger)
	if err == nil {
		return nil
	}
	// Suppress Firewall XPN error, as this is no retryable and requires action by security admin
	if fwErr, ok := err.(*firewalls.FirewallXPNError); ok {
		l4hc.recorder.Event(svc, corev1.EventTypeNormal, "XPN", fwErr.Message)
		return nil
	}
	return err
}

func newL4HealthCheck(name string, svcName types.NamespacedName, shared bool, path string, port int32, l4Type utils.L4LBType, scope meta.KeyType, region string, hcLogger klog.Logger) *composite.HealthCheck {
	httpSettings := composite.HTTPHealthCheck{
		Port:        int64(port),
		RequestPath: path,
	}

	desc, err := utils.MakeL4LBServiceDescription(svcName.String(), "", meta.VersionGA, shared, l4Type)
	if err != nil {
		hcLogger.Info("Failed to generate description for L4HealthCheck", "err", err)
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

// getHCFirewallSourceRanges returns the list of source CIDR ranges for the health check firewall rule.
//
// Arguments:
// l4Type: The type of L4 load balancer (ILB or XLB/NetLB).
// shared: Indicates whether the firewall rule is shared between different K8s Services (e.g., when ExternalTrafficPolicy=Cluster).
//
//	If shared is true, the firewall rule must include the ranges for both ILB and NetLB because a single global firewall rule is used.
//
// ipVersion: Specifies if we are retrieving ranges for an IPv6 firewall rule. If false, we return IPv4 ranges.
//
// Logic:
// The function gathers the appropriate CIDR ranges based on the LB type(s). If 'shared' is true, it aggregates
// the CIDRs for both ILB and NetLB.
// For each LB type, it parses the override flag (if provided) and extracts the requested IP family.
// If the flag is not provided, or if the extracted ranges are empty (meaning the flag was missing that specific IP family),
// it falls back to the default GCP health check ranges for that LB type and IP family.
// Finally, it removes duplicates from the aggregated ranges.
func getHCFirewallSourceRanges(l4Type utils.L4LBType, shared bool, ipVersion address.IPVersion) []string {
	var lbTypesToProcess []utils.L4LBType

	if shared {
		// If shared, we need to include ranges for both ILB and NetLB
		lbTypesToProcess = []utils.L4LBType{utils.ILB, utils.XLB}
	} else {
		lbTypesToProcess = []utils.L4LBType{l4Type}
	}

	var ranges []string

	for _, lbType := range lbTypesToProcess {
		var overrideFlag string
		var currentRanges []string

		// get custom value if provided:
		if lbType == utils.ILB {
			overrideFlag = flags.F.OverrideL4ILBHealthCheckSourceCIDRs
		} else {
			overrideFlag = flags.F.OverrideL4NetLBHealthCheckSourceCIDRs
		}
		if overrideFlag != "" {
			parsed, err := validation.ParseHealthCheckSourceCIDRs(overrideFlag)
			if err != nil {
				klog.Errorf("Failed to parse health check source CIDRs for %v: %v", lbType, err)
				return []string{} // fail closed
			}
			currentRanges = filterIPFamily(parsed, ipVersion == address.IPv6Version)
		}

		// get default value if flag was not provided or didn't contain the requested IP family
		if len(currentRanges) == 0 {

			if ipVersion == address.IPv6Version {
				if lbType == utils.ILB {
					currentRanges = []string{L4ILBIPv6HCRange}
				} else {
					currentRanges = []string{L4NetLBIPv6HCRange}
				}
			} else {
				// contains IPv4 only ranges for ILB + NetLB
				currentRanges = gce.L4LoadBalancerSrcRanges()
			}
		}

		ranges = append(ranges, currentRanges...)
	}

	return dropDuplicates(ranges)
}

func filterIPFamily(cidrs []string, ipv6 bool) []string {
	var result []string
	for _, cidr := range cidrs {
		if ipv6 && netutils.IsIPv6CIDRString(cidr) {
			result = append(result, cidr)
		}
		if !ipv6 && netutils.IsIPv4CIDRString(cidr) {
			result = append(result, cidr)
		}
	}
	return result
}

func dropDuplicates(cidrs []string) []string {
	var result []string
	seen := make(map[string]bool)
	for _, cidr := range cidrs {
		if !seen[cidr] {
			seen[cidr] = true
			result = append(result, cidr)
		}
	}
	return result
}
