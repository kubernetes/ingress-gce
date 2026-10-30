package address_test

import (
	"testing"

	"google.golang.org/api/compute/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4/address"
	"k8s.io/ingress-gce/pkg/l4/resources"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
	"k8s.io/klog/v2"

	"k8s.io/cloud-provider-gcp/providers/gce"
)

const (
	reservedIPv4Name = "reserved-ipv4"
	reservedIPv4     = "35.193.28.0"
	notReservedIPv4  = "35.193.28.1"
	reservedIPv6Name = "reserved-ipv6"
	reservedIPv6     = "2001:db8::1"
	notReservedIPv6  = "2001:db8::2"
)

func TestIPv4ToUse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		desc        string
		svc         *apiv1.Service
		fwd         *composite.ForwardingRule
		want        string
		name        string
		wantUserErr bool
	}{
		{
			desc: "nothing exists",
			svc:  &apiv1.Service{},
			want: "",
			name: "",
		},
		{
			desc: "existing forwarding rule",
			svc:  &apiv1.Service{},
			fwd: &composite.ForwardingRule{
				IPAddress: notReservedIPv4,
			},
			want: notReservedIPv4,
			name: "",
		},
		{
			desc: "existing forwarding rule with spec",
			svc: &apiv1.Service{
				Spec: apiv1.ServiceSpec{
					LoadBalancerIP: reservedIPv4,
				},
			},
			fwd: &composite.ForwardingRule{
				IPAddress: notReservedIPv4,
			},
			want: reservedIPv4,
			name: "",
		},
		{
			desc: "existing forwarding rule with annotation",
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"networking.gke.io/load-balancer-ip-addresses": reservedIPv4Name,
					},
				},
			},
			fwd: &composite.ForwardingRule{
				IPAddress: notReservedIPv4,
			},
			want: reservedIPv4,
			name: reservedIPv4Name,
		},
		{
			desc: "spec and annotation exist",
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"networking.gke.io/load-balancer-ip-addresses": reservedIPv4Name,
					},
				},
				Spec: apiv1.ServiceSpec{
					LoadBalancerIP: notReservedIPv4,
				},
			},
			fwd: &composite.ForwardingRule{
				IPAddress: notReservedIPv4,
			},
			// prefer annotation
			want: reservedIPv4,
			name: reservedIPv4Name,
		},
		{
			desc: "subnet change",
			svc:  &apiv1.Service{},
			fwd: &composite.ForwardingRule{
				Subnetwork: "other-subnetwork",
				IPAddress:  notReservedIPv4,
			},
			want: "",
			name: "",
		},
		{
			desc: "not reserved address",
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"networking.gke.io/load-balancer-ip-addresses": "not-existing",
					},
				},
			},
			want: "",
			name: "",
		},
		{
			desc: "ipv6 address in spec.loadBalancerIP returns user error",
			svc: &apiv1.Service{
				Spec: apiv1.ServiceSpec{
					LoadBalancerIP: reservedIPv6,
				},
			},
			wantUserErr: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			cloud, recorder := arrangeIPv4(t)

			got, name, err := address.IPv4ToUse(cloud, recorder, tc.svc, tc.fwd, "")
			if tc.wantUserErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !resources.IsUserError(err) {
					t.Errorf("resources.IsUserError(%v) = false, want true", err)
				}
				if !l4utils.IsIPConfigurationError(err) {
					t.Errorf("l4utils.IsIPConfigurationError(%v) = false, want true", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}

			if got != tc.want {
				t.Errorf("address.IPv4ToUse(_) = %q, want %q", got, tc.want)
			}
			if name != tc.name {
				t.Errorf("address.IPv4ToUse(_) = %q, want %q", name, tc.name)
			}
		})
	}
}

func arrangeIPv4(t *testing.T) (*gce.Cloud, record.EventRecorder) {
	t.Helper()

	vals := gce.DefaultTestClusterValues()
	fakeCloud := gce.NewFakeGCECloud(vals)
	addr := &compute.Address{
		Name:    reservedIPv4Name,
		Address: reservedIPv4,
	}
	err := fakeCloud.ReserveRegionAddress(addr, vals.Region)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &record.FakeRecorder{}
	return fakeCloud, recorder
}

