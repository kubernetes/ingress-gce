package loadbalancers

import (
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/legacy-cloud-providers/gce"
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
		},
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
