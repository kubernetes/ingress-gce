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
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/ingress-gce/pkg/metrics"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"

	istioV1alpha3 "istio.io/api/networking/v1alpha3"
	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/flags"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	testServiceNamespace    = "test-ns"
	testServiceName         = "test-Name"
	testNamedPort           = "named-Port"
	testNamedPortWithNumber = "80"
)

var (
	defaultBackend = utils.ServicePort{
		ID: utils.ServicePortID{
			Service: types.NamespacedName{
				Name:      "default-http-backend",
				Namespace: "kube-system",
			},
			Port: intstr.FromString("http"),
		},
		Port:       80,
		TargetPort: "9376"}

	defaultBackendService = &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "kube-system",
			Name:      "default-http-backend",
		},
		Spec: v1.ServiceSpec{
			ClusterIP: "1.2.3.4",
			Type:      v1.ServiceTypeNodePort,
			Ports: []v1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(9376),
					NodePort:   30001,
				},
			},
		},
	}

	defaultBackendServiceWithNeg = &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "kube-system",
			Name:      "default-http-backend",
			Annotations: map[string]string{
				annotations.NEGAnnotationKey: "{\"ingress\":true}",
			},
		},
		Spec: v1.ServiceSpec{
			ClusterIP: "1.2.3.4",
			Type:      v1.ServiceTypeNodePort,
			Ports: []v1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(9376),
					NodePort:   30001,
				},
			},
		},
	}
)

func newTestController(kubeClient kubernetes.Interface) *Controller {
	testContext := negtypes.NewTestContextWithKubeClient(kubeClient)
	dynamicSchema := runtime.NewScheme()
	kubeClient.CoreV1().ConfigMaps("kube-system").Create(context.TODO(), &apiv1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "ingress-controller-config-test"}, Data: map[string]string{"enable-asm": "true"}}, metav1.CreateOptions{})
	dynamicClient := dynamicfake.NewSimpleDynamicClient(dynamicSchema)
	destinationGVR := schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1alpha3", Resource: "destinationrules"}
	drDynamicInformer := dynamicinformer.NewFilteredDynamicInformer(dynamicClient, destinationGVR, apiv1.NamespaceAll, testContext.ResyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		nil)
	controller := NewController(
		kubeClient,
		testContext.SvcNegClient,
		dynamicClient.Resource(destinationGVR),
		testContext.KubeSystemUID,
		testContext.IngressInformer,
		testContext.ServiceInformer,
		testContext.PodInformer,
		testContext.NodeInformer,
		testContext.EndpointInformer,
		drDynamicInformer.Informer(),
		testContext.SvcNegInformer,
		func() bool { return true },
		metrics.NewControllerMetrics(),
		testContext.L4Namer,
		defaultBackend,
		negtypes.NewAdapter(testContext.Cloud),
		negtypes.NewFakeZoneGetter(),
		testContext.NegNamer,
		testContext.ResyncPeriod,
		testContext.ResyncPeriod,
		// TODO(freehan): enable readiness reflector for unit tests
		false, // enableReadinessReflector
		true,  // runIngress
		false, //runL4Controller
		false, //enableNonGcpMode
		true,  //eanbleAsm
		[]string{},
	)

	return controller
}

func TestIsHealthy(t *testing.T) {
	controller := newTestController(fake.NewSimpleClientset())
	defer controller.stop()

	err := controller.IsHealthy()
	if err != nil {
		t.Errorf("Expect controller to be healthy initially: %v", err)
	}

	timestamp := time.Now().Add(-61 * time.Minute)
	controller.syncTracker.Set(timestamp)
	err = controller.IsHealthy()
	if err == nil {
		t.Errorf("Expect controller to NOT be healthy")
	}

	controller.syncTracker.Track()
	err = controller.IsHealthy()
	if err != nil {
		t.Errorf("Expect controller to be healthy: %v", err)
	}
}

func TestNewNonNEGService(t *testing.T) {
	t.Parallel()

	controller := newTestController(fake.NewSimpleClientset())
	defer controller.stop()
	controller.serviceLister.Add(newTestService(controller, false, []int32{}))
	controller.ingressLister.Add(newTestIngress(testServiceName))
	err := controller.processService(utils.ServiceKeyFunc(testServiceNamespace, testServiceName))
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}

	validateSyncers(t, controller, 0, true)
}

func TestNewNEGService(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		exposedPorts   []int32
		ingress        bool
		expectNegPorts []int32
		desc           string
	}{
		{
			[]int32{80, 443, 8081, 8080},
			true,
			[]int32{80, 443, 8081, 8080},
			"With ingress, 3 ports same as in ingress, 1 new port",
		},
		{
			[]int32{80, 443, 8081, 8080, 1234, 5678},
			true,
			[]int32{80, 443, 8081, 8080, 1234, 5678},
			"With ingress, 3 ports same as ingress and 3 new ports",
		},
		{
			[]int32{80, 1234, 5678},
			true,
			[]int32{80, 443, 8081, 1234, 5678},
			"With ingress, 1 port same as ingress and 2 new ports",
		},
		{
			[]int32{},
			true,
			[]int32{80, 443, 8081},
			"With ingress, no additional ports",
		},
		{
			[]int32{80, 443, 8081, 8080},
			false,
			[]int32{80, 443, 8081, 8080},
			"No ingress, 4 ports",
		},
		{
			[]int32{80},
			false,
			[]int32{80},
			"No ingress, 1 port",
		},
		{
			[]int32{},
			false,
			[]int32{},
			"No ingress, no ports",
		},
	}

	testIngressPorts := sets.NewString([]string{"80", "443", "8081"}...)

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			controller := newTestController(fake.NewSimpleClientset())
			defer controller.stop()
			svcKey := utils.ServiceKeyFunc(testServiceNamespace, testServiceName)
			controller.serviceLister.Add(newTestService(controller, tc.ingress, tc.exposedPorts))

			if tc.ingress {
				controller.ingressLister.Add(newTestIngress(testServiceName))
			}

			err := controller.processService(svcKey)
			if err != nil {
				t.Fatalf("Failed to process service: %v", err)
			}

			expectedSyncers := len(tc.exposedPorts)
			if tc.ingress {
				svcPorts := sets.NewString()
				for _, port := range tc.exposedPorts {
					svcPorts.Insert(fmt.Sprintf("%v", port))
				}
				expectedSyncers = len(svcPorts.Union(testIngressPorts))
			}
			validateSyncers(t, controller, expectedSyncers, false)
			svcClient := controller.client.CoreV1().Services(testServiceNamespace)
			svc, err := svcClient.Get(context.TODO(), testServiceName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Service was not created successfully, err: %v", err)
			}
			validateServiceStateAnnotation(t, svc, tc.expectNegPorts, controller.namer)
		})
	}
}

