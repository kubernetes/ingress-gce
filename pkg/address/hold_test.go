package address_test

import (
	"context"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	compute "google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/address"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/forwardingrules"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

const (
	kubeSystemUID  = "ksuid123"
	namespace      = "test-ns"
	name           = "test-svc"
	legacyName     = "aksuid123"
	tcpName        = "k8s2-tcp-axyqjz2d-test-ns-test-svc-pyn67fp6"
	udpName        = "k8s2-udp-axyqjz2d-test-ns-test-svc-pyn67fp6"
	legacyNameIPv6 = "aksuid123-ipv6"
	tcpNameIPv6    = "k8s2-tcp-axyqjz2d-test-ns-test-svc-pyn67fp6-ipv6"
	udpNameIPv6    = "k8s2-udp-axyqjz2d-test-ns-test-svc-pyn67fp6-ipv6"
	region         = "us-central1"
	subnetURL      = "https://www.googleapis.com/compute/v1/projects/mock-project/regions/us-central1/subnetworks/default"
)

func TestHoldExternalIPv4(t *testing.T) {
	testCases := []struct {
		desc          string
		existingRules []*composite.ForwardingRule
		service       *api_v1.Service
		want          address.HoldResult
		wantTearDown  bool
	}{
		{
			desc: "no address passed",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
				},
			},
			want: address.HoldResult{
				IP:      notReservedIPv4,
				Managed: address.IPAddrManaged,
			},
		},
		{
			desc: "existing address",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Annotations: map[string]string{
						"networking.gke.io/load-balancer-ip-addresses": reservedIPv4Name,
					},
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
				},
			},
			want: address.HoldResult{
				IP:      reservedIPv4,
				Managed: address.IPAddrManaged,
			},
		},
		{
			desc: "existing address dual stack IPv4 first",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Annotations: map[string]string{
						"networking.gke.io/load-balancer-ip-addresses": reservedIPv4Name + "," + reservedIPv6Name,
					},
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
				},
			},
			want: address.HoldResult{
				IP:      reservedIPv4,
				Managed: address.IPAddrManaged,
			},
		},
		{
			desc: "existing address dual stack IPv6 first",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Annotations: map[string]string{
						"networking.gke.io/load-balancer-ip-addresses": reservedIPv6Name + "," + reservedIPv4Name,
					},
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
				},
			},
			want: address.HoldResult{
				IP:      reservedIPv4,
				Managed: address.IPAddrManaged,
			},
		},
		{
			desc: "legacy forwarding rule exists",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
				},
			},
			existingRules: []*composite.ForwardingRule{
				{
					Name:      legacyName,
					IPAddress: notReservedIPv4,
					Region:    region,
				},
			},
			want: address.HoldResult{
				IP:      notReservedIPv4,
				Managed: address.IPAddrManaged,
			},
		},
		{
			desc: "mixed protocol forwarding rule exist",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
				},
			},
			existingRules: []*composite.ForwardingRule{
				{
					Name:      tcpName,
					IPAddress: notReservedIPv4,
					Region:    region,
				},
				{
					Name:      udpName,
					IPAddress: notReservedIPv4,
					Region:    region,
				},
			},
			want: address.HoldResult{
				IP:      notReservedIPv4,
				Managed: address.IPAddrManaged,
			},
		},
		{
			desc: "tier from annotation: Premium to Premium",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
					Annotations: map[string]string{
						annotations.NetworkTierAnnotationKey: "Premium",
					},
				},
			},
			existingRules: []*composite.ForwardingRule{
				{
					Name:        legacyName,
					IPAddress:   notReservedIPv4,
					Region:      region,
					NetworkTier: cloud.NetworkTierPremium.ToGCEValue(),
				},
			},
			want: address.HoldResult{
				IP:      notReservedIPv4,
				Managed: address.IPAddrManaged,
			},
			wantTearDown: false,
		},
		{
			desc: "tier from annotation: Standard to Premium",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
					Annotations: map[string]string{
						annotations.NetworkTierAnnotationKey: "Premium",
					},
				},
			},
			existingRules: []*composite.ForwardingRule{
				{
					Name:        legacyName,
					IPAddress:   notReservedIPv4,
					Region:      region,
					NetworkTier: cloud.NetworkTierStandard.ToGCEValue(),
				},
			},
			want: address.HoldResult{
				IP:      notReservedIPv4,
				Managed: address.IPAddrManaged,
			},
			wantTearDown: true,
		},
		{
			desc: "tier from annotation: Premium to Standard",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
					Annotations: map[string]string{
						annotations.NetworkTierAnnotationKey: "Standard",
					},
				},
			},
			existingRules: []*composite.ForwardingRule{
				{
					Name:        legacyName,
					IPAddress:   notReservedIPv4,
					Region:      region,
					NetworkTier: cloud.NetworkTierPremium.ToGCEValue(),
				},
			},
			want: address.HoldResult{
				IP:      notReservedIPv4,
				Managed: address.IPAddrManaged,
			},
			wantTearDown: true,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			t.Parallel()
			// Arrange
			cfg := arrangeHoldExternal(t, tC.existingRules, tC.service, address.IPv4Version, "")

			// Act
			got, err := address.HoldExternal(cfg)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}

			// Assert
			if got.IP != tC.want.IP {
				t.Errorf("address.IPv4ToUse(_).IP = %q, want %q", got.IP, tC.want.IP)
			}
			if got.Managed != tC.want.Managed {
				t.Errorf("address.IPv4ToUse(_).Managed = %q, want %q", got.Managed, tC.want.Managed)
			}

			// Verify that address exists
			region := cfg.Cloud.Region()
			addr, err := cfg.Cloud.GetRegionAddressByIP(region, got.IP)
			if err != nil || addr == nil {
				t.Fatalf("unexpected err: %v", err)
			}

			// Check if release cleans up address
			if got.Release == nil {
				t.Fatal("address.IPv4ToUse(_).Release is nil")
			}
			if err := got.Release(); err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			addr, err = cfg.Cloud.GetRegionAddress(legacyName, region)
			if utils.IgnoreHTTPNotFound(err) != nil {
				t.Errorf("GetRegionAddress returned err: %v", err)
			}
			if addr != nil {
				t.Errorf("address %v not deleted", legacyName)
			}

			// Check teardown
			for _, rule := range tC.existingRules {
				if rule != nil {
					got, err := cfg.Cloud.GetRegionForwardingRule(rule.Name, rule.Region)
					if utils.IgnoreHTTPNotFound(err) != nil {
						t.Errorf("GetRegionForwardingRule returned unexpected err: %v", err)
						continue
					}
					wasDeleted := got == nil
					if wasDeleted != tC.wantTearDown {
						t.Errorf("%v deleted = %v, want %v", rule.Name, wasDeleted, tC.wantTearDown)
					}
				}
			}
		})
	}
}

