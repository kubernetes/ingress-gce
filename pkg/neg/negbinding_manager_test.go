/*
Copyright 2026 The Kubernetes Authors.

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
	"errors"
	"fmt"
	"testing"
	"time"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	negbindingv1beta1 "k8s.io/ingress-gce/pkg/apis/negbinding/v1beta1"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	"k8s.io/ingress-gce/pkg/neg/readiness"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	fakenegbinding "k8s.io/ingress-gce/pkg/negbinding/client/clientset/versioned/fake"
	informernegbinding "k8s.io/ingress-gce/pkg/negbinding/client/informers/externalversions/negbinding/v1beta1"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

func TestNEGBindingManager(t *testing.T) {
	kubeClient := fake.NewSimpleClientset()
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())

	// Create test Service
	namespace := "test-ns"
	svcName := "test-svc"
	svcPort := int32(80)
	targetPort := "8080"
	testSvc := &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      svcName,
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{
					Port:       svcPort,
					TargetPort: intstr.FromString(targetPort),
				},
			},
		},
	}
	_, err := kubeClient.CoreV1().Services(namespace).Create(context.TODO(), testSvc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	// Create test Binding
	bindingName := "test-binding"
	testBinding := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      bindingName,
		},
		Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
			BackendRef: &negbindingv1beta1.BackendRefConfig{
				Kind: negbindingv1beta1.ServiceKind,
				Name: svcName,
				Port: svcPort,
			},
			NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
				{
					Name:   "neg-1",
					Subnet: "default",
					Zones:  []string{"us-central1-a"},
				},
			},
		},
	}
	fakeNBClient := fakenegbinding.NewSimpleClientset()
	_, err = fakeNBClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), testBinding, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create binding: %v", err)
	}

	// Listers with custom Indexer
	indexers := cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
		ServiceKeyIndex:      ServiceKeyIndexFunc,
	}
	negBindingLister := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeNBClient, "", 0, indexers).GetIndexer()
	err = negBindingLister.Add(testBinding)
	if err != nil {
		t.Fatalf("failed to add binding to lister: %v", err)
	}

	podLister := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	serviceLister := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	err = serviceLister.Add(testSvc)
	if err != nil {
		t.Fatalf("failed to add service to lister: %v", err)
	}
	endpointSliceLister := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	nodeLister := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})

	nodeInformer := zonegetter.FakeNodeInformer()
	zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
	zoneGetter, err := zonegetter.NewFakeZoneGetter(nodeInformer, zonegetter.FakeNodeTopologyInformer(), "https://www.googleapis.com/compute/v1/projects/mock-project/regions/test-region/subnetworks/default", false)
	if err != nil {
		t.Fatalf("failed to create zone getter: %v", err)
	}

	clusterNamer := namer.NewNamer("cluster-id", "", klog.TODO())

	negMetrics := metrics.NewNegMetrics()
	syncerMetrics := metricscollector.FakeSyncerMetrics()
	defaultTestSubnetURL := "https://www.googleapis.com/compute/v1/projects/mock-project/regions/test-region/subnetworks/default"
	cloudAdapter := negtypes.NewAdapterWithNetwork(fakeGCE, "default-network", defaultTestSubnetURL, negMetrics)
	fakeNetworkResolver := network.NewFakeResolver(&network.NetworkInfo{IsDefault: true, NetworkURL: "default-network", SubnetworkURL: defaultTestSubnetURL})

	m := newNEGBindingManager(
		fakeNBClient,
		negBindingLister,
		podLister,
		serviceLister,
		endpointSliceLister,
		nodeLister,
		zoneGetter,
		fakeNetworkResolver,
		cloudAdapter,
		record.NewFakeRecorder(100),
		clusterNamer,
		negMetrics,
		syncerMetrics,
		&readiness.NoopReflector{},
		"kube-system-uid",
		klog.TODO(),
	)

	// Test EnsureSyncerForNEGBinding
	err = m.EnsureSyncerForNEGBinding(testBinding)
	if err != nil {
		t.Fatalf("EnsureSyncerForNEGBinding failed: %v", err)
	}

	// Verify BackendRef condition is True
	updatedBinding, err := fakeNBClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), bindingName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get updated binding: %v", err)
	}
	cond, found := findCondition(updatedBinding.Status.Conditions, "BackendRef")
	if !found {
		t.Fatalf("Condition BackendRef not found")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("Expected BackendRef condition status True, got %s", cond.Status)
	}
	if cond.Reason != "BackendRefValid" {
		t.Errorf("Expected BackendRef condition reason BackendRefValid, got %s", cond.Reason)
	}

	// Verify syncer is created and running
	bindingKey := fmt.Sprintf("%s/%s", namespace, bindingName)
	syncer, ok := m.syncerMap[bindingKey]
	if !ok {
		t.Fatalf("Syncer not found in map for key %s", bindingKey)
	}
	if syncer.IsStopped() {
		t.Errorf("Expected syncer to be running, but it is stopped")
	}

	// Test EnsureSyncersForService
	// First stop it
	m.StopSyncer(namespace, bindingName)
	if _, ok := m.syncerMap[bindingKey]; ok {
		t.Fatalf("Syncer should have been removed from map")
	}

	// Now ensure via service
	err = m.EnsureSyncersForService(namespace, svcName)
	if err != nil {
		t.Fatalf("EnsureSyncersForService failed: %v", err)
	}

	// Verify syncer is back
	syncer, ok = m.syncerMap[bindingKey]
	if !ok {
		t.Fatalf("Syncer not found in map after service ensure")
	}
	if syncer.IsStopped() {
		t.Errorf("Expected syncer to be running, but it is stopped")
	}

	// Test updating binding to have no NEGs
	testBindingNoNEGs := testBinding.DeepCopy()
	testBindingNoNEGs.Spec.NetworkEndpointGroups = []negbindingv1beta1.SpecNegRef{}
	negBindingLister.Update(testBindingNoNEGs)
	err = m.EnsureSyncerForNEGBinding(testBindingNoNEGs)
	if err != nil {
		t.Fatalf("EnsureSyncerForNEGBinding with empty NEGs failed: %v", err)
	}
	if _, ok := m.syncerMap[bindingKey]; ok {
		t.Fatalf("Syncer should have been removed from map when binding has no NEGs")
	}

	// Restore syncer for subsequent tests
	negBindingLister.Update(testBinding)
	err = m.EnsureSyncerForNEGBinding(testBinding)
	if err != nil {
		t.Fatalf("EnsureSyncerForNEGBinding restore failed: %v", err)
	}

	// Simulate service deletion from cache
	err = serviceLister.Delete(testSvc)
	if err != nil {
		t.Fatalf("failed to delete service from lister: %v", err)
	}

	// Test ProcessServiceDeletion (should stop and remove syncer)
	m.ProcessServiceDeletion(namespace, svcName)
	if _, ok := m.syncerMap[bindingKey]; ok {
		t.Fatalf("Syncer should be removed from map after ProcessServiceDeletion")
	}
	if _, ok := m.syncerConfigs[bindingKey]; ok {
		t.Errorf("Expected config to be removed from syncerConfigs after ProcessServiceDeletion")
	}

	// Call EnsureSyncerForNEGBinding (which simulates reconciliation)
	// Since Service is missing, it should fail with ErrServiceNotFound.
	err = m.EnsureSyncerForNEGBinding(testBinding)
	if !errors.Is(err, ErrServiceNotFound) {
		t.Fatalf("Expected EnsureSyncerForNEGBinding to fail with ErrServiceNotFound, got: %v", err)
	}

	// Shutdown
	m.ShutDown()
	if len(m.syncerMap) != 0 {
		t.Errorf("Expected syncerMap to be empty after ShutDown, got size %d", len(m.syncerMap))
	}
}

func TestNEGBindingManagerErrorCases(t *testing.T) {
	namespace := "test-ns"
	svcName := "test-svc"
	svcPort := int32(80)

	testSvc := &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      svcName,
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{
					Port:       svcPort,
					TargetPort: intstr.FromString("8080"),
				},
			},
		},
	}

	testCases := []struct {
		desc           string
		addService     bool
		isMultiNet     bool
		binding        *negbindingv1beta1.NetworkEndpointGroupBinding
		expectedErr    string
		expectedStatus metav1.ConditionStatus
		expectedReason string
	}{
		{
			desc:       "BackendRef is nil",
			addService: false,
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "binding-nil-ref"},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					BackendRef:            nil,
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{{Name: "neg-1", Subnet: "default", Zones: []string{"us-central1-a"}}},
				},
			},
			expectedErr:    ErrInvalidBackendRef.Error(),
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "InvalidBackendRef",
		},
		{
			desc:       "BackendRef Kind is invalid",
			addService: false,
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "binding-invalid-kind"},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					BackendRef: &negbindingv1beta1.BackendRefConfig{
						Kind: "InvalidKind",
						Name: svcName,
						Port: svcPort,
					},
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{{Name: "neg-1", Subnet: "default", Zones: []string{"us-central1-a"}}},
				},
			},
			expectedErr:    fmt.Sprintf("%v: unsupported Kind %q", ErrInvalidBackendRefKind, "InvalidKind"),
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "InvalidBackendRefKind",
		},
		{
			desc:       "Service does not exist",
			addService: false,
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "binding-no-svc"},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					BackendRef: &negbindingv1beta1.BackendRefConfig{
						Kind: negbindingv1beta1.ServiceKind,
						Name: svcName,
						Port: svcPort,
					},
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{{Name: "neg-1", Subnet: "default", Zones: []string{"us-central1-a"}}},
				},
			},
			expectedErr:    fmt.Sprintf("%v: %s/%s", ErrServiceNotFound, namespace, svcName),
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "ServiceNotFound",
		},
		{
			desc:       "Service port mismatch",
			addService: true,
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "binding-2"},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					BackendRef: &negbindingv1beta1.BackendRefConfig{
						Kind: negbindingv1beta1.ServiceKind,
						Name: svcName,
						Port: int32(9999),
					},
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
						{Name: "neg-1", Subnet: "default", Zones: []string{"us-central1-a"}},
					},
				},
			},
			expectedErr:    fmt.Sprintf("port 9999 not found in service %s/%s spec", namespace, svcName),
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "InvalidBackendRef",
		},
		{
			desc:       "Empty NEGs in binding spec",
			addService: true,
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "binding-3"},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					BackendRef: &negbindingv1beta1.BackendRefConfig{
						Kind: negbindingv1beta1.ServiceKind,
						Name: svcName,
						Port: svcPort,
					},
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{},
				},
			},
			expectedErr:    "",
			expectedStatus: metav1.ConditionTrue,
			expectedReason: "BackendRefValid",
		},
		{
			desc:       "Multi-network service unsupported",
			addService: true,
			isMultiNet: true,
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "binding-multinet"},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					BackendRef: &negbindingv1beta1.BackendRefConfig{
						Kind: negbindingv1beta1.ServiceKind,
						Name: svcName,
						Port: svcPort,
					},
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
						{Name: "neg-1", Subnet: "custom-subnet", Zones: []string{"us-central1-a"}},
					},
				},
			},
			expectedErr:    fmt.Sprintf("NEGBinding does not support multi-network, service: %s/%s", namespace, svcName),
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "InvalidBackendRef",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			fakeNBClient := fakenegbinding.NewSimpleClientset()
			indexers := cache.Indexers{
				cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
				ServiceKeyIndex:      ServiceKeyIndexFunc,
			}
			negBindingLister := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeNBClient, "", 0, indexers).GetIndexer()
			_ = negBindingLister.Add(tc.binding)
			_, _ = fakeNBClient.NetworkingV1beta1().NetworkEndpointGroupBindings(tc.binding.Namespace).Create(context.TODO(), tc.binding, metav1.CreateOptions{})

			serviceLister := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			if tc.addService {
				serviceLister.Add(testSvc)
			}
			podLister := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			endpointSliceLister := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			nodeLister := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})

			nodeInformer := zonegetter.FakeNodeInformer()
			zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
			zoneGetter, err := zonegetter.NewFakeZoneGetter(nodeInformer, zonegetter.FakeNodeTopologyInformer(), "https://www.googleapis.com/compute/v1/projects/mock-project/regions/test-region/subnetworks/default", false)
			if err != nil {
				t.Fatalf("failed to create zone getter: %v", err)
			}

			clusterNamer := namer.NewNamer("cluster-id", "", klog.TODO())
			negMetrics := metrics.NewNegMetrics()
			syncerMetrics := metricscollector.FakeSyncerMetrics()
			defaultTestSubnetURL := "https://www.googleapis.com/compute/v1/projects/mock-project/regions/test-region/subnetworks/default"
			cloudAdapter := negtypes.NewAdapterWithNetwork(fakeGCE, "default-network", defaultTestSubnetURL, negMetrics)
			fakeNetworkResolver := network.NewFakeResolver(&network.NetworkInfo{IsDefault: !tc.isMultiNet, NetworkURL: "default-network", SubnetworkURL: defaultTestSubnetURL})

			m := newNEGBindingManager(
				fakeNBClient,
				negBindingLister,
				podLister,
				serviceLister,
				endpointSliceLister,
				nodeLister,
				zoneGetter,
				fakeNetworkResolver,
				cloudAdapter,
				record.NewFakeRecorder(100),
				clusterNamer,
				negMetrics,
				syncerMetrics,
				&readiness.NoopReflector{},
				"kube-system-uid",
				klog.TODO(),
			)

			err = m.EnsureSyncerForNEGBinding(tc.binding)
			if tc.expectedErr != "" {
				if err == nil {
					t.Fatalf("EnsureSyncerForNEGBinding() expected error %q, got nil", tc.expectedErr)
				}
				if err.Error() != tc.expectedErr {
					t.Errorf("EnsureSyncerForNEGBinding() returned error %q, expected %q", err.Error(), tc.expectedErr)
				}
			} else {
				if err != nil {
					t.Fatalf("EnsureSyncerForNEGBinding() expected no error, got: %v", err)
				}
			}

			// Verify that no syncer is created for error/empty cases
			if len(m.syncerMap) != 0 {
				t.Errorf("Expected syncerMap to be empty, got size %d", len(m.syncerMap))
			}

			// Assert condition in fake client
			updatedBinding, err := fakeNBClient.NetworkingV1beta1().NetworkEndpointGroupBindings(tc.binding.Namespace).Get(context.TODO(), tc.binding.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get updated binding: %v", err)
			}

			cond, found := findCondition(updatedBinding.Status.Conditions, "BackendRef")
			if !found {
				t.Fatalf("Condition %s not found in updated status", "BackendRef")
			}

			if cond.Status != tc.expectedStatus {
				t.Errorf("Condition %s status got %s, expected %s", "BackendRef", cond.Status, tc.expectedStatus)
			}
			if cond.Reason != tc.expectedReason {
				t.Errorf("Condition %s reason got %s, expected %s", "BackendRef", cond.Reason, tc.expectedReason)
			}
		})
	}
}

func findCondition(conditions []negbindingv1beta1.Condition, conditionType string) (negbindingv1beta1.Condition, bool) {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition, true
		}
	}
	return negbindingv1beta1.Condition{}, false
}

func TestNEGBindingManagerConflictResolution(t *testing.T) {
	oldFlag := flags.F.EnableMultiSubnetClusterPhase1
	flags.F.EnableMultiSubnetClusterPhase1 = true
	defer func() { flags.F.EnableMultiSubnetClusterPhase1 = oldFlag }()

	kubeClient := fake.NewSimpleClientset()
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())

	namespace := "test-ns"
	svcName := "test-svc"
	svcPort := int32(80)
	targetPort := "8080"
	testSvc := &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: svcName},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{{Port: svcPort, TargetPort: intstr.FromString(targetPort)}},
		},
	}
	_, _ = kubeClient.CoreV1().Services(namespace).Create(context.TODO(), testSvc, metav1.CreateOptions{})

	// B1 wants neg-1
	b1 := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "b1"},
		Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
			BackendRef: &negbindingv1beta1.BackendRefConfig{Kind: negbindingv1beta1.ServiceKind, Name: svcName, Port: svcPort},
			NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
				{Name: "neg-1", Subnet: "default", Zones: []string{"us-central1-a"}},
			},
		},
	}

	// B2 wants neg-1
	b2 := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "b2"},
		Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
			BackendRef: &negbindingv1beta1.BackendRefConfig{Kind: negbindingv1beta1.ServiceKind, Name: svcName, Port: svcPort},
			NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
				{Name: "neg-1", Subnet: "default", Zones: []string{"us-central1-a"}},
			},
		},
	}

	fakeNBClient := fakenegbinding.NewSimpleClientset()
	_, _ = fakeNBClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), b1, metav1.CreateOptions{})
	_, _ = fakeNBClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), b2, metav1.CreateOptions{})

	indexers := cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
		ServiceKeyIndex:      ServiceKeyIndexFunc,
	}
	negBindingLister := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeNBClient, "", 0, indexers).GetIndexer()
	_ = negBindingLister.Add(b1)
	_ = negBindingLister.Add(b2)

	podLister := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	serviceLister := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	_ = serviceLister.Add(testSvc)
	endpointSliceLister := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	nodeLister := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})

	nodeInformer := zonegetter.FakeNodeInformer()
	zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
	zoneGetter, _ := zonegetter.NewFakeZoneGetter(nodeInformer, zonegetter.FakeNodeTopologyInformer(), "https://www.googleapis.com/compute/v1/projects/mock-project/regions/test-region/subnetworks/default", false)

	clusterNamer := namer.NewNamer("cluster-id", "", klog.TODO())
	negMetrics := metrics.NewNegMetrics()
	syncerMetrics := metricscollector.FakeSyncerMetrics()
	defaultTestSubnetURL := "https://www.googleapis.com/compute/v1/projects/mock-project/regions/test-region/subnetworks/default"
	cloudAdapter := negtypes.NewAdapterWithNetwork(fakeGCE, "default-network", defaultTestSubnetURL, negMetrics)
	fakeNetworkResolver := network.NewFakeResolver(&network.NetworkInfo{IsDefault: true, NetworkURL: "default-network", SubnetworkURL: defaultTestSubnetURL})

	m := newNEGBindingManager(
		fakeNBClient,
		negBindingLister,
		podLister,
		serviceLister,
		endpointSliceLister,
		nodeLister,
		zoneGetter,
		fakeNetworkResolver,
		cloudAdapter,
		record.NewFakeRecorder(100),
		clusterNamer,
		negMetrics,
		syncerMetrics,
		&readiness.NoopReflector{},
		"kube-system-uid",
		klog.TODO(),
	)

	// 1. Ensure B1. B1 should acquire neg-1.
	err := m.EnsureSyncerForNEGBinding(b1)
	if err != nil {
		t.Fatalf("EnsureSyncerForNEGBinding(b1) failed: %v", err)
	}
	var owner string
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		owner = m.ownershipRegistry.GetOwner("neg-1")
		return owner == "test-ns/b1", nil
	})
	if err != nil {
		t.Fatalf("Timed out waiting for B1 to acquire neg-1, current owner: %s", owner)
	}

	// 2. Ensure B2. B2 should try to acquire neg-1 but fail due to conflict.
	err = m.EnsureSyncerForNEGBinding(b2)
	if err != nil {
		t.Fatalf("EnsureSyncerForNEGBinding(b2) failed: %v", err)
	}
	// Verify B1 is still owner
	owner = m.ownershipRegistry.GetOwner("neg-1")
	if owner != "test-ns/b1" {
		t.Errorf("Expected owner of neg-1 to remain test-ns/b1, got %s", owner)
	}

	// Delete B1 from cache to simulate deletion before stopping syncer
	_ = negBindingLister.Delete(b1)

	// 3. Stop B1 (simulate deletion). This should release neg-1 and trigger sync of B2.
	m.StopSyncer("test-ns", "b1")

	// Verify B2 acquires neg-1. Since sync is async (runs in syncer goroutine), we poll.
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		owner = m.ownershipRegistry.GetOwner("neg-1")
		return owner == "test-ns/b2", nil
	})
	if err != nil {
		t.Errorf("Timed out waiting for B2 to acquire neg-1, current owner: %s", owner)
	}
}