func TestEnableNEGServiceWithIngress(t *testing.T) {
	t.Parallel()

	controller := newTestController(fake.NewSimpleClientset())
	defer controller.stop()
	controller.serviceLister.Add(newTestService(controller, false, []int32{}))
	controller.ingressLister.Add(newTestIngress(testServiceName))
	svcClient := controller.client.CoreV1().Services(testServiceNamespace)
	svcKey := utils.ServiceKeyFunc(testServiceNamespace, testServiceName)
	err := controller.processService(svcKey)
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	validateSyncers(t, controller, 0, true)
	svc, err := svcClient.Get(context.TODO(), testServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Service was not created.(*apiv1.Service) successfully, err: %v", err)
	}

	controller.serviceLister.Update(newTestService(controller, true, []int32{}))
	err = controller.processService(svcKey)
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	validateSyncers(t, controller, 3, false)
	svc, err = svcClient.Get(context.TODO(), testServiceName, metav1.GetOptions{})
	svcPorts := []int32{80, 8081, 443}
	if err != nil {
		t.Fatalf("Service was not created successfully, err: %v", err)
	}
	validateServiceStateAnnotation(t, svc, svcPorts, controller.namer)
}

//TestEnableNEGSeviceWithL4ILB tests L4 ILB service with NEGs enabled.
//Also verifies that modifying the TrafficPolicy on the service will
//take effect.
func TestEnableNEGServiceWithL4ILB(t *testing.T) {
	controller := newTestController(fake.NewSimpleClientset())
	manager := controller.manager.(*syncerManager)
	controller.runL4 = true
	defer controller.stop()
	var prevSyncerKey, updatedSyncerKey negtypes.NegSyncerKey
	localMode := false
	t.Logf("Creating L4 ILB service with ExternalTrafficPolicy:Cluster")
	controller.serviceLister.Add(newTestILBService(controller, localMode, 80))
	svcClient := controller.client.CoreV1().Services(testServiceNamespace)
	svcKey := utils.ServiceKeyFunc(testServiceNamespace, testServiceName)
	err := controller.processService(svcKey)
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	svc, err := svcClient.Get(context.TODO(), testServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Service was not created.(*apiv1.Service) successfully, err: %v", err)
	}
	expectedPortInfoMap := negtypes.NewPortInfoMapForVMIPNEG(testServiceNamespace, testServiceName,
		controller.l4Namer, localMode)
	// There will be only one entry in the map
	for key, val := range expectedPortInfoMap {
		prevSyncerKey = manager.getSyncerKey(testServiceNamespace, testServiceName, key, val)
	}
	ValidateSyncerByKey(t, controller, 1, prevSyncerKey, false)
	validateSyncerManagerWithPortInfoMap(t, controller, testServiceNamespace, testServiceName, expectedPortInfoMap)
	validateServiceAnnotationWithPortInfoMap(t, svc, expectedPortInfoMap)
	// Now Update the service to change the TrafficPolicy
	t.Logf("Updating L4 ILB service from ExternalTrafficPolicy:Cluster to Local")
	localMode = true
	if err = controller.serviceLister.Update(updateTestILBService(controller, localMode, svc)); err != nil {
		t.Fatalf("Failed to update test L4 ILB service: %v", err)
	}
	if err = controller.processService(svcKey); err != nil {
		t.Fatalf("Failed to process updated L4 ILB srvice: %v", err)
	}
	expectedPortInfoMap = negtypes.NewPortInfoMapForVMIPNEG(testServiceNamespace, testServiceName,
		controller.l4Namer, localMode)
	// There will be only one entry in the map
	for key, val := range expectedPortInfoMap {
		updatedSyncerKey = manager.getSyncerKey(testServiceNamespace, testServiceName, key, val)
	}
	// there should only be 2 syncers - one stopped and one running.
	ValidateSyncerByKey(t, controller, 2, updatedSyncerKey, false)
	ValidateSyncerByKey(t, controller, 2, prevSyncerKey, true)
	time.Sleep(1 * time.Second)
	controller.manager.(*syncerManager).GC()
	// check the port info map after all stale syncers have been deleted.
	validateSyncerManagerWithPortInfoMap(t, controller, testServiceNamespace, testServiceName, expectedPortInfoMap)
	validateServiceAnnotationWithPortInfoMap(t, svc, expectedPortInfoMap)
}

// TestEnableNEGServiceWithILBIngress tests ILB service with NEG enabled
func TestEnableNEGServiceWithILBIngress(t *testing.T) {
	// Not running in parallel since enabling global flag
	flags.F.EnableL7Ilb = true
	controller := newTestController(fake.NewSimpleClientset())
	defer controller.stop()
	controller.serviceLister.Add(newTestService(controller, false, []int32{}))
	ing := newTestIngress("ilb-ingress")
	ing.Annotations = map[string]string{annotations.IngressClassKey: annotations.GceL7ILBIngressClass}

	controller.ingressLister.Add(ing)
	svcClient := controller.client.CoreV1().Services(testServiceNamespace)
	svcKey := utils.ServiceKeyFunc(testServiceNamespace, testServiceName)
	err := controller.processService(svcKey)
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	validateSyncers(t, controller, 0, true)
	svc, err := svcClient.Get(context.TODO(), testServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Service was not created.(*apiv1.Service) successfully, err: %v", err)
	}

	controller.serviceLister.Update(newTestService(controller, true, []int32{}))
	err = controller.processService(svcKey)
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	validateSyncers(t, controller, 3, false)
	svc, err = svcClient.Get(context.TODO(), testServiceName, metav1.GetOptions{})
	svcPorts := []int32{80, 8081, 443}
	if err != nil {
		t.Fatalf("Service was not created successfully, err: %v", err)
	}
	validateServiceStateAnnotation(t, svc, svcPorts, controller.namer)
}

func TestDisableNEGServiceWithIngress(t *testing.T) {
	t.Parallel()

	controller := newTestController(fake.NewSimpleClientset())
	defer controller.stop()
	controller.serviceLister.Add(newTestService(controller, true, []int32{}))
	controller.ingressLister.Add(newTestIngress(testServiceName))
	err := controller.processService(utils.ServiceKeyFunc(testServiceNamespace, testServiceName))
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	validateSyncers(t, controller, 3, false)

	controller.serviceLister.Update(newTestService(controller, false, []int32{}))
	err = controller.processService(utils.ServiceKeyFunc(testServiceNamespace, testServiceName))
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	validateSyncers(t, controller, 3, true)
}

