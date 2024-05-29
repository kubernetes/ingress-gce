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
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/klog/v2"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	ga "google.golang.org/api/compute/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	servicehelper "k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/healthchecksl4"
	"k8s.io/ingress-gce/pkg/metrics"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/utils/strings/slices"
)

const (
	usersIP        = "35.10.211.60"
	usersIPPremium = "35.10.211.70"
)

var noExternalIPv6InSubnetError = regexp.MustCompile("subnet [a-z]([-a-z0-9]*[a-z0-9])? does not have external IPv6 ranges, required for an external IPv6 Service. You can specify an external IPv6 subnet using the \"networking.gke.io/load-balancer-subnet\" annotation on the Service")

func TestEnsureL4NetLoadBalancer(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}
	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	svc := test.NewL4NetLBRBSService(8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, namer_util.NewNamer(vals.ClusterName, "cluster-fw", klog.TODO()))

	l4NetLBParams := &L4NetLBParams{
		Service:         svc,
		Cloud:           fakeGCE,
		Namer:           namer,
		Recorder:        record.NewFakeRecorder(100),
		NetworkResolver: network.NewFakeResolver(network.DefaultNetwork(fakeGCE)),
	}
	l4netlb := NewL4NetLB(l4NetLBParams, klog.TODO())
	l4netlb.healthChecks = healthchecksl4.Fake(fakeGCE, l4netlb.recorder)

	if _, err := test.CreateAndInsertNodes(l4netlb.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}
	result := l4netlb.EnsureFrontend(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4netlb)
	}
	l4netlb.Service.Annotations = result.Annotations
	assertNetLBResources(t, l4netlb, nodeNames)
	if err := checkMetrics(result.MetricsLegacyState /*isManaged = */, true /*isPremium = */, true /*isUserError =*/, false); err != nil {
		t.Errorf("Metrics error: %v", err)
	}
}

func TestEnsureMultinetL4NetLoadBalancer(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}
	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	svc := test.NewL4NetLBRBSService(8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, namer_util.NewNamer(vals.ClusterName, "cluster-fw", klog.TODO()))

	l4NetLBParams := &L4NetLBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
		NetworkResolver: network.NewFakeResolver(&network.NetworkInfo{
			IsDefault:     false,
			K8sNetwork:    "secondary",
			NetworkURL:    "secondaryNetURL",
			SubnetworkURL: "secondarySubnetURL",
		}),
	}
	l4netlb := NewL4NetLB(l4NetLBParams, klog.TODO())
	l4netlb.healthChecks = healthchecksl4.Fake(fakeGCE, l4netlb.recorder)

	if _, err := test.CreateAndInsertNodes(l4netlb.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}
	result := l4netlb.EnsureFrontend(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4netlb)
	}
	l4netlb.Service.Annotations = result.Annotations
	assertNetLBResources(t, l4netlb, nodeNames)
	if err := checkMetrics(result.MetricsLegacyState /*isManaged = */, true /*isPremium = */, true /*isUserError =*/, false); err != nil {
		t.Errorf("Metrics error: %v", err)
	}
}

func TestDeleteL4NetLoadBalancer(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}
	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	svc := test.NewL4NetLBRBSService(8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, namer_util.NewNamer(vals.ClusterName, "cluster-fw", klog.TODO()))

	l4NetLBParams := &L4NetLBParams{
		Service:         svc,
		Cloud:           fakeGCE,
		Namer:           namer,
		Recorder:        record.NewFakeRecorder(100),
		NetworkResolver: network.NewFakeResolver(network.DefaultNetwork(fakeGCE)),
	}
	l4NetLB := NewL4NetLB(l4NetLBParams, klog.TODO())
	l4NetLB.healthChecks = healthchecksl4.Fake(fakeGCE, l4NetLB.recorder)

	if _, err := test.CreateAndInsertNodes(l4NetLB.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}
	result := l4NetLB.EnsureFrontend(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4NetLB)
	}
	l4NetLB.Service.Annotations = result.Annotations
	assertNetLBResources(t, l4NetLB, nodeNames)

	if err := l4NetLB.EnsureLoadBalancerDeleted(svc); err.Error != nil {
		t.Errorf("UnexpectedError %v", err.Error)
	}
	assertNetLBResourcesDeleted(t, l4NetLB)
}

func TestDeleteL4NetLoadBalancerWithSharedHC(t *testing.T) {
	t.Parallel()
	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)
	(fakeGCE.Compute().(*cloud.MockGCE)).MockRegionHealthChecks.DeleteHook = test.DeleteRegionalHealthCheckResourceInUseErrorHook

	_, _ = ensureLoadBalancer(8080, vals, fakeGCE, t)
	svc, l4NetLB := ensureLoadBalancer(8081, vals, fakeGCE, t)

	if err := l4NetLB.EnsureLoadBalancerDeleted(svc); err.Error != nil {
		t.Errorf("UnexpectedError %v", err.Error)
	}
	// Health check is in used by second service
	// we expectEqual that firewall rule will not be deleted
	hcFwName := l4NetLB.namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, true)
	firewall, err := l4NetLB.cloud.GetFirewall(hcFwName)
	if err != nil || firewall == nil {
		t.Errorf("Expected firewall exists err: %v, fwR: %v", err, firewall)
	}
}

func TestHealthCheckFirewallDeletionWithILB(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}
	vals := gce.DefaultTestClusterValues()

	fakeGCE := getFakeGCECloud(vals)
	namer := namer_util.NewL4Namer(kubeSystemUID, namer_util.NewNamer(vals.ClusterName, "cluster-fw", klog.TODO()))

	// Create ILB service
	_, l4ilb, ilbResult := ensureService(fakeGCE, namer, nodeNames, vals.ZoneName, 8080, t)
	if ilbResult != nil && ilbResult.Error != nil {
		t.Fatalf("Error ensuring service err: %v", ilbResult.Error)
	}

	// Create NetLB Service
	netlbSvc := test.NewL4NetLBRBSService(8080)
	l4NetlbParams := &L4NetLBParams{
		Service:         netlbSvc,
		Cloud:           fakeGCE,
		Namer:           namer,
		Recorder:        record.NewFakeRecorder(100),
		NetworkResolver: network.NewFakeResolver(network.DefaultNetwork(fakeGCE)),
	}
	l4NetLB := NewL4NetLB(l4NetlbParams, klog.TODO())

	// make sure both ilb and netlb use the same l4 healthcheck instance
	l4NetLB.healthChecks = l4ilb.healthChecks

	// create netlb resources
	result := l4NetLB.EnsureFrontend(nodeNames, netlbSvc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4NetLB)
	}
	l4NetLB.Service.Annotations = result.Annotations
	assertNetLBResources(t, l4NetLB, nodeNames)

	// Delete the NetLB loadbalancer.
	if err := l4NetLB.EnsureLoadBalancerDeleted(netlbSvc); err.Error != nil {
		t.Errorf("UnexpectedError %v", err.Error)
	}

	// When ILB health check uses the same firewall rules we expectEqual that hc firewall rule will not be deleted.
	hcName := l4NetLB.namer.L4HealthCheck(l4NetLB.Service.Namespace, l4NetLB.Service.Name, true)
	hcFwName := l4NetLB.namer.L4HealthCheckFirewall(l4NetLB.Service.Namespace, l4NetLB.Service.Name, true)
	firewall, err := l4NetLB.cloud.GetFirewall(hcFwName)
	if err != nil {
		t.Errorf("Expected error: firewall exists, got %v", err)
	}
	if firewall == nil {
		t.Error("Healthcheck Firewall should still exist, got nil")
	}

	// The healthcheck itself should be deleted.
	_, err = composite.GetHealthCheck(l4NetLB.cloud, meta.RegionalKey(hcName, l4NetLB.cloud.Region()), meta.VersionGA, klog.TODO())
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("Healthcheck %s should be deleted", hcName)
	}
}

