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
	"k8s.io/ingress-gce/pkg/healthchecks"
	"k8s.io/ingress-gce/pkg/metrics"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

// L4NetLB handles the resource creation/deletion/update for a given L4 External LoadBalancer service.
type L4NetLB struct {
	cloud       *gce.Cloud
	backendPool *backends.Backends
	scope       meta.KeyType
	namer       namer.L4ResourcesNamer
	// recorder is used to generate k8s Events.
	recorder       record.EventRecorder
	Service        *corev1.Service
	ServicePort    utils.ServicePort
	NamespacedName types.NamespacedName
	l4HealthChecks healthchecks.L4HealthChecks
	protocol       corev1.Protocol
	SyncResult     *L4NetLBSyncResult
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

// NewL4NetLB creates a new Handler for the given L4NetLB service.
func NewL4NetLB(service *corev1.Service, cloud *gce.Cloud, scope meta.KeyType, namer namer.L4ResourcesNamer, recorder record.EventRecorder) *L4NetLB {
	l4netlb := &L4NetLB{cloud: cloud,
		scope:          scope,
		namer:          namer,
		recorder:       recorder,
		Service:        service,
		NamespacedName: types.NamespacedName{Name: service.Name, Namespace: service.Namespace},
		backendPool:    backends.NewPool(cloud, namer),
		l4HealthChecks: healthchecks.L4(),
		protocol:       utils.GetProtocol(service.Spec.Ports),
	}
	portId := utils.ServicePortID{Service: l4netlb.NamespacedName}
	l4netlb.ServicePort = utils.ServicePort{
		ID:           portId,
		BackendNamer: l4netlb.namer,
		NodePort:     int64(service.Spec.Ports[0].NodePort),
		L4RBSEnabled: true,
	}
	l4netlb.clearSyncResult()
	return l4netlb
}

func (l4netlb *L4NetLB) clearSyncResult() {
	l4netlb.SyncResult = &L4NetLBSyncResult{
		Annotations: make(map[string]string),
		StartTime:   time.Now(),
		SyncType:    SyncTypeCreate,
	}
}

// createKey generates a meta.Key for a given GCE resource name.
func (l4netlb *L4NetLB) createKey(name string) (*meta.Key, error) {
	return composite.CreateKey(l4netlb.cloud, name, l4netlb.scope)
}

// EnsureFrontend ensures that all frontend resources for the given loadbalancer service have
// been created. It is health check, firewall rules, backend service and forwarding rule.
// It returns a LoadBalancerStatus with the updated ForwardingRule IP address.
// This function does not link instances to Backend Service.
func (l4netlb *L4NetLB) EnsureFrontend(nodeNames []string) {
	l4netlb.clearSyncResult()

	// If service already has an IP assigned, treat it as an update instead of a new Loadbalancer.
	if len(l4netlb.Service.Status.LoadBalancer.Ingress) > 0 {
		l4netlb.SyncResult.SyncType = SyncTypeUpdate
	}

	healthCheckLink := l4netlb.syncHealthCheck(nodeNames)
	if l4netlb.SyncResult.Error != nil {
		return
	}

	l4netlb.syncFirewall(nodeNames)
	if l4netlb.SyncResult.Error != nil {
		return
	}

	backendServiceLink := l4netlb.syncBackendService(healthCheckLink)
	if l4netlb.SyncResult.Error != nil {
		return
	}

	l4netlb.syncForwardingRule(backendServiceLink)
}

// EnsureLoadBalancerDeleted performs a cleanup of all GCE resources for the given loadbalancer service.
// It is health check, firewall rules and backend service
func (l4netlb *L4NetLB) EnsureLoadBalancerDeleted() {
	l4netlb.clearSyncResult()
	l4netlb.SyncResult.SyncType = SyncTypeDelete

	// If any resource deletion fails, continue cleanup.

	l4netlb.syncDeleteForwardingRule()

	l4netlb.syncDeleteAddress()

	l4netlb.syncDeleteFirewall()

	l4netlb.syncDeleteBackendService()

	l4netlb.syncDeleteHealthChecks()
}

func (l4netlb *L4NetLB) syncDeleteForwardingRule() {
	frName := l4netlb.GetFRName()
	key, err := l4netlb.createKey(frName)
	if err != nil {
		klog.Errorf("Failed to create key for forwarding rule resources with name %s for service %s - %v", frName, l4netlb.NamespacedName.String(), err)
		l4netlb.SyncResult.Error = err
		return
	}

	if err = utils.IgnoreHTTPNotFound(composite.DeleteForwardingRule(l4netlb.cloud, key, meta.VersionGA)); err != nil {
		klog.Errorf("Failed to delete forwarding rule %s for service %s - %v", frName, l4netlb.NamespacedName.String(), err)
		l4netlb.SyncResult.Error = err
		l4netlb.SyncResult.GCEResourceInError = annotations.ForwardingRuleResource
		return
	}
}

func (l4netlb *L4NetLB) syncDeleteAddress() {
	addressName := l4netlb.ServicePort.BackendName()

	err := ensureAddressDeleted(l4netlb.cloud, addressName, l4netlb.cloud.Region())
	if err != nil {
		klog.Errorf("Failed to delete address for service %s - %v", l4netlb.NamespacedName.String(), err)
		l4netlb.SyncResult.Error = err
		l4netlb.SyncResult.GCEResourceInError = annotations.AddressResource
	}
}

func (l4netlb *L4NetLB) syncDeleteFirewall() {
	firewallName := l4netlb.ServicePort.BackendName()

	err := l4netlb.deleteFirewall(firewallName)
	if err != nil {
		klog.Errorf("Failed to delete firewall rule %s for service %s - %v", firewallName, l4netlb.NamespacedName.String(), err)
		l4netlb.SyncResult.GCEResourceInError = annotations.FirewallRuleResource
		l4netlb.SyncResult.Error = err
	}
}

func (l4netlb *L4NetLB) syncDeleteBackendService() {
	backendServiceName := l4netlb.ServicePort.BackendName()

	err := utils.IgnoreHTTPNotFound(l4netlb.backendPool.Delete(backendServiceName, meta.VersionGA, meta.Regional))
	if err != nil {
		klog.Errorf("Failed to delete backends for L4 External LoadBalancer service %s - %v", l4netlb.NamespacedName.String(), err)
		l4netlb.SyncResult.GCEResourceInError = annotations.BackendServiceResource
		l4netlb.SyncResult.Error = err
	}
}

func (l4netlb *L4NetLB) syncDeleteHealthChecks() {
	// We don't delete health check during service update so
	// it is possible that there might be some health check leak
	// when externalTrafficPolicy is changed from Local to Cluster and new a health check was created.
	// When service is deleted we need to check both health checks shared and non-shared
	// and delete them if needed.
	for _, isShared := range []bool{true, false} {
		resourceInError, err := l4netlb.l4HealthChecks.DeleteHealthCheck(l4netlb.Service, l4netlb.namer, isShared, meta.Regional, utils.XLB)
		if err != nil {
			l4netlb.SyncResult.GCEResourceInError = resourceInError
			l4netlb.SyncResult.Error = err
			// continue with deletion of the non-shared Healthcheck regardless of the error, both healthchecks may need to be deleted,
		}
	}
}

func (l4netlb *L4NetLB) deleteFirewall(name string) error {
	err := firewalls.EnsureL4FirewallRuleDeleted(l4netlb.cloud, name)
	if err != nil {
		if fwErr, ok := err.(*firewalls.FirewallXPNError); ok {
			l4netlb.recorder.Eventf(l4netlb.Service, corev1.EventTypeNormal, "XPN", fwErr.Message)
			return nil
		}
		return err
	}
	return nil
}

// GetFRName returns the name of the forwarding rule for the given L4 External LoadBalancer service.
// This name should align with legacy forwarding rule name because we use forwarding rule to determine
// which controller should process the service Ingress-GCE or k/k service controller.
func (l4netlb *L4NetLB) GetFRName() string {
	return utils.LegacyForwardingRuleName(l4netlb.Service)
}

func (l4netlb *L4NetLB) syncHealthCheck(nodeNames []string) string {
	sharedHC := !helpers.RequestsOnlyLocalTraffic(l4netlb.Service)
	hcResult := l4netlb.l4HealthChecks.EnsureL4HealthCheck(l4netlb.Service, l4netlb.namer, sharedHC, l4netlb.scope, utils.XLB, nodeNames)

	if hcResult.Err != nil {
		l4netlb.SyncResult.GCEResourceInError = hcResult.GceResourceInError
		l4netlb.SyncResult.Error = fmt.Errorf("Failed to ensure health check %s - %w", hcResult.HCName, hcResult.Err)
		return ""
	}
	l4netlb.SyncResult.Annotations[annotations.HealthcheckKey] = hcResult.HCName
	l4netlb.SyncResult.Annotations[annotations.FirewallRuleForHealthcheckKey] = hcResult.HCFirewallRuleName

	return hcResult.HCLink
}

func (l4netlb *L4NetLB) syncFirewall(nodeNames []string) {
	firewallName := l4netlb.ServicePort.BackendName()

	portRanges := utils.GetPortRanges(l4netlb.Service.Spec.Ports)
	sourceRanges, err := helpers.GetLoadBalancerSourceRanges(l4netlb.Service)
	if err != nil {
		l4netlb.SyncResult.Error = err
		return
	}

	// Add firewall rule for L4 External LoadBalancer traffic to nodes
	nodesFWRParams := firewalls.FirewallParams{
		PortRanges:   portRanges,
		SourceRanges: sourceRanges.StringSlice(),
		Protocol:     string(l4netlb.protocol),
		Name:         firewallName,
		IP:           l4netlb.Service.Spec.LoadBalancerIP,
		NodeNames:    nodeNames,
	}
	err = firewalls.EnsureL4LBFirewallForNodes(l4netlb.Service, &nodesFWRParams, l4netlb.cloud, l4netlb.recorder)
	if err != nil {
		l4netlb.SyncResult.Error = err
		l4netlb.SyncResult.GCEResourceInError = annotations.FirewallRuleResource
		return
	}
	l4netlb.SyncResult.Annotations[annotations.FirewallRuleKey] = firewallName
}

func (l4netlb *L4NetLB) syncBackendService(healthCheckLink string) string {
	backendServiceName := l4netlb.ServicePort.BackendName()

	bs, err := l4netlb.backendPool.EnsureL4BackendService(backendServiceName, healthCheckLink, string(l4netlb.protocol), string(l4netlb.Service.Spec.SessionAffinity), string(cloud.SchemeExternal), l4netlb.NamespacedName, meta.VersionGA)
	if err != nil {
		l4netlb.SyncResult.GCEResourceInError = annotations.BackendServiceResource
		l4netlb.SyncResult.Error = fmt.Errorf("Failed to ensure backend service %s - %w", backendServiceName, err)
		return ""
	}
	l4netlb.SyncResult.Annotations[annotations.BackendServiceKey] = backendServiceName
	return bs.SelfLink
}

func (l4netlb *L4NetLB) syncForwardingRule(backendServiceLink string) {
	fr, ipAddrType, err := l4netlb.ensureExternalForwardingRule(backendServiceLink)
	if err != nil {
		// User can misconfigure the forwarding rule if Network Tier will not match service level Network Tier.
		l4netlb.SyncResult.MetricsState.IsUserError = utils.IsUserError(err)
		l4netlb.SyncResult.GCEResourceInError = annotations.ForwardingRuleResource
		l4netlb.SyncResult.Error = fmt.Errorf("Failed to ensure forwarding rule - %w", err)
		return
	}
	if fr.IPProtocol == string(corev1.ProtocolTCP) {
		l4netlb.SyncResult.Annotations[annotations.TCPForwardingRuleKey] = fr.Name
	} else {
		l4netlb.SyncResult.Annotations[annotations.UDPForwardingRuleKey] = fr.Name
	}
	l4netlb.SyncResult.Status = &corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: fr.IPAddress}}}
	l4netlb.SyncResult.MetricsState.IsPremiumTier = fr.NetworkTier == cloud.NetworkTierPremium.ToGCEValue()
	l4netlb.SyncResult.MetricsState.IsManagedIP = ipAddrType == IPAddrManaged
}
