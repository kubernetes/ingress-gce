package resources

import (
	"net/http"
	"strings"
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
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4/address"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	"k8s.io/ingress-gce/pkg/l4/forwardingrules"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"

	"k8s.io/cloud-provider-gcp/providers/gce"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
)

func TestL4CreateExternalForwardingRuleUserErrors(t *testing.T) {
	testCases := []struct {
		desc                  string
		insertError           *googleapi.Error
		expectedUserErrorFunc func(error) bool
	}{
		{
			desc:                  "Address already in-use",
			insertError:           &googleapi.Error{Code: http.StatusBadRequest, Message: "Invalid value for field 'resource.IPAddress': '1.1.1.1'. Specified IP address is in-use and would result in a conflict., invalid"},
			expectedUserErrorFunc: l4utils.IsIPConfigurationError,
		},
		{
			desc:                  "Conflicting port range",
			insertError:           &googleapi.Error{Code: http.StatusBadRequest, Message: "Invalid resource usage: 'Forwarding rule's port range is conflicting with forwarding rule: abcd'."},
			expectedUserErrorFunc: l4utils.IsConflictingPortsConfigurationError,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			targetIP := "1.1.1.1"
			l4 := NewL4NetLB(&L4NetLBParams{
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
				Cloud:    fakeGCE,
				Recorder: &record.FakeRecorder{},
				Namer:    namer.NewL4Namer(kubeSystemUID, nil),
			}, klog.TODO())

			addr := &compute.Address{Name: "my-important-address", Address: targetIP, AddressType: string(cloud.SchemeExternal)}
			fakeGCE.ReserveRegionAddress(addr, fakeGCE.Region())
			fakeGCE.Compute().(*cloud.MockGCE).MockForwardingRules.InsertHook = test.InsertForwardingRuleErrorHook(tc.insertError)
			_, _, _, err := l4.ensureIPv4ForwardingRule("link")

			require.Error(t, err)
			assert.True(t, tc.expectedUserErrorFunc(err), "actual", err)
		})
	}
}

func TestL4CreateExternalForwardingRule(t *testing.T) {
	serviceNamespace := "testNs"
	serviceName := "testSvc"

	bsLink := "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1"

	testCases := []struct {
		desc         string
		svc          *corev1.Service
		namedAddress *compute.Address
		wantRule     *composite.ForwardingRule
	}{
		{
			desc: "create",
			svc: &corev1.Service{
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
			},
			wantRule: &composite.ForwardingRule{
				PortRange:           "8080-8080",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeExternal),
				NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
				Version:             meta.VersionGA,
				BackendService:      bsLink,
				Description:         l4ServiceDescription(t, serviceName, serviceNamespace, "", utils.XLB),
			},
		},
		{
			desc: "create with port range and network tier",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: serviceNamespace, UID: types.UID("1"), Annotations: map[string]string{annotations.NetworkTierAnnotationKey: string(cloud.NetworkTierStandard)}},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port:     8080,
							Protocol: corev1.ProtocolUDP,
						},
						{
							Port:     8085,
							Protocol: corev1.ProtocolUDP,
						},
					},
					Type: "LoadBalancer",
				},
			},
			wantRule: &composite.ForwardingRule{
				PortRange:           "8080-8085",
				IPProtocol:          "UDP",
				LoadBalancingScheme: string(cloud.SchemeExternal),
				NetworkTier:         cloud.NetworkTierStandard.ToGCEValue(),
				Version:             meta.VersionGA,
				BackendService:      bsLink,
				Description:         l4ServiceDescription(t, serviceName, serviceNamespace, "", utils.XLB),
			},
		},
		{
			desc: "create with assigned IP",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: serviceNamespace, UID: types.UID("1")},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port:     8080,
							Protocol: corev1.ProtocolTCP,
						},
					},
					Type:           "LoadBalancer",
					LoadBalancerIP: "1.1.1.1",
				},
			},
			wantRule: &composite.ForwardingRule{
				IPAddress:           "1.1.1.1",
				PortRange:           "8080-8080",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeExternal),
				NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
				Version:             meta.VersionGA,
				BackendService:      bsLink,
				Description:         l4ServiceDescription(t, serviceName, serviceNamespace, "1.1.1.1", utils.XLB),
			},
		},
		{
			desc:         "create with named address",
			namedAddress: &compute.Address{Name: "my-addr", Address: "1.2.3.4", AddressType: string(cloud.SchemeExternal), NetworkTier: "PREMIUM"},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: serviceNamespace, UID: types.UID("1"), Annotations: map[string]string{annotations.StaticL4AddressesAnnotationKey: "my-addr"}},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port:     8080,
							Protocol: corev1.ProtocolTCP,
						},
					},
					Type: "LoadBalancer",
				},
			},
			wantRule: &composite.ForwardingRule{
				IPAddress:           "1.2.3.4",
				PortRange:           "8080-8080",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeExternal),
				NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
				Version:             meta.VersionGA,
				BackendService:      bsLink,
				Description:         l4ServiceDescription(t, serviceName, serviceNamespace, "1.2.3.4", utils.XLB),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			l4 := NewL4NetLB(&L4NetLBParams{
				Cloud:    fakeGCE,
				Service:  tc.svc,
				Recorder: &record.FakeRecorder{},
				Namer:    namer.NewL4Namer(kubeSystemUID, nil),
			}, klog.TODO())
			tc.wantRule.Name = utils.LegacyForwardingRuleName(tc.svc)
			if tc.namedAddress != nil {
				fakeGCE.ReserveRegionAddress(tc.namedAddress, fakeGCE.Region())
			}
			fr, _, updated, err := l4.ensureIPv4ForwardingRule(bsLink)
			if err != nil {
				t.Errorf("ensureIPv4ForwardingRule() err=%v", err)
			}
			if !updated {
				t.Errorf("ensureIPv4ForwardingRule() was supposed to return updated but did not")
			}

			if diff := cmp.Diff(tc.wantRule, fr, cmpopts.IgnoreFields(composite.ForwardingRule{}, "SelfLink", "Region", "Scope")); diff != "" {
				t.Errorf("ensureIPv4ForwardingRule() diff -want +got\n%v\n", diff)
			}
		})
	}
}

