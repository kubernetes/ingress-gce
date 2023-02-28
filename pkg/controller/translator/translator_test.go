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

package translator

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"reflect"
	"testing"
	"time"

	apiv1 "k8s.io/api/core/v1"
	discoveryapi "k8s.io/api/discovery/v1"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	informerv1 "k8s.io/client-go/informers/core/v1"
	discoveryinformer "k8s.io/client-go/informers/discovery/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	informerbackendconfig "k8s.io/ingress-gce/pkg/backendconfig/client/informers/externalversions/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/endpointslices"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
)

var (
	firstPodCreationTime = time.Date(2006, 01, 02, 15, 04, 05, 0, time.UTC)
	defaultBackend       = utils.ServicePort{
		ID: utils.ServicePortID{
			Service: types.NamespacedName{
				Name:      "default-http-backend",
				Namespace: "kube-system",
			},
			Port: v1.ServiceBackendPort{Name: "http"},
		},
		TargetPort: intstr.FromInt(9376),
	}
	defaultNamer = namer_util.NewNamer("uid1", "")
	port80       = v1.ServiceBackendPort{Number: 80}
)

func fakeTranslator() *Translator {
	return configuredFakeTranslator()
}

func configuredFakeTranslator() *Translator {
	client := fake.NewSimpleClientset()
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	namespace := apiv1.NamespaceAll
	resyncPeriod := 1 * time.Second

	ServiceInformer := informerv1.NewServiceInformer(client, namespace, resyncPeriod, utils.NewNamespaceIndexer())
	BackendConfigInformer := informerbackendconfig.NewBackendConfigInformer(backendConfigClient, namespace, resyncPeriod, utils.NewNamespaceIndexer())
	PodInformer := informerv1.NewPodInformer(client, namespace, resyncPeriod, utils.NewNamespaceIndexer())
	NodeInformer := informerv1.NewNodeInformer(client, resyncPeriod, utils.NewNamespaceIndexer())
	EndpointSliceInformer := discoveryinformer.NewEndpointSliceInformer(client, namespace, 0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc, endpointslices.EndpointSlicesByServiceIndex: endpointslices.EndpointSlicesByServiceFunc})
	return NewTranslator(
		ServiceInformer,
		BackendConfigInformer,
		NodeInformer,
		PodInformer,
		EndpointSliceInformer,
		client,
	)
}

func TestTranslateIngress(t *testing.T) {
	translator := fakeTranslator()
	svcLister := translator.ServiceInformer.GetIndexer()

	// default backend
	svc := test.NewService(types.NamespacedName{Name: "default-http-backend", Namespace: "kube-system"}, apiv1.ServiceSpec{
		Type:  apiv1.ServiceTypeNodePort,
		Ports: []apiv1.ServicePort{{Name: "http", Port: 80}},
	})
	svcLister.Add(svc)

	// first-service
	svc = test.NewService(types.NamespacedName{Name: "first-service", Namespace: "default"}, apiv1.ServiceSpec{
		Type:  apiv1.ServiceTypeNodePort,
		Ports: []apiv1.ServicePort{{Port: 80}},
	})
	svcLister.Add(svc)

	// other-service
	svc = test.NewService(types.NamespacedName{Name: "second-service", Namespace: "default"}, apiv1.ServiceSpec{
		Type:  apiv1.ServiceTypeNodePort,
		Ports: []apiv1.ServicePort{{Port: 80}},
	})
	svcLister.Add(svc)

	cases := []struct {
		desc          string
		ing           *v1.Ingress
		wantErrCount  int
		wantGCEURLMap *utils.GCEURLMap
	}{
		{
			desc: "default backend only",
			ing: test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
				v1.IngressSpec{
					DefaultBackend: test.Backend("first-service", port80),
				}),
			wantErrCount:  0,
			wantGCEURLMap: &utils.GCEURLMap{DefaultBackend: &utils.ServicePort{ID: utils.ServicePortID{Service: types.NamespacedName{Name: "first-service", Namespace: "default"}, Port: port80}}},
		},
		{
			desc:          "no backend",
			ing:           test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"}, v1.IngressSpec{}),
			wantErrCount:  0,
			wantGCEURLMap: &utils.GCEURLMap{DefaultBackend: &utils.ServicePort{ID: utils.ServicePortID{Service: types.NamespacedName{Name: "default-http-backend", Namespace: "kube-system"}, Port: v1.ServiceBackendPort{Name: "http"}}}},
		},
		{
			desc:          "no host",
			ing:           ingressFromFile(t, "ingress-no-host.yaml"),
			wantErrCount:  0,
			wantGCEURLMap: gceURLMapFromFile(t, "ingress-no-host.json"),
		},
		{
			desc:          "single host",
			ing:           ingressFromFile(t, "ingress-single-host.yaml"),
			wantErrCount:  0,
			wantGCEURLMap: gceURLMapFromFile(t, "ingress-single-host.json"),
		},
		{
			desc:          "two hosts",
			ing:           ingressFromFile(t, "ingress-two-hosts.yaml"),
			wantErrCount:  0,
			wantGCEURLMap: gceURLMapFromFile(t, "ingress-two-hosts.json"),
		},
		{
			desc:          "multiple paths",
			ing:           ingressFromFile(t, "ingress-multi-paths.yaml"),
			wantErrCount:  0,
			wantGCEURLMap: gceURLMapFromFile(t, "ingress-multi-paths.json"),
		},
		{
			desc:          "multiple empty paths",
			ing:           ingressFromFile(t, "ingress-multi-empty.yaml"),
			wantErrCount:  0,
			wantGCEURLMap: gceURLMapFromFile(t, "ingress-multi-empty.json"),
		},
		{
			desc:          "missing rule service",
			ing:           ingressFromFile(t, "ingress-missing-rule-svc.yaml"),
			wantErrCount:  1,
			wantGCEURLMap: gceURLMapFromFile(t, "ingress-missing-rule-svc.json"),
		},
		{
			desc:          "missing multiple rule service",
			ing:           ingressFromFile(t, "ingress-missing-multi-svc.yaml"),
			wantErrCount:  2,
			wantGCEURLMap: gceURLMapFromFile(t, "ingress-missing-multi-svc.json"),
		},
		{
			desc: "missing default service",
			ing: test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
				v1.IngressSpec{
					DefaultBackend: test.Backend("random-service", port80),
				}),
			wantErrCount:  1,
			wantGCEURLMap: utils.NewGCEURLMap(),
		},
		{
			desc: "null service default backend",
			ing: test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
				v1.IngressSpec{
					DefaultBackend: &v1.IngressBackend{},
				}),
			wantErrCount:  1,
			wantGCEURLMap: utils.NewGCEURLMap(),
		},
		{
			desc:          "null service backend",
			ing:           ingressFromFile(t, "ingress-null-service-backend.yaml"),
			wantErrCount:  1,
			wantGCEURLMap: gceURLMapFromFile(t, "ingress-null-service-backend.json"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			gotGCEURLMap, gotErrs := translator.TranslateIngress(tc.ing, defaultBackend.ID, defaultNamer)
			if len(gotErrs) != tc.wantErrCount {
				t.Errorf("%s: TranslateIngress() = _, %+v, want %v errs", tc.desc, gotErrs, tc.wantErrCount)
			}

			// Check that the GCEURLMaps point to the same ServicePortIDs.
			if !utils.EqualMapping(gotGCEURLMap, tc.wantGCEURLMap) {
				t.Errorf("%s: TranslateIngress() = %+v\nwant\n%+v", tc.desc, gotGCEURLMap.String(), tc.wantGCEURLMap.String())
			}
		})
	}
}

