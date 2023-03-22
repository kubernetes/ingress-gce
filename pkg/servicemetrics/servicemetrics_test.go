package servicemetrics

import (
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/google/go-cmp/cmp"
	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils/common"
	"testing"
)

func TestMetricsFromService(t *testing.T) {
	var timeout int32 = 10
	internalPolicyLocal := v1.ServiceInternalTrafficPolicyLocal
	ipFamilyPolicyRequireDualStack := v1.IPFamilyPolicyRequireDualStack

	cases := []struct {
		desc            string
		service         *v1.Service
		wantL4Protocol  *serviceL4ProtocolMetricState
		wantIPStack     *serviceIPStackMetricState
		wantGCPFeatures *serviceGCPFeaturesMetricState
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
			wantL4Protocol: &serviceL4ProtocolMetricState{
				Type:                  serviceTypeRBSXLB,
				ExternalTrafficPolicy: string(v1.ServiceExternalTrafficPolicyTypeCluster),
				InternalTrafficPolicy: string(v1.ServiceInternalTrafficPolicyCluster),
				SessionAffinityConfig: sessionAffinityBucketNone,
				NumberOfPorts:         "1",
				Protocol:              "TCP",
			},
			wantIPStack: &serviceIPStackMetricState{
				Type:                  serviceTypeRBSXLB,
				ExternalTrafficPolicy: string(v1.ServiceExternalTrafficPolicyTypeCluster),
				InternalTrafficPolicy: string(v1.ServiceInternalTrafficPolicyCluster),
				IPFamilies:            "IPv4",
				IPFamilyPolicy:        "SingleStack",
				IsStaticIPv4:          false,
				IsStaticIPv6:          false,
			},
			wantGCPFeatures: &serviceGCPFeaturesMetricState{
				Type:         serviceTypeRBSXLB,
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
			wantL4Protocol: &serviceL4ProtocolMetricState{
				Type:                  serviceTypeRBSXLB,
				ExternalTrafficPolicy: string(v1.ServiceExternalTrafficPolicyTypeLocal),
				InternalTrafficPolicy: string(v1.ServiceInternalTrafficPolicyLocal),
				SessionAffinityConfig: sessionAffinityBucketLessThanDefault,
				NumberOfPorts:         "2-5",
				Protocol:              "mixed",
			},
			wantIPStack: &serviceIPStackMetricState{
				Type:                  serviceTypeRBSXLB,
				ExternalTrafficPolicy: string(v1.ServiceExternalTrafficPolicyTypeLocal),
				InternalTrafficPolicy: string(v1.ServiceInternalTrafficPolicyLocal),
				IPFamilies:            "IPv6-IPv4",
				IPFamilyPolicy:        string(v1.IPFamilyPolicyRequireDualStack),
				IsStaticIPv4:          true,
				IsStaticIPv6:          false,
			},
			wantGCPFeatures: &serviceGCPFeaturesMetricState{
				Type:         serviceTypeRBSXLB,
				NetworkTier:  "Standard",
				GlobalAccess: true,
				CustomSubnet: true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			ports, ipStack, policy := metricsFromService(tc.service)
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

func TestMetricCounting(t *testing.T) {
	ipFamilyPolicyRequireDualStack := v1.IPFamilyPolicyRequireDualStack
	services := []*v1.Service{
		{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:       "testsvc1",
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
		{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:       "testsvc2",
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
				IPFamilyPolicy: &ipFamilyPolicyRequireDualStack,
				IPFamilies:     []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
				LoadBalancerIP: "34.12.153.11",
			},
		},
	}

	l4ProtocolKeyS1AndS2 := serviceL4ProtocolMetricState{
		Type:                  serviceTypeRBSXLB,
		ExternalTrafficPolicy: string(v1.ServiceExternalTrafficPolicyTypeCluster),
		InternalTrafficPolicy: string(v1.ServiceInternalTrafficPolicyCluster),
		SessionAffinityConfig: sessionAffinityBucketNone,
		NumberOfPorts:         "1",
		Protocol:              "TCP",
	}
	ipStackKeyS1 := serviceIPStackMetricState{
		Type:                  serviceTypeRBSXLB,
		ExternalTrafficPolicy: string(v1.ServiceExternalTrafficPolicyTypeCluster),
		InternalTrafficPolicy: string(v1.ServiceInternalTrafficPolicyCluster),
		IPFamilies:            "IPv4",
		IPFamilyPolicy:        string(v1.IPFamilyPolicySingleStack),
		IsStaticIPv4:          false,
		IsStaticIPv6:          false,
	}
	ipStackKeyS2 := serviceIPStackMetricState{
		Type:                  serviceTypeRBSXLB,
		ExternalTrafficPolicy: string(v1.ServiceExternalTrafficPolicyTypeCluster),
		InternalTrafficPolicy: string(v1.ServiceInternalTrafficPolicyCluster),
		IPFamilies:            "IPv4-IPv6",
		IPFamilyPolicy:        string(v1.IPFamilyPolicyRequireDualStack),
		IsStaticIPv4:          true,
		IsStaticIPv6:          false,
	}
	gcpFeaturesKeyS1AndS2 := serviceGCPFeaturesMetricState{
		Type:         serviceTypeRBSXLB,
		NetworkTier:  "Premium",
		GlobalAccess: false,
		CustomSubnet: false,
	}

	l4ProtocolState, ipStackState, gcpFeaturesState := calculateMetrics(services)

	l4ProtocolVal, ok := l4ProtocolState[l4ProtocolKeyS1AndS2]
	if !ok || l4ProtocolVal != 2 {
		t.Errorf("l4Protocol metrics were missing or invalid, present=%t, value=%d", ok, l4ProtocolVal)
	}
	ipStackVal, ok := ipStackState[ipStackKeyS1]
	if !ok || ipStackVal != 1 {
		t.Errorf("IP stack metrics were missing or invalid, present=%t, value=%d", ok, ipStackVal)
	}
	ipStackVal2, ok := ipStackState[ipStackKeyS2]
	if !ok || ipStackVal2 != 1 {
		t.Errorf("IP stack metrics were missing or invalid, present=%t, value=%d", ok, ipStackVal2)
	}
	gcpFeaturesVal, ok := gcpFeaturesState[gcpFeaturesKeyS1AndS2]
	if !ok || gcpFeaturesVal != 2 {
		t.Errorf("l4Protocol metrics were missing or invalid, present=%t, value=%d", ok, gcpFeaturesVal)
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
			ports: []v1.ServicePort{
				{
					Name: "p1",
					Port: 80,
				},
			},
			want: "1",
		},
		{
			desc: "2-5",
			ports: []v1.ServicePort{
				{
					Name: "p1",
					Port: 80,
				},
				{
					Name: "p2",
					Port: 123456,
				},
			},
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
			want: serviceTypeSubsettingILB,
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
			want: serviceTypeLegacyILB,
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
			want: serviceTypeRBSXLB,
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
			want: serviceTypeLegacyXLB,
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
			want:    sessionAffinityBucketNone,
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
			want: sessionAffinityBucketDefault,
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
			want: sessionAffinityBucketLessThanDefault,
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
			want: sessionAffinityBucketMoreThanDefault,
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

func TestIsStaticIPv4(t *testing.T) {
	cases := []struct {
		desc           string
		loadBalancerIP string
		want           bool
	}{
		{
			desc:           "empty",
			loadBalancerIP: "",
			want:           false,
		},
		{
			desc:           "valid IPv4",
			loadBalancerIP: "10.0.0.1",
			want:           true,
		},
		{
			desc:           "invalid IPv4",
			loadBalancerIP: "500.0.0.1",
			want:           false,
		},
		{
			desc:           "non IPv4",
			loadBalancerIP: "2001:db8::8a2e:370:7334",
			want:           false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := isStaticIPv4(tc.loadBalancerIP)
			if tc.want != got {
				t.Errorf("isStaticIPv4 output differed, want=%t, got=%t", tc.want, got)
			}
		})
	}
}

func TestIsStaticIPv6(t *testing.T) {
	cases := []struct {
		desc           string
		loadBalancerIP string
		want           bool
	}{
		{
			desc:           "empty",
			loadBalancerIP: "",
			want:           false,
		},
		{
			desc:           "valid IPv6",
			loadBalancerIP: "2001:db8::8a2e:370:7334",
			want:           true,
		},
		{
			desc:           "non IPv6",
			loadBalancerIP: "10.0.0.1",
			want:           false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := isStaticIPv6(tc.loadBalancerIP)
			if tc.want != got {
				t.Errorf("isStaticIPv6 output differed, want=%t, got=%t", tc.want, got)
			}
		})
	}
}
