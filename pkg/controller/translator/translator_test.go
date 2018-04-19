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
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/golang/glog"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/fake"
	unversionedcore "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"

	"k8s.io/api/extensions/v1beta1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
)

var (
	firstPodCreationTime = time.Date(2006, 01, 02, 15, 04, 05, 0, time.UTC)
)

func gceForTest(negEnabled bool) *GCE {
	client := fake.NewSimpleClientset()
	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(glog.Infof)
	broadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{
		Interface: client.Core().Events(""),
	})

	namer := utils.NewNamer("uid1", "fw1")

	ctx := context.NewControllerContext(client, nil, apiv1.NamespaceAll, 1*time.Second, negEnabled)
	gce := &GCE{
		recorders:  ctx,
		namer:      namer,
		svcLister:  ctx.ServiceInformer.GetIndexer(),
		nodeLister: ctx.NodeInformer.GetIndexer(),
		podLister:  ctx.PodInformer.GetIndexer(),
		negEnabled: negEnabled,
	}
	if ctx.EndpointInformer != nil {
		gce.endpointLister = ctx.EndpointInformer.GetIndexer()
	}
	return gce
}

func TestToURLMap(t *testing.T) {
	ing, err := test.GetTestIngress("../../test/manifests/ing1.yaml")
	if err != nil {
		t.Fatalf("Error occured when getting test Ingress: %v", err)
	}
	translator := gceForTest(false)
	backendToServicePortMap := map[v1beta1.IngressBackend]backends.ServicePort{
		v1beta1.IngressBackend{ServiceName: "default", ServicePort: intstr.FromInt(80)}: backends.ServicePort{NodePort: 30000},
		v1beta1.IngressBackend{ServiceName: "testy", ServicePort: intstr.FromInt(80)}:   backends.ServicePort{NodePort: 30001},
		v1beta1.IngressBackend{ServiceName: "testx", ServicePort: intstr.FromInt(80)}:   backends.ServicePort{NodePort: 30002},
		v1beta1.IngressBackend{ServiceName: "testz", ServicePort: intstr.FromInt(80)}:   backends.ServicePort{NodePort: 30003},
	}
	expectedUrlMap := utils.GCEURLMap{
		"foo.bar.com": {
			"/foo": translator.namer.Backend(30002),
			"/bar": translator.namer.Backend(30001),
			"/baz": translator.namer.Backend(30003),
		},
	}
	expectedUrlMap.PutDefaultBackendName(translator.namer.Backend(30000))
	urlMap, _ := translator.ToURLMap(&ing, backendToServicePortMap)
	if !reflect.DeepEqual(expectedUrlMap, urlMap) {
		t.Errorf("Result %v does not match expected %v", urlMap, expectedUrlMap)
	}
}

func TestGetProbe(t *testing.T) {
	translator := gceForTest(false)
	nodePortToHealthCheck := map[backends.ServicePort]string{
		{NodePort: 3001, Protocol: annotations.ProtocolHTTP}:  "/healthz",
		{NodePort: 3002, Protocol: annotations.ProtocolHTTPS}: "/foo",
	}
	for _, svc := range makeServices(nodePortToHealthCheck, apiv1.NamespaceDefault) {
		translator.svcLister.Add(svc)
	}
	for _, pod := range makePods(nodePortToHealthCheck, apiv1.NamespaceDefault) {
		translator.podLister.Add(pod)
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
	translator := gceForTest(false)
	nodePortToHealthCheck := map[backends.ServicePort]string{
		{NodePort: 3001, Protocol: annotations.ProtocolHTTP}: "/healthz",
	}
	for _, svc := range makeServices(nodePortToHealthCheck, apiv1.NamespaceDefault) {
		translator.svcLister.Add(svc)
	}
	for _, pod := range makePods(nodePortToHealthCheck, apiv1.NamespaceDefault) {
		pod.Spec.Containers[0].Ports[0].Name = "test"
		pod.Spec.Containers[0].ReadinessProbe.Handler.HTTPGet.Port = intstr.IntOrString{Type: intstr.String, StrVal: "test"}
		translator.podLister.Add(pod)
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
	translator := gceForTest(false)

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
	translator.podLister.Add(firstPod)
	nodePortToHealthCheck := map[backends.ServicePort]string{
		{NodePort: 3001, Protocol: annotations.ProtocolHTTP}: "/healthz",
	}
	for _, svc := range makeServices(nodePortToHealthCheck, apiv1.NamespaceDefault) {
		translator.svcLister.Add(svc)
	}
	for _, pod := range makePods(nodePortToHealthCheck, apiv1.NamespaceDefault) {
		pod.Spec.Containers[0].Ports[0].Name = "test"
		pod.Spec.Containers[0].ReadinessProbe.Handler.HTTPGet.Port = intstr.IntOrString{Type: intstr.String, StrVal: "test"}
		translator.podLister.Add(pod)
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

func makePods(nodePortToHealthCheck map[backends.ServicePort]string, ns string) []*apiv1.Pod {
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

func makeServices(nodePortToHealthCheck map[backends.ServicePort]string, ns string) []*apiv1.Service {
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
	translator := gceForTest(true)

	ep1 := "ep1"
	ep2 := "ep2"

	svcPorts := []backends.ServicePort{
		{NodePort: int64(30001)},
		{NodePort: int64(30002)},
		{
			SvcName:       types.NamespacedName{Namespace: "ns", Name: ep1},
			NodePort:      int64(30003),
			NEGEnabled:    true,
			SvcTargetPort: "80",
		},
		{
			SvcName:       types.NamespacedName{Namespace: "ns", Name: ep2},
			NodePort:      int64(30004),
			NEGEnabled:    true,
			SvcTargetPort: "named-port",
		},
	}

	translator.endpointLister.Add(newDefaultEndpoint(ep1))
	translator.endpointLister.Add(newDefaultEndpoint(ep2))

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

func int64ToMap(l []int64) map[int64]bool {
	ret := map[int64]bool{}
	for _, i := range l {
		ret[i] = true
	}
	return ret
}
