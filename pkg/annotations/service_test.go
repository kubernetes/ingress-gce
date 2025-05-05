/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package annotations

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/utils/common"
)

func TestWantsLB(t *testing.T) {
	type wants struct {
		ILB   bool
		NetLB bool
	}

	testCases := []struct {
		desc string
		svc  *v1.Service
		want wants
	}{
		{
			desc: "nil",
			svc:  nil,
			want: wants{},
		},
		{
			desc: "empty service",
			svc:  &v1.Service{},
			want: wants{},
		},
		{
			desc: "ilb",
			svc: &v1.Service{
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						gce.ServiceAnnotationLoadBalancerType: string(gce.LBTypeInternal),
					},
				},
			},
			want: wants{
				ILB: true,
			},
		},
		{
			desc: "netlb rbs",
			svc: &v1.Service{
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"cloud.google.com/l4-rbs": "enabled",
					},
				},
			},
			want: wants{
				NetLB: true,
			},
		},
		{
			desc: "ilb to netlb with finalizer",
			svc: &v1.Service{
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.ILBFinalizerV2},
					Annotations: map[string]string{
						"cloud.google.com/l4-rbs": "enabled",
					},
				},
			},
			want: wants{},
		},
		{
			desc: "netlb to ilb with finalizer",
			svc: &v1.Service{
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.NetLBFinalizerV2},
					Annotations: map[string]string{
						gce.ServiceAnnotationLoadBalancerType: string(gce.LBTypeInternal),
					},
				},
			},
			want: wants{},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {

		})
	}
}