func TestGetServicePort(t *testing.T) {
	cases := []struct {
		desc        string
		spec        apiv1.ServiceSpec
		annotations map[string]string
		id          utils.ServicePortID
		wantErr     bool
		wantPort    bool
		params      getServicePortParams
		wantedPort  apiv1.ServicePort
	}{
		{
			desc: "clusterIP service",
			spec: apiv1.ServiceSpec{
				Type:  apiv1.ServiceTypeClusterIP,
				Ports: []apiv1.ServicePort{{Name: "http", Port: 80}},
			},
			id:         utils.ServicePortID{Port: v1.ServiceBackendPort{Name: "http"}},
			wantErr:    true,
			wantPort:   false,
			wantedPort: apiv1.ServicePort{Name: "http", Port: 80},
		},
		{
			desc: "missing port",
			spec: apiv1.ServiceSpec{
				Type:  apiv1.ServiceTypeNodePort,
				Ports: []apiv1.ServicePort{{Name: "http", Port: 80}},
			},
			id:         utils.ServicePortID{Port: v1.ServiceBackendPort{Name: "badport"}},
			wantErr:    true,
			wantPort:   false,
			wantedPort: apiv1.ServicePort{Name: "http", Port: 80},
		},
		{
			desc: "app protocols malformed",
			spec: apiv1.ServiceSpec{
				Type:  apiv1.ServiceTypeNodePort,
				Ports: []apiv1.ServicePort{{Name: "http", Port: 80}},
			},
			annotations: map[string]string{
				"service.alpha.kubernetes.io/app-protocols": "bad-string",
			},
			id:         utils.ServicePortID{Port: v1.ServiceBackendPort{Name: "http"}},
			wantErr:    true,
			wantPort:   true,
			wantedPort: apiv1.ServicePort{Name: "http", Port: 80},
		},
		{
			desc: "find port by name",
			spec: apiv1.ServiceSpec{
				Type: apiv1.ServiceTypeNodePort,
				Ports: []apiv1.ServicePort{
					{Name: "http", Port: 80},
					{Name: "https", Port: 443},
					{Name: "otherPort", Port: 12345},
				},
			},
			id:         utils.ServicePortID{Port: v1.ServiceBackendPort{Name: "https"}},
			wantErr:    false,
			wantPort:   true,
			wantedPort: apiv1.ServicePort{Name: "https", Port: 443},
		},
		{
			desc: "find port by number",
			spec: apiv1.ServiceSpec{
				Type: apiv1.ServiceTypeNodePort,
				Ports: []apiv1.ServicePort{
					{Name: "http", Port: 80},
					{Name: "https", Port: 443},
					{Name: "otherPort", Port: 12345},
				},
			},
			id:         utils.ServicePortID{Port: v1.ServiceBackendPort{Number: 443}},
			wantErr:    false,
			wantPort:   true,
			wantedPort: apiv1.ServicePort{Name: "https", Port: 443},
		},
		{
			desc: "correct port spec",
			spec: apiv1.ServiceSpec{
				Type: apiv1.ServiceTypeNodePort,
				Ports: []apiv1.ServicePort{
					{Name: "http", Port: 80, NodePort: 123, TargetPort: intstr.FromString("podport")},
				},
			},
			id:         utils.ServicePortID{Port: v1.ServiceBackendPort{Number: 80}},
			wantErr:    false,
			wantPort:   true,
			wantedPort: apiv1.ServicePort{Name: "http", Port: 80, NodePort: 123, TargetPort: intstr.FromString("podport")},
		},
		{
			desc: "handle duplicated target port name by name",
			spec: apiv1.ServiceSpec{
				Type: apiv1.ServiceTypeNodePort,
				Ports: []apiv1.ServicePort{
					{Name: "http", Port: 80, TargetPort: intstr.FromString("pod-http")},
					{Name: "https", Port: 443, TargetPort: intstr.FromString("pod-https")},
					{Name: "otherPort", Port: 12345, TargetPort: intstr.FromString("pod-otherPort")},
				},
			},
			id:         utils.ServicePortID{Port: v1.ServiceBackendPort{Name: "https"}},
			wantErr:    false,
			wantPort:   true,
			wantedPort: apiv1.ServicePort{Name: "https", Port: 443, TargetPort: intstr.FromString("pod-https")},
		},
		{
			desc: "handle duplicated target port name by number",
			spec: apiv1.ServiceSpec{
				Type: apiv1.ServiceTypeNodePort,
				Ports: []apiv1.ServicePort{
					{Name: "http", Port: 80, TargetPort: intstr.FromString("pod-http")},
					{Name: "https", Port: 443, TargetPort: intstr.FromString("pod-https")},
					{Name: "otherPort", Port: 12345, TargetPort: intstr.FromString("pod-otherPort")},
				},
			},
			id:         utils.ServicePortID{Port: v1.ServiceBackendPort{Number: 443}},
			wantErr:    false,
			wantPort:   true,
			wantedPort: apiv1.ServicePort{Name: "https", Port: 443, TargetPort: intstr.FromString("pod-https")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			translator := fakeTranslator()
			svcLister := translator.ServiceInformer.GetIndexer()

			svcName := types.NamespacedName{Name: "foo", Namespace: "default"}
			svc := test.NewService(svcName, tc.spec)
			svc.Annotations = tc.annotations
			svcLister.Add(svc)
			tc.id.Service = svcName

			port, gotErr := translator.getServicePort(tc.id, &tc.params, defaultNamer)
			if (gotErr != nil) != tc.wantErr {
				t.Errorf("translator.getServicePort(%+v) = _, %v, want err? %v", tc.id, gotErr, tc.wantErr)
			}
			if (port != nil) != tc.wantPort {
				t.Errorf("translator.getServicePort(%+v) = %v, want port? %v", tc.id, port, tc.wantPort)
			}
			if tc.wantPort && port != nil {
				if port.Port != tc.wantedPort.Port {
					t.Errorf("Expected port.Port %d, got %d", port.Port, tc.wantedPort.Port)
				}
				if port.NodePort != int64(tc.wantedPort.NodePort) {
					t.Errorf("Expected port.NodePort %d, got %d", port.NodePort, tc.wantedPort.NodePort)
				}
				if port.PortName != tc.wantedPort.Name {
					t.Errorf("Expected port.PortName %s, got %s", port.PortName, tc.wantedPort.Name)
				}
				if port.TargetPort != tc.wantedPort.TargetPort {
					t.Errorf("Expected port.TargetPort %v, got %v", port.TargetPort, tc.wantedPort.TargetPort)
				}
			}
		})
	}
}