func TestL4CreateExternalForwardingRuleUpdate(t *testing.T) {
	serviceNamespace := "testNs"
	serviceName := "testSvc"

	bsLink := "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1"

	testCases := []struct {
		desc         string
		svc          *corev1.Service
		namedAddress *compute.Address
		existingRule *composite.ForwardingRule
		wantRule     *composite.ForwardingRule
		wantUpdate   l4utils.ResourceSyncStatus
		wantErrMsg   string
	}{
		{
			desc: "no update",
			svc: &corev1.Service{
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
			},
			existingRule: &composite.ForwardingRule{
				PortRange:           "8080-8080",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeExternal),
				NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
				Version:             meta.VersionGA,
				BackendService:      bsLink,
				Description:         l4ServiceDescription(t, serviceName, serviceNamespace, "", utils.XLB),
			},
			wantRule: &composite.ForwardingRule{
				PortRange:           "8080-8080",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeExternal),
				NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
				Version:             meta.VersionGA,
				BackendService:      bsLink,
				Description:         l4ServiceDescription(t, serviceName, serviceNamespace, "", utils.XLB),
			},
			wantUpdate: l4utils.ResourceResync,
		},
		{
			desc: "update ports",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: serviceNamespace, UID: types.UID("1")},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port:     8080,
							Protocol: corev1.ProtocolTCP,
						},
						{
							Port:     8082,
							Protocol: corev1.ProtocolTCP,
						},
					},
					Type: "LoadBalancer",
				},
			},
			existingRule: &composite.ForwardingRule{
				PortRange:           "8080-8080",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeExternal),
				NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
				Version:             meta.VersionGA,
				BackendService:      bsLink,
				Description:         l4ServiceDescription(t, serviceName, serviceNamespace, "", utils.XLB),
			},
			wantRule: &composite.ForwardingRule{
				PortRange:           "8080-8082",
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeExternal),
				NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
				Version:             meta.VersionGA,
				BackendService:      bsLink,
				Description:         l4ServiceDescription(t, serviceName, serviceNamespace, "", utils.XLB),
			},
			wantUpdate: l4utils.ResourceUpdate,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			l4 := NewL4NetLB(&L4NetLBParams{
				Cloud:    fakeGCE,
				Service:  tc.svc,
				Recorder: &record.FakeRecorder{},
				Namer:    namer.NewL4Namer(kubeSystemUID, nil),
			}, klog.TODO())
			tc.wantRule.Name = utils.LegacyForwardingRuleName(tc.svc)
			tc.existingRule.Name = utils.LegacyForwardingRuleName(tc.svc)
			if tc.existingRule != nil {
				l4.forwardingRules.Create(tc.existingRule)
			}
			if tc.namedAddress != nil {
				fakeGCE.ReserveRegionAddress(tc.namedAddress, fakeGCE.Region())
			}
			fr, _, updated, err := l4.ensureIPv4ForwardingRule(bsLink)

			if err != nil && tc.wantErrMsg == "" {
				t.Errorf("ensureIPv4ForwardingRule() err=%v", err)
			}
			if tc.wantErrMsg != "" {
				if err == nil {
					t.Errorf("ensureIPv4ForwardingRule() wanted error with msg=%q but got none", tc.wantErrMsg)
				} else if !strings.Contains(err.Error(), tc.wantErrMsg) {
					t.Errorf("ensureIPv4ForwardingRule() wanted error with msg=%q but got err=%v", tc.wantErrMsg, err)
				}
				return
			}
			if updated != tc.wantUpdate {
				t.Errorf("ensureIPv4ForwardingRule() wanted updated=%v but got=%v", tc.wantUpdate, updated)
			}

			if diff := cmp.Diff(tc.wantRule, fr, cmpopts.IgnoreFields(composite.ForwardingRule{}, "SelfLink", "Region", "Scope")); diff != "" {
				t.Errorf("ensureIPv4ForwardingRule() diff -want +got\n%v\n", diff)
			}
		})
	}
}

