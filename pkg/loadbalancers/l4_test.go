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
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

	bsName := l4.namer.L4Backend(l4.Service.Namespace, l4.Service.Name)
	_, err := l4.backendPool.EnsureL4BackendService(bsName, "", "TCP", string(svc.Spec.SessionAffinity), string(cloud.SchemeInternal), l4.NamespacedName, meta.VersionGA)
	if err != nil {
		t.Errorf("Failed to ensure backend service  %s - err %v", bsName, err)
	}

	// Update the Internal Backend Service with a new ServiceAffinity
	_, err = l4.backendPool.EnsureL4BackendService(bsName, "", "TCP", string(v1.ServiceAffinityNone), string(cloud.SchemeInternal), l4.NamespacedName, meta.VersionGA)
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
	bs, err = l4.backendPool.EnsureL4BackendService(bsName, "", "TCP", string(v1.ServiceAffinityNone), string(cloud.SchemeInternal), l4.NamespacedName, meta.VersionGA)
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
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

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
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

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
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

	if _, err := test.CreateAndInsertNodes(l4.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}

	lbName := l4.namer.L4Backend(svc.Namespace, svc.Name)

	// Create the expected resources necessary for an Internal Load Balancer
	sharedHC := !servicehelper.RequestsOnlyLocalTraffic(svc)
	hcResult := l4.healthChecks.EnsureHealthCheck(l4.Service, l4.namer, sharedHC, meta.Global, utils.ILB, []string{})

	if hcResult.Err != nil {
		t.Errorf("Failed to create healthcheck, err %v", hcResult.Err)
	}
	_, err := l4.backendPool.EnsureL4BackendService(lbName, hcResult.HCLink, "TCP", string(l4.Service.Spec.SessionAffinity),
		string(cloud.SchemeInternal), l4.NamespacedName, meta.VersionGA)
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
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

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
	fakeGCE.CreateFirewall(existingFirewall)

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
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

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
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

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
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

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
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

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

	_, _, result := ensureService(fakeGCE, namer, nodeNames, vals.ZoneName, 8080, t)
	if result != nil && result.Error != nil {
		t.Fatalf("Error ensuring service err: %v", result.Error)
	}
	svc2, l4, result := ensureService(fakeGCE, namer, nodeNames, vals.ZoneName, 8081, t)
	if result != nil && result.Error != nil {
		t.Fatalf("Error ensuring service err: %v", result.Error)
	}

	// Delete the loadbalancer.
	result = l4.EnsureInternalLoadBalancerDeleted(svc2)
	if result.Error != nil {
		t.Errorf("Unexpected error %v", result.Error)
	}
	// When health check is shared we expect that hc firewall rule will not be deleted.
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
	l4NetLB := NewL4NetLB(netlbSvc, fakeGCE, meta.Regional, namer, record.NewFakeRecorder(100))
	// make sure both ilb and netlb use the same l4 healtcheck instance
	l4NetLB.healthChecks = l4.healthChecks

	// create netlb resources
	xlbResult := l4NetLB.EnsureFrontend(nodeNames, netlbSvc)
	if xlbResult.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", xlbResult.Error)
	}
	if len(xlbResult.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4NetLB)
	}
	assertNetLbResources(t, netlbSvc, l4NetLB, nodeNames)

	// Delete the ILB loadbalancer
	result = l4.EnsureInternalLoadBalancerDeleted(ilbSvc)
	if result.Error != nil {
		t.Errorf("Unexpected error %v", result.Error)
	}

	// When NetLB health check uses the same firewall rules we expect that hc firewall rule will not be deleted.
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
	svc := test.NewL4ILBService(false, 8080)

	l4ilbParams := &L4ILBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4 := NewL4Handler(l4ilbParams)
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

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
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

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
			l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

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
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

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
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

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
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

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
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

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

	// Delete the service
	result = l4.EnsureInternalLoadBalancerDeleted(svc)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	assertILBResourcesDeleted(t, l4)
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
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, &test.FakeRecorderSource{})

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