func TestMetricsForStandardNetworkTier(t *testing.T) {
	nodeNames := []string{"test-node-1"}
	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)
	createUserStaticIPInStandardTier(fakeGCE, vals.Region)
	(fakeGCE.Compute().(*cloud.MockGCE)).MockForwardingRules.GetHook = test.GetRBSForwardingRuleInStandardTier

	svc := test.NewL4NetLBRBSService(8080)
	svc.Spec.LoadBalancerIP = usersIP
	svc.ObjectMeta.Annotations[annotations.NetworkTierAnnotationKey] = string(cloud.NetworkTierStandard)
	namer := namer_util.NewL4Namer(kubeSystemUID, namer_util.NewNamer(vals.ClusterName, "cluster-fw", klog.TODO()))

	l4NetLBParams := &L4NetLBParams{
		Service:         svc,
		Cloud:           fakeGCE,
		Namer:           namer,
		Recorder:        record.NewFakeRecorder(100),
		NetworkResolver: network.NewFakeResolver(network.DefaultNetwork(fakeGCE)),
	}
	l4netlb := NewL4NetLB(l4NetLBParams, klog.TODO())
	l4netlb.healthChecks = healthchecksl4.Fake(fakeGCE, l4netlb.recorder)

	if _, err := test.CreateAndInsertNodes(l4netlb.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}
	result := l4netlb.EnsureFrontend(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if err := checkMetrics(result.MetricsLegacyState /*isManaged = */, false /*isPremium = */, false /*isUserError =*/, false); err != nil {
		t.Errorf("Metrics error: %v", err)
	}
	// Check that service sync will return error if User Address IP Network Tier mismatch with service Network Tier.
	svc.ObjectMeta.Annotations[annotations.NetworkTierAnnotationKey] = string(cloud.NetworkTierPremium)
	result = l4netlb.EnsureFrontend(nodeNames, svc)
	if result.Error == nil || !utils.IsNetworkTierError(result.Error) {
		t.Errorf("LoadBalancer sync should return Network Tier error, err %v", result.Error)
	}
	if err := checkMetrics(result.MetricsLegacyState /*isManaged = */, false /*isPremium = */, false /*isUserError =*/, true); err != nil {
		t.Errorf("Metrics error: %v", err)
	}
	// Check that when network tier annotation will be deleted which will change desired service Network Tier to PREMIUM
	// service sync will return User Error because we do not support updating forwarding rule.
	// Forwarding rule with wrong tier should be tear down and it can be done only via annotation change.

	// Crete new Static IP in Premium Network Tier to match default service Network Tier.
	createUserStaticIPInPremiumTier(fakeGCE, vals.Region)
	svc.Spec.LoadBalancerIP = usersIPPremium
	delete(svc.ObjectMeta.Annotations, annotations.NetworkTierAnnotationKey)

	result = l4netlb.EnsureFrontend(nodeNames, svc)
	if result.Error == nil || !utils.IsNetworkTierError(result.Error) {
		t.Errorf("LoadBalancer sync should return Network Tier error, err %v", result.Error)
	}
	if err := checkMetrics(result.MetricsLegacyState /*isManaged = */, false /*isPremium = */, false /*isUserError =*/, true); err != nil {
		t.Errorf("Metrics error: %v", err)
	}
}

func TestEnsureNetLBFirewallDestinations(t *testing.T) {
	nodeNames := []string{"test-node-1"}
	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	svc := test.NewL4NetLBRBSService(8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)
	l4NetLBParams := &L4NetLBParams{
		Service:         svc,
		Cloud:           fakeGCE,
		Namer:           namer,
		Recorder:        record.NewFakeRecorder(100),
		NetworkResolver: network.NewFakeResolver(network.DefaultNetwork(fakeGCE)),
	}
	l4netlb := NewL4NetLB(l4NetLBParams, klog.TODO())
	l4netlb.healthChecks = healthchecksl4.Fake(fakeGCE, l4netlb.recorder)

	if _, err := test.CreateAndInsertNodes(l4netlb.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}

	flags.F.EnablePinhole = true
	fwName := l4netlb.namer.L4Backend(l4netlb.Service.Namespace, l4netlb.Service.Name)

	fwrParams := firewalls.FirewallParams{
		Name:              fwName,
		SourceRanges:      []string{"10.0.0.0/20"},
		DestinationRanges: []string{"20.0.0.0/20"},
		NodeNames:         nodeNames,
		Protocol:          string(v1.ProtocolTCP),
		IP:                "1.2.3.4",
	}

	err := firewalls.EnsureL4FirewallRule(l4netlb.cloud, utils.ServiceKeyFunc(svc.Namespace, svc.Name), &fwrParams /*sharedRule = */, false, klog.TODO())
	if err != nil {
		t.Errorf("Unexpected error %v when ensuring firewall rule %s for svc %+v", err, fwName, svc)
	}
	existingFirewall, err := l4netlb.cloud.GetFirewall(fwName)
	if err != nil || existingFirewall == nil || len(existingFirewall.Allowed) == 0 {
		t.Errorf("Unexpected error %v when looking up firewall %s, Got firewall %+v", err, fwName, existingFirewall)
	}
	oldDestinationRanges := existingFirewall.DestinationRanges

	fwrParams.DestinationRanges = []string{"30.0.0.0/20"}
	err = firewalls.EnsureL4FirewallRule(l4netlb.cloud, utils.ServiceKeyFunc(svc.Namespace, svc.Name), &fwrParams /*sharedRule = */, false, klog.TODO())
	if err != nil {
		t.Errorf("Unexpected error %v when ensuring firewall rule %s for svc %+v", err, fwName, svc)
	}

	updatedFirewall, err := l4netlb.cloud.GetFirewall(fwName)
	if err != nil || updatedFirewall == nil || len(updatedFirewall.Allowed) == 0 {
		t.Errorf("Unexpected error %v when looking up firewall %s, Got firewall %+v", err, fwName, updatedFirewall)
	}

	if reflect.DeepEqual(oldDestinationRanges, updatedFirewall.DestinationRanges) {
		t.Errorf("DestinationRanges is not updated. oldDestinationRanges:%v, updatedFirewall.DestinationRanges:%v", oldDestinationRanges, updatedFirewall.DestinationRanges)
	}
}

func TestEnsureExternalDualStackLoadBalancer(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}

	testCases := []struct {
		ipFamilies    []v1.IPFamily
		trafficPolicy v1.ServiceExternalTrafficPolicyType
		desc          string
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

			svc := test.NewL4NetLBRBSDualStackService(v1.ProtocolTCP, tc.ipFamilies, tc.trafficPolicy)
			l4NetLB := mustSetupNetLBTestHandler(t, svc, nodeNames)

			result := l4NetLB.EnsureFrontend(nodeNames, svc)
			if result.Error != nil {
				t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
			}
			if len(result.Status.Ingress) == 0 {
				t.Errorf("Got empty loadBalancer status using handler %v", l4NetLB)
			}
			l4NetLB.Service.Annotations = result.Annotations
			assertDualStackNetLBResources(t, l4NetLB, nodeNames)

			l4NetLB.EnsureLoadBalancerDeleted(l4NetLB.Service)
			assertDualStackNetLBResourcesDeleted(t, l4NetLB)
		})
	}
}

func TestEnsureDualStackNetLBNetworkTierChange(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}

	ipFamilies := []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol}
	svc := test.NewL4NetLBRBSDualStackService(v1.ProtocolTCP, ipFamilies, v1.ServiceExternalTrafficPolicyTypeCluster)
	svc.Annotations[annotations.NetworkTierAnnotationKey] = "Standard"
	l4NetLB := mustSetupNetLBTestHandler(t, svc, nodeNames)

	// Ensure dualstack load balancer with Standard Network Tier and verify it synced successfully.
	result := l4NetLB.EnsureFrontend(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4NetLB)
	}
	l4NetLB.Service.Annotations = result.Annotations
	l4NetLB.Service.Annotations[annotations.NetworkTierAnnotationKey] = "Standard"
	assertDualStackNetLBResources(t, l4NetLB, nodeNames)

	// Change network Tier to Premium, and trigger sync.
	svc.Annotations[annotations.NetworkTierAnnotationKey] = "Premium"
	result = l4NetLB.EnsureFrontend(nodeNames, svc)
	if result.Error == nil {
		// This is buggy existing behaviour. Switching Network Tier (even before DualStack),
		// returns error on the first sync. However, after that it immediately resyncs,
		// and successfully provides the service. This bug was reported and should be fixed
		// but in separate PR.
		t.Errorf("Expected error on the first sync after switching network tier, got %v", result.Error)
	}
	result = l4NetLB.EnsureFrontend(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4NetLB)
	}
	l4NetLB.Service.Annotations = result.Annotations
	svc.Annotations[annotations.NetworkTierAnnotationKey] = "Premium"
	assertDualStackNetLBResources(t, l4NetLB, nodeNames)

	l4NetLB.EnsureLoadBalancerDeleted(l4NetLB.Service)
	assertDualStackNetLBResourcesDeleted(t, l4NetLB)
}