// Tests of the address reservation of the single protocol NetLB IPv4 forwarding rule. Each
// one starts from an existing rule that forwards port 8080 of reservationTestIP to
// reservationTestBSLink.
const (
	reservationTestNamespace = "testNs"
	reservationTestName      = "testSvc"
	reservationTestIP        = "1.2.3.4"
	reservationTestBSLink    = "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1"
)

// newReservationTestService returns a NetLB Service with one TCP port per port number.
func newReservationTestService(ports []int32, loadBalancerIP string, annotations map[string]string) *corev1.Service {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: reservationTestName, Namespace: reservationTestNamespace, UID: types.UID("1"), Annotations: annotations},
		Spec: corev1.ServiceSpec{
			Type:           "LoadBalancer",
			LoadBalancerIP: loadBalancerIP,
		},
	}
	for _, port := range ports {
		svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{Port: port, Protocol: corev1.ProtocolTCP})
	}
	return svc
}

// reservationTestRule returns the single protocol forwarding rule of svc as a previous
// sync left it: port 8080 of reservationTestIP, forwarded to reservationTestBSLink.
func reservationTestRule(t *testing.T, svc *corev1.Service) *composite.ForwardingRule {
	return &composite.ForwardingRule{
		Name:                utils.LegacyForwardingRuleName(svc),
		IPAddress:           reservationTestIP,
		PortRange:           "8080-8080",
		IPProtocol:          "TCP",
		LoadBalancingScheme: string(cloud.SchemeExternal),
		NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
		Version:             meta.VersionGA,
		BackendService:      reservationTestBSLink,
		Description:         l4ServiceDescription(t, reservationTestName, reservationTestNamespace, reservationTestIP, utils.XLB),
	}
}

// setupReservationTest returns the NetLB handler of svc after creating its existing
// forwarding rule, and starts counting the address mutations of its fake cloud.
func setupReservationTest(t *testing.T, svc *corev1.Service) (*L4NetLB, *composite.ForwardingRule, *addressMutations) {
	t.Helper()
	l4 := NewL4NetLB(&L4NetLBParams{
		Cloud:    getFakeGCECloud(gce.DefaultTestClusterValues()),
		Service:  svc,
		Recorder: &record.FakeRecorder{},
		Namer:    namer.NewL4Namer(kubeSystemUID, nil),
	}, klog.TODO())
	existing := reservationTestRule(t, svc)
	if err := l4.forwardingRules.Create(existing); err != nil {
		t.Fatalf("forwardingRules.Create(%v) returned error %v, want nil", existing, err)
	}
	return l4, existing, trackAddressMutations(l4.cloud)
}

// assertControllerAddressReleased fails the test if the address the controller reserves
// for rule still exists: it must only be reserved for the duration of the sync.
func assertControllerAddressReleased(t *testing.T, l4 *L4NetLB, rule *composite.ForwardingRule) {
	t.Helper()
	if addr, err := l4.cloud.GetRegionAddress(rule.Name, l4.cloud.Region()); utils.IgnoreHTTPNotFound(err) != nil || addr != nil {
		t.Errorf("controller managed address %q = %v, err = %v, want it released after the sync", rule.Name, addr, err)
	}
}