func TestGetServicePortWithBackendConfigEnabled(t *testing.T) {
	backendConfig := test.NewBackendConfig(types.NamespacedName{Name: "config-http", Namespace: "default"}, backendconfig.BackendConfigSpec{
		Cdn: &backendconfig.CDNConfig{
			Enabled: true,
		},
	})

	testCases := []struct {
		desc        string
		annotations map[string]string
		id          utils.ServicePortID
		wantErr     bool
		wantPort    bool
		params      getServicePortParams
	}{
		{
			desc: "error getting backend config",
			annotations: map[string]string{
				annotations.BackendConfigKey: `{"ports":{"https":"config-https"}}`,
			},
			id:       utils.ServicePortID{Port: v1.ServiceBackendPort{Name: "https"}},
			wantErr:  true,
			wantPort: true,
		},
		{
			desc:        "no backend config annotation",
			annotations: nil,
			id:          utils.ServicePortID{Port: v1.ServiceBackendPort{Name: "http"}},
			wantErr:     false,
			wantPort:    true,
		},
		{
			desc: "no backend config name for port",
			annotations: map[string]string{
				annotations.BackendConfigKey: `{"ports":{"https":"config-https"}}`,
			},
			id:       utils.ServicePortID{Port: v1.ServiceBackendPort{Name: "http"}},
			wantErr:  false,
			wantPort: true,
		},
		{
			desc: "successfully got backend config for port",
			annotations: map[string]string{
				annotations.BackendConfigKey: `{"ports":{"http":"config-http"}}`,
			},
			id:       utils.ServicePortID{Port: v1.ServiceBackendPort{Name: "http"}},
			wantErr:  false,
			wantPort: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			translator := fakeTranslator()
			svcLister := translator.ServiceInformer.GetIndexer()
			backendConfigLister := translator.BackendConfigInformer.GetIndexer()
			svcName := types.NamespacedName{Name: "foo", Namespace: "default"}
			svc := test.NewService(svcName, apiv1.ServiceSpec{
				Type:  apiv1.ServiceTypeNodePort,
				Ports: []apiv1.ServicePort{{Name: "http", Port: 80}, {Name: "https", Port: 443}},
			})
			tc.id.Service = svcName
			svc.Annotations = tc.annotations

			svcLister.Add(svc)
			backendConfigLister.Add(backendConfig)

			port, gotErr := translator.getServicePort(tc.id, &tc.params, defaultNamer)
			if (gotErr != nil) != tc.wantErr {
				t.Errorf("%s: translator.getServicePort(%+v) = _, %v, want err? %v", tc.desc, tc.id, gotErr, tc.wantErr)
			}
			if (port != nil) != tc.wantPort {
				t.Errorf("%s: translator.getServicePort(%+v) = %v, want port? %v", tc.desc, tc.id, port, tc.wantPort)
			}
		})
	}
}