func TestNEGAnnotation(t *testing.T) {
	for _, tc := range []struct {
		desc                string
		svc                 *v1.Service
		expectNegAnnotation *NegAnnotation
		expectError         error
		expectFound         bool
		negEnabled          bool
		ingress             bool
		exposed             bool
	}{
		{
			desc:        "NEG annotation not specified",
			svc:         &v1.Service{},
			expectFound: false,
			expectError: nil,
		},
		{
			desc: "NEG annotation is malformed",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `foo`,
					},
				},
			},
			expectFound: true,
			expectError: ErrNEGAnnotationInvalid,
		},
		{
			desc: "NEG annotation is malformed 2",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{80:{}}}`,
					},
				},
			},
			expectFound: true,
			expectError: ErrNEGAnnotationInvalid,
		},
		{
			desc: "NEG enabled for ingress",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"ingress":true}`,
					},
				},
			},
			expectFound: true,
			expectNegAnnotation: &NegAnnotation{
				Ingress: true,
			},
			negEnabled: true,
			ingress:    true,
			exposed:    false,
		},
		{
			desc: "NEG exposed",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}}}`,
					},
				},
			},
			expectFound: true,
			expectNegAnnotation: &NegAnnotation{
				ExposedPorts: map[int32]NegAttributes{int32(80): {}},
			},
			negEnabled: true,
			ingress:    false,
			exposed:    true,
		},
		{
			desc: "Multiple ports exposed",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}, "443":{}}}`,
					},
				},
			},
			expectFound: true,
			expectNegAnnotation: &NegAnnotation{
				ExposedPorts: map[int32]NegAttributes{int32(80): {}, int32(443): {}},
			},
			negEnabled: true,
			ingress:    false,
			exposed:    true,
		},
		{
			desc: "Service is enabled for both ingress and exposedNEG",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"ingress":true,"exposed_ports":{"80":{}, "443":{}}}`,
					},
				},
			},
			expectFound: true,
			expectNegAnnotation: &NegAnnotation{
				Ingress:      true,
				ExposedPorts: map[int32]NegAttributes{int32(80): {}, int32(443): {}},
			},
			negEnabled: true,
			ingress:    true,
			exposed:    true,
		},
	} {
		negAnnotation, found, err := FromService(tc.svc).NEGAnnotation()
		if fmt.Sprintf("%q", err) != fmt.Sprintf("%q", tc.expectError) {
			t.Errorf("Test case %q: Expect error to be %q, but got: %q", tc.desc, tc.expectError, err)
		}

		if found != tc.expectFound {
			t.Errorf("Test case %q: Expect found to be %v, be got %v", tc.desc, tc.expectFound, found)
		}

		if tc.expectError != nil || !tc.expectFound {
			continue
		}

		if !reflect.DeepEqual(*tc.expectNegAnnotation, *negAnnotation) {
			t.Errorf("Test case %q: Expect NegAnnotation to be %v, be got %v", tc.desc, *tc.expectNegAnnotation, *negAnnotation)
		}

		if neg := negAnnotation.NEGEnabled(); neg != tc.negEnabled {
			t.Errorf("Test case %q: Expect EnabledNEG() to be %v, be got %v", tc.desc, tc.negEnabled, neg)
		}

		if ing := negAnnotation.NEGEnabledForIngress(); ing != tc.ingress {
			t.Errorf("Test case %q: Expect NEGEnabledForIngress() = %v; want %v", tc.desc, tc.ingress, ing)
		}

		if exposed := negAnnotation.NEGExposed(); exposed != tc.exposed {
			t.Errorf("Test case %q: Expect NEGExposed() = %v; want %v", tc.desc, tc.exposed, exposed)
		}
	}
}

func TestNEGStatus(t *testing.T) {
	for _, tc := range []struct {
		desc            string
		svc             *v1.Service
		expectNegStatus *NegStatus
		expectError     error
		expectFound     bool
	}{
		{
			desc:        "No NEG Status",
			svc:         &v1.Service{},
			expectFound: false,
			expectError: nil,
		},
		{
			desc: "Basic NEG Status",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGStatusKey: `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
					},
				},
			},
			expectNegStatus: &NegStatus{NetworkEndpointGroups: PortNegMap{"80": "neg-name"}, Zones: []string{"us-central1-a"}},
			expectFound:     true,
			expectError:     nil,
		},
		{
			desc: "Invalid NEG Status",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGStatusKey: `foobar`,
					},
				},
			},
			expectNegStatus: nil,
			expectFound:     true,
			expectError:     fmt.Errorf("Error parsing neg status: invalid character 'o' in literal false (expecting 'a')"),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			negStatus, found, err := FromService(tc.svc).NEGStatus()

			if fmt.Sprintf("%q", err) != fmt.Sprintf("%q", tc.expectError) {
				t.Errorf("Test case %q: Expect error to be %q, but got: %q", tc.desc, tc.expectError, err)
			}

			if found != tc.expectFound {
				t.Errorf("Test case %q: Expect found to be %v, be got %v", tc.desc, tc.expectFound, found)
			}

			if tc.expectError != nil || !tc.expectFound {
				return
			}

			if !reflect.DeepEqual(*tc.expectNegStatus, *negStatus) {
				t.Errorf("Test case %q: Expect NegStatus to be %v, be got %v", tc.desc, *tc.expectNegStatus, *negStatus)
			}
		})
	}
}

