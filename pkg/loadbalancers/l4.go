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
	"strconv"
	"strings"
	"sync"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider/service/helpers"
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

// Many of the functions in this file are re-implemented from gce_loadbalancer_internal.go
// L4 handles the resource creation/deletion/update for a given L4 ILB service.
type L4 struct {
	cloud       *gce.Cloud
	backendPool *backends.Backends
	scope       meta.KeyType
	namer       *namer.Namer
	// recorder is used to generate k8s Events.
	recorder            record.EventRecorder
	Service             *corev1.Service
	ServicePort         utils.ServicePort
	NamespacedName      types.NamespacedName
	sharedResourcesLock *sync.Mutex
	// metricsCollector exports L4 ILB service controller usage metrics.
	metricsCollector metrics.L4ILBMetricsCollector
}

// NewL4Handler creates a new L4Handler for the given L4 service.
func NewL4Handler(service *corev1.Service, cloud *gce.Cloud, scope meta.KeyType, namer *namer.Namer, recorder record.EventRecorder, lock *sync.Mutex, collector metrics.L4ILBMetricsCollector) *L4 {
	l := &L4{cloud: cloud, scope: scope, namer: namer, recorder: recorder, Service: service, sharedResourcesLock: lock, metricsCollector: collector}
	l.NamespacedName = types.NamespacedName{Name: service.Name, Namespace: service.Namespace}
	l.backendPool = backends.NewPool(l.cloud, l.namer)
	l.ServicePort = utils.ServicePort{ID: utils.ServicePortID{Service: l.NamespacedName}, BackendNamer: l.namer,
		VMIPNEGEnabled: true}
	return l
}

// CreateKey generates a meta.Key for a given GCE resource name.
func (l *L4) CreateKey(name string) (*meta.Key, error) {
	return composite.CreateKey(l.cloud, name, l.scope)
}

// getILBOptions fetches the optional features requested on the given ILB service.
func getILBOptions(svc *corev1.Service) gce.ILBOptions {
	return gce.ILBOptions{AllowGlobalAccess: gce.GetLoadBalancerAnnotationAllowGlobalAccess(svc),
		SubnetName: gce.GetLoadBalancerAnnotationSubnet(svc)}
}

// EnsureInternalLoadBalancerDeleted performs a cleanup of all GCE resources for the given loadbalancer service.
func (l *L4) EnsureInternalLoadBalancerDeleted(svc *corev1.Service) error {
	klog.V(2).Infof("EnsureInternalLoadBalancerDeleted(%s): attempting delete of load balancer resources", l.NamespacedName.String())
	sharedHC := !helpers.RequestsOnlyLocalTraffic(svc)
	// All resources use the NEG Name, except forwarding rule.
	name := l.namer.VMIPNEG(svc.Namespace, svc.Name)
	frName := l.GetFRName()
	key, err := l.CreateKey(frName)
	if err != nil {
		klog.Errorf("Failed to create key for LoadBalancer resources with name %s for service %s, err %v", frName, l.NamespacedName.String(), err)
		return err
	}
	retErr := err
	// If any resource deletion fails, log the error and continue cleanup.
	if err = utils.IgnoreHTTPNotFound(composite.DeleteForwardingRule(l.cloud, key, getAPIVersion(getILBOptions(l.Service)))); err != nil {
		klog.Errorf("Failed to delete forwarding rule for internal loadbalancer service %s, err %v", l.NamespacedName.String(), err)
		retErr = err
	}
	if err = ensureAddressDeleted(l.cloud, name, l.cloud.Region()); err != nil {
		klog.Errorf("Failed to delete address for internal loadbalancer service %s, err %v", l.NamespacedName.String(), err)
		retErr = err
	}
	hcName, hcFwName := healthchecks.HealthCheckName(sharedHC, l.namer.UID(), name)
	// delete fw rules
	deleteFunc := func(name string) error {
		err := firewalls.EnsureL4InternalFirewallRuleDeleted(l.cloud, name)
		if err != nil {
			if fwErr, ok := err.(*firewalls.FirewallXPNError); ok {
				l.recorder.Eventf(l.Service, corev1.EventTypeNormal, "XPN", fwErr.Message)
				return nil
			}
			return err
		}
		return nil
	}
	// delete firewall rule allowing load balancer source ranges
	err = deleteFunc(name)
	if err != nil {
		klog.Errorf("Failed to delete firewall rule %s for internal loadbalancer service %s, err %v", name, l.NamespacedName.String(), err)
		retErr = err
	}

	// delete firewall rule allowing healthcheck source ranges
	err = deleteFunc(hcFwName)
	if err != nil {
		klog.Errorf("Failed to delete firewall rule %s for internal loadbalancer service %s, err %v", hcFwName, l.NamespacedName.String(), err)
		retErr = err
	}
	// delete backend service
	err = utils.IgnoreHTTPNotFound(l.backendPool.Delete(name, meta.VersionGA, meta.Regional))
	if err != nil {
		klog.Errorf("Failed to delete backends for internal loadbalancer service %s, err  %v", l.NamespacedName.String(), err)
		retErr = err
	}

	// Delete healthcheck
	if sharedHC {
		l.sharedResourcesLock.Lock()
		defer l.sharedResourcesLock.Unlock()
	}
	err = utils.IgnoreHTTPNotFound(healthchecks.DeleteHealthCheck(l.cloud, hcName))
	if err != nil {
		if !utils.IsInUsedByError(err) {
			klog.Errorf("Failed to delete healthcheck for internal loadbalancer service %s, err %v", l.NamespacedName.String(), err)
			return err
		}
		// Ignore deletion error due to health check in use by another resource.
		// This will be hit if this is a shared healthcheck.
		klog.V(2).Infof("Failed to delete healthcheck %s: health check in use.", hcName)
	}
	if retErr == nil {
		klog.V(6).Infof("Service %s deleted, removing its state from metrics cache", l.NamespacedName.String())
		l.metricsCollector.DeleteL4ILBService(l.NamespacedName.String())
	}
	return retErr
}

