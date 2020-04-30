/*
Copyright 2020 The Kubernetes Authors.

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

package metrics

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/utils"
)

var (
	testTTL          = int64(10)
	defaultNamespace = "default"
	testServicePorts = []utils.ServicePort{
		{
			ID: utils.ServicePortID{
				Service: types.NamespacedName{
					Name:      "dummy-service",
					Namespace: defaultNamespace,
				},
				Port: intstr.FromInt(80),
			},
			BackendConfig: &backendconfigv1.BackendConfig{
				Spec: backendconfigv1.BackendConfigSpec{
					Cdn: &backendconfigv1.CDNConfig{
						Enabled:     true,
						CachePolicy: &backendconfigv1.CacheKeyPolicy{},
					},
					SessionAffinity: &backendconfigv1.SessionAffinityConfig{
						AffinityType:         "GENERATED_COOKIE",
						AffinityCookieTtlSec: &testTTL,
					},
					SecurityPolicy: &backendconfigv1.SecurityPolicyConfig{
						Name: "security-policy-1",
					},
					ConnectionDraining: &backendconfigv1.ConnectionDrainingConfig{
						DrainingTimeoutSec: testTTL,
					},
				},
			},
		},
		{
			ID: utils.ServicePortID{
				Service: types.NamespacedName{
					Name:      "foo-service",
					Namespace: defaultNamespace,
				},
				Port: intstr.FromInt(80),
			},
			NEGEnabled: true,
			BackendConfig: &backendconfigv1.BackendConfig{
				Spec: backendconfigv1.BackendConfigSpec{
					Iap: &backendconfigv1.IAPConfig{
						Enabled: true,
					},
					SessionAffinity: &backendconfigv1.SessionAffinityConfig{
						AffinityType:         "CLIENT_IP",
						AffinityCookieTtlSec: &testTTL,
					},
					TimeoutSec: &testTTL,
					CustomRequestHeaders: &backendconfigv1.CustomRequestHeadersConfig{
						Headers: []string{},
					},
				},
			},
		},
		// NEG default backend.
		{
			ID: utils.ServicePortID{
				Service: types.NamespacedName{
					Name:      "dummy-service",
					Namespace: defaultNamespace,
				},
				Port: intstr.FromInt(80),
			},
			NEGEnabled:   true,
			L7ILBEnabled: true,
		},
		{
			ID: utils.ServicePortID{
				Service: types.NamespacedName{
					Name:      "bar-service",
					Namespace: defaultNamespace,
				},
				Port: intstr.FromInt(5000),
			},
			NEGEnabled:   true,
			L7ILBEnabled: true,
			BackendConfig: &backendconfigv1.BackendConfig{
				Spec: backendconfigv1.BackendConfigSpec{
					Iap: &backendconfigv1.IAPConfig{
						Enabled: true,
					},
					SessionAffinity: &backendconfigv1.SessionAffinityConfig{
						AffinityType:         "GENERATED_COOKIE",
						AffinityCookieTtlSec: &testTTL,
					},
					ConnectionDraining: &backendconfigv1.ConnectionDrainingConfig{
						DrainingTimeoutSec: testTTL,
					},
				},
			},
		},
	}
	ingressStates = []struct {
		desc             string
		ing              *v1beta1.Ingress
		frontendFeatures []feature
		svcPorts         []utils.ServicePort
		backendFeatures  []feature
	}{
		{
			"empty spec",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress0",
				},
			},
			[]feature{ingress, externalIngress, httpEnabled},
			[]utils.ServicePort{},
			nil,
		},
		{
			"http disabled",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress1",
					Annotations: map[string]string{
						allowHTTPKey: "false"},
				},
			},
			[]feature{ingress, externalIngress},
			[]utils.ServicePort{},
			nil,
		},
		{
			"default backend",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress2",
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{},
				},
			},
			[]feature{ingress, externalIngress, httpEnabled},
			[]utils.ServicePort{testServicePorts[0]},
			[]feature{servicePort, externalServicePort, cloudCDN,
				cookieAffinity, cloudArmor, backendConnectionDraining},
		},
		{
			"host rule only",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress3",
				},
				Spec: v1beta1.IngressSpec{
					Rules: []v1beta1.IngressRule{
						{
							Host: "foo.bar",
						},
					},
				},
			},
			[]feature{ingress, externalIngress, httpEnabled, hostBasedRouting},
			[]utils.ServicePort{},
			nil,
		},
		{
			"both host and path rules",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress4",
				},
				Spec: v1beta1.IngressSpec{
					Rules: []v1beta1.IngressRule{
						{
							Host: "foo.bar",
							IngressRuleValue: v1beta1.IngressRuleValue{
								HTTP: &v1beta1.HTTPIngressRuleValue{
									Paths: []v1beta1.HTTPIngressPath{
										{
											Path: "/foo",
											Backend: v1beta1.IngressBackend{
												ServiceName: "foo-service",
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			[]feature{ingress, externalIngress, httpEnabled,
				hostBasedRouting, pathBasedRouting},
			[]utils.ServicePort{testServicePorts[1]},
			[]feature{servicePort, externalServicePort, neg, cloudIAP,
				clientIPAffinity, backendTimeout, customRequestHeaders},
		},
		{
			"default backend and host rule",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress5",
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{
						{
							Host: "foo.bar",
							IngressRuleValue: v1beta1.IngressRuleValue{
								HTTP: &v1beta1.HTTPIngressRuleValue{
									Paths: []v1beta1.HTTPIngressPath{
										{
											Path: "/foo",
											Backend: v1beta1.IngressBackend{
												ServiceName: "foo-service",
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			[]feature{ingress, externalIngress, httpEnabled,
				hostBasedRouting, pathBasedRouting},
			testServicePorts[:2],
			[]feature{servicePort, externalServicePort, cloudCDN,
				cookieAffinity, cloudArmor, backendConnectionDraining, neg, cloudIAP,
				clientIPAffinity, backendTimeout, customRequestHeaders},
		},
		{
			"tls termination with pre-shared certs",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress6",
					Annotations: map[string]string{
						preSharedCertKey: "pre-shared-cert1,pre-shared-cert2",
						SSLCertKey:       "pre-shared-cert1,pre-shared-cert2",
					},
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{},
				},
			},
			[]feature{ingress, externalIngress, httpEnabled,
				tlsTermination, preSharedCertsForTLS},
			[]utils.ServicePort{testServicePorts[0]},
			[]feature{servicePort, externalServicePort, cloudCDN,
				cookieAffinity, cloudArmor, backendConnectionDraining},
		},
		{
			"tls termination with google managed certs",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress7",
					Annotations: map[string]string{
						managedCertKey: "managed-cert1,managed-cert2",
						SSLCertKey:     "managed-cert1,managed-cert2",
					},
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{},
				},
			},
			[]feature{ingress, externalIngress, httpEnabled,
				tlsTermination, managedCertsForTLS},
			[]utils.ServicePort{testServicePorts[0]},
			[]feature{servicePort, externalServicePort, cloudCDN,
				cookieAffinity, cloudArmor, backendConnectionDraining},
		},
		{
			"tls termination with pre-shared and google managed certs",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress8",
					Annotations: map[string]string{
						preSharedCertKey: "pre-shared-cert1,pre-shared-cert2",
						managedCertKey:   "managed-cert1,managed-cert2",
						SSLCertKey:       "pre-shared-cert1,pre-shared-cert2,managed-cert1,managed-cert2",
					},
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{},
				},
			},
			[]feature{ingress, externalIngress, httpEnabled,
				tlsTermination, preSharedCertsForTLS, managedCertsForTLS},
			[]utils.ServicePort{testServicePorts[0]},
			[]feature{servicePort, externalServicePort, cloudCDN,
				cookieAffinity, cloudArmor, backendConnectionDraining},
		},
		{
			"tls termination with pre-shared and secret based certs",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress9",
					Annotations: map[string]string{
						preSharedCertKey: "pre-shared-cert1,pre-shared-cert2",
						SSLCertKey:       "pre-shared-cert1,pre-shared-cert2",
					},
				},
				Spec: v1beta1.IngressSpec{
					Rules: []v1beta1.IngressRule{
						{
							Host: "foo.bar",
							IngressRuleValue: v1beta1.IngressRuleValue{
								HTTP: &v1beta1.HTTPIngressRuleValue{
									Paths: []v1beta1.HTTPIngressPath{
										{
											Path: "/foo",
											Backend: v1beta1.IngressBackend{
												ServiceName: "foo-service",
												ServicePort: intstr.FromInt(80),
											},
										},
									},
								},
							},
						},
					},
					TLS: []v1beta1.IngressTLS{
						{
							Hosts:      []string{"foo.bar"},
							SecretName: "secret-1",
						},
					},
				},
			},
			[]feature{ingress, externalIngress, httpEnabled, hostBasedRouting,
				pathBasedRouting, tlsTermination, preSharedCertsForTLS, secretBasedCertsForTLS},
			[]utils.ServicePort{testServicePorts[1]},
			[]feature{servicePort, externalServicePort, neg, cloudIAP,
				clientIPAffinity, backendTimeout, customRequestHeaders},
		},
		{
			"global static ip",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress10",
					Annotations: map[string]string{
						preSharedCertKey: "pre-shared-cert1,pre-shared-cert2",
						SSLCertKey:       "pre-shared-cert1,pre-shared-cert2",
						staticIPKey:      "10.0.1.2",
					},
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{},
				},
			},
			[]feature{ingress, externalIngress, httpEnabled,
				tlsTermination, preSharedCertsForTLS, staticGlobalIP, managedStaticGlobalIP},
			[]utils.ServicePort{testServicePorts[0]},
			[]feature{servicePort, externalServicePort, cloudCDN,
				cookieAffinity, cloudArmor, backendConnectionDraining},
		},
		{
			"default backend, host rule for internal load-balancer",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress11",
					Annotations: map[string]string{
						ingressClassKey: gceL7ILBIngressClass,
					},
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{
						{
							Host: "bar",
							IngressRuleValue: v1beta1.IngressRuleValue{
								HTTP: &v1beta1.HTTPIngressRuleValue{
									Paths: []v1beta1.HTTPIngressPath{
										{
											Path: "/bar",
											Backend: v1beta1.IngressBackend{
												ServiceName: "bar-service",
												ServicePort: intstr.FromInt(5000),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			[]feature{ingress, internalIngress, httpEnabled,
				hostBasedRouting, pathBasedRouting},
			[]utils.ServicePort{testServicePorts[2], testServicePorts[3]},
			[]feature{servicePort, internalServicePort, neg, cloudIAP,
				cookieAffinity, backendConnectionDraining},
		},
		{
			"non-existent pre-shared cert",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress12",
					Annotations: map[string]string{
						preSharedCertKey: "pre-shared-cert1",
					},
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{},
				},
			},
			[]feature{ingress, externalIngress, httpEnabled},
			[]utils.ServicePort{},
			nil,
		},
		{
			"user specified global static IP",
			&v1beta1.Ingress{
				ObjectMeta: v1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "ingress13",
					Annotations: map[string]string{
						StaticIPNameKey: "user-spec-static-ip",
						staticIPKey:     "user-spec-static-ip",
					},
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "dummy-service",
						ServicePort: intstr.FromInt(80),
					},
					Rules: []v1beta1.IngressRule{},
				},
			},
			[]feature{ingress, externalIngress, httpEnabled,
				staticGlobalIP, specifiedStaticGlobalIP},
			[]utils.ServicePort{},
			nil,
		},
	}
)

func TestFeaturesForIngress(t *testing.T) {
	t.Parallel()
	for _, tc := range ingressStates {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			gotFrontendFeatures := featuresForIngress(tc.ing)
			if diff := cmp.Diff(tc.frontendFeatures, gotFrontendFeatures); diff != "" {
				t.Fatalf("Got diff for frontend features (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFeaturesForServicePort(t *testing.T) {
	t.Parallel()
	for _, tc := range ingressStates {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			backendFeatureMap := make(map[feature]bool)
			var gotBackendFeatures []feature
			for _, svcPort := range tc.svcPorts {
				for _, feature := range featuresForServicePort(svcPort) {
					if backendFeatureMap[feature] {
						continue
					}
					backendFeatureMap[feature] = true
					gotBackendFeatures = append(gotBackendFeatures, feature)
				}
			}
			if diff := cmp.Diff(tc.backendFeatures, gotBackendFeatures); diff != "" {
				t.Fatalf("Got diff for backend features (-want +got):\n%s", diff)
			}
		})
	}
}

func TestComputeIngressMetrics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc               string
		ingressStates      []IngressState
		expectIngressCount map[feature]int
		expectSvcPortCount map[feature]int
	}{
		{
			"frontend features only",
			[]IngressState{
				NewIngressState(ingressStates[0].ing, ingressStates[0].svcPorts),
				NewIngressState(ingressStates[1].ing, ingressStates[1].svcPorts),
				NewIngressState(ingressStates[3].ing, ingressStates[3].svcPorts),
				NewIngressState(ingressStates[13].ing, ingressStates[13].svcPorts),
			},
			map[feature]int{
				backendConnectionDraining: 0,
				backendTimeout:            0,
				clientIPAffinity:          0,
				cloudArmor:                0,
				cloudCDN:                  0,
				cloudIAP:                  0,
				cookieAffinity:            0,
				customRequestHeaders:      0,
				externalIngress:           4,
				httpEnabled:               3,
				hostBasedRouting:          1,
				ingress:                   4,
				internalIngress:           0,
				managedCertsForTLS:        0,
				managedStaticGlobalIP:     0,
				neg:                       0,
				pathBasedRouting:          0,
				preSharedCertsForTLS:      0,
				secretBasedCertsForTLS:    0,
				specifiedStaticGlobalIP:   1,
				staticGlobalIP:            1,
				tlsTermination:            0,
			},
			map[feature]int{
				backendConnectionDraining: 0,
				backendTimeout:            0,
				clientIPAffinity:          0,
				cloudArmor:                0,
				cloudCDN:                  0,
				cloudIAP:                  0,
				cookieAffinity:            0,
				customRequestHeaders:      0,
				internalServicePort:       0,
				servicePort:               0,
				externalServicePort:       0,
				neg:                       0,
			},
		},
		{
			"features for internal and external load-balancers",
			[]IngressState{
				NewIngressState(ingressStates[0].ing, ingressStates[0].svcPorts),
				NewIngressState(ingressStates[1].ing, ingressStates[1].svcPorts),
				NewIngressState(ingressStates[3].ing, ingressStates[3].svcPorts),
				NewIngressState(ingressStates[11].ing, ingressStates[11].svcPorts),
				NewIngressState(ingressStates[13].ing, ingressStates[13].svcPorts),
			},
			map[feature]int{
				backendConnectionDraining: 1,
				backendTimeout:            0,
				clientIPAffinity:          0,
				cloudArmor:                0,
				cloudCDN:                  0,
				cloudIAP:                  1,
				cookieAffinity:            1,
				customRequestHeaders:      0,
				externalIngress:           4,
				httpEnabled:               4,
				hostBasedRouting:          2,
				ingress:                   5,
				internalIngress:           1,
				managedCertsForTLS:        0,
				managedStaticGlobalIP:     0,
				neg:                       1,
				pathBasedRouting:          1,
				preSharedCertsForTLS:      0,
				secretBasedCertsForTLS:    0,
				specifiedStaticGlobalIP:   1,
				staticGlobalIP:            1,
				tlsTermination:            0,
			},
			map[feature]int{
				backendConnectionDraining: 1,
				backendTimeout:            0,
				clientIPAffinity:          0,
				cloudArmor:                0,
				cloudCDN:                  0,
				cloudIAP:                  1,
				cookieAffinity:            1,
				customRequestHeaders:      0,
				internalServicePort:       2,
				servicePort:               2,
				externalServicePort:       0,
				neg:                       2,
			},
		},
		{
			"frontend and backend features",
			[]IngressState{
				NewIngressState(ingressStates[2].ing, ingressStates[2].svcPorts),
				NewIngressState(ingressStates[4].ing, ingressStates[4].svcPorts),
				NewIngressState(ingressStates[6].ing, ingressStates[6].svcPorts),
				NewIngressState(ingressStates[8].ing, ingressStates[8].svcPorts),
				NewIngressState(ingressStates[10].ing, ingressStates[10].svcPorts),
				NewIngressState(ingressStates[12].ing, ingressStates[12].svcPorts),
			},
			map[feature]int{
				backendConnectionDraining: 4,
				backendTimeout:            1,
				clientIPAffinity:          1,
				cloudArmor:                4,
				cloudCDN:                  4,
				cloudIAP:                  1,
				cookieAffinity:            4,
				customRequestHeaders:      1,
				externalIngress:           6,
				httpEnabled:               6,
				hostBasedRouting:          1,
				ingress:                   6,
				internalIngress:           0,
				managedCertsForTLS:        1,
				managedStaticGlobalIP:     1,
				neg:                       1,
				pathBasedRouting:          1,
				preSharedCertsForTLS:      3,
				secretBasedCertsForTLS:    0,
				specifiedStaticGlobalIP:   0,
				staticGlobalIP:            1,
				tlsTermination:            3,
			},
			map[feature]int{
				backendConnectionDraining: 1,
				backendTimeout:            1,
				clientIPAffinity:          1,
				cloudArmor:                1,
				cloudCDN:                  1,
				cloudIAP:                  1,
				cookieAffinity:            1,
				customRequestHeaders:      1,
				internalServicePort:       0,
				servicePort:               2,
				externalServicePort:       2,
				neg:                       1,
			},
		},
		{
			"all ingress features",
			[]IngressState{
				NewIngressState(ingressStates[0].ing, ingressStates[0].svcPorts),
				NewIngressState(ingressStates[1].ing, ingressStates[1].svcPorts),
				NewIngressState(ingressStates[2].ing, ingressStates[2].svcPorts),
				NewIngressState(ingressStates[3].ing, ingressStates[3].svcPorts),
				NewIngressState(ingressStates[4].ing, ingressStates[4].svcPorts),
				NewIngressState(ingressStates[5].ing, ingressStates[5].svcPorts),
				NewIngressState(ingressStates[6].ing, ingressStates[6].svcPorts),
				NewIngressState(ingressStates[7].ing, ingressStates[7].svcPorts),
				NewIngressState(ingressStates[8].ing, ingressStates[8].svcPorts),
				NewIngressState(ingressStates[9].ing, ingressStates[9].svcPorts),
				NewIngressState(ingressStates[10].ing, ingressStates[10].svcPorts),
				NewIngressState(ingressStates[11].ing, ingressStates[11].svcPorts),
				NewIngressState(ingressStates[12].ing, ingressStates[12].svcPorts),
				NewIngressState(ingressStates[13].ing, ingressStates[13].svcPorts),
			},
			map[feature]int{
				backendConnectionDraining: 7,
				backendTimeout:            3,
				clientIPAffinity:          3,
				cloudArmor:                6,
				cloudCDN:                  6,
				cloudIAP:                  4,
				cookieAffinity:            7,
				customRequestHeaders:      3,
				externalIngress:           13,
				httpEnabled:               13,
				hostBasedRouting:          5,
				ingress:                   14,
				internalIngress:           1,
				managedCertsForTLS:        2,
				managedStaticGlobalIP:     1,
				neg:                       4,
				pathBasedRouting:          4,
				preSharedCertsForTLS:      4,
				secretBasedCertsForTLS:    1,
				specifiedStaticGlobalIP:   1,
				staticGlobalIP:            2,
				tlsTermination:            5,
			},
			map[feature]int{
				backendConnectionDraining: 2,
				backendTimeout:            1,
				clientIPAffinity:          1,
				cloudArmor:                1,
				cloudCDN:                  1,
				cloudIAP:                  2,
				cookieAffinity:            2,
				customRequestHeaders:      1,
				internalServicePort:       2,
				servicePort:               4,
				externalServicePort:       2,
				neg:                       3,
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			newMetrics := NewControllerMetrics()
			for _, ingState := range tc.ingressStates {
				ingKey := fmt.Sprintf("%s/%s", defaultNamespace, ingState.ingress.Name)
				newMetrics.SetIngress(ingKey, ingState)
			}
			gotIngressCount, gotSvcPortCount := newMetrics.computeIngressMetrics()
			if diff := cmp.Diff(tc.expectIngressCount, gotIngressCount); diff != "" {
				t.Errorf("Got diff for ingress features count (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.expectSvcPortCount, gotSvcPortCount); diff != "" {
				t.Fatalf("Got diff for service port features count (-want +got):\n%s", diff)
			}
		})
	}
}

func TestComputeNegMetrics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc           string
		negStates      []NegServiceState
		expectNegCount map[feature]int
	}{
		{
			"empty input",
			[]NegServiceState{},
			map[feature]int{
				standaloneNeg:  0,
				ingressNeg:     0,
				asmNeg:         0,
				neg:            0,
				vmIpNeg:        0,
				vmIpNegLocal:   0,
				vmIpNegCluster: 0,
			},
		},
		{
			"one neg service",
			[]NegServiceState{
				newNegState(0, 0, 1, nil),
			},
			map[feature]int{
				standaloneNeg:  0,
				ingressNeg:     0,
				asmNeg:         1,
				neg:            1,
				vmIpNeg:        0,
				vmIpNegLocal:   0,
				vmIpNegCluster: 0,
			},
		},
		{
			"vm primary ip neg in traffic policy cluster mode",
			[]NegServiceState{
				newNegState(0, 0, 1, &VmIpNegType{trafficPolicyLocal: false}),
			},
			map[feature]int{
				standaloneNeg:  0,
				ingressNeg:     0,
				asmNeg:         1,
				neg:            2,
				vmIpNeg:        1,
				vmIpNegLocal:   0,
				vmIpNegCluster: 1,
			},
		},
		{
			"many neg services",
			[]NegServiceState{
				newNegState(0, 0, 1, nil),
				newNegState(0, 1, 0, &VmIpNegType{trafficPolicyLocal: false}),
				newNegState(5, 0, 0, &VmIpNegType{trafficPolicyLocal: true}),
				newNegState(5, 3, 2, nil),
			},
			map[feature]int{
				standaloneNeg:  10,
				ingressNeg:     4,
				asmNeg:         3,
				neg:            19,
				vmIpNeg:        2,
				vmIpNegLocal:   1,
				vmIpNegCluster: 1,
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			newMetrics := NewControllerMetrics()
			for i, negState := range tc.negStates {
				newMetrics.SetNegService(string(i), negState)
			}

			gotNegCount := newMetrics.computeNegMetrics()
			if diff := cmp.Diff(tc.expectNegCount, gotNegCount); diff != "" {
				t.Errorf("Got diff for NEG counts (-want +got):\n%s", diff)
			}
		})
	}
}

func newNegState(standalone, ingress, asm int, negType *VmIpNegType) NegServiceState {
	return NegServiceState{
		IngressNeg:    ingress,
		StandaloneNeg: standalone,
		AsmNeg:        asm,
		VmIpNeg:       negType,
	}
}

func TestComputeL4ILBMetrics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc             string
		serviceStates    []L4ILBServiceState
		expectL4ILBCount map[feature]int
	}{
		{
			desc:          "empty input",
			serviceStates: []L4ILBServiceState{},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      0,
				l4IlbGlobalAccess: 0,
				l4IlbCustomSubnet: 0,
			},
		},
		{
			desc: "one l4 ilb service",
			serviceStates: []L4ILBServiceState{
				newL4IlbServiceState(false, false),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      1,
				l4IlbGlobalAccess: 0,
				l4IlbCustomSubnet: 0,
			},
		},
		{
			desc: "global access for l4 ilb service enabled",
			serviceStates: []L4ILBServiceState{
				newL4IlbServiceState(true, false),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      1,
				l4IlbGlobalAccess: 1,
				l4IlbCustomSubnet: 0,
			},
		},
		{
			desc: "custom subnet for l4 ilb service enabled",
			serviceStates: []L4ILBServiceState{
				newL4IlbServiceState(false, true),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      1,
				l4IlbGlobalAccess: 0,
				l4IlbCustomSubnet: 1,
			},
		},
		{
			desc: "both global access and custom subnet for l4 ilb service enabled",
			serviceStates: []L4ILBServiceState{
				newL4IlbServiceState(true, true),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      1,
				l4IlbGlobalAccess: 1,
				l4IlbCustomSubnet: 1,
			},
		},
		{
			desc: "many l4 ilb services",
			serviceStates: []L4ILBServiceState{
				newL4IlbServiceState(false, false),
				newL4IlbServiceState(false, true),
				newL4IlbServiceState(true, false),
				newL4IlbServiceState(true, true),
			},
			expectL4ILBCount: map[feature]int{
				l4ILBService:      4,
				l4IlbGlobalAccess: 2,
				l4IlbCustomSubnet: 2,
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			newMetrics := NewControllerMetrics()
			for i, serviceState := range tc.serviceStates {
				newMetrics.SetL4ILBService(string(i), serviceState)
			}
			got := newMetrics.computeL4ILBMetrics()
			if diff := cmp.Diff(tc.expectL4ILBCount, got); diff != "" {
				t.Fatalf("Got diff for L4 ILB service counts (-want +got):\n%s", diff)
			}
		})
	}
}

func newL4IlbServiceState(globalAccess, customSubnet bool) L4ILBServiceState {
	return L4ILBServiceState{
		EnabledGlobalAccess: globalAccess,
		EnabledCustomSubnet: customSubnet,
	}
}