func TestGetProbe(t *testing.T) {
	translator := fakeTranslator()
	nodePortToHealthCheck := map[utils.ServicePort]string{
		{NodePort: 3001, Protocol: annotations.ProtocolHTTP}:  "/healthz",
		{NodePort: 3002, Protocol: annotations.ProtocolHTTPS}: "/foo",
		{NodePort: 3003, Protocol: annotations.ProtocolHTTP2}: "/http2-check",
		{NodePort: 0, Protocol: annotations.ProtocolHTTPS, NEGEnabled: true,
			ID: utils.ServicePortID{Service: types.NamespacedName{Name: "svc0", Namespace: apiv1.NamespaceDefault}}}: "/bar",
	}
	for _, svc := range makeServices(nodePortToHealthCheck, apiv1.NamespaceDefault) {
		translator.ServiceInformer.GetIndexer().Add(svc)
	}
	for _, pod := range makePods(nodePortToHealthCheck, apiv1.NamespaceDefault) {
		translator.PodInformer.GetIndexer().Add(pod)
	}

	for p, exp := range nodePortToHealthCheck {
		got, err := translator.GetProbe(p)
		if err != nil || got == nil {
			t.Errorf("Failed to get probe for node port %v: %v", p, err)
		} else if getProbePath(got) != exp {
			t.Errorf("Wrong path for node port %v, got %v expected %v", p, getProbePath(got), exp)
		}
	}
}

func TestGetProbeNamedPort(t *testing.T) {
	translator := fakeTranslator()
	nodePortToHealthCheck := map[utils.ServicePort]string{
		{NodePort: 3001, Protocol: annotations.ProtocolHTTP}: "/healthz",
	}
	for _, svc := range makeServices(nodePortToHealthCheck, apiv1.NamespaceDefault) {
		translator.ServiceInformer.GetIndexer().Add(svc)
	}
	for _, pod := range makePods(nodePortToHealthCheck, apiv1.NamespaceDefault) {
		pod.Spec.Containers[0].Ports[0].Name = "test"
		pod.Spec.Containers[0].ReadinessProbe.ProbeHandler.HTTPGet.Port = intstr.IntOrString{Type: intstr.String, StrVal: "test"}
		translator.PodInformer.GetIndexer().Add(pod)
	}
	for p, exp := range nodePortToHealthCheck {
		got, err := translator.GetProbe(p)
		if err != nil || got == nil {
			t.Errorf("Failed to get probe for node port %v: %v", p, err)
		} else if getProbePath(got) != exp {
			t.Errorf("Wrong path for node port %v, got %v expected %v", p, getProbePath(got), exp)
		}
	}
}

func TestGetProbeCrossNamespace(t *testing.T) {
	translator := fakeTranslator()

	firstPod := &apiv1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			// labels match those added by "addPods", but ns and health check
			// path is different. If this pod was created in the same ns, it
			// would become the health check.
			Labels:            map[string]string{"app-3001": "test"},
			Name:              fmt.Sprintf("test-pod-new-ns"),
			Namespace:         "new-ns",
			CreationTimestamp: metav1.NewTime(firstPodCreationTime.Add(-time.Duration(time.Hour))),
		},
		Spec: apiv1.PodSpec{
			Containers: []apiv1.Container{
				{
					Ports: []apiv1.ContainerPort{{ContainerPort: 80}},
					ReadinessProbe: &apiv1.Probe{
						ProbeHandler: apiv1.ProbeHandler{
							HTTPGet: &apiv1.HTTPGetAction{
								Scheme: apiv1.URISchemeHTTP,
								Path:   "/badpath",
								Port: intstr.IntOrString{
									Type:   intstr.Int,
									IntVal: 80,
								},
							},
						},
					},
				},
			},
		},
	}
	translator.PodInformer.GetIndexer().Add(firstPod)
	nodePortToHealthCheck := map[utils.ServicePort]string{
		{NodePort: 3001, Protocol: annotations.ProtocolHTTP}: "/healthz",
	}
	for _, svc := range makeServices(nodePortToHealthCheck, apiv1.NamespaceDefault) {
		translator.ServiceInformer.GetIndexer().Add(svc)
	}
	for _, pod := range makePods(nodePortToHealthCheck, apiv1.NamespaceDefault) {
		pod.Spec.Containers[0].Ports[0].Name = "test"
		pod.Spec.Containers[0].ReadinessProbe.ProbeHandler.HTTPGet.Port = intstr.IntOrString{Type: intstr.String, StrVal: "test"}
		translator.PodInformer.GetIndexer().Add(pod)
	}

	for p, exp := range nodePortToHealthCheck {
		got, err := translator.GetProbe(p)
		if err != nil || got == nil {
			t.Errorf("Failed to get probe for node port %v: %v", p, err)
		} else if getProbePath(got) != exp {
			t.Errorf("Wrong path for node port %v, got %v expected %v", p, getProbePath(got), exp)
		}
	}
}

