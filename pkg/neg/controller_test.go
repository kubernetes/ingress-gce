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
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	informerv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"

	istioV1alpha3 "istio.io/api/networking/v1alpha3"
	apiv1 "k8s.io/api/core/v1"
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
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/cmconfig"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/flags"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	testServiceNamespace = "test-ns"
	testServiceName      = "test-Name"
	testNamedPort        = "named-Port"
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
		TargetPort: "9376"}
)

func newTestController(kubeClient kubernetes.Interface) *Controller {
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	namer := namer_util.NewNamer(ClusterID, "")
	dynamicSchema := runtime.NewScheme()
	//dynamicSchema.AddKnownTypeWithName(schema.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "List"}, &unstructured.UnstructuredList{})

	kubeClient.CoreV1().ConfigMaps("kube-system").Create(&apiv1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "ingress-controller-config-test"}, Data: map[string]string{"enable-asm": "true"}})

	ctxConfig := context.ControllerContextConfig{
		Namespace:             apiv1.NamespaceAll,
		ResyncPeriod:          1 * time.Second,
		DefaultBackendSvcPort: defaultBackend,
		EnableASMConfigMap:    true,
		ASMConfigMapNamespace: "kube-system",
		ASMConfigMapName:      "ingress-controller-config-test",
	}
	context := context.NewControllerContext(nil, kubeClient, backendConfigClient, nil, gce.NewFakeGCECloud(gce.DefaultTestClusterValues()), namer, ctxConfig)

	// Hack the context.Init func.
	configMapInformer := informerv1.NewConfigMapInformer(kubeClient, context.Namespace, context.ResyncPeriod, utils.NewNamespaceIndexer())
	context.ConfigMapInformer = configMapInformer
	context.ASMConfigController = cmconfig.NewConfigMapConfigController(kubeClient, nil, context.ASMConfigMapNamespace, context.ASMConfigMapName)
	dynamicClient := dynamicfake.NewSimpleDynamicClient(dynamicSchema)

	destrinationGVR := schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1alpha3", Resource: "destinationrules"}
	drDynamicInformer := dynamicinformer.NewFilteredDynamicInformer(dynamicClient, destrinationGVR, context.Namespace, context.ResyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		nil)
	context.DestinationRuleInformer = drDynamicInformer.Informer()
	context.DestinationRuleClient = dynamicClient.Resource(destrinationGVR)

	controller := NewController(
		negtypes.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network"),
		context,
		negtypes.NewFakeZoneGetter(),
		namer,
		1*time.Second,
		1*time.Second,
		// TODO(freehan): enable readiness reflector for unit tests
		false,
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
			svc, err := svcClient.Get(testServiceName, metav1.GetOptions{})
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
	svc, err := svcClient.Get(testServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Service was not created.(*apiv1.Service) successfully, err: %v", err)
	}

	controller.serviceLister.Update(newTestService(controller, true, []int32{}))
	err = controller.processService(svcKey)
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	validateSyncers(t, controller, 3, false)
	svc, err = svcClient.Get(testServiceName, metav1.GetOptions{})
	svcPorts := []int32{80, 8081, 443}
	if err != nil {
		t.Fatalf("Service was not created successfully, err: %v", err)
	}
	validateServiceStateAnnotation(t, svc, svcPorts, controller.namer)
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
	svc, err := svcClient.Get(testServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Service was not created.(*apiv1.Service) successfully, err: %v", err)
	}

	controller.serviceLister.Update(newTestService(controller, true, []int32{}))
	err = controller.processService(svcKey)
	if err != nil {
		t.Fatalf("Failed to process service: %v", err)
	}
	validateSyncers(t, controller, 3, false)
	svc, err = svcClient.Get(testServiceName, metav1.GetOptions{})
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
	}

	for _, tc := range testCases {
		controller := newTestController(fake.NewSimpleClientset())
		defer controller.stop()
		portMap := gatherPortMappingUsedByIngress(tc.ings, newTestService(controller, true, []int32{}))
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
			portMap: negtypes.NewPortInfoMap(namespace, name, negtypes.SvcPortMap{80: "named_port", 443: "other_port"}, namer, false),
		},
		{
			desc:            "same annotation applied twice",
			previousPortMap: negtypes.NewPortInfoMap(namespace, name, negtypes.SvcPortMap{80: "named_port", 4040: "other_port"}, namer, false),
			portMap:         negtypes.NewPortInfoMap(namespace, name, negtypes.SvcPortMap{80: "named_port", 4040: "other_port"}, namer, false),
		},
		{
			desc:            "apply new annotation and override previous annotation",
			previousPortMap: negtypes.NewPortInfoMap(namespace, name, negtypes.SvcPortMap{80: "named_port", 4040: "other_port"}, namer, false),
			portMap:         negtypes.NewPortInfoMap(namespace, name, negtypes.SvcPortMap{3000: "6000", 4000: "8000"}, namer, false),
		},
		{
			desc:            "remove previous annotation",
			previousPortMap: negtypes.NewPortInfoMap(namespace, name, negtypes.SvcPortMap{80: "named_port", 4040: "other_port"}, namer, false),
		},
		{
			desc: "remove annotation with no previous annotation",
		},
		{
			desc:            "readiness gate makes no difference 1",
			previousPortMap: negtypes.NewPortInfoMap(namespace, name, negtypes.SvcPortMap{80: "named_port", 4040: "other_port"}, namer, false),
			portMap:         negtypes.NewPortInfoMap(namespace, name, negtypes.SvcPortMap{3000: "6000", 4000: "8000"}, namer, true),
		},
		{
			desc:            "readiness gate makes no difference 2",
			previousPortMap: negtypes.NewPortInfoMap(namespace, name, negtypes.SvcPortMap{80: "named_port", 4040: "other_port"}, namer, true),
			portMap:         negtypes.NewPortInfoMap(namespace, name, negtypes.SvcPortMap{80: "named_port", 4040: "other_port"}, namer, false),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			controller.syncNegStatusAnnotation(namespace, name, tc.previousPortMap)
			svc, _ := svcClient.Get(name, metav1.GetOptions{})

			var oldSvcPorts []int32
			for port := range tc.previousPortMap {
				oldSvcPorts = append(oldSvcPorts, port.ServicePort)
			}
			validateServiceStateAnnotation(t, svc, oldSvcPorts, controller.namer)

			controller.syncNegStatusAnnotation(namespace, name, tc.portMap)
			svc, _ = svcClient.Get(name, metav1.GetOptions{})

			var svcPorts []int32
			for port := range tc.portMap {
				svcPorts = append(svcPorts, port.ServicePort)
			}
			validateServiceStateAnnotation(t, svc, svcPorts, controller.namer)
		})
	}
}

