package loadbalancers

import (
	"testing"

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
