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

package negstatushandler

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	k8stesting "k8s.io/client-go/testing"
	negbindingv1beta1 "k8s.io/ingress-gce/pkg/apis/negbinding/v1beta1"
	composite "k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/types/shared"
	fakenegbinding "k8s.io/ingress-gce/pkg/negbinding/client/clientset/versioned/fake"
	informernegbinding "k8s.io/ingress-gce/pkg/negbinding/client/informers/externalversions/negbinding/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

type mockRegistry struct {
	owners map[string]string
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{owners: make(map[string]string)}
}

func (r *mockRegistry) Acquire(negName string, owner string) (bool, string) {
	if current, ok := r.owners[negName]; ok {
		if current == owner {
			return true, ""
		}
		return false, current
	}
	r.owners[negName] = owner
	return true, ""
}

func (r *mockRegistry) ReleaseAllOwnedExcept(owner string, keep sets.Set[string]) {
	for k, v := range r.owners {
		if v == owner && !keep.Has(k) {
			delete(r.owners, k)
		}
	}
}

func (r *mockRegistry) GetOwner(negName string) string {
	return r.owners[negName]
}

func TestReportStatus(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"

	defaultBinding := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
			NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
				{
					Name:   "neg-default",
					Subnet: "default-subnet",
					Zones:  []string{"us-central1-a", "us-central1-b"},
				},
			},
		},
	}

	testCases := []struct {
		desc               string
		negs               []*composite.NetworkEndpointGroup
		errList            []error
		expectedConditions []negbindingv1beta1.Condition
		expectedNegs       []negbindingv1beta1.StatusNegRef
	}{
		{
			desc: "ReportStatus successfully transitions NEGsAttached condition and adds NEG to status",
			negs: []*composite.NetworkEndpointGroup{
				{
					Id:                  12345,
					SelfLink:            "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
					NetworkEndpointType: "GCE_VM_IP_PORT",
					Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
				},
			},
			errList: nil,
			expectedConditions: []negbindingv1beta1.Condition{
				{
					Type:    NegsAttachedCondition,
					Status:  metav1.ConditionTrue,
					Reason:  "NEGsAttachmentSuccessful",
					Message: "NEGs have been successfully attached and synced",
				},
			},
			expectedNegs: []negbindingv1beta1.StatusNegRef{
				{
					ResourceURL: "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
					SubnetURL:   "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
				},
			},
		},
		{
			desc: "ReportStatus successfully transitions NEGsAttached condition and populates multiple NEGs",
			negs: []*composite.NetworkEndpointGroup{
				{
					Id:                  12345,
					SelfLink:            "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
					NetworkEndpointType: "GCE_VM_IP_PORT",
					Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
				},
				{
					Id:                  67890,
					SelfLink:            "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-b/networkEndpointGroups/neg-default",
					NetworkEndpointType: "GCE_VM_IP_PORT",
					Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
				},
			},
			errList: nil,
			expectedConditions: []negbindingv1beta1.Condition{
				{
					Type:    NegsAttachedCondition,
					Status:  metav1.ConditionTrue,
					Reason:  "NEGsAttachmentSuccessful",
					Message: "NEGs have been successfully attached and synced",
				},
			},
			expectedNegs: []negbindingv1beta1.StatusNegRef{
				{
					ResourceURL: "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
					SubnetURL:   "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
				},
				{
					ResourceURL: "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-b/networkEndpointGroups/neg-default",
					SubnetURL:   "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
				},
			},
		},
		{
			desc:    "ReportStatus with initialization errors sets NEGsAttached condition to False",
			negs:    nil,
			errList: []error{fmt.Errorf("GCE API timeout error"), fmt.Errorf("Quota exceeded")},
			expectedConditions: []negbindingv1beta1.Condition{
				{
					Type:    NegsAttachedCondition,
					Status:  metav1.ConditionFalse,
					Reason:  "NEGsAttachmentFailed",
					Message: utilerrors.NewAggregate([]error{fmt.Errorf("GCE API timeout error"), fmt.Errorf("Quota exceeded")}).Error(),
				},
			},
			expectedNegs: nil,
		},
		{
			desc: "ReportStatus with partial success sets NEGsAttached condition to False but populates ensured NEGs list",
			negs: []*composite.NetworkEndpointGroup{
				{
					Id:                  12345,
					SelfLink:            "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
					NetworkEndpointType: "GCE_VM_IP_PORT",
					Subnetwork:          "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
				},
			},
			errList: []error{fmt.Errorf("Quota exceeded for zone us-central1-b")},
			expectedConditions: []negbindingv1beta1.Condition{
				{
					Type:    NegsAttachedCondition,
					Status:  metav1.ConditionFalse,
					Reason:  "NEGsAttachmentFailed",
					Message: utilerrors.NewAggregate([]error{fmt.Errorf("Quota exceeded for zone us-central1-b")}).Error(),
				},
			},
			expectedNegs: []negbindingv1beta1.StatusNegRef{
				{
					ResourceURL: "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
					SubnetURL:   "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeClient := fakenegbinding.NewSimpleClientset()
			indexer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", 0, utils.NewNamespaceIndexer()).GetIndexer()

			indexer.Add(defaultBinding.DeepCopy())
			fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), defaultBinding.DeepCopy(), metav1.CreateOptions{})

			negMetrics := metrics.FakeNegMetrics()
			registry := newMockRegistry()
			h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, negMetrics, registry, klog.TODO())

			err := h.ReportStatus(tc.negs, tc.errList)
			if err != nil {
				t.Fatalf("Reporting status failed unexpectedly: %v", err)
			}

			updatedBinding, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to retrieve updated binding: %v", err)
			}

			// Verify Conditions
			for _, expectedCond := range tc.expectedConditions {
				cond, _, exists := h.findCondition(updatedBinding.Status.Conditions, expectedCond.Type)
				if !exists {
					t.Errorf("Expected condition %s not found in updated status", expectedCond.Type)
					continue
				}
				if cond.Status != expectedCond.Status {
					t.Errorf("Condition %s status got %s, expected %s", expectedCond.Type, cond.Status, expectedCond.Status)
				}
				if cond.Reason != expectedCond.Reason {
					t.Errorf("Condition %s reason got %s, expected %s", expectedCond.Type, cond.Reason, expectedCond.Reason)
				}
				if cond.Message != expectedCond.Message {
					t.Errorf("Condition %s message got %q, expected %q", expectedCond.Type, cond.Message, expectedCond.Message)
				}
			}

			// Verify NEGs lists
			if len(updatedBinding.Status.NetworkEndpointGroups) != 0 || len(tc.expectedNegs) != 0 {
				if !reflect.DeepEqual(updatedBinding.Status.NetworkEndpointGroups, tc.expectedNegs) {
					t.Errorf("NetworkEndpointGroups got %+v, expected %+v", updatedBinding.Status.NetworkEndpointGroups, tc.expectedNegs)
				}
			}

			// Verify LastSyncTime is not modified (should remain zero)
			if !updatedBinding.Status.LastSyncTime.IsZero() {
				t.Errorf("Expected LastSyncTime to be zero, but got %v", updatedBinding.Status.LastSyncTime)
			}
		})
	}
}

