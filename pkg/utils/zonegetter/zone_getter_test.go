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
	"reflect"
	"testing"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

func TestList(t *testing.T) {
	fakeNodeInformer := FakeNodeInformer()
	zoneGetter := NewZoneGetter(fakeNodeInformer)
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

	for _, tc := range []struct {
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
	} {
		zones, _ := zoneGetter.List(tc.filter, klog.TODO())
		if len(zones) != tc.expectLen {
			t.Errorf("For test case %q, got %d zones, want %d,", tc.desc, len(zones), tc.expectLen)
		}
		for _, zone := range zones {
			if zone == "" {
				t.Errorf("For test case %q, got an empty zone,", tc.desc)
			}
		}
	}
}

func TestZoneForNode(t *testing.T) {
	fakeNodeInformer := FakeNodeInformer()
	zoneGetter := NewZoneGetter(fakeNodeInformer)
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

	for _, tc := range []struct {
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
	} {
		zone, err := zoneGetter.ZoneForNode(tc.nodeName, klog.TODO())
		if zone != tc.expectZone {
			t.Errorf("For test case %q, got zone: %s, want: %s,", tc.desc, zone, tc.expectZone)
		}
		if !errors.Is(err, tc.expectErr) {
			t.Errorf("For test case %q, got error: %s, want: %s,", tc.desc, err, tc.expectErr)
		}
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
	ret, err := zoneGetter.List(AllNodesFilter, klog.TODO())
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