func TestIPv6ToUse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		desc        string
		svc         *apiv1.Service
		fwd         *composite.ForwardingRule
		want        string
		name        string
		wantUserErr bool
	}{
		{
			desc: "nothing exists",
			svc:  &apiv1.Service{},
			want: "",
			name: "",
		},
		{
			desc: "existing forwarding rule",
			svc:  &apiv1.Service{},
			fwd: &composite.ForwardingRule{
				IPAddress: notReservedIPv6,
			},
			want: notReservedIPv6,
			name: "",
		},
		{
			desc: "existing forwarding rule with annotation",
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"networking.gke.io/load-balancer-ip-addresses": reservedIPv6Name,
					},
				},
			},
			fwd: &composite.ForwardingRule{
				IPAddress: notReservedIPv6,
			},
			want: reservedIPv6,
			name: reservedIPv6Name,
		},
		{
			desc: "subnet change",
			svc:  &apiv1.Service{},
			fwd: &composite.ForwardingRule{
				Subnetwork: "other-subnetwork",
				IPAddress:  notReservedIPv6,
			},
			want: "",
			name: "",
		},
		{
			desc: "subnet change on forwarding rule with ip collection keeps the ip",
			svc:  &apiv1.Service{},
			fwd: &composite.ForwardingRule{
				Subnetwork:   "other-subnetwork",
				IpCollection: "projects/test/regions/us-central1/publicDelegatedPrefixes/my-collection",
				IPAddress:    notReservedIPv6 + "/96",
			},
			want: notReservedIPv6,
			name: "",
		},
		{
			desc: "not reserved address",
			svc: &apiv1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"networking.gke.io/load-balancer-ip-addresses": "not-existing",
					},
				},
			},
			want: "",
			name: "",
		},
		{
			desc: "ipv6 spec.loadBalancerIP on single-stack IPv6 service returns user error",
			svc: &apiv1.Service{
				Spec: apiv1.ServiceSpec{
					IPFamilies:     []apiv1.IPFamily{apiv1.IPv6Protocol},
					LoadBalancerIP: reservedIPv6,
				},
			},
			wantUserErr: true,
		},
		{
			desc: "ipv4 spec.loadBalancerIP on single-stack IPv6 service returns user error",
			svc: &apiv1.Service{
				Spec: apiv1.ServiceSpec{
					IPFamilies:     []apiv1.IPFamily{apiv1.IPv6Protocol},
					LoadBalancerIP: reservedIPv4,
				},
			},
			wantUserErr: true,
		},
		{
			desc: "ipv6 spec.loadBalancerIP on dual-stack service returns user error",
			svc: &apiv1.Service{
				Spec: apiv1.ServiceSpec{
					IPFamilies:     []apiv1.IPFamily{apiv1.IPv4Protocol, apiv1.IPv6Protocol},
					LoadBalancerIP: reservedIPv6,
				},
			},
			wantUserErr: true,
		},
		{
			desc: "ipv4 spec.loadBalancerIP on dual-stack service is ignored for IPv6 forwarding rule",
			svc: &apiv1.Service{
				Spec: apiv1.ServiceSpec{
					IPFamilies:     []apiv1.IPFamily{apiv1.IPv4Protocol, apiv1.IPv6Protocol},
					LoadBalancerIP: reservedIPv4,
				},
			},
			want: "",
			name: "",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			cloud := arrangeIPv6(t)

			got, name, err := address.IPv6ToUse(cloud, tc.svc, tc.fwd, "", klog.TODO())
			if tc.wantUserErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !resources.IsUserError(err) {
					t.Errorf("resources.IsUserError(%v) = false, want true", err)
				}
				if !l4utils.IsIPConfigurationError(err) {
					t.Errorf("l4utils.IsIPConfigurationError(%v) = false, want true", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}

			if got != tc.want {
				t.Errorf("address.IPv6ToUse(_) = %q, want %q", got, tc.want)
			}
			if name != tc.name {
				t.Errorf("address.IPv6ToUse(_) = %q, want %q", name, tc.name)
			}
		})
	}
}

func arrangeIPv6(t *testing.T) *gce.Cloud {
	vals := gce.DefaultTestClusterValues()
	fakeCloud := gce.NewFakeGCECloud(vals)
	addr := &compute.Address{
		Name:      reservedIPv6Name,
		Address:   reservedIPv6,
		IpVersion: "IPV6",
	}
	err := fakeCloud.ReserveRegionAddress(addr, vals.Region)
	if err != nil {
		t.Fatal(err)
	}
	return fakeCloud
}