// GetFRName returns the name of the forwarding rule for the given ILB service.
// This appends the protocol to the forwarding rule name, which will help supporting multiple protocols in the same ILB
// service.
func (l *L4) GetFRName() string {
	_, _, protocol := utils.GetPortsAndProtocol(l.Service.Spec.Ports)
	lbName := l.namer.VMIPNEG(l.Service.Namespace, l.Service.Name)
	return lbName + "-" + strings.ToLower(string(protocol))
}

// EnsureInternalLoadBalancer ensures that all GCE resources for the given loadbalancer service have
// been created. It returns a LoadBalancerStatus with the updated ForwardingRule IP address.
func (l *L4) EnsureInternalLoadBalancer(nodeNames []string, svc *corev1.Service) (*corev1.LoadBalancerStatus, error) {
	// Use the same resource name for NEG, BackendService as well as FR, FWRule.
	l.Service = svc
	name := l.namer.VMIPNEG(l.Service.Namespace, l.Service.Name)
	options := getILBOptions(l.Service)

	// create healthcheck
	sharedHC := !helpers.RequestsOnlyLocalTraffic(l.Service)
	hcName, hcFwName := healthchecks.HealthCheckName(sharedHC, l.namer.UID(), name)
	hcPath, hcPort := gce.GetNodesHealthCheckPath(), gce.GetNodesHealthCheckPort()
	if !sharedHC {
		hcPath, hcPort = helpers.GetServiceHealthCheckPathPort(l.Service)
	} else {
		// Take the lock when creating the shared healthcheck
		l.sharedResourcesLock.Lock()
	}
	_, hcLink, err := healthchecks.EnsureL4HealthCheck(l.cloud, hcName, l.NamespacedName, sharedHC, hcPath, hcPort)
	if sharedHC {
		// unlock here so rest of the resource creation API can be called without unnecessarily holding the lock.
		l.sharedResourcesLock.Unlock()
	}
	if err != nil {
		return nil, err
	}

	_, portRanges, protocol := utils.GetPortsAndProtocol(l.Service.Spec.Ports)

	// ensure firewalls
	sourceRanges, err := helpers.GetLoadBalancerSourceRanges(l.Service)
	if err != nil {
		return nil, err
	}
	hcSourceRanges := gce.L4LoadBalancerSrcRanges()
	ensureFunc := func(name, IP string, sourceRanges, portRanges []string, proto string) error {
		nsName := utils.ServiceKeyFunc(l.Service.Namespace, l.Service.Name)
		err := firewalls.EnsureL4InternalFirewallRule(l.cloud, name, IP, nsName, sourceRanges, portRanges, nodeNames, proto)
		if err != nil {
			if fwErr, ok := err.(*firewalls.FirewallXPNError); ok {
				l.recorder.Eventf(l.Service, corev1.EventTypeNormal, "XPN", fwErr.Message)
				return nil
			}
			return err
		}
		return nil
	}
	// Add firewall rule for ILB traffic to nodes
	err = ensureFunc(name, "", sourceRanges.StringSlice(), portRanges, string(protocol))
	if err != nil {
		return nil, err
	}

	// Add firewall rule for healthchecks to nodes
	err = ensureFunc(hcFwName, "", hcSourceRanges, []string{strconv.Itoa(int(hcPort))}, string(corev1.ProtocolTCP))
	if err != nil {
		return nil, err
	}
	// ensure backend service
	bs, err := l.backendPool.EnsureL4BackendService(name, hcLink, string(protocol), string(l.Service.Spec.SessionAffinity),
		string(cloud.SchemeInternal), l.NamespacedName, meta.VersionGA)
	if err != nil {
		return nil, err
	}

	// create fr rule
	fr, err := l.ensureForwardingRule(l.GetFRName(), bs.SelfLink, options)
	if err != nil {
		klog.Errorf("EnsureInternalLoadBalancer: Failed to create forwarding rule - %v", err)
		return nil, err
	}

	var serviceState metrics.L4ILBServiceState
	if options.AllowGlobalAccess {
		serviceState.EnabledGlobalAccess = true
	}
	// SubnetName is overrided to nil value if Alpha feature gate for custom subnet
	// is not enabled. So, a non empty subnet name at this point implies that the
	// feature is in use.
	if options.SubnetName != "" {
		serviceState.EnabledCustomSubnet = true
	}
	klog.V(6).Infof("Service %s ensured, updating its state %v in metrics cache", l.NamespacedName.String(), serviceState)
	l.metricsCollector.SetL4ILBService(l.NamespacedName.String(), serviceState)

	return &corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: fr.IPAddress}}}, nil
}