func TestDefaultBackendServicePortInfoMap(t *testing.T) {
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
				negtypes.SvcPortMap{80: defaultBackend.TargetPort},
				controller.namer,
				false,
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
				negtypes.SvcPortMap{80: "8888"},
				controller.namer,
				false,
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

			newIng, err := controller.client.NetworkingV1beta1().Ingresses(testServiceNamespace).Create(ing)
			if err != nil {
				t.Fatal(err)
			}

			// Add to cache directly since the cache doesn't get updated
			if err := controller.ingressLister.Add(newIng); err != nil {
				t.Fatal(err)
			}
			result := make(negtypes.PortInfoMap)
			controller.mergeDefaultBackendServicePortInfoMap(controller.defaultBackendService.ID.Service.String(), result)
			if !reflect.DeepEqual(tc.want, result) {
				t.Fatalf("got %+v, want %+v", result, tc.want)
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
				negtypes.SvcPortMap{80: "80", 90: "90"},
				controllerHelper.namer,
				false,
			),
			wantDRPortMap: helperNewPortInfoMapWithDestinationRule(
				"namespace1",
				"service1",
				negtypes.SvcPortMap{80: "80", 90: "90"},
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
				negtypes.SvcPortMap{80: "80", 90: "90"},
				controllerHelper.namer,
				false,
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
			controller.client.CoreV1().Services(tc.service.GetNamespace()).Create(tc.service)

			expectedPortInfoMap := negtypes.PortInfoMap{}
			expectedPortInfoMap.Merge(tc.wantSvcPortMap)
			if tc.wantDRPortMap != nil {
				expectedPortInfoMap.Merge(tc.wantDRPortMap)
			}

			if tc.usDestinationRule != nil {
				controller.destinationRuleLister.Add(tc.usDestinationRule)
				if _, err := controller.destinationRuleClient.Namespace(tc.usDestinationRule.GetNamespace()).Create(tc.usDestinationRule,
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
			svc, err := svcClient.Get(tc.service.GetName(), metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Service was not created successfully, err: %v", err)
			}
			validateServiceAnnotationWithPortInfoMap(t, svc, tc.wantSvcPortMap)
			if tc.usDestinationRule != nil {
				usdr, err := controller.destinationRuleClient.Namespace(tc.usDestinationRule.GetNamespace()).Get(tc.usDestinationRule.GetName(), metav1.GetOptions{})
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
				negtypes.SvcPortMap{80: "80", 90: "90"},
				controller.namer,
				false,
			),
			wantDRPortMap: helperNewPortInfoMapWithDestinationRule(
				"namespace1",
				"service1",
				negtypes.SvcPortMap{80: "80", 90: "90"},
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
				negtypes.SvcPortMap{90: "90"},
				controller.namer,
				false,
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

func validateServiceStateAnnotation(t *testing.T, svc *apiv1.Service, svcPorts []int32, namer negtypes.NetworkEndpointGroupNamer) {
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

	zoneInStatus := sets.NewString(negStatus.Zones...)
	expectedZones := sets.NewString(zones...)

	if !zoneInStatus.Equal(expectedZones) {
		t.Fatalf("Expect Zone %v, but got %v", expectedZones.List(), zoneInStatus.List())
	}
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

func newTestService(c *Controller, negIngress bool, negSvcPorts []int32) *apiv1.Service {
	svcAnnotations := map[string]string{}
	if negIngress || len(negSvcPorts) > 0 {
		svcAnnotations[annotations.NEGAnnotationKey] = generateNegAnnotation(negIngress, negSvcPorts)
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

	c.client.CoreV1().Services(testServiceNamespace).Create(svc)
	return svc
}

func newTestServiceCus(t *testing.T, c *Controller, namespace, name string, ports []int32) *apiv1.Service {
	svcPorts := []apiv1.ServicePort{}
	for _, port := range ports {
		svcPorts = append(svcPorts,
			apiv1.ServicePort{
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
	c.client.CoreV1().Services(namespace).Create(svc)
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
	if _, err := c.destinationRuleClient.Namespace(namespace).Create(&usDr, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create destinationrule: %v", err)
	}
	return &dr, &usDr
}

func helperNewPortInfoMapWithDestinationRule(namespace, name string, svcPortMap negtypes.SvcPortMap, namer negtypes.NetworkEndpointGroupNamer, readinessGate bool,
	destinationRule *istioV1alpha3.DestinationRule) negtypes.PortInfoMap {
	rsl, _ := negtypes.NewPortInfoMapWithDestinationRule(namespace, name, svcPortMap, namer, readinessGate, destinationRule)
	return rsl
}
