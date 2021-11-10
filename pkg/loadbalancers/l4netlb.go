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
	"strconv"
	"strings"
	"sync"
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
	// TODO(52752) change namer for proper NetLB Namer
	namer namer.L4ResourcesNamer
	// recorder is used to generate k8s Events.
	recorder            record.EventRecorder
	Service             *corev1.Service
	ServicePort         utils.ServicePort
	NamespacedName      types.NamespacedName
	sharedResourcesLock *sync.Mutex
}

// SyncResultNetLB contains information about the outcome of an L4NetLB sync.
// It stores the list of resource name annotations,
// sync error, the GCE resource that hit the error along with the error type and more fields.
type SyncResultNetLB struct {
	Annotations        map[string]string
	Error              error
	GCEResourceInError string
	Status             *corev1.LoadBalancerStatus
	SyncType           string
	StartTime          time.Time
}

// NewL4NetLB creates a new Handler for the given L4NetLB service.
func NewL4NetLB(service *corev1.Service, cloud *gce.Cloud, scope meta.KeyType, namer namer.L4ResourcesNamer, recorder record.EventRecorder, lock *sync.Mutex) *L4NetLB {
	l4netlb := &L4NetLB{cloud: cloud,
		scope:               scope,
		namer:               namer,
		recorder:            recorder,
		Service:             service,
		sharedResourcesLock: lock,
		NamespacedName:      types.NamespacedName{Name: service.Name, Namespace: service.Namespace},
		backendPool:         backends.NewPool(cloud, namer),
	}
	portId := utils.ServicePortID{Service: l4netlb.NamespacedName}
	l4netlb.ServicePort = utils.ServicePort{
		ID:           portId,
		BackendNamer: l4netlb.namer,
		NodePort:     int64(service.Spec.Ports[0].NodePort),
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
func (l4netlb *L4NetLB) EnsureFrontend(nodeNames []string, svc *corev1.Service) *SyncResultNetLB {
	result := &SyncResultNetLB{
		Annotations: make(map[string]string),
		StartTime:   time.Now(),
		SyncType:    SyncTypeCreate}

	// If service already has an IP assigned, treat it as an update instead of a new Loadbalancer.
	if len(svc.Status.LoadBalancer.Ingress) > 0 {
		result.SyncType = SyncTypeUpdate
	}

	l4netlb.Service = svc
	// TODO(kl52752) Add unit tests for ExternalTrafficPolicy
	sharedHC := !helpers.RequestsOnlyLocalTraffic(svc)

	ensureHCFunc := func() (string, string, int32, string, error) {
		if sharedHC {
			// Take the lock when creating the shared healthcheck
			l4netlb.sharedResourcesLock.Lock()
			defer l4netlb.sharedResourcesLock.Unlock()
		}
		return healthchecks.EnsureL4HealthCheck(l4netlb.cloud, l4netlb.Service, l4netlb.namer, sharedHC, l4netlb.scope, utils.XLB)
	}
	hcLink, hcFwName, hcPort, _, err := ensureHCFunc()

	if err != nil {
		result.GCEResourceInError = annotations.HealthcheckResource
		result.Error = err
		return result
	}

	name := l4netlb.ServicePort.BackendName()
	protocol, res := l4netlb.createFirewalls(name, hcLink, hcFwName, hcPort, nodeNames, sharedHC)
	if res.Error != nil {
		return res
	}
	existingFR := &composite.ForwardingRule{}
	existingFR, result.Error = l4netlb.cleanupForwardingRuleIfProtocolChanged(name, protocol)
	if result.Error != nil {
		return result
	}

	bs, err := l4netlb.backendPool.EnsureL4BackendService(name, hcLink, protocol, string(l4netlb.Service.Spec.SessionAffinity), string(cloud.SchemeExternal), l4netlb.NamespacedName, meta.VersionGA)
	if err != nil {
		result.GCEResourceInError = annotations.BackendServiceResource
		result.Error = fmt.Errorf("Failed to ensure backend service - %v", err)
		return result
	}

	fr, err := l4netlb.ensureExternalForwardingRule(bs.SelfLink, existingFR)
	if err != nil {

		result.GCEResourceInError = annotations.ForwardingRuleResource
		result.Error = fmt.Errorf("Failed to ensure forwarding rule - %v", err)
		return result
	}

	result.Status = &corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: fr.IPAddress}}}
	return result
}