func TestPathValidation(t *testing.T) {
	hostname := "foo.bar.com"
	translator := fakeTranslator()
	svcLister := translator.ServiceInformer.GetIndexer()

	// default backend
	svc := test.NewService(types.NamespacedName{Name: "default-http-backend", Namespace: "kube-system"}, apiv1.ServiceSpec{
		Type:  apiv1.ServiceTypeNodePort,
		Ports: []apiv1.ServicePort{{Name: "http", Port: 80}},
	})
	svcLister.Add(svc)

	svc = test.NewService(types.NamespacedName{Name: "my-service", Namespace: "default"}, apiv1.ServiceSpec{
		Type:  apiv1.ServiceTypeNodePort,
		Ports: []apiv1.ServicePort{{Port: 80}},
	})
	svcLister.Add(svc)

	expectedBackend := &utils.ServicePort{
		ID: utils.ServicePortID{
			Service: types.NamespacedName{Name: "my-service", Namespace: "default"},
			Port:    port80,
		}}

	testcases := []struct {
		desc              string
		pathType          v1.PathType
		path              string
		expectValid       bool
		expectedPaths     []string
		ingressGADisabled bool
	}{
		{
			desc:          "Valid path for exact path type",
			pathType:      v1.PathTypeExact,
			path:          "/test",
			expectValid:   true,
			expectedPaths: []string{"/test"},
		},
		{
			desc:          "Valid path for exact path type with trailing '/'",
			pathType:      v1.PathTypeExact,
			path:          "/test/",
			expectValid:   true,
			expectedPaths: []string{"/test/"},
		},
		{
			desc:        "Invalid Exact Path includes a wildcard",
			pathType:    v1.PathTypeExact,
			path:        "/test/*",
			expectValid: false,
		},
		{
			desc:        "Invalid empty Exact Path",
			pathType:    v1.PathTypeExact,
			path:        "",
			expectValid: false,
		},
		{
			desc:          "Valid Prefix path without wildcard",
			pathType:      v1.PathTypePrefix,
			path:          "/test",
			expectValid:   true,
			expectedPaths: []string{"/test", "/test/*"},
		},
		{
			desc:          "Valid Prefix path with trailing /",
			pathType:      v1.PathTypePrefix,
			path:          "/test/",
			expectValid:   true,
			expectedPaths: []string{"/test", "/test/*"},
		},
		{
			desc:          "Valid Prefix path /",
			pathType:      v1.PathTypePrefix,
			path:          "/",
			expectValid:   true,
			expectedPaths: []string{"/*"},
		},
		{
			desc:        "Invalid Prefix path with a wildcard",
			pathType:    v1.PathTypePrefix,
			path:        "/test/*",
			expectValid: false,
		},
		{
			desc:        "Invalid Prefix empty path",
			pathType:    v1.PathTypePrefix,
			path:        "",
			expectValid: false,
		},
		{
			desc:          "Valid ImplementationSpecific path",
			pathType:      v1.PathTypeImplementationSpecific,
			path:          "/test",
			expectValid:   true,
			expectedPaths: []string{"/test"},
		},
		{
			desc:          "Empty path type, valid path type",
			pathType:      "",
			path:          "/test",
			expectValid:   true,
			expectedPaths: []string{"/test"},
		},
		{
			desc:          "Empty path type valid path without wildcard",
			pathType:      "",
			path:          "/test",
			expectValid:   true,
			expectedPaths: []string{"/test"},
		},
		{
			desc:          "Empty path type valid path with wildcard",
			pathType:      "",
			path:          "/test/*",
			expectValid:   true,
			expectedPaths: []string{"/test/*"},
		},
		{
			desc:          "Empty path type valid empty path",
			pathType:      "",
			path:          "",
			expectValid:   true,
			expectedPaths: []string{"/*"},
		},
		{
			desc:        "Invalid Path Type",
			pathType:    v1.PathType("InvalidType"),
			path:        "/test/*",
			expectValid: false,
		},
	}

	for _, tc := range testcases {
		path := v1.HTTPIngressPath{
			Path: tc.path,
			Backend: v1.IngressBackend{
				Service: &v1.IngressServiceBackend{
					Name: "my-service",
					Port: v1.ServiceBackendPort{
						Number: 80,
					},
				},
			},
		}

		// Empty Path Types should be nil, not an empty string in the spec
		if tc.pathType != v1.PathType("") {
			path.PathType = &tc.pathType
		}

		spec := v1.IngressSpec{
			Rules: []v1.IngressRule{
				{
					Host: hostname,
					IngressRuleValue: v1.IngressRuleValue{
						HTTP: &v1.HTTPIngressRuleValue{
							Paths: []v1.HTTPIngressPath{path},
						},
					},
				},
			},
		}
		ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"}, spec)

		expectedGCEURLMap := &utils.GCEURLMap{
			DefaultBackend: &utils.ServicePort{
				ID: utils.ServicePortID{Service: types.NamespacedName{Name: "default-http-backend", Namespace: "kube-system"}, Port: v1.ServiceBackendPort{Name: "http"}},
			},
		}

		var expectedPathRules []utils.PathRule
		for _, p := range tc.expectedPaths {
			pathRule := utils.PathRule{Path: p, Backend: *expectedBackend}
			expectedPathRules = append(expectedPathRules, pathRule)
		}
		expectedGCEURLMap.HostRules = []utils.HostRule{{Hostname: hostname, Paths: expectedPathRules}}

		gotGCEURLMap, gotErrs := translator.TranslateIngress(ing, defaultBackend.ID, defaultNamer)
		if tc.expectValid && len(gotErrs) > 0 {
			t.Fatalf("%s: TranslateIngress() = _, %+v, want no errs", tc.desc, gotErrs)
		} else if !tc.expectValid && len(gotErrs) == 0 {
			t.Errorf("%s: TranslateIngress() should result in errors but got none", tc.desc)
		}

		// Check that the GCEURLMaps have expected host path rules
		if tc.expectValid && !utils.EqualMapping(gotGCEURLMap, expectedGCEURLMap) {
			t.Errorf("%s: TranslateIngress() = %+v\nwant\n%+v", tc.desc, gotGCEURLMap.String(), expectedGCEURLMap.String())
		}
	}
}

