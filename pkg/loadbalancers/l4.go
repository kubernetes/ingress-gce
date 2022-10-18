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

package loadbalancers

import (
	"fmt"
	"strings"
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

// Many of the functions in this file are re-implemented from gce_loadbalancer_internal.go
// L4 handles the resource creation/deletion/update for a given L4 ILB service.
type L4 struct {
	cloud       *gce.Cloud
	backendPool *backends.Backends
	scope       meta.KeyType
	namer       namer.L4ResourcesNamer
	// recorder is used to generate k8s Events.
	recorder        record.EventRecorder
	Service         *corev1.Service
	ServicePort     utils.ServicePort
	NamespacedName  types.NamespacedName
	forwardingRules ForwardingRulesProvider
	healthChecks    healthchecksl4.L4HealthChecks
}

// L4ILBSyncResult contains information about the outcome of an L4 ILB sync. It stores the list of resource name annotations,
// sync error, the GCE resource that hit the error along with the error type, metrics and more fields.
type L4ILBSyncResult struct {
	Annotations        map[string]string
	Error              error
	GCEResourceInError string
	Status             *corev1.LoadBalancerStatus
	MetricsState       metrics.L4ILBServiceState
	SyncType           string
	StartTime          time.Time
}

type L4ILBParams struct {
	Service  *corev1.Service
	Cloud    *gce.Cloud
	Namer    namer.L4ResourcesNamer
	Recorder record.EventRecorder
}

// NewL4Handler creates a new L4Handler for the given L4 service.
func NewL4Handler(params *L4ILBParams) *L4 {
	var scope meta.KeyType = meta.Regional
	l4 := &L4{
		cloud:           params.Cloud,
		scope:           scope,
		namer:           params.Namer,
		recorder:        params.Recorder,
		Service:         params.Service,
		healthChecks:    healthchecksl4.NewL4HealthChecks(params.Cloud, params.Recorder),
		forwardingRules: forwardingrules.New(params.Cloud, meta.VersionGA, scope),
	}
	l4.NamespacedName = types.NamespacedName{Name: params.Service.Name, Namespace: params.Service.Namespace}
	l4.backendPool = backends.NewPool(l4.cloud, l4.namer)
	l4.ServicePort = utils.ServicePort{ID: utils.ServicePortID{Service: l4.NamespacedName}, BackendNamer: l4.namer,
		VMIPNEGEnabled: true}
	return l4
}

// CreateKey generates a meta.Key for a given GCE resource name.
func (l4 *L4) CreateKey(name string) (*meta.Key, error) {
	return composite.CreateKey(l4.cloud, name, l4.scope)
}

// getILBOptions fetches the optional features requested on the given ILB service.
func (l4 *L4) getILBOptions() gce.ILBOptions {
	if l4.cloud.IsLegacyNetwork() {
		l4.recorder.Event(l4.Service, corev1.EventTypeWarning, "ILBOptionsIgnored", "Internal LoadBalancer options are not supported with Legacy Networks.")
		return gce.ILBOptions{}
	}

	return gce.ILBOptions{AllowGlobalAccess: gce.GetLoadBalancerAnnotationAllowGlobalAccess(l4.Service),
		SubnetName: gce.GetLoadBalancerAnnotationSubnet(l4.Service)}
}

// EnsureInternalLoadBalancerDeleted performs a cleanup of all GCE resources for the given loadbalancer service.
func (l4 *L4) EnsureInternalLoadBalancerDeleted(svc *corev1.Service) *L4ILBSyncResult {
	klog.V(2).Infof("EnsureInternalLoadBalancerDeleted(%s): attempting delete of load balancer resources", l4.NamespacedName.String())
	result := &L4ILBSyncResult{SyncType: SyncTypeDelete, StartTime: time.Now()}
	// All resources use the L4Backend Name, except forwarding rule.
	name := l4.namer.L4Backend(svc.Namespace, svc.Name)
	frName := l4.GetFRName()
	// If any resource deletion fails, log the error and continue cleanup.
	err := l4.forwardingRules.Delete(frName)
	if err != nil {
		klog.Errorf("Failed to delete forwarding rule for internal loadbalancer service %s, err %v", l4.NamespacedName.String(), err)
		result.Error = err
		result.GCEResourceInError = annotations.ForwardingRuleResource
	}
	if err = ensureAddressDeleted(l4.cloud, frName, l4.cloud.Region()); err != nil {
		klog.Errorf("Failed to delete address for internal loadbalancer service %s, err %v", l4.NamespacedName.String(), err)
		result.Error = err
		result.GCEResourceInError = annotations.AddressResource
	}

	// delete firewall rule allowing load balancer source ranges
	firewallName := l4.namer.L4Firewall(l4.Service.Namespace, l4.Service.Name)
	err = l4.deleteFirewall(firewallName)
	if err != nil {
		klog.Errorf("Failed to delete firewall rule %s for internal loadbalancer service %s, err %v", firewallName, l4.NamespacedName.String(), err)
		result.GCEResourceInError = annotations.FirewallRuleResource
		result.Error = err
	}
	// Delete backend service
	err = utils.IgnoreHTTPNotFound(l4.backendPool.Delete(name, meta.VersionGA, meta.Regional))
	if err != nil {
		klog.Errorf("Failed to delete backends for internal loadbalancer service %s, err  %v", l4.NamespacedName.String(), err)
		result.GCEResourceInError = annotations.BackendServiceResource
		result.Error = err
	}

	// Delete healthcheck
	// We don't delete health check during service update so
	// it is possible that there might be some health check leak
	// when externalTrafficPolicy is changed from Local to Cluster and a new health check was created.
	// When service is deleted we need to check both health checks shared and non-shared
	// and delete them if needed.
	for _, isShared := range []bool{true, false} {
		resourceInError, err := l4.healthChecks.DeleteHealthCheckWithFirewall(svc, l4.namer, isShared, meta.Global, utils.ILB)
		if err != nil {
			result.GCEResourceInError = resourceInError
			result.Error = err
		}
	}
	return result
}

func (l4 *L4) deleteFirewall(name string) error {
	err := firewalls.EnsureL4FirewallRuleDeleted(l4.cloud, name)
	if err != nil {
		if fwErr, ok := err.(*firewalls.FirewallXPNError); ok {
			l4.recorder.Eventf(l4.Service, corev1.EventTypeNormal, "XPN", fwErr.Message)
			return nil
		}
		return err
	}
	return nil
}

// GetFRName returns the name of the forwarding rule for the given ILB service.
// This appends the protocol to the forwarding rule name, which will help supporting multiple protocols in the same ILB
// service.
func (l4 *L4) GetFRName() string {
	protocol := utils.GetProtocol(l4.Service.Spec.Ports)
	return l4.getFRNameWithProtocol(string(protocol))
}

func (l4 *L4) getFRNameWithProtocol(protocol string) string {
	return l4.namer.L4ForwardingRule(l4.Service.Namespace, l4.Service.Name, strings.ToLower(protocol))
}

// EnsureInternalLoadBalancer ensures that all GCE resources for the given loadbalancer service have
// been created. It returns a LoadBalancerStatus with the updated ForwardingRule IP address.
func (l4 *L4) EnsureInternalLoadBalancer(nodeNames []string, svc *corev1.Service) *L4ILBSyncResult {
	result := &L4ILBSyncResult{
		Annotations: make(map[string]string),
		StartTime:   time.Now(),
		SyncType:    SyncTypeCreate,
	}

	// If service already has an IP assigned, treat it as an update instead of a new Loadbalancer.
	// This will also cover cases where an external LB is updated to an ILB, which is technically a create for ILB.
	// But this is still the easiest way to identify create vs update in the common case.
	if len(svc.Status.LoadBalancer.Ingress) > 0 {
		result.SyncType = SyncTypeUpdate
	}

	l4.Service = svc

	// create healthcheck
	sharedHC := !helpers.RequestsOnlyLocalTraffic(l4.Service)
	hcResult := l4.healthChecks.EnsureHealthCheckWithFirewall(l4.Service, l4.namer, sharedHC, meta.Global, utils.ILB, nodeNames)
	if hcResult.Err != nil {
		result.GCEResourceInError = hcResult.GceResourceInError
		result.Error = hcResult.Err
		return result
	}
	result.Annotations[annotations.HealthcheckKey] = hcResult.HCName

	// Reserve existing IP address before making any changes
	existingFR, err := l4.getOldForwardingRule()
	options := l4.getILBOptions()
	subnetworkURL, err := l4.getServiceSubnetworkURL(options)
	ipToUse := l4lbIPToUse(l4.Service, existingFR, subnetworkURL)
	expectedFRName := l4.GetFRName()
	if !l4.cloud.IsLegacyNetwork() {
		nm := types.NamespacedName{Namespace: l4.Service.Namespace, Name: l4.Service.Name}.String()
		// ILB can be created only in Premium Tier
		addrMgr := newAddressManager(l4.cloud, nm, l4.cloud.Region(), subnetworkURL, expectedFRName, ipToUse, cloud.SchemeInternal, cloud.NetworkTierPremium)
		ipToUse, _, err = addrMgr.HoldAddress()
		if err != nil {
			result.Error = fmt.Errorf("EnsureInternalLoadBalancer error: addrMgr.HoldAddress() returned error %w", err)
			return result
		}
		klog.V(2).Infof("EnsureInternalLoadBalancer(%v): reserved IP %q", nm, ipToUse)
		defer func() {
			// Release the address that was reserved, in all cases. If the forwarding rule was successfully created,
			// the ephemeral IP is not needed anymore. If it was not created, the address should be released to prevent leaks.
			if err := addrMgr.ReleaseAddress(); err != nil {
				klog.Errorf("EnsureInternalLoadBalancer: failed to release address reservation, possibly causing an orphan: %v", err)
			}
		}()
	}

	// if Service protocol changed, we must delete forwarding rule before changing backend service,
	// otherwise, on updating backend service, google cloud api will return error
	servicePorts := l4.Service.Spec.Ports
	protocol := utils.GetProtocol(servicePorts)
	if existingFR != nil && existingFR.IPProtocol != string(protocol) {
		err = l4.forwardingRules.Delete(existingFR.Name)
		if err != nil {
			klog.Errorf("Failed to delete forwarding rule %s, err %v", existingFR.Name, err)
		}
	}

	// ensure backend service
	bsName := l4.namer.L4Backend(l4.Service.Namespace, l4.Service.Name)
	bs, err := l4.backendPool.EnsureL4BackendService(bsName, hcResult.HCLink, string(protocol), string(l4.Service.Spec.SessionAffinity),
		string(cloud.SchemeInternal), l4.NamespacedName, meta.VersionGA)
	if err != nil {
		result.GCEResourceInError = annotations.BackendServiceResource
		result.Error = err
		return result
	}
	result.Annotations[annotations.BackendServiceKey] = bsName

	// create fr rule
	fr, err := l4.ensureForwardingRule(bs.SelfLink, options, existingFR, subnetworkURL, ipToUse)
	if err != nil {
		klog.Errorf("EnsureInternalLoadBalancer: Failed to create forwarding rule - %v", err)
		result.GCEResourceInError = annotations.ForwardingRuleResource
		result.Error = err
		return result
	}
	if fr.IPProtocol == string(corev1.ProtocolTCP) {
		result.Annotations[annotations.TCPForwardingRuleKey] = fr.Name
	} else {
		result.Annotations[annotations.UDPForwardingRuleKey] = fr.Name
	}

	// ensure firewalls
	sourceRanges, err := helpers.GetLoadBalancerSourceRanges(l4.Service)
	if err != nil {
		result.Error = err
		return result
	}
	// Add firewall rule for ILB traffic to nodes
	firewallName := l4.namer.L4Firewall(l4.Service.Namespace, l4.Service.Name)
	portRanges := utils.GetServicePortRanges(servicePorts)
	nodesFWRParams := firewalls.FirewallParams{
		PortRanges:        portRanges,
		SourceRanges:      sourceRanges.StringSlice(),
		DestinationRanges: []string{fr.IPAddress},
		Protocol:          string(protocol),
		Name:              firewallName,
		NodeNames:         nodeNames,
		L4Type:            utils.ILB,
	}

	if err := firewalls.EnsureL4LBFirewallForNodes(l4.Service, &nodesFWRParams, l4.cloud, l4.recorder); err != nil {
		result.GCEResourceInError = annotations.FirewallRuleResource
		result.Error = err
		return result
	}
	result.Annotations[annotations.FirewallRuleKey] = firewallName
	result.Annotations[annotations.FirewallRuleForHealthcheckKey] = hcResult.HCFirewallRuleName

	result.MetricsState.InSuccess = true
	if options.AllowGlobalAccess {
		result.MetricsState.EnabledGlobalAccess = true
	}
	// SubnetName is overwritten to nil value if Alpha feature gate for custom subnet
	// is not enabled. So, a non empty subnet name at this point implies that the
	// feature is in use.
	if options.SubnetName != "" {
		result.MetricsState.EnabledCustomSubnet = true
	}
	result.Status = &corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: fr.IPAddress}}}
	return result
}

