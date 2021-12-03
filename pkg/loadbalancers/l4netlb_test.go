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
	"strings"
	"sync"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	servicehelper "k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	defaultNodePort = 30234
)

func TestEnsureL4NetLoadBalancer(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}
	vals := gce.DefaultTestClusterValues()
	fakeGCE := getFakeGCECloud(vals)

	svc := test.NewL4NetLBService(8080, defaultNodePort)
	namer := namer_util.NewL4Namer(kubeSystemUID, namer_util.NewNamer(vals.ClusterName, "cluster-fw"))

	l4netlb := NewL4NetLB(svc, fakeGCE, meta.Regional, namer, record.NewFakeRecorder(100), &sync.Mutex{})

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
	assertNetLbResources(t, svc, l4netlb, nodeNames)
}

func checkAnnotations(result *SyncResultNetLB, l4netlb *L4NetLB) error {
	expBackendName := l4netlb.ServicePort.BackendName()
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
	_, expHcFwName := l4netlb.namer.L4HealthCheck(l4netlb.Service.Namespace, l4netlb.Service.Name, true)
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

	svc := test.NewL4NetLBService(8080, defaultNodePort)
	namer := namer_util.NewL4Namer(kubeSystemUID, namer_util.NewNamer(vals.ClusterName, "cluster-fw"))

	l4NetLB := NewL4NetLB(svc, fakeGCE, meta.Regional, namer, record.NewFakeRecorder(100), &sync.Mutex{})

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
	assertNetLbResources(t, svc, l4NetLB, nodeNames)

	if err := l4NetLB.EnsureLoadBalancerDeleted(svc); err.Error != nil {
		t.Errorf("UnexpectedError %v", err.Error)
	}
	ensureNetLBResourceDeleted(t, svc, l4NetLB)
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
	// we expect that firewall rule will not be deleted
	_, hcFwName := l4NetLB.namer.L4HealthCheck(svc.Namespace, svc.Name, true)
	firewall, err := l4NetLB.cloud.GetFirewall(hcFwName)
	if err != nil || firewall == nil {
		t.Fatalf("Firewall rule should not be deleted err: %v", err)
	}
}

func ensureLoadBalancer(port int, vals gce.TestClusterValues, fakeGCE *gce.Cloud, t *testing.T) (*v1.Service, *L4NetLB) {
	svc := test.NewL4NetLBService(port, defaultNodePort)
	namer := namer_util.NewL4Namer(kubeSystemUID, namer_util.NewNamer(vals.ClusterName, "cluster-fw"))
	emptyNodes := []string{}
	l4NetLB := NewL4NetLB(svc, fakeGCE, meta.Regional, namer, record.NewFakeRecorder(100), &sync.Mutex{})
	result := l4NetLB.EnsureFrontend(emptyNodes, svc)
	if result.Error != nil {
		t.Errorf("Failed to ensure loadBalancer, err %v", result.Error)
	}
	if len(result.Status.Ingress) == 0 {
		t.Errorf("Got empty loadBalancer status using handler %v", l4NetLB)
	}
	assertNetLbResources(t, svc, l4NetLB, emptyNodes)
	return svc, l4NetLB
}

func ensureNetLBResourceDeleted(t *testing.T, apiService *v1.Service, l4NetLb *L4NetLB) {
	t.Helper()

	resourceName := l4NetLb.ServicePort.BackendName()
	sharedHC := !servicehelper.RequestsOnlyLocalTraffic(apiService)
	hcName, hcFwName := l4NetLb.namer.L4HealthCheck(apiService.Namespace, apiService.Name, sharedHC)

	for _, fwName := range []string{resourceName, hcFwName} {
		_, err := l4NetLb.cloud.GetFirewall(fwName)
		if err == nil || !utils.IsNotFoundError(err) {
			t.Fatalf("Firewall rule %q should be deleted", fwName)
		}
	}

	_, err := composite.GetHealthCheck(l4NetLb.cloud, meta.RegionalKey(hcName, l4NetLb.cloud.Region()), meta.VersionGA)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("Healthcheck %s should be deleted", hcName)
	}

	key := meta.RegionalKey(resourceName, l4NetLb.cloud.Region())
	_, err = composite.GetBackendService(l4NetLb.cloud, key, meta.VersionGA)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("Failed to fetch backend service %s - err %v", resourceName, err)
	}

	frName := l4NetLb.GetFRName()
	_, err = composite.GetForwardingRule(l4NetLb.cloud, meta.RegionalKey(frName, l4NetLb.cloud.Region()), meta.VersionGA)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("Forwarding rule %s should be deleted", frName)
	}

	addr, err := l4NetLb.cloud.GetRegionAddress(frName, l4NetLb.cloud.Region())
	if err == nil || addr != nil {
		t.Errorf("Address %v should be deleted", addr)
	}
}