// EnsureLoadBalancerDeleted performs a cleanup of all GCE resources for the given loadbalancer service.
// It is health check, firewall rules and backend service
func (l4netlb *L4NetLB) EnsureLoadBalancerDeleted(svc *corev1.Service) *SyncResult {
	result := &SyncResult{SyncType: SyncTypeDelete, StartTime: time.Now()}

	frName := l4netlb.GetFRName()
	key, err := l4netlb.createKey(frName)
	if err != nil {
		klog.Errorf("Failed to create key for forwarding rule resources with name %s for service %s - %v", frName, l4netlb.NamespacedName.String(), err)
		result.Error = err
		return result
	}
	// If any resource deletion fails, log the error and continue cleanup.
	if err = utils.IgnoreHTTPNotFound(composite.DeleteForwardingRule(l4netlb.cloud, key, meta.VersionGA)); err != nil {
		klog.Errorf("Failed to delete forwarding rule %s for service %s - %v", frName, l4netlb.NamespacedName.String(), err)
		result.Error = err
		result.GCEResourceInError = annotations.ForwardingRuleResource
	}
	name := l4netlb.ServicePort.BackendName()
	if err = ensureAddressDeleted(l4netlb.cloud, name, l4netlb.cloud.Region()); err != nil {
		klog.Errorf("Failed to delete address for service %s - %v", l4netlb.NamespacedName.String(), err)
		result.Error = err
		result.GCEResourceInError = annotations.AddressResource
	}
	sharedHC := !helpers.RequestsOnlyLocalTraffic(svc)
	hcName, hcFwName := l4netlb.namer.L4HealthCheck(svc.Namespace, svc.Name, sharedHC)

	deleteFirewallFunc := func(name string) error {
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
	// delete firewall rule allowing load balancer source ranges
	err = deleteFirewallFunc(name)
	if err != nil {
		klog.Errorf("Failed to delete firewall rule %s for service %s - %v", name, l4netlb.NamespacedName.String(), err)
		result.GCEResourceInError = annotations.FirewallRuleResource
		result.Error = err
	}

	// delete firewall rule allowing healthcheck source ranges
	err = deleteFirewallFunc(hcFwName)
	if err != nil {
		klog.Errorf("Failed to delete firewall rule %s for service %s - %v", hcFwName, l4netlb.NamespacedName.String(), err)
		result.GCEResourceInError = annotations.FirewallForHealthcheckResource
		result.Error = err
	}
	// delete backend service
	err = utils.IgnoreHTTPNotFound(l4netlb.backendPool.Delete(name, meta.VersionGA, meta.Regional))
	if err != nil {
		klog.Errorf("Failed to delete backends for L4 External LoadBalancer service %s - %v", l4netlb.NamespacedName.String(), err)
		result.GCEResourceInError = annotations.BackendServiceResource
		result.Error = err
	}

	// Delete healthcheck
	if sharedHC {
		l4netlb.sharedResourcesLock.Lock()
		defer l4netlb.sharedResourcesLock.Unlock()
	}
	// TODO(kl52752) Add deletion for shared and dedicated HC despise the bool value.
	// We need because in case of update ExternalTrafficPolicy from Local to Cluster
	// there may be some old HC that have not been deleted.
	// Add this check also to L4 ILB
	err = utils.IgnoreHTTPNotFound(healthchecks.DeleteHealthCheck(l4netlb.cloud, hcName, l4netlb.scope))
	if err != nil {
		if !utils.IsInUsedByError(err) {
			klog.Errorf("Failed to delete healthcheck for service %s - %v", l4netlb.NamespacedName.String(), err)
			result.GCEResourceInError = annotations.HealthcheckResource
			result.Error = err
			return result
		}
		// Ignore deletion error due to health check in use by another resource.
		// This will be hit if this is a shared healthcheck.
		klog.V(2).Infof("Failed to delete healthcheck %s: health check in use.", hcName)
	}
	return result
}

// GetFRName returns the name of the forwarding rule for the given L4 External LoadBalancer service.
// This appends the protocol to the forwarding rule name, which will help supporting multiple protocols in the same service.
func (l4netlb *L4NetLB) GetFRName() string {
	_, _, _, protocol := utils.GetPortsAndProtocol(l4netlb.Service.Spec.Ports)
	return l4netlb.getFRNameWithProtocol(string(protocol))
}

func (l4netlb *L4NetLB) getFRNameWithProtocol(protocol string) string {
	return l4netlb.namer.L4ForwardingRule(l4netlb.Service.Namespace, l4netlb.Service.Name, strings.ToLower(protocol))
}

func (l4netlb *L4NetLB) createFirewalls(name, hcLink, hcFwName string, hcPort int32, nodeNames []string, sharedHC bool) (string, *SyncResultNetLB) {
	_, portRanges, _, protocol := utils.GetPortsAndProtocol(l4netlb.Service.Spec.Ports)
	result := &SyncResultNetLB{}
	sourceRanges, err := helpers.GetLoadBalancerSourceRanges(l4netlb.Service)
	if err != nil {
		result.Error = err
		return "", result
	}
	hcSourceRanges := gce.L4LoadBalancerSrcRanges()
	ensureFunc := func(name, IP string, sourceRanges, portRanges []string, proto string, shared bool) error {
		if shared {
			l4netlb.sharedResourcesLock.Lock()
			defer l4netlb.sharedResourcesLock.Unlock()
		}
		nsName := utils.ServiceKeyFunc(l4netlb.Service.Namespace, l4netlb.Service.Name)
		err := firewalls.EnsureL4FirewallRule(l4netlb.cloud, name, IP, nsName, sourceRanges, portRanges, nodeNames, proto, shared, utils.XLB)
		if err != nil {
			if fwErr, ok := err.(*firewalls.FirewallXPNError); ok {
				l4netlb.recorder.Eventf(l4netlb.Service, corev1.EventTypeNormal, "XPN", fwErr.Message)
				return nil
			}
			return err
		}
		return nil
	}

	// Add firewall rule for L4 External LoadBalncer traffic to nodes
	result.Error = ensureFunc(name, hcLink, sourceRanges.StringSlice(), portRanges, string(protocol), sharedHC)
	if result.Error != nil {
		result.GCEResourceInError = annotations.FirewallRuleResource
		return "", result
	}

	// Add firewall rule for healthchecks to nodes
	result.Error = ensureFunc(hcFwName, "", hcSourceRanges, []string{strconv.Itoa(int(hcPort))}, string(corev1.ProtocolTCP), sharedHC)
	if result.Error != nil {
		result.GCEResourceInError = annotations.FirewallForHealthcheckResource
	}
	return string(protocol), result
}

func (l4netlb *L4NetLB) cleanupForwardingRuleIfProtocolChanged(name, protocol string) (*composite.ForwardingRule, error) {
	// Check if protocol has changed for this service. In this case, forwarding rule should be deleted before
	// the backend service can be updated.
	existingBS, err := l4netlb.backendPool.Get(name, meta.VersionGA, l4netlb.scope)
	err = utils.IgnoreHTTPNotFound(err)
	if err != nil {
		klog.Errorf("Failed to lookup existing backend service, ignoring err: %v", err)
		return nil, err
	}
	existingFR := l4netlb.GetForwardingRule(l4netlb.GetFRName(), meta.VersionGA)
	if existingBS != nil && existingBS.Protocol != protocol {
		klog.Infof("Protocol changed from %q to %q for service %s", existingBS.Protocol, protocol, l4netlb.NamespacedName)
		// Delete forwarding rule if it exists
		existingFR = l4netlb.GetForwardingRule(l4netlb.getFRNameWithProtocol(existingBS.Protocol), meta.VersionGA)
		l4netlb.deleteForwardingRule(l4netlb.getFRNameWithProtocol(existingBS.Protocol), meta.VersionGA)
	}
	return existingFR, nil
}