func TestGatherPortMappingUsedByIngress(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		ings   []v1beta1.Ingress
		expect []int32
		desc   string
	}{
		{
			[]v1beta1.Ingress{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testServiceNamespace,
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "nonExists",
						ServicePort: intstr.FromString(testNamedPort),
					},
				},
			}},
			[]int32{},
			"no match",
		},
		{
			[]v1beta1.Ingress{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testServiceNamespace,
				},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: testServiceName,
						ServicePort: intstr.FromString("NonExisted"),
					},
				},
			}},
			[]int32{},
			"ingress spec point to non-existed service port",
		},
		{
			[]v1beta1.Ingress{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testServiceNamespace,
				},
				Spec: v1beta1.IngressSpec{
					Rules: []v1beta1.IngressRule{
						{
							IngressRuleValue: v1beta1.IngressRuleValue{
								HTTP: &v1beta1.HTTPIngressRuleValue{
									Paths: []v1beta1.HTTPIngressPath{
										{
											Path: "/path1",
											Backend: v1beta1.IngressBackend{
												ServiceName: testServiceName,
												ServicePort: intstr.FromInt(80),
											},
										},
										{
											Path: "/path2",
											Backend: v1beta1.IngressBackend{
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
			[]v1beta1.Ingress{*newTestIngress(testServiceName), *newTestIngress(testServiceName)},
			[]int32{80, 443, 8081},
			"two ingresses with multiple different references to service",
		},
		{
			[]v1beta1.Ingress{*newTestIngress(testServiceName)},
			[]int32{80, 443, 8081},
			"one ingress with multiple different references to service",
		},
		{
			[]v1beta1.Ingress{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testServiceNamespace,
				},
				Spec: v1beta1.IngressSpec{
					Rules: []v1beta1.IngressRule{
						{
							IngressRuleValue: v1beta1.IngressRuleValue{
								HTTP: &v1beta1.HTTPIngressRuleValue{
									Paths: []v1beta1.HTTPIngressPath{
										{
											Path: "/path1",
											Backend: v1beta1.IngressBackend{
												ServiceName: testServiceName,
												ServicePort: intstr.FromString(testNamedPortWithNumber),
											},
										},
									},
								},
							},
						},
					},
				},
			}},
			[]int32{8881},
			"ingress reference service with service port name with number",
		},
	}

	for _, tc := range testCases {
		controller := newTestController(fake.NewSimpleClientset())
		defer controller.stop()
		portTupleSet := gatherPortMappingUsedByIngress(tc.ings, newTestService(controller, true, []int32{}))
		if len(portTupleSet) != len(tc.expect) {
			t.Errorf("Expect %v ports, but got %v.", len(tc.expect), len(portTupleSet))
		}

		for _, exp := range tc.expect {
			found := false
			for tuple := range portTupleSet {
				if tuple.Port == exp {
					found = true
					if !reflect.DeepEqual(getTestSvcPortTuple(exp), tuple) {
						t.Errorf("For test case %q, expect tuple %v, but got %v", tc.desc, getTestSvcPortTuple(exp), tuple)
					}
				}
			}
			if !found {
				t.Errorf("Expect ports to include %v.", exp)
			}
		}
	}
}

func TestSyncNegAnnotation(t *testing.T) {
	t.Parallel()
	// TODO: test that c.serviceLister.Update is called whenever the annotation
	// is changed. When there is no change, Update should not be called.
	controller := newTestController(fake.NewSimpleClientset())
	defer controller.stop()
	svcClient := controller.client.CoreV1().Services(testServiceNamespace)
	newTestService(controller, false, []int32{})
	namespace := testServiceNamespace
	name := testServiceName
	namer := controller.namer
	testCases := []struct {
		desc            string
		previousPortMap negtypes.PortInfoMap
		portMap         negtypes.PortInfoMap
	}{
		{
			desc:    "apply new annotation with no previous annotation",
			portMap: negtypes.NewPortInfoMap(namespace, name, negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 80, TargetPort: "named_port"}, negtypes.SvcPortTuple{Port: 443, TargetPort: "other_port"}), namer, false, nil),
		},
		{
			desc:            "same annotation applied twice",
			previousPortMap: negtypes.NewPortInfoMap(namespace, name, negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 80, TargetPort: "named_port"}, negtypes.SvcPortTuple{Port: 4040, TargetPort: "other_port"}), namer, false, nil),
			portMap:         negtypes.NewPortInfoMap(namespace, name, negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 80, TargetPort: "named_port"}, negtypes.SvcPortTuple{Port: 4040, TargetPort: "other_port"}), namer, false, nil),
		},
		{
			desc:            "apply new annotation and override previous annotation",
			previousPortMap: negtypes.NewPortInfoMap(namespace, name, negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 80, TargetPort: "named_port"}, negtypes.SvcPortTuple{Port: 4040, TargetPort: "other_port"}), namer, false, nil),
			portMap:         negtypes.NewPortInfoMap(namespace, name, negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 3000, TargetPort: "6000"}, negtypes.SvcPortTuple{Port: 4000, TargetPort: "8000"}), namer, false, nil),
		},
		{
			desc:            "remove previous annotation",
			previousPortMap: negtypes.NewPortInfoMap(namespace, name, negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 80, TargetPort: "named_port"}, negtypes.SvcPortTuple{Port: 4040, TargetPort: "other_port"}), namer, false, nil),
		},
		{
			desc: "remove annotation with no previous annotation",
		},
		{
			desc:            "readiness gate makes no difference 1",
			previousPortMap: negtypes.NewPortInfoMap(namespace, name, negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 80, TargetPort: "named_port"}, negtypes.SvcPortTuple{Port: 4040, TargetPort: "other_port"}), namer, false, nil),
			portMap:         negtypes.NewPortInfoMap(namespace, name, negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 3000, TargetPort: "6000"}, negtypes.SvcPortTuple{Port: 4000, TargetPort: "8000"}), namer, true, nil),
		},
		{
			desc:            "readiness gate makes no difference 2",
			previousPortMap: negtypes.NewPortInfoMap(namespace, name, negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 80, TargetPort: "named_port"}, negtypes.SvcPortTuple{Port: 4040, TargetPort: "other_port"}), namer, true, nil),
			portMap:         negtypes.NewPortInfoMap(namespace, name, negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 80, TargetPort: "named_port"}, negtypes.SvcPortTuple{Port: 4040, TargetPort: "other_port"}), namer, false, nil),
		},
		{
			desc:            "no difference with port name",
			previousPortMap: negtypes.NewPortInfoMap(namespace, name, negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 80, TargetPort: "named_port"}, negtypes.SvcPortTuple{Port: 4040, TargetPort: "other_port"}), namer, true, nil),
			portMap:         negtypes.NewPortInfoMap(namespace, name, negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 80, Name: "foo", TargetPort: "named_port"}, negtypes.SvcPortTuple{Port: 4040, Name: "bar", TargetPort: "other_port"}), namer, false, nil),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			controller.syncNegStatusAnnotation(namespace, name, tc.previousPortMap)
			svc, _ := svcClient.Get(context.TODO(), name, metav1.GetOptions{})

			var oldSvcPorts []int32
			for port := range tc.previousPortMap {
				oldSvcPorts = append(oldSvcPorts, port.ServicePort)
			}
			validateServiceStateAnnotation(t, svc, oldSvcPorts, controller.namer)

			controller.syncNegStatusAnnotation(namespace, name, tc.portMap)
			svc, _ = svcClient.Get(context.TODO(), name, metav1.GetOptions{})

			var svcPorts []int32
			for port := range tc.portMap {
				svcPorts = append(svcPorts, port.ServicePort)
			}
			validateServiceStateAnnotation(t, svc, svcPorts, controller.namer)
		})
	}
}

