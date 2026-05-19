package forwardingrules_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4/address"
	"k8s.io/ingress-gce/pkg/l4/forwardingrules"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"

	"k8s.io/cloud-provider-gcp/providers/gce"
)

const (
	kubeSystemUID = "ksuid123"
	namespace     = "test-ns"
	name          = "test-svc"
	ip            = "1.2.3.4"
	tcpName       = "k8s2-tcp-axyqjz2d-test-ns-test-svc-pyn67fp6"
	udpName       = "k8s2-udp-axyqjz2d-test-ns-test-svc-pyn67fp6"
	bsLink        = "http://compute.googleapis.com/projects/test/regions/us-central1/backendServices/bs1"
	legacyName    = "aksuid123"
)

func TestMixedManagerNetLB_EnsureIPv4_SplitRules(t *testing.T) {
	startingState := []struct {
		desc   string
		tcp    *compute.ForwardingRule
		udp    *compute.ForwardingRule
		legacy *compute.ForwardingRule
	}{
		{
			desc: "nothing",
		},
		{
			desc: "tcp 80",
			legacy: &compute.ForwardingRule{
				Name:           legacyName,
				IPAddress:      ip,
				IPProtocol:     "TCP",
				NetworkTier:    "PREMIUM",
				BackendService: bsLink,
				Ports:          []string{"80"},
			},
		},
		{
			desc: "l3 default", // When rolling back from L3 to split rules
			legacy: &compute.ForwardingRule{
				Name:           legacyName,
				IPAddress:      ip,
				IPProtocol:     "L3_DEFAULT",
				NetworkTier:    "PREMIUM",
				BackendService: bsLink,
				AllPorts:       true,
			},
		},
		{
			desc: "tcp 80, udp 53",
			tcp: &compute.ForwardingRule{
				Name:           tcpName,
				IPAddress:      ip,
				IPProtocol:     "TCP",
				NetworkTier:    "PREMIUM",
				BackendService: bsLink,
				Ports:          []string{"80"},
			},
			udp: &compute.ForwardingRule{
				Name:           udpName,
				IPAddress:      ip,
				IPProtocol:     "UDP",
				NetworkTier:    "PREMIUM",
				BackendService: bsLink,
				Ports:          []string{"53"},
			},
		},
		{
			desc: "tcp 80, udp 53,60",
			tcp: &compute.ForwardingRule{
				Name:           tcpName,
				IPAddress:      ip,
				IPProtocol:     "TCP",
				NetworkTier:    "PREMIUM",
				BackendService: bsLink,
				Ports:          []string{"80"},
			},
			udp: &compute.ForwardingRule{
				Name:           udpName,
				IPAddress:      ip,
				IPProtocol:     "UDP",
				NetworkTier:    "PREMIUM",
				BackendService: bsLink,
				Ports:          []string{"53", "60"},
			},
		},
		{
			desc: "tcp 80,81,82,83,84,85,86, udp 53",
			tcp: &compute.ForwardingRule{
				Name:           tcpName,
				IPAddress:      ip,
				IPProtocol:     "TCP",
				NetworkTier:    "PREMIUM",
				BackendService: bsLink,
				PortRange:      "80-86",
			},
			udp: &compute.ForwardingRule{
				Name:           udpName,
				IPAddress:      ip,
				IPProtocol:     "UDP",
				NetworkTier:    "PREMIUM",
				BackendService: bsLink,
				Ports:          []string{"53"},
			},
		},
		{
			desc: "failed_update_with_leftovers",
			tcp: &compute.ForwardingRule{
				Name:           tcpName,
				IPAddress:      ip,
				IPProtocol:     "TCP",
				NetworkTier:    "PREMIUM",
				BackendService: bsLink,
				Ports:          []string{"80"},
			},
			udp: &compute.ForwardingRule{
				Name:           udpName,
				IPAddress:      ip,
				IPProtocol:     "UDP",
				NetworkTier:    "PREMIUM",
				BackendService: bsLink,
				Ports:          []string{"53"},
			},
			legacy: &compute.ForwardingRule{
				Name:           legacyName,
				IPAddress:      ip,
				IPProtocol:     "L3_DEFAULT",
				NetworkTier:    "PREMIUM",
				BackendService: bsLink,
				AllPorts:       true,
			},
		},
	}

	var moreThanMaxTCPPorts []api_v1.ServicePort
	for i := 0; i <= 6; i++ {
		moreThanMaxTCPPorts = append(moreThanMaxTCPPorts, api_v1.ServicePort{
			Protocol: api_v1.ProtocolTCP,
			Port:     int32(80 + i),
		})
	}
	maxPorts := append(moreThanMaxTCPPorts, api_v1.ServicePort{
		Protocol: api_v1.ProtocolUDP,
		Port:     53,
	})

	rangeTCPRule := fwdRule(tcpName, "TCP", nil)
	rangeTCPRule.PortRange = "80-86"

	endState := []struct {
		desc  string
		ports []api_v1.ServicePort
		tcp   *composite.ForwardingRule
		udp   *composite.ForwardingRule
	}{
		{
			desc: "tcp 80, udp 53",
			ports: []api_v1.ServicePort{
				{
					Protocol: api_v1.ProtocolTCP,
					Port:     80,
				},
				{
					Protocol: api_v1.ProtocolUDP,
					Port:     53,
				},
			},
			tcp: fwdRule(tcpName, "TCP", []string{"80"}),
			udp: fwdRule(udpName, "UDP", []string{"53"}),
		},
		{
			desc: "tcp 80, udp 53,60",
			ports: []api_v1.ServicePort{
				{
					Protocol: api_v1.ProtocolTCP,
					Port:     80,
				},
				{
					Protocol: api_v1.ProtocolUDP,
					Port:     53,
				},
				{
					Protocol: api_v1.ProtocolUDP,
					Port:     60,
				},
			},
			tcp: fwdRule(tcpName, "TCP", []string{"80"}),
			udp: fwdRule(udpName, "UDP", []string{"53", "60"}),
		},
		{
			desc:  "tcp 80,81,82,83,84,85,86, udp 53",
			ports: maxPorts,
			tcp:   rangeTCPRule,
			udp:   fwdRule(udpName, "UDP", []string{"53"}),
		},
	}

	for _, start := range startingState {
		for _, end := range endState {
			desc := start.desc + " -> " + end.desc
			start, end := start, end
			t.Run(desc, func(t *testing.T) {
				t.Parallel()

				// Arrange
				fakeGCE, mgr := arrange(end.ports, false)
				region := fakeGCE.Region()
				for _, rule := range []*compute.ForwardingRule{start.legacy, start.tcp, start.udp} {
					if rule != nil {
						if err := fakeGCE.CreateRegionForwardingRule(rule, region); err != nil {
							t.Fatalf("Couldn't set up forwarding rule %v for test", rule)
						}
					}
				}

				// Act
				result, err := mgr.EnsureIPv4(bsLink)
				// Assert
				if err != nil {
					t.Errorf("EnsureIPv4() error = %v", err)
				}

				fwdRuleIgnoreFields := cmpopts.IgnoreFields(composite.ForwardingRule{}, "SelfLink")
				if diff := cmp.Diff(end.tcp, result.TCPFwdRule, fwdRuleIgnoreFields); diff != "" {
					t.Errorf("EnsureIPv4().TCPFwdRule mismatch (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(end.udp, result.UDPFwdRule, fwdRuleIgnoreFields); diff != "" {
					t.Errorf("EnsureIPv4().UDPFwdRule mismatch (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func TestMixedManagerNetLB_EnsureIPv4_L3Default(t *testing.T) {
	t.Parallel()

	l3Rule := &composite.ForwardingRule{
		Name:                "k8s2-l3-axyqjz2d-test-ns-test-svc-pyn67fp6",
		IPAddress:           ip,
		AllPorts:            true,
		IPProtocol:          "L3_DEFAULT",
		NetworkTier:         "PREMIUM",
		Version:             "ga",
		Scope:               "regional",
		BackendService:      bsLink,
		LoadBalancingScheme: "EXTERNAL",
		Region:              "us-central1",
		Description:         `{"networking.gke.io/service-name":"test-ns/test-svc","networking.gke.io/api-version":"ga","networking.gke.io/service-ip":"1.2.3.4"}`,
		SelfLink:            "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/forwardingRules/k8s2-l3-axyqjz2d-test-ns-test-svc-pyn67fp6",
	}

	startingState := []struct {
		desc            string
		forwardingRules []*compute.ForwardingRule
	}{
		{
			desc: "nothing",
		},
		{
			desc: "tcp 80",
			forwardingRules: []*compute.ForwardingRule{
				{
					Name:           legacyName,
					IPAddress:      ip,
					IPProtocol:     "TCP",
					NetworkTier:    "PREMIUM",
					BackendService: bsLink,
					Ports:          []string{"80"},
				},
			},
		},
		{
			desc: "tcp 80, udp 53",
			forwardingRules: []*compute.ForwardingRule{
				{
					Name:           tcpName,
					IPAddress:      ip,
					IPProtocol:     "TCP",
					NetworkTier:    "PREMIUM",
					BackendService: bsLink,
					Ports:          []string{"80"},
				},
				{
					Name:           udpName,
					IPAddress:      ip,
					IPProtocol:     "UDP",
					NetworkTier:    "PREMIUM",
					BackendService: bsLink,
					Ports:          []string{"53"},
				},
			},
		},
		{
			desc: "tcp 80, udp 53,60",
			forwardingRules: []*compute.ForwardingRule{
				{
					Name:           tcpName,
					IPAddress:      ip,
					IPProtocol:     "TCP",
					NetworkTier:    "PREMIUM",
					BackendService: bsLink,
					Ports:          []string{"80"},
				},
				{
					Name:           udpName,
					IPAddress:      ip,
					IPProtocol:     "UDP",
					NetworkTier:    "PREMIUM",
					BackendService: bsLink,
					Ports:          []string{"53", "60"},
				},
			},
		},
		{
			desc: "tcp 80,81,82,83,84,85,86, udp 53",
			forwardingRules: []*compute.ForwardingRule{
				{
					Name:           tcpName,
					IPAddress:      ip,
					IPProtocol:     "TCP",
					NetworkTier:    "PREMIUM",
					BackendService: bsLink,
					PortRange:      "80-86",
				},
				{
					Name:           udpName,
					IPAddress:      ip,
					IPProtocol:     "UDP",
					NetworkTier:    "PREMIUM",
					BackendService: bsLink,
					Ports:          []string{"53"},
				},
			},
		},
		{
			desc: "l3_default",
			forwardingRules: []*compute.ForwardingRule{
				{
					Name:           legacyName,
					IPAddress:      ip,
					IPProtocol:     "L3_DEFAULT",
					NetworkTier:    "PREMIUM",
					BackendService: bsLink,
					AllPorts:       true,
				},
			},
		},
		{
			desc: "failed_update_with_leftovers",
			forwardingRules: []*compute.ForwardingRule{
				{
					Name:           tcpName,
					IPAddress:      ip,
					IPProtocol:     "TCP",
					NetworkTier:    "PREMIUM",
					BackendService: bsLink,
					Ports:          []string{"80"},
				},
				{
					Name:           udpName,
					IPAddress:      ip,
					IPProtocol:     "UDP",
					NetworkTier:    "PREMIUM",
					BackendService: bsLink,
					Ports:          []string{"53"},
				},
				{
					Name:           legacyName,
					IPAddress:      ip,
					IPProtocol:     "L3_DEFAULT",
					NetworkTier:    "PREMIUM",
					BackendService: bsLink,
					AllPorts:       true,
				},
			},
		},
	}

	wantResult := forwardingrules.EnsureNetLBResult{
		TCPFwdRule: nil,
		UDPFwdRule: nil,
		L3FwdRule:  l3Rule,
		SyncStatus: utils.ResourceUpdate,
		IPManaged:  address.IPAddrManaged,
	}

	endState := []struct {
		desc  string
		ports []api_v1.ServicePort
		// want is going to be always the same l3 rule
	}{
		{
			desc: "tcp 80, udp 53",
			ports: []api_v1.ServicePort{
				{
					Protocol: api_v1.ProtocolTCP,
					Port:     80,
				},
				{
					Protocol: api_v1.ProtocolUDP,
					Port:     53,
				},
			},
		},
		{
			desc: "tcp 80, udp 53,60",
			ports: []api_v1.ServicePort{
				{
					Protocol: api_v1.ProtocolTCP,
					Port:     80,
				},
				{
					Protocol: api_v1.ProtocolUDP,
					Port:     53,
				},
				{
					Protocol: api_v1.ProtocolUDP,
					Port:     60,
				},
			},
		},
	}

	for _, start := range startingState {
		for _, end := range endState {
			desc := start.desc + " -> " + end.desc
			start, end := start, end
			t.Run(desc, func(t *testing.T) {
				t.Parallel()

				// Arrange
				fakeGCE, mgr := arrange(end.ports, true)
				region := fakeGCE.Region()
				for _, rule := range start.forwardingRules {
					if rule != nil {
						if err := fakeGCE.CreateRegionForwardingRule(rule, region); err != nil {
							t.Fatalf("Couldn't set up forwarding rule %v for test", rule)
						}
					}
				}

				// Act
				result, err := mgr.EnsureIPv4(bsLink)
				// Assert
				if err != nil {
					t.Errorf("EnsureIPv4() error = %v", err)
				}

				if diff := cmp.Diff(wantResult, result); diff != "" {
					t.Errorf("EnsureIPv4() mismatch (-want +got):\n%s", diff)
				}

				for _, unwantedRuleName := range []string{tcpName, udpName} {
					_, err := fakeGCE.GetRegionForwardingRule(unwantedRuleName, region)
					if err == nil || !utils.IsNotFoundError(err) {
						t.Errorf("fakeGCE.GetRegionForwardingRule(%s) got err %v, want not found err", unwantedRuleName, err)
					}
				}
			})
		}
	}
}

// TestMixedManagerNetLB_EnsureIPv4_Resync verifies that in case of
// no changes, the resync will not update any resources.
func TestMixedManagerNetLB_EnsureIPv4_Resync(t *testing.T) {
	t.Parallel()

	// Arrange
	ports := []api_v1.ServicePort{{Protocol: api_v1.ProtocolTCP, Port: 80}, {Protocol: api_v1.ProtocolUDP, Port: 53}}
	_, mgr := arrange(ports, true)
	// Run first to create resources
	if _, err := mgr.EnsureIPv4(bsLink); err != nil {
		t.Fatalf("EnsureIPv4() error = %v", err)
	}

	// Act
	result, err := mgr.EnsureIPv4(bsLink)
	// Assert
	if err != nil {
		t.Fatalf("EnsureIPv4() error = %v", err)
	}

	if result.SyncStatus != utils.ResourceResync {
		t.Errorf("EnsureIPv4() sync status = %v, want %v", result.SyncStatus, utils.ResourceResync)
	}
}

func fwdRule(name, protocol string, ports []string) *composite.ForwardingRule {
	return &composite.ForwardingRule{
		Version:             "ga",
		Scope:               "regional",
		BackendService:      bsLink,
		Description:         `{"networking.gke.io/service-name":"test-ns/test-svc","networking.gke.io/api-version":"ga","networking.gke.io/service-ip":"1.2.3.4"}`,
		IPAddress:           ip,
		IPProtocol:          protocol,
		LoadBalancingScheme: "EXTERNAL",
		Name:                name,
		NetworkTier:         "PREMIUM",
		Ports:               ports,
		Region:              "us-central1",
	}
}

func TestMixedManagerNetLB_EnsureIPv4_SingleProtocol(t *testing.T) {
	t.Parallel()

	for _, enableL3 := range []bool{false, true} {
		t.Run(fmt.Sprintf("l3DefaultEnabled=%t", enableL3), func(t *testing.T) {
			// Arrange
			_, mgr := arrange([]api_v1.ServicePort{{Protocol: api_v1.ProtocolTCP, Port: 8080}}, enableL3)

			// Act
			_, err := mgr.EnsureIPv4(bsLink)

			// Assert
			if err == nil {
				t.Errorf("Expected to receive error for single protocol service")
			}
		})
	}
}

func TestMixedManagerNetLB_AllRules(t *testing.T) {
	testCases := []struct {
		desc   string
		tcp    *compute.ForwardingRule
		udp    *compute.ForwardingRule
		legacy *compute.ForwardingRule
	}{
		{
			desc: "no rules",
		},
		{
			desc:   "legacy named exists",
			legacy: &compute.ForwardingRule{Name: legacyName},
		},
		{
			desc: "mixed protocol exists",
			tcp:  &compute.ForwardingRule{Name: tcpName},
			udp:  &compute.ForwardingRule{Name: udpName},
		},
		{
			desc:   "mixed protocol after failed cleanup",
			tcp:    &compute.ForwardingRule{Name: tcpName},
			udp:    &compute.ForwardingRule{Name: udpName},
			legacy: &compute.ForwardingRule{Name: legacyName},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			// Arrange
			fakeGCE, mgr := arrange(nil, true)
			region := fakeGCE.Region()
			for _, rule := range []*compute.ForwardingRule{tc.tcp, tc.udp, tc.legacy} {
				if rule != nil {
					fakeGCE.CreateRegionForwardingRule(rule, region)
				}
			}

			// Act
			rules, err := mgr.AllRules()
			// Assert
			if err != nil {
				t.Errorf("AllRules() error = %v", err)
			}
			if tc.legacy != nil && rules.Legacy == nil {
				t.Errorf("single protocol named forwarding rule was not found")
			}
			if tc.tcp != nil && rules.TCP == nil {
				t.Errorf("tcp forwarding rule for mixed protocol was not found")
			}
			if tc.udp != nil && rules.UDP == nil {
				t.Errorf("udp forwarding rule for mixed protocol was not found")
			}
		})
	}
}

// TestMixedManagerNetLB_EnsureIPv4_L3ExistsDuringMigration verifies that
// when there is a mixed protocol service that has been created using split rules
// (has both TCP and UDP rule) it can be migrated to L3 rule without traffic interruption
// by creating the L3 rule before TCP and UPD rules are deleted.
//
// This can be done thanks to:
// https://docs.cloud.google.com/load-balancing/docs/network/networklb-backend-service#forwarding-rule-selection
func TestMixedManagerNetLB_EnsureIPv4_L3ExistsDuringMigration(t *testing.T) {
	// Arrange
	fakeGCE, m := arrange([]api_v1.ServicePort{{Protocol: api_v1.ProtocolTCP, Port: 8080}, {Protocol: api_v1.ProtocolUDP, Port: 8080}}, false)

	rt := resourceTracker{}

	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	mockGCE.MockForwardingRules.PatchHook = func(ctx context.Context, k *meta.Key, fr *compute.ForwardingRule, mfr *cloud.MockForwardingRules, o ...cloud.Option) error {
		// For our use case here we should never attempt to modify Forwarding Rules
		return fmt.Errorf("forwarding rules are immutable")
	}

	backendServiceLink := "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/backendServices/k8s2-ai-ml-enabler"
	res, err := m.EnsureIPv4(backendServiceLink)
	if err != nil {
		t.Errorf("EnsureIPv4() error = %v", err)
	}

	// Check that split rules are present
	if res.TCPFwdRule == nil || res.UDPFwdRule == nil {
		t.Errorf("EnsureIPv4() didn't create TCP and UDP forwarding rules, got %v", res)
	}

	// Check that L3 rule is not present
	if res.L3FwdRule != nil {
		t.Errorf("EnsureIPv4() created L3 forwarding rule without the flag enabled, got %v", res)
	}

	// For the second call with the flag enabled let's monitor order of calls made
	mockGCE.MockForwardingRules.InsertHook = func(ctx context.Context, key *meta.Key, obj *compute.ForwardingRule, m *cloud.MockForwardingRules, options ...cloud.Option) (bool, error) {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		rt.ops = append(rt.ops, operation{name: obj.Name, opType: create})
		return false, nil
	}
	mockGCE.MockForwardingRules.DeleteHook = func(ctx context.Context, key *meta.Key, m *cloud.MockForwardingRules, options ...cloud.Option) (bool, error) {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		rt.ops = append(rt.ops, operation{name: key.Name, opType: delete})
		return false, nil
	}

	// Act
	m.L3DefaultEnabled = true
	res, err = m.EnsureIPv4(backendServiceLink)
	if err != nil {
		t.Errorf("EnsureIPv4() error = %v", err)
	}

	if res.TCPFwdRule != nil || res.UDPFwdRule != nil {
		t.Errorf("EnsureIPv4() didn't delete TCP and UDP forwarding rules, got %v", res)
	}
	if res.L3FwdRule == nil {
		t.Errorf("EnsureIPv4() didn't create L3 forwarding rule, got %v", res)
	}

	t.Logf("Ops performed during migration: %v", rt.ops)

	// Check that the L3 rule was created before the TCP and UDP rules were deleted
	rt.mu.Lock()
	defer rt.mu.Unlock()

	l3CreateIdx := len(rt.ops)
	for i, op := range rt.ops {
		if op.name == res.L3FwdRule.Name && op.opType == create {
			l3CreateIdx = i
			break
		}
	}

	for i, op := range rt.ops {
		if (op.name == tcpName || op.name == udpName) && op.opType == delete {
			if i < l3CreateIdx {
				t.Errorf("TCP or UDP forwarding rule was deleted before creation of L3 forwarding rule, i=%d, ops: %v", i, rt.ops)
			} else {
				t.Logf("Found delete op at idx=%d for name=%s, which is after L3 create op at idx=%d", i, op.name, l3CreateIdx)
			}
		}
	}
}

func TestMixedManagerNetLB_DeleteIPv4(t *testing.T) {
	testCases := []struct {
		desc   string
		tcp    *compute.ForwardingRule
		udp    *compute.ForwardingRule
		legacy *compute.ForwardingRule
	}{
		{
			desc: "no rules",
		},
		{
			desc:   "single protocol exists",
			legacy: &compute.ForwardingRule{Name: legacyName},
		},
		{
			desc: "mixed protocol exists",
			tcp:  &compute.ForwardingRule{Name: tcpName},
			udp:  &compute.ForwardingRule{Name: udpName},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			// Arrange
			fakeGCE, mgr := arrange(nil, false)
			region := fakeGCE.Region()
			for _, rule := range []*compute.ForwardingRule{tc.tcp, tc.udp, tc.legacy} {
				if rule != nil {
					fakeGCE.CreateRegionForwardingRule(rule, region)
				}
			}

			// Act
			err := mgr.DeleteIPv4()
			// Assert
			if err != nil {
				t.Errorf("DeleteIPv4() error = %v", err)
			}

			if tc.legacy != nil {
				rule, err := fakeGCE.GetRegionForwardingRule(legacyName, region)
				if err != nil || rule == nil {
					t.Errorf("single protocol named forwarding rule was deleted by mixed manager")
				}
			}

			ruleTCP, err := fakeGCE.GetRegionForwardingRule(tcpName, region)
			if ruleTCP != nil || !utils.IsNotFoundError(err) {
				t.Errorf("tcp forwarding rule for mixed protocol wasn't deleted")
			}

			ruleUDP, err := fakeGCE.GetRegionForwardingRule(udpName, region)
			if ruleUDP != nil || !utils.IsNotFoundError(err) {
				t.Errorf("udp forwarding rule for mixed protocol wasn't deleted")
			}
		})
	}
}

func arrange(ports []api_v1.ServicePort, l3DefaultEnabled bool) (*gce.Cloud, *forwardingrules.MixedManagerNetLB) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	mockGCE.MockAddresses.GetHook = func(ctx context.Context, key *meta.Key, m *cloud.MockAddresses, options ...cloud.Option) (bool, *compute.Address, error) {
		// fakeGCE by default returns just inserted values
		// however if we insert empty address we should get address automatically filed by GCE
		if key.Name == "aksuid123" {
			return true, &compute.Address{
				Name:        "aksuid123",
				AddressType: "EXTERNAL",
				IpVersion:   "IPv4",
				NetworkTier: "PREMIUM",
				Address:     ip,
			}, nil
		}
		return false, nil, nil
	}

	m := &forwardingrules.MixedManagerNetLB{
		Namer:            namer.NewL4Namer(kubeSystemUID, nil),
		Provider:         forwardingrules.New(fakeGCE, meta.VersionGA, meta.Regional, klog.TODO()),
		Recorder:         &record.FakeRecorder{},
		Logger:           klog.TODO(),
		Cloud:            fakeGCE,
		L3DefaultEnabled: l3DefaultEnabled,
		Service: &api_v1.Service{
			ObjectMeta: meta_v1.ObjectMeta{
				UID:       kubeSystemUID,
				Namespace: namespace,
				Name:      name,
			},
			Spec: api_v1.ServiceSpec{
				Ports: ports,
			},
		},
	}
	return fakeGCE, m
}

type resourceTracker struct {
	mu  sync.Mutex
	ops []operation
}

type operation struct {
	name   string
	opType operationType
}

// enum for operation type
type operationType string

const (
	create operationType = "create"
	update operationType = "update"
	delete operationType = "delete"
)
