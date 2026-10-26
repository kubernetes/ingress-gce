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

// TestL4NetLBEnsureIPv4ForwardingRuleAddressReservation verifies that the IP is reserved
// only when the forwarding rule is going to be created, updated or deleted.
func TestL4NetLBEnsureIPv4ForwardingRuleAddressReservation(t *testing.T) {
	t.Parallel()
	const (
		serviceNamespace = "testNs"
		serviceName      = "testSvc"
		existingIP       = "1.2.3.4"
		bsLink           = "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs1"
	)
	newService := func(ports []int32, loadBalancerIP string, annotations map[string]string) *corev1.Service {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: serviceNamespace, UID: types.UID("1"), Annotations: annotations},
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
	existingRule := func(svc *corev1.Service) *composite.ForwardingRule {
		return &composite.ForwardingRule{
			Name:                utils.LegacyForwardingRuleName(svc),
			IPAddress:           existingIP,
			PortRange:           "8080-8080",
			IPProtocol:          "TCP",
			LoadBalancingScheme: string(cloud.SchemeExternal),
			NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
			Version:             meta.VersionGA,
			BackendService:      bsLink,
			Description:         l4ServiceDescription(t, serviceName, serviceNamespace, existingIP, utils.XLB),
		}
	}

	testCases := []struct {
		desc string
		svc  *corev1.Service
		// userAddress is reserved in the cloud before the sync.
		userAddress *compute.Address
		// leftoverMixedRule is created with the mixed protocol TCP name before the sync.
		leftoverMixedRule bool
		bsLink            string
		wantReserved      bool
		wantUpdate        l4utils.ResourceSyncStatus
		wantManaged       address.IPAddressType
		// wantErr is the check for the error returned by the sync, nil when no error is expected.
		wantErr func(error) bool
		// wantRuleDeleted is set when the sync is expected to delete the existing rule without re-creating it.
		wantRuleDeleted bool
	}{
		{
			desc:         "unchanged rule with ephemeral IP",
			svc:          newService([]int32{8080}, "", nil),
			bsLink:       bsLink,
			wantReserved: false,
			wantUpdate:   l4utils.ResourceResync,
			wantManaged:  address.IPAddrManaged,
		},
		{
			desc:         "unchanged rule with user static IP",
			svc:          newService([]int32{8080}, existingIP, nil),
			userAddress:  &compute.Address{Name: "user-addr", Address: existingIP, AddressType: string(cloud.SchemeExternal), NetworkTier: cloud.NetworkTierPremium.ToGCEValue()},
			bsLink:       bsLink,
			wantReserved: false,
			wantUpdate:   l4utils.ResourceResync,
			wantManaged:  address.IPAddrUnmanaged,
		},
		{
			desc:         "unchanged rule with network tier annotation matching the rule",
			svc:          newService([]int32{8080}, "", map[string]string{annotations.NetworkTierAnnotationKey: string(cloud.NetworkTierPremium)}),
			bsLink:       bsLink,
			wantReserved: false,
			wantUpdate:   l4utils.ResourceResync,
			wantManaged:  address.IPAddrManaged,
		},
		{
			desc:              "unchanged rule with leftover mixed protocol rule",
			svc:               newService([]int32{8080}, "", nil),
			leftoverMixedRule: true,
			bsLink:            bsLink,
			wantReserved:      true,
			wantUpdate:        l4utils.ResourceResync,
			wantManaged:       address.IPAddrManaged,
		},
		{
			desc:         "changed ports",
			svc:          newService([]int32{8080, 8081}, "", nil),
			bsLink:       bsLink,
			wantReserved: true,
			wantUpdate:   l4utils.ResourceUpdate,
			wantManaged:  address.IPAddrManaged,
		},
		{
			desc:         "changed backend service",
			svc:          newService([]int32{8080}, "", nil),
			bsLink:       "http://www.googleapis.com/projects/test/regions/us-central1/backendServices/bs2",
			wantReserved: true,
			wantUpdate:   l4utils.ResourceUpdate,
			wantManaged:  address.IPAddrManaged,
		},
		{
			// The rule with the wrong tier is torn down while the IP is reserved, and the sync
			// reports the mismatch; the rule is re-created with the new tier by the next sync.
			desc:            "changed network tier",
			svc:             newService([]int32{8080}, "", map[string]string{annotations.NetworkTierAnnotationKey: string(cloud.NetworkTierStandard)}),
			bsLink:          bsLink,
			wantReserved:    true,
			wantUpdate:      l4utils.ResourceUpdate,
			wantManaged:     address.IPAddrUndefined,
			wantErr:         l4utils.IsNetworkTierError,
			wantRuleDeleted: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fakeGCE := getFakeGCECloud(gce.DefaultTestClusterValues())
			l4namer := namer.NewL4Namer(kubeSystemUID, nil)
			l4 := NewL4NetLB(&L4NetLBParams{
				Cloud:    fakeGCE,
				Service:  tc.svc,
				Recorder: &record.FakeRecorder{},
				Namer:    l4namer,
			}, klog.TODO())
			existing := existingRule(tc.svc)
			if err := l4.forwardingRules.Create(existing); err != nil {
				t.Fatalf("forwardingRules.Create(%v) returned error %v, want nil", existing, err)
			}
			if tc.leftoverMixedRule {
				mixedRule := existingRule(tc.svc)
				mixedRule.Name = l4namer.L4ForwardingRule(serviceNamespace, serviceName, "tcp")
				if err := l4.forwardingRules.Create(mixedRule); err != nil {
					t.Fatalf("forwardingRules.Create(%v) returned error %v, want nil", mixedRule, err)
				}
			}
			if tc.userAddress != nil {
				if err := fakeGCE.ReserveRegionAddress(tc.userAddress, fakeGCE.Region()); err != nil {
					t.Fatalf("ReserveRegionAddress(%v) returned error %v, want nil", tc.userAddress, err)
				}
			}
			am := trackAddressMutations(fakeGCE)

			fr, managed, updated, err := l4.ensureIPv4ForwardingRule(tc.bsLink)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("ensureIPv4ForwardingRule() returned error %v, want nil", err)
			}
			if tc.wantErr != nil && !tc.wantErr(err) {
				t.Fatalf("ensureIPv4ForwardingRule() returned error %v, want an error accepted by wantErr", err)
			}

			inserts, _ := am.counts()
			if reserved := inserts > 0; reserved != tc.wantReserved {
				t.Errorf("ensureIPv4ForwardingRule() reserved the address = %v, want %v (%d address inserts)", reserved, tc.wantReserved, inserts)
			}
			if updated != tc.wantUpdate {
				t.Errorf("ensureIPv4ForwardingRule() returned sync status %v, want %v", updated, tc.wantUpdate)
			}
			if managed != tc.wantManaged {
				t.Errorf("ensureIPv4ForwardingRule() returned IP management type %v, want %v", managed, tc.wantManaged)
			}
			if err == nil && fr.IPAddress != existingIP {
				t.Errorf("ensureIPv4ForwardingRule() returned rule with IP %q, want %q", fr.IPAddress, existingIP)
			}
			if tc.leftoverMixedRule {
				mixedRule, err := l4.forwardingRules.Get(l4namer.L4ForwardingRule(serviceNamespace, serviceName, "tcp"))
				if err != nil || mixedRule != nil {
					t.Errorf("leftover mixed protocol forwarding rule = %v, err = %v, want it deleted", mixedRule, err)
				}
			}
			if tc.wantRuleDeleted {
				rule, err := l4.forwardingRules.Get(existing.Name)
				if err != nil || rule != nil {
					t.Errorf("forwarding rule %q = %v, err = %v, want it deleted", existing.Name, rule, err)
				}
			}
			// The address is only reserved for the duration of the sync.
			if addr, err := fakeGCE.GetRegionAddress(existing.Name, fakeGCE.Region()); utils.IgnoreHTTPNotFound(err) != nil || addr != nil {
				t.Errorf("controller managed address %q = %v, err = %v, want it released after the sync", existing.Name, addr, err)
			}
		})
	}
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
			_, _, err := l4.ensureIPv4ForwardingRule("link", gce.ILBOptions{}, nil, "subnetworkX", "1.1.1.1", true)

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

	forwardingRule, syncStatus, err := l4.ensureIPv4ForwardingRule(bsLink, gce.ILBOptions{}, nil, subnetworkURL, ipToUse, true)
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
			fr, updated, err := l4.ensureIPv4ForwardingRule(bsLink, gce.ILBOptions{}, tc.existingRule, subnetworkURL, "", true)

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
