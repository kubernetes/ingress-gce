/*
Copyright 2023 The Kubernetes Authors.

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

package zonegetter

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

const defaultTestSubnetURL = "https://www.googleapis.com/compute/v1/projects/proj/regions/us-central1/subnetworks/default"

func TestListZones(t *testing.T) {
	t.Parallel()
	fakeNodeInformer := FakeNodeInformer()
	zoneGetter := NewZoneGetter(fakeNodeInformer, defaultTestSubnetURL)
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeWithProviderID",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-a/bar-node",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	})
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "UnReadyNodeWithProviderID",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-b/bar-node",
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
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeWithoutProviderID",
		},
		Spec: apiv1.NodeSpec{},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	})
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "UnReadyNodeWithoutProviderID",
		},
		Spec: apiv1.NodeSpec{},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionFalse,
				},
			},
		},
	})
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeInvalidProviderID",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://us-central1-c/bar-node",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	})
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "UpgradingNodeWithProviderID",
			Labels: map[string]string{
				"operation.gke.io/type": "drain",
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-f/bar-node",
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
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeWithEmptyZone",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project//bar-node",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	})

	testCases := []struct {
		desc      string
		filter    Filter
		expectLen int
	}{
		{
			desc:      "List with AllNodesFilter",
			filter:    AllNodesFilter,
			expectLen: 3,
		},
		{
			desc:      "List with CandidateNodesFilter",
			filter:    CandidateNodesFilter,
			expectLen: 1,
		},
		{
			desc:      "List with CandidateAndUnreadyNodesFilter",
			filter:    CandidateAndUnreadyNodesFilter,
			expectLen: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			zones, _ := zoneGetter.ListZones(tc.filter, klog.TODO())
			if len(zones) != tc.expectLen {
				t.Errorf("For test case %q, got %d zones, want %d,", tc.desc, len(zones), tc.expectLen)
			}
			for _, zone := range zones {
				if zone == "" {
					t.Errorf("For test case %q, got an empty zone,", tc.desc)
				}
			}
		})

	}
}

func TestListNodes(t *testing.T) {
	t.Parallel()
	fakeNodeInformer := FakeNodeInformer()
	zoneGetter := NewZoneGetter(fakeNodeInformer, defaultTestSubnetURL)
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeWithProviderID",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-a/bar-node",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	})
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "UnReadyNodeWithProviderID",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-b/bar-node",
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
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeWithoutProviderID",
		},
		Spec: apiv1.NodeSpec{},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	})
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "UnReadyNodeWithoutProviderID",
		},
		Spec: apiv1.NodeSpec{},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionFalse,
				},
			},
		},
	})
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeInvalidProviderID",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://us-central1-c/bar-node",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	})
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "UpgradingNodeWithProviderID",
			Labels: map[string]string{
				"operation.gke.io/type": "drain",
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-f/bar-node",
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
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeWithEmptyZone",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project//bar-node",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	})

	testCases := []struct {
		desc      string
		filter    Filter
		expectLen int
	}{
		{
			desc:      "List with AllNodesFilter",
			filter:    AllNodesFilter,
			expectLen: 7,
		},
		{
			desc:      "List with CandidateNodesFilter",
			filter:    CandidateNodesFilter,
			expectLen: 4,
		},
		{
			desc:      "List with CandidateAndUnreadyNodesFilter",
			filter:    CandidateAndUnreadyNodesFilter,
			expectLen: 6,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			nodes, _ := zoneGetter.ListNodes(tc.filter, klog.TODO())
			if len(nodes) != tc.expectLen {
				t.Errorf("For test case %q, got %d nodes, want %d,", tc.desc, len(nodes), tc.expectLen)
			}
		})
	}

}

func TestZoneForNode(t *testing.T) {
	t.Parallel()
	fakeNodeInformer := FakeNodeInformer()
	zoneGetter := NewZoneGetter(fakeNodeInformer, defaultTestSubnetURL)
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "NodeWithValidProviderID",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-a/bar-node",
		},
	})
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "NodeWithInvalidProviderID",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://us-central1-a/bar-node",
		},
	})
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "NodeWithNoProviderID",
		},
		Spec: apiv1.NodeSpec{},
	})
	zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "NodeWithEmptyZone",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project//bar-node",
		},
	})

	testCases := []struct {
		desc       string
		nodeName   string
		expectZone string
		expectErr  error
	}{
		{
			desc:       "Node not found",
			nodeName:   "fooNode",
			expectZone: "",
			expectErr:  ErrNodeNotFound,
		},
		{
			desc:       "Node with valid provider ID",
			nodeName:   "NodeWithValidProviderID",
			expectZone: "us-central1-a",
			expectErr:  nil,
		},
		{
			desc:       "Node with invalid provider ID",
			nodeName:   "NodeWithInvalidProviderID",
			expectZone: "",
			expectErr:  ErrSplitProviderID,
		},
		{
			desc:       "Node with no provider ID",
			nodeName:   "NodeWithNoProviderID",
			expectZone: "",
			expectErr:  ErrProviderIDNotFound,
		},
		{
			desc:       "Node with empty zone in providerID",
			nodeName:   "NodeWithEmptyZone",
			expectZone: "",
			expectErr:  ErrSplitProviderID,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			zone, err := zoneGetter.ZoneForNode(tc.nodeName, klog.TODO())
			if zone != tc.expectZone {
				t.Errorf("For test case %q, got zone: %s, want: %s,", tc.desc, zone, tc.expectZone)
			}
			if !errors.Is(err, tc.expectErr) {
				t.Errorf("For test case %q, got error: %s, want: %s,", tc.desc, err, tc.expectErr)
			}
		})
	}
}

func TestGetZone(t *testing.T) {
	for _, tc := range []struct {
		desc       string
		node       apiv1.Node
		expectZone string
		expectErr  error
	}{
		{
			desc: "Node with valid providerID",
			node: apiv1.Node{
				Spec: apiv1.NodeSpec{
					ProviderID: "gce://foo-project/us-central1-a/bar-node",
				},
			},
			expectZone: "us-central1-a",
			expectErr:  nil,
		},
		{
			desc: "Node with invalid providerID",
			node: apiv1.Node{
				Spec: apiv1.NodeSpec{
					ProviderID: "gce://us-central1-a/bar-node",
				},
			},
			expectZone: "",
			expectErr:  ErrSplitProviderID,
		},
		{
			desc: "Node with no providerID",
			node: apiv1.Node{
				Spec: apiv1.NodeSpec{
					ProviderID: "",
				},
			},
			expectZone: "",
			expectErr:  ErrProviderIDNotFound,
		},
		{
			desc: "Node with empty zone in providerID",
			node: apiv1.Node{
				Spec: apiv1.NodeSpec{
					ProviderID: "gce://foo-project//bar-node",
				},
			},
			expectZone: "",
			expectErr:  ErrSplitProviderID,
		},
	} {
		zone, err := getZone(&tc.node)
		if zone != tc.expectZone {
			t.Errorf("For test case %q, got zone: %s, want: %s,", tc.desc, zone, tc.expectZone)
		}
		if !errors.Is(err, tc.expectErr) {
			t.Errorf("For test case %q, got error: %s, want: %s,", tc.desc, err, tc.expectErr)
		}
	}
}

func TestNonGCPZoneGetter(t *testing.T) {
	zone := "foo"
	zoneGetter := NewNonGCPZoneGetter(zone)
	ret, err := zoneGetter.ListZones(AllNodesFilter, klog.TODO())
	if err != nil {
		t.Errorf("expect err = nil, but got %v", err)
	}
	expectZones := []string{zone}
	if !reflect.DeepEqual(expectZones, ret) {
		t.Errorf("expect list zones = %v, but got %v", expectZones, ret)
	}

	validateGetZoneForNode := func(node string) {
		retZone, err := zoneGetter.ZoneForNode(node, klog.TODO())
		if err != nil {
			t.Errorf("expect err = nil, but got %v", err)
		}

		if retZone != zone {
			t.Errorf("expect zone = %q, but got %q", zone, retZone)
		}
	}
	validateGetZoneForNode("foo-node")
	validateGetZoneForNode("bar-node")
}

func TestGetNodeConditionPredicate(t *testing.T) {
	tests := []struct {
		node                                             apiv1.Node
		expectAccept, expectAcceptByUnreadyNodePredicate bool
		name                                             string
	}{
		{
			node:         apiv1.Node{},
			expectAccept: false,

			name: "empty",
		},
		{
			node: apiv1.Node{
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue},
					},
				},
			},
			expectAccept:                       true,
			expectAcceptByUnreadyNodePredicate: true,
			name:                               "ready node",
		},
		{
			node: apiv1.Node{
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionFalse},
					},
				},
			},
			expectAccept:                       false,
			expectAcceptByUnreadyNodePredicate: true,
			name:                               "unready node",
		},
		{
			node: apiv1.Node{
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionUnknown},
					},
				},
			},
			expectAccept:                       false,
			expectAcceptByUnreadyNodePredicate: true,
			name:                               "ready status unknown",
		},
		{
			node: apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "node1",
					Labels: map[string]string{utils.LabelNodeRoleExcludeBalancer: "true"},
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue},
					},
				},
			},
			expectAccept:                       false,
			expectAcceptByUnreadyNodePredicate: false,
			name:                               "ready node, excluded from loadbalancers",
		},
		{
			node: apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
					Labels: map[string]string{
						utils.GKECurrentOperationLabel: utils.NodeDrain,
					},
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue},
					},
				},
			},
			expectAccept:                       true,
			expectAcceptByUnreadyNodePredicate: false,
			name:                               "ready node, upgrade/drain in progress",
		},
		{
			node: apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
					Labels: map[string]string{
						utils.GKECurrentOperationLabel: "random",
					},
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue},
					},
				},
			},
			expectAccept:                       true,
			expectAcceptByUnreadyNodePredicate: true,
			name:                               "ready node, non-drain operation",
		},
		{
			node: apiv1.Node{
				Spec: apiv1.NodeSpec{Unschedulable: true},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue},
					},
				},
			},
			expectAccept:                       true,
			expectAcceptByUnreadyNodePredicate: true,
			name:                               "unschedulable",
		},
		{
			node: apiv1.Node{
				Spec: apiv1.NodeSpec{
					Taints: []apiv1.Taint{
						{
							Key:    utils.ToBeDeletedTaint,
							Value:  fmt.Sprint(time.Now().Unix()),
							Effect: apiv1.TaintEffectNoSchedule,
						},
					},
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue},
					},
				},
			},
			expectAccept:                       false,
			expectAcceptByUnreadyNodePredicate: false,
			name:                               "ToBeDeletedByClusterAutoscaler-taint",
		},
	}
	pred := candidateNodesPredicate
	unreadyPred := candidateNodesPredicateIncludeUnreadyExcludeUpgradingNodes
	for _, test := range tests {
		accept := pred(&test.node, klog.TODO())
		if accept != test.expectAccept {
			t.Errorf("Test failed for %s, got %v, want %v", test.name, accept, test.expectAccept)
		}
		unreadyAccept := unreadyPred(&test.node, klog.TODO())
		if unreadyAccept != test.expectAcceptByUnreadyNodePredicate {
			t.Errorf("Test failed for unreadyNodesPredicate in case %s, got %v, want %v", test.name, unreadyAccept, test.expectAcceptByUnreadyNodePredicate)
		}
	}
}

func TestGetPredicate(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc       string
		filter     Filter
		expectPred nodeConditionPredicate
		expectNil  bool
	}{

		{
			desc:       "AllNodesFilter",
			filter:     AllNodesFilter,
			expectPred: allNodesPredicate,
			expectNil:  true,
		},
		{
			desc:       "CandidateNodesFilter",
			filter:     CandidateNodesFilter,
			expectPred: candidateNodesPredicate,
			expectNil:  true,
		},
		{
			desc:       "CandidateAndUnreadyNodesFilter",
			filter:     CandidateAndUnreadyNodesFilter,
			expectPred: candidateNodesPredicateIncludeUnreadyExcludeUpgradingNodes,
			expectNil:  true,
		},
		{
			desc:       "No matching Predicate",
			filter:     Filter("random-filter"),
			expectPred: nil,
			expectNil:  false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			gotPred, gotErr := getPredicate(tc.filter)
			if tc.expectNil && gotErr != nil {
				t.Errorf("getPredicate(%s) = got err %v, expect nil", tc.filter, gotErr)
			}
			if !tc.expectNil && gotErr == nil {
				t.Errorf("getPredicate(%s) = got err nil, expect non-nil", tc.filter)
			}
			if reflect.ValueOf(gotPred) != reflect.ValueOf(tc.expectPred) {
				t.Errorf("getPredicate(%s) = got pred %v, expect %v", tc.filter, gotPred, tc.expectPred)
			}
		})
	}
}
