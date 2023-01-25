package servicemetrics

import (
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/google/go-cmp/cmp"
	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/metrics"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils/common"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
)

const clusterUID = "aaaaa"

func createController() *Controller {
	kubeClient := fake.NewSimpleClientset()
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	namer := namer_util.NewNamer(clusterUID, "")
	ctxConfig := context.ControllerContextConfig{
		Namespace:             apiv1.NamespaceAll,
		ResyncPeriod:          1 * time.Minute,
		DefaultBackendSvcPort: test.DefaultBeSvcPort,
		HealthCheckPath:       "/",
	}
	stopCh := make(chan struct{})

	ctx := context.NewControllerContext(nil, kubeClient, backendConfigClient, nil, nil, nil, nil, fakeGCE, namer, "" /*kubeSystemUID*/, ctxConfig)

	controller := NewController(ctx, stopCh)
	return controller
}

func TestSync(t *testing.T) {
	var timeout int32 = 10
	internalPolicyLocal := v1.ServiceInternalTrafficPolicyLocal
	ipFamilyPolicyRequireDualStack := v1.IPFamilyPolicyRequireDualStack

	cases := []struct {
		desc            string
		service         *v1.Service
		wantL4Protocol  *metrics.ServiceL4ProtocolMetricState
		wantIPStack     *metrics.ServiceIPStackMetricState
		wantGCPFeatures *metrics.ServiceGCPFeaturesMetricState
	}{
		{
			desc: "default netXLB",
			service: &v1.Service{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Service",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "testsvc",
					Namespace:  "testns",
					Finalizers: []string{common.NetLBFinalizerV2},
				},
				Spec: apiv1.ServiceSpec{
					Type: apiv1.ServiceTypeLoadBalancer,
					Ports: []apiv1.ServicePort{
						{
							Name: "http",
							Port: 80,
						},
					},
					IPFamilies: []v1.IPFamily{v1.IPv4Protocol},
				},
			},
			wantL4Protocol: &metrics.ServiceL4ProtocolMetricState{
				Type:                  "RBSXLB",
				ExternalTrafficPolicy: "Cluster",
				InternalTrafficPolicy: "Cluster",
				SessionAffinityConfig: "",
				NumberOfPorts:         "1",
				Protocol:              "TCP",
			},
			wantIPStack: &metrics.ServiceIPStackMetricState{
				Type:                  "RBSXLB",
				ExternalTrafficPolicy: "Cluster",
				InternalTrafficPolicy: "Cluster",
				IPFamilies:            "IPv4",
				IPFamilyPolicy:        "SingleStack",
				IsStaticIPv4:          false,
				IsStaticIPv6:          false,
			},
			wantGCPFeatures: &metrics.ServiceGCPFeaturesMetricState{
				Type:         "RBSXLB",
				NetworkTier:  "Premium",
				GlobalAccess: false,
				CustomSubnet: false,
			},
		},
		{
			desc: "non defaults",
			service: &v1.Service{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Service",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "testsvc",
					Namespace:  "testns",
					Finalizers: []string{common.NetLBFinalizerV2},
					Annotations: map[string]string{
						annotations.NetworkTierAnnotationKey:      string(cloud.NetworkTierStandard),
						gce.ServiceAnnotationILBAllowGlobalAccess: "true",
						gce.ServiceAnnotationILBSubnet:            "testcustomsubnet",
					},
				},

				Spec: apiv1.ServiceSpec{
					Type: apiv1.ServiceTypeLoadBalancer,
					Ports: []apiv1.ServicePort{
						{
							Name: "http",
							Port: 80,
						},
						{
							Name:     "udp",
							Port:     80,
							Protocol: v1.ProtocolUDP,
						},
					},
					ExternalTrafficPolicy: v1.ServiceExternalTrafficPolicyTypeLocal,
					InternalTrafficPolicy: &internalPolicyLocal,
					IPFamilies:            []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol},
					IPFamilyPolicy:        &ipFamilyPolicyRequireDualStack,
					SessionAffinity:       v1.ServiceAffinityClientIP,
					SessionAffinityConfig: &v1.SessionAffinityConfig{
						ClientIP: &v1.ClientIPConfig{
							TimeoutSeconds: &timeout,
						},
					},
					LoadBalancerIP: "10.0.0.1",
				},
			},
			wantL4Protocol: &metrics.ServiceL4ProtocolMetricState{
				Type:                  "RBSXLB",
				ExternalTrafficPolicy: "Local",
				InternalTrafficPolicy: "Local",
				SessionAffinityConfig: "0-10799",
				NumberOfPorts:         "2-5",
				Protocol:              "mixed",
			},
			wantIPStack: &metrics.ServiceIPStackMetricState{
				Type:                  "RBSXLB",
				ExternalTrafficPolicy: "Local",
				InternalTrafficPolicy: "Local",
				IPFamilies:            "IPv6-IPv4",
				IPFamilyPolicy:        "RequireDualStack",
				IsStaticIPv4:          true,
				IsStaticIPv6:          false,
			},
			wantGCPFeatures: &metrics.ServiceGCPFeaturesMetricState{
				Type:         "RBSXLB",
				NetworkTier:  "Standard",
				GlobalAccess: true,
				CustomSubnet: true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {

			controller := createController()
			controller.ctx.ServiceInformer.GetIndexer().Add(tc.service)

			key, err := common.KeyFunc(tc.service)
			if err != nil {
				t.Fatalf("Unexpected error getting key for Service %v: %v", tc.service.Name, err)
			}
			err = controller.sync(key)
			if err != nil {
				t.Errorf("sync() failed, err=%v", err)
			}
			ports, ipStack, policy := controller.ctx.ControllerMetrics.GetServiceMetrics(key)
			if diff := cmp.Diff(tc.wantL4Protocol, ports); diff != "" {
				t.Errorf("ports metrics mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantIPStack, ipStack); diff != "" {
				t.Errorf("IP stack metrics mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantGCPFeatures, policy); diff != "" {
				t.Errorf("policy metrics mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	controller := createController()
	svc := test.NewService(test.DefaultBeSvcPort.ID.Service, apiv1.ServiceSpec{
		Type: apiv1.ServiceTypeLoadBalancer,
		Ports: []apiv1.ServicePort{
			{
				Name: "http",
				Port: 80,
			},
		},
		IPFamilies: []v1.IPFamily{v1.IPv4Protocol},
	})
	svc.ObjectMeta.Finalizers = []string{common.NetLBFinalizerV2}
	key, err := common.KeyFunc(svc)
	if err != nil {
		t.Fatalf("Unexpected error getting key for Service %v: %v", svc.Name, err)
	}
	l4Protocol := &metrics.ServiceL4ProtocolMetricState{
		Type:                  "RBSXLB",
		ExternalTrafficPolicy: "Cluster",
		InternalTrafficPolicy: "Cluster",
		SessionAffinityConfig: "10800",
		NumberOfPorts:         "1",
		Protocol:              "TCP",
	}
	ipStack := &metrics.ServiceIPStackMetricState{
		Type:                  "RBSXLB",
		ExternalTrafficPolicy: "Cluster",
		InternalTrafficPolicy: "Cluster",
		IPFamilies:            "IPv4",
		IPFamilyPolicy:        "SingleStack",
		IsStaticIPv4:          false,
		IsStaticIPv6:          false,
	}
	gcpFeatures := &metrics.ServiceGCPFeaturesMetricState{
		Type:         "RBSXLB",
		NetworkTier:  "Standard",
		GlobalAccess: false,
		CustomSubnet: false,
	}
	controller.ctx.ControllerMetrics.SetServiceMetrics(key, *l4Protocol, *ipStack, *gcpFeatures)

	err = controller.sync(key)
	if err != nil {
		t.Errorf("sync() failed, err=%v", err)
	}

	gotL4Protocol, gotIPStack, gotGCPFeatures := controller.ctx.ControllerMetrics.GetServiceMetrics(key)
	if gotL4Protocol != nil {
		t.Errorf("L4PProtocol should be emtpy but was %+v", gotL4Protocol)
	}
	if gotIPStack != nil {
		t.Errorf("ip stack should be emtpy but was %+v", gotIPStack)
	}
	if gotGCPFeatures != nil {
		t.Errorf("GCPfeatures should be emtpy but was %+v", gotGCPFeatures)
	}
}

func TestGetExternalTrafficPolicy(t *testing.T) {
	cases := []struct {
		desc    string
		service *v1.Service
		want    string
	}{
		{
			desc:    "default",
			service: &v1.Service{},
			want:    string(v1.ServiceExternalTrafficPolicyTypeCluster),
		},
		{
			desc: "cluster",
			service: &v1.Service{
				Spec: v1.ServiceSpec{
					ExternalTrafficPolicy: v1.ServiceExternalTrafficPolicyTypeCluster,
				},
			},
			want: string(v1.ServiceExternalTrafficPolicyTypeCluster),
		},
		{
			desc: "local",
			service: &v1.Service{
				Spec: v1.ServiceSpec{
					ExternalTrafficPolicy: v1.ServiceExternalTrafficPolicyTypeLocal,
				},
			},
			want: string(v1.ServiceExternalTrafficPolicyTypeLocal),
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := getExternalTrafficPolicy(tc.service)
			if tc.want != got {
				t.Errorf("getExternalTrafficPolicy output differed, want=%q, got=%q", tc.want, got)
			}
		})
	}
}

func TestGetInternalTrafficPolicy(t *testing.T) {
	policyCluster := v1.ServiceInternalTrafficPolicyCluster
	policyLocal := v1.ServiceInternalTrafficPolicyLocal
	cases := []struct {
		desc    string
		service *v1.Service
		want    string
	}{
		{
			desc:    "default",
			service: &v1.Service{},
			want:    string(v1.ServiceInternalTrafficPolicyCluster),
		},
		{
			desc: "cluster",
			service: &v1.Service{
				Spec: v1.ServiceSpec{
					InternalTrafficPolicy: &policyCluster,
				},
			},
			want: string(v1.ServiceInternalTrafficPolicyCluster),
		},
		{
			desc: "local",
			service: &v1.Service{
				Spec: v1.ServiceSpec{
					InternalTrafficPolicy: &policyLocal,
				},
			},
			want: string(v1.ServiceInternalTrafficPolicyLocal),
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := getInternalTrafficPolicy(tc.service)
			if tc.want != got {
				t.Errorf("getInternalTrafficPolicy output differed, want=%q, got=%q", tc.want, got)
			}
		})
	}

}

func TestGetPortsBucket(t *testing.T) {
	cases := []struct {
		desc  string
		ports []v1.ServicePort
		want  string
	}{
		{
			desc:  "0",
			ports: []v1.ServicePort{},
			want:  "0",
		},
		{
			desc: "1",
			ports: []v1.ServicePort{{
				Name: "p1",
				Port: 80,
			},
			},
			want: "1",
		},
		{
			desc: "2-5",
			ports: []v1.ServicePort{{
				Name: "p1",
				Port: 80,
			},
				{
					Name: "p2",
					Port: 123456,
				}},
			want: "2-5",
		},
		{
			desc:  "6-100",
			ports: make([]v1.ServicePort, 10),
			want:  "6-100",
		},
		{
			desc:  "100+",
			ports: make([]v1.ServicePort, 101),
			want:  "100+",
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := getPortsBucket(tc.ports)
			if tc.want != got {
				t.Errorf("getPortsBucket output differed, want=%q, got=%q", tc.want, got)
			}
		})
	}
}

func TestGetProtocol(t *testing.T) {
	cases := []struct {
		desc  string
		ports []v1.ServicePort
		want  string
	}{
		{
			desc:  "default",
			ports: []v1.ServicePort{{Name: "default port"}},
			want:  "TCP",
		},
		{
			desc:  "TCP",
			ports: []v1.ServicePort{{Protocol: v1.ProtocolTCP}},
			want:  "TCP",
		},
		{
			desc:  "UDP",
			ports: []v1.ServicePort{{Protocol: v1.ProtocolUDP}, {Protocol: v1.ProtocolUDP}},
			want:  "UDP",
		},
		{
			desc:  "different",
			ports: []v1.ServicePort{{Protocol: v1.ProtocolTCP}, {Protocol: v1.ProtocolUDP}},
			want:  "mixed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := getProtocol(tc.ports)
			if tc.want != got {
				t.Errorf("getProtocol output differed, want=%q, got=%q", tc.want, got)
			}
		})
	}
}

func TestGetIPFamilies(t *testing.T) {
	cases := []struct {
		desc     string
		families []v1.IPFamily
		want     string
	}{
		{
			desc:     "IPv4",
			families: []v1.IPFamily{v1.IPv4Protocol},
			want:     "IPv4",
		},
		{
			desc:     "IPv6",
			families: []v1.IPFamily{v1.IPv6Protocol},
			want:     "IPv6",
		},
		{
			desc:     "IPv6-IPv4",
			families: []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol},
			want:     "IPv6-IPv4",
		},
		{
			desc:     "IPv4-IPv6",
			families: []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
			want:     "IPv4-IPv6",
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := getIPFamilies(tc.families)
			if tc.want != got {
				t.Errorf("getIPFamilies output differed, want=%q, got=%q", tc.want, got)
			}
		})
	}
}

