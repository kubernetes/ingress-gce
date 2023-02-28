package loadbalancers

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

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/healthchecksl4"
	"k8s.io/ingress-gce/pkg/utils"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	servicehelper "k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/test"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
// TODO Uncomment after https://github.com/kubernetes/kubernetes/pull/87667 is available in vendor.
// eventMsgFirewallChange = "XPN Firewall change required by network admin"
)

func getFakeGCECloud(vals gce.TestClusterValues) *gce.Cloud {
	fakeGCE := gce.NewFakeGCECloud(vals)
	// InsertHook required to assign an IP Address for forwarding rule
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAddresses.InsertHook = mock.InsertAddressHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAlphaAddresses.X = mock.AddressAttributes{}
	(fakeGCE.Compute().(*cloud.MockGCE)).MockAddresses.X = mock.AddressAttributes{}
	(fakeGCE.Compute().(*cloud.MockGCE)).MockForwardingRules.InsertHook = mock.InsertFwdRuleHook

	(fakeGCE.Compute().(*cloud.MockGCE)).MockRegionBackendServices.UpdateHook = mock.UpdateRegionBackendServiceHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockHealthChecks.UpdateHook = mock.UpdateHealthCheckHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockFirewalls.PatchHook = mock.UpdateFirewallHook
	return fakeGCE
}

func TestEnsureInternalBackendServiceUpdates(t *testing.T) {
	t.Parallel()
	fakeGCE := getFakeGCECloud(gce.DefaultTestClusterValues())

	svc := test.NewL4ILBService(false, 8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)
	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	bsName := l4.namer.L4Backend(l4.Service.Namespace, l4.Service.Name)
	_, err := l4.backendPool.EnsureL4BackendService(bsName, "", "TCP", string(svc.Spec.SessionAffinity), string(cloud.SchemeInternal), l4.NamespacedName)
	if err != nil {
		t.Errorf("Failed to ensure backend service  %s - err %v", bsName, err)
	}

	// Update the Internal Backend Service with a new ServiceAffinity
	_, err = l4.backendPool.EnsureL4BackendService(bsName, "", "TCP", string(v1.ServiceAffinityNone), string(cloud.SchemeInternal), l4.NamespacedName)
	if err != nil {
		t.Errorf("Failed to ensure backend service  %s - err %v", bsName, err)
	}
	key := meta.RegionalKey(bsName, l4.cloud.Region())
	bs, err := composite.GetBackendService(l4.cloud, key, meta.VersionGA)
	if err != nil {
		t.Errorf("Failed to get backend service  %s - err %v", bsName, err)
	}
	if bs.SessionAffinity != strings.ToUpper(string(v1.ServiceAffinityNone)) {
		t.Errorf("Expected session affinity '%s' in %+v, Got '%s'", strings.ToUpper(string(v1.ServiceAffinityNone)), bs, bs.SessionAffinity)
	}
	// Change the Connection Draining timeout to a different value manually. Also update session Affinity to trigger
	// an update in the Ensure method. The timeout value should not be reconciled.
	newTimeout := int64(backends.DefaultConnectionDrainingTimeoutSeconds * 2)
	bs.ConnectionDraining.DrainingTimeoutSec = newTimeout
	bs.SessionAffinity = strings.ToUpper(string(v1.ServiceAffinityClientIP))
	err = composite.UpdateBackendService(l4.cloud, key, bs)
	if err != nil {
		t.Errorf("Failed to update backend service with new connection draining timeout - err %v", err)
	}
	bs, err = l4.backendPool.EnsureL4BackendService(bsName, "", "TCP", string(v1.ServiceAffinityNone), string(cloud.SchemeInternal), l4.NamespacedName)
	if err != nil {
		t.Errorf("Failed to ensure backend service  %s - err %v", bsName, err)
	}
	if bs.SessionAffinity != strings.ToUpper(string(v1.ServiceAffinityNone)) {
		t.Errorf("Backend service did not get updated.")
	}
	if bs.ConnectionDraining.DrainingTimeoutSec != newTimeout {
		t.Errorf("Connection Draining timeout got reconciled to %d, expected %d", bs.ConnectionDraining.DrainingTimeoutSec, newTimeout)
	}
}

func TestEnsureInternalLoadBalancer(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}
	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	svc := test.NewL4ILBService(false, 8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}

	result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)

	backendServiceName := l4.namer.L4Backend(l4.Service.Namespace, l4.Service.Name)
	key := meta.RegionalKey(backendServiceName, l4.cloud.Region())
	bs, err := composite.GetBackendService(l4.cloud, key, meta.VersionGA)
	if err != nil {
		t.Errorf("Failed to lookup backend service, err %v", err)
	}
	if len(bs.Backends) != 0 {
		// Backends are populated by NEG linker.
		t.Errorf("Unexpected backends list - %v, expected empty", bs.Backends)
	}
	// Add a backend list to simulate NEG linker populating the backends.
	bs.Backends = []*composite.Backend{{Group: "test"}}
	if err := composite.UpdateBackendService(l4.cloud, key, bs); err != nil {
		t.Errorf("Failed updating backend service, err %v", err)
	}
	// Simulate a periodic sync. The backends list should not be reconciled.
	result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)
	bs, err = composite.GetBackendService(l4.cloud, meta.RegionalKey(backendServiceName, l4.cloud.Region()), meta.VersionGA)
	if err != nil {
		t.Errorf("Failed to lookup backend service, err %v", err)
	}
	if len(bs.Backends) == 0 {
		t.Errorf("Backends got reconciled by the periodic sync")
	}
}

func TestEnsureInternalLoadBalancerTypeChange(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}
	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	svc := test.NewL4ILBService(false, 8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}
	result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Unexpected error %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)

	// Now add the latest annotation and change scheme to external
	svc.Annotations[gce.ServiceAnnotationLoadBalancerType] = ""
	// This will be invoked by service_controller
	if result = l4.EnsureInternalLoadBalancerDeleted(svc); result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	assertILBResourcesDeleted(t, l4)
}

func TestEnsureInternalLoadBalancerWithExistingResources(t *testing.T) {
	t.Parallel()

	vals := gce.DefaultTestClusterValues()
	nodeNames := []string{"test-node-1"}

	fakeGCE := getFakeGCECloud(vals)
	svc := test.NewL4ILBService(false, 8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}

	lbName := l4.namer.L4Backend(svc.Namespace, svc.Name)

	// Create the expected resources necessary for an Internal Load Balancer
	sharedHC := !servicehelper.RequestsOnlyLocalTraffic(svc)
	hcResult := l4.healthChecks.EnsureHealthCheckWithFirewall(l4.Service, l4.namer, sharedHC, meta.Global, utils.ILB, []string{})

	if hcResult.Err != nil {
		t.Errorf("Failed to create healthcheck, err %v", hcResult.Err)
	}
	_, err := l4.backendPool.EnsureL4BackendService(lbName, hcResult.HCLink, "TCP", string(l4.Service.Spec.SessionAffinity), string(cloud.SchemeInternal), l4.NamespacedName)
	if err != nil {
		t.Errorf("Failed to create backendservice, err %v", err)
	}
	result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)
}

// TestEnsureInternalLoadBalancerClearPreviousResources creates ILB resources with incomplete configuration and verifies
// that they are updated when the controller processes the load balancer service.
func TestEnsureInternalLoadBalancerClearPreviousResources(t *testing.T) {
	t.Parallel()

	vals := gce.DefaultTestClusterValues()
	nodeNames := []string{"test-node-1"}

	fakeGCE := getFakeGCECloud(vals)

	svc := test.NewL4ILBService(true, 8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	_, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName)
	if err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}

	lbName := l4.namer.L4Backend(svc.Namespace, svc.Name)
	frName := l4.GetFRName()
	key, err := composite.CreateKey(l4.cloud, frName, meta.Regional)
	if err != nil {
		t.Errorf("Unexpected error when creating key - %v", err)
	}

	// Create a ForwardingRule that's missing an IP address
	existingFwdRule := &composite.ForwardingRule{
		Name:                frName,
		IPAddress:           "",
		Ports:               []string{"123"},
		IPProtocol:          "TCP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
	}
	if err = composite.CreateForwardingRule(l4.cloud, key, existingFwdRule); err != nil {
		t.Errorf("Failed to create fake forwarding rule %s, err %v", lbName, err)
	}
	key.Name = lbName
	// Create a Firewall that's missing a Description
	existingFirewall := &compute.Firewall{
		Name:    lbName,
		Network: fakeGCE.NetworkURL(),
		Allowed: []*compute.FirewallAllowed{
			{
				IPProtocol: "tcp",
				Ports:      []string{"123"},
			},
		},
	}
	if err = fakeGCE.CreateFirewall(existingFirewall); err != nil {
		t.Errorf("fakeGCE.CreateFirewall(%v) returned error %v", existingFirewall, err)
	}

	sharedHealthCheck := !servicehelper.RequestsOnlyLocalTraffic(svc)
	hcName := l4.namer.L4HealthCheck(svc.Namespace, svc.Name, sharedHealthCheck)

	// Create a healthcheck with an incomplete fields
	existingHC := &composite.HealthCheck{Name: hcName}
	// hcName will be same as lbName since this service uses trafficPolicy Local. So the same key can be used.
	if err = composite.CreateHealthCheck(fakeGCE, key, existingHC); err != nil {
		t.Errorf("Failed to create fake healthcheck %s, err %v", hcName, err)
	}

	// Create a backend Service that's missing Description and Backends
	existingBS := &composite.BackendService{
		Name:                lbName,
		Protocol:            "TCP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
	}

	if err = composite.CreateBackendService(fakeGCE, key, existingBS); err != nil {
		t.Errorf("Failed to create fake backend service %s, err %v", lbName, err)
	}
	key.Name = frName
	// Set the backend service link correctly, so that forwarding rule comparison works correctly
	existingFwdRule.BackendService = cloud.SelfLink(meta.VersionGA, vals.ProjectID, "backendServices", meta.RegionalKey(existingBS.Name, vals.Region))
	if err = composite.DeleteForwardingRule(fakeGCE, key, meta.VersionGA); err != nil {
		t.Errorf("Failed to delete forwarding rule, err %v", err)
	}
	if err = composite.CreateForwardingRule(fakeGCE, key, existingFwdRule); err != nil {
		t.Errorf("Failed to update forwarding rule with new BS link, err %v", err)
	}
	if result := l4.EnsureInternalLoadBalancer(nodeNames, svc); result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer %s, err %v", lbName, result.Error)
	}
	key.Name = frName
	// Expect new resources with the correct attributes to be created
	newFwdRule, err := composite.GetForwardingRule(fakeGCE, key, meta.VersionGA)
	if err != nil {
		t.Errorf("Failed to lookup forwarding rule %s, err %v", lbName, err)
	}
	if newFwdRule == existingFwdRule {
		t.Errorf("Expected incomplete forwarding rule to be updated")
	}

	newFirewall, err := fakeGCE.GetFirewall(lbName)
	if err != nil {
		t.Errorf("Failed to lookup firewall rule %s, err %v", lbName, err)
	}
	if newFirewall == existingFirewall {
		t.Errorf("Expected incomplete firewall rule to be updated")
	}

	key.Name = lbName
	newHC, err := composite.GetHealthCheck(fakeGCE, key, meta.VersionGA)
	if err != nil {
		t.Errorf("Failed to lookup healthcheck %s, err %v", lbName, err)
	}
	if newHC == existingHC || newHC.SelfLink == "" {
		t.Errorf("Expected incomplete healthcheck to be updated")
	}

	newBS, err := composite.GetBackendService(fakeGCE, key, meta.VersionGA)
	if err != nil {
		t.Errorf("Failed to lookup backend service %s, err %v", lbName, err)
	}
	if newBS == existingBS {
		t.Errorf("Expected incomplete backend service to be updated")
	}
}