func TestDefaultBackendServicePortInfoMapForL7ILB(t *testing.T) {
	// Not using t.Parallel() since we are sharing the controller
	controller := newTestController(fake.NewSimpleClientset())
	defer controller.stop()
	newTestService(controller, false, []int32{})

	testCases := []struct {
		desc    string
		ingName string
		// forIlb sets the annotation for Ilb
		forIlb bool
		// defaultOverride sets backend to nil
		defaultOverride                  bool
		defaultBackendServiceServicePort utils.ServicePort
		want                             negtypes.PortInfoMap
	}{
		{
			desc:            "ingress with backend",
			ingName:         "ingress-name-1",
			forIlb:          false,
			defaultOverride: false,
			want:            negtypes.PortInfoMap{},
		},
		{
			desc:                             "ingress without backend",
			ingName:                          "ingress-name-2",
			forIlb:                           false,
			defaultOverride:                  true,
			defaultBackendServiceServicePort: defaultBackend,
			want:                             negtypes.PortInfoMap{},
		},
		{
			desc:            "ingress with backend with ILB",
			ingName:         "ingress-name-3",
			forIlb:          true,
			defaultOverride: false,
			want:            negtypes.PortInfoMap{},
		},
		{
			desc:                             "ingress without backend with ILB",
			ingName:                          "ingress-name-4",
			forIlb:                           true,
			defaultOverride:                  true,
			defaultBackendServiceServicePort: defaultBackend,
			want: negtypes.NewPortInfoMap(
				defaultBackend.ID.Service.Namespace,
				defaultBackend.ID.Service.Name,
				negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Name: "http", Port: 80, TargetPort: defaultBackend.TargetPort}),
				controller.namer,
				false,
				nil,
			),
		},
		{
			desc:            "User default backend with different port",
			ingName:         "ingress-name-5",
			forIlb:          true,
			defaultOverride: true,
			defaultBackendServiceServicePort: utils.ServicePort{
				ID: utils.ServicePortID{
					Service: types.NamespacedName{
						Namespace: testServiceNamespace, Name: "newDefaultBackend",
					},
					Port: intstr.FromInt(80),
				},
				Port:       80,
				TargetPort: "8888",
			},
			want: negtypes.NewPortInfoMap(
				testServiceNamespace,
				"newDefaultBackend",
				negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Name: "80", Port: 80, TargetPort: "8888"}),
				controller.namer,
				false,
				nil,
			),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			ing := newTestIngress(tc.ingName)

			if tc.forIlb {
				ing.Annotations = map[string]string{annotations.IngressClassKey: annotations.GceL7ILBIngressClass}
			}

			if tc.defaultOverride {
				// Override backend service
				controller.defaultBackendService = tc.defaultBackendServiceServicePort
				ing.Spec.Backend = nil
			}

			newIng, err := controller.client.NetworkingV1beta1().Ingresses(testServiceNamespace).Create(context.TODO(), ing, metav1.CreateOptions{})
			if err != nil {
				t.Fatal(err)
			}

			// Add to cache directly since the cache doesn't get updated
			if err := controller.ingressLister.Add(newIng); err != nil {
				t.Fatal(err)
			}
			result := make(negtypes.PortInfoMap)
			controller.mergeDefaultBackendServicePortInfoMap(controller.defaultBackendService.ID.Service.String(), defaultBackendService, result)
			if !reflect.DeepEqual(tc.want, result) {
				t.Fatalf("got %+v, want %+v", result, tc.want)
			}
		})
	}
}

func TestMergeDefaultBackendServicePortInfoMap(t *testing.T) {
	controller := newTestController(fake.NewSimpleClientset())
	controller.defaultBackendService = defaultBackend
	newTestService(controller, false, []int32{})
	defaultBackendServiceKey := defaultBackend.ID.Service.String()
	expectPortMap := negtypes.NewPortInfoMap(
		defaultBackend.ID.Service.Namespace,
		defaultBackend.ID.Service.Name,
		negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Name: "http", Port: 80, TargetPort: defaultBackend.TargetPort}),
		controller.namer,
		false,
		nil,
	)
	expectEmptyPortmap := make(negtypes.PortInfoMap)

	for _, tc := range []struct {
		desc           string
		getIngress     func() *v1beta1.Ingress
		defaultService *v1.Service
		expectNeg      bool
	}{
		{
			desc:           "no ingress",
			getIngress:     func() *v1beta1.Ingress { return nil },
			defaultService: defaultBackendService,
			expectNeg:      false,
		},
		{
			desc:           "no ingress and default backend service has NEG annotation",
			getIngress:     func() *v1beta1.Ingress { return nil },
			defaultService: defaultBackendServiceWithNeg,
			expectNeg:      false,
		},
		{
			desc: "ing1 has backend and default backend service does not have NEG annotation",
			getIngress: func() *v1beta1.Ingress {
				ing := newTestIngress("ing1")
				ing.Spec.Backend = &v1beta1.IngressBackend{
					ServiceName: "svc1",
				}
				return ing
			},
			defaultService: defaultBackendService,
			expectNeg:      false,
		},
		{
			desc:           "ing1 has backend and default backend service has NEG annotation",
			getIngress:     func() *v1beta1.Ingress { return nil },
			defaultService: defaultBackendServiceWithNeg,
			expectNeg:      false,
		},
		{
			desc: "ing2 does not backend and default backend service does not have NEG annotation",
			getIngress: func() *v1beta1.Ingress {
				ing := newTestIngress("ing2")
				ing.Spec.Backend = nil
				return ing
			},
			defaultService: defaultBackendService,
			expectNeg:      false,
		},
		{
			desc:           "ing2 does not backend and default backend service has NEG annotation",
			getIngress:     func() *v1beta1.Ingress { return nil },
			defaultService: defaultBackendServiceWithNeg,
			expectNeg:      true,
		},
		{
			desc: "ing3 is L7 ILB, has backend and default backend service does not have NEG annotation",
			getIngress: func() *v1beta1.Ingress {
				ing := newTestIngress("ing3")
				ing.Annotations = map[string]string{annotations.IngressClassKey: annotations.GceL7ILBIngressClass}
				return ing
			},
			defaultService: defaultBackendService,
			expectNeg:      false,
		},
		{
			desc: "ing4 is L7 ILB, does not has backend and default backend service does not have NEG annotation",
			getIngress: func() *v1beta1.Ingress {
				ing := newTestIngress("ing4")
				ing.Annotations = map[string]string{annotations.IngressClassKey: annotations.GceL7ILBIngressClass}
				ing.Spec.Backend = nil
				return ing
			},
			defaultService: defaultBackendService,
			expectNeg:      true,
		},
		{
			desc:           "cluster has many ingresses (ILB and XLB) without backend and default backend service has NEG annotation",
			getIngress:     func() *v1beta1.Ingress { return nil },
			defaultService: defaultBackendServiceWithNeg,
			expectNeg:      true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			ing := tc.getIngress()
			if ing != nil {
				if err := controller.ingressLister.Add(ing); err != nil {
					t.Fatal(err)
				}
			}

			portMap := make(negtypes.PortInfoMap)
			if err := controller.mergeDefaultBackendServicePortInfoMap(defaultBackendServiceKey, tc.defaultService, portMap); err != nil {
				t.Errorf("for test case %q, expect err == nil; but got %v", tc.desc, err)
			}

			if tc.expectNeg {
				if !reflect.DeepEqual(portMap, expectPortMap) {
					t.Errorf("for test case %q, expect port map == %v, but got %v", tc.desc, expectPortMap, portMap)
				}
			} else {
				if !reflect.DeepEqual(portMap, expectEmptyPortmap) {
					t.Errorf("for test case %q, expect port map == %v, but got %v", tc.desc, expectEmptyPortmap, portMap)
				}
			}
		})
	}
}