// This is exhaustive test that checks for all possible transitions of
// - ServiceExternalTrafficPolicy
// - Protocol
// - IPFamilies
// for dual-stack service. In total 400 combinations.
func TestDualStackNetLBTransitions(t *testing.T) {
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
								desc:                 dualStackNetLBTransitionTestDesc(initialIPFamily, finalIPFamily, initialTrafficPolicy, finalTrafficPolicy, initialProtocol, finalProtocol),
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

			svc := test.NewL4NetLBRBSDualStackService(tc.initialProtocol, tc.initialIPFamily, tc.initialTrafficPolicy)
			l4NetLB := mustSetupNetLBTestHandler(t, svc, nodeNames)

			l4NetLB.cloud.Compute().(*cloud.MockGCE).MockForwardingRules.DeleteHook = assertAddressOldReservedHook(t, l4NetLB.cloud)

			result := l4NetLB.EnsureFrontend(nodeNames, svc)
			svc.Annotations = result.Annotations
			assertDualStackNetLBResources(t, l4NetLB, nodeNames)

			finalSvc := test.NewL4NetLBRBSDualStackService(tc.finalProtocol, tc.finalIPFamily, tc.finalTrafficPolicy)
			finalSvc.Annotations = svc.Annotations
			l4NetLB.Service = finalSvc

			result = l4NetLB.EnsureFrontend(nodeNames, svc)
			finalSvc.Annotations = result.Annotations
			assertDualStackNetLBResources(t, l4NetLB, nodeNames)

			l4NetLB.cloud.Compute().(*cloud.MockGCE).MockForwardingRules.DeleteHook = nil
			l4NetLB.EnsureLoadBalancerDeleted(l4NetLB.Service)
			assertDualStackNetLBResourcesDeleted(t, l4NetLB)
		})
	}
}

func dualStackNetLBTransitionTestDesc(initialIPFamily []v1.IPFamily, finalIPFamily []v1.IPFamily, initialTrafficPolicy v1.ServiceExternalTrafficPolicyType, finalTrafficPolicy v1.ServiceExternalTrafficPolicyType, initialProtocol v1.Protocol, finalProtocol v1.Protocol) string {
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

func TestDualStackNetLBSyncIgnoresNoAnnotationIPv6Resources(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}

	svc := test.NewL4NetLBRBSService(8080)
	svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol}
	l4NetLB := mustSetupNetLBTestHandler(t, svc, nodeNames)

	result := l4NetLB.EnsureFrontend(nodeNames, svc)
	svc.Annotations = result.Annotations

	// Delete IPv4 resources annotation
	annotationsToDelete := []string{annotations.TCPForwardingRuleIPv6Key, annotations.FirewallRuleIPv6Key, annotations.FirewallRuleForHealthcheckIPv6Key}
	for _, annotationToDelete := range annotationsToDelete {
		delete(svc.Annotations, annotationToDelete)
	}
	svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv6Protocol}

	// Run new sync. Controller should not delete resources, if they don't exist in annotation
	result = l4NetLB.EnsureFrontend(nodeNames, svc)
	svc.Annotations = result.Annotations

	// Verify IPv4 Firewall was not deleted
	ipv6FWName := l4NetLB.namer.L4IPv6Firewall(l4NetLB.Service.Namespace, l4NetLB.Service.Name)
	err := verifyFirewallNotExists(l4NetLB.cloud, ipv6FWName)
	if err == nil {
		t.Errorf("firewall rule %s was deleted, expected not", ipv6FWName)
	}

	// Verify IPv6 Forwarding Rule was not deleted
	ipv6FRName := l4NetLB.ipv6FRName()
	err = verifyForwardingRuleNotExists(l4NetLB.cloud, ipv6FRName)
	if err == nil {
		t.Errorf("forwarding rule %s was deleted, expected not", ipv6FRName)
	}

	l4NetLB.EnsureLoadBalancerDeleted(l4NetLB.Service)
	// After complete deletion, IPv6 and IPv4 resources should be cleaned up, even if the were leaked
	assertDualStackNetLBResourcesDeleted(t, l4NetLB)
}

func TestDualStackNetLBSyncIgnoresNoAnnotationIPv4Resources(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}

	svc := test.NewL4NetLBRBSService(8080)
	svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol}
	l4NetLB := mustSetupNetLBTestHandler(t, svc, nodeNames)

	result := l4NetLB.EnsureFrontend(nodeNames, svc)
	svc.Annotations = result.Annotations
	assertDualStackNetLBResources(t, l4NetLB, nodeNames)

	// Delete IPv4 resources annotation
	annotationsToDelete := []string{annotations.TCPForwardingRuleKey, annotations.FirewallRuleKey, annotations.FirewallRuleForHealthcheckKey}
	for _, annotationToDelete := range annotationsToDelete {
		delete(svc.Annotations, annotationToDelete)
	}
	svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv6Protocol}

	// Run new sync. Controller should not delete resources, if they don't exist in annotation
	result = l4NetLB.EnsureFrontend(nodeNames, svc)
	svc.Annotations = result.Annotations

	// Verify IPv4 Firewall was not deleted
	fwName := l4NetLB.namer.L4Backend(l4NetLB.Service.Namespace, l4NetLB.Service.Name)
	err := verifyFirewallNotExists(l4NetLB.cloud, fwName)
	if err == nil {
		t.Errorf("firewall rule %s was deleted, expected not", fwName)
	}

	// Verify IPv4 Forwarding Rule was not deleted
	ipv4FRName := l4NetLB.frName()
	err = verifyForwardingRuleNotExists(l4NetLB.cloud, ipv4FRName)
	if err == nil {
		t.Errorf("forwarding rule %s was deleted, expected not", ipv4FRName)
	}

	l4NetLB.EnsureLoadBalancerDeleted(l4NetLB.Service)
	// After complete deletion, IPv6 and IPv4 resources should be cleaned up, even if the were leaked
	assertDualStackNetLBResourcesDeleted(t, l4NetLB)
}

// TestEnsureIPv6ExternalLoadBalancerCustomSubnet verifies custom subnet work with IPv6 NetLB:
//  1. Creates Service on cluster default subnet.
//  2. Creates custom "test-subnet" and specifies it in Service Annotation and verifies sync works properly.
//  3. Creates another subnet, puts it in annotation and verifies sync switches to it.
//  4. Removes custom subnet annotation and verifies syncing moves LB back to default subnet.
func TestEnsureIPv6ExternalLoadBalancerCustomSubnet(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}

	svc := test.NewL4NetLBRBSService(8080)
	svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol}
	l4NetLB := mustSetupNetLBTestHandler(t, svc, nodeNames)

	result := l4NetLB.EnsureFrontend(nodeNames, l4NetLB.Service)
	if result.Error != nil {
		t.Fatalf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	// Copy result annotations to the service, as assertion verifies that service got proper annotations.
	svc.Annotations = result.Annotations
	assertDualStackNetLBResourcesWithCustomIPv6Subnet(t, l4NetLB, nodeNames, l4NetLB.cloud.SubnetworkURL())

	// create custom subnet
	subnetKey := meta.RegionalKey("test-subnet", l4NetLB.cloud.Region())
	subnetToCreate := &ga.Subnetwork{
		Ipv6AccessType: subnetExternalIPv6AccessType,
		StackType:      "IPV4_IPV6",
	}
	err := l4NetLB.cloud.Compute().(*cloud.MockGCE).Subnetworks().Insert(context.TODO(), subnetKey, subnetToCreate)
	if err != nil {
		t.Fatalf("Failed to create subnet, error: %v", err)
	}
	svc.Annotations[annotations.CustomSubnetAnnotationKey] = "test-subnet"
	result = l4NetLB.EnsureFrontend(nodeNames, svc)
	if result.Error != nil {
		t.Fatalf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	svc.Annotations = result.Annotations
	assertDualStackNetLBResourcesWithCustomIPv6Subnet(t, l4NetLB, nodeNames, "test-subnet")

	// Change to a different subnet
	otherSubnetKey := meta.RegionalKey("another-subnet", l4NetLB.cloud.Region())
	err = l4NetLB.cloud.Compute().(*cloud.MockGCE).Subnetworks().Insert(context.TODO(), otherSubnetKey, subnetToCreate)
	if err != nil {
		t.Fatalf("Failed to create subnet, error: %v", err)
	}
	svc.Annotations[annotations.CustomSubnetAnnotationKey] = "another-subnet"
	result = l4NetLB.EnsureFrontend(nodeNames, svc)
	if result.Error != nil {
		t.Fatalf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	svc.Annotations = result.Annotations
	assertDualStackNetLBResourcesWithCustomIPv6Subnet(t, l4NetLB, nodeNames, "another-subnet")

	// remove the annotation - NetLB should revert to default subnet.
	delete(svc.Annotations, annotations.CustomSubnetAnnotationKey)
	result = l4NetLB.EnsureFrontend(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	svc.Annotations = result.Annotations
	assertDualStackNetLBResourcesWithCustomIPv6Subnet(t, l4NetLB, nodeNames, l4NetLB.cloud.SubnetworkURL())

	// Delete the loadbalancer
	result = l4NetLB.EnsureLoadBalancerDeleted(svc)
	if result.Error != nil {
		t.Errorf("Unexpected error deleting loadbalancer - err %v", result.Error)
	}
	assertDualStackNetLBResourcesDeleted(t, l4NetLB)
}

func TestDualStackNetLBBadCustomSubnet(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}

	testCases := []struct {
		desc                 string
		subnetStackType      string
		subnetIpv6AccessType string
	}{
		{
			desc:            "Should return error on ipv4 subnet",
			subnetStackType: "IPV4",
		},
		{
			desc:                 "Should return error on internal ipv6 subnet",
			subnetIpv6AccessType: subnetInternalIPv6AccessType,
			subnetStackType:      "IPV4_IPV6",
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			svc := test.NewL4NetLBRBSService(8080)
			svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol}
			l4NetLB := mustSetupNetLBTestHandler(t, svc, nodeNames)

			customBadSubnetName := "bad-subnet"
			key := meta.RegionalKey(customBadSubnetName, l4NetLB.cloud.Region())
			subnetToCreate := &ga.Subnetwork{
				Ipv6AccessType: tc.subnetIpv6AccessType,
				StackType:      tc.subnetStackType,
			}
			err := l4NetLB.cloud.Compute().(*cloud.MockGCE).Subnetworks().Insert(context.TODO(), key, subnetToCreate)
			if err != nil {
				t.Fatalf("failed to create subnet %v, error: %v", subnetToCreate, err)
			}

			svc.Annotations[annotations.CustomSubnetAnnotationKey] = customBadSubnetName

			result := l4NetLB.EnsureFrontend(nodeNames, svc)
			if result.Error == nil {
				t.Fatalf("Expected error ensuring external dualstack loadbalancer in bad subnet, got: %v", result.Error)
			}
			if !utils.IsUserError(result.Error) {
				t.Errorf("Expected to get user error if internal IPv6 subnet specified for external IPv6 service, got %v", result.Error)
			}
			if !noExternalIPv6InSubnetError.MatchString(result.Error.Error()) {
				t.Errorf("Expected error to match %v regexp, got %v", noExternalIPv6InSubnetError.String(), result.Error)
			}
		})
	}
}