// TestUpdateResourceLinks verifies that an existing backend service created with different healthchecks is reconciled
// upon load balancer sync. The other healthchecks are not deleted.
func TestUpdateResourceLinks(t *testing.T) {
	t.Parallel()

	vals := gce.DefaultTestClusterValues()
	nodeNames := []string{"test-node-1"}

	fakeGCE := getFakeGCECloud(vals)

	svc := test.NewL4ILBService(true, 8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	_, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName)
	if err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}

	lbName := l4.namer.L4Backend(svc.Namespace, svc.Name)
	key, err := composite.CreateKey(l4.cloud, lbName, meta.Regional)
	if err != nil {
		t.Errorf("Unexpected error when creating key - %v", err)
	}
	key.Name = "hc1"
	hc1 := &composite.HealthCheck{Name: "hc1"}
	if err = composite.CreateHealthCheck(fakeGCE, key, hc1); err != nil {
		t.Errorf("Failed to create fake healthcheck hc1 , err %v", err)
	}

	key.Name = "hc2"
	hc2 := &composite.HealthCheck{Name: "hc2"}
	if err = composite.CreateHealthCheck(fakeGCE, key, hc2); err != nil {
		t.Errorf("Failed to create fake healthcheck hc2, err %v", err)
	}

	key.Name = lbName
	// Create a backend Service that's missing Description and Backends
	existingBS := &composite.BackendService{
		Name:                lbName,
		Protocol:            "TCP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		HealthChecks:        []string{"hc1", "hc2"},
	}

	if err = composite.CreateBackendService(fakeGCE, key, existingBS); err != nil {
		t.Errorf("Failed to create fake backend service %s, err %v", lbName, err)
	}
	bs, err := composite.GetBackendService(fakeGCE, key, meta.VersionGA)
	if err != nil {
		t.Errorf("Failed to lookup backend service")
	}
	if !reflect.DeepEqual(bs.HealthChecks, []string{"hc1", "hc2"}) {
		t.Errorf("Unexpected healthchecks in backend service - %v", bs.HealthChecks)
	}
	result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer %s, err %v", lbName, result.Error)
	}
	// verifies that the right healthcheck is present
	assertILBResources(t, l4, nodeNames, result.Annotations)

	// ensure that the other healthchecks still exist.
	key.Name = "hc1"
	if hc1, err = composite.GetHealthCheck(fakeGCE, key, meta.VersionGA); err != nil {
		t.Errorf("Failed to lookup healthcheck - hc1")
	}
	if hc1 == nil {
		t.Errorf("Got nil healthcheck")
	}
	key.Name = "hc2"
	if hc2, err = composite.GetHealthCheck(fakeGCE, key, meta.VersionGA); err != nil {
		t.Errorf("Failed to lookup healthcheck - hc1")
	}
	if hc2 == nil {
		t.Errorf("Got nil healthcheck")
	}
}

func TestEnsureInternalLoadBalancerHealthCheckConfigurable(t *testing.T) {
	t.Parallel()

	vals := gce.DefaultTestClusterValues()
	nodeNames := []string{"test-node-1"}

	fakeGCE := getFakeGCECloud(vals)

	svc := test.NewL4ILBService(true, 8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	_, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName)
	if err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}
	lbName := l4.namer.L4Backend(svc.Namespace, svc.Name)
	key, err := composite.CreateKey(l4.cloud, lbName, meta.Regional)
	if err != nil {
		t.Errorf("Unexpected error when creating key - %v", err)
	}
	sharedHealthCheck := !servicehelper.RequestsOnlyLocalTraffic(svc)
	hcName := l4.namer.L4HealthCheck(svc.Namespace, svc.Name, sharedHealthCheck)

	// Create a healthcheck with an incorrect threshold, default value is 8s.
	existingHC := &composite.HealthCheck{Name: hcName, CheckIntervalSec: 6000}
	if err = composite.CreateHealthCheck(fakeGCE, key, existingHC); err != nil {
		t.Errorf("Failed to create fake healthcheck %s, err %v", hcName, err)
	}

	if result := l4.EnsureInternalLoadBalancer(nodeNames, svc); result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer %s, err %v", lbName, result.Error)
	}

	newHC, err := composite.GetHealthCheck(fakeGCE, key, meta.VersionGA)
	if err != nil {
		t.Errorf("Failed to lookup healthcheck %s, err %v", lbName, err)
	}
	if newHC.CheckIntervalSec != existingHC.CheckIntervalSec {
		t.Errorf("Check interval got incorrectly reconciled")
	}
}

func TestEnsureInternalLoadBalancerDeleted(t *testing.T) {
	t.Parallel()

	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	nodeNames := []string{"test-node-1"}
	svc := test.NewL4ILBService(false, 8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}
	result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)

	// Delete the loadbalancer.
	result = l4.EnsureInternalLoadBalancerDeleted(svc)
	if result.Error != nil {
		t.Errorf("Unexpected error %v", result.Error)
	}
	assertILBResourcesDeleted(t, l4)
}

func TestEnsureInternalLoadBalancerDeletedTwiceDoesNotError(t *testing.T) {
	t.Parallel()

	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	nodeNames := []string{"test-node-1"}
	svc := test.NewL4ILBService(false, 8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}
	result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)

	// Delete the loadbalancer
	result = l4.EnsureInternalLoadBalancerDeleted(svc)
	if result.Error != nil {
		t.Errorf("Unexpected error %v", result.Error)
	}
	assertILBResourcesDeleted(t, l4)

	// Deleting the loadbalancer and resources again should not cause an error.
	result = l4.EnsureInternalLoadBalancerDeleted(svc)
	if result.Error != nil {
		t.Errorf("Unexpected error %v", result.Error)
	}
	assertILBResourcesDeleted(t, l4)
}

func TestEnsureInternalLoadBalancerDeletedWithSharedHC(t *testing.T) {
	t.Parallel()

	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	(fakeGCE.Compute().(*cloud.MockGCE)).MockHealthChecks.DeleteHook = test.DeleteHealthCheckResourceInUseErrorHook
	nodeNames := []string{"test-node-1"}
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	svc, l4, result := ensureService(fakeGCE, namer, nodeNames, vals.ZoneName, 8080, t)
	if result != nil && result.Error != nil {
		t.Fatalf("Error ensuring service err: %v", result.Error)
	}

	// Delete the loadbalancer.
	result = l4.EnsureInternalLoadBalancerDeleted(svc)
	if result.Error != nil {
		t.Errorf("Unexpected error %v", result.Error)
	}
	// When health check is shared we expectEqual that hc firewall rule will not be deleted.
	hcFwName := l4.namer.L4HealthCheckFirewall(l4.Service.Namespace, l4.Service.Name, true)
	firewall, err := l4.cloud.GetFirewall(hcFwName)
	if err != nil || firewall == nil {
		t.Errorf("Expected firewall exists err: %v, fwR: %v", err, firewall)
	}
}

func TestHealthCheckFirewallDeletionWithNetLB(t *testing.T) {
	t.Parallel()
	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	nodeNames := []string{"test-node-1"}
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	// Create ILB Service
	ilbSvc, l4, result := ensureService(fakeGCE, namer, nodeNames, vals.ZoneName, 8081, t)
	if result != nil && result.Error != nil {
		t.Fatalf("Error ensuring service err: %v", result.Error)
	}

	// Create NetLB Service
	netlbSvc := test.NewL4NetLBRBSService(8080)
	l4NetLBParams := &L4NetLBParams{
		Service:  netlbSvc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4NetLB := NewL4NetLB(l4NetLBParams)
	// make sure both ilb and netlb use the same l4 healthcheck instance
	l4NetLB.healthChecks = l4.healthChecks

	// create netlb resources
	xlbResult := l4NetLB.EnsureFrontend(nodeNames, netlbSvc)
	if xlbResult.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", xlbResult.Error)
	}
	if len(xlbResult.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4NetLB)
	}
	l4NetLB.Service.Annotations = xlbResult.Annotations
	assertNetLBResources(t, l4NetLB, nodeNames)

	// Delete the ILB loadbalancer
	result = l4.EnsureInternalLoadBalancerDeleted(ilbSvc)
	if result.Error != nil {
		t.Errorf("Unexpected error %v", result.Error)
	}

	// When NetLB health check uses the same firewall rules we expectEqual that hc firewall rule will not be deleted.
	hcName := l4.namer.L4HealthCheck(l4.Service.Namespace, l4.Service.Name, true)
	hcFwName := l4.namer.L4HealthCheckFirewall(l4.Service.Namespace, l4.Service.Name, true)
	firewall, err := l4.cloud.GetFirewall(hcFwName)
	if err != nil {
		t.Errorf("Expected error: firewall exists, got %v", err)
	}
	if firewall == nil {
		t.Error("Healthcheck Firewall should still exist, got nil")
	}

	// The healthcheck itself should be deleted.
	healthcheck, err := l4.cloud.GetHealthCheck(hcName)
	if err == nil || healthcheck != nil {
		t.Errorf("Expected error when looking up shared healthcheck after deletion")
	}
}

