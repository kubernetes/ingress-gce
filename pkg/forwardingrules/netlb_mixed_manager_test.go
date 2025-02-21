package forwardingrules_test

import (
	"context"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/forwardingrules"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
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

func TestMixedManagerNetLB_EnsureIPv4(t *testing.T) {
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
			t.Run(desc, func(t *testing.T) {
				t.Parallel()

				// Arrange
				fakeGCE, mgr := arrange(end.ports)
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
	// Arrange
	_, mgr := arrange([]api_v1.ServicePort{
		{
			Protocol: api_v1.ProtocolTCP,
			Port:     8080,
		},
	})

	// Act
	_, err := mgr.EnsureIPv4(bsLink)

	// Assert
	if err == nil {
		t.Errorf("Expected to receive error for single protocol service")
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
			fakeGCE, mgr := arrange(nil)
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
			fakeGCE, mgr := arrange(nil)
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

func arrange(ports []api_v1.ServicePort) (*gce.Cloud, *forwardingrules.MixedManagerNetLB) {
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
		Namer:    namer.NewL4Namer(kubeSystemUID, nil),
		Provider: forwardingrules.New(fakeGCE, meta.VersionGA, meta.Regional, klog.TODO()),
		Recorder: &record.FakeRecorder{},
		Logger:   klog.TODO(),
		Cloud:    fakeGCE,
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