func makePods(nodePortToHealthCheck map[utils.ServicePort]string, ns string) []*apiv1.Pod {
	delay := 1 * time.Minute

	var pods []*apiv1.Pod
	for np, u := range nodePortToHealthCheck {
		pod := &apiv1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels:            map[string]string{fmt.Sprintf("app-%d", np.NodePort): "test"},
				Name:              fmt.Sprintf("pod%d", np.NodePort),
				Namespace:         ns,
				CreationTimestamp: metav1.NewTime(firstPodCreationTime.Add(delay)),
			},
			Spec: apiv1.PodSpec{
				Containers: []apiv1.Container{
					{
						Ports: []apiv1.ContainerPort{{Name: "test", ContainerPort: 80}},
						ReadinessProbe: &apiv1.Probe{
							ProbeHandler: apiv1.ProbeHandler{
								HTTPGet: &apiv1.HTTPGetAction{
									Scheme: getProbeScheme(np.Protocol),
									Path:   u,
									Port: intstr.IntOrString{
										Type:   intstr.Int,
										IntVal: 80,
									},
								},
							},
						},
					},
				},
			},
		}
		pods = append(pods, pod)
		delay = time.Duration(2) * delay
	}
	return pods
}

func makeServices(nodePortToHealthCheck map[utils.ServicePort]string, ns string) []*apiv1.Service {
	var services []*apiv1.Service
	for np := range nodePortToHealthCheck {
		svc := &apiv1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("svc%d", np.NodePort),
				Namespace: ns,
			},
			Spec: apiv1.ServiceSpec{
				Selector: map[string]string{fmt.Sprintf("app-%d", np.NodePort): "test"},
				Ports: []apiv1.ServicePort{
					{
						NodePort: int32(np.NodePort),
						TargetPort: intstr.IntOrString{
							Type:   intstr.Int,
							IntVal: 80,
						},
					},
				},
			},
		}
		services = append(services, svc)
	}
	return services
}

func getProbePath(p *apiv1.Probe) string {
	return p.ProbeHandler.HTTPGet.Path
}