func TestReportSyncStatus(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"

	defaultBinding := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
			NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
				{
					Name:   "neg-default",
					Subnet: "default-subnet",
					Zones:  []string{"us-central1-a", "us-central1-b"},
				},
			},
		},
	}

	testCases := []struct {
		desc               string
		initialStatus      negbindingv1beta1.NetworkEndpointGroupBindingStatus
		syncErr            error
		expectedNeedInit   bool
		expectedConditions []negbindingv1beta1.Condition
	}{
		{
			desc:             "Uninitialized status",
			initialStatus:    negbindingv1beta1.NetworkEndpointGroupBindingStatus{},
			syncErr:          nil,
			expectedNeedInit: true,
			expectedConditions: []negbindingv1beta1.Condition{
				{
					Type:    NegsAttachedCondition,
					Status:  metav1.ConditionTrue,
					Reason:  NEGsAttachmentSuccessful,
					Message: "NEGs have been successfully attached and synced",
				},
			},
		},
		{
			desc: "Populated NEGs but missing NEGsAttached condition",
			initialStatus: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
				NetworkEndpointGroups: []negbindingv1beta1.StatusNegRef{
					{
						ResourceURL: "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
						SubnetURL:   "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
					},
				},
			},
			syncErr:          nil,
			expectedNeedInit: true,
			expectedConditions: []negbindingv1beta1.Condition{
				{
					Type:    NegsAttachedCondition,
					Status:  metav1.ConditionTrue,
					Reason:  NEGsAttachmentSuccessful,
					Message: "NEGs have been successfully attached and synced",
				},
			},
		},
		{
			desc: "NEGsAttached condition present but empty NEGs",
			initialStatus: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
				Conditions: []negbindingv1beta1.Condition{
					{
						Type:   NegsAttachedCondition,
						Status: metav1.ConditionTrue,
						Reason: NEGsAttachmentSuccessful,
					},
				},
			},
			syncErr:          nil,
			expectedNeedInit: true,
			expectedConditions: []negbindingv1beta1.Condition{
				{
					Type:    NegsAttachedCondition,
					Status:  metav1.ConditionTrue,
					Reason:  NEGsAttachmentSuccessful,
					Message: "NEGs have been successfully attached and synced",
				},
			},
		},
		{
			desc: "Sync success with initialized condition and populated NEGs",
			initialStatus: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
				Conditions: []negbindingv1beta1.Condition{
					{
						Type:   NegsAttachedCondition,
						Status: metav1.ConditionTrue,
						Reason: NEGsAttachmentSuccessful,
					},
				},
				NetworkEndpointGroups: []negbindingv1beta1.StatusNegRef{
					{
						ResourceURL: "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
						SubnetURL:   "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
					},
				},
			},
			syncErr:          nil,
			expectedNeedInit: false,
			expectedConditions: []negbindingv1beta1.Condition{
				{
					Type:    NegsAttachedCondition,
					Status:  metav1.ConditionTrue,
					Reason:  NEGsAttachmentSuccessful,
					Message: "NEGs have been successfully attached and synced",
				},
			},
		},
		{
			desc: "Sync failure with initialized condition and populated NEGs",
			initialStatus: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
				Conditions: []negbindingv1beta1.Condition{
					{
						Type:   NegsAttachedCondition,
						Status: metav1.ConditionTrue,
						Reason: NEGsAttachmentSuccessful,
					},
				},
				NetworkEndpointGroups: []negbindingv1beta1.StatusNegRef{
					{
						ResourceURL: "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
						SubnetURL:   "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
					},
				},
			},
			syncErr:          fmt.Errorf("syncer failed to reconcile endpoints"),
			expectedNeedInit: false,
			expectedConditions: []negbindingv1beta1.Condition{
				{
					Type:    NegsAttachedCondition,
					Status:  metav1.ConditionFalse,
					Reason:  NEGsAttachmentFailed,
					Message: "syncer failed to reconcile endpoints",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeClient := fakenegbinding.NewSimpleClientset()
			indexer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", 0, utils.NewNamespaceIndexer()).GetIndexer()

			binding := defaultBinding.DeepCopy()
			binding.Status = tc.initialStatus

			indexer.Add(binding.DeepCopy())
			fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), binding.DeepCopy(), metav1.CreateOptions{})

			negMetrics := metrics.FakeNegMetrics()
			registry := newMockRegistry()
			h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, negMetrics, registry, klog.TODO())

			gotNeedInit, err := h.ReportSyncStatus(tc.syncErr)
			if err != nil {
				t.Fatalf("Reporting sync status failed unexpectedly: %v", err)
			}
			if gotNeedInit != tc.expectedNeedInit {
				t.Errorf("ReportSyncStatus() got needInit = %v, expected %v", gotNeedInit, tc.expectedNeedInit)
			}

			updatedBinding, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to retrieve updated binding: %v", err)
			}

			// Verify Conditions
			for _, expectedCond := range tc.expectedConditions {
				cond, _, exists := h.findCondition(updatedBinding.Status.Conditions, expectedCond.Type)
				if !exists {
					t.Errorf("Expected condition %s not found in updated status", expectedCond.Type)
					continue
				}
				if cond.Status != expectedCond.Status {
					t.Errorf("Condition %s status got %s, expected %s", expectedCond.Type, cond.Status, expectedCond.Status)
				}
				if cond.Reason != expectedCond.Reason {
					t.Errorf("Condition %s reason got %s, expected %s", expectedCond.Type, cond.Reason, expectedCond.Reason)
				}
			}

			// Verify LastSyncTime is set during ReportSyncStatus execution
			if updatedBinding.Status.LastSyncTime.IsZero() {
				t.Error("Expected LastSyncTime to be set during ReportSyncStatus, but got zero timestamp")
			}
		})
	}
}