func TestDualStackNetLBStaticIPAnnotation(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}

	ipv4Address := &ga.Address{
		Name:    "ipv4-address",
		Address: "111.111.111.111",
	}
	ipv6Address := &ga.Address{
		Name:      "ipv6-address",
		Address:   "2::2/80",
		IpVersion: "IPV6",
	}

	testCases := []struct {
		desc                          string
		staticAnnotationVal           string
		addressesToReserve            []*ga.Address
		expectedStaticLoadBalancerIPs []string
	}{
		{
			desc:                          "2 Reserved addresses",
			staticAnnotationVal:           "ipv4-address,ipv6-address",
			addressesToReserve:            []*ga.Address{ipv4Address, ipv6Address},
			expectedStaticLoadBalancerIPs: []string{"111.111.111.111", "2::2"},
		},
		{
			desc:                          "Addresses in annotation, but not reserved",
			staticAnnotationVal:           "ipv4-address,ipv6-address",
			addressesToReserve:            []*ga.Address{},
			expectedStaticLoadBalancerIPs: []string{},
		},
		{
			desc:                          "1 Reserved address, 1 random",
			staticAnnotationVal:           "ipv6-address",
			addressesToReserve:            []*ga.Address{ipv6Address},
			expectedStaticLoadBalancerIPs: []string{"2::2"},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			svc := test.NewL4NetLBRBSService(8080)
			l4NetLB := mustSetupNetLBTestHandler(t, svc, nodeNames)

			for _, addr := range tc.addressesToReserve {
				err := l4NetLB.cloud.ReserveRegionAddress(addr, l4NetLB.cloud.Region())
				if err != nil {
					t.Fatal(err)
				}
			}
			svc.Annotations[annotations.StaticL4AddressesAnnotationKey] = tc.staticAnnotationVal
			svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol}

			result := l4NetLB.EnsureFrontend(nodeNames, svc)
			if result.Error != nil {
				t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
			}
			svc.Annotations = result.Annotations
			assertDualStackNetLBResources(t, l4NetLB, nodeNames)

			var gotIPs []string
			for _, ip := range result.Status.Ingress {
				gotIPs = append(gotIPs, ip.IP)
			}
			if len(gotIPs) != 2 {
				t.Errorf("Expected to get 2 addresses for RequireDualStack Service, got %v", gotIPs)
			}
			for _, expectedAddr := range tc.expectedStaticLoadBalancerIPs {
				if !slices.Contains(gotIPs, expectedAddr) {
					t.Errorf("Expected to find static address %s in load balancer IPs, got %v", expectedAddr, gotIPs)
				}
			}
			// Delete the loadbalancer
			result = l4NetLB.EnsureLoadBalancerDeleted(svc)
			if result.Error != nil {
				t.Errorf("Unexpected error deleting loadbalancer - err %v", result.Error)
			}
			assertDualStackNetLBResourcesDeleted(t, l4NetLB)

			// Verify user reserved addresses were not deleted
			for _, addr := range tc.addressesToReserve {
				cloudAddr, err := l4NetLB.cloud.GetRegionAddress(addr.Name, l4NetLB.cloud.Region())
				if err != nil || cloudAddr == nil {
					t.Errorf("Reserved address should exist after service deletion. Got addr: %v, err: %v", cloudAddr, err)
				}
			}
		})
	}
}

func mustSetupNetLBTestHandler(t *testing.T, svc *v1.Service, nodeNames []string) *L4NetLB {
	t.Helper()

	vals := gce.DefaultTestClusterValues()
	namer := namer_util.NewL4Namer(kubeSystemUID, nil)
	fakeGCE := getFakeGCECloud(vals)

	l4NetLBParams := &L4NetLBParams{
		Service:          svc,
		Cloud:            fakeGCE,
		Namer:            namer,
		Recorder:         record.NewFakeRecorder(100),
		DualStackEnabled: true,
		NetworkResolver:  network.NewFakeResolver(network.DefaultNetwork(fakeGCE)),
	}
	l4NetLB := NewL4NetLB(l4NetLBParams, klog.TODO())
	l4NetLB.healthChecks = healthchecksl4.Fake(fakeGCE, l4NetLBParams.Recorder)

	if _, err := test.CreateAndInsertNodes(l4NetLB.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Fatalf("unexpected error when adding nodes %v", err)
	}

	// Create cluster subnet. Mock GCE uses subnet with empty string name.
	test.MustCreateDualStackClusterSubnet(t, l4NetLB.cloud, subnetExternalIPv6AccessType)
	return l4NetLB
}