func TestHoldExternalIPv6(t *testing.T) {
	testCases := []struct {
		desc          string
		existingRules []*composite.ForwardingRule
		service       *api_v1.Service
		want          address.HoldResult
		wantTearDown  bool
	}{
		{
			desc: "no address passed",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
				},
			},
			want: address.HoldResult{
				IP:      notReservedIPv6,
				Managed: address.IPAddrManaged,
			},
		},
		{
			desc: "existing address",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Annotations: map[string]string{
						"networking.gke.io/load-balancer-ip-addresses": reservedIPv6Name,
					},
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
				},
			},
			want: address.HoldResult{
				IP:      reservedIPv6,
				Managed: address.IPAddrManaged,
			},
		},
		{
			desc: "existing address dual stack IPv4 first",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Annotations: map[string]string{
						"networking.gke.io/load-balancer-ip-addresses": reservedIPv4Name + "," + reservedIPv6Name,
					},
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
				},
			},
			want: address.HoldResult{
				IP:      reservedIPv6,
				Managed: address.IPAddrManaged,
			},
		},
		{
			desc: "existing address dual stack IPv6 first",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Annotations: map[string]string{
						"networking.gke.io/load-balancer-ip-addresses": reservedIPv6Name + "," + reservedIPv4Name,
					},
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
				},
			},
			want: address.HoldResult{
				IP:      reservedIPv6,
				Managed: address.IPAddrManaged,
			},
		},
		{
			desc: "legacy forwarding rule exists",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
				},
			},
			existingRules: []*composite.ForwardingRule{
				{
					Name:      legacyNameIPv6,
					IPAddress: notReservedIPv6,
					Region:    region,
				},
			},
			want: address.HoldResult{
				IP:      notReservedIPv6,
				Managed: address.IPAddrManaged,
			},
		},
		{
			desc: "mixed protocol forwarding rule exist",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
				},
			},
			existingRules: []*composite.ForwardingRule{
				{
					Name:      tcpNameIPv6,
					IPAddress: notReservedIPv6,
					Region:    region,
				},
				{
					Name:      udpNameIPv6,
					IPAddress: notReservedIPv6,
					Region:    region,
				},
			},
			want: address.HoldResult{
				IP:      notReservedIPv6,
				Managed: address.IPAddrManaged,
			},
		},
		{
			desc: "tier from annotation: Premium to Premium",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
					Annotations: map[string]string{
						annotations.NetworkTierAnnotationKey: "Premium",
					},
				},
			},
			existingRules: []*composite.ForwardingRule{
				{
					Name:        legacyNameIPv6,
					IPAddress:   notReservedIPv6,
					Region:      region,
					NetworkTier: cloud.NetworkTierPremium.ToGCEValue(),
				},
			},
			want: address.HoldResult{
				IP:      notReservedIPv6,
				Managed: address.IPAddrManaged,
			},
			wantTearDown: false,
		},
		{
			// While this shouldn't be possible
			// There was a bug that allowed to create Standard tier IPv6 Forwarding rules
			desc: "tier from annotation: Standard to Premium",
			service: &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
					UID:       kubeSystemUID,
					Annotations: map[string]string{
						annotations.NetworkTierAnnotationKey: "Premium",
					},
				},
			},
			existingRules: []*composite.ForwardingRule{
				{
					Name:        legacyNameIPv6,
					IPAddress:   notReservedIPv6,
					Region:      region,
					NetworkTier: cloud.NetworkTierStandard.ToGCEValue(),
				},
			},
			want: address.HoldResult{
				IP:      notReservedIPv6,
				Managed: address.IPAddrManaged,
			},
			wantTearDown: true,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			t.Parallel()
			// Arrange
			cfg := arrangeHoldExternal(t, tC.existingRules, tC.service, address.IPv6Version, subnetURL)

			// Act
			got, err := address.HoldExternal(cfg)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}

			// Assert
			if got.IP != tC.want.IP {
				t.Errorf("address.HoldExternal(_).IP = %q, want %q", got.IP, tC.want.IP)
			}
			if got.Managed != tC.want.Managed {
				t.Errorf("address.HoldExternal(_).Managed = %q, want %q", got.Managed, tC.want.Managed)
			}

			// Verify that address exists
			region := cfg.Cloud.Region()
			addr, err := cfg.Cloud.GetRegionAddressByIP(region, got.IP)
			if err != nil || addr == nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if addr.Subnetwork != subnetURL {
				t.Errorf("addr.Subnetwork = %q, want %q", addr.Subnetwork, subnetURL)
			}

			// Check if release cleans up address
			if got.Release == nil {
				t.Fatal("address.HoldExternal(_).Release is nil")
			}
			if err := got.Release(); err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			addr, err = cfg.Cloud.GetRegionAddress(legacyNameIPv6, region)
			if utils.IgnoreHTTPNotFound(err) != nil {
				t.Errorf("GetRegionAddress returned err: %v", err)
			}
			if addr != nil {
				t.Errorf("address %v not deleted", legacyNameIPv6)
			}

			// Check teardown
			for _, rule := range tC.existingRules {
				if rule != nil {
					got, err := cfg.Cloud.GetRegionForwardingRule(rule.Name, rule.Region)
					if utils.IgnoreHTTPNotFound(err) != nil {
						t.Errorf("GetRegionForwardingRule returned unexpected err: %v", err)
						continue
					}
					wasDeleted := got == nil
					if wasDeleted != tC.wantTearDown {
						t.Errorf("%v deleted = %v, want %v", rule.Name, wasDeleted, tC.wantTearDown)
					}
				}
			}
		})
	}
}