func TestSubnetToZonesMap(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"

	testCases := []struct {
		desc        string
		binding     *negbindingv1beta1.NetworkEndpointGroupBinding
		expectedMap shared.ZonesPerSubnetMap
	}{
		{
			desc: "Empty status returns empty map",
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
					NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
						{
							Name:   "neg-default",
							Subnet: "default-subnet",
							Zones:  []string{"us-central1-a", "us-central1-b"},
						},
					},
				},
			},
			expectedMap: shared.ZonesPerSubnetMap{},
		},
		{
			desc: "Populated status returns correct mapping",
			binding: &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Status: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
					NetworkEndpointGroups: []negbindingv1beta1.StatusNegRef{
						{
							ResourceURL: "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
							SubnetURL:   "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
						},
						{
							ResourceURL: "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-b/networkEndpointGroups/neg-default",
							SubnetURL:   "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
						},
						{
							ResourceURL: "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-c/networkEndpointGroups/neg-non-default",
							SubnetURL:   "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/subnet-b",
						},
					},
				},
			},
			expectedMap: shared.ZonesPerSubnetMap{
				"default-subnet": sets.New("us-central1-a", "us-central1-b"),
				"subnet-b":       sets.New("us-central1-c"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeClient := fakenegbinding.NewSimpleClientset()
			indexer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", 0, utils.NewNamespaceIndexer()).GetIndexer()

			indexer.Add(tc.binding.DeepCopy())

			negMetrics := metrics.FakeNegMetrics()
			registry := newMockRegistry()
			h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, negMetrics, registry, klog.TODO())

			gotMap, err := h.SubnetToZonesMap()
			if err != nil {
				t.Fatalf("SubnetToZonesMap failed unexpectedly: %v", err)
			}

			if !reflect.DeepEqual(gotMap, tc.expectedMap) {
				t.Errorf("SubnetToZonesMap() got %+v, expected %+v", gotMap, tc.expectedMap)
			}
		})
	}
}

func TestSubnetToZonesMapInvalidTypeInCache(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"

	fakeClient := fakenegbinding.NewSimpleClientset()
	indexer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", 0, utils.NewNamespaceIndexer()).GetIndexer()

	negMetrics := metrics.FakeNegMetrics()
	registry := newMockRegistry()
	h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, negMetrics, registry, klog.TODO())

	invalidObj := &metav1.PartialObjectMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
	indexer.Add(invalidObj)

	_, err := h.SubnetToZonesMap()
	if err == nil {
		t.Errorf("SubnetToZonesMap() returned no error, expected error when cache has invalid type")
	} else {
		expectedErr := fmt.Sprintf(`cached object "%s/%s" is of type *v1.PartialObjectMetadata, expected *NetworkEndpointGroupBinding`, namespace, name)
		if err.Error() != expectedErr {
			t.Errorf("SubnetToZonesMap() returned error %q, expected %q", err.Error(), expectedErr)
		}
	}
}

func TestSubnetToZonesMapNotInStore(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"

	fakeClient := fakenegbinding.NewSimpleClientset()
	indexer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", 0, utils.NewNamespaceIndexer()).GetIndexer()

	negMetrics := metrics.FakeNegMetrics()
	registry := newMockRegistry()
	h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, negMetrics, registry, klog.TODO())

	_, err := h.SubnetToZonesMap()
	if err == nil {
		t.Errorf("SubnetToZonesMap() returned no error, expected error when object is not in store")
	} else {
		expectedErr := fmt.Sprintf("negbinding %s/%s is not in store", namespace, name)
		if err.Error() != expectedErr {
			t.Errorf("SubnetToZonesMap() returned error %q, expected %q", err.Error(), expectedErr)
		}
	}
}

func TestPatchStatusNoChanges(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"

	defaultBinding := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
			NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
				{
					Name:   "neg-default",
					Subnet: "default-subnet",
					Zones:  []string{"us-central1-a"},
				},
			},
		},
	}

	fakeClient := fakenegbinding.NewSimpleClientset()
	indexer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", 0, utils.NewNamespaceIndexer()).GetIndexer()

	indexer.Add(defaultBinding.DeepCopy())
	fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), defaultBinding.DeepCopy(), metav1.CreateOptions{})

	negMetrics := metrics.FakeNegMetrics()
	registry := newMockRegistry()
	h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, negMetrics, registry, klog.TODO())

	negs := []*composite.NetworkEndpointGroup{
		{
			Id:         12345,
			SelfLink:   "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
			Subnetwork: "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
		},
	}

	fakeClient.ClearActions()

	// First ReportStatus call - should trigger Patch
	err := h.ReportStatus(negs, nil)
	if err != nil {
		t.Fatalf("First ReportStatus failed: %v", err)
	}

	// Verify one action (Patch) was performed
	actions := fakeClient.Actions()
	if len(actions) != 1 {
		t.Errorf("Expected 1 action (Patch), got %d: %+v", len(actions), actions)
	} else if actions[0].GetVerb() != "patch" {
		t.Errorf("Expected action to be patch, got %s", actions[0].GetVerb())
	}

	// Retrieve updated binding and update indexer (simulating informer sync)
	updatedBinding, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get updated binding: %v", err)
	}

	// Verify updatedBinding has expected status
	expectedNegs := []negbindingv1beta1.StatusNegRef{
		{
			ResourceURL: "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
			SubnetURL:   "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
		},
	}
	if !reflect.DeepEqual(updatedBinding.Status.NetworkEndpointGroups, expectedNegs) {
		t.Errorf("NetworkEndpointGroups got %+v, expected %+v", updatedBinding.Status.NetworkEndpointGroups, expectedNegs)
	}

	expectedCond := negbindingv1beta1.Condition{
		Type:    NegsAttachedCondition,
		Status:  metav1.ConditionTrue,
		Reason:  "NEGsAttachmentSuccessful",
		Message: "NEGs have been successfully attached and synced",
	}
	cond, _, exists := h.findCondition(updatedBinding.Status.Conditions, expectedCond.Type)
	if !exists {
		t.Errorf("Expected condition %s not found in updated status", expectedCond.Type)
	} else {
		if cond.Status != expectedCond.Status {
			t.Errorf("Condition %s status got %s, expected %s", expectedCond.Type, cond.Status, expectedCond.Status)
		}
		if cond.Reason != expectedCond.Reason {
			t.Errorf("Condition %s reason got %s, expected %s", expectedCond.Type, cond.Reason, expectedCond.Reason)
		}
		if cond.Message != expectedCond.Message {
			t.Errorf("Condition %s message got %q, expected %q", expectedCond.Type, cond.Message, expectedCond.Message)
		}
	}

	indexer.Update(updatedBinding)

	// Clear actions to start fresh for the second call
	fakeClient.ClearActions()

	// Second ReportStatus call with same data - should NOT trigger Patch
	err = h.ReportStatus(negs, nil)
	if err != nil {
		t.Fatalf("Second ReportStatus failed: %v", err)
	}

	// Verify no new client actions were performed (Actions list should be empty)
	actions = fakeClient.Actions()
	if len(actions) != 0 {
		t.Errorf("Expected 0 actions on second ReportStatus with same data, got %d: %+v", len(actions), actions)
	}
}

