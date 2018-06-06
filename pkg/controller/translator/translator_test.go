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
	"log"
	"testing"
	"time"

	apiv1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/fake"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/test"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/utils"
)

var (
	firstPodCreationTime = time.Date(2006, 01, 02, 15, 04, 05, 0, time.UTC)
	defaultBackend       = utils.ServicePortID{Service: types.NamespacedName{Name: "default-http-backend", Namespace: "kube-system"}, Port: intstr.FromString("http")}
)

func fakeTranslator(negEnabled bool) *Translator {
	client := fake.NewSimpleClientset()
	backendConfigClient := backendconfigclient.NewSimpleClientset()

	namer := utils.NewNamer("uid1", "")
	ctx := context.NewControllerContext(client, backendConfigClient, nil, apiv1.NamespaceAll, 1*time.Second, negEnabled, false)
	gce := &Translator{
		namer: namer,
		ctx:   ctx,
	}
	return gce
}

func TestTranslateIngress(t *testing.T) {
	translator := fakeTranslator(false)

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

	cases := map[string]struct {
		ing           *extensions.Ingress
		wantErrCount  int
		wantGCEURLMap *utils.GCEURLMap
	}{
		"Default Backend Only": {
			ing: test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
				extensions.IngressSpec{
					Backend: test.Backend("first-service", intstr.FromInt(80)),
				}),
			wantErrCount:  0,
			wantGCEURLMap: &utils.GCEURLMap{DefaultBackend: &utils.ServicePort{ID: utils.ServicePortID{Service: types.NamespacedName{Name: "first-service", Namespace: "default"}, Port: intstr.FromInt(80)}}},
		},
		"No Backend": {
			ing:           test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"}, extensions.IngressSpec{}),
			wantErrCount:  0,
			wantGCEURLMap: &utils.GCEURLMap{DefaultBackend: &utils.ServicePort{ID: utils.ServicePortID{Service: types.NamespacedName{Name: "default-http-backend", Namespace: "kube-system"}, Port: intstr.FromString("http")}}},
		},
		"No Host": {
			ing:           ingressFromFile("ingress-no-host.yaml"),
			wantErrCount:  0,
			wantGCEURLMap: gceURLMapFromFile("ingress-no-host.json"),
		},
		"Single Host": {
			ing:           ingressFromFile("ingress-single-host.yaml"),
			wantErrCount:  0,
			wantGCEURLMap: gceURLMapFromFile("ingress-single-host.json"),
		},
		"Two Hosts": {
			ing:           ingressFromFile("ingress-two-hosts.yaml"),
			wantErrCount:  0,
			wantGCEURLMap: gceURLMapFromFile("ingress-two-hosts.json"),
		},
		"Multiple Paths": {
			ing:           ingressFromFile("ingress-multi-paths.yaml"),
			wantErrCount:  0,
			wantGCEURLMap: gceURLMapFromFile("ingress-multi-paths.json"),
		},
		"Multiple Empty Paths": {
			ing:           ingressFromFile("ingress-multi-empty.yaml"),
			wantErrCount:  0,
			wantGCEURLMap: gceURLMapFromFile("ingress-multi-empty.json"),
		},
		"Missing Rule Service": {
			ing:           ingressFromFile("ingress-missing-rule-svc.yaml"),
			wantErrCount:  1,
			wantGCEURLMap: gceURLMapFromFile("ingress-missing-rule-svc.json"),
		},
		"Missing Multiple Rule Service": {
			ing:           ingressFromFile("ingress-missing-multi-svc.yaml"),
			wantErrCount:  2,
			wantGCEURLMap: gceURLMapFromFile("ingress-missing-multi-svc.json"),
		},
		"Missing Default Service": {
			ing: test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
				extensions.IngressSpec{
					Backend: test.Backend("random-service", intstr.FromInt(80)),
				}),
			wantErrCount:  1,
			wantGCEURLMap: utils.NewGCEURLMap(),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gotGCEURLMap, gotErrs := translator.TranslateIngress(tc.ing, defaultBackend)
			if len(gotErrs) != tc.wantErrCount {
				t.Errorf("TranslateIngress() = _, %+v, want %v errs", gotErrs, tc.wantErrCount)
			}

			// Check that the GCEURLMaps point to the same ServicePortIDs.
			if !utils.EqualMapping(gotGCEURLMap, tc.wantGCEURLMap) {
				t.Errorf("TranslateIngress() = %+v\nwant\n%+v", gotGCEURLMap.String(), tc.wantGCEURLMap.String())
			}
		})
	}
}

func TestGetProbe(t *testing.T) {
	translator := fakeTranslator(false)
	nodePortToHealthCheck := map[utils.ServicePort]string{
		{NodePort: 3001, Protocol: annotations.ProtocolHTTP}:  "/healthz",
		{NodePort: 3002, Protocol: annotations.ProtocolHTTPS}: "/foo",
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
	translator := fakeTranslator(false)
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
	translator := fakeTranslator(false)

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
				Name:              fmt.Sprintf("%d", np.NodePort),
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
									Scheme: apiv1.URIScheme(string(np.Protocol)),
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
				Name:      fmt.Sprintf("%d", np.NodePort),
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
	translator := fakeTranslator(true)

	ep1 := "ep1"
	ep2 := "ep2"

	svcPorts := []utils.ServicePort{
		{NodePort: int64(30001)},
		{NodePort: int64(30002)},
		{
			ID:            utils.ServicePortID{Service: types.NamespacedName{Namespace: "ns", Name: ep1}},
			NodePort:      int64(30003),
			NEGEnabled:    true,
			SvcTargetPort: "80",
		},
		{
			ID:            utils.ServicePortID{Service: types.NamespacedName{Namespace: "ns", Name: ep2}},
			NodePort:      int64(30004),
			NEGEnabled:    true,
			SvcTargetPort: "named-port",
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

func ingressFromFile(filename string) *extensions.Ingress {
	data, err := ioutil.ReadFile("testdata/" + filename)
	if err != nil {
		log.Fatal(err)
	}
	ing, err := test.DecodeIngress(data)
	if err != nil {
		log.Fatal(err)
	}
	return ing
}

func gceURLMapFromFile(filename string) *utils.GCEURLMap {
	data, err := ioutil.ReadFile("testdata/" + filename)
	if err != nil {
		log.Fatal(err)
	}
	v := &utils.GCEURLMap{}
	if err := json.Unmarshal(data, v); err != nil {
		log.Fatal(err)
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
