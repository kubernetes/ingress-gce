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

	cloudprovider "github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
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
	// instance is a sinngleton instance, created by InitializeL4
	instance *l4HealthChecks
	// mutex for preventing multiple initialization
	initLock = &sync.Mutex{}
)

type l4HealthChecks struct {
	mutex           sync.Mutex
	cloud           *gce.Cloud
	recorderFactory events.RecorderProducer
}

// InitializeL4 creates singleton instance, must be run before GetL4() func
func InitializeL4(cloud *gce.Cloud, recorderFactory events.RecorderProducer) {
	if instance == nil {
		initLock.Lock()
		defer initLock.Unlock()

		if instance == nil {
			instance = &l4HealthChecks{
				cloud:           cloud,
				recorderFactory: recorderFactory,
			}
		}
	}
}

// FakeL4 creates instance of l4HealthChecks> USe for test only.
func FakeL4(cloud *gce.Cloud, recorderFactory events.RecorderProducer) *l4HealthChecks {
	instance = &l4HealthChecks{
		cloud:           cloud,
		recorderFactory: recorderFactory,
	}
	return instance
}

// GetL4 returns singleton instance, must be run after InitializeL4
func GetL4() *l4HealthChecks {
	return instance
}

// EnsureL4HealthCheck creates a new HTTP health check for an L4 LoadBalancer service, and the firewall rule required
// for the healthcheck. If healthcheck is shared (external traffic policy 'cluster') then firewall rules will be shared
// regardless of scope param.
// If the healthcheck already exists, it is updated as needed.
func (l4hc *l4HealthChecks) EnsureL4HealthCheck(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType, nodeNames []string) (string, string, string, string, error) {
	hcName, hcFwName := namer.L4HealthCheck(svc.Namespace, svc.Name, sharedHC)
	hcPath, hcPort := helpers.GetServiceHealthCheckPathPort(svc)
	if sharedHC {
		hcPath, hcPort = gce.GetNodesHealthCheckPath(), gce.GetNodesHealthCheckPort()
		// lock out entire EnsureL4HealthCheck process
		l4hc.mutex.Lock()
		defer l4hc.mutex.Unlock()
	}

	namespacedName := types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}
	_, hcLink, err := l4hc.ensureL4HealthCheckInternal(hcName, namespacedName, sharedHC, hcPath, hcPort, scope, l4Type)
	if err != nil {
		return "", "", "", annotations.HealthcheckResource, err
	}
	err = l4hc.ensureFirewall(svc, hcFwName, hcPort, sharedHC, nodeNames)
	if err != nil {
		return "", "", "", annotations.FirewallForHealthcheckResource, err
	}

	return hcLink, hcFwName, hcName, "", err
}

// DeleteHealthCheck deletes health check (and firewall rule) for l4 service. Checks if shared resources are safe to delete.
func (l4hc *l4HealthChecks) DeleteHealthCheck(svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType) (string, error) {
	if sharedHC {
		// lock out entire DeleteHealthCheck process
		l4hc.mutex.Lock()
		defer l4hc.mutex.Unlock()
	}

	hcName, hcFwName := namer.L4HealthCheck(svc.Namespace, svc.Name, sharedHC)
	namespacedName := types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}
	err := utils.IgnoreHTTPNotFound(l4hc.deleteHealthCheck(hcName, scope))
	if err != nil {
		if !utils.IsInUsedByError(err) {
			klog.Errorf("Failed to delete healthcheck for service %s - %v", namespacedName.String(), err)
			return annotations.HealthcheckResource, err
		}
		// Ignore deletion error due to health check in use by another resource.
		// This will be hit if this is a shared healthcheck.
		klog.V(2).Infof("Failed to delete healthcheck %s: health check in use.", hcName)
		return "", nil
	}
	// Health check deleted, now delete the firewall rule
	return l4hc.deleteHealthCheckFirewall(svc, hcName, hcFwName, sharedHC, l4Type)
}