// TestL4NetLBEnsureIPv4ForwardingRuleSkipsReservationOfUnchangedRule verifies that the
// address is not reserved when the existing forwarding rule already matches the Service.
func TestL4NetLBEnsureIPv4ForwardingRuleSkipsReservationOfUnchangedRule(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		desc string
		svc  *corev1.Service
		// userAddress is reserved in the cloud before the sync.
		userAddress *compute.Address
		wantManaged address.IPAddressType
	}{
		{
			desc:        "ephemeral IP",
			svc:         newReservationTestService([]int32{8080}, "", nil),
			wantManaged: address.IPAddrManaged,
		},
		{
			desc:        "user static IP",
			svc:         newReservationTestService([]int32{8080}, reservationTestIP, nil),
			userAddress: &compute.Address{Name: "user-addr", Address: reservationTestIP, AddressType: string(cloud.SchemeExternal), NetworkTier: cloud.NetworkTierPremium.ToGCEValue()},
			wantManaged: address.IPAddrUnmanaged,
		},
		{
			desc:        "network tier annotation matching the rule",
			svc:         newReservationTestService([]int32{8080}, "", map[string]string{annotations.NetworkTierAnnotationKey: string(cloud.NetworkTierPremium)}),
			wantManaged: address.IPAddrManaged,
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			l4, _, am := setupReservationTest(t, tc.svc)
			if tc.userAddress != nil {
				if err := l4.cloud.ReserveRegionAddress(tc.userAddress, l4.cloud.Region()); err != nil {
					t.Fatalf("ReserveRegionAddress(%v) returned error %v, want nil", tc.userAddress, err)
				}
				am.reset()
			}

			fr, managed, updated, err := l4.ensureIPv4ForwardingRule(reservationTestBSLink)
			if err != nil {
				t.Fatalf("ensureIPv4ForwardingRule() returned error %v, want nil", err)
			}
			if inserts, deletes := am.counts(); inserts != 0 || deletes != 0 {
				t.Errorf("ensureIPv4ForwardingRule() made %d address inserts and %d address deletes, want 0 and 0", inserts, deletes)
			}
			if updated != l4utils.ResourceResync {
				t.Errorf("ensureIPv4ForwardingRule() returned sync status %v, want %v", updated, l4utils.ResourceResync)
			}
			if managed != tc.wantManaged {
				t.Errorf("ensureIPv4ForwardingRule() returned IP management type %v, want %v", managed, tc.wantManaged)
			}
			if fr.IPAddress != reservationTestIP {
				t.Errorf("ensureIPv4ForwardingRule() returned rule with IP %q, want %q", fr.IPAddress, reservationTestIP)
			}
		})
	}
}

// TestL4NetLBEnsureIPv4ForwardingRuleReservesAddressToUpdateRule verifies that the address
// is reserved while a changed forwarding rule is updated, and released afterwards.
func TestL4NetLBEnsureIPv4ForwardingRuleReservesAddressToUpdateRule(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		desc   string
		svc    *corev1.Service
		bsLink string
	}{
		{
			desc:   "changed ports",
			svc:    newReservationTestService([]int32{8080, 8081}, "", nil),
			bsLink: reservationTestBSLink,
		},
		{
			desc:   "changed backend service",
			svc:    newReservationTestService([]int32{8080}, "", nil),
			bsLink: "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs2",
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			l4, existing, am := setupReservationTest(t, tc.svc)

			fr, managed, updated, err := l4.ensureIPv4ForwardingRule(tc.bsLink)
			if err != nil {
				t.Fatalf("ensureIPv4ForwardingRule() returned error %v, want nil", err)
			}
			if inserts, _ := am.counts(); inserts == 0 {
				t.Errorf("ensureIPv4ForwardingRule() reserved %d addresses, want at least 1", inserts)
			}
			if updated != l4utils.ResourceUpdate {
				t.Errorf("ensureIPv4ForwardingRule() returned sync status %v, want %v", updated, l4utils.ResourceUpdate)
			}
			if managed != address.IPAddrManaged {
				t.Errorf("ensureIPv4ForwardingRule() returned IP management type %v, want %v", managed, address.IPAddrManaged)
			}
			if fr.IPAddress != reservationTestIP {
				t.Errorf("ensureIPv4ForwardingRule() returned rule with IP %q, want %q", fr.IPAddress, reservationTestIP)
			}
			assertControllerAddressReleased(t, l4, existing)
		})
	}
}