func TestNewDestinationRule(t *testing.T) {
	controllerHelper := newTestController(fake.NewSimpleClientset())
	defer controllerHelper.stop()
	n1s1 := newTestServiceCus(t, controllerHelper, "namespace1", "service1", []int32{80, 90})
	n2Dr1, n2usDr1 := newTestDestinationRule(t, controllerHelper, "namespace2", "test-destination-rule", "service1.namespace1", []string{"v1", "v2"})

	testcases := []struct {
		desc              string
		service           *apiv1.Service
		destinationRule   *istioV1alpha3.DestinationRule
		usDestinationRule *unstructured.Unstructured
		wantSvcPortMap    negtypes.PortInfoMap
		wantDRPortMap     negtypes.PortInfoMap
	}{
		{
			desc:              "controller should create NEGs for services and destinationrules",
			service:           n1s1,
			usDestinationRule: n2usDr1,
			destinationRule:   n2Dr1,
			wantSvcPortMap: negtypes.NewPortInfoMap(
				"namespace1",
				"service1",
				negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Name: "port80", Port: 80, TargetPort: "80"}, negtypes.SvcPortTuple{Name: "port90", Port: 90, TargetPort: "90"}),
				controllerHelper.namer,
				false,
				nil,
			),
			wantDRPortMap: helperNewPortInfoMapWithDestinationRule(
				"namespace1",
				"service1",
				negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Name: "port80", Port: 80, TargetPort: "80"}, negtypes.SvcPortTuple{Name: "port90", Port: 90, TargetPort: "90"}),
				controllerHelper.namer,
				false,
				n2Dr1,
			),
		},
		{
			desc:              "controller should create NEGs for services",
			service:           n1s1,
			usDestinationRule: nil,
			destinationRule:   nil,
			wantSvcPortMap: negtypes.NewPortInfoMap(
				"namespace1",
				"service1",
				negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Name: "port80", Port: 80, TargetPort: "80"}, negtypes.SvcPortTuple{Name: "port90", Port: 90, TargetPort: "90"}),
				controllerHelper.namer,
				false,
				nil,
			),
			wantDRPortMap: negtypes.PortInfoMap{},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			controller := newTestController(fake.NewSimpleClientset())
			defer controller.stop()
			svcKey := utils.ServiceKeyFunc(tc.service.GetNamespace(), tc.service.GetName())

			controller.serviceLister.Add(tc.service)
			controller.client.CoreV1().Services(tc.service.GetNamespace()).Create(context.TODO(), tc.service, metav1.CreateOptions{})

			expectedPortInfoMap := negtypes.PortInfoMap{}
			expectedPortInfoMap.Merge(tc.wantSvcPortMap)
			if tc.wantDRPortMap != nil {
				expectedPortInfoMap.Merge(tc.wantDRPortMap)
			}

			if tc.usDestinationRule != nil {
				controller.destinationRuleLister.Add(tc.usDestinationRule)
				if _, err := controller.destinationRuleClient.Namespace(tc.usDestinationRule.GetNamespace()).Create(context.TODO(),
					tc.usDestinationRule,
					metav1.CreateOptions{}); err != nil {
					t.Fatalf("failed to create destinationrule: %v", err)
				}
			}

			err := controller.processService(svcKey)
			if err != nil {
				t.Fatalf("Failed to process service: %v", err)
			}

			validateSyncerManagerWithPortInfoMap(t, controller, tc.service.GetNamespace(), tc.service.GetName(), expectedPortInfoMap)

			svcClient := controller.client.CoreV1().Services(tc.service.GetNamespace())
			svc, err := svcClient.Get(context.TODO(), tc.service.GetName(), metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Service was not created successfully, err: %v", err)
			}
			validateServiceAnnotationWithPortInfoMap(t, svc, tc.wantSvcPortMap)
			if tc.usDestinationRule != nil {
				usdr, err := controller.destinationRuleClient.Namespace(tc.usDestinationRule.GetNamespace()).Get(context.TODO(), tc.usDestinationRule.GetName(), metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Destinationrule was not created successfully, err: %v", err)
				}
				validateDestinationRuleAnnotationWithPortInfoMap(t, usdr, tc.wantDRPortMap)
			}

		})
	}
}

func TestMergeCSMPortInfoMap(t *testing.T) {
	controller := newTestController(fake.NewSimpleClientset())
	defer controller.stop()
	n1s1 := newTestServiceCus(t, controller, "namespace1", "service1", []int32{80, 90})
	n2s1 := newTestServiceCus(t, controller, "namespace2", "service1", []int32{90})
	ds1, usDr1 := newTestDestinationRule(t, controller, "namespac2", "test-destination-rule", "service1.namespace1", []string{"v1", "v2"})
	if err := controller.destinationRuleLister.Add(usDr1); err != nil {
		t.Fatal(err)
	}

	testcases := []struct {
		desc           string
		srvNamespace   string
		srvName        string
		service        *apiv1.Service
		wantSvcPortMap negtypes.PortInfoMap
		wantDRPortMap  negtypes.PortInfoMap
	}{
		{
			desc:         "controller should create NEGs for services and destinationrules",
			srvNamespace: "namespace1",
			srvName:      "service1",
			service:      n1s1,
			wantSvcPortMap: negtypes.NewPortInfoMap(
				"namespace1",
				"service1",
				negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Name: "port80", Port: 80, TargetPort: "80"}, negtypes.SvcPortTuple{Name: "port90", Port: 90, TargetPort: "90"}),
				controller.namer,
				false,
				nil,
			),
			wantDRPortMap: helperNewPortInfoMapWithDestinationRule(
				"namespace1",
				"service1",
				negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Name: "port80", Port: 80, TargetPort: "80"}, negtypes.SvcPortTuple{Name: "port90", Port: 90, TargetPort: "90"}),
				controller.namer,
				false,
				ds1,
			),
		},
		{
			desc:         "controller should create NEGs for services",
			srvNamespace: "namespace2",
			srvName:      "service1",
			service:      n2s1,
			wantSvcPortMap: negtypes.NewPortInfoMap(
				"namespace2",
				"service1",
				negtypes.NewSvcPortTupleSet(negtypes.SvcPortTuple{Name: "port90", Port: 90, TargetPort: "90"}),
				controller.namer,
				false,
				nil,
			),
			wantDRPortMap: negtypes.PortInfoMap{},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			portInfoMap := make(negtypes.PortInfoMap)
			portInfoMap, destinationRulePortInfoMap, err := controller.getCSMPortInfoMap(tc.srvNamespace, tc.srvName, tc.service)
			if err != nil {
				t.Fatalf("Failed to run mergeCSMPortInfoMap: %v", err)
			}
			if !reflect.DeepEqual(tc.wantSvcPortMap, portInfoMap) {
				t.Fatalf("Wrong services PortInfoMap, got %+v, want %+v", portInfoMap, tc.wantSvcPortMap)
			}
			if !reflect.DeepEqual(tc.wantDRPortMap, destinationRulePortInfoMap) {
				t.Fatalf("Wrong DestinationRule PortInfoMap, got %+v, want %+v", destinationRulePortInfoMap, tc.wantDRPortMap)
			}

		})
	}
}