func ensureService(fakeGCE *gce.Cloud, namer *namer_util.L4Namer, nodeNames []string, zoneName string, port int, t *testing.T) (*v1.Service, *L4, *L4ILBSyncResult) {
	svc := test.NewL4ILBService(false, port)

	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, zoneName); err != nil {
		return nil, nil, &L4ILBSyncResult{Error: fmt.Errorf("Unexpected error when adding nodes %v", err)}
	}
	result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		return nil, nil, result
	}
	if len(result.Status.Ingress) == 0 {
		result.Error = fmt.Errorf("Got empty loadBalancer status using handler %v", l4)
		return nil, nil, result
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)
	return svc, l4, nil
}

func TestEnsureInternalLoadBalancerWithSpecialHealthCheck(t *testing.T) {
	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	nodeNames := []string{"test-node-1"}
	svc := test.NewL4ILBService(false, 8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}

	healthCheckNodePort := int32(10101)
	svc.Spec.HealthCheckNodePort = healthCheckNodePort
	svc.Spec.Type = v1.ServiceTypeLoadBalancer
	svc.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal

	result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)

	lbName := l4.namer.L4Backend(svc.Namespace, svc.Name)
	key, err := composite.CreateKey(l4.cloud, lbName, meta.Global)
	if err != nil {
		t.Errorf("Unexpected error when creating key - %v", err)
	}
	hc, err := composite.GetHealthCheck(l4.cloud, key, meta.VersionGA)
	if err != nil || hc == nil {
		t.Errorf("Failed to get healthcheck, err %v", err)
	}
	if hc.HttpHealthCheck.Port != int64(healthCheckNodePort) {
		t.Errorf("Unexpected port in healthcheck, expected %d, Got %d", healthCheckNodePort, hc.HttpHealthCheck.Port)
	}
}

type EnsureILBParams struct {
	clusterName     string
	clusterID       string
	service         *v1.Service
	existingFwdRule *composite.ForwardingRule
}

// newEnsureILBParams is the constructor of EnsureILBParams.
func newEnsureILBParams() *EnsureILBParams {
	vals := gce.DefaultTestClusterValues()
	return &EnsureILBParams{
		vals.ClusterName,
		vals.ClusterID,
		test.NewL4ILBService(false, 8080),
		nil,
	}
}

// TestEnsureInternalLoadBalancerErrors tests the function
// EnsureInternalLoadBalancer, making sure the system won't panic when
// exceptions raised by gce.
func TestEnsureInternalLoadBalancerErrors(t *testing.T) {
	vals := gce.DefaultTestClusterValues()
	var params *EnsureILBParams

	for desc, tc := range map[string]struct {
		adjustParams func(*EnsureILBParams)
		injectMock   func(*cloud.MockGCE)
	}{
		"EnsureInternalBackendService failed": {
			injectMock: func(c *cloud.MockGCE) {
				c.MockRegionBackendServices.GetHook = mock.GetRegionBackendServicesErrHook
			},
		},
		"Create internal health check failed": {
			injectMock: func(c *cloud.MockGCE) {
				c.MockHealthChecks.GetHook = mock.GetHealthChecksInternalErrHook
			},
		},
		"Create firewall failed": {
			injectMock: func(c *cloud.MockGCE) {
				c.MockFirewalls.InsertHook = mock.InsertFirewallsUnauthorizedErrHook
			},
		},
		"Create region forwarding rule failed": {
			injectMock: func(c *cloud.MockGCE) {
				c.MockForwardingRules.InsertHook = mock.InsertForwardingRulesInternalErrHook
			},
		},
		"Get region forwarding rule failed": {
			injectMock: func(c *cloud.MockGCE) {
				c.MockForwardingRules.GetHook = mock.GetForwardingRulesInternalErrHook
			},
		},
		"Delete region forwarding rule failed": {
			adjustParams: func(params *EnsureILBParams) {
				params.existingFwdRule = &composite.ForwardingRule{BackendService: "badBackendService"}
			},
			injectMock: func(c *cloud.MockGCE) {
				c.MockForwardingRules.DeleteHook = mock.DeleteForwardingRuleErrHook
			},
		},
	} {
		t.Run(desc, func(t *testing.T) {
			nodeNames := []string{"test-node-1"}
			params = newEnsureILBParams()
			if tc.adjustParams != nil {
				tc.adjustParams(params)
			}
			namer := namer_util.NewL4Namer(kubeSystemUID, nil)
			fakeGCE := getFakeGCECloud(gce.DefaultTestClusterValues())
			l4ilbParams := &L4ILBParams{
				Service:  params.service,
				Cloud:    fakeGCE,
				Namer:    namer,
				Recorder: record.NewFakeRecorder(100),
			}
			l4 := NewL4Handler(l4ilbParams)
			l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

			//lbName :=l4.namer.L4Backend(params.service.Namespace, params.service.Name)
			frName := l4.GetFRName()
			key, err := composite.CreateKey(l4.cloud, frName, meta.Regional)
			if err != nil {
				t.Errorf("Unexpected error when creating key - %v", err)
			}
			_, err = test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName)
			if err != nil {
				t.Errorf("Unexpected error when adding nodes %v", err)
			}
			// Create a dummy forwarding rule in order to trigger a delete in the EnsureInternalLoadBalancer function.
			if err = composite.CreateForwardingRule(l4.cloud, key, &composite.ForwardingRule{Name: frName}); err != nil {
				t.Errorf("Failed to create fake forwarding rule %s, err %v", frName, err)
			}
			// Inject error hooks after creating the forwarding rule.
			if tc.injectMock != nil {
				tc.injectMock(fakeGCE.Compute().(*cloud.MockGCE))
			}
			result := l4.EnsureInternalLoadBalancer(nodeNames, params.service)
			if result.Error == nil {
				t.Errorf("Expected error when %s", desc)
			}
			if result.Status != nil {
				t.Errorf("Expected empty status when %s, Got %v", desc, result.Status)
			}
		})
	}
}

/* TODO uncomment after https://github.com/kubernetes/kubernetes/pull/87667 is available in vendor
func TestEnsureLoadBalancerDeletedSucceedsOnXPN(t *testing.T) {
	t.Parallel()

	vals := gce.DefaultTestClusterValues()
	vals.OnXPN = true
	fakeGCE := gce.NewFakeGCECloud(vals)
	c := fakeGCE.Compute().(*cloud.MockGCE)
	svc := test.NewL4ILBService(false, 8080)
	nodeNames := []string{"test-node-1"}
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)
	recorder := record.NewFakeRecorder(100)
	l4 := NewL4Handler(svc, fakeGCE, meta.Regional, namer, recorder, &sync.Mutex{}))
	_, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName)
	if err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}
	fwName :=l4.namer.L4Backend(svc.Namespace, svc.Name)
	status, err :=l4.EnsureInternalLoadBalancer(nodeNames, svc, &metrics.L4ILBServiceState{})
	if err != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", err)
	}
	if len(status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, svc, l4, nodeNames)

	c.MockFirewalls.DeleteHook = mock.DeleteFirewallsUnauthorizedErrHook

	err =l4.EnsureInternalLoadBalancerDeleted(svc)
	if err != nil {
		t.Errorf("Failed to delete loadBalancer, err %v", err)
	}
	gcloudcmd := gce.FirewallToGCloudDeleteCmd(fwName, fakeGCE.ProjectID())
	XPNErrMsg := fmt.Sprintf("%s %s: `%v`", v1.EventTypeNormal, eventMsgFirewallChange, gcloudcmd)
	err = test.CheckEvent(recorder, XPNErrMsg, true)
	if err != nil {
		t.Errorf("Failed to check event, err %v", err)
	}
}
*/

func TestEnsureInternalLoadBalancerEnableGlobalAccess(t *testing.T) {
	t.Parallel()

	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	nodeNames := []string{"test-node-1"}
	svc := test.NewL4ILBService(false, 8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}
	frName := l4.GetFRName()
	result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)

	// Change service to include the global access annotation
	svc.Annotations[gce.ServiceAnnotationILBAllowGlobalAccess] = "true"
	result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)
	descString, err := utils.MakeL4LBServiceDescription(utils.ServiceKeyFunc(svc.Namespace, svc.Name), "1.2.3.0", meta.VersionGA, false, utils.ILB)
	if err != nil {
		t.Errorf("Unexpected error when creating description - %v", err)
	}
	key, err := composite.CreateKey(l4.cloud, frName, meta.Regional)
	if err != nil {
		t.Errorf("Unexpected error when creating key - %v", err)
	}
	fwdRule, err := composite.GetForwardingRule(l4.cloud, key, meta.VersionGA)
	if err != nil {
		t.Errorf("Unexpected error when looking up forwarding rule - %v", err)
	}
	if !fwdRule.AllowGlobalAccess {
		t.Errorf("Unexpected false value for AllowGlobalAccess")
	}
	if fwdRule.Description != descString {
		t.Errorf("Expected description %s, Got %s", descString, fwdRule.Description)
	}
	// remove the annotation and disable global access.
	delete(svc.Annotations, gce.ServiceAnnotationILBAllowGlobalAccess)
	result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	// make sure GlobalAccess field is off.
	fwdRule, err = composite.GetForwardingRule(l4.cloud, key, meta.VersionGA)
	if err != nil {
		t.Errorf("Unexpected error when looking up forwarding rule - %v", err)
	}

	if fwdRule.AllowGlobalAccess {
		t.Errorf("Unexpected true value for AllowGlobalAccess")
	}
	if fwdRule.Description != descString {
		t.Errorf("Expected description %s, Got %s", descString, fwdRule.Description)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)
	// Delete the service
	result = l4.EnsureInternalLoadBalancerDeleted(svc)
	if result.Error != nil {
		t.Errorf("Unexpected error %v", err)
	}
	assertILBResourcesDeleted(t, l4)
}