func TestGetIPFamilyPolicy(t *testing.T) {
	singleStack := v1.IPFamilyPolicySingleStack
	preferDualStack := v1.IPFamilyPolicyPreferDualStack
	requireDualStack := v1.IPFamilyPolicyRequireDualStack
	cases := []struct {
		desc       string
		policyType *v1.IPFamilyPolicyType
		want       string
	}{
		{
			desc:       "default",
			policyType: nil,
			want:       string(v1.IPFamilyPolicySingleStack),
		},
		{
			desc:       "single stack",
			policyType: &singleStack,
			want:       string(v1.IPFamilyPolicySingleStack),
		},
		{
			desc:       "prefer dual stack",
			policyType: &preferDualStack,
			want:       string(v1.IPFamilyPolicyPreferDualStack),
		},
		{
			desc:       "require dual stack",
			policyType: &requireDualStack,
			want:       string(v1.IPFamilyPolicyRequireDualStack),
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := getIPFamilyPolicy(tc.policyType)
			if tc.want != got {
				t.Errorf("getIPFamilyPolicy output differed, want=%q, got=%q", tc.want, got)
			}
		})
	}
}

func TestGetLBType(t *testing.T) {
	cases := []struct {
		desc    string
		service *v1.Service
		want    string
	}{
		{
			desc:    "non LB",
			service: &v1.Service{},
			want:    "",
		},
		{
			desc: "SubsettingILB",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{gce.ServiceAnnotationLoadBalancerType: string(gce.LBTypeInternal)},
					Finalizers:  []string{common.ILBFinalizerV2},
				},
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
			},
			want: "SubsettingILB",
		},
		{
			desc: "LegacyILB",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{gce.ServiceAnnotationLoadBalancerType: string(gce.LBTypeInternal)},
				},
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
			},
			want: "LegacyILB",
		},
		{
			desc: "RBSXLB",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
					Finalizers:  []string{common.NetLBFinalizerV2},
				},
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
			},
			want: "RBSXLB",
		},
		{
			desc: "LegacyXLB",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
			},
			want: "LegacyXLB",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := getServiceType(tc.service)
			if tc.want != got {
				t.Errorf("getServiceType output differed, want=%q, got=%q", tc.want, got)
			}
		})
	}
}

