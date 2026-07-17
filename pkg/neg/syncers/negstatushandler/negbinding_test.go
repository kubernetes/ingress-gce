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
	"fmt"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	negbindingv1beta1 "k8s.io/ingress-gce/pkg/apis/negbinding/v1beta1"
	composite "k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/types/shared"
	fakenegbinding "k8s.io/ingress-gce/pkg/negbinding/client/clientset/versioned/fake"
	informernegbinding "k8s.io/ingress-gce/pkg/negbinding/client/informers/externalversions/negbinding/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

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
					Type:    NEGsAttached,
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
					Type:    NEGsAttached,
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
					Type:    NEGsAttached,
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
					Type:    NEGsAttached,
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

			negMetrics := metrics.NewNegMetrics()
			h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, negMetrics, klog.TODO())

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
					Type:    NEGsAttached,
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
					Type:    NEGsAttached,
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
						Type:   NEGsAttached,
						Status: metav1.ConditionTrue,
						Reason: NEGsAttachmentSuccessful,
					},
				},
			},
			syncErr:          nil,
			expectedNeedInit: true,
			expectedConditions: []negbindingv1beta1.Condition{
				{
					Type:    NEGsAttached,
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
						Type:   NEGsAttached,
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
					Type:    NEGsAttached,
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
						Type:   NEGsAttached,
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
					Type:    NEGsAttached,
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

			negMetrics := metrics.NewNegMetrics()
			h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, negMetrics, klog.TODO())

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

			negMetrics := metrics.NewNegMetrics()
			h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, negMetrics, klog.TODO())

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

	negMetrics := metrics.NewNegMetrics()
	h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, negMetrics, klog.TODO())

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

	negMetrics := metrics.NewNegMetrics()
	h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, negMetrics, klog.TODO())

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

	negMetrics := metrics.NewNegMetrics()
	h := NewNEGBindingStatusHandler(name, namespace, fakeClient, indexer, negMetrics, klog.TODO())

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
		Type:    NEGsAttached,
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