func TestEnsureInternalLoadBalancerCustomSubnet(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}
	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	svc := test.NewL4ILBService(false, 8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}
	result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)

	frName := l4.GetFRName()
	fwdRule, err := composite.GetForwardingRule(l4.cloud, meta.RegionalKey(frName, l4.cloud.Region()), meta.VersionGA)
	if err != nil || fwdRule == nil {
		t.Errorf("Unexpected error looking up forwarding rule - err %v", err)
	}
	if fwdRule.Subnetwork != "" {
		t.Errorf("Unexpected subnet value %s in ILB ForwardingRule", fwdRule.Subnetwork)
	}

	// Change service to include the global access annotation and request static ip
	requestedIP := "4.5.6.7"
	svc.Annotations[gce.ServiceAnnotationILBSubnet] = "test-subnet"
	svc.Spec.LoadBalancerIP = requestedIP
	result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if err != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", err)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)
	if result.Status.Ingress[0].IP != requestedIP {
		t.Fatalf("Reserved IP %s not propagated, Got '%s'", requestedIP, result.Status.Ingress[0].IP)
	}
	fwdRule, err = composite.GetForwardingRule(l4.cloud, meta.RegionalKey(frName, l4.cloud.Region()), meta.VersionGA)
	if err != nil || fwdRule == nil {
		t.Errorf("Unexpected error looking up forwarding rule - err %v", err)
	}
	if !strings.HasSuffix(fwdRule.Subnetwork, "test-subnet") {
		t.Errorf("Unexpected subnet value '%s' in ILB ForwardingRule, expected 'test-subnet'", fwdRule.Subnetwork)
	}

	// Change to a different subnet
	svc.Annotations[gce.ServiceAnnotationILBSubnet] = "another-subnet"
	result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)
	if result.Status.Ingress[0].IP != requestedIP {
		t.Errorf("Reserved IP %s not propagated, Got %s", requestedIP, result.Status.Ingress[0].IP)
	}
	fwdRule, err = composite.GetForwardingRule(l4.cloud, meta.RegionalKey(frName, l4.cloud.Region()), meta.VersionGA)
	if err != nil || fwdRule == nil {
		t.Errorf("Unexpected error looking up forwarding rule - err %v", err)
	}
	if !strings.HasSuffix(fwdRule.Subnetwork, "another-subnet") {
		t.Errorf("Unexpected subnet value' %s' in ILB ForwardingRule, expected 'another-subnet'", fwdRule.Subnetwork)
	}
	// remove the annotation - ILB should revert to default subnet.
	delete(svc.Annotations, gce.ServiceAnnotationILBSubnet)
	result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	fwdRule, err = composite.GetForwardingRule(l4.cloud, meta.RegionalKey(frName, l4.cloud.Region()), meta.VersionGA)
	if err != nil || fwdRule == nil {
		t.Errorf("Unexpected error %v", err)
	}
	if fwdRule.Subnetwork != "" {
		t.Errorf("Unexpected subnet value '%s' in ILB ForwardingRule.", fwdRule.Subnetwork)
	}
	// Delete the loadbalancer
	result = l4.EnsureInternalLoadBalancerDeleted(svc)
	if result.Error != nil {
		t.Errorf("Unexpected error deleting loadbalancer - err %v", result.Error)
	}
	assertILBResourcesDeleted(t, l4)
}

func TestEnsureInternalFirewallPortRanges(t *testing.T) {
	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	svc := test.NewL4ILBService(false, 8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	fwName := l4.namer.L4Backend(l4.Service.Namespace, l4.Service.Name)
	tc := struct {
		Input  []int
		Result []string
	}{
		Input: []int{15, 37, 900, 2002, 2003, 2003, 2004, 2004}, Result: []string{"15", "37", "900", "2002-2004"},
	}
	c := fakeGCE.Compute().(*cloud.MockGCE)
	c.MockFirewalls.InsertHook = nil
	c.MockFirewalls.PatchHook = nil

	nodeNames := []string{"test-node-1"}
	_, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName)
	if err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}
	c.MockFirewalls.InsertHook = nil
	c.MockFirewalls.PatchHook = nil

	fwrParams := firewalls.FirewallParams{
		Name:              fwName,
		SourceRanges:      []string{"10.0.0.0/20"},
		DestinationRanges: []string{"20.0.0.0/20"},
		PortRanges:        utils.GetPortRanges(tc.Input),
		NodeNames:         nodeNames,
		Protocol:          string(v1.ProtocolTCP),
		IP:                "1.2.3.4",
	}
	err = firewalls.EnsureL4FirewallRule(l4.cloud, utils.ServiceKeyFunc(svc.Namespace, svc.Name), &fwrParams /*sharedRule = */, false)
	if err != nil {
		t.Errorf("Unexpected error %v when ensuring firewall rule %s for svc %+v", err, fwName, svc)
	}
	existingFirewall, err := l4.cloud.GetFirewall(fwName)
	if err != nil || existingFirewall == nil || len(existingFirewall.Allowed) == 0 {
		t.Errorf("Unexpected error %v when looking up firewall %s, Got firewall %+v", err, fwName, existingFirewall)
	}
	existingPorts := existingFirewall.Allowed[0].Ports
	if !reflect.DeepEqual(existingPorts, tc.Result) {
		t.Errorf("Expected firewall rule with ports %v,got %v", tc.Result, existingPorts)
	}
}

func TestEnsureInternalLoadBalancerModifyProtocol(t *testing.T) {
	t.Parallel()

	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	c := fakeGCE.Compute().(*cloud.MockGCE)
	nodeNames := []string{"test-node-1"}
	svc := test.NewL4ILBService(false, 8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	_, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName)
	if err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}
	// This function simulates the error where backend service protocol cannot be changed
	// before deleting the forwarding rule.
	c.MockRegionBackendServices.UpdateHook = func(ctx context.Context, key *meta.Key, be *compute.BackendService, m *cloud.MockRegionBackendServices) error {
		// Check FRnames with both protocols to make sure there is no leak or incorrect update.
		frNames := []string{l4.getFRNameWithProtocol("TCP"), l4.getFRNameWithProtocol("UDP")}
		for _, name := range frNames {
			key, err := composite.CreateKey(l4.cloud, name, meta.Regional)
			if err != nil {
				return fmt.Errorf("unexpected error when creating key - %v", err)
			}
			fr, err := c.MockForwardingRules.Get(ctx, key)
			if utils.IgnoreHTTPNotFound(err) != nil {
				return err
			}
			if fr != nil && fr.IPProtocol != be.Protocol {
				return fmt.Errorf("protocol mismatch between Forwarding Rule value %q and Backend service value %q", fr.IPProtocol, be.Protocol)
			}

		}
		return mock.UpdateRegionBackendServiceHook(ctx, key, be, m)
	}
	// Before deleting forwarding rule, check, that the address was reserved
	c.MockForwardingRules.DeleteHook = func(ctx context.Context, key *meta.Key, m *cloud.MockForwardingRules) (bool, error) {
		fr, err := c.MockForwardingRules.Get(ctx, key)
		// if forwarding rule not exists, don't need to check if address reserved
		if utils.IsNotFoundError(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}

		addr, err := l4.cloud.GetRegionAddressByIP(fr.Region, fr.IPAddress)
		if utils.IgnoreHTTPNotFound(err) != nil {
			return true, err
		}
		if addr == nil || utils.IsNotFoundError(err) {
			t.Errorf("Address not reserved before deleting forwarding rule +%v", fr)
		}

		return false, nil
	}

	frName := l4.getFRNameWithProtocol("TCP")
	result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)
	key, err := composite.CreateKey(l4.cloud, frName, meta.Regional)
	if err != nil {
		t.Errorf("Unexpected error when creating key - %v", err)
	}
	fwdRule, err := composite.GetForwardingRule(l4.cloud, key, meta.VersionGA)
	if err != nil {
		t.Errorf("Unexpected error when looking up forwarding rule - %v", err)
	}
	if fwdRule.IPProtocol != "TCP" {
		t.Errorf("Unexpected protocol value %s, expected TCP", fwdRule.IPProtocol)
	}
	// change the protocol to UDP
	svc.Spec.Ports[0].Protocol = v1.ProtocolUDP
	result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)
	// Make sure the old forwarding rule is deleted
	fwdRule, err = composite.GetForwardingRule(l4.cloud, key, meta.VersionGA)
	if !utils.IsNotFoundError(err) {
		t.Errorf("Failed to delete ForwardingRule %s", frName)
	}
	frName = l4.getFRNameWithProtocol("UDP")
	if key, err = composite.CreateKey(l4.cloud, frName, meta.Regional); err != nil {
		t.Errorf("Unexpected error when creating key - %v", err)
	}
	if fwdRule, err = composite.GetForwardingRule(l4.cloud, key, meta.VersionGA); err != nil {
		t.Errorf("Unexpected error when looking up forwarding rule - %v", err)
	}
	if fwdRule.IPProtocol != "UDP" {
		t.Errorf("Unexpected protocol value %s, expected UDP", fwdRule.IPProtocol)
	}

	// on final deletion of Load Balancer we don't need to check if address was reserved (which was happening in the hook)
	c.MockForwardingRules.DeleteHook = nil
	// Delete the service
	result = l4.EnsureInternalLoadBalancerDeleted(svc)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	assertILBResourcesDeleted(t, l4)
}

