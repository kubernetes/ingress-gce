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
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	apiv1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/utils"

	"k8s.io/apimachinery/pkg/util/intstr"
)

func newTestController(kubeClient kubernetes.Interface) *Controller {
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	context := context.NewControllerContext(kubeClient, backendConfigClient, nil, apiv1.NamespaceAll, 1*time.Second, true, false)
	controller, _ := NewController(
		NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network"),
		context,
		NewFakeZoneGetter(),
		utils.NewNamer(CluseterID, ""),
		1*time.Second,
	)
	return controller
}

func TestNewNonNEGService(t *testing.T) {
	t.Parallel()

	controller := newTestController(fake.NewSimpleClientset())
	defer controller.stop()
	controller.serviceLister.Add(newTestService(false, []int32{}))
	controller.ingressLister.Add(newTestIngress())
	err := controller.processService(serviceKeyFunc(testServiceNamespace, testServiceName))
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}

	validateSyncers(t, controller, 0, true)
}

func TestNewNEGService(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		svcPorts []int32
		ingress  bool
		desc     string
	}{
		{
			[]int32{80, 443, 8081, 8080},
			true,
			"With ingress, 3 ports same as in ingress, 1 new port",
		},
		{
			[]int32{80, 443, 8081, 8080, 1234, 5678},
			true,
			"With ingress, 3 ports same as ingress and 3 new ports",
		},
		{
			[]int32{80, 1234, 5678},
			true,
			"With ingress, 1 port same as ingress and 2 new ports",
		},
		{
			[]int32{},
			false,
			"With ingress, no additional ports",
		},
		{
			[]int32{80, 443, 8081, 8080},
			false,
			"No ingress, 4 ports",
		},
		{
			[]int32{80},
			false,
			"No ingress, 1 port",
		},
		{
			[]int32{},
			false,
			"No ingress, no ports",
		},
	}

	testIngressPorts := sets.NewString([]string{"80", "443", "8081"}...)

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			controller := newTestController(fake.NewSimpleClientset())
			defer controller.stop()
			svcKey := serviceKeyFunc(testServiceNamespace, testServiceName)
			controller.serviceLister.Add(newTestService(tc.ingress, tc.svcPorts))

			if tc.ingress {
				controller.ingressLister.Add(newTestIngress())
			}

			err := controller.processService(svcKey)
			if err != nil {
				t.Fatalf("Failed to process service: %v", err)
			}

			expectedSyncers := len(tc.svcPorts)
			if tc.ingress {
				svcPorts := sets.NewString()
				for _, port := range tc.svcPorts {
					svcPorts.Insert(fmt.Sprintf("%v", port))
				}
				expectedSyncers = len(svcPorts.Union(testIngressPorts))
			}
			validateSyncers(t, controller, expectedSyncers, false)
			svc, exists, _ := controller.serviceLister.GetByKey(svcKey)
			if !exists || err != nil {
				t.Fatalf("Service was not created successfully, err: %v", err)
			}
			validateServiceStateAnnotation(t, svc.(*apiv1.Service), tc.svcPorts)
		})
	}
}

func TestEnableNEGServiceWithIngress(t *testing.T) {
	t.Parallel()

	controller := newTestController(fake.NewSimpleClientset())
	defer controller.stop()
	controller.serviceLister.Add(newTestService(false, []int32{}))
	controller.ingressLister.Add(newTestIngress())
	svcKey := serviceKeyFunc(testServiceNamespace, testServiceName)
	err := controller.processService(svcKey)
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	validateSyncers(t, controller, 0, true)
	svc, exists, _ := controller.serviceLister.GetByKey(svcKey)
	if !exists || err != nil {
		t.Fatalf("Service was not created successfully, err: %v", err)
	}

	controller.serviceLister.Update(newTestService(true, []int32{}))
	err = controller.processService(svcKey)
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	validateSyncers(t, controller, 3, false)
	svc, exists, _ = controller.serviceLister.GetByKey(svcKey)
	svcPorts := []int32{80, 8081, 443}
	if !exists || err != nil {
		t.Fatalf("Service was not created successfully, err: %v", err)
	}
	validateServiceStateAnnotation(t, svc.(*apiv1.Service), svcPorts)
}

