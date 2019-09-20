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
	"testing"
	"time"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
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
			Port: intstr.FromString("http"),
		},
		TargetPort: "9376",
	}
	defaultNamer = namer_util.NewNamer("uid1", "")
)

func fakeTranslator() *Translator {
	client := fake.NewSimpleClientset()
	backendConfigClient := backendconfigclient.NewSimpleClientset()

	ctxConfig := context.ControllerContextConfig{
		Namespace:                     apiv1.NamespaceAll,
		ResyncPeriod:                  1 * time.Second,
		DefaultBackendSvcPort:         defaultBackend,
		HealthCheckPath:               "/",
		DefaultBackendHealthCheckPath: "/healthz",
	}
	ctx := context.NewControllerContext(client, nil, backendConfigClient, nil, nil, defaultNamer, ctxConfig)
	gce := &Translator{
		ctx: ctx,
	}
	return gce
}

func TestTranslateIngress(t *testing.T) {
	translator := fakeTranslator()
	svcLister := translator.ctx.ServiceInformer.GetIndexer()

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
		ing           *v1beta1.Ingress
		wantErrCount  int
		wantGCEURLMap *utils.GCEURLMap
	}{
		{
			desc: "default backend only",
			ing: test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
				v1beta1.IngressSpec{
					Backend: test.Backend("first-service", intstr.FromInt(80)),
				}),
			wantErrCount:  0,
			wantGCEURLMap: &utils.GCEURLMap{DefaultBackend: &utils.ServicePort{ID: utils.ServicePortID{Service: types.NamespacedName{Name: "first-service", Namespace: "default"}, Port: intstr.FromInt(80)}}},
		},
		{
			desc:          "no backend",
			ing:           test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"}, v1beta1.IngressSpec{}),
			wantErrCount:  0,
			wantGCEURLMap: &utils.GCEURLMap{DefaultBackend: &utils.ServicePort{ID: utils.ServicePortID{Service: types.NamespacedName{Name: "default-http-backend", Namespace: "kube-system"}, Port: intstr.FromString("http")}}},
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
				v1beta1.IngressSpec{
					Backend: test.Backend("random-service", intstr.FromInt(80)),
				}),
			wantErrCount:  1,
			wantGCEURLMap: utils.NewGCEURLMap(),
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
	}{
		{
			desc: "clusterIP service",
			spec: apiv1.ServiceSpec{
				Type:  apiv1.ServiceTypeClusterIP,
				Ports: []apiv1.ServicePort{{Name: "http", Port: 80}},
			},
			id:       utils.ServicePortID{Port: intstr.FromString("http")},
			wantErr:  true,
			wantPort: false,
		},
		{
			desc: "missing port",
			spec: apiv1.ServiceSpec{
				Type:  apiv1.ServiceTypeNodePort,
				Ports: []apiv1.ServicePort{{Name: "http", Port: 80}},
			},
			id:       utils.ServicePortID{Port: intstr.FromString("badport")},
			wantErr:  true,
			wantPort: false,
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
			id:       utils.ServicePortID{Port: intstr.FromString("http")},
			wantErr:  true,
			wantPort: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			translator := fakeTranslator()
			svcLister := translator.ctx.ServiceInformer.GetIndexer()

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
			id:       utils.ServicePortID{Port: intstr.FromString("https")},
			wantErr:  true,
			wantPort: true,
		},
		{
			desc:        "no backend config annotation",
			annotations: nil,
			id:          utils.ServicePortID{Port: intstr.FromString("http")},
			wantErr:     false,
			wantPort:    true,
		},
		{
			desc: "no backend config name for port",
			annotations: map[string]string{
				annotations.BackendConfigKey: `{"ports":{"https":"config-https"}}`,
			},
			id:       utils.ServicePortID{Port: intstr.FromString("http")},
			wantErr:  false,
			wantPort: true,
		},
		{
			desc: "successfully got backend config for port",
			annotations: map[string]string{
				annotations.BackendConfigKey: `{"ports":{"http":"config-http"}}`,
			},
			id:       utils.ServicePortID{Port: intstr.FromString("http")},
			wantErr:  false,
			wantPort: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			translator := fakeTranslator()
			svcLister := translator.ctx.ServiceInformer.GetIndexer()
			backendConfigLister := translator.ctx.BackendConfigInformer.GetIndexer()
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
		translator.ctx.ServiceInformer.GetIndexer().Add(svc)
	}
	for _, pod := range makePods(nodePortToHealthCheck, apiv1.NamespaceDefault) {
		translator.ctx.PodInformer.GetIndexer().Add(pod)
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
		translator.ctx.ServiceInformer.GetIndexer().Add(svc)
	}
	for _, pod := range makePods(nodePortToHealthCheck, apiv1.NamespaceDefault) {
		pod.Spec.Containers[0].Ports[0].Name = "test"
		pod.Spec.Containers[0].ReadinessProbe.Handler.HTTPGet.Port = intstr.IntOrString{Type: intstr.String, StrVal: "test"}
		translator.ctx.PodInformer.GetIndexer().Add(pod)
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
						Handler: apiv1.Handler{
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
	translator.ctx.PodInformer.GetIndexer().Add(firstPod)
	nodePortToHealthCheck := map[utils.ServicePort]string{
		{NodePort: 3001, Protocol: annotations.ProtocolHTTP}: "/healthz",
	}
	for _, svc := range makeServices(nodePortToHealthCheck, apiv1.NamespaceDefault) {
		translator.ctx.ServiceInformer.GetIndexer().Add(svc)
	}
	for _, pod := range makePods(nodePortToHealthCheck, apiv1.NamespaceDefault) {
		pod.Spec.Containers[0].Ports[0].Name = "test"
		pod.Spec.Containers[0].ReadinessProbe.Handler.HTTPGet.Port = intstr.IntOrString{Type: intstr.String, StrVal: "test"}
		translator.ctx.PodInformer.GetIndexer().Add(pod)
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
							Handler: apiv1.Handler{
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
	return p.Handler.HTTPGet.Path
}

func TestGatherEndpointPorts(t *testing.T) {
	translator := fakeTranslator()

	ep1 := "ep1"
	ep2 := "ep2"

	svcPorts := []utils.ServicePort{
		{NodePort: int64(30001)},
		{NodePort: int64(30002)},
		{
			ID:         utils.ServicePortID{Service: types.NamespacedName{Namespace: "ns", Name: ep1}},
			NodePort:   int64(30003),
			NEGEnabled: true,
			TargetPort: "80",
		},
		{
			ID:         utils.ServicePortID{Service: types.NamespacedName{Namespace: "ns", Name: ep2}},
			NodePort:   int64(30004),
			NEGEnabled: true,
			TargetPort: "named-port",
		},
	}

	endpointLister := translator.ctx.EndpointInformer.GetIndexer()
	endpointLister.Add(newDefaultEndpoint(ep1))
	endpointLister.Add(newDefaultEndpoint(ep2))

	expected := []string{"80", "8080", "8081"}
	got := translator.GatherEndpointPorts(svcPorts)
	if !sets.NewString(got...).Equal(sets.NewString(expected...)) {
		t.Errorf("GatherEndpointPorts() = %v, expected %v", got, expected)
	}
}

func TestGetZoneForNode(t *testing.T) {
	nodeName := "node"
	zone := "us-central1-a"
	translator := fakeTranslator()
	translator.ctx.NodeInformer.GetIndexer().Add(&apiv1.Node{
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

func newDefaultEndpoint(name string) *apiv1.Endpoints {
	return &apiv1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
		Subsets: []apiv1.EndpointSubset{
			{
				Ports: []apiv1.EndpointPort{
					{Name: "", Port: int32(80), Protocol: apiv1.ProtocolTCP},
					{Name: "named-port", Port: int32(8080), Protocol: apiv1.ProtocolTCP},
				},
			},
			{
				Ports: []apiv1.EndpointPort{
					{Name: "named-port", Port: int32(80), Protocol: apiv1.ProtocolTCP},
				},
			},
			{
				Ports: []apiv1.EndpointPort{
					{Name: "named-port", Port: int32(8081), Protocol: apiv1.ProtocolTCP},
				},
			},
		},
	}
}

func ingressFromFile(t *testing.T, filename string) *v1beta1.Ingress {
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