func TestService(t *testing.T) {
	for _, tc := range []struct {
		svc             *v1.Service
		appProtocolsErr bool
		appProtocols    map[string]AppProtocol
	}{
		{
			svc:          &v1.Service{},
			appProtocols: map[string]AppProtocol{},
		},
		{
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						GoogleServiceApplicationProtocolKey: `{"80": "HTTP", "443": "HTTPS"}`,
					},
				},
			},
			appProtocols: map[string]AppProtocol{"80": "HTTP", "443": "HTTPS"},
		},
		{
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						GoogleServiceApplicationProtocolKey: `{"80": "HTTP", "443": "HTTPS"}`,
						ServiceApplicationProtocolKey:       `{"81": "HTTP", "444": "HTTPS"}`,
					},
				},
			},
			appProtocols: map[string]AppProtocol{"81": "HTTP", "444": "HTTPS"},
		},
		{
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						ServiceApplicationProtocolKey: `{"80": "HTTP", "443": "HTTPS"}`,
					},
				},
			},
			appProtocols: map[string]AppProtocol{"80": "HTTP", "443": "HTTPS"},
		},
		{
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						ServiceApplicationProtocolKey: `{"443": "HTTP2"}`,
					},
				},
			},
			appProtocols: map[string]AppProtocol{"443": "HTTP2"},
		},
		{
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						ServiceApplicationProtocolKey: `invalid`,
					},
				},
			},
			appProtocolsErr: true,
		},
		{
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						ServiceApplicationProtocolKey: `{"SSH": "22"}`,
					},
				},
			},
			appProtocolsErr: true,
		},
	} {
		svc := FromService(tc.svc)
		ap, err := svc.ApplicationProtocols()
		if tc.appProtocolsErr {
			if err == nil {
				t.Errorf("for service %+v; svc.ApplicationProtocols() = _, %v; want _, error", tc.svc, err)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(ap, tc.appProtocols) {
			t.Errorf("for service %+v; svc.ApplicationProtocols() = %v, %v; want %v, nil", tc.svc, ap, err, tc.appProtocols)
		}
	}
}

func TestBackendConfigs(t *testing.T) {
	testcases := []struct {
		desc            string
		svc             *v1.Service
		expectedConfigs *BackendConfigs
		expectedErr     error
	}{
		{
			desc:        "no backendConfig annotation",
			svc:         &v1.Service{},
			expectedErr: ErrBackendConfigAnnotationMissing,
		},
		{
			desc: "single backendConfig",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						BackendConfigKey: `{"ports":{"http": "config-http"}}`,
					},
				},
			},
			expectedConfigs: &BackendConfigs{
				Ports: map[string]string{
					"http": "config-http",
				},
			},
		},
		{
			desc: "multiple backendConfigs",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						BackendConfigKey: `{"ports":{"http": "config-http", "https": "config-https"}}`,
					},
				},
			},
			expectedConfigs: &BackendConfigs{
				Ports: map[string]string{
					"http":  "config-http",
					"https": "config-https",
				},
			},
		}, {
			desc: "multiple backendConfigs with default",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						BackendConfigKey: `{"default": "config-default", "ports":{"http": "config-http", "https": "config-https"}}`,
					},
				},
			},
			expectedConfigs: &BackendConfigs{
				Default: "config-default",
				Ports: map[string]string{
					"http":  "config-http",
					"https": "config-https",
				},
			},
		},
		{
			desc: "invalid backendConfig annotation",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						BackendConfigKey: `invalid`,
					},
				},
			},
			expectedErr: ErrBackendConfigInvalidJSON,
		},
		{
			desc: "wrong field name in backendConfig annotation",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						BackendConfigKey: `{"portstypo":{"https": "config-https"}}`,
					},
				},
			},
			expectedErr: ErrBackendConfigNoneFound,
		},
	}

	for _, tc := range testcases {
		svc := FromService(tc.svc)
		configs, err := svc.GetBackendConfigs()
		if !reflect.DeepEqual(configs, tc.expectedConfigs) || tc.expectedErr != err {
			t.Errorf("%s: for annotations %+v; svc.GetBackendConfigs() = %v, %v; want %v, %v", tc.desc, svc.v, configs, err, tc.expectedConfigs, tc.expectedErr)
		}
	}
}

