package loadbalancers

import (
	"net/http"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/forwardingrules"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
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
				err = composite.CreateAddress(fakeGCE, key, tc.address)
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

	fwdRules := []*composite.ForwardingRule{
		{
			Name:                "empty-ip-address-fwd-rule",
			IPAddress:           "",
			Ports:               []string{"123"},
			IPProtocol:          "TCP",
			LoadBalancingScheme: string(cloud.SchemeInternal),
			BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
		},
		{
			Name:                "tcp-fwd-rule",
			IPAddress:           "10.0.0.0",
			Ports:               []string{"123"},
			IPProtocol:          "TCP",
			LoadBalancingScheme: string(cloud.SchemeInternal),
			BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
		},
		{
			Name:                "udp-fwd-rule",
			IPAddress:           "10.0.0.0",
			Ports:               []string{"123"},
			IPProtocol:          "UDP",
			LoadBalancingScheme: string(cloud.SchemeInternal),
			BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
		},
		{
			Name:                "global-access-fwd-rule",
			IPAddress:           "10.0.0.0",
			Ports:               []string{"123"},
			IPProtocol:          "TCP",
			LoadBalancingScheme: string(cloud.SchemeInternal),
			AllowGlobalAccess:   true,
			BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
		},
		{
			Name:                "fwd-rule-bs-link1",
			IPAddress:           "10.0.0.0",
			Ports:               []string{"123"},
			IPProtocol:          "TCP",
			LoadBalancingScheme: string(cloud.SchemeInternal),
			BackendService:      "http://compute.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
		},
		{
			Name:                "fwd-rule-bs-link2",
			IPAddress:           "10.0.0.0",
			Ports:               []string{"123"},
			IPProtocol:          "TCP",
			LoadBalancingScheme: string(cloud.SchemeInternal),
			BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
		},
		{
			Name:                "udp-fwd-rule-all-ports",
			IPAddress:           "10.0.0.0",
			Ports:               []string{"123"},
			AllPorts:            true,
			IPProtocol:          "UDP",
			LoadBalancingScheme: string(cloud.SchemeInternal),
			BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			NetworkTier:         cloud.NetworkTierPremium.ToGCEValue(),
		},
		{
			Name:                "fwd-rule-bs-link2-standard-ntier",
			IPAddress:           "10.0.0.0",
			Ports:               []string{"123"},
			IPProtocol:          "TCP",
			LoadBalancingScheme: string(cloud.SchemeInternal),
			BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			NetworkTier:         string(cloud.NetworkTierStandard),
		},
		{
			Name:                "fwd-rule-bs-link2-premium-ntier",
			IPAddress:           "10.0.0.0",
			Ports:               []string{"123"},
			IPProtocol:          "TCP",
			LoadBalancingScheme: string(cloud.SchemeInternal),
			BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
			NetworkTier:         cloud.NetworkTierPremium.ToGCEValue(),
		},
	}

	frPortRange1 := &composite.ForwardingRule{
		Name:                "tcp-fwd-rule",
		IPAddress:           "10.0.0.0",
		PortRange:           "2-3",
		IPProtocol:          "TCP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
	}
	frPortRange2 := &composite.ForwardingRule{
		Name:                "tcp-fwd-rule",
		IPAddress:           "10.0.0.0",
		PortRange:           "1-2",
		IPProtocol:          "TCP",
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1",
	}

	for _, tc := range []struct {
		desc       string
		oldFwdRule *composite.ForwardingRule
		newFwdRule *composite.ForwardingRule
		expect     bool
	}{
		{
			desc:       "empty ip address does not match valid ip",
			oldFwdRule: fwdRules[0],
			newFwdRule: fwdRules[1],
			expect:     false,
		},
		{
			desc:       "global access enabled",
			oldFwdRule: fwdRules[1],
			newFwdRule: fwdRules[3],
			expect:     false,
		},
		{
			desc:       "IP protocol changed",
			oldFwdRule: fwdRules[1],
			newFwdRule: fwdRules[2],
			expect:     false,
		},
		{
			desc:       "same forwarding rule",
			oldFwdRule: fwdRules[3],
			newFwdRule: fwdRules[3],
			expect:     true,
		},
		{
			desc:       "same forwarding rule, different basepath",
			oldFwdRule: fwdRules[4],
			newFwdRule: fwdRules[5],
			expect:     true,
		},
		{
			desc:       "same forwarding rule, one uses ALL keyword for ports",
			oldFwdRule: fwdRules[2],
			newFwdRule: fwdRules[6],
			expect:     false,
		},
		{
			desc:       "network tier mismatch",
			oldFwdRule: fwdRules[6],
			newFwdRule: fwdRules[7],
			expect:     false,
		},
		{
			desc:       "same forwarding rule, different port ranges",
			oldFwdRule: frPortRange1,
			newFwdRule: frPortRange2,
			expect:     false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := Equal(tc.oldFwdRule, tc.newFwdRule)
			if err != nil {
				t.Errorf("forwardingRulesEqual(_, _) = %v, want nil error", err)
			}
			if got != tc.expect {
				t.Errorf("forwardingRulesEqual(_, _) = %t, want %t", got, tc.expect)
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
