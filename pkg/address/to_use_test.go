package address_test

import (
	"testing"

	"google.golang.org/api/compute/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/address"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/klog/v2"
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
		desc string
		svc  *apiv1.Service
		fwd  *composite.ForwardingRule
		want string
	}{
		{
			desc: "nothing exists",
			svc:  &apiv1.Service{},
			want: "",
		},
		{
			desc: "existing forwarding rule",
			svc:  &apiv1.Service{},
			fwd: &composite.ForwardingRule{
				IPAddress: notReservedIPv4,
			},
			want: notReservedIPv4,
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
		},
		{
			desc: "subnet change",
			svc:  &apiv1.Service{},
			fwd: &composite.ForwardingRule{
				Subnetwork: "other-subnetwork",
				IPAddress:  notReservedIPv4,
			},
			want: "",
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
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			cloud, recorder := arrangeIPv4(t)

			got, err := address.IPv4ToUse(cloud, recorder, tc.svc, tc.fwd, "")
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}

			if got != tc.want {
				t.Errorf("address.IPv4ToUse(_) = %q, want %q", got, tc.want)
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
		desc string
		svc  *apiv1.Service
		fwd  *composite.ForwardingRule
		want string
	}{
		{
			desc: "nothing exists",
			svc:  &apiv1.Service{},
			want: "",
		},
		{
			desc: "existing forwarding rule",
			svc:  &apiv1.Service{},
			fwd: &composite.ForwardingRule{
				IPAddress: notReservedIPv6,
			},
			want: notReservedIPv6,
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
		},
		{
			desc: "subnet change",
			svc:  &apiv1.Service{},
			fwd: &composite.ForwardingRule{
				Subnetwork: "other-subnetwork",
				IPAddress:  notReservedIPv6,
			},
			want: "",
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
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			cloud := arrangeIPv6(t)

			got, err := address.IPv6ToUse(cloud, tc.svc, tc.fwd, "", klog.TODO())
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}

			if got != tc.want {
				t.Errorf("address.IPv4ToUse(_) = %q, want %q", got, tc.want)
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
