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
	enableDualStack bool
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
	Service          *corev1.Service
	Cloud            *gce.Cloud
	Namer            namer.L4ResourcesNamer
	Recorder         record.EventRecorder
	DualStackEnabled bool
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
		enableDualStack: params.DualStackEnabled,
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

	hcLink := l4netlb.provideHealthChecks(nodeNames, result)
	if result.Error != nil {
		return result
	}

	bsLink := l4netlb.provideBackendService(result, hcLink)
	if result.Error != nil {
		return result
	}

	if l4netlb.enableDualStack {
		l4netlb.ensureDualStackResources(result, nodeNames, bsLink)
	} else {
		l4netlb.ensureIPv4Resources(result, nodeNames, bsLink)
	}

	return result
}

func (l4netlb *L4NetLB) provideHealthChecks(nodeNames []string, result *L4NetLBSyncResult) string {
	if l4netlb.enableDualStack {
		return l4netlb.provideDualStackHealthChecks(nodeNames, result)
	}
	return l4netlb.provideIPv4HealthChecks(nodeNames, result)
}

func (l4netlb *L4NetLB) provideDualStackHealthChecks(nodeNames []string, result *L4NetLBSyncResult) string {
	sharedHC := !helpers.RequestsOnlyLocalTraffic(l4netlb.Service)
	hcResult := l4netlb.healthChecks.EnsureHealthCheckWithDualStackFirewalls(l4netlb.Service, l4netlb.namer, sharedHC, l4netlb.scope, utils.XLB, nodeNames, utils.NeedsIPv4(l4netlb.Service), utils.NeedsIPv6(l4netlb.Service))
	if hcResult.Err != nil {
		result.GCEResourceInError = hcResult.GceResourceInError
		result.Error = hcResult.Err
		return ""
	}

	if hcResult.HCFirewallRuleName != "" {
		result.Annotations[annotations.FirewallRuleForHealthcheckKey] = hcResult.HCFirewallRuleName
	}
	if hcResult.HCFirewallRuleIPv6Name != "" {
		result.Annotations[annotations.FirewallRuleForHealthcheckIPv6Key] = hcResult.HCFirewallRuleIPv6Name
	}
	result.Annotations[annotations.HealthcheckKey] = hcResult.HCName
	return hcResult.HCLink
}

func (l4netlb *L4NetLB) provideIPv4HealthChecks(nodeNames []string, result *L4NetLBSyncResult) string {
	sharedHC := !helpers.RequestsOnlyLocalTraffic(l4netlb.Service)
	hcResult := l4netlb.healthChecks.EnsureHealthCheckWithFirewall(l4netlb.Service, l4netlb.namer, sharedHC, l4netlb.scope, utils.XLB, nodeNames)
	if hcResult.Err != nil {
		result.GCEResourceInError = hcResult.GceResourceInError
		result.Error = hcResult.Err
		return ""
	}
	result.Annotations[annotations.HealthcheckKey] = hcResult.HCName
	result.Annotations[annotations.FirewallRuleForHealthcheckKey] = hcResult.HCFirewallRuleName
	return hcResult.HCLink
}

func (l4netlb *L4NetLB) provideBackendService(syncResult *L4NetLBSyncResult, hcLink string) string {
	bsName := l4netlb.namer.L4Backend(l4netlb.Service.Namespace, l4netlb.Service.Name)
	servicePorts := l4netlb.Service.Spec.Ports
	protocol := utils.GetProtocol(servicePorts)
	bs, err := l4netlb.backendPool.EnsureL4BackendService(bsName, hcLink, string(protocol), string(l4netlb.Service.Spec.SessionAffinity), string(cloud.SchemeExternal), l4netlb.NamespacedName)
	if err != nil {
		syncResult.GCEResourceInError = annotations.BackendServiceResource
		syncResult.Error = fmt.Errorf("Failed to ensure backend service %s - %w", bsName, err)
		return ""
	}
	syncResult.Annotations[annotations.BackendServiceKey] = bsName
	return bs.SelfLink
}

