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
	"reflect"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	ga "google.golang.org/api/compute/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
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
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	usersIP        = "35.10.211.60"
	usersIPPremium = "35.10.211.70"
)

func TestEnsureL4NetLoadBalancer(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}
	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	svc := test.NewL4NetLBRBSService(8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, namer_util.NewNamer(vals.ClusterName, "cluster-fw"))

	l4NetLBParams := &L4NetLBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4netlb := NewL4NetLB(l4NetLBParams)
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
	if err := checkAnnotations(result, l4netlb); err != nil {
		t.Errorf("Annotations error: %v", err)
	}
	assertNetLBResources(t, l4netlb, nodeNames)
	if err := checkMetrics(result.MetricsState /*isManaged = */, true /*isPremium = */, true /*isUserError =*/, false); err != nil {
		t.Errorf("Metrics error: %v", err)
	}
}

func checkAnnotations(result *L4NetLBSyncResult, l4netlb *L4NetLB) error {
	expBackendName := l4netlb.namer.L4Backend(l4netlb.Service.Namespace, l4netlb.Service.Name)
	if result.Annotations[annotations.BackendServiceKey] != expBackendName {
		return fmt.Errorf("BackendServiceKey mismatch %v != %v", expBackendName, result.Annotations[annotations.BackendServiceKey])
	}
	expTcpFR := l4netlb.GetFRName()
	if result.Annotations[annotations.TCPForwardingRuleKey] != expTcpFR {
		return fmt.Errorf("TCPForwardingRuleKey mismatch %v != %v", expTcpFR, result.Annotations[annotations.TCPForwardingRuleKey])
	}
	expFwRule := expBackendName
	if result.Annotations[annotations.FirewallRuleKey] != expFwRule {
		return fmt.Errorf("FirewallRuleKey mismatch %v != %v", expFwRule, result.Annotations[annotations.FirewallRuleKey])
	}
	expHcFwName := l4netlb.namer.L4HealthCheckFirewall(l4netlb.Service.Namespace, l4netlb.Service.Name, true)
	if result.Annotations[annotations.FirewallRuleForHealthcheckKey] != expHcFwName {
		return fmt.Errorf("FirewallRuleForHealthcheckKey mismatch %v != %v", expHcFwName, result.Annotations[annotations.FirewallRuleForHealthcheckKey])
	}
	return nil
}

func TestDeleteL4NetLoadBalancer(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}
	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	svc := test.NewL4NetLBRBSService(8080)
	namer := namer_util.NewL4Namer(kubeSystemUID, namer_util.NewNamer(vals.ClusterName, "cluster-fw"))

	l4NetLBParams := &L4NetLBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4NetLB := NewL4NetLB(l4NetLBParams)
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
	namer := namer_util.NewL4Namer(kubeSystemUID, namer_util.NewNamer(vals.ClusterName, "cluster-fw"))

	// Create ILB service
	_, l4ilb, ilbResult := ensureService(fakeGCE, namer, nodeNames, vals.ZoneName, 8080, t)
	if ilbResult != nil && ilbResult.Error != nil {
		t.Fatalf("Error ensuring service err: %v", ilbResult.Error)
	}

	// Create NetLB Service
	netlbSvc := test.NewL4NetLBRBSService(8080)
	l4NetlbParams := &L4NetLBParams{
		Service:  netlbSvc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4NetLB := NewL4NetLB(l4NetlbParams)

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
	_, err = composite.GetHealthCheck(l4NetLB.cloud, meta.RegionalKey(hcName, l4NetLB.cloud.Region()), meta.VersionGA)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("Healthcheck %s should be deleted", hcName)
	}

}

func ensureLoadBalancer(port int, vals gce.TestClusterValues, fakeGCE *gce.Cloud, t *testing.T) (*v1.Service, *L4NetLB) {
	svc := test.NewL4NetLBRBSService(port)
	namer := namer_util.NewL4Namer(kubeSystemUID, namer_util.NewNamer(vals.ClusterName, "cluster-fw"))
	emptyNodes := []string{}

	l4NetLBParams := &L4NetLBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4NetLB := NewL4NetLB(l4NetLBParams)
	l4NetLB.healthChecks = healthchecksl4.Fake(fakeGCE, l4NetLB.recorder)

	result := l4NetLB.EnsureFrontend(emptyNodes, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4NetLB)
	}
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

	frName := l4netlb.GetFRName()
	err := verifyForwardingRuleNotExists(l4netlb.cloud, frName)
	if err != nil {
		t.Errorf("verifyForwardingRuleNotExists(_, %s) returned error %v, want nil", frName, err)
	}

	hcNameShared := l4netlb.namer.L4HealthCheck(l4netlb.Service.Namespace, l4netlb.Service.Name, true)
	err = verifyHealthCheckNotExists(l4netlb.cloud, hcNameShared, meta.Regional)
	if err != nil {
		t.Errorf("verifyHealthCheckNotExists(_, %s)", hcNameShared)
	}

	hcNameNonShared := l4netlb.namer.L4HealthCheck(l4netlb.Service.Namespace, l4netlb.Service.Name, false)
	err = verifyHealthCheckNotExists(l4netlb.cloud, hcNameNonShared, meta.Regional)
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