func TestGatherEndpointPorts(t *testing.T) {
	serviceName1 := "svc"
	serviceName2 := "svc2"
	serviceName3 := "svc3"
	serviceName4 := "svc4"
	emptyPortName := ""
	testPortName := "test-port"
	anotherTestPortName := "another-test-port"
	port6001 := int32(6001)
	port6002 := int32(6002)
	port6003 := int32(6003)
	port7101 := int32(7101)
	port7102 := int32(7102)
	port7103 := int32(7103)
	protocolTcp := apiv1.ProtocolTCP
	testCases := []struct {
		desc           string
		svcPorts       []utils.ServicePort
		endpoints      []*apiv1.Endpoints
		endpointSlices []*discoveryapi.EndpointSlice
		expectedPorts  []string
	}{
		{
			desc:           "Target port is a number so it is used instead",
			svcPorts:       []utils.ServicePort{createSvcPort(serviceName1, "", intstr.FromInt(80), true)},
			endpoints:      []*apiv1.Endpoints{},
			endpointSlices: []*discoveryapi.EndpointSlice{},
			expectedPorts:  []string{"80"},
		},
		{
			desc:           "Target port is not a number so ports is taken from endpoints by port name",
			svcPorts:       []utils.ServicePort{createSvcPort(serviceName1, "port", intstr.FromString("some-other-port"), true)},
			endpoints:      []*apiv1.Endpoints{createEndpoints(serviceName1, []apiv1.EndpointSubset{createSubset("port", 8080)})},
			endpointSlices: []*discoveryapi.EndpointSlice{createEndpointSlice(serviceName1, "-1", "port", 8080)},
			expectedPorts:  []string{"8080"},
		},
		{
			desc: "More complex test with multiple endpoints and services",
			svcPorts: []utils.ServicePort{
				createSvcPort(serviceName1, "", intstr.FromInt(80), true),
				createSvcPort(serviceName2, "test-port", intstr.FromString("some-other-port"), true),
				createSvcPort(serviceName3, "", intstr.FromInt(8081), false),
				createSvcPort(serviceName4, "another-test-port", intstr.FromString("some-other-port"), false),
			},
			endpoints: []*apiv1.Endpoints{
				createEndpoints(serviceName1, []apiv1.EndpointSubset{createSubset(emptyPortName, 12345)}),
				createEndpoints(serviceName2, []apiv1.EndpointSubset{
					{
						Ports: []apiv1.EndpointPort{
							{Name: testPortName, Port: 6001, Protocol: apiv1.ProtocolTCP},
							{Name: emptyPortName, Port: 6002, Protocol: apiv1.ProtocolTCP},
							{Name: anotherTestPortName, Port: 6003, Protocol: apiv1.ProtocolTCP},
						},
					},
					createSubset(emptyPortName, 6101),
					createSubset(testPortName, 6201),
				}),
				createEndpoints(serviceName3, []apiv1.EndpointSubset{createSubset(emptyPortName, 23456)}),
				createEndpoints(serviceName4, []apiv1.EndpointSubset{
					{
						Ports: []apiv1.EndpointPort{
							{Name: testPortName, Port: 7001, Protocol: apiv1.ProtocolTCP},
							{Name: emptyPortName, Port: 7002, Protocol: apiv1.ProtocolTCP},
							{Name: anotherTestPortName, Port: 7003, Protocol: apiv1.ProtocolTCP},
						},
					},
					createSubset(emptyPortName, 7101),
					createSubset(anotherTestPortName, 7201),
				}),
			},
			endpointSlices: []*discoveryapi.EndpointSlice{
				createEndpointSlice(serviceName1, "-1", emptyPortName, 12345),
				&discoveryapi.EndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      serviceName2 + "-1",
						Namespace: "ns",
						Labels:    map[string]string{discoveryapi.LabelServiceName: serviceName2},
					},
					Ports: []discoveryapi.EndpointPort{
						{Name: &testPortName, Port: &port6001, Protocol: &protocolTcp},
						{Name: &emptyPortName, Port: &port6002, Protocol: &protocolTcp},
						{Name: &anotherTestPortName, Port: &port6003, Protocol: &protocolTcp},
					},
				},
				createEndpointSlice(serviceName2, "-2", "", 6101),
				createEndpointSlice(serviceName2, "-3", testPortName, 6201),
				createEndpointSlice(serviceName3, "-1", "", 23456),
				&discoveryapi.EndpointSlice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      serviceName4 + "-1",
						Namespace: "ns",
						Labels:    map[string]string{discoveryapi.LabelServiceName: serviceName4},
					},
					Ports: []discoveryapi.EndpointPort{
						{Name: &testPortName, Port: &port7101, Protocol: &protocolTcp},
						{Name: &emptyPortName, Port: &port7102, Protocol: &protocolTcp},
						{Name: &anotherTestPortName, Port: &port7103, Protocol: &protocolTcp},
					},
				},
				createEndpointSlice(serviceName4, "-2", emptyPortName, 7101),
				createEndpointSlice(serviceName4, "-3", anotherTestPortName, 7201),
			},
			expectedPorts: []string{"80", "6001", "6201"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			translator := configuredFakeTranslator()
			// Add endpoints or endpoint slices to informers.
			endpointSliceLister := translator.EndpointSliceInformer.GetIndexer()
			for _, slice := range tc.endpointSlices {
				endpointSliceLister.Add(slice)
			}

			gotPorts := translator.GatherEndpointPorts(tc.svcPorts)
			if !sets.NewString(gotPorts...).Equal(sets.NewString(tc.expectedPorts...)) {
				t.Errorf("GatherEndpointPorts() = %v, expected %v", gotPorts, tc.expectedPorts)
			}
		})
	}
}

func TestGetZoneForNode(t *testing.T) {
	nodeName := "node"
	zone := "us-central1-a"
	translator := fakeTranslator()
	translator.NodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
			Name:      nodeName,
			Labels: map[string]string{
				annotations.ZoneKey: zone,
			},
		},
		Spec: apiv1.NodeSpec{
			Unschedulable: false,
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionFalse,
				},
			},
		},
	})

	ret, err := translator.GetZoneForNode(nodeName)
	if err != nil {
		t.Errorf("Expect error = nil, but got %v", err)
	}

	if zone != ret {
		t.Errorf("Expect zone = %q, but got %q", zone, ret)
	}
}

func createSvcPort(serviceName string, portName string, targetPort intstr.IntOrString, negEnabled bool) utils.ServicePort {
	return utils.ServicePort{
		ID:         utils.ServicePortID{Service: types.NamespacedName{Namespace: "ns", Name: serviceName}},
		NodePort:   int64(30003),
		NEGEnabled: negEnabled,
		TargetPort: targetPort,
		PortName:   portName,
	}
}