func TestEnableNegCRD(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		desc             string
		exposedPortNames map[int32]string
		expectNegPorts   []int32
		ingress          bool
		expectErr        bool
	}{
		{
			desc:             "No ingress, multiple ports all custom names",
			exposedPortNames: map[int32]string{80: "neg-1", 443: "neg-2", 8081: "neg-3", 8080: "neg-4"},
			expectNegPorts:   []int32{80, 443, 8081, 8080},
		},
		{
			desc:             "No ingress, multiple ports, mix of custom names",
			exposedPortNames: map[int32]string{80: "neg-1", 443: "", 8081: "neg-3"},
			expectNegPorts:   []int32{80, 443, 8081},
		},
		{
			desc:             "No ingress, one port, custom name",
			exposedPortNames: map[int32]string{80: "neg-1"},
			expectNegPorts:   []int32{80},
		},
		{
			desc:             "ingress, one port, custom name, neg crd is enabled",
			exposedPortNames: map[int32]string{80: "neg-1"},
			expectNegPorts:   []int32{80},
			ingress:          true,
			expectErr:        true,
		},
		{
			desc:             "ingress, one port, neg crd is enabled",
			exposedPortNames: map[int32]string{80: ""},
			expectNegPorts:   []int32{80},
			ingress:          true,
			expectErr:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			controller := newTestController(fake.NewSimpleClientset())
			svcNegClient := controller.manager.(*syncerManager).svcNegClient
			manager := controller.manager.(*syncerManager)
			defer controller.stop()
			svcKey := utils.ServiceKeyFunc(testServiceNamespace, testServiceName)
			service := newTestServiceCustomNamedNeg(controller, tc.exposedPortNames, tc.ingress)
			controller.serviceLister.Add(service)

			err := controller.processService(svcKey)
			if !tc.expectErr && err != nil {
				t.Fatalf("Failed to process service: %v", err)
			} else if tc.expectErr && err == nil {
				t.Fatalf("Expected an error when processing service")
			} else if tc.expectErr && err != nil {
				// ensure no leaked goroutines
				controller.serviceLister.Delete(service)
				err = controller.processService(svcKey)
				if err != nil {
					t.Fatalf("Failed to process service: %v", err)
				}
				return
			}

			expectedSyncers := len(tc.exposedPortNames)
			validateSyncers(t, controller, expectedSyncers, false)
			svcClient := controller.client.CoreV1().Services(testServiceNamespace)
			svc, err := svcClient.Get(context.TODO(), testServiceName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Service was not created successfully, err: %v", err)
			}
			if !tc.ingress {
				validateServiceStateAnnotationWithPortNameMap(t, svc, tc.expectNegPorts, controller.namer, tc.exposedPortNames)
				validateNegCRs(t, svc, svcNegClient, controller.namer, tc.exposedPortNames)

			} else {
				validateServiceStateAnnotation(t, svc, tc.expectNegPorts, controller.namer)
			}

			// Populate manager's ServiceNetworkEndpointGroup Cache
			negs, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svc.Namespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				t.Errorf("failed to retrieve negs")
			}

			for _, neg := range negs.Items {
				n := neg
				manager.svcNegLister.Add(&n)
			}

			controller.serviceLister.Delete(service)
			err = controller.processService(svcKey)
			if err != nil {
				t.Fatalf("Failed to process service: %v", err)
			}

			validateSyncers(t, controller, expectedSyncers, true)
		})
	}
}