func (l4hc *l4HealthChecks) ensureL4HealthCheckInternal(name string, svcName types.NamespacedName, shared bool, path string, port int32, scope meta.KeyType, l4Type utils.L4LBType) (*composite.HealthCheck, string, error) {
	selfLink := ""
	key, err := composite.CreateKey(l4hc.cloud, name, scope)
	if err != nil {
		return nil, selfLink, fmt.Errorf("Failed to create key for healthcheck with name %s for service %s", name, svcName.String())
	}
	hc, err := composite.GetHealthCheck(l4hc.cloud, key, meta.VersionGA)
	if err != nil {
		if !utils.IsNotFoundError(err) {
			return nil, selfLink, err
		}
	}
	var region string
	if scope == meta.Regional {
		region = l4hc.cloud.Region()
	}
	expectedHC := NewL4HealthCheck(name, svcName, shared, path, port, l4Type, scope, region)
	if hc == nil {
		// Create the healthcheck
		klog.V(2).Infof("Creating healthcheck %s for service %s, shared = %v", name, svcName, shared)
		err = composite.CreateHealthCheck(l4hc.cloud, key, expectedHC)
		if err != nil {
			return nil, selfLink, err
		}
		selfLink = cloudprovider.SelfLink(meta.VersionGA, l4hc.cloud.ProjectID(), "healthChecks", key)
		return expectedHC, selfLink, nil
	}
	selfLink = hc.SelfLink
	if !needToUpdateHealthChecks(hc, expectedHC) {
		// nothing to do
		return hc, selfLink, nil
	}
	mergeHealthChecks(hc, expectedHC)
	klog.V(2).Infof("Updating healthcheck %s for service %s", name, svcName)
	err = composite.UpdateHealthCheck(l4hc.cloud, key, expectedHC)
	if err != nil {
		return nil, selfLink, err
	}
	return expectedHC, selfLink, err
}

// ensureFirewall rule for L4 service.
// The firewall rules are the same for ILB and NetLB that use external traffic policy 'local' (sharedHC == true)
// despite healthchecks have different scopes (global vs regional)
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

func (l4hc *l4HealthChecks) deleteHealthCheck(name string, scope meta.KeyType) error {
	key, err := composite.CreateKey(l4hc.cloud, name, scope)
	if err != nil {
		return fmt.Errorf("Failed to create composite key for healthcheck %s - %w", name, err)
	}
	return composite.DeleteHealthCheck(l4hc.cloud, key, meta.VersionGA)
}

func (l4hc *l4HealthChecks) deleteHealthCheckFirewall(svc *corev1.Service, hcName, hcFwName string, sharedHC bool, l4Type utils.L4LBType) (string, error) {
	namespacedName := types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}

	safeToDelete, err := l4hc.healthCheckFirewallSafeToDelete(hcName, sharedHC, l4Type)
	if err != nil {
		klog.Errorf("Failed to delete health check firewall rule %s for service %s - %v", hcFwName, namespacedName.String(), err)
		return annotations.HealthcheckResource, err
	}
	if !safeToDelete {
		klog.V(2).Infof("Failed to delete health check firewall rule %s: health check in use.", hcName)
		return "", nil
	}
	// Delete healthcheck firewall rule if no healthcheck uses the firewall rule.
	err = l4hc.deleteFirewall(hcFwName, svc)
	if err != nil {
		klog.Errorf("Failed to delete firewall rule %s for internal loadbalancer service %s, err %v", hcFwName, namespacedName.String(), err)
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
	key, err := composite.CreateKey(l4hc.cloud, hcName, scopeToCheck)
	if err != nil {
		return false, fmt.Errorf("Failed to create composite key for healthcheck %s - %w", hcName, err)
	}
	_, err = composite.GetHealthCheck(l4hc.cloud, key, meta.VersionGA)
	return utils.IsNotFoundError(err), nil
}

func (l4hc *l4HealthChecks) deleteFirewall(name string, svc *corev1.Service) error {
	err := firewalls.EnsureL4FirewallRuleDeleted(l4hc.cloud, name)
	if err != nil {
		if fwErr, ok := err.(*firewalls.FirewallXPNError); ok {
			recorder := l4hc.recorderFactory.Recorder(svc.Namespace)
			recorder.Eventf(svc, corev1.EventTypeNormal, "XPN", fwErr.Message)
			return nil
		}
		return err
	}
	return nil
}

func NewL4HealthCheck(name string, svcName types.NamespacedName, shared bool, path string, port int32, l4Type utils.L4LBType, scope meta.KeyType, region string) *composite.HealthCheck {
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
// since it is created by the NewL4HealthCheck call.
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
