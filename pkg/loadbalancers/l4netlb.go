/*
Copyright 2021 The Kubernetes Authors.

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

package loadbalancers

import (
	"fmt"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/forwardingrules"
	"k8s.io/ingress-gce/pkg/healthchecksl4"
	"k8s.io/ingress-gce/pkg/metrics"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
	"k8s.io/legacy-cloud-providers/gce"
)

// L4NetLB handles the resource creation/deletion/update for a given L4 External LoadBalancer service.
type L4NetLB struct {
	cloud       *gce.Cloud
	backendPool *backends.Backends
	scope       meta.KeyType
	namer       namer.L4ResourcesNamer
	// recorder is used to generate k8s Events.
	recorder        record.EventRecorder
	Service         *corev1.Service
	NamespacedName  types.NamespacedName
	healthChecks    healthchecksl4.L4HealthChecks
	forwardingRules ForwardingRulesProvider
}

// L4NetLBSyncResult contains information about the outcome of an L4 NetLB sync. It stores the list of resource name annotations,
// sync error, the GCE resource that hit the error along with the error type, metrics and more fields.
type L4NetLBSyncResult struct {
	Annotations        map[string]string
	Error              error
	GCEResourceInError string
	Status             *corev1.LoadBalancerStatus
	MetricsState       metrics.L4NetLBServiceState
	SyncType           string
	StartTime          time.Time
}

func NewL4SyncResult(syncType string) *L4NetLBSyncResult {
	result := &L4NetLBSyncResult{
		Annotations: make(map[string]string),
		StartTime:   time.Now(),
		SyncType:    syncType,
	}
	result.MetricsState = metrics.InitL4NetLBServiceState(&result.StartTime)
	return result
}

// SetMetricsForSuccessfulServiceSync should be call after successful sync.
func (r *L4NetLBSyncResult) SetMetricsForSuccessfulServiceSync() {
	r.MetricsState.FirstSyncErrorTime = nil
	r.MetricsState.InSuccess = true
}

type L4NetLBParams struct {
	Service  *corev1.Service
	Cloud    *gce.Cloud
	Namer    namer.L4ResourcesNamer
	Recorder record.EventRecorder
}

// NewL4NetLB creates a new Handler for the given L4NetLB service.
func NewL4NetLB(params *L4NetLBParams) *L4NetLB {
	l4netlb := &L4NetLB{
		cloud:           params.Cloud,
		scope:           meta.Regional,
		namer:           params.Namer,
		recorder:        params.Recorder,
		Service:         params.Service,
		NamespacedName:  types.NamespacedName{Name: params.Service.Name, Namespace: params.Service.Namespace},
		backendPool:     backends.NewPool(params.Cloud, params.Namer),
		healthChecks:    healthchecksl4.NewL4HealthChecks(params.Cloud, params.Recorder),
		forwardingRules: forwardingrules.New(params.Cloud, meta.VersionGA, meta.Regional),
	}
	return l4netlb
}

// createKey generates a meta.Key for a given GCE resource name.
func (l4netlb *L4NetLB) createKey(name string) (*meta.Key, error) {
	return composite.CreateKey(l4netlb.cloud, name, l4netlb.scope)
}

// EnsureFrontend ensures that all frontend resources for the given loadbalancer service have
// been created. It is health check, firewall rules, backend service and forwarding rule.
// It returns a LoadBalancerStatus with the updated ForwardingRule IP address.
// This function does not link instances to Backend Service.
func (l4netlb *L4NetLB) EnsureFrontend(nodeNames []string, svc *corev1.Service) *L4NetLBSyncResult {
	result := NewL4SyncResult(SyncTypeCreate)

	// If service already has an IP assigned, treat it as an update instead of a new Loadbalancer.
	if len(svc.Status.LoadBalancer.Ingress) > 0 {
		result.SyncType = SyncTypeUpdate
	}
	klog.V(3).Infof("EnsureFrontend started for service %s/%s, len(nodeNames) = %d, SyncType: %s", svc.Namespace, svc.Name, len(nodeNames), result.SyncType)

	l4netlb.Service = svc

	sharedHC := !helpers.RequestsOnlyLocalTraffic(svc)
	hcResult := l4netlb.healthChecks.EnsureHealthCheckWithFirewall(l4netlb.Service, l4netlb.namer, sharedHC, l4netlb.scope, utils.XLB, nodeNames)
	if hcResult.Err != nil {
		result.GCEResourceInError = hcResult.GceResourceInError
		result.Error = fmt.Errorf("Failed to ensure health check %s - %w", hcResult.HCName, hcResult.Err)
		return result
	}
	result.Annotations[annotations.HealthcheckKey] = hcResult.HCName
	result.Annotations[annotations.FirewallRuleForHealthcheckKey] = hcResult.HCFirewallRuleName

	bsName := l4netlb.namer.L4Backend(l4netlb.Service.Namespace, l4netlb.Service.Name)
	servicePorts := l4netlb.Service.Spec.Ports
	protocol := utils.GetProtocol(servicePorts)
	bs, err := l4netlb.backendPool.EnsureL4BackendService(bsName, hcResult.HCLink, string(protocol), string(l4netlb.Service.Spec.SessionAffinity), string(cloud.SchemeExternal), l4netlb.NamespacedName)
	if err != nil {
		result.GCEResourceInError = annotations.BackendServiceResource
		result.Error = fmt.Errorf("Failed to ensure backend service %s - %w", bsName, err)
		return result
	}
	result.Annotations[annotations.BackendServiceKey] = bsName

	fr, ipAddrType, err := l4netlb.ensureExternalForwardingRule(bs.SelfLink)
	if err != nil {
		// User can misconfigure the forwarding rule if Network Tier will not match service level Network Tier.
		result.MetricsState.IsUserError = utils.IsUserError(err)
		result.GCEResourceInError = annotations.ForwardingRuleResource
		result.Error = fmt.Errorf("Failed to ensure forwarding rule - %w", err)
		return result
	}
	if fr.IPProtocol == string(corev1.ProtocolTCP) {
		result.Annotations[annotations.TCPForwardingRuleKey] = fr.Name
	} else {
		result.Annotations[annotations.UDPForwardingRuleKey] = fr.Name
	}

	result.Status = utils.AddIPToLBStatus(result.Status, fr.IPAddress)
	result.MetricsState.IsPremiumTier = fr.NetworkTier == cloud.NetworkTierPremium.ToGCEValue()
	result.MetricsState.IsManagedIP = ipAddrType == IPAddrManaged

	firewallName := l4netlb.namer.L4Firewall(l4netlb.Service.Namespace, l4netlb.Service.Name)
	portRanges := utils.GetServicePortRanges(servicePorts)
	res := l4netlb.ensureNodesFirewall(firewallName, nodeNames, fr.IPAddress, portRanges, string(protocol))
	if res.Error != nil {
		return res
	}
	result.Annotations[annotations.FirewallRuleKey] = firewallName

	return result
}

// EnsureLoadBalancerDeleted performs a cleanup of all GCE resources for the given loadbalancer service.
// It is health check, firewall rules and backend service
func (l4netlb *L4NetLB) EnsureLoadBalancerDeleted(svc *corev1.Service) *L4NetLBSyncResult {
	result := NewL4SyncResult(SyncTypeDelete)
	l4netlb.Service = svc

	// If any resource deletion fails, log the error and continue cleanup.
	l4netlb.deleteExternalForwardingRule(result)
	l4netlb.deleteAddress(result)
	l4netlb.deleteNodesFirewall(result)
	l4netlb.deleteBackendService(result)
	l4netlb.deleteHealthChecksWithFirewall(result)

	return result
}

func (l4netlb *L4NetLB) deleteNodesFirewall(result *L4NetLBSyncResult) {
	firewallName := l4netlb.namer.L4Firewall(l4netlb.Service.Namespace, l4netlb.Service.Name)

	start := time.Now()
	klog.V(2).Infof("Deleting nodes firewall %s for L4 NetLB Service %s/%s", firewallName, l4netlb.Service.Namespace, l4netlb.Service.Name)
	defer func() {
		klog.V(2).Infof("Finished deleting nodes firewall %s for L4 NetLB Service %s/%s, time taken: %v", firewallName, l4netlb.Service.Namespace, l4netlb.Service.Name, time.Since(start))
	}()

	err := firewalls.EnsureL4FirewallRuleDeleted(l4netlb.cloud, firewallName)
	if err != nil {
		if fwErr, ok := err.(*firewalls.FirewallXPNError); ok {
			l4netlb.recorder.Eventf(l4netlb.Service, corev1.EventTypeNormal, "XPN", fwErr.Message)
			return
		}

		klog.Errorf("Failed to delete firewall rule %s for service %s - %v", firewallName, l4netlb.NamespacedName.String(), err)
		result.GCEResourceInError = annotations.FirewallRuleResource
		result.Error = err
	}
}

func (l4netlb *L4NetLB) deleteAddress(result *L4NetLBSyncResult) {
	addressName := l4netlb.GetFRName()

	start := time.Now()
	klog.V(2).Infof("Deleting external static address %s for L4 NetLB service %s/%s", addressName, l4netlb.Service.Namespace, l4netlb.Service.Name)
	defer func() {
		klog.V(2).Infof("Finished deleting external static address %s for L4 NetLB service %s/%s, time taken: %v", addressName, l4netlb.Service.Namespace, l4netlb.Service.Name, time.Since(start))
	}()

	err := ensureAddressDeleted(l4netlb.cloud, addressName, l4netlb.cloud.Region())
	if err != nil {
		klog.Errorf("Failed to delete address for service %s - %v", l4netlb.NamespacedName.String(), err)
		result.Error = err
		result.GCEResourceInError = annotations.AddressResource
	}
}

func (l4netlb *L4NetLB) deleteBackendService(result *L4NetLBSyncResult) {
	bsName := l4netlb.namer.L4Backend(l4netlb.Service.Namespace, l4netlb.Service.Name)

	start := time.Now()
	klog.V(2).Infof("Deleting backend service %s, for L4 NetLB Service %s/%s", bsName, l4netlb.Service.Namespace, l4netlb.Service.Name)
	defer func() {
		klog.V(2).Infof("Finished deleting backend service %s, for L4 NetLB Service %s/%s, time taken", bsName, l4netlb.Service.Namespace, l4netlb.Service.Name, time.Since(start))
	}()

	err := utils.IgnoreHTTPNotFound(l4netlb.backendPool.Delete(bsName, meta.VersionGA, meta.Regional))
	if err != nil {
		klog.Errorf("Failed to delete backends for L4 External LoadBalancer service %s - %v", l4netlb.NamespacedName.String(), err)
		result.GCEResourceInError = annotations.BackendServiceResource
		result.Error = err
	}
}

func (l4netlb *L4NetLB) deleteHealthChecksWithFirewall(result *L4NetLBSyncResult) {
	start := time.Now()
	klog.V(2).Infof("Deleting all health checks and firewalls for health checks for L4 NetLB service %s/%s", l4netlb.Service.Namespace, l4netlb.Service.Name)
	defer func() {
		klog.V(2).Infof("Finished deleting all health checks and firewalls for health checks for for L4 NetLB service %s/%s, time taken: %v", l4netlb.Service.Namespace, l4netlb.Service.Name, time.Since(start))
	}()

	// Delete healthcheck
	// We don't delete health check during service update so
	// it is possible that there might be some health check leak
	// when externalTrafficPolicy is changed from Local to Cluster and new a health check was created.
	// When service is deleted we need to check both health checks shared and non-shared
	// and delete them if needed.
	for _, isShared := range []bool{true, false} {
		resourceInError, err := l4netlb.healthChecks.DeleteHealthCheckWithFirewall(l4netlb.Service, l4netlb.namer, isShared, meta.Regional, utils.XLB)
		if err != nil {
			result.GCEResourceInError = resourceInError
			result.Error = err
			// continue with deletion of the non-shared Healthcheck regardless of the error, both healthchecks may need to be deleted,
		}
	}
}

// GetFRName returns the name of the forwarding rule for the given L4 External LoadBalancer service.
// This name should align with legacy forwarding rule name because we use forwarding rule to determine
// which controller should process the service Ingress-GCE or k/k service controller.
func (l4netlb *L4NetLB) GetFRName() string {
	return utils.LegacyForwardingRuleName(l4netlb.Service)
}

func (l4netlb *L4NetLB) ensureNodesFirewall(name string, nodeNames []string, ipAddress string, portRanges []string, protocol string) *L4NetLBSyncResult {
	start := time.Now()
	klog.V(2).Infof("Ensuring nodes firewall %s for L4 NetLB Service %s/%s, ipAddress: %s, protocol: %s, len(nodeNames): %v, portRanges: %v", name, l4netlb.Service.Namespace, l4netlb.Service.Name, ipAddress, protocol, len(nodeNames), portRanges)
	defer func() {
		klog.V(2).Infof("Finished ensuring nodes firewall %s for L4 NetLB Service %s/%s, time taken: %v", name, l4netlb.Service.Namespace, l4netlb.Service.Name, time.Since(start))
	}()

	result := &L4NetLBSyncResult{}
	sourceRanges, err := helpers.GetLoadBalancerSourceRanges(l4netlb.Service)
	if err != nil {
		result.Error = err
		return result
	}

	// Add firewall rule for L4 External LoadBalancer traffic to nodes
	nodesFWRParams := firewalls.FirewallParams{
		PortRanges:        portRanges,
		SourceRanges:      sourceRanges.StringSlice(),
		DestinationRanges: []string{ipAddress},
		Protocol:          protocol,
		Name:              name,
		IP:                l4netlb.Service.Spec.LoadBalancerIP,
		NodeNames:         nodeNames,
	}
	result.Error = firewalls.EnsureL4LBFirewallForNodes(l4netlb.Service, &nodesFWRParams, l4netlb.cloud, l4netlb.recorder)
	if result.Error != nil {
		result.GCEResourceInError = annotations.FirewallRuleResource
		result.Error = err
		return result
	}
	return result
}