// TestL4NetLBEnsureIPv4ForwardingRuleReservesAddressToDeleteLeftoverMixedRule verifies that
// the address is reserved while a leftover mixed protocol rule is deleted, even though the
// single protocol rule is unchanged.
func TestL4NetLBEnsureIPv4ForwardingRuleReservesAddressToDeleteLeftoverMixedRule(t *testing.T) {
	t.Parallel()
	svc := newReservationTestService([]int32{8080}, "", nil)
	l4, existing, am := setupReservationTest(t, svc)
	leftover := reservationTestRule(t, svc)
	leftover.Name = l4.namer.L4ForwardingRule(reservationTestNamespace, reservationTestName, "tcp")
	if err := l4.forwardingRules.Create(leftover); err != nil {
		t.Fatalf("forwardingRules.Create(%v) returned error %v, want nil", leftover, err)
	}

	fr, managed, updated, err := l4.ensureIPv4ForwardingRule(reservationTestBSLink)
	if err != nil {
		t.Fatalf("ensureIPv4ForwardingRule() returned error %v, want nil", err)
	}
	if inserts, _ := am.counts(); inserts == 0 {
		t.Errorf("ensureIPv4ForwardingRule() reserved %d addresses, want at least 1", inserts)
	}
	if got, err := l4.forwardingRules.Get(leftover.Name); err != nil || got != nil {
		t.Errorf("forwardingRules.Get(%q) = %v, %v, want the leftover mixed protocol rule deleted", leftover.Name, got, err)
	}
	if updated != l4utils.ResourceResync {
		t.Errorf("ensureIPv4ForwardingRule() returned sync status %v, want %v", updated, l4utils.ResourceResync)
	}
	if managed != address.IPAddrManaged {
		t.Errorf("ensureIPv4ForwardingRule() returned IP management type %v, want %v", managed, address.IPAddrManaged)
	}
	if fr.IPAddress != reservationTestIP {
		t.Errorf("ensureIPv4ForwardingRule() returned rule with IP %q, want %q", fr.IPAddress, reservationTestIP)
	}
	assertControllerAddressReleased(t, l4, existing)
}

// TestL4NetLBEnsureIPv4ForwardingRuleTearsDownRuleOnNetworkTierChange verifies that a
// network tier change is not mistaken for an unchanged rule. The rule with the wrong tier
// is torn down before the address is reserved, since an IP can't move to another tier,
// and the sync reports the mismatch; the next sync creates the rule with the new tier.
func TestL4NetLBEnsureIPv4ForwardingRuleTearsDownRuleOnNetworkTierChange(t *testing.T) {
	t.Parallel()
	svc := newReservationTestService([]int32{8080}, "", map[string]string{annotations.NetworkTierAnnotationKey: string(cloud.NetworkTierStandard)})
	l4, existing, _ := setupReservationTest(t, svc)

	_, managed, updated, err := l4.ensureIPv4ForwardingRule(reservationTestBSLink)
	if !l4utils.IsNetworkTierError(err) {
		t.Fatalf("ensureIPv4ForwardingRule() returned error %v, want a network tier error", err)
	}
	if got, err := l4.forwardingRules.Get(existing.Name); err != nil || got != nil {
		t.Errorf("forwardingRules.Get(%q) = %v, %v, want the rule deleted", existing.Name, got, err)
	}
	if updated != l4utils.ResourceUpdate {
		t.Errorf("ensureIPv4ForwardingRule() returned sync status %v, want %v", updated, l4utils.ResourceUpdate)
	}
	if managed != address.IPAddrUndefined {
		t.Errorf("ensureIPv4ForwardingRule() returned IP management type %v, want %v", managed, address.IPAddrUndefined)
	}
	assertControllerAddressReleased(t, l4, existing)
}

// TestL4EnsureIPv4ForwardingRuleRequiresReservedIPToRecreate verifies that the ILB IPv4
// forwarding rule is only deleted to be re-created while its IP is reserved.
func TestL4EnsureIPv4ForwardingRuleRequiresReservedIPToRecreate(t *testing.T) {
	t.Parallel()
	const bsLink = "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1"
	l4 := mustSetupILBTestHandler(t, test.NewL4ILBService(false, 8080), []string{"test-node-1"})
	// The existing rule forwards port 9090 while the Service wants 8080, which can't be
	// patched: the rule has to be deleted and re-created.
	existing := &composite.ForwardingRule{
		Name: l4.GetFRName(), IPAddress: "10.0.0.5", IPProtocol: "TCP", Ports: []string{"9090"},
		LoadBalancingScheme: string(cloud.SchemeInternal), BackendService: bsLink,
		NetworkTier: cloud.NetworkTierPremium.ToGCEValue(),
	}

	assertRecreateRequiresReservedIP(t, l4.forwardingRules, existing, func(ipReserved bool) error {
		ip := address.IPToUse{IP: existing.IPAddress, Reserved: ipReserved}
		_, _, err := l4.ensureIPv4ForwardingRule(bsLink, l4.getILBOptions(), existing, l4.network.SubnetworkURL, ip)
		return err
	})
}