func TestCheckStrongSessionAffinityRequirements(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		desc                        string
		enableStrongSessionAffinity bool
		serviceAnnotations          map[string]string
		sessionAffinityType         v1.ServiceAffinity
		sessionAffinityConfig       *v1.SessionAffinityConfig
		expectError                 bool
	}{
		{
			desc:                        "strong session affinity wasn't specified",
			enableStrongSessionAffinity: false,
			serviceAnnotations:          make(map[string]string),
			sessionAffinityConfig:       &v1.SessionAffinityConfig{},
			expectError:                 false,
		},
		{
			desc:                        "strong session affinity doesn't have required flag",
			enableStrongSessionAffinity: false,
			serviceAnnotations: map[string]string{
				annotations.StrongSessionAffinityAnnotationKey: annotations.StrongSessionAffinityEnabled,
			},
			sessionAffinityType:   v1.ServiceAffinityClientIP,
			sessionAffinityConfig: &v1.SessionAffinityConfig{},
			expectError:           true,
		},
		{
			desc:                        "strong session affinity was enabled on cluster but not on service annotation",
			enableStrongSessionAffinity: true,
			serviceAnnotations:          make(map[string]string),
			sessionAffinityType:         v1.ServiceAffinityClientIP,
			sessionAffinityConfig:       &v1.SessionAffinityConfig{},
			expectError:                 false,
		},
		{
			desc:                        "strong session affinity has wrong ServiceAffinity type",
			enableStrongSessionAffinity: true,
			serviceAnnotations: map[string]string{
				annotations.StrongSessionAffinityAnnotationKey: annotations.StrongSessionAffinityEnabled,
			},
			sessionAffinityType:   v1.ServiceAffinityNone,
			sessionAffinityConfig: &v1.SessionAffinityConfig{ClientIP: &v1.ClientIPConfig{TimeoutSeconds: proto.Int32(minStrongSessionAffinityIdleTimeout)}},
			expectError:           true,
		},
		{
			desc:                        "strong session affinity has wrong timeout type",
			enableStrongSessionAffinity: true,
			serviceAnnotations: map[string]string{
				annotations.StrongSessionAffinityAnnotationKey: annotations.StrongSessionAffinityEnabled,
			},
			sessionAffinityType:   v1.ServiceAffinityClientIP,
			sessionAffinityConfig: &v1.SessionAffinityConfig{ClientIP: &v1.ClientIPConfig{TimeoutSeconds: proto.Int32(maxSessionAffinityIdleTimeout + 1)}},
			expectError:           true,
		},
		{
			desc:                        "strong session affinity has empty ClientIPConfig",
			enableStrongSessionAffinity: true,
			serviceAnnotations: map[string]string{
				annotations.StrongSessionAffinityAnnotationKey: annotations.StrongSessionAffinityEnabled,
			},
			sessionAffinityType:   v1.ServiceAffinityClientIP,
			sessionAffinityConfig: &v1.SessionAffinityConfig{ClientIP: &v1.ClientIPConfig{}},
			expectError:           true,
		},
		{
			desc:                        "strong session affinity set up is correct",
			enableStrongSessionAffinity: true,
			serviceAnnotations: map[string]string{
				annotations.StrongSessionAffinityAnnotationKey: annotations.StrongSessionAffinityEnabled,
			},
			sessionAffinityType:   v1.ServiceAffinityClientIP,
			sessionAffinityConfig: &v1.SessionAffinityConfig{ClientIP: &v1.ClientIPConfig{TimeoutSeconds: proto.Int32(minStrongSessionAffinityIdleTimeout)}},
			expectError:           false,
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			service := test.NewL4NetLBRBSService(8080)
			for key, val := range tc.serviceAnnotations {
				service.Annotations[key] = val
			}
			service.Spec.SessionAffinity = tc.sessionAffinityType
			service.Spec.SessionAffinityConfig = tc.sessionAffinityConfig
			l4netlb := NewL4NetLB(&L4NetLBParams{
				Service:                      service,
				StrongSessionAffinityEnabled: tc.enableStrongSessionAffinity,
			}, klog.TODO())

			err := l4netlb.checkStrongSessionAffinityRequirements()
			if tc.expectError != (err != nil) {
				t.Errorf("checkStrongSessionAffinityRequirements returned (%v) but WasErrorExpected=%v", err, tc.expectError)
			}
		})
	}
}

func ensureLoadBalancer(port int, vals gce.TestClusterValues, fakeGCE *gce.Cloud, t *testing.T) (*v1.Service, *L4NetLB) {
	svc := test.NewL4NetLBRBSService(port)
	namer := namer_util.NewL4Namer(kubeSystemUID, namer_util.NewNamer(vals.ClusterName, "cluster-fw", klog.TODO()))
	emptyNodes := []string{}

	l4NetLBParams := &L4NetLBParams{
		Service:         svc,
		Cloud:           fakeGCE,
		Namer:           namer,
		Recorder:        record.NewFakeRecorder(100),
		NetworkResolver: network.NewFakeResolver(network.DefaultNetwork(fakeGCE)),
	}
	l4NetLB := NewL4NetLB(l4NetLBParams, klog.TODO())
	l4NetLB.healthChecks = healthchecksl4.Fake(fakeGCE, l4NetLBParams.Recorder)

	result := l4NetLB.EnsureFrontend(emptyNodes, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4NetLB)
	}
	l4NetLB.Service.Annotations = result.Annotations
	assertNetLBResources(t, l4NetLB, emptyNodes)
	return svc, l4NetLB
}

func assertNetLBResourcesDeleted(t *testing.T, l4netlb *L4NetLB) {
	t.Helper()

	nodesFwName := l4netlb.namer.L4Firewall(l4netlb.Service.Namespace, l4netlb.Service.Name)
	hcFwNameShared := l4netlb.namer.L4HealthCheckFirewall(l4netlb.Service.Namespace, l4netlb.Service.Name, true)
	hcFwNameNonShared := l4netlb.namer.L4HealthCheckFirewall(l4netlb.Service.Namespace, l4netlb.Service.Name, false)

	fwNames := []string{
		nodesFwName,
		hcFwNameShared,
		hcFwNameNonShared,
	}

	for _, fwName := range fwNames {
		err := verifyFirewallNotExists(l4netlb.cloud, fwName)
		if err != nil {
			t.Errorf("verifyFirewallNotExists(_, %s) returned error %v, want nil", fwName, err)
		}
	}

	frName := l4netlb.frName()
	err := verifyForwardingRuleNotExists(l4netlb.cloud, frName)
	if err != nil {
		t.Errorf("verifyForwardingRuleNotExists(_, %s) returned error %v, want nil", frName, err)
	}

	hcNameShared := l4netlb.namer.L4HealthCheck(l4netlb.Service.Namespace, l4netlb.Service.Name, true)
	err = verifyHealthCheckNotExists(l4netlb.cloud, hcNameShared, meta.Regional, klog.TODO())
	if err != nil {
		t.Errorf("verifyHealthCheckNotExists(_, %s)", hcNameShared)
	}

	hcNameNonShared := l4netlb.namer.L4HealthCheck(l4netlb.Service.Namespace, l4netlb.Service.Name, false)
	err = verifyHealthCheckNotExists(l4netlb.cloud, hcNameNonShared, meta.Regional, klog.TODO())
	if err != nil {
		t.Errorf("verifyHealthCheckNotExists(_, %s)", hcNameNonShared)
	}

	err = verifyBackendServiceNotExists(l4netlb.cloud, nodesFwName)
	if err != nil {
		t.Errorf("verifyBackendServiceNotExists(_, %s)", nodesFwName)
	}

	err = verifyAddressNotExists(l4netlb.cloud, frName)
	if err != nil {
		t.Errorf("verifyAddressNotExists(_, %s)", frName)
	}
}

func assertDualStackNetLBResourcesDeleted(t *testing.T, l4netlb *L4NetLB) {
	t.Helper()

	err := verifyNetLBCommonDualStackResourcesDeleted(l4netlb)
	if err != nil {
		t.Errorf("verifyNetLBCommonDualStackResourcesDeleted(_) returned erorr %v, want nil", err)
	}

	err = verifyNetLBIPv4ResourcesDeletedOnSync(l4netlb)
	if err != nil {
		t.Errorf("verifyNetLBIPv4ResourcesDeletedOnSync(_) returned erorr %v, want nil", err)
	}

	err = verifyNetLBIPv6ResourcesDeletedOnSync(l4netlb)
	if err != nil {
		t.Errorf("verifyNetLBIPv6ResourcesDeletedOnSync(_) returned erorr %v, want nil", err)
	}

	// Check health check firewalls separately, because we don't clean them on sync, only on final deletion
	ipv4HcFwNameShared := l4netlb.namer.L4HealthCheckFirewall(l4netlb.Service.Namespace, l4netlb.Service.Name, true)
	ipv6HcFwNameShared := l4netlb.namer.L4IPv6HealthCheckFirewall(l4netlb.Service.Namespace, l4netlb.Service.Name, true)
	ipv4HcFwNameNonShared := l4netlb.namer.L4HealthCheckFirewall(l4netlb.Service.Namespace, l4netlb.Service.Name, false)
	ipv6HcFwNameNonShared := l4netlb.namer.L4IPv6HealthCheckFirewall(l4netlb.Service.Namespace, l4netlb.Service.Name, false)

	fwNames := []string{
		ipv4HcFwNameShared,
		ipv4HcFwNameNonShared,
		ipv6HcFwNameShared,
		ipv6HcFwNameNonShared,
	}

	for _, fwName := range fwNames {
		err = verifyFirewallNotExists(l4netlb.cloud, fwName)
		if err != nil {
			t.Errorf("verifyFirewallNotExists(_, %s) returned error %v, want nil", fwName, err)
		}
	}
}

