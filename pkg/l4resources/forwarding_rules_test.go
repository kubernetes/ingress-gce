package l4resources

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
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/forwardingrules"
	"k8s.io/ingress-gce/pkg/l4annotations"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

func TestL4CreateExternalForwardingRuleAddressAlreadyInUse(t *testing.T) {
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
	insertError := &googleapi.Error{Code: http.StatusBadRequest, Message: "Invalid value for field 'resource.IPAddress': '1.1.1.1'. Specified IP address is in-use and would result in a conflict., invalid"}
	fakeGCE.Compute().(*cloud.MockGCE).MockForwardingRules.InsertHook = test.InsertForwardingRuleErrorHook(insertError)
	_, _, _, err := l4.ensureIPv4ForwardingRule("link")

	require.Error(t, err)
	assert.True(t, utils.IsIPConfigurationError(err))
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
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: serviceNamespace, UID: types.UID("1"), Annotations: map[string]string{l4annotations.NetworkTierAnnotationKey: string(cloud.NetworkTierStandard)}},
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
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: serviceNamespace, UID: types.UID("1"), Annotations: map[string]string{l4annotations.StaticL4AddressesAnnotationKey: "my-addr"}},
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
		wantUpdate   utils.ResourceSyncStatus
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
			wantUpdate: utils.ResourceResync,
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
			wantUpdate: utils.ResourceUpdate,
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

func TestL4EnsureIPv4ForwardingRuleAddressAlreadyInUse(t *testing.T) {
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
	insertError := &googleapi.Error{Code: http.StatusConflict, Message: "IP_IN_USE_BY_ANOTHER_RESOURCE - IP '10.107.116.14' is already being used by another resource."}
	fakeGCE.Compute().(*cloud.MockGCE).MockForwardingRules.InsertHook = test.InsertForwardingRuleErrorHook(insertError)
	_, _, err := l4.ensureIPv4ForwardingRule("link", gce.ILBOptions{}, nil, "subnetworkX", "1.1.1.1")

	require.Error(t, err)
	assert.True(t, utils.IsIPConfigurationError(err))
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

	forwardingRule, syncStatus, err := l4.ensureIPv4ForwardingRule(bsLink, gce.ILBOptions{}, nil, subnetworkURL, ipToUse)
	require.NoError(t, err)
	require.Equal(t, utils.ResourceUpdate, syncStatus)

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
		wantUpdate   utils.ResourceSyncStatus
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
			wantUpdate: utils.ResourceResync,
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
			wantUpdate: utils.ResourceUpdate,
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
			fr, updated, err := l4.ensureIPv4ForwardingRule(bsLink, gce.ILBOptions{}, tc.existingRule, subnetworkURL, "")

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