func TestL4EnsureIPv4ForwardingRuleAddressAlreadyInUse(t *testing.T) {
	testCases := []struct {
		desc                  string
		insertError           *googleapi.Error
		expectedUserErrorFunc func(error) bool
	}{
		{
			desc:                  "Address already in-use",
			insertError:           &googleapi.Error{Code: http.StatusConflict, Message: "IP_IN_USE_BY_ANOTHER_RESOURCE - IP '10.107.116.14' is already being used by another resource."},
			expectedUserErrorFunc: l4utils.IsIPConfigurationError,
		},
		{
			desc:                  "Conflicting port range",
			insertError:           &googleapi.Error{Code: http.StatusBadRequest, Message: "Invalid resource usage: 'Forwarding rule's port range is conflicting with forwarding rule: abcd'."},
			expectedUserErrorFunc: l4utils.IsConflictingPortsConfigurationError,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			targetIP := "1.1.1.1"
			l4 := L4{
				cloud:           fakeGCE,
				forwardingRules: forwardingrules.New(fakeGCE, meta.VersionGA, meta.Regional, klog.TODO()),
				namer:           namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO())),
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
			fakeGCE.Compute().(*cloud.MockGCE).MockForwardingRules.InsertHook = test.InsertForwardingRuleErrorHook(tc.insertError)
			_, _, err := l4.ensureIPv4ForwardingRule("link", gce.ILBOptions{}, nil, "subnetworkX", address.IPToUse{IP: "1.1.1.1", Reserved: true})

			require.Error(t, err)
			assert.True(t, tc.expectedUserErrorFunc(err))
		})
	}
}

func TestL4EnsureIPv4ForwardingRule(t *testing.T) {
	subnetworkURL := "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet"
	networkURL := "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc"
	l4namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))
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
	forwardingRules := forwardingrules.New(fakeGCE, meta.VersionGA, meta.Regional, klog.TODO())

	l4 := &L4{
		cloud:           fakeGCE,
		forwardingRules: forwardingRules,
		namer:           l4namer,
		recorder:        record.NewFakeRecorder(100),
		Service:         defaultService,
		network: network.NetworkInfo{
			IsDefault:  false,
			NetworkURL: networkURL,
		},
	}
	ipToUse := "1.1.1.1"
	bsLink := "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1"

	forwardingRule, syncStatus, err := l4.ensureIPv4ForwardingRule(bsLink, gce.ILBOptions{}, nil, subnetworkURL, address.IPToUse{IP: ipToUse, Reserved: true})
	require.NoError(t, err)
	require.Equal(t, l4utils.ResourceUpdate, syncStatus)

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
		Description:         l4ServiceDescription(t, serviceName, serviceNamespace, "1.1.1.1", utils.ILB),
	}
	if diff := cmp.Diff(wantForwardingRule, forwardingRule, cmpopts.IgnoreFields(composite.ForwardingRule{}, "SelfLink", "Region", "Scope")); diff != "" {
		t.Errorf("ensureIPv4ForwardingRule() diff -want +got\n%v\n", diff)
	}
}

func l4ServiceDescription(t *testing.T, svcName, svcNamespace, ipToUse string, lbType utils.L4LBType) string {
	description, err := utils.MakeL4LBServiceDescription(utils.ServiceKeyFunc(svcNamespace, svcName), ipToUse,
		meta.VersionGA, false, lbType)
	if err != nil {
		t.Errorf("utils.MakeL4LBServiceDescription() failed, err=%v", err)
	}
	return description
}