func assertILBResources(t *testing.T, l4 *L4, nodeNames []string, resourceAnnotations map[string]string) {
	t.Helper()

	err := verifyNodesFirewall(l4, nodeNames)
	if err != nil {
		t.Errorf("verifyNodesFirewall(_, %v) returned error %v, want nil", nodeNames, err)
	}

	err = verifyHealthCheckFirewall(l4, nodeNames)
	if err != nil {
		t.Errorf("verifyHealthCheckFirewall(_, %v) returned error %v, want nil", nodeNames, err)
	}

	healthCheck, err := getAndVerifyHealthCheck(l4)
	if err != nil {
		t.Errorf("getAndVerifyHealthCheck(_) returned error %v, want nil", err)
	}

	backendService, err := getAndVerifyBackendService(l4, healthCheck)
	if err != nil {
		t.Errorf("getAndVerifyBackendService(_, %v) returned error %v, want nil", healthCheck, err)
	}

	err = verifyForwardingRule(l4, backendService.SelfLink)
	if err != nil {
		t.Errorf("verifyForwardingRule(_, %s) returned error %v, want nil", backendService.SelfLink, err)
	}

	expectedAnnotations := buildExpectedAnnotations(l4)
	if !reflect.DeepEqual(expectedAnnotations, resourceAnnotations) {
		t.Fatalf("Expected annotations %v, got %v", expectedAnnotations, resourceAnnotations)
	}
}