func (l4netlb *L4NetLB) ensureDualStackResources(result *L4NetLBSyncResult, nodeNames []string, bsLink string) {
	if utils.NeedsIPv4(l4netlb.Service) {
		l4netlb.ensureIPv4Resources(result, nodeNames, bsLink)
	} else {
		l4netlb.deleteIPv4ResourcesOnSync(result)
	}
	if utils.NeedsIPv6(l4netlb.Service) {
		l4netlb.ensureIPv6Resources(result, nodeNames, bsLink)
	} else {
		l4netlb.deleteIPv6ResourcesOnSync(result)
	}
}

// ensureIPv4Resources creates resources specific to IPv4 L4 Load Balancers:
// - IPv4 Forwarding Rule
// - IPv4 Firewall
func (l4netlb *L4NetLB) ensureIPv4Resources(result *L4NetLBSyncResult, nodeNames []string, bsLink string) {
	fr, ipAddrType, err := l4netlb.ensureIPv4ForwardingRule(bsLink)
	if err != nil {
		// User can misconfigure the forwarding rule if Network Tier will not match service level Network Tier.
		result.MetricsState.IsUserError = utils.IsUserError(err)
		result.GCEResourceInError = annotations.ForwardingRuleResource
		result.Error = fmt.Errorf("failed to ensure forwarding rule - %w", err)
		return
	}
	if fr.IPProtocol == string(corev1.ProtocolTCP) {
		result.Annotations[annotations.TCPForwardingRuleKey] = fr.Name
	} else {
		result.Annotations[annotations.UDPForwardingRuleKey] = fr.Name
	}
	result.MetricsState.IsManagedIP = ipAddrType == IPAddrManaged
	result.MetricsState.IsPremiumTier = fr.NetworkTier == cloud.NetworkTierPremium.ToGCEValue()

	l4netlb.ensureIPv4NodesFirewall(nodeNames, fr.IPAddress, result)
	if result.Error != nil {
		klog.Errorf("ensureIPv4Resources: Failed to ensure nodes firewall for L4 NetLB Service %s/%s, error: %v", l4netlb.Service.Namespace, l4netlb.Service.Name, err)
		return
	}

	result.Status = utils.AddIPToLBStatus(result.Status, fr.IPAddress)
}

func (l4netlb *L4NetLB) ensureIPv4NodesFirewall(nodeNames []string, ipAddress string, result *L4NetLBSyncResult) {
	start := time.Now()

	firewallName := l4netlb.namer.L4Firewall(l4netlb.Service.Namespace, l4netlb.Service.Name)
	servicePorts := l4netlb.Service.Spec.Ports
	portRanges := utils.GetServicePortRanges(servicePorts)
	protocol := utils.GetProtocol(servicePorts)

	klog.V(2).Infof("Ensuring nodes firewall %s for L4 NetLB Service %s/%s, ipAddress: %s, protocol: %s, len(nodeNames): %v, portRanges: %v", firewallName, l4netlb.Service.Namespace, l4netlb.Service.Name, ipAddress, protocol, len(nodeNames), portRanges)
	defer func() {
		klog.V(2).Infof("Finished ensuring nodes firewall %s for L4 NetLB Service %s/%s, time taken: %v", firewallName, l4netlb.Service.Namespace, l4netlb.Service.Name, time.Since(start))
	}()

	sourceRanges, err := utils.IPv4ServiceSourceRanges(l4netlb.Service)
	if err != nil {
		result.Error = err
		return
	}

	// Add firewall rule for L4 External LoadBalancer traffic to nodes
	nodesFWRParams := firewalls.FirewallParams{
		PortRanges:        portRanges,
		SourceRanges:      sourceRanges,
		DestinationRanges: []string{ipAddress},
		Protocol:          string(protocol),
		Name:              firewallName,
		IP:                l4netlb.Service.Spec.LoadBalancerIP,
		NodeNames:         nodeNames,
	}
	result.Error = firewalls.EnsureL4LBFirewallForNodes(l4netlb.Service, &nodesFWRParams, l4netlb.cloud, l4netlb.recorder)
	if result.Error != nil {
		result.GCEResourceInError = annotations.FirewallRuleResource
		result.Error = err
		return
	}
	result.Annotations[annotations.FirewallRuleKey] = firewallName
}