func TestL4EnsureInternalForwardingRuleUpdate(t *testing.T) {
	serviceNamespace := "testNs"
	serviceName := "testSvc"
	l4namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))

	bsLink := "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1"
	networkURL := "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc"
	subnetworkURL := "https://www.googleapis.com/compute/v1/projects/test-poject/regions/us-central1/subnetworks/default-subnet"

	testCases := []struct {
		desc         string
		svc          *corev1.Service
		namedAddress *compute.Address
		existingRule *composite.ForwardingRule
		wantRule     *composite.ForwardingRule
		wantUpdate   l4utils.ResourceSyncStatus
		wantErrMsg   string
	}{
		{
			desc: "no update",
			svc: &corev1.Service{
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
			},
			existingRule: &composite.ForwardingRule{
				Ports:               []string{"8080"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
				Version:             meta.VersionGA,
				Network:             networkURL,
				Subnetwork:          subnetworkURL,
				BackendService:      bsLink,
				Description:         l4ServiceDescription(t, serviceName, serviceNamespace, "", utils.ILB),
			},
			wantRule: &composite.ForwardingRule{
				Ports:               []string{"8080"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
				Version:             meta.VersionGA,
				Network:             networkURL,
				Subnetwork:          subnetworkURL,
				BackendService:      bsLink,
				Description:         l4ServiceDescription(t, serviceName, serviceNamespace, "", utils.ILB),
			},
			wantUpdate: l4utils.ResourceResync,
		},
		{
			desc: "update ports",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: serviceNamespace, UID: types.UID("1")},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port:     8080,
							Protocol: corev1.ProtocolTCP,
						},
						{
							Port:     8082,
							Protocol: corev1.ProtocolTCP,
						},
					},
					Type: "LoadBalancer",
				},
			},
			existingRule: &composite.ForwardingRule{
				Ports:               []string{"8080"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
				Version:             meta.VersionGA,
				Network:             networkURL,
				Subnetwork:          subnetworkURL,
				BackendService:      bsLink,
				Description:         l4ServiceDescription(t, serviceName, serviceNamespace, "", utils.ILB),
			},
			wantRule: &composite.ForwardingRule{
				Ports:               []string{"8080", "8082"},
				IPProtocol:          "TCP",
				LoadBalancingScheme: string(cloud.SchemeInternal),
				NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
				Version:             meta.VersionGA,
				Network:             networkURL,
				Subnetwork:          subnetworkURL,
				BackendService:      bsLink,
				Description:         l4ServiceDescription(t, serviceName, serviceNamespace, "", utils.ILB),
			},
			wantUpdate: l4utils.ResourceUpdate,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			l4 := &L4{
				cloud:           fakeGCE,
				forwardingRules: forwardingrules.New(fakeGCE, meta.VersionGA, meta.Regional, klog.TODO()),
				namer:           l4namer,
				Service:         tc.svc,
				network: network.NetworkInfo{
					IsDefault:     true,
					NetworkURL:    networkURL,
					SubnetworkURL: subnetworkURL,
				},
				recorder: &record.FakeRecorder{},
			}
			tc.wantRule.Name = l4.GetFRName()
			tc.existingRule.Name = l4.GetFRName()
			if tc.namedAddress != nil {
				fakeGCE.ReserveRegionAddress(tc.namedAddress, fakeGCE.Region())
			}
			fr, updated, err := l4.ensureIPv4ForwardingRule(bsLink, gce.ILBOptions{}, tc.existingRule, subnetworkURL, address.IPToUse{Reserved: true})

			if err != nil && tc.wantErrMsg == "" {
				t.Errorf("ensureIPv4ForwardingRule() err=%v", err)
			}
			if tc.wantErrMsg != "" {
				if err == nil {
					t.Errorf("ensureIPv4ForwardingRule() wanted error with msg=%q but got none", tc.wantErrMsg)
				} else if !strings.Contains(err.Error(), tc.wantErrMsg) {
					t.Errorf("ensureIPv4ForwardingRule() wanted error with msg=%q but got err=%v", tc.wantErrMsg, err)
				}
				return
			}
			if updated != tc.wantUpdate {
				t.Errorf("ensureIPv4ForwardingRule() wanted updated=%v but got=%v", tc.wantUpdate, updated)
			}

			if diff := cmp.Diff(tc.wantRule, fr, cmpopts.IgnoreFields(composite.ForwardingRule{}, "SelfLink", "Region", "Scope")); diff != "" {
				t.Errorf("ensureIPv4ForwardingRule() diff -want +got\n%v\n", diff)
			}
		})
	}
}

