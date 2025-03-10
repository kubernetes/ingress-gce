package loadbalancers

import (
	"net/http"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/forwardingrules"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

func TestGetEffectiveIP(t *testing.T) {
	testCases := []struct {
		desc        string
		address     *composite.Address
		scope       meta.KeyType
		wantIp      string
		wantManaged bool
		wantErr     bool
	}{
		{
			desc:        "L7 ILB with Address created",
			address:     &composite.Address{Name: "test-ilb", Address: "10.2.3.4"},
			scope:       meta.Regional,
			wantIp:      "10.2.3.4",
			wantManaged: false,
			wantErr:     false,
		},
		{
			desc:        "L7 ILB without address created",
			scope:       meta.Regional,
			wantManaged: true,
			wantErr:     false,
		},
		{
			desc:        "XLB with Address created",
			address:     &composite.Address{Name: "test-ilb", Address: "35.2.3.4"},
			scope:       meta.Global,
			wantIp:      "35.2.3.4",
			wantManaged: false,
			wantErr:     false,
		},
		{
			desc:        "XLB without Address created",
			scope:       meta.Global,
			wantManaged: true,
			wantErr:     false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			l7 := L7{
				cloud:       fakeGCE,
				scope:       tc.scope,
				runtimeInfo: &L7RuntimeInfo{StaticIPName: ""},
			}

			// Create Address if specified
			if tc.address != nil {
				key, err := l7.CreateKey(tc.address.Name)
				if err != nil {
					t.Fatal(err)
				}
				err = composite.CreateAddress(fakeGCE, key, tc.address, klog.TODO())
				if err != nil {
					t.Fatal(err)
				}
				l7.runtimeInfo.StaticIPName = tc.address.Name
			}

			ip, managed, err := l7.getEffectiveIP()
			if (err != nil) != tc.wantErr {
				t.Errorf("getEffectiveIP() error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if tc.address != nil && ip != tc.wantIp {
				t.Errorf("getEffectiveIP() ip = %v, want %v", ip, tc.wantIp)
			}
			if managed != tc.wantManaged {
				t.Errorf("getEffectiveIP() managed = %v, want %v", managed, tc.wantManaged)
			}
		})
	}
}

func TestForwardingRulesEqual(t *testing.T) {
	t.Parallel()

	fwdRuleTCP := &composite.ForwardingRule{
		Name:                "tcp-fwd-rule",
		IPAddress:           "10.0.0.0",
		Ports:               []string{"123"},
		IPProtocol:          "TCP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
	}
	fwdRuleUDP := &composite.ForwardingRule{
		Name:                "udp-fwd-rule",
		IPAddress:           "10.0.0.0",
		Ports:               []string{"123"},
		IPProtocol:          "UDP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
	}

	for _, tc := range []struct {
		desc                   string
		oldFwdRule             *composite.ForwardingRule
		newFwdRule             *composite.ForwardingRule
		discretePortForwarding bool
		expectEqual            bool
	}{
		{
			desc: "empty ip address does not match valid ip",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "empty-ip-address-fwd-rule",
				IPAddress:           "",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule:  fwdRuleTCP,
			expectEqual: false,
		},
		{
			desc:       "global access enabled",
			oldFwdRule: fwdRuleTCP,
			newFwdRule: &composite.ForwardingRule{
				Name:                "global-access-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				AllowGlobalAccess:   true,
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			expectEqual: false,
		},
		{
			desc:        "IP protocol changed",
			oldFwdRule:  fwdRuleTCP,
			newFwdRule:  fwdRuleUDP,
			expectEqual: false,
		},
		{
			desc:        "same forwarding rule",
			oldFwdRule:  fwdRuleTCP,
			newFwdRule:  fwdRuleTCP,
			expectEqual: true,
		},
		{
			desc: "same forwarding rule, different basepath",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "fwd-rule-bs-link1",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://compute.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "fwd-rule-bs-link2",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			expectEqual: true,
		},
		{
			desc:       "same forwarding rule, one uses ALL keyword for ports",
			oldFwdRule: fwdRuleUDP,
			newFwdRule: &composite.ForwardingRule{
				Name:                "udp-fwd-rule-all-ports",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				AllPorts:            true,
				IPProtocol:          "UDP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				NetworkTier:         cloud.NetworkTierPremium.ToGCEValue(),
			},
			expectEqual: false,
		},
		{
			desc: "network tier mismatch",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "fwd-rule-bs-link2-standard-ntier",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				NetworkTier:         string(cloud.NetworkTierStandard),
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "fwd-rule-bs-link2-premium-ntier",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				NetworkTier:         cloud.NetworkTierPremium.ToGCEValue(),
			},
			expectEqual: false,
		},
		{
			desc: "same forwarding rule, port range mismatch",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				PortRange:           "1-2",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				PortRange:           "2-3",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			expectEqual: false,
		},
		{
			desc: "same forwarding rule, ports mismatch",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"1", "2"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"2", "3"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			expectEqual: false,
		},
		{
			desc: "port range, discrete ports disabled",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				PortRange:           "1-3",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"1", "3"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			expectEqual: false,
		},
		{
			desc: "port range, discrete ports enabled, from range to ports",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				PortRange:           "1-3",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"1", "2", "3"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			discretePortForwarding: true,
			expectEqual:            true,
		},
		{
			desc: "port range, discrete ports enabled, from range to ports, port outside of range",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				PortRange:           "1-3",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"1", "3", "5"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			discretePortForwarding: true,
			expectEqual:            false,
		},
		{
			desc: "port range, discrete ports enabled, from ports to range",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"1", "2", "3"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				PortRange:           "1-3",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			discretePortForwarding: true,
			expectEqual:            false,
		},
		{
			desc: "same forwarding rule, ports vs port ranges mismatch",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				PortRange:           "1-3",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"1", "5"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			},
			expectEqual: false,
		},
		{
			desc: "network mismatch",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-other-vpc",
			},
			expectEqual: false,
		},
		{
			desc: "subnetwork mismatch",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/other-subnet",
			},
			expectEqual: false,
		},
		{
			desc: "equal network data",
			oldFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet",
			},
			newFwdRule: &composite.ForwardingRule{
				Name:                "tcp-fwd-rule",
				IPAddress:           "10.0.0.0",
				Ports:               []string{"123"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
				Network:             "projects/test-poject/global/networks/test-vpc",
				Subnetwork:          "projects/test-poject/regions/us-central1/subnetworks/default-subnet",
			},
			expectEqual: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			flags.F.EnableDiscretePortForwarding = tc.discretePortForwarding
			got, err := Equal(tc.oldFwdRule, tc.newFwdRule)
			if err != nil {
				t.Errorf("forwardingRulesEqual(_, _) = %v, want nil error", err)
			}
			if got != tc.expectEqual {
				t.Errorf("forwardingRulesEqual(_, _) = %t, want %t", got, tc.expectEqual)
			}
		})
	}
}