func assertNetLBResources(t *testing.T, l4NetLB *L4NetLB, nodeNames []string) {
	t.Helper()

	err := verifyNetLBNodesFirewall(l4NetLB, nodeNames)
	if err != nil {
		t.Errorf("verifyNetLBNodesFirewall(_, %v) returned error %v, want nil", nodeNames, err)
	}

	err = verifyNetLBHealthCheckFirewall(l4NetLB, nodeNames)
	if err != nil {
		t.Errorf("verifyNetLBHealthCheckFirewall(_, %v) returned error %v, want nil", nodeNames, err)
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

	err = verifyNetLBForwardingRule(l4NetLB, backendService.SelfLink)
	if err != nil {
		t.Errorf("verifyNetLBForwardingRule(_, %s) returned error %v, want nil", backendService.SelfLink, err)
	}
}

func verifyNetLBNodesFirewall(l4netlb *L4NetLB, nodeNames []string) error {
	fwName := l4netlb.namer.L4Firewall(l4netlb.Service.Namespace, l4netlb.Service.Name)
	fwDesc, err := utils.MakeL4LBServiceDescription(utils.ServiceKeyFunc(l4netlb.Service.Namespace, l4netlb.Service.Name), "", meta.VersionGA, false, utils.XLB)
	if err != nil {
		return fmt.Errorf("failed to create description for resources, err %w", err)
	}

	sourceRanges, err := utils.IPv4ServiceSourceRanges(l4netlb.Service)
	if err != nil {
		return fmt.Errorf("servicehelper.GetLoadBalancerSourceRanges(%+v) returned error %v, want nil", l4netlb.Service, err)
	}
	return verifyFirewall(l4netlb.cloud, nodeNames, fwName, fwDesc, sourceRanges)
}

func verifyNetLBHealthCheckFirewall(l4netlb *L4NetLB, nodeNames []string) error {
	isSharedHC := !servicehelper.RequestsOnlyLocalTraffic(l4netlb.Service)

	hcFwName := l4netlb.namer.L4HealthCheckFirewall(l4netlb.Service.Namespace, l4netlb.Service.Name, isSharedHC)
	hcFwDesc, err := utils.MakeL4LBFirewallDescription(utils.ServiceKeyFunc(l4netlb.Service.Namespace, l4netlb.Service.Name), "", meta.VersionGA, isSharedHC)
	if err != nil {
		return fmt.Errorf("failed to calculate decsription for health check for service %v, error %v", l4netlb.Service, err)
	}

	return verifyFirewall(l4netlb.cloud, nodeNames, hcFwName, hcFwDesc, gce.L4LoadBalancerSrcRanges())
}

func getAndVerifyNetLBHealthCheck(l4netlb *L4NetLB) (*composite.HealthCheck, error) {
	isSharedHC := !servicehelper.RequestsOnlyLocalTraffic(l4netlb.Service)
	hcName := l4netlb.namer.L4HealthCheck(l4netlb.Service.Namespace, l4netlb.Service.Name, isSharedHC)

	healthcheck, err := composite.GetHealthCheck(l4netlb.cloud, meta.RegionalKey(hcName, l4netlb.cloud.Region()), meta.VersionGA)
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
	bs, err := composite.GetBackendService(l4netlb.cloud, key, meta.VersionGA)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch backend service %s - err %w", backendServiceName, err)
	}
	proto := utils.GetProtocol(l4netlb.Service.Spec.Ports)
	if bs.Protocol != string(proto) {
		return nil, fmt.Errorf("unexpected protocol '%s' for backend service %v", bs.Protocol, bs)
	}
	backendServiceLink := cloud.SelfLink(meta.VersionGA, l4netlb.cloud.ProjectID(), "backendServices", key)
	if bs.SelfLink != backendServiceLink {
		return nil, fmt.Errorf("unexpected self link in backend service with name %s - Expected %s, Got %s", backendServiceName, bs.SelfLink, backendServiceLink)
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

func verifyNetLBForwardingRule(l4netlb *L4NetLB, backendServiceLink string) error {
	frName := l4netlb.GetFRName()
	fwdRule, err := composite.GetForwardingRule(l4netlb.cloud, meta.RegionalKey(frName, l4netlb.cloud.Region()), meta.VersionGA)
	if err != nil {
		return fmt.Errorf("failed to fetch forwarding rule %s - err %w", frName, err)
	}
	if fwdRule.LoadBalancingScheme != string(cloud.SchemeExternal) {
		return fmt.Errorf("unexpected LoadBalancingScheme for forwarding rule '%s' - expected '%s'", fwdRule.LoadBalancingScheme, cloud.SchemeExternal)
	}

	proto := utils.GetProtocol(l4netlb.Service.Spec.Ports)
	if fwdRule.IPProtocol != string(proto) {
		return fmt.Errorf("expected protocol '%s', got '%s' for forwarding rule %v", proto, fwdRule.IPProtocol, fwdRule)
	}

	if fwdRule.BackendService != backendServiceLink {
		return fmt.Errorf("unexpected backend service link '%s' for forwarding rule, expected '%s'", fwdRule.BackendService, backendServiceLink)
	}

	addr, err := l4netlb.cloud.GetRegionAddress(frName, l4netlb.cloud.Region())
	if err == nil || addr != nil {
		return fmt.Errorf("expected error when looking up ephemeral address, got %v", addr)
	}
	return nil
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
	namer := namer_util.NewL4Namer(kubeSystemUID, namer_util.NewNamer(vals.ClusterName, "cluster-fw"))

	l4NetLBParams := &L4NetLBParams{
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4netlb := NewL4NetLB(l4NetLBParams)
	l4netlb.healthChecks = healthchecksl4.Fake(fakeGCE, l4netlb.recorder)

	if _, err := test.CreateAndInsertNodes(l4netlb.cloud, nodeNames, vals.ZoneName); err != nil {
		t.Errorf("Unexpected error when adding nodes %v", err)
	}
	result := l4netlb.EnsureFrontend(nodeNames, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if err := checkMetrics(result.MetricsState /*isManaged = */, false /*isPremium = */, false /*isUserError =*/, false); err != nil {
		t.Errorf("Metrics error: %v", err)
	}
	// Check that service sync will return error if User Address IP Network Tier mismatch with service Network Tier.
	svc.ObjectMeta.Annotations[annotations.NetworkTierAnnotationKey] = string(cloud.NetworkTierPremium)
	result = l4netlb.EnsureFrontend(nodeNames, svc)
	if result.Error == nil || !utils.IsNetworkTierError(result.Error) {
		t.Errorf("LoadBalancer sync should return Network Tier error, err %v", result.Error)
	}
	if err := checkMetrics(result.MetricsState /*isManaged = */, false /*isPremium = */, false /*isUserError =*/, true); err != nil {
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
	if err := checkMetrics(result.MetricsState /*isManaged = */, false /*isPremium = */, false /*isUserError =*/, true); err != nil {
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
		Service:  svc,
		Cloud:    fakeGCE,
		Namer:    namer,
		Recorder: record.NewFakeRecorder(100),
	}
	l4netlb := NewL4NetLB(l4NetLBParams)
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

	err := firewalls.EnsureL4FirewallRule(l4netlb.cloud, utils.ServiceKeyFunc(svc.Namespace, svc.Name), &fwrParams /*sharedRule = */, false)
	if err != nil {
		t.Errorf("Unexpected error %v when ensuring firewall rule %s for svc %+v", err, fwName, svc)
	}
	existingFirewall, err := l4netlb.cloud.GetFirewall(fwName)
	if err != nil || existingFirewall == nil || len(existingFirewall.Allowed) == 0 {
		t.Errorf("Unexpected error %v when looking up firewall %s, Got firewall %+v", err, fwName, existingFirewall)
	}
	oldDestinationRanges := existingFirewall.DestinationRanges

	fwrParams.DestinationRanges = []string{"30.0.0.0/20"}
	err = firewalls.EnsureL4FirewallRule(l4netlb.cloud, utils.ServiceKeyFunc(svc.Namespace, svc.Name), &fwrParams /*sharedRule = */, false)
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

func checkMetrics(m metrics.L4NetLBServiceState, isManaged, isPremium, isUserError bool) error {
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