func TestNEGBindingStatusHandlerOwnership(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"

	fakeClient := fakenegbinding.NewSimpleClientset()
	indexer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", 0, utils.NewNamespaceIndexer()).GetIndexer()

	binding := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
			BackendRef: &negbindingv1beta1.BackendRefConfig{Kind: negbindingv1beta1.ServiceKind, Name: "test-svc", Port: 80},
			NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
				{Name: "neg-1", Subnet: "subnet-1"},
				{Name: "neg-2", Subnet: "subnet-2"},
			},
		},
	}
	indexer.Add(binding)
	fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), binding, metav1.CreateOptions{})

	registry := newMockRegistry()
	ownerKey := fmt.Sprintf("%s/%s", namespace, name)
	h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, metrics.FakeNegMetrics(), registry, klog.TODO())

	// Case 1: No conflicts. ReportStatus should succeed normally (Attached=True)
	registry.Acquire("neg-1", ownerKey)
	registry.Acquire("neg-2", ownerKey)

	negs := []*composite.NetworkEndpointGroup{
		{SelfLink: "url-1", Subnetwork: "subnet-url-1"},
		{SelfLink: "url-2", Subnetwork: "subnet-url-2"},
	}
	err := h.ReportStatus(negs, nil)
	if err != nil {
		t.Fatalf("ReportStatus failed: %v", err)
	}

	updatedBinding, _ := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	cond, _, found := h.findCondition(updatedBinding.Status.Conditions, NegsAttachedCondition)
	if !found || cond.Status != metav1.ConditionTrue {
		t.Errorf("Expected NEGsAttached=True, got: %+v", cond)
	}
	cond, _, found = h.findCondition(updatedBinding.Status.Conditions, ManagedCondition)
	if !found || cond.Status != metav1.ConditionTrue {
		t.Errorf("Expected Managed=True, got: %+v", cond)
	}
	if cond.Reason != NEGOwnershipNoConflicts {
		t.Errorf("Expected Reason=%s, got %q", NEGOwnershipNoConflicts, cond.Reason)
	}

	// Case 2: "neg-1" is stolen by other owner
	registry.ReleaseAllOwnedExcept(ownerKey, sets.New("neg-2"))
	otherOwner := "test-namespace/other-binding"
	registry.Acquire("neg-1", otherOwner)

	err = h.ReportStatus(negs, nil)
	if err != nil {
		t.Fatalf("ReportStatus failed: %v", err)
	}

	updatedBinding, _ = fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	cond, _, found = h.findCondition(updatedBinding.Status.Conditions, ManagedCondition)
	if !found || cond.Status != metav1.ConditionFalse {
		t.Errorf("Expected Managed=False, got: %+v", cond)
	}
	if cond.Reason != NEGOwnershipConflict {
		t.Errorf("Expected Reason=NEGOwnershipConflict, got %q", cond.Reason)
	}
	expectedMessage := `NEG "neg-1" is owned by "test-namespace/other-binding"`
	if cond.Message != expectedMessage {
		t.Errorf("Expected Message %q, got %q", expectedMessage, cond.Message)
	}
	cond, _, found = h.findCondition(updatedBinding.Status.Conditions, NegsAttachedCondition)
	if !found || cond.Status != metav1.ConditionTrue {
		t.Errorf("Expected NEGsAttached=True on conflict (no sync error), got: %+v", cond)
	}

	// Case 3: ReportSyncStatus with conflict
	_, err = h.ReportSyncStatus(nil)
	if err != nil {
		t.Fatalf("ReportSyncStatus failed: %v", err)
	}
	updatedBinding, _ = fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	cond, _, found = h.findCondition(updatedBinding.Status.Conditions, ManagedCondition)
	if !found || cond.Status != metav1.ConditionFalse || cond.Reason != NEGOwnershipConflict {
		t.Errorf("Expected Managed=False due to conflict in ReportSyncStatus, got: %+v", cond)
	}
	cond, _, found = h.findCondition(updatedBinding.Status.Conditions, NegsAttachedCondition)
	if !found || cond.Status != metav1.ConditionTrue {
		t.Errorf("Expected NEGsAttached=True due to conflict in ReportSyncStatus (no sync error), got: %+v", cond)
	}

	// Case 4: Conflict resolved. ReportStatus should set Managed=True and NEGsAttached=True
	registry.ReleaseAllOwnedExcept(otherOwner, sets.New[string]()) // release from otherOwner
	registry.Acquire("neg-1", ownerKey)                            // acquire back
	registry.Acquire("neg-2", ownerKey)

	err = h.ReportStatus(negs, nil)
	if err != nil {
		t.Fatalf("ReportStatus failed: %v", err)
	}

	updatedBinding, _ = fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	cond, _, found = h.findCondition(updatedBinding.Status.Conditions, ManagedCondition)
	if !found || cond.Status != metav1.ConditionTrue {
		t.Errorf("Expected Managed=True after conflict resolved, got: %+v", cond)
	}
	if cond.Reason != NEGOwnershipNoConflicts {
		t.Errorf("Expected Reason=%s after conflict resolved, got %q", NEGOwnershipNoConflicts, cond.Reason)
	}
	cond, _, found = h.findCondition(updatedBinding.Status.Conditions, NegsAttachedCondition)
	if !found || cond.Status != metav1.ConditionTrue {
		t.Errorf("Expected NEGsAttached=True after conflict resolved, got: %+v", cond)
	}
}

