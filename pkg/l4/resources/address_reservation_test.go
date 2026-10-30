/*
Copyright 2026 The Kubernetes Authors.

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

package resources

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	ga "google.golang.org/api/compute/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4/forwardingrules"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
)

// addressMutations counts the calls that change addresses in a fake cloud:
// inserts, and deletes of addresses that exist. Deletes of addresses that
// don't exist (e.g. the cleanup of IPv4 resources of an IPv6 only Service)
// don't change anything and are not counted.
type addressMutations struct {
	mu      sync.Mutex
	inserts int
	deletes int
}

// trackAddressMutations installs hooks on the fake cloud that count address mutations.
// Inserts keep assigning IPs the same way getFakeGCECloud does.
func trackAddressMutations(fakeGCE *gce.Cloud) *addressMutations {
	am := &addressMutations{}
	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	mockGCE.MockAddresses.InsertHook = func(ctx context.Context, key *meta.Key, obj *ga.Address, m *cloud.MockAddresses, options ...cloud.Option) (bool, error) {
		am.mu.Lock()
		am.inserts++
		am.mu.Unlock()
		return mock.InsertAddressHook(ctx, key, obj, m, options...)
	}
	mockGCE.MockAddresses.DeleteHook = func(ctx context.Context, key *meta.Key, m *cloud.MockAddresses, options ...cloud.Option) (bool, error) {
		m.Lock.Lock()
		_, exists := m.Objects[*key]
		m.Lock.Unlock()
		if exists {
			am.mu.Lock()
			am.deletes++
			am.mu.Unlock()
		}
		return false, nil
	}
	return am
}

func (am *addressMutations) reset() {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.inserts, am.deletes = 0, 0
}

func (am *addressMutations) counts() (inserts, deletes int) {
	am.mu.Lock()
	defer am.mu.Unlock()
	return am.inserts, am.deletes
}

// assertForwardingRuleIPReservedOnDeleteHook returns a forwarding rules DeleteHook that
// fails the test when a forwarding rule is deleted while its IP is not reserved by an
// address, for both IPv4 and IPv6 rules.
func assertForwardingRuleIPReservedOnDeleteHook(t *testing.T, fakeGCE *gce.Cloud) func(ctx context.Context, key *meta.Key, m *cloud.MockForwardingRules, options ...cloud.Option) (bool, error) {
	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	return func(ctx context.Context, key *meta.Key, _ *cloud.MockForwardingRules, _ ...cloud.Option) (bool, error) {
		fr, err := mockGCE.MockForwardingRules.Get(ctx, key)
		if utils.IsNotFoundError(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		// IPv6 forwarding rules store the address in CIDR form.
		frIP := net.ParseIP(strings.Split(fr.IPAddress, "/")[0])
		addrs, err := mockGCE.MockAddresses.List(ctx, fakeGCE.Region(), filter.None)
		if err != nil {
			return false, err
		}
		for _, addr := range addrs {
			if net.ParseIP(addr.Address).Equal(frIP) {
				return false, nil
			}
		}
		t.Errorf("forwarding rule %s is deleted while its IP %s is not reserved", fr.Name, fr.IPAddress)
		return false, nil
	}
}

// sameIngressIPs reports whether the two LoadBalancer statuses list the same IPs,
// regardless of the notation used for IPv6 addresses.
func sameIngressIPs(want, got []v1.LoadBalancerIngress) bool {
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if !net.ParseIP(want[i].IP).Equal(net.ParseIP(got[i].IP)) {
			return false
		}
	}
	return true
}

// TestEnsureInternalLoadBalancerResyncDoesNotReserveAddress verifies that syncing an
// unchanged ILB Service doesn't reserve (and release) its addresses, while creating
// the load balancer and re-creating a forwarding rule still do.
func TestEnsureInternalLoadBalancerResyncDoesNotReserveAddress(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}

	testCases := []struct {
		desc       string
		ipFamilies []v1.IPFamily
	}{
		{desc: "ipv4", ipFamilies: []v1.IPFamily{v1.IPv4Protocol}},
		{desc: "ipv6", ipFamilies: []v1.IPFamily{v1.IPv6Protocol}},
		{desc: "ipv4 ipv6", ipFamilies: []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol}},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			svc := test.NewL4ILBDualStackService(8080, v1.ProtocolTCP, tc.ipFamilies, v1.ServiceExternalTrafficPolicyTypeCluster)
			l4 := mustSetupILBTestHandler(t, svc, nodeNames)
			am := trackAddressMutations(l4.cloud)
			l4.cloud.Compute().(*cloud.MockGCE).MockForwardingRules.DeleteHook = assertForwardingRuleIPReservedOnDeleteHook(t, l4.cloud)

			// Creating the load balancer reserves the address(es) to pick the IP.
			result := l4.EnsureInternalLoadBalancer(nodeNames, svc)
			if result.Error != nil {
				t.Fatalf("EnsureInternalLoadBalancer() on create returned error %v, want nil", result.Error)
			}
			if inserts, _ := am.counts(); inserts == 0 {
				t.Errorf("EnsureInternalLoadBalancer() on create reserved %d addresses, want at least 1", inserts)
			}
			svc.Annotations = result.Annotations
			assertDualStackILBResources(t, l4, nodeNames)
			ipsAfterCreate := result.Status.Ingress

			// Syncing the unchanged Service must not touch addresses at all.
			am.reset()
			result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
			if result.Error != nil {
				t.Fatalf("EnsureInternalLoadBalancer() on resync returned error %v, want nil", result.Error)
			}
			if result.ResourceUpdates.WereAnyResourcesModified() {
				t.Errorf("EnsureInternalLoadBalancer() on resync modified resources: %v, want none", result.ResourceUpdates.String())
			}
			if inserts, deletes := am.counts(); inserts != 0 || deletes != 0 {
				t.Errorf("EnsureInternalLoadBalancer() on resync made %d address inserts and %d address deletes, want 0 and 0", inserts, deletes)
			}
			if !sameIngressIPs(ipsAfterCreate, result.Status.Ingress) {
				t.Errorf("EnsureInternalLoadBalancer() on resync changed the LoadBalancer IPs from %v to %v", ipsAfterCreate, result.Status.Ingress)
			}
			assertDualStackILBResources(t, l4, nodeNames)

			// Changing the ports re-creates the forwarding rule(s), the IP must be reserved meanwhile.
			am.reset()
			svc.Spec.Ports[0].Port = 8081
			result = l4.EnsureInternalLoadBalancer(nodeNames, svc)
			if result.Error != nil {
				t.Fatalf("EnsureInternalLoadBalancer() on update returned error %v, want nil", result.Error)
			}
			if inserts, _ := am.counts(); inserts == 0 {
				t.Errorf("EnsureInternalLoadBalancer() on update reserved %d addresses, want at least 1", inserts)
			}
			if !sameIngressIPs(ipsAfterCreate, result.Status.Ingress) {
				t.Errorf("EnsureInternalLoadBalancer() on update changed the LoadBalancer IPs from %v to %v", ipsAfterCreate, result.Status.Ingress)
			}
			svc.Annotations = result.Annotations
			assertDualStackILBResources(t, l4, nodeNames)
		})
	}
}

// TestEnsureFrontendResyncDoesNotReserveAddress verifies that syncing an unchanged
// NetLB Service doesn't reserve (and release) its addresses, while creating the load
// balancer and re-creating a forwarding rule still do.
func TestEnsureFrontendResyncDoesNotReserveAddress(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}

	testCases := []struct {
		desc       string
		ipFamilies []v1.IPFamily
	}{
		{desc: "ipv4", ipFamilies: []v1.IPFamily{v1.IPv4Protocol}},
		{desc: "ipv6", ipFamilies: []v1.IPFamily{v1.IPv6Protocol}},
		{desc: "ipv4 ipv6", ipFamilies: []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol}},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			svc := test.NewL4NetLBRBSDualStackService(v1.ProtocolTCP, tc.ipFamilies, v1.ServiceExternalTrafficPolicyTypeCluster)
			l4NetLB := mustSetupNetLBTestHandler(t, svc, nodeNames)
			am := trackAddressMutations(l4NetLB.cloud)
			l4NetLB.cloud.Compute().(*cloud.MockGCE).MockForwardingRules.DeleteHook = assertForwardingRuleIPReservedOnDeleteHook(t, l4NetLB.cloud)

			// Creating the load balancer reserves the address(es) to pick the IP.
			result := l4NetLB.EnsureFrontend(nodeNames, svc, time.Now())
			if result.Error != nil {
				t.Fatalf("EnsureFrontend() on create returned error %v, want nil", result.Error)
			}
			if inserts, _ := am.counts(); inserts == 0 {
				t.Errorf("EnsureFrontend() on create reserved %d addresses, want at least 1", inserts)
			}
			svc.Annotations = result.Annotations
			assertDualStackNetLBResources(t, l4NetLB, nodeNames)
			ipsAfterCreate := result.Status.Ingress

			// Syncing the unchanged Service must not touch addresses at all.
			am.reset()
			result = l4NetLB.EnsureFrontend(nodeNames, svc, time.Now())
			if result.Error != nil {
				t.Fatalf("EnsureFrontend() on resync returned error %v, want nil", result.Error)
			}
			if result.GCEResourceUpdate.WereAnyResourcesModified() {
				t.Errorf("EnsureFrontend() on resync modified resources: %v, want none", result.GCEResourceUpdate.String())
			}
			if inserts, deletes := am.counts(); inserts != 0 || deletes != 0 {
				t.Errorf("EnsureFrontend() on resync made %d address inserts and %d address deletes, want 0 and 0", inserts, deletes)
			}
			if !sameIngressIPs(ipsAfterCreate, result.Status.Ingress) {
				t.Errorf("EnsureFrontend() on resync changed the LoadBalancer IPs from %v to %v", ipsAfterCreate, result.Status.Ingress)
			}
			assertDualStackNetLBResources(t, l4NetLB, nodeNames)

			// Changing the ports re-creates the forwarding rule(s), the IP must be reserved meanwhile.
			am.reset()
			svc.Spec.Ports[0].Port = 8081
			result = l4NetLB.EnsureFrontend(nodeNames, svc, time.Now())
			if result.Error != nil {
				t.Fatalf("EnsureFrontend() on update returned error %v, want nil", result.Error)
			}
			if inserts, _ := am.counts(); inserts == 0 {
				t.Errorf("EnsureFrontend() on update reserved %d addresses, want at least 1", inserts)
			}
			if !sameIngressIPs(ipsAfterCreate, result.Status.Ingress) {
				t.Errorf("EnsureFrontend() on update changed the LoadBalancer IPs from %v to %v", ipsAfterCreate, result.Status.Ingress)
			}
			svc.Annotations = result.Annotations
			assertDualStackNetLBResources(t, l4NetLB, nodeNames)
		})
	}
}

// TestL4IsForwardingRuleUpToDate pins the conditions under which the ILB address
// reservation is skipped, in particular the ones that are not a plain difference
// between the existing and the wanted forwarding rule.
func TestL4IsForwardingRuleUpToDate(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}

	svc := test.NewL4ILBDualStackService(8080, v1.ProtocolTCP, []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol}, v1.ServiceExternalTrafficPolicyTypeCluster)
	l4 := mustSetupILBTestHandler(t, svc, nodeNames)
	if result := l4.EnsureInternalLoadBalancer(nodeNames, svc); result.Error != nil {
		t.Fatalf("EnsureInternalLoadBalancer() returned error %v, want nil", result.Error)
	}

	existingBS, err := l4.backendPool.Get(l4.namer.L4Backend(svc.Namespace, svc.Name), meta.VersionGA, l4.scope, l4.svcLogger)
	if err != nil || existingBS == nil {
		t.Fatalf("backendPool.Get() = %v, %v, want the backend service", existingBS, err)
	}
	existingIPv4FR, err := l4.forwardingRules.Get(l4.GetFRName())
	if err != nil || existingIPv4FR == nil {
		t.Fatalf("forwardingRules.Get(%q) = %v, %v, want the IPv4 forwarding rule", l4.GetFRName(), existingIPv4FR, err)
	}
	existingIPv6FR, err := l4.forwardingRules.Get(l4.getIPv6FRName())
	if err != nil || existingIPv6FR == nil {
		t.Fatalf("forwardingRules.Get(%q) = %v, %v, want the IPv6 forwarding rule", l4.getIPv6FRName(), existingIPv6FR, err)
	}
	options := l4.getILBOptions()
	subnetworkURL, err := l4.getServiceSubnetworkURL(options)
	if err != nil {
		t.Fatalf("getServiceSubnetworkURL() returned error %v, want nil", err)
	}
	// l4.Service is svc itself, keep the original ports to restore them after
	// the cases that mutate them.
	originalPorts := svc.Spec.Ports

	// inputs of the predicates, as computed by EnsureInternalLoadBalancer on a resync.
	type inputs struct {
		bs              *composite.BackendService
		backendProtocol string
		ipv4FR, ipv6FR  *composite.ForwardingRule
		options         gce.ILBOptions
		ipv4, ipv6      string
		ports           []v1.ServicePort
	}
	resyncInputs := func() inputs {
		return inputs{
			bs:              existingBS,
			backendProtocol: string(v1.ProtocolTCP),
			ipv4FR:          existingIPv4FR,
			ipv6FR:          existingIPv6FR,
			options:         options,
			ipv4:            existingIPv4FR.IPAddress,
			ipv6:            strings.Split(existingIPv6FR.IPAddress, "/")[0],
			ports:           originalPorts,
		}
	}

	testCases := []struct {
		desc     string
		mutate   func(in *inputs)
		wantIPv4 bool
		wantIPv6 bool
	}{
		{
			desc:     "ports change",
			mutate:   func(in *inputs) { in.ports = []v1.ServicePort{{Port: 8081, Protocol: v1.ProtocolTCP}} },
			wantIPv4: false,
			wantIPv6: false,
		},
		{
			// Runs after "ports change" to verify that the cases are isolated from each other.
			desc:     "unchanged service",
			mutate:   func(in *inputs) {},
			wantIPv4: true,
			wantIPv6: true,
		},
		{
			desc:     "no backend service",
			mutate:   func(in *inputs) { in.bs = nil },
			wantIPv4: false,
			wantIPv6: false,
		},
		{
			desc:     "backend service protocol changes",
			mutate:   func(in *inputs) { in.backendProtocol = string(v1.ProtocolUDP) },
			wantIPv4: false,
			wantIPv6: false,
		},
		{
			desc:     "no existing forwarding rules",
			mutate:   func(in *inputs) { in.ipv4FR, in.ipv6FR = nil, nil },
			wantIPv4: false,
			wantIPv6: false,
		},
		{
			desc:     "IPs are reset",
			mutate:   func(in *inputs) { in.ipv4, in.ipv6 = "", "" },
			wantIPv4: false,
			wantIPv6: false,
		},
		{
			desc:     "different IPv6 address requested",
			mutate:   func(in *inputs) { in.ipv6 = "2001:db8::1" },
			wantIPv4: true,
			wantIPv6: false,
		},
		{
			desc:     "global access enabled",
			mutate:   func(in *inputs) { in.options.AllowGlobalAccess = true },
			wantIPv4: false,
			wantIPv6: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			in := resyncInputs()
			tc.mutate(&in)
			l4.Service.Spec.Ports = in.ports
			defer func() { l4.Service.Spec.Ports = originalPorts }()

			if got := l4.isIPv4ForwardingRuleUpToDate(in.bs, in.backendProtocol, in.ipv4FR, in.options, subnetworkURL, in.ipv4); got != tc.wantIPv4 {
				t.Errorf("isIPv4ForwardingRuleUpToDate() = %v, want %v", got, tc.wantIPv4)
			}
			if got := l4.isIPv6ForwardingRuleUpToDate(in.bs, in.backendProtocol, in.ipv6FR, in.options, in.ipv6); got != tc.wantIPv6 {
				t.Errorf("isIPv6ForwardingRuleUpToDate() = %v, want %v", got, tc.wantIPv6)
			}
		})
	}
}

// TestL4NetLBIsIPv6ForwardingRuleUpToDate pins the conditions under which the NetLB
// IPv6 address reservation is skipped, in particular the ones that are not a plain
// difference between the existing and the wanted forwarding rule.
func TestL4NetLBIsIPv6ForwardingRuleUpToDate(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}

	svc := test.NewL4NetLBRBSDualStackService(v1.ProtocolTCP, []v1.IPFamily{v1.IPv6Protocol}, v1.ServiceExternalTrafficPolicyTypeCluster)
	l4NetLB := mustSetupNetLBTestHandler(t, svc, nodeNames)
	if result := l4NetLB.EnsureFrontend(nodeNames, svc, time.Now()); result.Error != nil {
		t.Fatalf("EnsureFrontend() returned error %v, want nil", result.Error)
	}

	existingFR, err := l4NetLB.forwardingRules.Get(l4NetLB.ipv6FRName())
	if err != nil || existingFR == nil {
		t.Fatalf("forwardingRules.Get(%q) = %v, %v, want the IPv6 forwarding rule", l4NetLB.ipv6FRName(), existingFR, err)
	}
	subnetworkURL, err := l4NetLB.ipv6SubnetURL()
	if err != nil {
		t.Fatalf("ipv6SubnetURL() returned error %v, want nil", err)
	}
	ipCollection, err := l4NetLB.ipv6IPCollection()
	if err != nil {
		t.Fatalf("ipv6IPCollection() returned error %v, want nil", err)
	}
	// l4NetLB.Service is svc itself, keep the original ports to restore them
	// after the cases that mutate them.
	originalPorts := svc.Spec.Ports

	// inputs of the predicate, as computed by ensureIPv6ForwardingRule on a resync.
	type inputs struct {
		existingFR, toDeleteFR *composite.ForwardingRule
		bsLink                 string
		ipv6                   string
		netTier                cloud.NetworkTier
		ports                  []v1.ServicePort
	}
	resyncInputs := func() inputs {
		return inputs{
			existingFR: existingFR,
			bsLink:     existingFR.BackendService,
			ipv6:       strings.Split(existingFR.IPAddress, "/")[0],
			netTier:    cloud.NetworkTierPremium,
			ports:      originalPorts,
		}
	}

	testCases := []struct {
		desc   string
		mutate func(in *inputs)
		want   bool
	}{
		{
			desc:   "ports change",
			mutate: func(in *inputs) { in.ports = []v1.ServicePort{{Port: 8081, Protocol: v1.ProtocolTCP}} },
			want:   false,
		},
		{
			// Runs after "ports change" to verify that the cases are isolated from each other.
			desc:   "unchanged service",
			mutate: func(in *inputs) {},
			want:   true,
		},
		{
			desc:   "no existing forwarding rule",
			mutate: func(in *inputs) { in.existingFR = nil },
			want:   false,
		},
		{
			desc:   "forwarding rule with the alternate single/mixed protocol name to delete",
			mutate: func(in *inputs) { in.toDeleteFR = existingFR },
			want:   false,
		},
		{
			desc:   "IP is reset",
			mutate: func(in *inputs) { in.ipv6 = "" },
			want:   false,
		},
		{
			desc:   "different IPv6 address requested",
			mutate: func(in *inputs) { in.ipv6 = "2001:db8::1" },
			want:   false,
		},
		{
			desc:   "network tier changes",
			mutate: func(in *inputs) { in.netTier = cloud.NetworkTierStandard },
			want:   false,
		},
		{
			desc:   "backend service changes",
			mutate: func(in *inputs) { in.bsLink = existingFR.BackendService + "-other" },
			want:   false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			in := resyncInputs()
			tc.mutate(&in)
			l4NetLB.Service.Spec.Ports = in.ports
			defer func() { l4NetLB.Service.Spec.Ports = originalPorts }()

			if got := l4NetLB.isIPv6ForwardingRuleUpToDate(in.existingFR, in.toDeleteFR, in.bsLink, in.ipv6, subnetworkURL, ipCollection, in.netTier, l4NetLB.svcLogger); got != tc.want {
				t.Errorf("isIPv6ForwardingRuleUpToDate() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRecreateRequiresReservedIP verifies that a forwarding rule is never deleted to be
// re-created while its IP is not reserved: the sync fails with ErrIPNotReserved and the
// rule is left in place, for the four kinds of rules and the code paths that delete them.
func TestRecreateRequiresReservedIP(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}
	const bsLink = "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1"

	ilb := mustSetupILBTestHandler(t, test.NewL4ILBService(false, 8080), nodeNames)
	netlb := mustSetupNetLBTestHandler(t, test.NewL4NetLBRBSService(8080), nodeNames)

	// The existing rules forward port 9090 while the Services want 8080, which can't be
	// patched: the rules have to be deleted and re-created.
	ilbIPv4 := &composite.ForwardingRule{
		Name: ilb.GetFRName(), IPAddress: "10.0.0.5", IPProtocol: "TCP", Ports: []string{"9090"},
		LoadBalancingScheme: string(cloud.SchemeInternal), BackendService: bsLink,
	}
	ilbIPv6 := &composite.ForwardingRule{
		Name: ilb.getIPv6FRName(), IPAddress: "2001:db8::5/96", IPProtocol: "TCP", Ports: []string{"9090"},
		LoadBalancingScheme: string(cloud.SchemeInternal), BackendService: bsLink, IpVersion: IPVersionIPv6,
	}
	netlbIPv4 := &composite.ForwardingRule{
		Name: netlb.frName(), IPAddress: "35.0.0.5", IPProtocol: "TCP", PortRange: "9090-9090",
		LoadBalancingScheme: string(cloud.SchemeExternal), BackendService: bsLink, NetworkTier: cloud.NetworkTierPremium.ToGCEValue(),
	}
	netlbIPv6 := &composite.ForwardingRule{
		Name: netlb.ipv6FRName(), IPAddress: "2600:1900::5/96", IPProtocol: "TCP", PortRange: "9090-9090",
		LoadBalancingScheme: string(cloud.SchemeExternal), BackendService: bsLink, NetworkTier: cloud.NetworkTierPremium.ToGCEValue(), IpVersion: IPVersionIPv6,
	}

	testCases := []struct {
		desc     string
		provider ForwardingRulesProvider
		existing *composite.ForwardingRule
		// recreate runs the code path that deletes existing to re-create it.
		recreate func(ipReserved bool) error
	}{
		{
			desc:     "ILB IPv4",
			provider: ilb.forwardingRules,
			existing: ilbIPv4,
			recreate: func(ipReserved bool) error {
				_, _, err := ilb.ensureIPv4ForwardingRule(bsLink, ilb.getILBOptions(), ilbIPv4, ilb.network.SubnetworkURL, ilbIPv4.IPAddress, ipReserved)
				return err
			},
		},
		{
			desc:     "ILB IPv6",
			provider: ilb.forwardingRules,
			existing: ilbIPv6,
			recreate: func(ipReserved bool) error {
				_, _, err := ilb.ensureIPv6ForwardingRule(bsLink, ilb.getILBOptions(), ilbIPv6, "2001:db8::5", ipReserved)
				return err
			},
		},
		{
			desc:     "NetLB IPv4",
			provider: netlb.forwardingRules,
			existing: netlbIPv4,
			recreate: func(ipReserved bool) error {
				wanted, err := netlb.buildExpectedIPv4ForwardingRule(bsLink, netlbIPv4.IPAddress)
				if err != nil {
					return err
				}
				return netlb.updateForwardingRule(netlbIPv4, wanted, ipReserved, netlb.svcLogger)
			},
		},
		{
			desc:     "NetLB IPv6",
			provider: netlb.forwardingRules,
			existing: netlbIPv6,
			recreate: func(ipReserved bool) error {
				expected, err := netlb.buildExpectedIPv6ForwardingRule(bsLink, "2600:1900::5", "", "", cloud.NetworkTierPremium)
				if err != nil {
					return err
				}
				return netlb.deleteChangedIPv6ForwardingRule(netlbIPv6, expected, ipReserved)
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			if err := tc.provider.Create(tc.existing); err != nil {
				t.Fatalf("Create(%v) returned error %v, want nil", tc.existing, err)
			}

			err := tc.recreate(false)
			if !errors.Is(err, forwardingrules.ErrIPNotReserved) {
				t.Fatalf("re-creating the rule with an unreserved IP returned error %v, want an error wrapping ErrIPNotReserved", err)
			}
			got, err := tc.provider.Get(tc.existing.Name)
			if err != nil || got == nil {
				t.Fatalf("Get(%q) = %v, %v, want the existing rule to be left in place", tc.existing.Name, got, err)
			}
			if got.PortRange != tc.existing.PortRange || !utils.EqualStringSets(got.Ports, tc.existing.Ports) {
				t.Errorf("Get(%q) = %+v, want the existing rule unchanged", tc.existing.Name, got)
			}

			if err := tc.recreate(true); err != nil {
				t.Errorf("re-creating the rule with a reserved IP returned error %v, want nil", err)
			}
		})
	}
}