func TestGetSessionAffinityConfig(t *testing.T) {
	var oneSecond int32 = 1
	var defaultPlus1 int32 = 10801
	cases := []struct {
		desc    string
		service *v1.Service
		want    string
	}{
		{
			desc:    "no session affinity",
			service: &v1.Service{},
			want:    "",
		},
		{
			desc: "default client IP",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{gce.ServiceAnnotationLoadBalancerType: string(gce.LBTypeInternal)},
					Finalizers:  []string{common.ILBFinalizerV2},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityClientIP,
				},
			},
			want: "10800",
		},
		{
			desc: "0-10799",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{gce.ServiceAnnotationLoadBalancerType: string(gce.LBTypeInternal)},
					Finalizers:  []string{common.ILBFinalizerV2},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityClientIP,
					SessionAffinityConfig: &v1.SessionAffinityConfig{
						ClientIP: &v1.ClientIPConfig{
							TimeoutSeconds: &oneSecond,
						},
					},
				},
			},
			want: "0-10799",
		},
		{
			desc: "10800+",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{gce.ServiceAnnotationLoadBalancerType: string(gce.LBTypeInternal)},
					Finalizers:  []string{common.ILBFinalizerV2},
				},
				Spec: v1.ServiceSpec{
					SessionAffinity: v1.ServiceAffinityClientIP,
					SessionAffinityConfig: &v1.SessionAffinityConfig{
						ClientIP: &v1.ClientIPConfig{
							TimeoutSeconds: &defaultPlus1,
						},
					},
				},
			},
			want: "10800+",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := getSessionAffinityConfig(tc.service)
			if tc.want != got {
				t.Errorf("getSessionAffinityConfig output differed, want=%q, got=%q", tc.want, got)
			}
		})
	}
}
