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

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/healthchecksprovider"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	// L4 Load Balancer parameters
	gceHcCheckIntervalSeconds = int64(8)
	gceHcTimeoutSeconds       = int64(1)
	// Start sending requests as soon as one healthcheck succeeds.
	gceHcHealthyThreshold = int64(1)
	// Defaults to 3 * 8 = 24 seconds before the LB will steer traffic away.
	gceHcUnhealthyThreshold = int64(3)
)

var (
	// instanceLock to prevent duplicate initialization.
	instanceLock = &sync.Mutex{}
	// instance is a singleton instance, created by Initialize
	instance *l4HealthChecks
)

type l4HealthChecks struct {
	// sharedResourceLock serializes operations on the healthcheck and firewall
	// resources shared across multiple Services.
	sharedResourcesLock sync.Mutex
	hcProvider          healthChecksProvider
	cloud               *gce.Cloud
	recorderFactory     events.RecorderProducer
}

// Initialize creates singleton instance, must be run before GetInstance() func
func Initialize(cloud *gce.Cloud, recorderFactory events.RecorderProducer) {
	instanceLock.Lock()
	defer instanceLock.Unlock()

	if instance != nil {
		klog.Error("Multiple L4 Healthchecks initialization attempts")
		return
	}

	instance = &l4HealthChecks{
		cloud:           cloud,
		recorderFactory: recorderFactory,
		hcProvider:      healthchecksprovider.NewHealthChecks(cloud, meta.VersionGA),
	}
	klog.V(3).Infof("Initialized L4 Healthchecks")
}

// Fake creates instance of l4HealthChecks. Use for test only.
func Fake(cloud *gce.Cloud, recorderFactory events.RecorderProducer) *l4HealthChecks {
	instance = &l4HealthChecks{
		cloud:           cloud,
		recorderFactory: recorderFactory,
		hcProvider:      healthchecksprovider.NewHealthChecks(cloud, meta.VersionGA),
	}
	return instance
}

// GetInstance returns singleton instance, must be run after Initialize
func GetInstance() *l4HealthChecks {
	return instance
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
func (l4hc *l4HealthChecks) EnsureHealthCheckWithFirewall(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType, nodeNames []string) *EnsureL4HealthCheckResult {
	namespacedName := types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}

	hcName := namer.L4HealthCheck(svc.Namespace, svc.Name, sharedHC)
	hcFwName := namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, sharedHC)
	hcPath, hcPort := helpers.GetServiceHealthCheckPathPort(svc)
	klog.V(3).Infof("Ensuring L4 healthcheck: %s and firewall rule %s from service %s, shared: %v.", hcName, hcFwName, namespacedName.String(), sharedHC)

	if sharedHC {
		hcPath, hcPort = gce.GetNodesHealthCheckPath(), gce.GetNodesHealthCheckPort()
		// We need to acquire a controller-wide mutex to ensure that in the case of a healthcheck shared between loadbalancers that the sync of the GCE resources is not performed in parallel.
		l4hc.sharedResourcesLock.Lock()
		defer l4hc.sharedResourcesLock.Unlock()
	}
	klog.V(3).Infof("L4 Healthcheck %s, path: %q, port %d", hcName, hcPath, hcPort)

	hcLink, err := l4hc.ensureHealthCheck(hcName, namespacedName, sharedHC, hcPath, hcPort, scope, l4Type)
	if err != nil {
		return &EnsureL4HealthCheckResult{
			GceResourceInError: annotations.HealthcheckResource,
			Err:                err,
		}
	}

	klog.V(3).Infof("Healthcheck created, ensuring firewall rule %s", hcFwName)
	err = l4hc.ensureFirewall(svc, hcFwName, hcPort, sharedHC, nodeNames)
	if err != nil {
		return &EnsureL4HealthCheckResult{
			GceResourceInError: annotations.HealthcheckResource,
			Err:                err,
		}
	}
	return &EnsureL4HealthCheckResult{
		HCName:             hcName,
		HCLink:             hcLink,
		HCFirewallRuleName: hcFwName,
	}
}