func arrangeHoldExternal(t *testing.T, existingRules []*composite.ForwardingRule, service *api_v1.Service, ipVersion address.IPVersion, subnet string) address.HoldConfig {
	t.Helper()

	vals := gce.DefaultTestClusterValues()
	fakeGCE := gce.NewFakeGCECloud(vals)

	if err := fakeGCE.ReserveRegionAddress(&compute.Address{
		Name:        reservedIPv4Name,
		Region:      vals.Region,
		Address:     reservedIPv4,
		AddressType: "EXTERNAL",
		IpVersion:   "IPV4",
		Subnetwork:  subnet,
	}, vals.Region); err != nil {
		t.Fatal(err)
	}
	if err := fakeGCE.ReserveRegionAddress(&compute.Address{
		Name:        reservedIPv6Name,
		Region:      vals.Region,
		Address:     reservedIPv6,
		AddressType: "EXTERNAL",
		IpVersion:   "IPV6",
		NetworkTier: "PREMIUM",
		Subnetwork:  subnet,
	}, vals.Region); err != nil {
		t.Fatal(err)
	}

	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	mockGCE.MockAddresses.GetHook = func(ctx context.Context, key *meta.Key, m *cloud.MockAddresses, options ...cloud.Option) (bool, *compute.Address, error) {
		// fakeGCE by default returns just inserted values
		// however if we insert empty address we should get address automatically filed by GCE
		if obj, ok := m.Objects[*key]; ok {
			ga := obj.ToGA()
			if ga.Address == "" {
				isIPv4 := ga.IpVersion == "IPV4" || ga.IpVersion == ""
				if isIPv4 {
					ga.Address = notReservedIPv4
				} else {
					ga.Address = notReservedIPv6
				}
			}
			m.Objects[*key] = m.Obj(ga)
			return true, obj.ToGA(), nil
		}
		return false, nil, nil
	}
	provider := forwardingrules.New(fakeGCE, meta.VersionGA, meta.Regional, klog.TODO())
	for _, rule := range existingRules {
		if err := provider.Create(rule); err != nil {
			t.Fatal(err)
		}
	}

	return address.HoldConfig{
		Cloud:                 fakeGCE,
		Recorder:              &record.FakeRecorder{},
		Logger:                klog.TODO(),
		ExistingRules:         existingRules,
		Service:               service,
		ForwardingRuleDeleter: provider,
		IPVersion:             ipVersion,
		SubnetworkURL:         subnet,
	}
}