func getAndVerifyHealthCheck(l4 *L4) (*composite.HealthCheck, error) {
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

func getAndVerifyBackendService(l4 *L4, healthCheck *composite.HealthCheck) (*composite.BackendService, error) {
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

func verifyForwardingRule(l4 *L4, backendServiceLink string) error {
	frName := l4.GetFRName()
	fwdRule, err := composite.GetForwardingRule(l4.cloud, meta.RegionalKey(frName, l4.cloud.Region()), meta.VersionGA)
	if err != nil {
		return fmt.Errorf("failed to fetch forwarding rule %s - err %w", frName, err)
	}
	if fwdRule.Name != frName {
		return fmt.Errorf("unexpected name for forwarding rule '%s' - expected '%s'", fwdRule.Name, frName)
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

func verifyNodesFirewall(l4 *L4, nodeNames []string) error {
	fwName := l4.namer.L4Backend(l4.Service.Namespace, l4.Service.Name)
	fwDesc, err := utils.MakeL4LBServiceDescription(utils.ServiceKeyFunc(l4.Service.Namespace, l4.Service.Name), "", meta.VersionGA, false, utils.ILB)
	if err != nil {
		return fmt.Errorf("failed to create description for resources, err %w", err)
	}

	return verifyFirewall(l4, nodeNames, fwName, fwDesc)
}

func verifyHealthCheckFirewall(l4 *L4, nodeNames []string) error {
	isSharedHC := !servicehelper.RequestsOnlyLocalTraffic(l4.Service)

	hcFwName := l4.namer.L4HealthCheckFirewall(l4.Service.Namespace, l4.Service.Name, isSharedHC)
	hcFwDesc, err := utils.MakeL4LBFirewallDescription(utils.ServiceKeyFunc(l4.Service.Namespace, l4.Service.Name), "", meta.VersionGA, isSharedHC)
	if err != nil {
		return fmt.Errorf("failed to calculate decsription for health check for service %v, error %v", l4.Service, err)
	}

	return verifyFirewall(l4, nodeNames, hcFwName, hcFwDesc)
}

func verifyFirewall(l4 *L4, nodeNames []string, firewallName, expectedDescription string) error {
	firewall, err := l4.cloud.GetFirewall(firewallName)
	if err != nil {
		return fmt.Errorf("failed to fetch firewall rule %q - err %w", firewallName, err)
	}
	if !utils.EqualStringSets(nodeNames, firewall.TargetTags) {
		return fmt.Errorf("expected firewall rule target tags '%v', Got '%v'", nodeNames, firewall.TargetTags)
	}
	if len(firewall.SourceRanges) == 0 {
		return fmt.Errorf("unexpected empty source range for firewall rule %v", firewall)
	}
	if firewall.Description != expectedDescription {
		return fmt.Errorf("unexpected description in firewall %q - Expected %s, Got %s", firewallName, expectedDescription, firewall.Description)
	}
	return nil
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

	hcFwName := l4.namer.L4HealthCheckFirewall(l4.Service.Namespace, l4.Service.Name, isSharedHC)

	expectedAnnotations[annotations.FirewallRuleForHealthcheckKey] = hcFwName
	expectedAnnotations[annotations.FirewallRuleKey] = backendName

	frName := l4.GetFRName()
	if proto == v1.ProtocolTCP {
		expectedAnnotations[annotations.TCPForwardingRuleKey] = frName
	} else {
		expectedAnnotations[annotations.UDPForwardingRuleKey] = frName
	}
	return expectedAnnotations
}

func assertILBResourcesDeleted(t *testing.T, l4 *L4) {
	t.Helper()

	resourceName := l4.namer.L4Backend(l4.Service.Namespace, l4.Service.Name)
	hcFwNameShared := l4.namer.L4HealthCheckFirewall(l4.Service.Namespace, l4.Service.Name, true)
	hcFwNameNonShared := l4.namer.L4HealthCheckFirewall(l4.Service.Namespace, l4.Service.Name, false)

	fwNames := []string{
		resourceName,
		hcFwNameShared,
		hcFwNameNonShared,
	}

	for _, fwName := range fwNames {
		err := verifyFirewallNotExists(l4, fwName)
		if err != nil {
			t.Errorf("verifyFirewallNotExists(_, %s) returned error %v, want nil", fwName, err)
		}
	}

	frName := l4.GetFRName()
	err := verifyForwardingRuleNotExists(l4, frName)
	if err != nil {
		t.Errorf("verifyForwardingRuleNotExists(_, %s) returned error %v, want nil", frName, err)
	}

	hcNameShared := l4.namer.L4HealthCheck(l4.Service.Namespace, l4.Service.Name, true)
	err = verifyHealthCheckNotExists(l4, hcNameShared)
	if err != nil {
		t.Errorf("verifyHealthCheckNotExists(_, %s)", hcNameShared)
	}

	hcNameNonShared := l4.namer.L4HealthCheck(l4.Service.Namespace, l4.Service.Name, false)
	err = verifyHealthCheckNotExists(l4, hcNameNonShared)
	if err != nil {
		t.Errorf("verifyHealthCheckNotExists(_, %s)", hcNameNonShared)
	}

	err = verifyBackendServiceNotExists(l4, resourceName)
	if err != nil {
		t.Errorf("verifyBackendServiceNotExists(_, %s)", resourceName)
	}

	err = verifyAddressNotExists(l4, resourceName)
	if err != nil {
		t.Errorf("verifyAddressNotExists(_, %s)", resourceName)
	}
}

func verifyFirewallNotExists(l4 *L4, fwName string) error {
	firewall, err := l4.cloud.GetFirewall(fwName)
	if !utils.IsNotFoundError(err) || firewall != nil {
		return fmt.Errorf("firewall %s exists, expected not", fwName)
	}
	return nil
}

func verifyForwardingRuleNotExists(l4 *L4, frName string) error {
	fwdRule, err := l4.cloud.GetRegionForwardingRule(frName, l4.cloud.Region())
	if !utils.IsNotFoundError(err) || fwdRule != nil {
		return fmt.Errorf("forwarding rule %s exists, expected not", frName)
	}
	return nil
}

func verifyHealthCheckNotExists(l4 *L4, hcName string) error {
	healthCheck, err := l4.cloud.GetHealthCheck(hcName)
	if !utils.IsNotFoundError(err) || healthCheck != nil {
		return fmt.Errorf("health check %s exists, expected not", hcName)
	}
	return nil
}

func verifyBackendServiceNotExists(l4 *L4, bsName string) error {
	bs, err := l4.cloud.GetRegionBackendService(bsName, l4.cloud.Region())
	if !utils.IsNotFoundError(err) || bs != nil {
		return fmt.Errorf("backend service %s exists, expected not", bsName)
	}
	return nil
}

func verifyAddressNotExists(l4 *L4, addressName string) error {
	addr, err := l4.cloud.GetRegionAddress(addressName, l4.cloud.Region())
	if !utils.IsNotFoundError(err) || addr != nil {
		return fmt.Errorf("address %s exists, expected not", addressName)
	}
	return nil
}