func validateNegCRs(t *testing.T, svc *v1.Service, svcNegClient svcnegclient.Interface, namer negtypes.NetworkEndpointGroupNamer, negPortNameMap map[int32]string) {
	t.Helper()

	for port, expectedName := range negPortNameMap {
		name := expectedName
		if name == "" {
			name = namer.NEG(svc.Namespace, svc.Name, port)
		}
		neg, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svc.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("neg cr was not created successfully: err: %s", err)
		}

		ownerReferences := neg.GetOwnerReferences()
		if len(ownerReferences) != 1 {
			t.Fatalf("neg cr should only have 1 owner reference, has %d", len(ownerReferences))
		}

		if ownerReferences[0].UID != svc.UID {
			t.Fatalf("neg cr owner reference does not point to service %s/%s", svc.Namespace, svc.Name)
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

func ValidateSyncerByKey(t *testing.T, controller *Controller, num int, expectedKey negtypes.NegSyncerKey, stopped bool) {
	t.Helper()
	if len(controller.manager.(*syncerManager).syncerMap) != num {
		t.Errorf("got %v syncer, want %v.", len(controller.manager.(*syncerManager).syncerMap), num)
	}
	if syncer, ok := controller.manager.(*syncerManager).syncerMap[expectedKey]; ok {
		if syncer.IsStopped() == stopped {
			return
		}
		t.Errorf("got syncer %q IsStopped() == %v, want %v.", expectedKey, syncer.IsStopped(), stopped)
	}
	t.Errorf("got %v syncer map, want syncer key %q", controller.manager.(*syncerManager).syncerMap, expectedKey)
}

func validateSyncerManagerWithPortInfoMap(t *testing.T, controller *Controller, namespace, svcName string, expectedPortInfoMap negtypes.PortInfoMap) {
	t.Helper()
	if len(controller.manager.(*syncerManager).syncerMap) != len(expectedPortInfoMap) {
		t.Errorf("got %v syncer, want %v.", len(controller.manager.(*syncerManager).syncerMap), len(expectedPortInfoMap))
	}
	portInfoMap, ok := controller.manager.(*syncerManager).svcPortMap[serviceKey{namespace: namespace, name: svcName}]
	if !ok {
		t.Errorf("SyncerManager don't have PortInfoMap for namespace: %v, serviceName: %v", namespace, svcName)
	}
	if !reflect.DeepEqual(portInfoMap, expectedPortInfoMap) {
		t.Errorf("Controller manager gives a different PortInoMap, got %v, want: %v", controller.manager.(*syncerManager).svcPortMap, expectedPortInfoMap)
	}
}

func validateServiceAnnotationWithPortInfoMap(t *testing.T, svc *apiv1.Service, portInfoMap negtypes.PortInfoMap) {
	v, ok := svc.Annotations[annotations.NEGStatusKey]
	if !ok {
		t.Fatalf("Failed to apply the NEG service state annotation, got %+v", svc.Annotations)
	}

	zoneGetter := negtypes.NewFakeZoneGetter()
	zones, _ := zoneGetter.ListZones()
	for _, zone := range zones {
		if !strings.Contains(v, zone) {
			t.Fatalf("Expected NEG service state annotation to contain zone %v, got %v", zone, v)
		}
	}

	// negStatus validation
	negStatus, err := annotations.ParseNegStatus(v)
	if err != nil {
		t.Fatalf("Failed to parse neg status annotation %q: %v", v, err)
	}

	wantNegStatus := annotations.NewNegStatus(zones, portInfoMap.ToPortNegMap())
	if !reflect.DeepEqual(negStatus.NetworkEndpointGroups, wantNegStatus.NetworkEndpointGroups) {
		t.Errorf("Failed to validate the service annotation, got %v, want %v", negStatus, wantNegStatus)
	}
}

func validateDestinationRuleAnnotationWithPortInfoMap(t *testing.T, usdr *unstructured.Unstructured, portInfoMap negtypes.PortInfoMap) {
	v, ok := usdr.GetAnnotations()[annotations.NEGStatusKey]
	if !ok {
		t.Fatalf("Failed to apply the NEG service state annotation, got %+v", usdr.GetAnnotations())
	}

	zoneGetter := negtypes.NewFakeZoneGetter()
	zones, _ := zoneGetter.ListZones()
	for _, zone := range zones {
		if !strings.Contains(v, zone) {
			t.Fatalf("Expected NEG service state annotation to contain zone %v, got %v", zone, v)
		}
	}

	// negStatus validation
	negStatus, err := annotations.ParseDestinationRuleNEGStatus(v)
	if err != nil {
		t.Fatalf("Failed to parse DestinationRule NEG status annotation %q: %v", v, err)
	}

	wantNegStatus := annotations.NewDestinationRuleNegStatus(zones, portInfoMap.ToPortSubsetNegMap())
	if !reflect.DeepEqual(negStatus.NetworkEndpointGroups, wantNegStatus.NetworkEndpointGroups) {
		t.Errorf("Failed to validate the DestinationRule annotation, got %v, want %v", negStatus, wantNegStatus)
	}
}

// validateServiceStateAnnotationWithPortNameMap validates all aspects of the service annotation
// and also checks for custon names if specified in given portNameMap
func validateServiceStateAnnotationWithPortNameMap(t *testing.T, svc *apiv1.Service, svcPorts []int32, namer negtypes.NetworkEndpointGroupNamer, portNameMap map[int32]string) {

	negStatus := validateServiceStateAnnotationExceptNames(t, svc, svcPorts)
	for svcPort, name := range portNameMap {
		negName, ok := negStatus.NetworkEndpointGroups[strconv.Itoa(int(svcPort))]
		if !ok {
			t.Fatalf("NEG for port %d was not found", svcPort)
		}
		var expectName = name
		if name == "" {
			expectName = namer.NEG(svc.Namespace, svc.Name, svcPort)
		}
		if negName != expectName {
			t.Fatalf("Expect NEG name of service port %d to be %q, but got %q", svcPort, expectName, negName)
		}
	}
}

func validateServiceStateAnnotation(t *testing.T, svc *apiv1.Service, svcPorts []int32, namer negtypes.NetworkEndpointGroupNamer) {
	t.Helper()

	negStatus := validateServiceStateAnnotationExceptNames(t, svc, svcPorts)
	for _, svcPort := range svcPorts {
		negName, ok := negStatus.NetworkEndpointGroups[strconv.Itoa(int(svcPort))]
		if !ok {
			t.Fatalf("NEG for port %d was not found", svcPort)
		}
		expectName := namer.NEG(svc.Namespace, svc.Name, svcPort)
		if negName != expectName {
			t.Fatalf("Expect NEG name of service port %d to be %q, but got %q", svcPort, expectName, negName)
		}
	}
}

// validateServiceStateAnnotationExceptNames will validate all aspects of the status annotation except for the name
func validateServiceStateAnnotationExceptNames(t *testing.T, svc *apiv1.Service, svcPorts []int32) annotations.NegStatus {
	t.Helper()
	if len(svcPorts) == 0 {
		v, ok := svc.Annotations[annotations.NEGStatusKey]
		if ok {
			t.Fatalf("Expected no NEG service state annotation when there are no servicePorts, got: %v", v)
		}
		return annotations.NegStatus{}
	}

	v, ok := svc.Annotations[annotations.NEGStatusKey]
	if !ok {
		t.Fatalf("Failed to apply the NEG service state annotation, got %+v", svc.Annotations)
	}

	// plain text validation
	for _, port := range svcPorts {
		if !strings.Contains(v, fmt.Sprintf("%v", port)) {
			t.Fatalf("Expected NEG service state annotation to contain port %v, got %v", port, v)
		}
	}

	zoneGetter := negtypes.NewFakeZoneGetter()
	zones, _ := zoneGetter.ListZones()
	for _, zone := range zones {
		if !strings.Contains(v, zone) {
			t.Fatalf("Expected NEG service state annotation to contain zone %v, got %v", zone, v)
		}
	}

	// negStatus validation
	negStatus, err := annotations.ParseNegStatus(v)
	if err != nil {
		t.Fatalf("Failed to parse neg status annotation %q: %v", v, err)
	}

	if len(negStatus.NetworkEndpointGroups) != len(svcPorts) {
		t.Fatalf("Expect # of NEG to be %d, but got %d", len(svcPorts), len(negStatus.NetworkEndpointGroups))
	}

	zoneInStatus := sets.NewString(negStatus.Zones...)
	expectedZones := sets.NewString(zones...)

	if !zoneInStatus.Equal(expectedZones) {
		t.Fatalf("Expect Zone %v, but got %v", expectedZones.List(), zoneInStatus.List())
	}
	return negStatus
}

func generateNegAnnotation(ingress bool, svcPorts []int32) string {
	var annotation annotations.NegAnnotation
	enabledPorts := make(map[int32]annotations.NegAttributes)
	for _, port := range svcPorts {
		enabledPorts[port] = annotations.NegAttributes{}
	}

	annotation.Ingress = ingress
	annotation.ExposedPorts = enabledPorts
	formattedAnnotation, _ := json.Marshal(annotation)
	return string(formattedAnnotation)
}

func generateCustomNamedNegAnnotation(ingress bool, svcPorts map[int32]string) string {
	var annotation annotations.NegAnnotation
	enabledPorts := make(map[int32]annotations.NegAttributes)
	for port, name := range svcPorts {
		if name != "" {
			enabledPorts[port] = annotations.NegAttributes{Name: name}
		} else {
			enabledPorts[port] = annotations.NegAttributes{}
		}
	}

	annotation.Ingress = ingress
	annotation.ExposedPorts = enabledPorts
	formattedAnnotation, _ := json.Marshal(annotation)
	return string(formattedAnnotation)
}

func newTestIngress(name string) *v1beta1.Ingress {
	return &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testServiceNamespace,
		},
		Spec: v1beta1.IngressSpec{
			Backend: &v1beta1.IngressBackend{
				ServiceName: testServiceName,
				ServicePort: intstr.FromString(testNamedPort),
			},
			Rules: []v1beta1.IngressRule{
				{
					IngressRuleValue: v1beta1.IngressRuleValue{
						HTTP: &v1beta1.HTTPIngressRuleValue{
							Paths: []v1beta1.HTTPIngressPath{
								{
									Path: "/path1",
									Backend: v1beta1.IngressBackend{
										ServiceName: testServiceName,
										ServicePort: intstr.FromInt(80),
									},
								},
								{
									Path: "/path2",
									Backend: v1beta1.IngressBackend{
										ServiceName: testServiceName,
										ServicePort: intstr.FromInt(443),
									},
								},
								{
									Path: "/path3",
									Backend: v1beta1.IngressBackend{
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

var ports = []apiv1.ServicePort{
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
	{
		Name:       testNamedPortWithNumber,
		Port:       8881,
		TargetPort: intstr.FromInt(8882),
	},
}

func getTestSvcPortTuple(svcPort int32) negtypes.SvcPortTuple {
	for _, port := range ports {
		if port.Port == svcPort {
			return negtypes.SvcPortTuple{
				Name:       port.Name,
				Port:       port.Port,
				TargetPort: port.TargetPort.String(),
			}
		}
	}
	return negtypes.SvcPortTuple{}
}

func newTestILBService(c *Controller, onlyLocal bool, port int) *apiv1.Service {
	svc := &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        testServiceName,
			Namespace:   testServiceNamespace,
			Annotations: map[string]string{gce.ServiceAnnotationLoadBalancerType: string(gce.LBTypeInternal)},
		},
		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeLoadBalancer,
			Ports: []apiv1.ServicePort{
				{Name: "testport", Port: int32(port)},
			},
		},
	}
	if onlyLocal {
		svc.Spec.ExternalTrafficPolicy = apiv1.ServiceExternalTrafficPolicyTypeLocal
	}

	c.client.CoreV1().Services(testServiceNamespace).Create(context.TODO(), svc, metav1.CreateOptions{})
	return svc
}

func updateTestILBService(c *Controller, onlyLocal bool, svc *apiv1.Service) *apiv1.Service {
	if onlyLocal {
		svc.Spec.ExternalTrafficPolicy = apiv1.ServiceExternalTrafficPolicyTypeLocal
	} else {
		svc.Spec.ExternalTrafficPolicy = apiv1.ServiceExternalTrafficPolicyTypeCluster
	}
	c.client.CoreV1().Services(svc.Namespace).Update(context.TODO(), svc, metav1.UpdateOptions{})
	return svc
}

func newTestService(c *Controller, negIngress bool, negSvcPorts []int32) *apiv1.Service {
	svcAnnotations := map[string]string{}
	if negIngress || len(negSvcPorts) > 0 {
		svcAnnotations[annotations.NEGAnnotationKey] = generateNegAnnotation(negIngress, negSvcPorts)
	}

	// append additional ports if the service does not contain the service port
	for _, port := range negSvcPorts {
		exists := false

		for _, svcPort := range ports {
			if svcPort.Port == port {
				exists = true
				break
			}
		}

		if !exists {
			ports = append(
				ports,
				apiv1.ServicePort{
					Name:       fmt.Sprintf("port%v", port),
					Port:       port,
					TargetPort: intstr.FromString(fmt.Sprintf("%v", port)),
				},
			)
		}
	}

	svc := &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        testServiceName,
			Namespace:   testServiceNamespace,
			Annotations: svcAnnotations,
		},
		Spec: apiv1.ServiceSpec{
			Ports: ports,
		},
	}

	c.client.CoreV1().Services(testServiceNamespace).Create(context.TODO(), svc, metav1.CreateOptions{})
	return svc
}

func newTestServiceCustomNamedNeg(c *Controller, negSvcPorts map[int32]string, ingress bool) *apiv1.Service {
	svcAnnotations := map[string]string{}
	if len(negSvcPorts) > 0 {
		svcAnnotations[annotations.NEGAnnotationKey] = generateCustomNamedNegAnnotation(ingress, negSvcPorts)
	}

	// append additional ports if the service does not contain the service port
	for port := range negSvcPorts {
		exists := false

		for _, svcPort := range ports {
			if svcPort.Port == port {
				exists = true
				break
			}
		}

		if !exists {
			ports = append(
				ports,
				apiv1.ServicePort{
					Name:       fmt.Sprintf("port%v", port),
					Port:       port,
					TargetPort: intstr.FromString(fmt.Sprintf("%v", port)),
				},
			)
		}
	}

	svc := &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        testServiceName,
			Namespace:   testServiceNamespace,
			Annotations: svcAnnotations,
		},
		Spec: apiv1.ServiceSpec{
			Ports: ports,
		},
	}

	c.client.CoreV1().Services(testServiceNamespace).Create(context.TODO(), svc, metav1.CreateOptions{})
	return svc
}

func newTestServiceCus(t *testing.T, c *Controller, namespace, name string, ports []int32) *apiv1.Service {
	svcPorts := []apiv1.ServicePort{}
	for _, port := range ports {
		svcPorts = append(svcPorts,
			apiv1.ServicePort{
				Name:       fmt.Sprintf("port%v", port),
				Port:       port,
				TargetPort: intstr.FromString(fmt.Sprintf("%v", port)),
			},
		)
	}

	svc := &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: apiv1.ServiceSpec{
			Ports:    svcPorts,
			Selector: map[string]string{"v": "v1"},
		},
	}
	c.client.CoreV1().Services(namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
	return svc
}

func newTestDestinationRule(t *testing.T, c *Controller, namespace, name, host string, versions []string) (*istioV1alpha3.DestinationRule, *unstructured.Unstructured) {
	dr := istioV1alpha3.DestinationRule{
		Host: host,
	}
	for _, v := range versions {
		dr.Subsets = append(dr.Subsets, &istioV1alpha3.Subset{Name: v, Labels: map[string]string{"version": v}})
	}
	usDr := unstructured.Unstructured{}
	usDr.SetName(name)
	usDr.SetNamespace(namespace)
	usDr.SetKind("DestinationRule")
	usDr.SetAPIVersion("networking.istio.io/v1alpha3")
	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&dr)
	if err != nil {
		t.Fatalf("failed convert DestinationRule to Unstructured: %v", err)
	}
	usDr.Object["spec"] = spec
	if _, err := c.destinationRuleClient.Namespace(namespace).Create(context.TODO(), &usDr, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create destinationrule: %v", err)
	}
	return &dr, &usDr
}

func helperNewPortInfoMapWithDestinationRule(namespace, name string, svcPortTupleSet negtypes.SvcPortTupleSet, namer negtypes.NetworkEndpointGroupNamer, readinessGate bool,
	destinationRule *istioV1alpha3.DestinationRule) negtypes.PortInfoMap {
	rsl, _ := negtypes.NewPortInfoMapWithDestinationRule(namespace, name, svcPortTupleSet, namer, readinessGate, destinationRule)
	return rsl
}