func TestWeightedNetLB(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		desc                     string
		addAnnotationForWeighted bool
		weightedFlagEnabled      bool
		externalTrafficPolicy    v1.ServiceExternalTrafficPolicy
		wantWeighted             bool
	}{
		{
			desc:                     "Flag enabled, Service with weighted annotation, externalTrafficPolicy local",
			addAnnotationForWeighted: true,
			weightedFlagEnabled:      true,
			externalTrafficPolicy:    v1.ServiceExternalTrafficPolicyTypeLocal,
			wantWeighted:             true,
		},
		{
			desc:                     "Flag enabled, NO weighted annotation, externalTrafficPolicy local",
			addAnnotationForWeighted: false,
			weightedFlagEnabled:      true,
			externalTrafficPolicy:    v1.ServiceExternalTrafficPolicyTypeLocal,
			wantWeighted:             false,
		},
		{
			desc:                     "Flag DISABLED, Service with weighted annotation, externalTrafficPolicy local",
			addAnnotationForWeighted: true,
			weightedFlagEnabled:      false,
			externalTrafficPolicy:    v1.ServiceExternalTrafficPolicyTypeLocal,
			wantWeighted:             false,
		},
		{
			desc:                     "Flag enabled, Service with weighted annotation and externalTrafficPolicy CLUSTER",
			addAnnotationForWeighted: true,
			weightedFlagEnabled:      true,
			externalTrafficPolicy:    v1.ServiceExternalTrafficPolicyTypeCluster,
			wantWeighted:             false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			svc := test.NewL4NetLBRBSService(8080)
			svc.Spec.ExternalTrafficPolicy = tc.externalTrafficPolicy
			if tc.addAnnotationForWeighted {
				svc.Annotations[annotations.WeightedL4AnnotationKey] = annotations.WeightedL4AnnotationEnabled
			}

			nodeNames := []string{"test-node-1"}

			l4NetLB := mustSetupNetLBTestHandler(t, svc, nodeNames)
			l4NetLB.enableWeightedLB = tc.weightedFlagEnabled

			result := l4NetLB.EnsureFrontend(nodeNames, svc)
			if result.Error != nil {
				t.Fatalf("Failed to ensure loadBalancer, err %v", result.Error)
			}

			backendServiceName := l4NetLB.namer.L4Backend(l4NetLB.Service.Namespace, l4NetLB.Service.Name)
			key := meta.RegionalKey(backendServiceName, l4NetLB.cloud.Region())
			bs, err := composite.GetBackendService(l4NetLB.cloud, key, meta.VersionGA, klog.TODO())

			if err != nil {
				t.Fatalf("failed to read BackendService, %v", err)
			}
			backedHasWeighted := (bs.LocalityLbPolicy == backends.WeightedLocalityLbPolicy)
			if tc.wantWeighted != backedHasWeighted {
				t.Errorf("Enexpected BackendService LocalityLbPolicy value %v, got weighted: %v, want weighted: %v", bs.LocalityLbPolicy, tc.wantWeighted, backedHasWeighted)
			}
		})
	}
}

func verifyNetLBCommonDualStackResourcesDeleted(l4netlb *L4NetLB) error {
	backendServiceName := l4netlb.namer.L4Backend(l4netlb.Service.Namespace, l4netlb.Service.Name)

	err := verifyBackendServiceNotExists(l4netlb.cloud, backendServiceName)
	if err != nil {
		return fmt.Errorf("verifyBackendServiceNotExists(_, %s)", backendServiceName)
	}

	hcNameShared := l4netlb.namer.L4HealthCheck(l4netlb.Service.Namespace, l4netlb.Service.Name, true)
	err = verifyHealthCheckNotExists(l4netlb.cloud, hcNameShared, meta.Regional, klog.TODO())
	if err != nil {
		return fmt.Errorf("verifyHealthCheckNotExists(_, %s)", hcNameShared)
	}

	hcNameNonShared := l4netlb.namer.L4HealthCheck(l4netlb.Service.Namespace, l4netlb.Service.Name, false)
	err = verifyHealthCheckNotExists(l4netlb.cloud, hcNameNonShared, meta.Regional, klog.TODO())
	if err != nil {
		return fmt.Errorf("verifyHealthCheckNotExists(_, %s)", hcNameNonShared)
	}

	err = verifyAddressNotExists(l4netlb.cloud, backendServiceName)
	if err != nil {
		return fmt.Errorf("verifyAddressNotExists(_, %s)", backendServiceName)
	}
	return nil
}

func assertNetLBResources(t *testing.T, l4NetLB *L4NetLB, nodeNames []string) {
	t.Helper()

	err := verifyNetLBIPv4NodesFirewall(l4NetLB, nodeNames)
	if err != nil {
		t.Errorf("verifyNetLBIPv4NodesFirewall(_, %v) returned error %v, want nil", nodeNames, err)
	}

	err = verifyNetLBIPv4HealthCheckFirewall(l4NetLB, nodeNames)
	if err != nil {
		t.Errorf("verifyNetLBIPv4HealthCheckFirewall(_, %v) returned error %v, want nil", nodeNames, err)
	}

	// Check that HealthCheck is created
	healthcheck, err := getAndVerifyNetLBHealthCheck(l4NetLB)
	if err != nil {
		t.Errorf("getAndVerifyNetLBHealthCheck(_) returned error %v, want nil", err)
	}

	backendService, err := getAndVerifyNetLBBackendService(l4NetLB, healthcheck)
	if err != nil {
		t.Errorf("getAndVerifyNetLBBackendService(_, %v) returned error %v, want nil", healthcheck, err)
	}

	err = verifyNetLBIPv4ForwardingRule(l4NetLB, backendService.SelfLink)
	if err != nil {
		t.Errorf("verifyNetLBIPv4ForwardingRule(_, %s) returned error %v, want nil", backendService.SelfLink, err)
	}

	expectedAnnotations := buildExpectedNetLBAnnotations(l4NetLB)
	if !reflect.DeepEqual(expectedAnnotations, l4NetLB.Service.Annotations) {
		diff := cmp.Diff(expectedAnnotations, l4NetLB.Service.Annotations)
		t.Errorf("Expected annotations %v, got %v, diff %v", expectedAnnotations, l4NetLB.Service.Annotations, diff)
	}
}
func assertDualStackNetLBResources(t *testing.T, l4NetLB *L4NetLB, nodeNames []string) {
	t.Helper()
	assertDualStackNetLBResourcesWithCustomIPv6Subnet(t, l4NetLB, nodeNames, l4NetLB.cloud.SubnetworkURL())
}

func assertDualStackNetLBResourcesWithCustomIPv6Subnet(t *testing.T, l4NetLB *L4NetLB, nodeNames []string, expectedIPv6Subnet string) {
	t.Helper()

	// Check that HealthCheck is created
	healthCheck, err := getAndVerifyNetLBHealthCheck(l4NetLB)
	if err != nil {
		t.Errorf("getAndVerifyNetLBHealthCheck(_) returned error %v, want nil", err)
	}

	backendService, err := getAndVerifyNetLBBackendService(l4NetLB, healthCheck)
	if err != nil {
		t.Fatalf("getAndVerifyNetLBBackendService(_, %v) returned error %v, want nil", healthCheck, err)
	}

	if utils.NeedsIPv4(l4NetLB.Service) {
		err = verifyNetLBIPv4ForwardingRule(l4NetLB, backendService.SelfLink)
		if err != nil {
			t.Errorf("verifyNetLBIPv4ForwardingRule(_, %s) returned error %v, want nil", backendService.SelfLink, err)
		}

		err = verifyNetLBIPv4NodesFirewall(l4NetLB, nodeNames)
		if err != nil {
			t.Errorf("verifyNetLBIPv4NodesFirewall(_, %v) returned error %v, want nil", nodeNames, err)
		}

		err = verifyNetLBIPv4HealthCheckFirewall(l4NetLB, nodeNames)
		if err != nil {
			t.Errorf("verifyNetLBIPv4HealthCheckFirewall(_, %v) returned error %v, want nil", nodeNames, err)
		}
	} else {
		err = verifyNetLBIPv4ResourcesDeletedOnSync(l4NetLB)
		if err != nil {
			t.Errorf("verifyNetLBIPv4ResourcesDeletedOnSync(_) returned error %v, want nil", err)
		}
	}
	if utils.NeedsIPv6(l4NetLB.Service) {
		err = verifyNetLBIPv6ForwardingRule(l4NetLB, backendService.SelfLink, expectedIPv6Subnet)
		if err != nil {
			t.Errorf("verifyNetLBIPv6ForwardingRule(_, %s) returned error %v, want nil", backendService.SelfLink, err)
		}

		err = verifyNetLBIPv6NodesFirewall(l4NetLB, nodeNames)
		if err != nil {
			t.Errorf("verifyNetLBIPv6NodesFirewall(_, %v) returned error %v, want nil", nodeNames, err)
		}

		err = verifyNetLBIPv6HealthCheckFirewall(l4NetLB, nodeNames)
		if err != nil {
			t.Errorf("verifyNetLBIPv6HealthCheckFirewall(_, %v) returned error %v, want nil", nodeNames, err)
		}
	} else {
		err = verifyNetLBIPv6ResourcesDeletedOnSync(l4NetLB)
		if err != nil {
			t.Errorf("verifyNetLBIPv6ResourcesDeletedOnSync(_) returned error %v, want nil", err)
		}
	}

	expectedAnnotations := buildExpectedNetLBAnnotations(l4NetLB)
	if !reflect.DeepEqual(expectedAnnotations, l4NetLB.Service.Annotations) {
		diff := cmp.Diff(expectedAnnotations, l4NetLB.Service.Annotations)
		t.Errorf("Expected annotations %v, got %v, diff %v", expectedAnnotations, l4NetLB.Service.Annotations, diff)
	}
}