func TestDisableNEGServiceWithIngress(t *testing.T) {
	t.Parallel()

	controller := newTestController(fake.NewSimpleClientset())
	defer controller.stop()
	controller.serviceLister.Add(newTestService(true, []int32{}))
	controller.ingressLister.Add(newTestIngress())
	err := controller.processService(serviceKeyFunc(testServiceNamespace, testServiceName))
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	validateSyncers(t, controller, 3, false)

	controller.serviceLister.Update(newTestService(false, []int32{}))
	err = controller.processService(serviceKeyFunc(testServiceNamespace, testServiceName))
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	validateSyncers(t, controller, 3, true)
}

func TestGatherServiceTargetPortUsedByIngress(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		ings   []extensions.Ingress
		expect []int32
		desc   string
	}{
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
			[]int32{},
			"no match",
		},
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
			[]int32{},
			"ingress spec point to non-existed service port",
		},
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
			[]int32{80},
			"ingress point to multiple services",
		},
		{
			[]extensions.Ingress{*newTestIngress(), *newTestIngress()},
			[]int32{80, 443, 8081},
			"two ingresses with multiple different references to service",
		},
		{
			[]extensions.Ingress{*newTestIngress()},
			[]int32{80, 443, 8081},
			"one ingress with multiple different references to service",
		},
	}

	for _, tc := range testCases {
		portMap := gatherPortMappingUsedByIngress(tc.ings, newTestService(true, []int32{}))
		if len(portMap) != len(tc.expect) {
			t.Errorf("Expect %v ports, but got %v.", len(tc.expect), len(portMap))
		}

		for _, exp := range tc.expect {
			if _, ok := portMap[exp]; !ok {
				t.Errorf("Expect ports to include %v.", exp)
			}
		}
	}
}

func validateSyncers(t *testing.T, controller *Controller, num int, stopped bool) {
	t.Helper()
	if len(controller.manager.(*syncerManager).syncerMap) != num {
		t.Errorf("got %v syncer, want %v.", len(controller.manager.(*syncerManager).syncerMap), num)
	}
	for key, syncer := range controller.manager.(*syncerManager).syncerMap {
		if syncer.IsStopped() != stopped {
			t.Errorf("got syncer %q IsStopped() == %v, want %v.", key, syncer.IsStopped(), stopped)
		}
	}
}

func validateServiceStateAnnotation(t *testing.T, svc *apiv1.Service, svcPorts []int32) {
	t.Helper()
	if len(svcPorts) == 0 {
		v, ok := svc.Annotations[annotations.NEGStatusKey]
		if ok {
			t.Fatalf("Expected no NEG service state annotation when there are no servicePorts, got: %v", v)
		}
		return
	}

	v, ok := svc.Annotations[annotations.NEGStatusKey]
	if !ok {
		t.Fatalf("Failed to apply the NEG service state annotation, got %+v", svc.Annotations)
	}

	for _, port := range svcPorts {
		if !strings.Contains(v, fmt.Sprintf("%v", port)) {
			t.Fatalf("Expected NEG service state annotation to contain port %v, got %v", port, v)
		}
	}

	zoneGetter := NewFakeZoneGetter()
	zones, _ := zoneGetter.ListZones()
	for _, zone := range zones {
		if !strings.Contains(v, zone) {
			t.Fatalf("Expected NEG service state annotation to contain zone %v, got %v", zone, v)
		}
	}
}

func generateNegAnnotation(svcPorts []int32) string {
	if len(svcPorts) == 0 {
		return ""
	}
	annotation := make(map[int32]annotations.NegAttributes)
	for _, port := range svcPorts {
		annotation[port] = annotations.NegAttributes{}
	}
	formattedAnnotation, _ := json.Marshal(annotation)
	return string(formattedAnnotation)
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

func newTestService(negIngress bool, negSvcPorts []int32) *apiv1.Service {
	svcAnnotations := map[string]string{}
	svcAnnotations[annotations.NetworkEndpointGroupAlphaAnnotation] = fmt.Sprint(negIngress)
	if len(negSvcPorts) > 0 {
		svcAnnotations[annotations.ExposeNEGAnnotationKey] = generateNegAnnotation(negSvcPorts)
	}

	ports := []apiv1.ServicePort{
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
	}

	for _, port := range negSvcPorts {
		ports = append(
			ports,
			apiv1.ServicePort{
				Port:       port,
				TargetPort: intstr.FromString(fmt.Sprintf("%v", port)),
			},
		)
	}

	return &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        testServiceName,
			Namespace:   testServiceNamespace,
			Annotations: svcAnnotations,
		},
		Spec: apiv1.ServiceSpec{
			Ports: ports,
		},
	}
}