func TestL4CreateExternalForwardingRuleAddressAlreadyInUse(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	targetIP := "1.1.1.1"
	l4 := L4NetLB{
		cloud:           fakeGCE,
		forwardingRules: forwardingrules.New(fakeGCE, meta.VersionGA, meta.Regional),
		Service: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "testService", Namespace: "default", UID: types.UID("1")},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Port: 8080,
					},
				},
				Type:           "LoadBalancer",
				LoadBalancerIP: targetIP,
			},
		},
	}

	addr := &compute.Address{Name: "my-important-address", Address: targetIP, AddressType: string(cloud.SchemeExternal)}
	fakeGCE.ReserveRegionAddress(addr, fakeGCE.Region())
	insertError := &googleapi.Error{Code: http.StatusBadRequest, Message: "Invalid value for field 'resource.IPAddress': '1.1.1.1'. Specified IP address is in-use and would result in a conflict., invalid"}
	fakeGCE.Compute().(*cloud.MockGCE).MockForwardingRules.InsertHook = test.InsertForwardingRuleErrorHook(insertError)
	_, _, err := l4.ensureIPv4ForwardingRule("link")

	require.Error(t, err)
	assert.True(t, utils.IsIPConfigurationError(err))
}

func TestL4EnsureIPv4ForwardingRuleAddressAlreadyInUse(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	targetIP := "1.1.1.1"
	l4 := L4{
		cloud:           fakeGCE,
		forwardingRules: forwardingrules.New(fakeGCE, meta.VersionGA, meta.Regional),
		namer:           namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall")),
		Service: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "testService", Namespace: "default", UID: types.UID("1")},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Port: 8080,
					},
				},
				Type:           "LoadBalancer",
				LoadBalancerIP: targetIP,
			},
		},
	}

	addr := &compute.Address{Name: "my-important-address", Address: targetIP, AddressType: string(cloud.SchemeInternal)}
	fakeGCE.ReserveRegionAddress(addr, fakeGCE.Region())
	insertError := &googleapi.Error{Code: http.StatusConflict, Message: "IP_IN_USE_BY_ANOTHER_RESOURCE - IP '10.107.116.14' is already being used by another resource."}
	fakeGCE.Compute().(*cloud.MockGCE).MockForwardingRules.InsertHook = test.InsertForwardingRuleErrorHook(insertError)
	_, err := l4.ensureIPv4ForwardingRule("link", gce.ILBOptions{}, nil, "subnetworkX", "1.1.1.1")

	require.Error(t, err)
	assert.True(t, utils.IsIPConfigurationError(err))
}