func TestUpdateBackendRefAttachedCondition(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"

	existingNEGs := []negbindingv1beta1.StatusNegRef{
		{
			ResourceURL: "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
			SubnetURL:   "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
		},
	}

	testCases := []struct {
		desc           string
		initialStatus  negbindingv1beta1.NetworkEndpointGroupBindingStatus
		validationErr  error
		expectedStatus metav1.ConditionStatus
		expectedReason string
		expectedMsg    string
	}{
		{
			desc:           "valid backend ref sets ConditionTrue",
			validationErr:  nil,
			expectedStatus: metav1.ConditionTrue,
			expectedReason: BackendRefValidReason,
			expectedMsg:    "",
		},
		{
			desc:           "ErrInvalidBackendRefKind sets InvalidBackendRefKind reason",
			validationErr:  fmt.Errorf("%w: unsupported Kind %q", ErrInvalidBackendRefKind, "Pod"),
			expectedStatus: metav1.ConditionFalse,
			expectedReason: InvalidBackendRefKindReason,
			expectedMsg:    `unsupported BackendRef Kind: unsupported Kind "Pod"`,
		},
		{
			desc:           "ErrServiceNotFound sets ServiceNotFound reason",
			validationErr:  fmt.Errorf("%w: %s/missing-svc", ErrServiceNotFound, namespace),
			expectedStatus: metav1.ConditionFalse,
			expectedReason: ServiceNotFoundReason,
			expectedMsg:    "service not found: test-namespace/missing-svc",
		},
		{
			desc:           "ErrInvalidBackendRef sets InvalidBackendRef reason",
			validationErr:  ErrInvalidBackendRef,
			expectedStatus: metav1.ConditionFalse,
			expectedReason: InvalidBackendRefReason,
			expectedMsg:    "BackendRef is nil",
		},
		{
			desc:           "unclassified error defaults to InvalidBackendRef reason and preserves existing status fields",
			validationErr:  errors.New("port 80 not found in service test-namespace/test-svc spec"),
			expectedStatus: metav1.ConditionFalse,
			expectedReason: InvalidBackendRefReason,
			expectedMsg:    "port 80 not found in service test-namespace/test-svc spec",
			initialStatus: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
				Conditions: []negbindingv1beta1.Condition{
					{
						Type:   ManagedCondition,
						Status: metav1.ConditionTrue,
						Reason: NEGOwnershipNoConflicts,
					},
					{
						Type:   NegsAttachedCondition,
						Status: metav1.ConditionTrue,
						Reason: NEGsAttachmentSuccessful,
					},
				},
				NetworkEndpointGroups: existingNEGs,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeClient := fakenegbinding.NewSimpleClientset()
			indexer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", 0, utils.NewNamespaceIndexer()).GetIndexer()

			binding := &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Status: tc.initialStatus,
			}
			if err := indexer.Add(binding.DeepCopy()); err != nil {
				t.Fatalf("failed to add binding to indexer: %v", err)
			}
			if _, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), binding.DeepCopy(), metav1.CreateOptions{}); err != nil {
				t.Fatalf("failed to create binding: %v", err)
			}

			h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, metrics.FakeNegMetrics(), newMockRegistry(), klog.TODO())
			if err := h.UpdateBackendRefAttachedCondition(tc.validationErr); err != nil {
				t.Fatalf("UpdateBackendRefAttachedCondition(%v) failed: %v", tc.validationErr, err)
			}

			updatedBinding, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to get updated binding: %v", err)
			}

			cond, _, exists := h.findCondition(updatedBinding.Status.Conditions, BackendRefAttachedCondition)
			if !exists {
				t.Fatalf("expected %s condition to exist", BackendRefAttachedCondition)
			}
			if cond.Status != tc.expectedStatus {
				t.Errorf("got status %s, want %s", cond.Status, tc.expectedStatus)
			}
			if cond.Reason != tc.expectedReason {
				t.Errorf("got reason %q, want %q", cond.Reason, tc.expectedReason)
			}
			if cond.Message != tc.expectedMsg {
				t.Errorf("got message %q, want %q", cond.Message, tc.expectedMsg)
			}
			if cond.LastTransitionTime.IsZero() {
				t.Errorf("expected non-zero LastTransitionTime")
			}

			for _, initCond := range tc.initialStatus.Conditions {
				got, _, ok := h.findCondition(updatedBinding.Status.Conditions, initCond.Type)
				if !ok || got.Status != initCond.Status || got.Reason != initCond.Reason {
					t.Errorf("expected existing condition %+v to be preserved, got %+v (exists=%v)", initCond, got, ok)
				}
			}
			if len(tc.initialStatus.NetworkEndpointGroups) > 0 && !reflect.DeepEqual(updatedBinding.Status.NetworkEndpointGroups, tc.initialStatus.NetworkEndpointGroups) {
				t.Errorf("got NetworkEndpointGroups %+v, want %+v", updatedBinding.Status.NetworkEndpointGroups, tc.initialStatus.NetworkEndpointGroups)
			}
			if !updatedBinding.Status.LastSyncTime.IsZero() {
				t.Errorf("expected LastSyncTime to remain zero, got %v", updatedBinding.Status.LastSyncTime)
			}
		})
	}
}

func TestUpdateBackendRefAttachedConditionTransitionTimeAndNoOp(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"

	initialTime := metav1.NewTime(time.Now().Add(-5 * time.Minute).Truncate(time.Second))
	binding := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
			Conditions: []negbindingv1beta1.Condition{
				{
					Type:               BackendRefAttachedCondition,
					Status:             metav1.ConditionFalse,
					Reason:             ServiceNotFoundReason,
					Message:            "service not found in cache: test-namespace/old-svc",
					LastTransitionTime: initialTime,
				},
			},
		},
	}

	fakeClient := fakenegbinding.NewSimpleClientset()
	indexer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", 0, utils.NewNamespaceIndexer()).GetIndexer()
	if err := indexer.Add(binding.DeepCopy()); err != nil {
		t.Fatalf("failed to add binding to indexer: %v", err)
	}
	if _, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), binding.DeepCopy(), metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create binding: %v", err)
	}

	h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, metrics.FakeNegMetrics(), newMockRegistry(), klog.TODO())

	// Same ConditionFalse status with a new reason/message preserves LastTransitionTime.
	if err := h.UpdateBackendRefAttachedCondition(errors.New("port 8080 missing")); err != nil {
		t.Fatalf("UpdateBackendRefAttachedCondition failed: %v", err)
	}
	updatedBinding, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated binding: %v", err)
	}
	cond, _, exists := h.findCondition(updatedBinding.Status.Conditions, BackendRefAttachedCondition)
	if !exists {
		t.Fatalf("expected %s condition to exist", BackendRefAttachedCondition)
	}
	if !cond.LastTransitionTime.Equal(&initialTime) {
		t.Errorf("expected LastTransitionTime %v to be preserved, got %v", initialTime, cond.LastTransitionTime)
	}
	if cond.Reason != InvalidBackendRefReason {
		t.Errorf("got reason %q, want %q", cond.Reason, InvalidBackendRefReason)
	}

	// Sync indexer and transition ConditionFalse -> ConditionTrue: LastTransitionTime must advance.
	if err := indexer.Update(updatedBinding.DeepCopy()); err != nil {
		t.Fatalf("failed to update indexer: %v", err)
	}
	if err := h.UpdateBackendRefAttachedCondition(nil); err != nil {
		t.Fatalf("UpdateBackendRefAttachedCondition(nil) failed: %v", err)
	}
	updatedBinding, err = fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated binding: %v", err)
	}
	cond, _, exists = h.findCondition(updatedBinding.Status.Conditions, BackendRefAttachedCondition)
	if !exists {
		t.Fatalf("expected %s condition to exist", BackendRefAttachedCondition)
	}
	if !cond.LastTransitionTime.After(initialTime.Time) {
		t.Errorf("expected LastTransitionTime %v to advance past %v", cond.LastTransitionTime, initialTime)
	}

	// Sync indexer and repeat identical ConditionTrue update: must skip K8s Patch ("{}" no-op).
	if err := indexer.Update(updatedBinding.DeepCopy()); err != nil {
		t.Fatalf("failed to update indexer: %v", err)
	}
	fakeClient.ClearActions()
	if err := h.UpdateBackendRefAttachedCondition(nil); err != nil {
		t.Fatalf("second UpdateBackendRefAttachedCondition(nil) failed: %v", err)
	}
	if actions := fakeClient.Actions(); len(actions) != 0 {
		t.Errorf("expected 0 client actions on no-op update, got %d: %+v", len(actions), actions)
	}
}