func TestDualStackInternalLoadBalancerModifyProtocol(t *testing.T) {
	t.Parallel()

	vals := gce.DefaultTestClusterValues()
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)
	nodeNames := []string{"test-node-1"}

	testCases := []struct {
		desc       string
		ipFamilies []v1.IPFamily
	}{
		{
			desc:       "Test ipv4 ipv6 service protocol change",
			ipFamilies: []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
		},
		{
			desc:       "Test ipv4 ipv6 local service protocol change",
			ipFamilies: []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
		},
		{
			desc:       "Test ipv6 ipv4 service protocol change",
			ipFamilies: []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol},
		},
		{
			desc:       "Test ipv4 service protocol change",
			ipFamilies: []v1.IPFamily{v1.IPv4Protocol},
		},
		{
			desc:       "Test ipv6 service protocol change",
			ipFamilies: []v1.IPFamily{v1.IPv6Protocol},
		},
	}
	for _, tc := range testCases {
		tc := tc

		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			fakeGCE := getFakeGCECloud(vals)

			svc := test.NewL4ILBDualStackService(8080, v1.ProtocolTCP, tc.ipFamilies, v1.ServiceExternalTrafficPolicyTypeCluster)

			l4ilbParams := &L4ILBParams{
				Service:          svc,
				Cloud:            fakeGCE,
				Namer:            namer,
				Recorder:         record.NewFakeRecorder(100),
				DualStackEnabled: true,
			}
			l4 := NewL4Handler(l4ilbParams)
			l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

			// This function simulates the error where backend service protocol cannot be changed
			// before deleting the forwarding rule.
			c := fakeGCE.Compute().(*cloud.MockGCE)
			c.MockRegionBackendServices.UpdateHook = func(ctx context.Context, key *meta.Key, bs *compute.BackendService, m *cloud.MockRegionBackendServices) error {
				// Check FR names with both protocols to make sure there is no leak or incorrect update.
				frNames := []string{l4.getFRNameWithProtocol("TCP"), l4.getFRNameWithProtocol("UDP"), l4.getIPv6FRNameWithProtocol("TCP"), l4.getIPv6FRNameWithProtocol("UDP")}
				for _, name := range frNames {
					key, err := composite.CreateKey(fakeGCE, name, meta.Regional)
					if err != nil {
						return fmt.Errorf("unexpected error when creating key - %v", err)
					}
					fr, err := c.MockForwardingRules.Get(ctx, key)
					if utils.IgnoreHTTPNotFound(err) != nil {
						return err
					}
					if fr != nil && fr.IPProtocol != bs.Protocol {
						return fmt.Errorf("protocol mismatch between Forwarding Rule value %q and Backend service value %q", fr.IPProtocol, bs.Protocol)
					}

				}
				return mock.UpdateRegionBackendServiceHook(ctx, key, bs, m)
			}
			// Before deleting forwarding rule, check, that the address was reserved
			c.MockForwardingRules.DeleteHook = func(ctx context.Context, key *meta.Key, m *cloud.MockForwardingRules) (bool, error) {
				fr, err := c.MockForwardingRules.Get(ctx, key)
				// if forwarding rule not exists, don't need to check if address reserved
				if utils.IsNotFoundError(err) {
					return false, nil
				}
				if err != nil {
					return false, err
				}
				// we don't reserve addresses for IPv6 forwarding rules
				if fr.IpVersion == "IPV6" {
					return false, nil
				}

				addr, err := l4.cloud.GetRegionAddressByIP(fr.Region, fr.IPAddress)
				if utils.IgnoreHTTPNotFound(err) != nil {
					return true, err
				}
				if addr == nil || utils.IsNotFoundError(err) {
					t.Errorf("Address not reserved before deleting forwarding rule +%v", fr)
				}

				return false, nil
			}

			if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName); err != nil {
				t.Errorf("Unexpected error when adding nodes %v", err)
			}

			result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
			if result.Error != nil {
				t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
			}
			l4.Service.Annotations = result.Annotations
			assertDualStackILBResources(t, l4, nodeNames)

			// Change Protocol and trigger sync
			svc.Spec.Ports[0].Protocol = v1.ProtocolUDP
			result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
			if result.Error != nil {
				t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
			}
			l4.Service.Annotations = result.Annotations
			assertDualStackILBResources(t, l4, nodeNames)

			c.MockForwardingRules.DeleteHook = nil
			l4.EnsureInternalLoadBalancerDeleted(l4.Service)
			assertDualStackILBResourcesDeleted(t, l4)
		})
	}
}

func TestDualStackInternalLoadBalancerModifyPorts(t *testing.T) {
	t.Parallel()

	vals := gce.DefaultTestClusterValues()
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)
	nodeNames := []string{"test-node-1"}

	testCases := []struct {
		desc       string
		ipFamilies []v1.IPFamily
	}{
		{
			desc:       "Test ipv4 ipv6 service port change",
			ipFamilies: []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
		},
		{
			desc:       "Test ipv4 ipv6 local service port change",
			ipFamilies: []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
		},
		{
			desc:       "Test ipv6 ipv4 service port change",
			ipFamilies: []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol},
		},
		{
			desc:       "Test ipv4 service port change",
			ipFamilies: []v1.IPFamily{v1.IPv4Protocol},
		},
		{
			desc:       "Test ipv6 service port change",
			ipFamilies: []v1.IPFamily{v1.IPv6Protocol},
		},
	}
	for _, tc := range testCases {
		tc := tc

		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			fakeGCE := getFakeGCECloud(vals)

			svc := test.NewL4ILBDualStackService(8080, v1.ProtocolTCP, tc.ipFamilies, v1.ServiceExternalTrafficPolicyTypeCluster)

			l4ilbParams := &L4ILBParams{
				Service:          svc,
				Cloud:            fakeGCE,
				Namer:            namer,
				Recorder:         record.NewFakeRecorder(100),
				DualStackEnabled: true,
			}
			l4 := NewL4Handler(l4ilbParams)
			l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

			if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName); err != nil {
				t.Errorf("Unexpected error when adding nodes %v", err)
			}

			result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
			if result.Error != nil {
				t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
			}
			l4.Service.Annotations = result.Annotations
			assertDualStackILBResources(t, l4, nodeNames)

			// Change Protocol and trigger sync
			svc.Spec.Ports[0].Port = 80
			result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
			if result.Error != nil {
				t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
			}
			l4.Service.Annotations = result.Annotations
			assertDualStackILBResources(t, l4, nodeNames)

			l4.EnsureInternalLoadBalancerDeleted(l4.Service)
			assertDualStackILBResourcesDeleted(t, l4)
		})
	}
}

func TestEnsureInternalLoadBalancerAllPorts(t *testing.T) {
	t.Parallel()

	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	nodeNames := []string{"test-node-1"}
	svc := test.NewL4ILBService(false, 8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}
	result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)
	frName := l4.getFRNameWithProtocol("TCP")
	key, err := composite.CreateKey(l4.cloud, frName, meta.Regional)
	if err != nil {
		t.Errorf("Unexpected error when creating key - %v", err)
	}
	fwdRule, err := composite.GetForwardingRule(l4.cloud, key, meta.VersionGA)
	if err != nil {
		t.Errorf("Unexpected error when looking up forwarding rule - %v", err)
	}
	if fwdRule.Ports[0] != "8080" {
		t.Errorf("Unexpected port value %v, expected '8080'", fwdRule.Ports)
	}
	// Add more than 5 ports to service spec
	svc.Spec.Ports = []v1.ServicePort{
		{Name: "testport", Port: int32(8080), Protocol: "TCP"},
		{Name: "testport", Port: int32(8090), Protocol: "TCP"},
		{Name: "testport", Port: int32(8100), Protocol: "TCP"},
		{Name: "testport", Port: int32(8200), Protocol: "TCP"},
		{Name: "testport", Port: int32(8300), Protocol: "TCP"},
		{Name: "testport", Port: int32(8400), Protocol: "TCP"},
	}
	result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)
	fwdRule, err = composite.GetForwardingRule(l4.cloud, key, meta.VersionGA)
	if err != nil {
		t.Errorf("Unexpected error when looking up forwarding rule - %v", err)
	}
	if !fwdRule.AllPorts {
		t.Errorf("Expected AllPorts field to be set in forwarding rule - %+v", fwdRule)
	}
	if len(fwdRule.Ports) != 0 {
		t.Errorf("Unexpected port value %v, expected empty list", fwdRule.Ports)
	}
	// Modify the service to use less than 5 ports
	svc.Spec.Ports = []v1.ServicePort{
		{Name: "testport", Port: int32(8090), Protocol: "TCP"},
		{Name: "testport", Port: int32(8100), Protocol: "TCP"},
		{Name: "testport", Port: int32(8300), Protocol: "TCP"},
		{Name: "testport", Port: int32(8400), Protocol: "TCP"},
	}
	expectPorts := []string{"8090", "8100", "8300", "8400"}
	result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4)
	}
	assertILBResources(t, l4, nodeNames, result.Annotations)
	fwdRule, err = composite.GetForwardingRule(l4.cloud, key, meta.VersionGA)
	if err != nil {
		t.Errorf("Unexpected error when looking up forwarding rule - %v", err)
	}
	if !utils.EqualStringSets(fwdRule.Ports, expectPorts) {
		t.Errorf("Unexpected port value %v, expected %v", fwdRule.Ports, expectPorts)
	}
	if fwdRule.AllPorts {
		t.Errorf("Expected AllPorts field to be unset in forwarding rule - %+v", fwdRule)
	}
	// Delete the service
	result = l4.EnsureInternalLoadBalancerDeleted(svc)
	if result.Error != nil {
		t.Errorf("Unexpected error %v", result.Error)
	}
	assertILBResourcesDeleted(t, l4)
}

func TestEnsureInternalDualStackLoadBalancer(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}
	vals := gce.DefaultTestClusterValues()

	namer := namer_util.NewL4Namer(kubeSystemUID, nil)

	testCases := []struct {
		desc          string
		ipFamilies    []v1.IPFamily
		trafficPolicy v1.ServiceExternalTrafficPolicyType
	}{
		{
			desc:          "Test ipv4 ipv6 service",
			ipFamilies:    []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
			trafficPolicy: v1.ServiceExternalTrafficPolicyTypeCluster,
		},
		{
			desc:          "Test ipv4 ipv6 local service",
			ipFamilies:    []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
			trafficPolicy: v1.ServiceExternalTrafficPolicyTypeLocal,
		},
		{
			desc:          "Test ipv6 ipv4 service",
			ipFamilies:    []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol},
			trafficPolicy: v1.ServiceExternalTrafficPolicyTypeCluster,
		},
		{
			desc:          "Test ipv4 service",
			ipFamilies:    []v1.IPFamily{v1.IPv4Protocol},
			trafficPolicy: v1.ServiceExternalTrafficPolicyTypeCluster,
		},
		{
			desc:          "Test ipv6 service",
			ipFamilies:    []v1.IPFamily{v1.IPv6Protocol},
			trafficPolicy: v1.ServiceExternalTrafficPolicyTypeCluster,
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			fakeGCE := getFakeGCECloud(vals)

			svc := test.NewL4ILBDualStackService(8080, v1.ProtocolTCP, tc.ipFamilies, tc.trafficPolicy)

			l4ilbParams := &L4ILBParams{
				Service:          svc,
				Cloud:            fakeGCE,
				Namer:            namer,
				Recorder:         record.NewFakeRecorder(100),
				DualStackEnabled: true,
			}
			l4 := NewL4Handler(l4ilbParams)
			l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

			if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName); err != nil {
				t.Errorf("Unexpected error when adding nodes %v", err)
			}

			result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
			if result.Error != nil {
				t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
			}
			if len(result.Status.Ingress) == 0 {
				t.Errorf("Got empty loadBalancer status using handler %v", l4)
			}
			l4.Service.Annotations = result.Annotations
			assertDualStackILBResources(t, l4, nodeNames)

			l4.EnsureInternalLoadBalancerDeleted(l4.Service)
			assertDualStackILBResourcesDeleted(t, l4)
		})
	}
}