// EnsureLoadBalancerDeleted performs a cleanup of all GCE resources for the given loadbalancer service.
// It is health check, firewall rules and backend service
func (l4netlb *L4NetLB) EnsureLoadBalancerDeleted(svc *corev1.Service) *L4NetLBSyncResult {
	result := NewL4SyncResult(SyncTypeDelete)
	l4netlb.Service = svc

	l4netlb.deleteIPv4ResourcesOnDelete(result)
	if l4netlb.enableDualStack {
		l4netlb.deleteIPv6ResourcesOnDelete(result)
	}

	l4netlb.deleteBackendService(result)
	l4netlb.deleteHealthChecksWithFirewall(result)

	return result
}

// deleteIPv4ResourcesOnSync deletes resources specific to IPv4,
// only if corresponding resource annotation exist on Service object.
// This function is called only on Service update or periodic sync.
// Checking for annotation saves us from emitting too much error logs "Resource not found".
// If annotation was deleted, but resource still exists, it will be left till the Service deletion,
// where we delete all resources, no matter if they exist in annotations.
func (l4netlb *L4NetLB) deleteIPv4ResourcesOnSync(result *L4NetLBSyncResult) {
	klog.Infof("Deleting IPv4 resources for L4 NetLB Service %s/%s on sync, with checking for existence in annotation", l4netlb.Service.Namespace, l4netlb.Service.Name)
	l4netlb.deleteIPv4ResourcesAnnotationBased(result, false)
}

// deleteIPv4ResourcesOnDelete deletes all resources specific to IPv4.
// This function is called only on Service deletion.
// During sync, we delete resources only that exist in annotations,
// so they could be leaked, if annotation was deleted.
// That's why on service deletion we delete all IPv4 resources, ignoring their existence in annotations
func (l4netlb *L4NetLB) deleteIPv4ResourcesOnDelete(result *L4NetLBSyncResult) {
	klog.Infof("Deleting IPv4 resources for L4 NetLB Service %s/%s on delete, without checking for existence in annotation", l4netlb.Service.Namespace, l4netlb.Service.Name)
	l4netlb.deleteIPv4ResourcesAnnotationBased(result, true)
}

// deleteIPv4ResourcesAnnotationBased deletes IPv4 only resources with checking,
// if resource exists in Service annotation, if shouldIgnoreAnnotations not set to true
// IPv4 Specific resources:
// - IPv4 Forwarding Rule
// - IPv4 Address
// - IPv4 Firewall
// This function does not delete Backend Service and Health Check, because they are shared between IPv4 and IPv6.
// IPv4 Firewall Rule for Health Check also will not be deleted here, and will be left till the Service Deletion.
func (l4netlb *L4NetLB) deleteIPv4ResourcesAnnotationBased(result *L4NetLBSyncResult, shouldIgnoreAnnotations bool) {
	if shouldIgnoreAnnotations || l4netlb.hasAnnotation(annotations.TCPForwardingRuleKey) || l4netlb.hasAnnotation(annotations.UDPForwardingRuleKey) {
		err := l4netlb.deleteIPv4ForwardingRule()
		if err != nil {
			klog.Errorf("Failed to delete forwarding rule for NetLB RBS service %s, error: %v", l4netlb.NamespacedName.String(), err)
			result.Error = err
			result.GCEResourceInError = annotations.ForwardingRuleResource
		}
	}

	// Deleting non-existent address do not print error audit logs, and we don't store address in annotations
	// that's why we can delete it without checking annotation
	err := l4netlb.deleteIPv4Address()
	if err != nil {
		klog.Errorf("Failed to delete address for NetLB RBS service %s, error: %v", l4netlb.NamespacedName.String(), err)
		result.Error = err
		result.GCEResourceInError = annotations.AddressResource
	}

	// delete firewall rule allowing load balancer source ranges
	if shouldIgnoreAnnotations || l4netlb.hasAnnotation(annotations.FirewallRuleKey) {
		err = l4netlb.deleteIPv4NodesFirewall()
		if err != nil {
			klog.Errorf("Failed to delete firewall rule for NetLB RBS service %s, error: %v", l4netlb.NamespacedName.String(), err)
			result.GCEResourceInError = annotations.FirewallRuleResource
			result.Error = err
		}
	}
}