func TestUpdateBackendRefAttachedConditionErrors(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"

	fakeClient := fakenegbinding.NewSimpleClientset()
	indexer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", 0, utils.NewNamespaceIndexer()).GetIndexer()
	h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, metrics.FakeNegMetrics(), newMockRegistry(), klog.TODO())

	// Missing binding in store returns error without issuing K8s Patch.
	if err := h.UpdateBackendRefAttachedCondition(nil); err == nil {
		t.Fatalf("expected error when binding is missing from store, got nil")
	}
	if actions := fakeClient.Actions(); len(actions) != 0 {
		t.Errorf("expected 0 client actions when binding is missing, got %d", len(actions))
	}

	// Patch failure returns error.
	binding := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
	if err := indexer.Add(binding.DeepCopy()); err != nil {
		t.Fatalf("failed to add binding to indexer: %v", err)
	}
	if _, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), binding.DeepCopy(), metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create binding: %v", err)
	}
	patchErr := errors.New("injected apiserver patch failure")
	fakeClient.PrependReactor("patch", "networkendpointgroupbindings", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, patchErr
	})

	err := h.UpdateBackendRefAttachedCondition(nil)
	if !errors.Is(err, patchErr) {
		t.Fatalf("expected error wrapping %v, got %v", patchErr, err)
	}
}

func TestNEGBindingStatusHandlerStaleIndexer(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"

	fakeClient := fakenegbinding.NewSimpleClientset()
	indexer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", 0, utils.NewNamespaceIndexer()).GetIndexer()

	binding := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			UID:       types.UID("uid-1"),
		},
		Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
			NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
				{Name: "neg-default", Subnet: "default-subnet", Zones: []string{"us-central1-a"}},
			},
		},
	}
	if err := indexer.Add(binding.DeepCopy()); err != nil {
		t.Fatalf("failed to add binding to indexer: %v", err)
	}
	if _, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), binding.DeepCopy(), metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create binding: %v", err)
	}

	registry := newMockRegistry()
	registry.Acquire("neg-default", fmt.Sprintf("%s/%s", namespace, name))
	h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, metrics.FakeNegMetrics(), registry, klog.TODO())

	negs := []*composite.NetworkEndpointGroup{
		{
			Id:         12345,
			SelfLink:   "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
			Subnetwork: "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
		},
	}
	expectedStatusNEGs := []negbindingv1beta1.StatusNegRef{
		{
			ResourceURL: negs[0].SelfLink,
			SubnetURL:   negs[0].Subnetwork,
		},
	}

	// Set BackendRefAttached=True; indexer remains stale (empty Status).
	if err := h.UpdateBackendRefAttachedCondition(nil); err != nil {
		t.Fatalf("UpdateBackendRefAttachedCondition(nil) failed: %v", err)
	}
	updatedBinding, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated binding after initial BackendRefAttached update: %v", err)
	}
	initialBackendCond, _, ok := h.findCondition(updatedBinding.Status.Conditions, BackendRefAttachedCondition)
	if !ok || initialBackendCond.Status != metav1.ConditionTrue {
		t.Fatalf("expected BackendRefAttached=True, got %+v (exists=%v)", initialBackendCond, ok)
	}

	// ReportStatus while indexer still lacks BackendRefAttached.
	if err := h.ReportStatus(negs, nil); err != nil {
		t.Fatalf("ReportStatus failed: %v", err)
	}
	updatedBinding, err = fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated binding after ReportStatus: %v", err)
	}
	for _, wantType := range []string{BackendRefAttachedCondition, ManagedCondition, NegsAttachedCondition} {
		cond, _, exists := h.findCondition(updatedBinding.Status.Conditions, wantType)
		if !exists || cond.Status != metav1.ConditionTrue {
			t.Errorf("expected %s=True after ReportStatus, got %+v (exists=%v)", wantType, cond, exists)
		}
	}
	backendCondAfterReportStatus, _, _ := h.findCondition(updatedBinding.Status.Conditions, BackendRefAttachedCondition)
	if !backendCondAfterReportStatus.LastTransitionTime.Equal(&initialBackendCond.LastTransitionTime) {
		t.Errorf("expected BackendRefAttached LastTransitionTime %v to be preserved, got %v", initialBackendCond.LastTransitionTime, backendCondAfterReportStatus.LastTransitionTime)
	}
	if !reflect.DeepEqual(updatedBinding.Status.NetworkEndpointGroups, expectedStatusNEGs) {
		t.Errorf("got NetworkEndpointGroups %+v after ReportStatus, want %+v", updatedBinding.Status.NetworkEndpointGroups, expectedStatusNEGs)
	}

	// Update BackendRefAttached=False while indexer still lacks Managed, NegsAttached, and NEGs.
	if err := h.UpdateBackendRefAttachedCondition(fmt.Errorf("%w: %s/missing-svc", ErrServiceNotFound, namespace)); err != nil {
		t.Fatalf("UpdateBackendRefAttachedCondition(ErrServiceNotFound) failed: %v", err)
	}
	updatedBinding, err = fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated binding after BackendRefAttached failure update: %v", err)
	}
	backendCond, _, ok := h.findCondition(updatedBinding.Status.Conditions, BackendRefAttachedCondition)
	if !ok || backendCond.Status != metav1.ConditionFalse || backendCond.Reason != ServiceNotFoundReason {
		t.Errorf("expected BackendRefAttached=False (%s), got %+v (exists=%v)", ServiceNotFoundReason, backendCond, ok)
	}
	for _, wantType := range []string{ManagedCondition, NegsAttachedCondition} {
		cond, _, exists := h.findCondition(updatedBinding.Status.Conditions, wantType)
		if !exists || cond.Status != metav1.ConditionTrue {
			t.Errorf("expected %s=True to be preserved after BackendRefAttached update, got %+v (exists=%v)", wantType, cond, exists)
		}
	}
	if !reflect.DeepEqual(updatedBinding.Status.NetworkEndpointGroups, expectedStatusNEGs) {
		t.Errorf("got NetworkEndpointGroups %+v after BackendRefAttached update, want %+v", updatedBinding.Status.NetworkEndpointGroups, expectedStatusNEGs)
	}
}