func TestParseNegStatus(t *testing.T) {
	for _, tc := range []struct {
		desc            string
		status          string
		expectNegStatus *NegStatus
		expectError     error
	}{
		{
			desc:            "Test empty string",
			status:          "",
			expectNegStatus: &NegStatus{},
			expectError:     fmt.Errorf("unexpected end of JSON input"),
		},
		{
			desc:            "Test basic status",
			status:          `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
			expectNegStatus: &NegStatus{NetworkEndpointGroups: PortNegMap{"80": "neg-name"}, Zones: []string{"us-central1-a"}},
			expectError:     nil,
		},
		{
			desc:            "Test NEG status with 2 ports",
			status:          `{"network_endpoint_groups":{"80":"neg-name", "443":"another-neg-name"},"zones":["us-central1-a"]}`,
			expectNegStatus: &NegStatus{NetworkEndpointGroups: PortNegMap{"80": "neg-name", "443": "another-neg-name"}, Zones: []string{"us-central1-a"}},
			expectError:     nil,
		},
		{
			desc:            "Incorrect fields",
			status:          `{"network_endpoint_group":{"80":"neg-name"},"zone":["us-central1-a"]}`,
			expectNegStatus: &NegStatus{},
			expectError:     nil,
		},
		{
			desc:            "Invalid annotation",
			status:          `{"network_endpoint_groups":{"80":"neg-name"},"zones":"us-central1-a"]}`,
			expectNegStatus: &NegStatus{},
			expectError:     fmt.Errorf("invalid character ']' after object key:value pair"),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			negStatus, err := ParseNegStatus(tc.status)

			if fmt.Sprintf("%q", err) != fmt.Sprintf("%q", tc.expectError) {
				t.Errorf("Test case %q: Expect error to be %q, but got: %q", tc.desc, tc.expectError, err)
			}

			if !reflect.DeepEqual(*tc.expectNegStatus, negStatus) {
				t.Errorf("Expected NegStatus to be %v, got %v instead", tc.expectNegStatus, negStatus)
			}
		})
	}
}

func TestOnlyStatusAnnotationsChanged(t *testing.T) {
	for _, tc := range []struct {
		desc           string
		service1       *v1.Service
		service2       *v1.Service
		expectedResult bool
	}{
		{
			desc: "Test add neg annotation",
			service1: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
				},
			},
			service2: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service2",
					Annotations: map[string]string{
						NEGStatusKey: `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
					},
				},
			},
			expectedResult: true,
		},
		{
			desc: "Test valid diff",
			service1: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					Annotations: map[string]string{
						NEGStatusKey: `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
					},
				},
			},
			service2: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service2",
					Annotations: map[string]string{
						"RandomAnnotation": "abcde",
					},
				},
			},
			expectedResult: false,
		},
		{
			desc: "Test no change",
			service1: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					Annotations: map[string]string{
						"RandomAnnotation": "abcde",
					},
				},
			},
			service2: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service2",
					Annotations: map[string]string{
						"RandomAnnotation": "abcde",
					},
				},
			},
			expectedResult: true,
		},
		{
			desc: "Test remove NEG annotation",
			service1: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					Annotations: map[string]string{
						NEGStatusKey:       `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
						"RandomAnnotation": "abcde",
					},
				},
			},
			service2: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service2",
					Annotations: map[string]string{
						"RandomAnnotation": "abcde",
					},
				},
			},
			expectedResult: true,
		},
		{
			desc: "Test only ILB ForwardingRule annotation diff",
			service1: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					Annotations: map[string]string{
						FirewallRuleKey:      "rule1",
						TCPForwardingRuleKey: "tcprule",
						NEGStatusKey:         `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
						"RandomAnnotation":   "abcde",
					},
				},
			},
			service2: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service2",
					Annotations: map[string]string{
						FirewallRuleKey:      "rule1",
						UDPForwardingRuleKey: "udprule",
						NEGStatusKey:         `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
						"RandomAnnotation":   "abcde",
					},
				},
			},
			expectedResult: true,
		},
		{
			desc: "Test all status annotations removed",
			service1: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					Annotations: map[string]string{
						FirewallRuleKey:      "rule1",
						TCPForwardingRuleKey: "tcprule",
						NEGStatusKey:         `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
						"RandomAnnotation":   "abcde",
					},
				},
			},
			service2: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service2",
					Annotations: map[string]string{
						"RandomAnnotation": "abcde",
					},
				},
			},
			expectedResult: true,
		},
		{
			desc: "Test change value of non-status annotation",
			service1: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					Annotations: map[string]string{
						FirewallRuleKey:      "rule1",
						TCPForwardingRuleKey: "tcprule",
						NEGStatusKey:         `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
						"RandomAnnotation":   "abcde",
					},
				},
			},
			service2: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service2",
					Annotations: map[string]string{
						FirewallRuleKey:      "rule1",
						TCPForwardingRuleKey: "tcprule",
						NEGStatusKey:         `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
						"RandomAnnotation":   "xyz",
					},
				},
			},
			expectedResult: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			result := OnlyStatusAnnotationsChanged(tc.service1, tc.service2)
			if result != tc.expectedResult {
				t.Errorf("%s: Expected result for input %v, %v to be %v, got %v instead", tc.desc, tc.service1.Annotations, tc.service2.Annotations, tc.expectedResult, result)
			}
		})
	}
}

func TestRBSAnnotation(t *testing.T) {
	for _, tc := range []struct {
		desc string
		svc  *v1.Service
		want bool
	}{
		{
			desc: "RBS annotation not specified",
			svc:  &v1.Service{},
			want: false,
		},
		{
			desc: "RBS annotation enabled",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						RBSAnnotationKey: RBSEnabled,
					},
				},
			},
			want: true,
		},
		{
			desc: "RBS annotation present but not with enabled value",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						RBSAnnotationKey: "otherValue",
					},
				},
			},
			want: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got := HasRBSAnnotation(tc.svc)
			if tc.want != got {
				t.Errorf("output of HasRBSAnnotaiton differed, want=%v, got=%v", tc.want, got)
			}
		})
	}
}

func TestHasStrongSessionAffinityAnnotation(t *testing.T) {
	for _, tc := range []struct {
		desc string
		svc  *v1.Service
		want bool
	}{
		{
			desc: "Strong Session Affinity annotation was not specified",
			svc:  &v1.Service{},
			want: false,
		},
		{
			desc: "Strong Session Affinity annotation was correctly specified",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						StrongSessionAffinityAnnotationKey: StrongSessionAffinityEnabled,
					},
				},
			},
			want: true,
		},
		{
			desc: "Strong Session Affinity annotation has wrong value",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						StrongSessionAffinityAnnotationKey: "otherValue",
					},
				},
			},
			want: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got := HasStrongSessionAffinityAnnotation(tc.svc)
			if tc.want != got {
				t.Errorf("output of HasStrongSessionAffinityAnnotation differed, want=%v, got=%v", tc.want, got)
			}
		})
	}
}

func TestIsLegacyL4ILBService(t *testing.T) {
	t.Parallel()
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "testsvc",
			Namespace:   "default",
			Annotations: map[string]string{gce.ServiceAnnotationLoadBalancerType: string(gce.LBTypeInternal)},
			Finalizers:  []string{common.LegacyILBFinalizer},
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{
				{Name: "testport", Port: int32(80)},
			},
		},
	}
	if !IsLegacyL4ILBService(svc) {
		t.Errorf("Expected True for Legacy service %s, got False", svc.Name)
	}

	// Remove the finalizer and ensure the check returns False.
	svc.ObjectMeta.Finalizers = nil
	if IsLegacyL4ILBService(svc) {
		t.Errorf("Expected False for Legacy service %s, got True", svc.Name)
	}
}

func TestLBBasedOnFinalizer(t *testing.T) {
	type wants struct {
		IsLegacyL4ILBService     bool
		IsSubsettingL4ILBService bool
		HasL4ILBFinalizerV2      bool

		IsRBSL4NetLB          bool
		HasL4NetLBFinalizerV2 bool
		HasL4NetLBFinalizerV3 bool
	}

	testCases := []struct {
		finalizer string
		want      wants
	}{
		{
			finalizer: common.LegacyILBFinalizer,
			want: wants{
				IsLegacyL4ILBService: true,
			},
		},
		{
			finalizer: common.ILBFinalizerV2,
			want: wants{
				IsSubsettingL4ILBService: true,
				HasL4ILBFinalizerV2:      true,
			},
		},
		{
			finalizer: common.NetLBFinalizerV2,
			want: wants{
				IsRBSL4NetLB:          true,
				HasL4NetLBFinalizerV2: true,
			},
		},
		{
			finalizer: common.NetLBFinalizerV3,
			want: wants{
				IsRBSL4NetLB:          true,
				HasL4NetLBFinalizerV3: true,
			},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.finalizer, func(t *testing.T) {
			svc := &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{tC.finalizer},
				},
			}

			got := wants{
				IsLegacyL4ILBService:     IsLegacyL4ILBService(svc),
				IsSubsettingL4ILBService: IsSubsettingL4ILBService(svc),
				HasL4ILBFinalizerV2:      HasL4ILBFinalizerV2(svc),

				IsRBSL4NetLB:          IsRBSL4NetLB(svc),
				HasL4NetLBFinalizerV2: HasL4NetLBFinalizerV2(svc),
				HasL4NetLBFinalizerV3: HasL4NetLBFinalizerV3(svc),
			}

			if diff := cmp.Diff(tC.want, got); diff != "" {
				t.Errorf("got != want, diff(-tc.want +got) = %s", diff)
			}
		})
	}
}