func (l4netlb *L4NetLB) deleteIPv4ForwardingRule() error {
	start := time.Now()

	frName := l4netlb.frName()

	klog.V(2).Infof("Deleting IPv4 external forwarding rule %s for L4 NetLB Service %s/%s", frName, l4netlb.Service.Namespace, l4netlb.Service.Name)
	defer func() {
		klog.V(2).Infof("Finished deleting IPv4 external forwarding rule %s for L4 NetLB Service %s/%s, time taken: %v", frName, l4netlb.Service.Namespace, l4netlb.Service.Name, time.Since(start))
	}()

	return l4netlb.forwardingRules.Delete(frName)
}

func (l4netlb *L4NetLB) deleteIPv4Address() error {
	addressName := l4netlb.frName()

	start := time.Now()
	klog.V(2).Infof("Deleting IPv4 external static address %s for L4 NetLB service %s/%s", addressName, l4netlb.Service.Namespace, l4netlb.Service.Name)
	defer func() {
		klog.V(2).Infof("Finished deleting IPv4 external static address %s for L4 NetLB service %s/%s, time taken: %v", addressName, l4netlb.Service.Namespace, l4netlb.Service.Name, time.Since(start))
	}()

	return ensureAddressDeleted(l4netlb.cloud, addressName, l4netlb.cloud.Region())
}

func (l4netlb *L4NetLB) deleteIPv4NodesFirewall() error {
	start := time.Now()

	firewallName := l4netlb.namer.L4Firewall(l4netlb.Service.Namespace, l4netlb.Service.Name)

	klog.V(2).Infof("Deleting IPv4 nodes firewall %s for L4 NetLB Service %s/%s", firewallName, l4netlb.Service.Namespace, l4netlb.Service.Name)
	defer func() {
		klog.V(2).Infof("Finished deleting IPv4 nodes firewall %s for L4 NetLB Service %s/%s, time taken: %v", firewallName, l4netlb.Service.Namespace, l4netlb.Service.Name, time.Since(start))
	}()

	return l4netlb.deleteFirewall(firewallName)
}

func (l4netlb *L4NetLB) deleteFirewall(firewallName string) error {
	err := firewalls.EnsureL4FirewallRuleDeleted(l4netlb.cloud, firewallName)
	if err != nil {
		if fwErr, ok := err.(*firewalls.FirewallXPNError); ok {
			l4netlb.recorder.Eventf(l4netlb.Service, corev1.EventTypeNormal, "XPN", fwErr.Message)
			return nil
		}
		return err
	}
	return nil
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
		if l4netlb.enableDualStack {
			resourceInError, err := l4netlb.healthChecks.DeleteHealthCheckWithDualStackFirewalls(l4netlb.Service, l4netlb.namer, isShared, meta.Regional, utils.XLB)
			if err != nil {
				result.GCEResourceInError = resourceInError
				result.Error = err
				// continue with deletion of the non-shared Healthcheck regardless of the error, both healthchecks may need to be deleted,
			}
		} else {
			resourceInError, err := l4netlb.healthChecks.DeleteHealthCheckWithFirewall(l4netlb.Service, l4netlb.namer, isShared, meta.Regional, utils.XLB)
			if err != nil {
				result.GCEResourceInError = resourceInError
				result.Error = err
				// continue with deletion of the non-shared Healthcheck regardless of the error, both healthchecks may need to be deleted,
			}
		}
	}
}

func (l4netlb *L4NetLB) hasAnnotation(annotationKey string) bool {
	if _, ok := l4netlb.Service.Annotations[annotationKey]; ok {
		return true
	}
	return false
}

// frName returns the name of the forwarding rule for the given L4 External LoadBalancer service.
// This name should align with legacy forwarding rule name because we use forwarding rule to determine
// which controller should process the service Ingress-GCE or k/k service controller.
func (l4netlb *L4NetLB) frName() string {
	return utils.LegacyForwardingRuleName(l4netlb.Service)
}