// This is exhaustive test that checks for all possible transitions of
// - ServiceExternalTrafficPolicy
// - Protocol
// - IPFamilies
// for dual-stack service. In total 400 combinations
func TestDualStackILBTransitions(t *testing.T) {
	t.Parallel()

	trafficPolicyStates := []v1.ServiceExternalTrafficPolicyType{v1.ServiceExternalTrafficPolicyTypeLocal, v1.ServiceExternalTrafficPolicyTypeCluster}
	protocols := []v1.Protocol{v1.ProtocolTCP, v1.ProtocolUDP}
	ipFamiliesStates := [][]v1.IPFamily{
		{v1.IPv4Protocol},
		{v1.IPv4Protocol, v1.IPv6Protocol},
		{v1.IPv6Protocol},
		{v1.IPv6Protocol, v1.IPv4Protocol},
		{},
	}

	type testCase struct {
		desc                 string
		initialIPFamily      []v1.IPFamily
		finalIPFamily        []v1.IPFamily
		initialTrafficPolicy v1.ServiceExternalTrafficPolicyType
		finalTrafficPolicy   v1.ServiceExternalTrafficPolicyType
		initialProtocol      v1.Protocol
		finalProtocol        v1.Protocol
	}

	var testCases []testCase

	for _, initialIPFamily := range ipFamiliesStates {
		for _, finalIPFamily := range ipFamiliesStates {
			for _, initialTrafficPolicy := range trafficPolicyStates {
				for _, finalTrafficPolicy := range trafficPolicyStates {
					for _, initialProtocol := range protocols {
						for _, finalProtocol := range protocols {
							testCases = append(testCases, testCase{
								desc:                 dualStackILBTransitionTestDesc(initialIPFamily, finalIPFamily, initialTrafficPolicy, finalTrafficPolicy, initialProtocol, finalProtocol),
								initialIPFamily:      initialIPFamily,
								finalIPFamily:        finalIPFamily,
								initialTrafficPolicy: initialTrafficPolicy,
								finalTrafficPolicy:   finalTrafficPolicy,
								initialProtocol:      initialProtocol,
								finalProtocol:        finalProtocol,
							})
						}
					}
				}
			}
		}
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			nodeNames := []string{"test-node-1"}
			vals := gce.DefaultTestClusterValues()

			namer := namer_util.NewL4Namer(kubeSystemUID, nil)
			fakeGCE := getFakeGCECloud(vals)

			svc := test.NewL4ILBDualStackService(8080, tc.initialProtocol, tc.initialIPFamily, tc.initialTrafficPolicy)
			l4ilbParams := &L4ILBParams{
				Service:          svc,
				Cloud:            fakeGCE,
				Namer:            namer,
				Recorder:         record.NewFakeRecorder(100),
				DualStackEnabled: true,
			}
			l4 := NewL4Handler(l4ilbParams)
			l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

			if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName); err != nil {
				t.Errorf("Unexpected error when adding nodes %v", err)
			}

			result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
			svc.Annotations = result.Annotations
			assertDualStackILBResources(t, l4, nodeNames)

			finalSvc := test.NewL4ILBDualStackService(8080, tc.finalProtocol, tc.finalIPFamily, tc.finalTrafficPolicy)
			finalSvc.Annotations = svc.Annotations
			l4.Service = finalSvc

			result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
			finalSvc.Annotations = result.Annotations
			assertDualStackILBResources(t, l4, nodeNames)

			l4.EnsureInternalLoadBalancerDeleted(l4.Service)
			assertDualStackILBResourcesDeleted(t, l4)
		})
	}
}

func dualStackILBTransitionTestDesc(initialIPFamily []v1.IPFamily, finalIPFamily []v1.IPFamily, initialTrafficPolicy v1.ServiceExternalTrafficPolicyType, finalTrafficPolicy v1.ServiceExternalTrafficPolicyType, initialProtocol v1.Protocol, finalProtocol v1.Protocol) string {
	var stringInitialIPFamily []string
	for _, f := range initialIPFamily {
		stringInitialIPFamily = append(stringInitialIPFamily, string(f))
	}

	var stringFinalIPFamily []string
	for _, f := range finalIPFamily {
		stringFinalIPFamily = append(stringFinalIPFamily, string(f))
	}
	fromIPFamily := strings.Join(stringInitialIPFamily, ",")
	toIPFamily := strings.Join(stringFinalIPFamily, ",")
	fromTrafficPolicy := string(initialTrafficPolicy)
	toTrafficPolicy := string(finalTrafficPolicy)
	fromProtocol := string(initialProtocol)
	toProtocol := string(finalProtocol)

	return fmt.Sprintf("IP family: %s->%s, Traffic Policy: %s->%s, Protocol: %s->%s,", fromIPFamily, toIPFamily, fromTrafficPolicy, toTrafficPolicy, fromProtocol, toProtocol)
}

func TestDualStackILBSyncIgnoresNoAnnotationIPv6Resources(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}
	vals := gce.DefaultTestClusterValues()

	namer := namer_util.NewL4Namer(kubeSystemUID, nil)
	fakeGCE := getFakeGCECloud(vals)

	svc := test.NewL4ILBService(false, 8080)
	l4ilbParams := &L4ILBParams{
		Service:          svc,
		Cloud:            fakeGCE,
		Namer:            namer,
		Recorder:         record.NewFakeRecorder(100),
		DualStackEnabled: true,
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}

	svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol}
	result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	svc.Annotations = result.Annotations
	assertDualStackILBResources(t, l4, nodeNames)

	// Delete resources annotation
	annotationsToDelete := []string{annotations.TCPForwardingRuleIPv6Key, annotations.FirewallRuleIPv6Key, annotations.FirewallRuleForHealthcheckIPv6Key}
	for _, annotationToDelete := range annotationsToDelete {
		delete(svc.Annotations, annotationToDelete)
	}
	svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv4Protocol}

	// Run new sync. Controller should not delete resources, if they don't exist in annotation
	result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
	svc.Annotations = result.Annotations

	ipv6FWName := l4.namer.L4IPv6Firewall(l4.Service.Namespace, l4.Service.Name)
	err := verifyFirewallNotExists(l4.cloud, ipv6FWName)
	if err == nil {
		t.Errorf("firewall rule %s was deleted, expected not", ipv6FWName)
	}

	// Verify IPv6 Forwarding Rule was not deleted
	ipv6FRName := l4.getIPv6FRName()
	err = verifyForwardingRuleNotExists(l4.cloud, ipv6FRName)
	if err == nil {
		t.Errorf("forwarding rule %s was deleted, expected not", ipv6FRName)
	}

	l4.EnsureInternalLoadBalancerDeleted(l4.Service)
	// After complete deletion, IPv6 and IPv4 resources should be cleaned up, even if the were leaked
	assertDualStackILBResourcesDeleted(t, l4)
}

func TestDualStackILBSyncIgnoresNoAnnotationIPv4Resources(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}
	vals := gce.DefaultTestClusterValues()

	namer := namer_util.NewL4Namer(kubeSystemUID, nil)
	fakeGCE := getFakeGCECloud(vals)

	svc := test.NewL4ILBService(false, 8080)
	l4ilbParams := &L4ILBParams{
		Service:          svc,
		Cloud:            fakeGCE,
		Namer:            namer,
		Recorder:         record.NewFakeRecorder(100),
		DualStackEnabled: true,
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ilbParams.Recorder)

	if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}

	svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol}
	result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
	svc.Annotations = result.Annotations
	assertDualStackILBResources(t, l4, nodeNames)

	// Delete resources annotation
	annotationsToDelete := []string{annotations.TCPForwardingRuleKey, annotations.FirewallRuleKey, annotations.FirewallRuleForHealthcheckKey}
	for _, annotationToDelete := range annotationsToDelete {
		delete(svc.Annotations, annotationToDelete)
	}
	svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv6Protocol}

	// Run new sync. Controller should not delete resources, if they don't exist in annotation
	result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
	svc.Annotations = result.Annotations

	// Verify IPv6 Firewall was not deleted
	backendServiceName := l4.namer.L4Backend(l4.Service.Namespace, l4.Service.Name)
	err := verifyFirewallNotExists(l4.cloud, backendServiceName)
	if err == nil {
		t.Errorf("firewall rule %s was deleted, expected not", backendServiceName)
	}

	// Verify IPv6 Forwarding Rule was not deleted
	ipv4FRName := l4.GetFRName()
	err = verifyForwardingRuleNotExists(l4.cloud, ipv4FRName)
	if err == nil {
		t.Errorf("forwarding rule %s was deleted, expected not", ipv4FRName)
	}

	l4.EnsureInternalLoadBalancerDeleted(l4.Service)
	// After complete deletion, IPv6 and IPv4 resources should be cleaned up, even if the were leaked
	assertDualStackILBResourcesDeleted(t, l4)
}

func assertILBResources(t *testing.T, l4 *L4, nodeNames []string, resourceAnnotations map[string]string) {
	t.Helper()

	err := verifyILBIPv4NodesFirewall(l4, nodeNames)
	if err != nil {
		t.Errorf("verifyILBIPv4NodesFirewall(_, %v) returned error %v, want nil", nodeNames, err)
	}

	err = verifyILBIPv4HealthCheckFirewall(l4, nodeNames)
	if err != nil {
		t.Errorf("verifyILBIPv4HealthCheckFirewall(_, %v) returned error %v, want nil", nodeNames, err)
	}

	healthCheck, err := getAndVerifyILBHealthCheck(l4)
	if err != nil {
		t.Errorf("getAndVerifyILBHealthCheck(_) returned error %v, want nil", err)
	}

	backendService, err := getAndVerifyILBBackendService(l4, healthCheck)
	if err != nil {
		t.Errorf("getAndVerifyILBBackendService(_, %v) returned error %v, want nil", healthCheck, err)
	}

	err = verifyILBIPv4ForwardingRule(l4, backendService.SelfLink)
	if err != nil {
		t.Errorf("getAndVerifyILBForwardingRule(_, %s) returned error %v, want nil", backendService.SelfLink, err)
	}

	expectedAnnotations := buildExpectedAnnotations(l4)
	if !reflect.DeepEqual(expectedAnnotations, resourceAnnotations) {
		t.Fatalf("Expected annotations %v, got %v", expectedAnnotations, resourceAnnotations)
	}
}