func TestL4EnsureIPv4ForwardingRule(t *testing.T) {
	subnetworkURL := "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet"
	networkURL := "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc"
	l4namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall"))
	serviceName := "testService"
	serviceNamespace := "default"
	defaultService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: serviceNamespace, UID: types.UID("1")},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:     8080,
					Protocol: corev1.ProtocolTCP,
				},
			},
			Type: "LoadBalancer",
		},
	}

	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	forwardingRules := forwardingrules.New(fakeGCE, meta.VersionGA, meta.Regional)

	l4 := &L4{
		cloud:           fakeGCE,
		forwardingRules: forwardingRules,
		namer:           l4namer,
		Service:         defaultService,
		network: network.NetworkInfo{
			IsDefault:  false,
			NetworkURL: networkURL,
		},
	}
	ipToUse := "1.1.1.1"
	bsLink := "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1"

	forwardingRule, err := l4.ensureIPv4ForwardingRule(bsLink, gce.ILBOptions{}, nil, subnetworkURL, ipToUse)
	require.NoError(t, err)

	wantForwardingRule := &composite.ForwardingRule{
		Name:                l4namer.L4ForwardingRule(serviceNamespace, serviceName, "tcp"),
		IPAddress:           "1.1.1.1",
		Ports:               []string{"8080"},
		IPProtocol:          "TCP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		Subnetwork:          subnetworkURL,
		Network:             networkURL,
		NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
		Version:             meta.VersionGA,
		BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
		AllowGlobalAccess:   false,
		Description:         ilbServiceDescription(t, serviceName, serviceNamespace, "1.1.1.1"),
	}
	if diff := cmp.Diff(wantForwardingRule, forwardingRule, cmpopts.IgnoreFields(composite.ForwardingRule{}, "SelfLink", "Region", "Scope")); diff != "" {
		t.Errorf("ensureIPv4ForwardingRule() diff -want +got\n%v\n", diff)
	}

}

func ilbServiceDescription(t *testing.T, svcName, svcNamespace, ipToUse string) string {
	description, err := utils.MakeL4LBServiceDescription(utils.ServiceKeyFunc(svcNamespace, svcName), ipToUse,
		meta.VersionGA, false, utils.ILB)
	if err != nil {
		t.Errorf("utils.MakeL4LBServiceDescription() failed, err=%v", err)
	}
	return description
}