func TestNEGBindingStatusHandlerSerialization(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"

	fakeClient := fakenegbinding.NewSimpleClientset()
	indexer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", 0, utils.NewNamespaceIndexer()).GetIndexer()

	binding := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			UID:       types.UID("uid-serialized"),
		},
		Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
			NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
				{Name: "neg-default", Subnet: "default-subnet", Zones: []string{"us-central1-a"}},
			},
		},
	}
	if err := indexer.Add(binding.DeepCopy()); err != nil {
		t.Fatalf("failed to add binding to indexer: %v", err)
	}
	if _, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), binding.DeepCopy(), metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create binding: %v", err)
	}

	var inFlight atomic.Int32
	var overlapDetected atomic.Bool
	var isFirstPatch atomic.Bool
	isFirstPatch.Store(true)
	firstPatchEntered := make(chan struct{})
	releaseFirstPatch := make(chan struct{})

	fakeClient.PrependReactor("patch", "networkendpointgroupbindings", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if inFlight.Add(1) > 1 {
			overlapDetected.Store(true)
		}
		defer inFlight.Add(-1)

		if isFirstPatch.CompareAndSwap(true, false) {
			close(firstPatchEntered)
			<-releaseFirstPatch
		}
		return false, nil, nil
	})

	registry := newMockRegistry()
	registry.Acquire("neg-default", fmt.Sprintf("%s/%s", namespace, name))
	h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, metrics.FakeNegMetrics(), registry, klog.TODO())

	negs := []*composite.NetworkEndpointGroup{
		{
			Id:         12345,
			SelfLink:   "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
			Subnetwork: "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
		},
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := h.UpdateBackendRefAttachedCondition(nil); err != nil {
			t.Errorf("UpdateBackendRefAttachedCondition failed: %v", err)
		}
	}()

	// Wait until the first patch is in flight under patchMu, then launch concurrent callers.
	<-firstPatchEntered

	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := h.ReportStatus(negs, nil); err != nil {
			t.Errorf("ReportStatus failed: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := h.ReportSyncStatus(nil); err != nil {
			t.Errorf("ReportSyncStatus failed: %v", err)
		}
	}()

	time.Sleep(10 * time.Millisecond)
	if cur := inFlight.Load(); cur != 1 {
		t.Errorf("expected exactly 1 in-flight patch while patchMu is held, got %d", cur)
	}

	close(releaseFirstPatch)
	wg.Wait()

	if overlapDetected.Load() {
		t.Errorf("detected overlapping in-flight K8s Patch calls; patchMu failed to serialize")
	}

	updatedBinding, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated binding: %v", err)
	}
	for _, wantType := range []string{BackendRefAttachedCondition, ManagedCondition, NegsAttachedCondition} {
		cond, _, exists := h.findCondition(updatedBinding.Status.Conditions, wantType)
		if !exists || cond.Status != metav1.ConditionTrue {
			t.Errorf("expected %s=True after serialized updates, got %+v (exists=%v)", wantType, cond, exists)
		}
	}
	if len(updatedBinding.Status.NetworkEndpointGroups) != 1 {
		t.Errorf("expected 1 status NEG after serialized updates, got %+v", updatedBinding.Status.NetworkEndpointGroups)
	}
	if updatedBinding.Status.LastSyncTime.IsZero() {
		t.Errorf("expected non-zero LastSyncTime after serialized updates")
	}
}

func TestNEGBindingStatusHandlerPatchFailure(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"

	fakeClient := fakenegbinding.NewSimpleClientset()
	indexer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", 0, utils.NewNamespaceIndexer()).GetIndexer()

	binding := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			UID:       types.UID("uid-retry"),
		},
		Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
			NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
				{Name: "neg-default", Subnet: "default-subnet", Zones: []string{"us-central1-a"}},
			},
		},
	}
	if err := indexer.Add(binding.DeepCopy()); err != nil {
		t.Fatalf("failed to add binding to indexer: %v", err)
	}
	if _, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), binding.DeepCopy(), metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create binding: %v", err)
	}

	registry := newMockRegistry()
	registry.Acquire("neg-default", fmt.Sprintf("%s/%s", namespace, name))
	h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, metrics.FakeNegMetrics(), registry, klog.TODO())

	if err := h.UpdateBackendRefAttachedCondition(nil); err != nil {
		t.Fatalf("UpdateBackendRefAttachedCondition failed: %v", err)
	}
	updatedBinding, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated binding: %v", err)
	}
	if err := indexer.Update(updatedBinding.DeepCopy()); err != nil {
		t.Fatalf("failed to update indexer: %v", err)
	}

	var failNextPatch atomic.Bool
	failNextPatch.Store(true)
	patchErr := errors.New("transient patch error")
	fakeClient.PrependReactor("patch", "networkendpointgroupbindings", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if failNextPatch.CompareAndSwap(true, false) {
			return true, nil, patchErr
		}
		return false, nil, nil
	})

	negs := []*composite.NetworkEndpointGroup{
		{
			Id:         12345,
			SelfLink:   "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
			Subnetwork: "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
		},
	}

	// Failed ReportStatus must not advance lastPatchedStatus.
	if err := h.ReportStatus(negs, nil); !errors.Is(err, patchErr) {
		t.Fatalf("expected transient patch error %v, got %v", patchErr, err)
	}

	// Subsequent ReportSyncStatus still sees missing NegsAttachedCondition (needInit=true) because failed ReportStatus was not saved.
	needInit, err := h.ReportSyncStatus(nil)
	if err != nil {
		t.Fatalf("ReportSyncStatus failed: %v", err)
	}
	if !needInit {
		t.Errorf("expected needInit=true after failed ReportStatus did not update lastPatchedStatus, got false")
	}

	// Retry ReportStatus succeeds and preserves BackendRefAttached=True.
	if err := h.ReportStatus(negs, nil); err != nil {
		t.Fatalf("retry ReportStatus failed: %v", err)
	}
	updatedBinding, err = fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated binding: %v", err)
	}
	for _, wantType := range []string{BackendRefAttachedCondition, ManagedCondition, NegsAttachedCondition} {
		cond, _, exists := h.findCondition(updatedBinding.Status.Conditions, wantType)
		if !exists || cond.Status != metav1.ConditionTrue {
			t.Errorf("expected %s=True, got %+v (exists=%v)", wantType, cond, exists)
		}
	}
	if len(updatedBinding.Status.NetworkEndpointGroups) != 1 {
		t.Errorf("expected 1 status NEG after retry, got %+v", updatedBinding.Status.NetworkEndpointGroups)
	}
}