func assertDualStackILBResources(t *testing.T, l4 *L4, nodeNames []string) {
	t.Helper()

	healthCheck, err := getAndVerifyILBHealthCheck(l4)
	if err != nil {
		t.Errorf("getAndVerifyHealthCheck(_) returned error %v, want nil", err)
	}

	backendService, err := getAndVerifyILBBackendService(l4, healthCheck)
	if err != nil {
		t.Errorf("getAndVerifyBackendService(_, %v) returned error %v, want nil", healthCheck, err)
	}

	if utils.NeedsIPv4(l4.Service) {
		err = verifyILBIPv4ForwardingRule(l4, backendService.SelfLink)
		if err != nil {
			t.Errorf("verifyILBIPv4ForwardingRule(_, %s) returned error %v, want nil", backendService.SelfLink, err)
		}

		err = verifyILBIPv4NodesFirewall(l4, nodeNames)
		if err != nil {
			t.Errorf("verifyILBIPv4NodesFirewall(_, %s) returned error %v, want nil", nodeNames, err)
		}

		err = verifyILBIPv4HealthCheckFirewall(l4, nodeNames)
		if err != nil {
			t.Errorf("verifyILBIPv4HealthCheckFirewall(_, %s) returned error %v, want nil", nodeNames, err)
		}
	} else {
		err = verifyILBIPv4ResourcesDeletedOnSync(l4)
		if err != nil {
			t.Errorf("verifyILBIPv4ResourcesDeletedOnSync(_) returned error %v, want nil", err)
		}
	}
	if utils.NeedsIPv6(l4.Service) {
		err = verifyILBIPv6ForwardingRule(l4, backendService.SelfLink)
		if err != nil {
			t.Errorf("verifyILBIPv6ForwardingRule(_, %s) returned error %v, want nil", backendService.SelfLink, err)
		}

		err = verifyILBIPv6NodesFirewall(l4, nodeNames)
		if err != nil {
			t.Errorf("verifyILBIPv6NodesFirewall(_, %v) returned error %v, want nil", nodeNames, err)
		}

		err = verifyILBIPv6HealthCheckFirewall(l4, nodeNames)
		if err != nil {
			t.Errorf("verifyILBIPv6HealthCheckFirewall(_, %v) returned error %v, want nil", nodeNames, err)
		}
	} else {
		err = verifyILBIPv6ResourcesDeletedOnSync(l4)
		if err != nil {
			t.Errorf("verifyILBIPv6ResourcesDeletedOnSync(_) returned error %v, want nil", err)
		}
	}

	expectedAnnotations := buildExpectedAnnotations(l4)
	if !reflect.DeepEqual(expectedAnnotations, l4.Service.Annotations) {
		diff := cmp.Diff(expectedAnnotations, l4.Service.Annotations)
		t.Errorf("Expected annotations %v, got %v, diff %v", expectedAnnotations, l4.Service.Annotations, diff)
	}
}

func buildExpectedAnnotations(l4 *L4) map[string]string {
	isSharedHC := !servicehelper.RequestsOnlyLocalTraffic(l4.Service)
	proto := utils.GetProtocol(l4.Service.Spec.Ports)

	backendName := l4.namer.L4Backend(l4.Service.Namespace, l4.Service.Name)
	hcName := l4.namer.L4HealthCheck(l4.Service.Namespace, l4.Service.Name, isSharedHC)

	expectedAnnotations := map[string]string{
		annotations.BackendServiceKey: backendName,
		annotations.HealthcheckKey:    hcName,
	}

	if utils.NeedsIPv4(l4.Service) {
		hcFwName := l4.namer.L4HealthCheckFirewall(l4.Service.Namespace, l4.Service.Name, isSharedHC)

		expectedAnnotations[annotations.FirewallRuleForHealthcheckKey] = hcFwName
		expectedAnnotations[annotations.FirewallRuleKey] = backendName

		ipv4FRName := l4.GetFRName()
		if proto == v1.ProtocolTCP {
			expectedAnnotations[annotations.TCPForwardingRuleKey] = ipv4FRName
		} else {
			expectedAnnotations[annotations.UDPForwardingRuleKey] = ipv4FRName
		}
	}
	if utils.NeedsIPv6(l4.Service) {
		ipv6hcFwName := l4.namer.L4IPv6HealthCheckFirewall(l4.Service.Namespace, l4.Service.Name, isSharedHC)
		ipv6FirewallName := l4.namer.L4IPv6Firewall(l4.Service.Namespace, l4.Service.Name)

		expectedAnnotations[annotations.FirewallRuleForHealthcheckIPv6Key] = ipv6hcFwName
		expectedAnnotations[annotations.FirewallRuleIPv6Key] = ipv6FirewallName

		ipv6FRName := l4.getIPv6FRName()
		if proto == v1.ProtocolTCP {
			expectedAnnotations[annotations.TCPForwardingRuleIPv6Key] = ipv6FRName
		} else {
			expectedAnnotations[annotations.UDPForwardingRuleIPv6Key] = ipv6FRName
		}
	}
	return expectedAnnotations
}

func getAndVerifyILBHealthCheck(l4 *L4) (*composite.HealthCheck, error) {
	isSharedHC := !servicehelper.RequestsOnlyLocalTraffic(l4.Service)
	hcName := l4.namer.L4HealthCheck(l4.Service.Namespace, l4.Service.Name, isSharedHC)

	healthcheck, err := composite.GetHealthCheck(l4.cloud, meta.GlobalKey(hcName), meta.VersionGA)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch healthcheck %s - err %w", hcName, err)
	}

	if healthcheck.Name != hcName {
		return nil, fmt.Errorf("unexpected name for healthcheck '%s' - expected '%s'", healthcheck.Name, hcName)
	}

	expectedDesc, err := utils.MakeL4LBServiceDescription(utils.ServiceKeyFunc(l4.Service.Namespace, l4.Service.Name), "", meta.VersionGA, isSharedHC, utils.ILB)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate Health Check description")
	}
	if healthcheck.Description != expectedDesc {
		return nil, fmt.Errorf("unexpected description in healthcheck - Expected %s, Got %s", healthcheck.Description, expectedDesc)
	}
	return healthcheck, nil
}

func getAndVerifyILBBackendService(l4 *L4, healthCheck *composite.HealthCheck) (*composite.BackendService, error) {
	backendServiceName := l4.namer.L4Backend(l4.Service.Namespace, l4.Service.Name)
	key := meta.RegionalKey(backendServiceName, l4.cloud.Region())
	bs, err := composite.GetBackendService(l4.cloud, key, meta.VersionGA)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch backend service %s - err %w", backendServiceName, err)
	}
	proto := utils.GetProtocol(l4.Service.Spec.Ports)
	if bs.Protocol != string(proto) {
		return nil, fmt.Errorf("unexpected protocol '%s' for backend service %v", bs.Protocol, bs)
	}
	backendServiceLink := cloud.SelfLink(meta.VersionGA, l4.cloud.ProjectID(), "backendServices", key)
	if bs.SelfLink != backendServiceLink {
		return nil, fmt.Errorf("unexpected self link in backend service - Expected %s, Got %s", bs.SelfLink, backendServiceLink)
	}

	resourceDesc, err := utils.MakeL4LBServiceDescription(utils.ServiceKeyFunc(l4.Service.Namespace, l4.Service.Name), "", meta.VersionGA, false, utils.ILB)
	if err != nil {
		return nil, fmt.Errorf("failed to create description for resources, err %w", err)
	}
	if bs.Description != resourceDesc {
		return nil, fmt.Errorf("unexpected description in backend service - Expected %s, Got %s", bs.Description, resourceDesc)
	}
	if !utils.EqualStringSets(bs.HealthChecks, []string{healthCheck.SelfLink}) {
		return nil, fmt.Errorf("unexpected healthcheck reference '%v' in backend service, expected '%s'", bs.HealthChecks,
			healthCheck.SelfLink)
	}
	return bs, nil
}

func verifyILBIPv4ForwardingRule(l4 *L4, backendServiceLink string) error {
	frName := l4.GetFRName()
	return verifyILBForwardingRule(l4, frName, backendServiceLink)
}

func verifyILBIPv6ForwardingRule(l4 *L4, backendServiceLink string) error {
	ipv6FrName := l4.getIPv6FRName()
	return verifyILBForwardingRule(l4, ipv6FrName, backendServiceLink)
}

func verifyILBForwardingRule(l4 *L4, frName string, backendServiceLink string) error {
	fwdRule, err := composite.GetForwardingRule(l4.cloud, meta.RegionalKey(frName, l4.cloud.Region()), meta.VersionGA)
	if err != nil {
		return fmt.Errorf("failed to fetch forwarding rule %s - err %w", frName, err)
	}
	if fwdRule.Name != frName {
		return fmt.Errorf("unexpected name for forwarding rule '%s' - expected '%s'", fwdRule.Name, frName)
	}
	if fwdRule.LoadBalancingScheme != string(cloud.SchemeInternal) {
		return fmt.Errorf("unexpected LoadBalancingScheme for forwarding rule '%s' - expected '%s'", fwdRule.LoadBalancingScheme, cloud.SchemeInternal)
	}

	proto := utils.GetProtocol(l4.Service.Spec.Ports)
	if fwdRule.IPProtocol != string(proto) {
		return fmt.Errorf("unexpected protocol '%s' for forwarding rule %v", fwdRule.IPProtocol, fwdRule)
	}

	if fwdRule.BackendService != backendServiceLink {
		return fmt.Errorf("unexpected backend service link '%s' for forwarding rule, expected '%s'", fwdRule.BackendService, backendServiceLink)
	}

	subnet := l4.Service.Annotations[gce.ServiceAnnotationILBSubnet]
	if subnet == "" {
		subnet = l4.cloud.SubnetworkURL()
	} else {
		key := meta.RegionalKey(subnet, l4.cloud.Region())
		subnet = cloud.SelfLink(meta.VersionGA, l4.cloud.ProjectID(), "subnetworks", key)
	}
	if fwdRule.Subnetwork != subnet {
		return fmt.Errorf("unexpected subnetwork %q in forwarding rule, expected %q",
			fwdRule.Subnetwork, subnet)
	}

	addr, err := l4.cloud.GetRegionAddress(frName, l4.cloud.Region())
	if err == nil || addr != nil {
		return fmt.Errorf("expected error when looking up ephemeral address, got %v", addr)
	}
	return nil
}