// TestL4IsForwardingRuleUpToDate pins the conditions under which the ILB address
// reservation is skipped, in particular the ones that are not a plain difference
// between the existing and the wanted forwarding rule.
func TestL4IsForwardingRuleUpToDate(t *testing.T) {
	t.Parallel()
	nodeNames := []string{"test-node-1"}

	svc := test.NewL4ILBDualStackService(8080, corev1.ProtocolTCP, []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol}, corev1.ServiceExternalTrafficPolicyTypeCluster)
	l4 := mustSetupILBTestHandler(t, svc, nodeNames)
	if result := l4.EnsureInternalLoadBalancer(nodeNames, svc); result.Error != nil {
		t.Fatalf("EnsureInternalLoadBalancer() returned error %v, want nil", result.Error)
	}

	existingBS, err := l4.backendPool.Get(l4.namer.L4Backend(svc.Namespace, svc.Name), meta.VersionGA, l4.scope, l4.svcLogger)
	if err != nil || existingBS == nil {
		t.Fatalf("backendPool.Get() = %v, %v, want the backend service", existingBS, err)
	}
	existingIPv4FR, err := l4.forwardingRules.Get(l4.GetFRName())
	if err != nil || existingIPv4FR == nil {
		t.Fatalf("forwardingRules.Get(%q) = %v, %v, want the IPv4 forwarding rule", l4.GetFRName(), existingIPv4FR, err)
	}
	existingIPv6FR, err := l4.forwardingRules.Get(l4.getIPv6FRName())
	if err != nil || existingIPv6FR == nil {
		t.Fatalf("forwardingRules.Get(%q) = %v, %v, want the IPv6 forwarding rule", l4.getIPv6FRName(), existingIPv6FR, err)
	}
	options := l4.getILBOptions()
	subnetworkURL, err := l4.getServiceSubnetworkURL(options)
	if err != nil {
		t.Fatalf("getServiceSubnetworkURL() returned error %v, want nil", err)
	}
	// l4.Service is svc itself, keep the original ports to restore them after
	// the cases that mutate them.
	originalPorts := svc.Spec.Ports

	// inputs of the predicates, as computed by EnsureInternalLoadBalancer on a resync.
	type inputs struct {
		bs              *composite.BackendService
		backendProtocol string
		ipv4FR, ipv6FR  *composite.ForwardingRule
		options         gce.ILBOptions
		ipv4, ipv6      string
		ports           []corev1.ServicePort
	}
	resyncInputs := func() inputs {
		return inputs{
			bs:              existingBS,
			backendProtocol: string(corev1.ProtocolTCP),
			ipv4FR:          existingIPv4FR,
			ipv6FR:          existingIPv6FR,
			options:         options,
			ipv4:            existingIPv4FR.IPAddress,
			ipv6:            strings.Split(existingIPv6FR.IPAddress, "/")[0],
			ports:           originalPorts,
		}
	}

	testCases := []struct {
		desc     string
		mutate   func(in *inputs)
		wantIPv4 bool
		wantIPv6 bool
	}{
		{
			desc:     "unchanged service",
			mutate:   func(in *inputs) {},
			wantIPv4: true,
			wantIPv6: true,
		},
		{
			desc:     "ports change",
			mutate:   func(in *inputs) { in.ports = []corev1.ServicePort{{Port: 8081, Protocol: corev1.ProtocolTCP}} },
			wantIPv4: false,
			wantIPv6: false,
		},
		{
			desc:     "no backend service",
			mutate:   func(in *inputs) { in.bs = nil },
			wantIPv4: false,
			wantIPv6: false,
		},
		{
			desc:     "backend service protocol changes",
			mutate:   func(in *inputs) { in.backendProtocol = string(corev1.ProtocolUDP) },
			wantIPv4: false,
			wantIPv6: false,
		},
		{
			desc:     "no existing forwarding rules",
			mutate:   func(in *inputs) { in.ipv4FR, in.ipv6FR = nil, nil },
			wantIPv4: false,
			wantIPv6: false,
		},
		{
			desc:     "IPs are reset",
			mutate:   func(in *inputs) { in.ipv4, in.ipv6 = "", "" },
			wantIPv4: false,
			wantIPv6: false,
		},
		{
			desc:     "different IPv6 address requested",
			mutate:   func(in *inputs) { in.ipv6 = "2001:db8::1" },
			wantIPv4: true,
			wantIPv6: false,
		},
		{
			desc:     "global access enabled",
			mutate:   func(in *inputs) { in.options.AllowGlobalAccess = true },
			wantIPv4: false,
			wantIPv6: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			in := resyncInputs()
			tc.mutate(&in)
			l4.Service.Spec.Ports = in.ports
			defer func() { l4.Service.Spec.Ports = originalPorts }()

			if got := l4.isIPv4ForwardingRuleUpToDate(in.bs, in.backendProtocol, in.ipv4FR, in.options, subnetworkURL, in.ipv4); got != tc.wantIPv4 {
				t.Errorf("isIPv4ForwardingRuleUpToDate() = %v, want %v", got, tc.wantIPv4)
			}
			if got := l4.isIPv6ForwardingRuleUpToDate(in.bs, in.backendProtocol, in.ipv6FR, in.options, subnetworkURL, in.ipv6); got != tc.wantIPv6 {
				t.Errorf("isIPv6ForwardingRuleUpToDate() = %v, want %v", got, tc.wantIPv6)
			}
		})
	}
}