func verifyNetLBIPv4NodesFirewall(l4netlb *L4NetLB, nodeNames []string) error {
	fwName := l4netlb.namer.L4Firewall(l4netlb.Service.Namespace, l4netlb.Service.Name)
	fwDesc, err := utils.MakeL4LBServiceDescription(utils.ServiceKeyFunc(l4netlb.Service.Namespace, l4netlb.Service.Name), "", meta.VersionGA, false, utils.XLB)
	if err != nil {
		return fmt.Errorf("failed to create description for resources, err %w", err)
	}

	sourceRanges, err := utils.IPv4ServiceSourceRanges(l4netlb.Service)
	if err != nil {
		return fmt.Errorf("servicehelper.GetLoadBalancerSourceRanges(%+v) returned error %v, want nil", l4netlb.Service, err)
	}
	return verifyFirewall(l4netlb.cloud, nodeNames, fwName, fwDesc, sourceRanges, l4netlb.networkInfo.NetworkURL)
}

func verifyNetLBIPv6NodesFirewall(l4netlb *L4NetLB, nodeNames []string) error {
	ipv6FirewallName := l4netlb.namer.L4IPv6Firewall(l4netlb.Service.Namespace, l4netlb.Service.Name)

	fwDesc, err := utils.MakeL4LBServiceDescription(utils.ServiceKeyFunc(l4netlb.Service.Namespace, l4netlb.Service.Name), "", meta.VersionGA, false, utils.XLB)
	if err != nil {
		return fmt.Errorf("failed to create description for resources, err %w", err)
	}

	sourceRanges, err := utils.IPv6ServiceSourceRanges(l4netlb.Service)
	if err != nil {
		return fmt.Errorf("servicehelper.GetLoadBalancerSourceRanges(%+v) returned error %v, want nil", l4netlb.Service, err)
	}
	return verifyFirewall(l4netlb.cloud, nodeNames, ipv6FirewallName, fwDesc, sourceRanges, l4netlb.networkInfo.NetworkURL)
}

func verifyNetLBIPv4HealthCheckFirewall(l4netlb *L4NetLB, nodeNames []string) error {
	isSharedHC := !servicehelper.RequestsOnlyLocalTraffic(l4netlb.Service)

	hcFwName := l4netlb.namer.L4HealthCheckFirewall(l4netlb.Service.Namespace, l4netlb.Service.Name, isSharedHC)
	hcFwDesc, err := utils.MakeL4LBFirewallDescription(utils.ServiceKeyFunc(l4netlb.Service.Namespace, l4netlb.Service.Name), "", meta.VersionGA, isSharedHC)
	if err != nil {
		return fmt.Errorf("failed to calculate decsription for health check for service %v, error %v", l4netlb.Service, err)
	}

	return verifyFirewall(l4netlb.cloud, nodeNames, hcFwName, hcFwDesc, gce.L4LoadBalancerSrcRanges(), l4netlb.networkInfo.NetworkURL)
}

func verifyNetLBIPv6HealthCheckFirewall(l4netlb *L4NetLB, nodeNames []string) error {
	isSharedHC := !servicehelper.RequestsOnlyLocalTraffic(l4netlb.Service)

	ipv6hcFwName := l4netlb.namer.L4IPv6HealthCheckFirewall(l4netlb.Service.Namespace, l4netlb.Service.Name, isSharedHC)
	hcFwDesc, err := utils.MakeL4LBFirewallDescription(utils.ServiceKeyFunc(l4netlb.Service.Namespace, l4netlb.Service.Name), "", meta.VersionGA, isSharedHC)
	if err != nil {
		return fmt.Errorf("failed to calculate decsription for health check for service %v, error %v", l4netlb.Service, err)
	}

	return verifyFirewall(l4netlb.cloud, nodeNames, ipv6hcFwName, hcFwDesc, []string{healthchecksl4.L4NetLBIPv6HCRange}, l4netlb.networkInfo.NetworkURL)
}

func getAndVerifyNetLBHealthCheck(l4netlb *L4NetLB) (*composite.HealthCheck, error) {
	isSharedHC := !servicehelper.RequestsOnlyLocalTraffic(l4netlb.Service)
	hcName := l4netlb.namer.L4HealthCheck(l4netlb.Service.Namespace, l4netlb.Service.Name, isSharedHC)

	healthcheck, err := composite.GetHealthCheck(l4netlb.cloud, meta.RegionalKey(hcName, l4netlb.cloud.Region()), meta.VersionGA, klog.TODO())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch healthcheck %s - err %w", hcName, err)
	}

	if healthcheck.Name != hcName {
		return nil, fmt.Errorf("unexpected name for healthcheck '%s' - expected '%s'", healthcheck.Name, hcName)
	}

	expectedDesc, err := utils.MakeL4LBServiceDescription(utils.ServiceKeyFunc(l4netlb.Service.Namespace, l4netlb.Service.Name), "", meta.VersionGA, isSharedHC, utils.XLB)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate Health Check description")
	}
	if healthcheck.Description != expectedDesc {
		return nil, fmt.Errorf("unexpected description in healthcheck - Expected %s, Got %s", healthcheck.Description, expectedDesc)
	}
	return healthcheck, nil
}

func getAndVerifyNetLBBackendService(l4netlb *L4NetLB, healthCheck *composite.HealthCheck) (*composite.BackendService, error) {
	backendServiceName := l4netlb.namer.L4Backend(l4netlb.Service.Namespace, l4netlb.Service.Name)
	key := meta.RegionalKey(backendServiceName, l4netlb.cloud.Region())
	bs, err := composite.GetBackendService(l4netlb.cloud, key, meta.VersionGA, klog.TODO())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch backend service %s - err %w", backendServiceName, err)
	}
	proto := utils.GetProtocol(l4netlb.Service.Spec.Ports)
	if bs.Protocol != string(proto) {
		return nil, fmt.Errorf("unexpected protocol '%s' for backend service %v", bs.Protocol, bs)
	}
	backendServiceLink := cloud.SelfLink(meta.VersionGA, l4netlb.cloud.ProjectID(), "backendServices", key)
	if bs.SelfLink != backendServiceLink {
		return nil, fmt.Errorf("unexpected self link in backend service - Expected %s, Got %s", bs.SelfLink, backendServiceLink)
	}

	resourceDesc, err := utils.MakeL4LBServiceDescription(utils.ServiceKeyFunc(l4netlb.Service.Namespace, l4netlb.Service.Name), "", meta.VersionGA, false, utils.XLB)
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

func verifyNetLBIPv4ForwardingRule(l4netlb *L4NetLB, backendServiceLink string) error {
	frName := l4netlb.frName()
	return verifyNetLBForwardingRule(l4netlb, frName, backendServiceLink, "")
}

func verifyNetLBIPv6ForwardingRule(l4netlb *L4NetLB, backendServiceLink string, expectedSubnet string) error {
	ipv6FrName := l4netlb.ipv6FRName()
	return verifyNetLBForwardingRule(l4netlb, ipv6FrName, backendServiceLink, expectedSubnet)
}

func verifyNetLBForwardingRule(l4netlb *L4NetLB, frName string, backendServiceLink string, expectedSubnet string) error {
	fwdRule, err := composite.GetForwardingRule(l4netlb.cloud, meta.RegionalKey(frName, l4netlb.cloud.Region()), meta.VersionGA, klog.TODO())
	if err != nil {
		return fmt.Errorf("failed to fetch forwarding rule %s - err %w", frName, err)
	}
	if fwdRule.Name != frName {
		return fmt.Errorf("unexpected name for forwarding rule '%s' - expected '%s'", fwdRule.Name, frName)
	}
	if fwdRule.LoadBalancingScheme != string(cloud.SchemeExternal) {
		return fmt.Errorf("unexpected LoadBalancingScheme for forwarding rule '%s' - expected '%s'", fwdRule.LoadBalancingScheme, cloud.SchemeExternal)
	}

	proto := utils.GetProtocol(l4netlb.Service.Spec.Ports)
	if fwdRule.IPProtocol != string(proto) {
		return fmt.Errorf("unexpected protocol '%s' for forwarding rule %v", fwdRule.IPProtocol, fwdRule)
	}

	if fwdRule.BackendService != backendServiceLink {
		return fmt.Errorf("unexpected backend service link '%s' for forwarding rule, expected '%s'", fwdRule.BackendService, backendServiceLink)
	}

	serviceNetTier, _ := utils.GetNetworkTier(l4netlb.Service)
	if fwdRule.NetworkTier != serviceNetTier.ToGCEValue() {
		return fmt.Errorf("unexpected network tier '%s' for forwarding rule, expected '%s'", fwdRule.NetworkTier, serviceNetTier.ToGCEValue())
	}

	addr, err := l4netlb.cloud.GetRegionAddress(frName, l4netlb.cloud.Region())
	if err == nil || addr != nil {
		return fmt.Errorf("expected error when looking up ephemeral address, got %v", addr)
	}

	if !strings.HasSuffix(fwdRule.Subnetwork, expectedSubnet) {
		return fmt.Errorf("fwdRule.Subnetwork = %s, expectedSubnet = %s. Exepected suffixes to match", fwdRule.Subnetwork, expectedSubnet)
	}

	return nil
}

