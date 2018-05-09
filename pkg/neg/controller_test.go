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

package neg

import (
	"testing"
	"time"

	apiv1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/utils"

	"k8s.io/apimachinery/pkg/util/intstr"
)

func newTestController(kubeClient kubernetes.Interface) *Controller {
	context := context.NewControllerContext(kubeClient, apiv1.NamespaceAll, 1*time.Second, true)
	controller, _ := NewController(kubeClient,
		NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network"),
		context,
		NewFakeZoneGetter(),
		utils.NewNamer(CluseterID, ""),
		1*time.Second,
	)
	return controller
}

func TestNewNonNEGService(t *testing.T) {
	controller := newTestController(fake.NewSimpleClientset())
	defer controller.stop()
	controller.serviceLister.Add(newTestService(false))
	controller.ingressLister.Add(newTestIngress())
	err := controller.processService(serviceKeyFunc(testServiceNamespace, testServiceName))
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}

	validateSyncers(t, controller, 0, true)
}

func TestNewNEGService(t *testing.T) {
	controller := newTestController(fake.NewSimpleClientset())
	defer controller.stop()
	controller.serviceLister.Add(newTestService(true))
	controller.ingressLister.Add(newTestIngress())
	err := controller.processService(serviceKeyFunc(testServiceNamespace, testServiceName))
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}

	validateSyncers(t, controller, 3, false)
}

func TestEnableNEGService(t *testing.T) {
	controller := newTestController(fake.NewSimpleClientset())
	defer controller.stop()
	controller.serviceLister.Add(newTestService(false))
	controller.ingressLister.Add(newTestIngress())
	err := controller.processService(serviceKeyFunc(testServiceNamespace, testServiceName))
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	validateSyncers(t, controller, 0, true)

	controller.serviceLister.Update(newTestService(true))
	err = controller.processService(serviceKeyFunc(testServiceNamespace, testServiceName))
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	validateSyncers(t, controller, 3, false)
}

func TestDisableNEGService(t *testing.T) {
	controller := newTestController(fake.NewSimpleClientset())
	defer controller.stop()
	controller.serviceLister.Add(newTestService(true))
	controller.ingressLister.Add(newTestIngress())
	err := controller.processService(serviceKeyFunc(testServiceNamespace, testServiceName))
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	validateSyncers(t, controller, 3, false)

	controller.serviceLister.Update(newTestService(false))
	err = controller.processService(serviceKeyFunc(testServiceNamespace, testServiceName))
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	validateSyncers(t, controller, 3, true)
}

func TestGatherServiceTargetPortUsedByIngress(t *testing.T) {
	testCases := []struct {
		ings   []extensions.Ingress
		expect []string
	}{
		// no match
		{
			[]extensions.Ingress{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testServiceNamespace,
				},
				Spec: extensions.IngressSpec{
					Backend: &extensions.IngressBackend{
						ServiceName: "nonExists",
						ServicePort: intstr.FromString(testNamedPort),
					},
				},
			}},
			[]string{},
		},
		// ingress spec point to non-existed service port
		{
			[]extensions.Ingress{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testServiceNamespace,
				},
				Spec: extensions.IngressSpec{
					Backend: &extensions.IngressBackend{
						ServiceName: testServiceName,
						ServicePort: intstr.FromString("NonExisted"),
					},
				},
			}},
			[]string{},
		},
		// ingress point to multiple services
		{
			[]extensions.Ingress{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testServiceNamespace,
				},
				Spec: extensions.IngressSpec{
					Rules: []extensions.IngressRule{
						{
							IngressRuleValue: extensions.IngressRuleValue{
								HTTP: &extensions.HTTPIngressRuleValue{
									Paths: []extensions.HTTPIngressPath{
										{
											Path: "/path1",
											Backend: extensions.IngressBackend{
												ServiceName: testServiceName,
												ServicePort: intstr.FromInt(80),
											},
										},
										{
											Path: "/path2",
											Backend: extensions.IngressBackend{
												ServiceName: "nonExisted",
												ServicePort: intstr.FromInt(443),
											},
										},
									},
								},
							},
						},
					},
				},
			}},
			[]string{"8080"},
		},
		// two ingresses with multiple different references to service
		{
			[]extensions.Ingress{*newTestIngress(), *newTestIngress()},
			[]string{"8080", "8081", testNamedPort},
		},
		// one ingress with multiple different references to service
		{
			[]extensions.Ingress{*newTestIngress()},
			[]string{"8080", "8081", testNamedPort},
		},
	}

	for _, tc := range testCases {
		ports := gatherSerivceTargetPortUsedByIngress(tc.ings, newTestService(true))
		if len(ports) != len(tc.expect) {
			t.Errorf("Expect %v ports, but got %v.", len(tc.expect), len(ports))
		}

		for _, exp := range tc.expect {
			if !ports.Has(exp) {
				t.Errorf("Expect ports to include %q.", exp)
			}
		}
	}
}

func validateSyncers(t *testing.T, controller *Controller, num int, stopped bool) {
	if len(controller.manager.(*syncerManager).syncerMap) != num {
		t.Errorf("got %v syncer, want %v.", len(controller.manager.(*syncerManager).syncerMap), num)
	}
	for key, syncer := range controller.manager.(*syncerManager).syncerMap {
		if syncer.IsStopped() != stopped {
			t.Errorf("got syncer %q IsStopped() == %v, want %v.", key, syncer.IsStopped(), stopped)
		}
	}
}

func newTestIngress() *extensions.Ingress {
	return &extensions.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testServiceName,
			Namespace: testServiceNamespace,
		},
		Spec: extensions.IngressSpec{
			Backend: &extensions.IngressBackend{
				ServiceName: testServiceName,
				ServicePort: intstr.FromString(testNamedPort),
			},
			Rules: []extensions.IngressRule{
				{
					IngressRuleValue: extensions.IngressRuleValue{
						HTTP: &extensions.HTTPIngressRuleValue{
							Paths: []extensions.HTTPIngressPath{
								{
									Path: "/path1",
									Backend: extensions.IngressBackend{
										ServiceName: testServiceName,
										ServicePort: intstr.FromInt(80),
									},
								},
								{
									Path: "/path2",
									Backend: extensions.IngressBackend{
										ServiceName: testServiceName,
										ServicePort: intstr.FromInt(443),
									},
								},
								{
									Path: "/path3",
									Backend: extensions.IngressBackend{
										ServiceName: testServiceName,
										ServicePort: intstr.FromString(testNamedPort),
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func newTestService(negEnabled bool) *apiv1.Service {
	svcAnnotations := map[string]string{}
	if negEnabled {
		svcAnnotations[annotations.NetworkEndpointGroupAlphaAnnotation] = "true"
	}
	return &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        testServiceName,
			Namespace:   testServiceNamespace,
			Annotations: svcAnnotations,
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{
					Port:       80,
					TargetPort: intstr.FromInt(8080),
				},
				{
					Port:       443,
					TargetPort: intstr.FromString(testNamedPort),
				},
				{
					Name:       testNamedPort,
					Port:       8081,
					TargetPort: intstr.FromInt(8081),
				},
				{
					Port:       8888,
					TargetPort: intstr.FromInt(8888),
				},
			},
		},
	}
}