func verifyILBIPv4NodesFirewall(l4 *L4, nodeNames []string) error {
	fwName := l4.namer.L4Firewall(l4.Service.Namespace, l4.Service.Name)
	fwDesc, err := utils.MakeL4LBServiceDescription(utils.ServiceKeyFunc(l4.Service.Namespace, l4.Service.Name), "", meta.VersionGA, false, utils.ILB)
	if err != nil {
		return fmt.Errorf("failed to create description for resources, err %w", err)
	}

	sourceRanges, err := utils.IPv4ServiceSourceRanges(l4.Service)
	if err != nil {
		return fmt.Errorf("utils.IPv4ServiceSourceRanges(%+v) returned error %v, want nil", l4.Service, err)
	}
	return verifyFirewall(l4.cloud, nodeNames, fwName, fwDesc, sourceRanges)
}

func verifyILBIPv6NodesFirewall(l4 *L4, nodeNames []string) error {
	ipv6FirewallName := l4.namer.L4IPv6Firewall(l4.Service.Namespace, l4.Service.Name)

	fwDesc, err := utils.MakeL4LBServiceDescription(utils.ServiceKeyFunc(l4.Service.Namespace, l4.Service.Name), "", meta.VersionGA, false, utils.ILB)
	if err != nil {
		return fmt.Errorf("failed to create description for resources, err %w", err)
	}

	sourceRanges, err := utils.IPv6ServiceSourceRanges(l4.Service)
	if err != nil {
		return fmt.Errorf("utils.IPv6ServiceSourceRanges(%+v) returned error %v, want nil", l4.Service, err)
	}
	return verifyFirewall(l4.cloud, nodeNames, ipv6FirewallName, fwDesc, sourceRanges)
}

func verifyILBIPv4HealthCheckFirewall(l4 *L4, nodeNames []string) error {
	isSharedHC := !servicehelper.RequestsOnlyLocalTraffic(l4.Service)

	hcFwName := l4.namer.L4HealthCheckFirewall(l4.Service.Namespace, l4.Service.Name, isSharedHC)
	hcFwDesc, err := utils.MakeL4LBFirewallDescription(utils.ServiceKeyFunc(l4.Service.Namespace, l4.Service.Name), "", meta.VersionGA, isSharedHC)
	if err != nil {
		return fmt.Errorf("failed to calculate description for health check for service %v, error %v", l4.Service, err)
	}

	return verifyFirewall(l4.cloud, nodeNames, hcFwName, hcFwDesc, gce.L4LoadBalancerSrcRanges())
}

func verifyILBIPv6HealthCheckFirewall(l4 *L4, nodeNames []string) error {
	isSharedHC := !servicehelper.RequestsOnlyLocalTraffic(l4.Service)

	ipv6hcFwName := l4.namer.L4IPv6HealthCheckFirewall(l4.Service.Namespace, l4.Service.Name, isSharedHC)
	hcFwDesc, err := utils.MakeL4LBFirewallDescription(utils.ServiceKeyFunc(l4.Service.Namespace, l4.Service.Name), "", meta.VersionGA, isSharedHC)
	if err != nil {
		return fmt.Errorf("failed to calculate decsription for health check for service %v, error %v", l4.Service, err)
	}

	return verifyFirewall(l4.cloud, nodeNames, ipv6hcFwName, hcFwDesc, []string{healthchecksl4.L4ILBIPv6HCRange})
}

func assertILBResourcesDeleted(t *testing.T, l4 *L4) {
	t.Helper()

	nodesFwName := l4.namer.L4Firewall(l4.Service.Namespace, l4.Service.Name)
	hcFwNameShared := l4.namer.L4HealthCheckFirewall(l4.Service.Namespace, l4.Service.Name, true)
	hcFwNameNonShared := l4.namer.L4HealthCheckFirewall(l4.Service.Namespace, l4.Service.Name, false)

	fwNames := []string{
		nodesFwName,
		hcFwNameShared,
		hcFwNameNonShared,
	}

	for _, fwName := range fwNames {
		err := verifyFirewallNotExists(l4.cloud, fwName)
		if err != nil {
			t.Errorf("verifyFirewallNotExists(_, %s) returned error %v, want nil", fwName, err)
		}
	}

	frName := l4.GetFRName()
	err := verifyForwardingRuleNotExists(l4.cloud, frName)
	if err != nil {
		t.Errorf("verifyForwardingRuleNotExists(_, %s) returned error %v, want nil", frName, err)
	}

	hcNameShared := l4.namer.L4HealthCheck(l4.Service.Namespace, l4.Service.Name, true)
	err = verifyHealthCheckNotExists(l4.cloud, hcNameShared, meta.Global)
	if err != nil {
		t.Errorf("verifyHealthCheckNotExists(_, %s)", hcNameShared)
	}

	hcNameNonShared := l4.namer.L4HealthCheck(l4.Service.Namespace, l4.Service.Name, false)
	err = verifyHealthCheckNotExists(l4.cloud, hcNameNonShared, meta.Global)
	if err != nil {
		t.Errorf("verifyHealthCheckNotExists(_, %s)", hcNameNonShared)
	}

	err = verifyBackendServiceNotExists(l4.cloud, nodesFwName)
	if err != nil {
		t.Errorf("verifyBackendServiceNotExists(_, %s)", nodesFwName)
	}

	err = verifyAddressNotExists(l4.cloud, frName)
	if err != nil {
		t.Errorf("verifyAddressNotExists(_, %s)", frName)
	}
}

func assertDualStackILBResourcesDeleted(t *testing.T, l4 *L4) {
	t.Helper()

	err := verifyILBCommonDualStackResourcesDeleted(l4)
	if err != nil {
		t.Errorf("verifyCommonDualStackResourcesDeleted(_) returned erorr %v, want nil", err)
	}

	err = verifyILBIPv4ResourcesDeletedOnSync(l4)
	if err != nil {
		t.Errorf("verifyILBIPv4ResourcesDeletedOnSync(_) returned erorr %v, want nil", err)
	}

	err = verifyILBIPv6ResourcesDeletedOnSync(l4)
	if err != nil {
		t.Errorf("verifyILBIPv4ResourcesDeletedOnSync(_) returned erorr %v, want nil", err)
	}

	// Check health check firewalls separately, because we don't clean them on sync, only on final deletion
	ipv4HcFwNameShared := l4.namer.L4HealthCheckFirewall(l4.Service.Namespace, l4.Service.Name, true)
	ipv6HcFwNameShared := l4.namer.L4IPv6HealthCheckFirewall(l4.Service.Namespace, l4.Service.Name, true)
	ipv4HcFwNameNonShared := l4.namer.L4HealthCheckFirewall(l4.Service.Namespace, l4.Service.Name, false)
	ipv6HcFwNameNonShared := l4.namer.L4IPv6HealthCheckFirewall(l4.Service.Namespace, l4.Service.Name, false)

	fwNames := []string{
		ipv4HcFwNameShared,
		ipv4HcFwNameNonShared,
		ipv6HcFwNameShared,
		ipv6HcFwNameNonShared,
	}

	for _, fwName := range fwNames {
		err = verifyFirewallNotExists(l4.cloud, fwName)
		if err != nil {
			t.Errorf("verifyFirewallNotExists(_, %s) returned error %v, want nil", fwName, err)
		}
	}
}

func verifyILBCommonDualStackResourcesDeleted(l4 *L4) error {
	backendServiceName := l4.namer.L4Backend(l4.Service.Namespace, l4.Service.Name)

	err := verifyBackendServiceNotExists(l4.cloud, backendServiceName)
	if err != nil {
		return fmt.Errorf("verifyBackendServiceNotExists(_, %s)", backendServiceName)
	}

	hcNameShared := l4.namer.L4HealthCheck(l4.Service.Namespace, l4.Service.Name, true)
	err = verifyHealthCheckNotExists(l4.cloud, hcNameShared, meta.Global)
	if err != nil {
		return fmt.Errorf("verifyHealthCheckNotExists(_, %s)", hcNameShared)
	}

	hcNameNonShared := l4.namer.L4HealthCheck(l4.Service.Namespace, l4.Service.Name, false)
	err = verifyHealthCheckNotExists(l4.cloud, hcNameNonShared, meta.Global)
	if err != nil {
		return fmt.Errorf("verifyHealthCheckNotExists(_, %s)", hcNameNonShared)
	}

	err = verifyAddressNotExists(l4.cloud, backendServiceName)
	if err != nil {
		return fmt.Errorf("verifyAddressNotExists(_, %s)", backendServiceName)
	}
	return nil
}

// we don't delete ipv4 health check firewall on sync
func verifyILBIPv4ResourcesDeletedOnSync(l4 *L4) error {
	ipv4FwName := l4.namer.L4Backend(l4.Service.Namespace, l4.Service.Name)
	err := verifyFirewallNotExists(l4.cloud, ipv4FwName)
	if err != nil {
		return fmt.Errorf("verifyFirewallNotExists(_, %s) returned error %w, want nil", ipv4FwName, err)
	}

	ipv4FrName := l4.GetFRName()
	err = verifyForwardingRuleNotExists(l4.cloud, ipv4FrName)
	if err != nil {
		return fmt.Errorf("verifyForwardingRuleNotExists(_, %s) returned error %w, want nil", ipv4FrName, err)
	}

	addressName := ipv4FwName
	err = verifyAddressNotExists(l4.cloud, addressName)
	if err != nil {
		return fmt.Errorf("verifyAddressNotExists(_, %s)", addressName)
	}

	return nil
}

// we don't delete ipv6 health check firewall on sync
func verifyILBIPv6ResourcesDeletedOnSync(l4 *L4) error {
	ipv6FwName := l4.namer.L4IPv6Firewall(l4.Service.Namespace, l4.Service.Name)
	err := verifyFirewallNotExists(l4.cloud, ipv6FwName)
	if err != nil {
		return fmt.Errorf("verifyFirewallNotExists(_, %s) returned error %w, want nil", ipv6FwName, err)
	}

	ipv6FrName := l4.getIPv6FRName()
	err = verifyForwardingRuleNotExists(l4.cloud, ipv6FrName)
	if err != nil {
		return fmt.Errorf("verifyForwardingRuleNotExists(_, %s) returned error %w, want nil", ipv6FrName, err)
	}

	return nil
}