func assertNetLbResources(t *testing.T, apiService *v1.Service, l4NetLb *L4NetLB, nodeNames []string) {
	t.Helper()
	// Check that Firewalls are created for the LoadBalancer and the HealthCheck
	resourceName := l4NetLb.ServicePort.BackendName()

	_, _, _, proto := utils.GetPortsAndProtocol(apiService.Spec.Ports)

	hcName, hcFwName := l4NetLb.namer.L4HealthCheck(apiService.Namespace, apiService.Name, true)

	fwNamesAndDesc := []string{resourceName, hcFwName}

	if hcFwName == resourceName {
		t.Errorf("Got the same name %q for LB firewall rule and Healthcheck firewall rule", hcFwName)
	}
	for _, fwName := range fwNamesAndDesc {
		firewall, err := l4NetLb.cloud.GetFirewall(fwName)
		if err != nil {
			t.Fatalf("Failed to fetch firewall rule %q - err %v", fwName, err)
		}
		if !utils.EqualStringSets(nodeNames, firewall.TargetTags) {
			t.Fatalf("Expected firewall rule target tags '%v', Got '%v'", nodeNames, firewall.TargetTags)
		}
		if len(firewall.SourceRanges) == 0 {
			t.Fatalf("Unexpected empty source range for firewall rule %v", firewall)
		}
	}

	// Check that HealthCheck is created
	healthcheck, err := composite.GetHealthCheck(l4NetLb.cloud, meta.RegionalKey(hcName, l4NetLb.cloud.Region()), meta.VersionGA)
	if err != nil {
		t.Errorf("Failed to fetch healthcheck %s - err %v", hcName, err)
	}
	if healthcheck.Name != hcName {
		t.Errorf("Unexpected name for healthcheck '%s' - expected '%s'", healthcheck.Name, hcName)
	}

	// Check that BackendService exists
	backendServiceName := resourceName
	key := meta.RegionalKey(backendServiceName, l4NetLb.cloud.Region())
	backendServiceLink := cloud.SelfLink(meta.VersionGA, l4NetLb.cloud.ProjectID(), "backendServices", key)
	bs, err := composite.GetBackendService(l4NetLb.cloud, key, meta.VersionGA)
	if err != nil {
		t.Errorf("Failed to fetch backend service %s - err %v", backendServiceName, err)
	}
	if bs.Protocol != string(proto) {
		t.Errorf("Unexpected protocol '%s' for backend service %v", bs.Protocol, bs)
	}
	if bs.SelfLink != backendServiceLink {
		t.Errorf("Unexpected self link in backend service - Expected %s, Got %s", bs.SelfLink, backendServiceLink)
	}
	if bs.LoadBalancingScheme != string(cloud.SchemeExternal) {
		t.Errorf("Unexpected load balancing scheme - Expected EXTERNAL, Got %s", bs.LoadBalancingScheme)
	}

	if !utils.EqualStringSets(bs.HealthChecks, []string{healthcheck.SelfLink}) {
		t.Errorf("Unexpected healthcheck reference '%v' in backend service, expected '%s'", bs.HealthChecks,
			healthcheck.SelfLink)
	}
	// Check that ForwardingRule is created
	frName := l4NetLb.GetFRName()
	fwdRule, err := composite.GetForwardingRule(l4NetLb.cloud, meta.RegionalKey(frName, l4NetLb.cloud.Region()), meta.VersionGA)
	if err != nil {
		t.Errorf("Failed to fetch forwarding rule %s - err %v", frName, err)
	}
	if fwdRule.Name != frName {
		t.Errorf("Unexpected name for forwarding rule '%s' - expected '%s'", fwdRule.Name, frName)
	}
	if fwdRule.IPProtocol != string(proto) {
		t.Errorf("Unexpected protocol '%s' for forwarding rule %v", fwdRule.IPProtocol, fwdRule)
	}
	if fwdRule.BackendService != backendServiceLink {
		t.Errorf("Unexpected backend service link '%s' for forwarding rule, expected '%s'", fwdRule.BackendService, backendServiceLink)
	}

	addr, err := l4NetLb.cloud.GetRegionAddress(frName, l4NetLb.cloud.Region())
	if err == nil || addr != nil {
		t.Errorf("Expected error when looking up ephemeral address, got %v", addr)
	}
}