// we don't delete ipv4 health check firewall on sync
func verifyNetLBIPv4ResourcesDeletedOnSync(l4netlb *L4NetLB) error {
	ipv4FwName := l4netlb.namer.L4Backend(l4netlb.Service.Namespace, l4netlb.Service.Name)
	err := verifyFirewallNotExists(l4netlb.cloud, ipv4FwName)
	if err != nil {
		return fmt.Errorf("verifyFirewallNotExists(_, %s) returned error %w, want nil", ipv4FwName, err)
	}

	ipv4FrName := l4netlb.frName()
	err = verifyForwardingRuleNotExists(l4netlb.cloud, ipv4FrName)
	if err != nil {
		return fmt.Errorf("verifyForwardingRuleNotExists(_, %s) returned error %w, want nil", ipv4FrName, err)
	}

	addressName := ipv4FwName
	err = verifyAddressNotExists(l4netlb.cloud, addressName)
	if err != nil {
		return fmt.Errorf("verifyAddressNotExists(_, %s)", addressName)
	}

	return nil
}

// we don't delete ipv6 health check firewall on sync
func verifyNetLBIPv6ResourcesDeletedOnSync(l4netlb *L4NetLB) error {
	ipv6FwName := l4netlb.namer.L4IPv6Firewall(l4netlb.Service.Namespace, l4netlb.Service.Name)
	err := verifyFirewallNotExists(l4netlb.cloud, ipv6FwName)
	if err != nil {
		return fmt.Errorf("verifyFirewallNotExists(_, %s) returned error %w, want nil", ipv6FwName, err)
	}

	ipv6FrName := l4netlb.ipv6FRName()
	err = verifyForwardingRuleNotExists(l4netlb.cloud, ipv6FrName)
	if err != nil {
		return fmt.Errorf("verifyForwardingRuleNotExists(_, %s) returned error %w, want nil", ipv6FrName, err)
	}

	return nil
}

func buildExpectedNetLBAnnotations(l4netlb *L4NetLB) map[string]string {
	isSharedHC := !servicehelper.RequestsOnlyLocalTraffic(l4netlb.Service)
	proto := utils.GetProtocol(l4netlb.Service.Spec.Ports)

	backendName := l4netlb.namer.L4Backend(l4netlb.Service.Namespace, l4netlb.Service.Name)
	hcName := l4netlb.namer.L4HealthCheck(l4netlb.Service.Namespace, l4netlb.Service.Name, isSharedHC)

	expectedAnnotations := map[string]string{
		annotations.BackendServiceKey: backendName,
		annotations.HealthcheckKey:    hcName,
	}

	if utils.NeedsIPv4(l4netlb.Service) {
		hcFwName := l4netlb.namer.L4HealthCheckFirewall(l4netlb.Service.Namespace, l4netlb.Service.Name, isSharedHC)

		expectedAnnotations[annotations.FirewallRuleForHealthcheckKey] = hcFwName
		expectedAnnotations[annotations.FirewallRuleKey] = backendName

		ipv4FRName := l4netlb.frName()
		if proto == v1.ProtocolTCP {
			expectedAnnotations[annotations.TCPForwardingRuleKey] = ipv4FRName
		} else {
			expectedAnnotations[annotations.UDPForwardingRuleKey] = ipv4FRName
		}
	}
	if utils.NeedsIPv6(l4netlb.Service) {
		ipv6hcFwName := l4netlb.namer.L4IPv6HealthCheckFirewall(l4netlb.Service.Namespace, l4netlb.Service.Name, isSharedHC)
		ipv6FirewallName := l4netlb.namer.L4IPv6Firewall(l4netlb.Service.Namespace, l4netlb.Service.Name)

		expectedAnnotations[annotations.FirewallRuleForHealthcheckIPv6Key] = ipv6hcFwName
		expectedAnnotations[annotations.FirewallRuleIPv6Key] = ipv6FirewallName

		ipv6FRName := l4netlb.ipv6FRName()
		if proto == v1.ProtocolTCP {
			expectedAnnotations[annotations.TCPForwardingRuleIPv6Key] = ipv6FRName
		} else {
			expectedAnnotations[annotations.UDPForwardingRuleIPv6Key] = ipv6FRName
		}
	}
	if val, ok := l4netlb.Service.Annotations[annotations.CustomSubnetAnnotationKey]; ok {
		expectedAnnotations[annotations.CustomSubnetAnnotationKey] = val
	}
	if val, ok := l4netlb.Service.Annotations[annotations.NetworkTierAnnotationKey]; ok {
		expectedAnnotations[annotations.NetworkTierAnnotationKey] = val
	}
	return expectedAnnotations
}

func createUserStaticIPInStandardTier(fakeGCE *gce.Cloud, region string) {
	fakeGCE.Compute().(*cloud.MockGCE).MockAddresses.InsertHook = mock.InsertAddressHook
	fakeGCE.Compute().(*cloud.MockGCE).MockAlphaAddresses.X = mock.AddressAttributes{}
	fakeGCE.Compute().(*cloud.MockGCE).MockAddresses.X = mock.AddressAttributes{}
	newAddr := &ga.Address{
		Name:        "userAddrName",
		Description: fmt.Sprintf(`{"kubernetes.io/service-name":"%s"}`, "userAddrName"),
		Address:     usersIP,
		AddressType: string(cloud.SchemeExternal),
		NetworkTier: cloud.NetworkTierStandard.ToGCEValue(),
	}
	fakeGCE.ReserveRegionAddress(newAddr, region)
}

func createUserStaticIPInPremiumTier(fakeGCE *gce.Cloud, region string) {
	fakeGCE.Compute().(*cloud.MockGCE).MockAddresses.InsertHook = mock.InsertAddressHook
	fakeGCE.Compute().(*cloud.MockGCE).MockAlphaAddresses.X = mock.AddressAttributes{}
	fakeGCE.Compute().(*cloud.MockGCE).MockAddresses.X = mock.AddressAttributes{}
	newAddr := &ga.Address{
		Name:        "userAddrNamePremium",
		Description: fmt.Sprintf(`{"kubernetes.io/service-name":"%s"}`, "userAddrName"),
		Address:     usersIPPremium,
		AddressType: string(cloud.SchemeExternal),
		NetworkTier: cloud.NetworkTierPremium.ToGCEValue(),
	}
	fakeGCE.ReserveRegionAddress(newAddr, region)
}

func checkMetrics(m metrics.L4NetLBServiceLegacyState, isManaged, isPremium, isUserError bool) error {
	if m.IsPremiumTier != isPremium {
		return fmt.Errorf("L4 NetLB metric premium tier should be %v", isPremium)
	}
	if m.IsManagedIP != isManaged {
		return fmt.Errorf("L4 NetLB metric is managed ip should be %v", isManaged)
	}
	if m.IsUserError != isUserError {
		return fmt.Errorf("L4 NetLB metric is user error should be %v", isUserError)
	}
	return nil
}

func assertAddressOldReservedHook(t *testing.T, gceCloud *gce.Cloud) func(ctx context.Context, key *meta.Key, m *cloud.MockForwardingRules, options ...cloud.Option) (bool, error) {
	mockGCE := gceCloud.Compute().(*cloud.MockGCE)
	return func(ctx context.Context, key *meta.Key, _ *cloud.MockForwardingRules, _ ...cloud.Option) (bool, error) {
		fr, err := mockGCE.MockForwardingRules.Get(ctx, key)
		// if forwarding rule not exists, don't need to check if address reserved
		if utils.IsNotFoundError(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}

		addr, err := gceCloud.GetRegionAddressByIP(fr.Region, fr.IPAddress)
		if utils.IgnoreHTTPNotFound(err) != nil {
			return true, err
		}
		if addr == nil || utils.IsNotFoundError(err) {
			t.Errorf("Address not reserved before deleting forwarding rule +%v", fr)
		}

		return false, nil
	}
}
