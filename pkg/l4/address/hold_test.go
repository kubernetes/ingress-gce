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
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4/address"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	"k8s.io/ingress-gce/pkg/l4/forwardingrules"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"

	"k8s.io/cloud-provider-gcp/providers/gce"
)

const (
	kubeSystemUID = "ksuid123"
	namespace     = "test-ns"
	name          = "test-svc"
	tcpName       = "k8s2-tcp-axyqjz2d-test-ns-test-svc-pyn67fp6"
	udpName       = "k8s2-udp-axyqjz2d-test-ns-test-svc-pyn67fp6"
	legacyName    = "aksuid123"
	region        = "us-central1"
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
				Managed: address.IPAddrUnmanaged,
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
		tC := tC
		t.Run(tC.desc, func(t *testing.T) {
			t.Parallel()
			// Arrange
			cfg := arrange(t, tC.existingRules, tC.service)

			// Act
			got, err := address.HoldExternalIPv4(cfg)
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

func arrange(t *testing.T, existingRules []*composite.ForwardingRule, service *api_v1.Service) address.HoldConfig {
	t.Helper()

	vals := gce.DefaultTestClusterValues()
	fakeGCE := gce.NewFakeGCECloud(vals)

	addr := &compute.Address{
		Name:        reservedIPv4Name,
		Region:      vals.Region,
		Address:     reservedIPv4,
		AddressType: "EXTERNAL",
		NetworkTier: "PREMIUM",
	}
	if err := fakeGCE.ReserveRegionAddress(addr, vals.Region); err != nil {
		t.Fatal(err)
	}

	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	mockGCE.MockAddresses.GetHook = func(ctx context.Context, key *meta.Key, m *cloud.MockAddresses, options ...cloud.Option) (bool, *compute.Address, error) {
		// fakeGCE by default returns just inserted values
		// however if we insert empty address we should get address automatically filed by GCE
		if obj, ok := m.Objects[*key]; ok {
			ga := obj.ToGA()
			if ga.Address == "" {
				ga.Address = notReservedIPv4
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
	}
}