func TestNEGBindingStatusHandlerNoOpSeedsLastPatchedStatus(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"

	transitionTime := metav1.NewTime(time.Now().Add(-2 * time.Minute).Truncate(time.Second))
	binding := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			UID:       types.UID("uid-noop"),
		},
		Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
			NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
				{Name: "neg-default", Subnet: "default-subnet", Zones: []string{"us-central1-a"}},
			},
		},
		Status: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
			Conditions: []negbindingv1beta1.Condition{
				{
					Type:               BackendRefAttachedCondition,
					Status:             metav1.ConditionTrue,
					Reason:             BackendRefValidReason,
					LastTransitionTime: transitionTime,
				},
			},
		},
	}

	fakeClient := fakenegbinding.NewSimpleClientset()
	indexer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", 0, utils.NewNamespaceIndexer()).GetIndexer()
	if err := indexer.Add(binding.DeepCopy()); err != nil {
		t.Fatalf("failed to add binding to indexer: %v", err)
	}
	if _, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), binding.DeepCopy(), metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create binding: %v", err)
	}
	fakeClient.ClearActions()

	registry := newMockRegistry()
	registry.Acquire("neg-default", fmt.Sprintf("%s/%s", namespace, name))
	h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, metrics.FakeNegMetrics(), registry, klog.TODO())

	// Startup no-op ("{}") emits 0 patches while still seeding lastPatchedStatus.
	if err := h.UpdateBackendRefAttachedCondition(nil); err != nil {
		t.Fatalf("UpdateBackendRefAttachedCondition(nil) failed: %v", err)
	}
	if actions := fakeClient.Actions(); len(actions) != 0 {
		t.Fatalf("expected 0 actions on no-op startup update, got %d: %+v", len(actions), actions)
	}

	// Simulate external /status edit removing BackendRefAttached in K8s and informer cache.
	externallyEdited := binding.DeepCopy()
	externallyEdited.Status.Conditions = nil
	if _, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).UpdateStatus(context.TODO(), externallyEdited.DeepCopy(), metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to simulate external status edit: %v", err)
	}
	if err := indexer.Update(externallyEdited.DeepCopy()); err != nil {
		t.Fatalf("failed to update indexer: %v", err)
	}

	// Next syncer update restores BackendRefAttached from seeded lastPatchedStatus.
	if _, err := h.ReportSyncStatus(nil); err != nil {
		t.Fatalf("ReportSyncStatus failed: %v", err)
	}
	updatedBinding, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated binding: %v", err)
	}
	cond, _, exists := h.findCondition(updatedBinding.Status.Conditions, BackendRefAttachedCondition)
	if !exists || cond.Status != metav1.ConditionTrue {
		t.Errorf("expected BackendRefAttached=True to be restored from seeded lastPatchedStatus, got %+v (exists=%v)", cond, exists)
	}
}

func TestNEGBindingStatusHandlerUIDChange(t *testing.T) {
	namespace := "test-namespace"
	name := "test-binding"

	fakeClient := fakenegbinding.NewSimpleClientset()
	indexer := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeClient, "", 0, utils.NewNamespaceIndexer()).GetIndexer()

	bindingU1 := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			UID:       types.UID("uid-1"),
		},
		Spec: negbindingv1beta1.NetworkEndpointGroupBindingSpec{
			NetworkEndpointGroups: []negbindingv1beta1.SpecNegRef{
				{Name: "neg-default", Subnet: "default-subnet", Zones: []string{"us-central1-a"}},
			},
		},
	}
	if err := indexer.Add(bindingU1.DeepCopy()); err != nil {
		t.Fatalf("failed to add bindingU1 to indexer: %v", err)
	}
	if _, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), bindingU1.DeepCopy(), metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create bindingU1: %v", err)
	}

	registry := newMockRegistry()
	registry.Acquire("neg-default", fmt.Sprintf("%s/%s", namespace, name))
	h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, metrics.FakeNegMetrics(), registry, klog.TODO())

	negs := []*composite.NetworkEndpointGroup{
		{
			Id:         12345,
			SelfLink:   "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/networkEndpointGroups/neg-default",
			Subnetwork: "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/default-subnet",
		},
	}
	if err := h.UpdateBackendRefAttachedCondition(nil); err != nil {
		t.Fatalf("UpdateBackendRefAttachedCondition for uid-1 failed: %v", err)
	}
	if err := h.ReportStatus(negs, nil); err != nil {
		t.Fatalf("ReportStatus for uid-1 failed: %v", err)
	}

	// Simulate same-name delete+recreate (U1 -> U2) where the handler is reused.
	bindingU2 := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			UID:       types.UID("uid-2"),
		},
	}
	if err := indexer.Update(bindingU2.DeepCopy()); err != nil {
		t.Fatalf("failed to update indexer with bindingU2: %v", err)
	}
	if err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("failed to delete bindingU1: %v", err)
	}
	if _, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), bindingU2.DeepCopy(), metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create bindingU2: %v", err)
	}

	// Inject 1 transient patch failure on uid-2's first call: uid-1's lastPatchedStatus must be ignored without clearing it.
	var failNextPatch atomic.Bool
	failNextPatch.Store(true)
	patchErr := errors.New("transient uid-2 patch error")
	fakeClient.PrependReactor("patch", "networkendpointgroupbindings", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if failNextPatch.CompareAndSwap(true, false) {
			return true, nil, patchErr
		}
		return false, nil, nil
	})

	if err := h.UpdateBackendRefAttachedCondition(ErrInvalidBackendRef); !errors.Is(err, patchErr) {
		t.Fatalf("expected transient patch error %v, got %v", patchErr, err)
	}
	if h.lastPatchedUID != bindingU1.UID || h.lastPatchedStatus == nil {
		t.Errorf("expected failed uid-2 patch not to overwrite uid-1 cached status, got uid=%q status=%+v", h.lastPatchedUID, h.lastPatchedStatus)
	}

	// Next call on uid-2 must not inherit uid-1's BackendRefAttached=True or NEGs, and updates cache to uid-2 on success.
	if _, err := h.ReportSyncStatus(nil); err != nil {
		t.Fatalf("ReportSyncStatus for uid-2 failed: %v", err)
	}
	if h.lastPatchedUID != bindingU2.UID {
		t.Errorf("expected lastPatchedUID to update to %q after successful uid-2 patch, got %q", bindingU2.UID, h.lastPatchedUID)
	}
	updatedU2, err := fakeClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated bindingU2: %v", err)
	}
	if _, _, exists := h.findCondition(updatedU2.Status.Conditions, BackendRefAttachedCondition); exists {
		t.Errorf("expected uid-1 BackendRefAttached not to carry over onto uid-2 after failed patch, got %+v", updatedU2.Status.Conditions)
	}
	if len(updatedU2.Status.NetworkEndpointGroups) != 0 {
		t.Errorf("expected empty NetworkEndpointGroups on uid-2, got %+v", updatedU2.Status.NetworkEndpointGroups)
	}
}