func (l4hc *l4HealthChecks) ensureHealthCheck(hcName string, svcName types.NamespacedName, shared bool, path string, port int32, scope meta.KeyType, l4Type utils.L4LBType) (string, error) {
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
		klog.V(3).Infof("Healthcheck %v already exists", hcName)
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

// ensureFirewall rule for `svc`.
//
// L4 ILB and L4 NetLB Services with ExternalTrafficPolicy=Cluster use the same firewall
// rule at global scope.
func (l4hc *l4HealthChecks) ensureFirewall(svc *corev1.Service, hcFwName string, hcPort int32, sharedHC bool, nodeNames []string) error {
	// Add firewall rule for healthchecks to nodes
	hcFWRParams := firewalls.FirewallParams{
		PortRanges:   []string{strconv.Itoa(int(hcPort))},
		SourceRanges: gce.L4LoadBalancerSrcRanges(),
		Protocol:     string(corev1.ProtocolTCP),
		Name:         hcFwName,
		NodeNames:    nodeNames,
	}
	return firewalls.EnsureL4LBFirewallForHc(svc, sharedHC, &hcFWRParams, l4hc.cloud, l4hc.recorderFactory.Recorder(svc.Namespace))
}

// DeleteHealthCheckWithFirewall deletes health check (and firewall rule) for l4 service. Checks if shared resources are safe to delete.
func (l4hc *l4HealthChecks) DeleteHealthCheckWithFirewall(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType) (string, error) {
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

	// Health check deleted, now delete the firewall rule
	return l4hc.deleteHealthCheckFirewall(svc, namer, sharedHC, l4Type)
}

func (l4hc *l4HealthChecks) deleteHealthCheck(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType) (bool, error) {
	hcName := namer.L4HealthCheck(svc.Namespace, svc.Name, sharedHC)
	klog.V(3).Infof("Deleting L4 healthcheck %s for service %s/%s, shared: %v, scope: %v", hcName, svc.Namespace, svc.Name, sharedHC, scope)

	err := l4hc.hcProvider.Delete(hcName, scope)
	if err != nil {
		// Ignore deletion error due to health check in use by another resource.
		if !utils.IsInUsedByError(err) {
			klog.Errorf("Failed to delete healthcheck %s for service %s/%s - %v", hcName, svc.Namespace, svc.Name, err)
			return false, err
		}
		klog.V(2).Infof("Failed to delete healthcheck %s: shared health check in use.", hcName)
		return false, nil
	}
	return true, nil
}

func (l4hc *l4HealthChecks) deleteHealthCheckFirewall(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, l4Type utils.L4LBType) (string, error) {
	hcName := namer.L4HealthCheck(svc.Namespace, svc.Name, sharedHC)
	hcFwName := namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, sharedHC)

	safeToDelete, err := l4hc.healthCheckFirewallSafeToDelete(hcName, sharedHC, l4Type)
	if err != nil {
		klog.Errorf("Failed to delete health check firewall rule %s for service %s/%s - %v", hcFwName, svc.Namespace, svc.Name, err)
		return annotations.HealthcheckResource, err
	}
	if !safeToDelete {
		klog.V(3).Infof("Failed to delete health check firewall rule %s: health check is in use.", hcName)
		return "", nil
	}
	klog.V(3).Infof("Deleting healthcheck firewall rule %s for health check %s", hcFwName, hcName)
	// Delete healthcheck firewall rule if no healthcheck uses the firewall rule.
	err = l4hc.deleteFirewall(hcFwName, svc)
	if err != nil {
		klog.Errorf("Failed to delete firewall rule %s for loadbalancer service %s/%s, err %v", hcFwName, svc.Namespace, svc.Name, err)
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
		recorder := l4hc.recorderFactory.Recorder(svc.Namespace)
		recorder.Eventf(svc, corev1.EventTypeNormal, "XPN", fwErr.Message)
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
	return &composite.HealthCheck{
		Name:               name,
		CheckIntervalSec:   gceHcCheckIntervalSeconds,
		TimeoutSec:         gceHcTimeoutSeconds,
		HealthyThreshold:   gceHcHealthyThreshold,
		UnhealthyThreshold: gceHcUnhealthyThreshold,
		HttpHealthCheck:    &httpSettings,
		Type:               "HTTP",
		Description:        desc,
		Scope:              scope,
		// Region will be omited by GCP API if Scope is set to Global
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