func (l4 *L4) getServiceSubnetworkURL(options gce.ILBOptions) (string, error) {
	// Custom subnet feature is always enabled when running L4 controller.
	// Changes to subnet annotation will be picked up and reflected in the forwarding rule.
	// Removing the annotation will set the forwarding rule to use the default subnet.
	if options.SubnetName != "" {
		return l4.getSubnetworkURLByName(options.SubnetName)
	}
	return l4.cloud.SubnetworkURL(), nil
}

func (l4 *L4) getSubnetworkURLByName(subnetName string) (string, error) {
	subnetKey, err := l4.CreateKey(subnetName)
	if err != nil {
		return "", err
	}
	return cloud.SelfLink(meta.VersionGA, l4.cloud.NetworkProjectID(), "subnetworks", subnetKey), nil
}

func (l4 *L4) getOldForwardingRule() (*composite.ForwardingRule, error) {
	bsName := l4.namer.L4Backend(l4.Service.Namespace, l4.Service.Name)
	// Check if protocol has changed for this service. In this case, forwarding rule has different protocol and name
	existingBS, err := l4.backendPool.Get(bsName, meta.VersionGA, l4.scope)
	err = utils.IgnoreHTTPNotFound(err)
	if err != nil {
		klog.Errorf("Failed to lookup existing backend service, ignoring err: %v", err)
	}

	servicePorts := l4.Service.Spec.Ports
	protocol := utils.GetProtocol(servicePorts)
	if existingBS != nil && existingBS.Protocol != string(protocol) {
		klog.Infof("Protocol changed from %q to %q for service %s", existingBS.Protocol, string(protocol), l4.NamespacedName)
		// Delete forwarding rule if it exists
		oldProtocolFRRName := l4.getFRNameWithProtocol(existingBS.Protocol)
		return l4.forwardingRules.Get(oldProtocolFRRName)
	}

	return l4.forwardingRules.Get(l4.GetFRName())
}