func createEndpoints(serviceName string, subsets []apiv1.EndpointSubset) *apiv1.Endpoints {
	return &apiv1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: "ns"},
		Subsets:    subsets,
	}
}

func createSubset(portName string, port int) apiv1.EndpointSubset {
	return apiv1.EndpointSubset{
		Ports: []apiv1.EndpointPort{
			{
				Name:     portName,
				Port:     int32(port),
				Protocol: apiv1.ProtocolTCP,
			},
		},
	}
}

func createEndpointSlice(serviceName string, sliceSuffix string, portName string, port int) *discoveryapi.EndpointSlice {
	portNumber := int32(port)
	tcpProtocol := apiv1.ProtocolTCP
	return &discoveryapi.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName + sliceSuffix,
			Namespace: "ns",
			Labels:    map[string]string{discoveryapi.LabelServiceName: serviceName},
		},
		Ports: []discoveryapi.EndpointPort{
			{Name: &portName, Port: &portNumber, Protocol: &tcpProtocol},
		},
	}
}

func ingressFromFile(t *testing.T, filename string) *v1.Ingress {
	t.Helper()

	data, err := ioutil.ReadFile("testdata/" + filename)
	if err != nil {
		t.Fatalf("ioutil.ReadFile(%q) = %v", filename, err)
	}
	ing, err := test.DecodeIngress(data)
	if err != nil {
		t.Fatalf("test.DecodeIngress(%q) = %v", filename, err)
	}
	return ing
}

func gceURLMapFromFile(t *testing.T, filename string) *utils.GCEURLMap {
	data, err := ioutil.ReadFile("testdata/" + filename)
	if err != nil {
		t.Fatalf("ioutil.ReadFile(%q) = %v", filename, err)
	}
	v := &utils.GCEURLMap{}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("json.Unmarshal(%q) = %v", filename, err)
	}
	return v
}

func int64ToMap(l []int64) map[int64]bool {
	ret := map[int64]bool{}
	for _, i := range l {
		ret[i] = true
	}
	return ret
}

func TestSetTrafficScaling(t *testing.T) {
	// No t.Parallel()

	oldFlag := flags.F.EnableTrafficScaling
	flags.F.EnableTrafficScaling = true
	defer func() {
		flags.F.EnableTrafficScaling = oldFlag
	}()

	newService := func(ann map[string]string) *apiv1.Service {
		return &apiv1.Service{
			ObjectMeta: metav1.ObjectMeta{Annotations: ann},
		}
	}

	f64 := func(x float64) *float64 { return &x }

	for _, tc := range []struct {
		name    string
		svc     *apiv1.Service
		want    *utils.ServicePort
		wantErr bool
	}{
		{
			name: "no settings",
			svc:  newService(map[string]string{}),
			want: &utils.ServicePort{},
		},
		{
			name: "max-rate-per-endpoint",
			svc: newService(map[string]string{
				"networking.gke.io/max-rate-per-endpoint": "1000",
			}),
			want: &utils.ServicePort{
				MaxRatePerEndpoint: f64(1000),
			},
		},
		{
			name: "capacity-scaler",
			svc: newService(map[string]string{
				"networking.gke.io/capacity-scaler": "0.5",
			}),
			want: &utils.ServicePort{
				CapacityScaler: f64(0.5),
			},
		},
		{
			name: "both",
			svc: newService(map[string]string{
				"networking.gke.io/max-rate-per-endpoint": "999",
				"networking.gke.io/capacity-scaler":       "0.75",
			}),
			want: &utils.ServicePort{
				MaxRatePerEndpoint: f64(999),
				CapacityScaler:     f64(0.75),
			},
		},
		{
			name: "invalid max-rate-per-endpoint (bad parse)",
			svc: newService(map[string]string{
				"networking.gke.io/max-rate-per-endpoint": "abc",
			}),
			wantErr: true,
		},
		{
			name: "invalid max-rate-per-endpoint (< 0)",
			svc: newService(map[string]string{
				"networking.gke.io/max-rate-per-endpoint": "-10",
			}),
			wantErr: true,
		},
		{
			name: "invalid-capacity-scaler (bad parse)",
			svc: newService(map[string]string{
				"networking.gke.io/capacity-scaler": "abc",
			}),
			wantErr: true,
		},
		{
			name: "invalid-capacity-scaler (bad value)",
			svc: newService(map[string]string{
				"networking.gke.io/capacity-scaler": "-1.1",
			}),
			wantErr: true,
		},
		{
			name: "invalid-capacity-scaler (bad value)",
			svc: newService(map[string]string{
				"networking.gke.io/capacity-scaler": "1.1",
			}),
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got utils.ServicePort
			err := setTrafficScaling(&got, tc.svc)
			if gotErr := err != nil; tc.wantErr != gotErr {
				t.Fatalf("setTrafficScaling(_, %+v) = %v; gotErr = %t, want %t", tc.svc, err, gotErr, tc.wantErr)
			}
			if err != nil {
				return
			}
			if !reflect.DeepEqual(got, *tc.want) {
				t.Errorf("setTrafficScaling(_, %+v); got %+v, want %+v", tc.svc, got, *tc.want)
			}
		})
	}
}
