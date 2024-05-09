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

	api_v1 "k8s.io/api/core/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

func TestListZones(t *testing.T) {
	t.Parallel()
	fakeNodeInformer := FakeNodeInformer()
	zoneGetter := NewZoneGetter(fakeNodeInformer, defaultTestSubnetURL)
	zoneGetter.onlyIncludeDefaultSubnetNodes = true
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeWithProviderID",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-a/bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}) // Ready node with valid zone.
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "UnReadyNodeWithProviderID",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-b/bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionFalse,
				},
			},
		},
	}) // Unready node with valid zone.
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeWithoutProviderID",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			PodCIDR:  "10.100.1.0/24",
			PodCIDRs: []string{"10.100.1.0/24"},
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}) // Ready node with invalid zone.
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "UnReadyNodeWithoutProviderID",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			PodCIDR:  "10.100.1.0/24",
			PodCIDRs: []string{"10.100.1.0/24"},
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionFalse,
				},
			},
		},
	}) // Unready node with invalid zone.
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeInvalidProviderID",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://us-central1-c/bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}) // Ready node with invalid zone.
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "UpgradingNodeWithProviderID",
			Labels: map[string]string{
				"operation.gke.io/type": "drain",
				utils.LabelNodeSubnet:   defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-f/bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionFalse,
				},
			},
		},
	}) // Upgrading node with valid zone.
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeWithEmptyZone",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project//bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}) // Ready node with invalid zone.
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeWithoutPodCIDR",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-d/bar-node",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}) // Invalid node since PodCIDR isn't populated, with valid zone.
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeWithoutLabel",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-e/bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}) // Ready node with valid zone.
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeInNonDefaultSubnet",
			Labels: map[string]string{
				utils.LabelNodeSubnet: nonDefaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-g/bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}) // Invalid node since it is in the non-default subnet, with valid zone.

	testCases := []struct {
		desc      string
		filter    Filter
		expectLen int
	}{
		{
			desc:      "List with AllNodesFilter",
			filter:    AllNodesFilter,
			expectLen: 4,
		},
		{
			desc:      "List with CandidateNodesFilter",
			filter:    CandidateNodesFilter,
			expectLen: 2,
		},
		{
			desc:      "List with CandidateAndUnreadyNodesFilter",
			filter:    CandidateAndUnreadyNodesFilter,
			expectLen: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			zones, _ := zoneGetter.ListZones(tc.filter, klog.TODO())
			if len(zones) != tc.expectLen {
				t.Errorf("For test case %q, got %d zones, want %d zones", tc.desc, len(zones), tc.expectLen)
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
	zoneGetter.onlyIncludeDefaultSubnetNodes = true
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeWithProviderID",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-a/bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
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
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "UnReadyNodeWithProviderID",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-b/bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
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
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeWithoutProviderID",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			PodCIDR:  "10.100.1.0/24",
			PodCIDRs: []string{"10.100.1.0/24"},
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
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "UnReadyNodeWithoutProviderID",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			PodCIDR:  "10.100.1.0/24",
			PodCIDRs: []string{"10.100.1.0/24"},
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
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeInvalidProviderID",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://us-central1-c/bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
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
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "UpgradingNodeWithProviderID",
			Labels: map[string]string{
				"operation.gke.io/type": "drain",
				utils.LabelNodeSubnet:   defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-f/bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
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
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeWithEmptyZone",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project//bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
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
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeWithoutPodCIDR",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-d/bar-node",
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
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeWithoutLabel",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-e/bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
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
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ReadyNodeInNonDefaultSubnet",
			Labels: map[string]string{
				utils.LabelNodeSubnet: nonDefaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-g/bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
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
			expectLen: 8,
		},
		{
			desc:      "List with CandidateNodesFilter",
			filter:    CandidateNodesFilter,
			expectLen: 5,
		},
		{
			desc:      "List with CandidateAndUnreadyNodesFilter",
			filter:    CandidateAndUnreadyNodesFilter,
			expectLen: 7,
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
	zoneGetter.onlyIncludeDefaultSubnetNodes = true
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "NodeWithValidProviderID",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-a/bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
		},
	})
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "NodeWithInvalidProviderID",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://us-central1-a/bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
		},
	})
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "NodeWithNoProviderID",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			PodCIDR:  "10.100.1.0/24",
			PodCIDRs: []string{"10.100.1.0/24"},
		},
	})
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "NodeWithEmptyZone",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project//bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
		},
	})
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "NodeWithoutPodCIDR",
			Labels: map[string]string{
				utils.LabelNodeSubnet: defaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-d/bar-node",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}) // Invalid node since PodCIDR isn't populated, with valid zone.
	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "NodeWithoutLabel",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-e/bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}) // Ready node with valid zone.

	zoneGetter.nodeLister.Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "NodeInNonDefaultSubnet",
			Labels: map[string]string{
				utils.LabelNodeSubnet: nonDefaultTestSubnet,
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/us-central1-g/bar-node",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"10.100.1.0/24"},
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}) // Invalid node since it is in the non-default subnet, with valid zone.

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
		{
			desc:       "Node without PodCIDR",
			nodeName:   "NodeWithoutPodCIDR",
			expectZone: "",
			expectErr:  ErrNodeNotInDefaultSubnet,
		},
		{
			desc:       "Node without Subnet Label",
			nodeName:   "NodeWithoutLabel",
			expectZone: "us-central1-e",
			expectErr:  nil,
		},
		{
			desc:       "Node in non-default subnet",
			nodeName:   "NodeInNonDefaultSubnet",
			expectZone: "",
			expectErr:  ErrNodeNotInDefaultSubnet,
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

func TestIsNodeSelectedByFilter(t *testing.T) {
	fakeNodeInformer := FakeNodeInformer()
	zoneGetter := NewZoneGetter(fakeNodeInformer, defaultTestSubnetURL)
	zoneGetter.onlyIncludeDefaultSubnetNodes = true

	testCases := []struct {
		node                    apiv1.Node
		expectAcceptByAll       bool
		expectAcceptByCandidate bool
		expectAcceptByUnready   bool
		name                    string
	}{
		{
			node:                    apiv1.Node{},
			expectAcceptByAll:       false,
			expectAcceptByCandidate: false,
			expectAcceptByUnready:   false,
			name:                    "empty",
		},
		{
			node: apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						utils.LabelNodeSubnet: defaultTestSubnet,
					},
				},
				Spec: apiv1.NodeSpec{
					PodCIDR:  "10.100.1.0/24",
					PodCIDRs: []string{"10.100.1.0/24"},
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue},
					},
				},
			},
			expectAcceptByAll:       true,
			expectAcceptByCandidate: true,
			expectAcceptByUnready:   true,
			name:                    "ready node",
		},
		{
			node: apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						utils.LabelNodeSubnet: defaultTestSubnet,
					},
				},
				Spec: apiv1.NodeSpec{
					PodCIDR:  "10.100.1.0/24",
					PodCIDRs: []string{"10.100.1.0/24"},
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionFalse},
					},
				},
			},
			expectAcceptByAll:       true,
			expectAcceptByCandidate: false,
			expectAcceptByUnready:   true,
			name:                    "unready node",
		},
		{
			node: apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						utils.LabelNodeSubnet: defaultTestSubnet,
					},
				},
				Spec: apiv1.NodeSpec{
					PodCIDR:  "10.100.1.0/24",
					PodCIDRs: []string{"10.100.1.0/24"},
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionUnknown},
					},
				},
			},
			expectAcceptByAll:       true,
			expectAcceptByCandidate: false,
			expectAcceptByUnready:   true,
			name:                    "ready status unknown",
		},
		{
			node: apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
					Labels: map[string]string{
						utils.LabelNodeRoleExcludeBalancer: "true",
						utils.LabelNodeSubnet:              defaultTestSubnet,
					},
				},
				Spec: apiv1.NodeSpec{
					PodCIDR:  "10.100.1.0/24",
					PodCIDRs: []string{"10.100.1.0/24"},
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue},
					},
				},
			},
			expectAcceptByAll:       true,
			expectAcceptByCandidate: false,
			expectAcceptByUnready:   false,
			name:                    "ready node, excluded from loadbalancers",
		},
		{
			node: apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
					Labels: map[string]string{
						utils.GKECurrentOperationLabel: utils.NodeDrain,
						utils.LabelNodeSubnet:          defaultTestSubnet,
					},
				},
				Spec: apiv1.NodeSpec{
					PodCIDR:  "10.100.1.0/24",
					PodCIDRs: []string{"10.100.1.0/24"},
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue},
					},
				},
			},
			expectAcceptByAll:       true,
			expectAcceptByCandidate: true,
			expectAcceptByUnready:   false,
			name:                    "ready node, upgrade/drain in progress",
		},
		{
			node: apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
					Labels: map[string]string{
						utils.GKECurrentOperationLabel: "random",
						utils.LabelNodeSubnet:          defaultTestSubnet,
					},
				},
				Spec: apiv1.NodeSpec{
					PodCIDR:  "10.100.1.0/24",
					PodCIDRs: []string{"10.100.1.0/24"},
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue},
					},
				},
			},
			expectAcceptByAll:       true,
			expectAcceptByCandidate: true,
			expectAcceptByUnready:   true,
			name:                    "ready node, non-drain operation",
		},
		{
			node: apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						utils.LabelNodeSubnet: defaultTestSubnet,
					},
				},
				Spec: apiv1.NodeSpec{
					Unschedulable: true,
					PodCIDR:       "10.100.1.0/24",
					PodCIDRs:      []string{"10.100.1.0/24"},
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue},
					},
				},
			},
			expectAcceptByAll:       true,
			expectAcceptByCandidate: true,
			expectAcceptByUnready:   true,
			name:                    "unschedulable",
		},
		{
			node: apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						utils.LabelNodeSubnet: defaultTestSubnet,
					},
				},
				Spec: apiv1.NodeSpec{
					Taints: []apiv1.Taint{
						{
							Key:    utils.ToBeDeletedTaint,
							Value:  fmt.Sprint(time.Now().Unix()),
							Effect: apiv1.TaintEffectNoSchedule,
						},
					},
					PodCIDR:  "10.100.1.0/24",
					PodCIDRs: []string{"10.100.1.0/24"},
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue},
					},
				},
			},
			expectAcceptByAll:       true,
			expectAcceptByCandidate: false,
			expectAcceptByUnready:   false,
			name:                    "ToBeDeletedByClusterAutoscaler-taint",
		},
		{
			node: apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						utils.LabelNodeSubnet: nonDefaultTestSubnet,
					},
				},
				Spec: apiv1.NodeSpec{
					PodCIDR:  "10.100.1.0/24",
					PodCIDRs: []string{"10.100.1.0/24"},
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue},
					},
				},
			},
			expectAcceptByAll:       false,
			expectAcceptByCandidate: false,
			expectAcceptByUnready:   false,
			name:                    "node in non-default subnet",
		},
		{
			node: apiv1.Node{
				Spec: apiv1.NodeSpec{
					PodCIDR:  "10.100.1.0/24",
					PodCIDRs: []string{"10.100.1.0/24"},
				},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue},
					},
				},
			},
			expectAcceptByAll:       true,
			expectAcceptByCandidate: true,
			expectAcceptByUnready:   true,
			name:                    "node without subnet label",
		},
		{
			node: apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						utils.LabelNodeSubnet: defaultTestSubnet,
					},
				},
				Spec: apiv1.NodeSpec{},
				Status: apiv1.NodeStatus{
					Conditions: []apiv1.NodeCondition{
						{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue},
					},
				},
			},
			expectAcceptByAll:       false,
			expectAcceptByCandidate: false,
			expectAcceptByUnready:   false,
			name:                    "node without PodCIDR",
		},
	}
	for _, tc := range testCases {
		acceptByAll := zoneGetter.IsNodeSelectedByFilter(&tc.node, AllNodesFilter, klog.TODO())
		if acceptByAll != tc.expectAcceptByAll {
			t.Errorf("Test failed for %s, got %v, want %v", tc.name, acceptByAll, tc.expectAcceptByAll)
		}

		acceptByCandidate := zoneGetter.IsNodeSelectedByFilter(&tc.node, CandidateNodesFilter, klog.TODO())
		if acceptByCandidate != tc.expectAcceptByCandidate {
			t.Errorf("Test failed for %s, got %v, want %v", tc.name, acceptByCandidate, tc.expectAcceptByCandidate)
		}
		acceptByUnready := zoneGetter.IsNodeSelectedByFilter(&tc.node, CandidateAndUnreadyNodesFilter, klog.TODO())
		if acceptByUnready != tc.expectAcceptByUnready {
			t.Errorf("Test failed for unreadyNodesPredicate in case %s, got %v, want %v", tc.name, acceptByUnready, tc.expectAcceptByUnready)
		}
	}
}

func TestIsNodeInDefaultSubnet(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc string
		node *apiv1.Node
		want bool
	}{
		{
			desc: "Node in the default subnet",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "NodeInDefaultSubnet",
					Labels: map[string]string{
						utils.LabelNodeSubnet: defaultTestSubnet,
					},
				},
				Spec: apiv1.NodeSpec{
					PodCIDR:  "10.100.1.0/24",
					PodCIDRs: []string{"10.100.1.0/24"},
				},
			},
			want: true,
		},
		{
			desc: "Node without PodCIDR",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "NodeWithoutPodCIDR",
					Labels: map[string]string{
						utils.LabelNodeSubnet: defaultTestSubnet,
					},
				},
				Spec: apiv1.NodeSpec{},
			},
			want: false,
		},
		{
			desc: "Node with PodCIDR, without subnet label",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "NodeWithoutSubnetLabel",
				},
				Spec: apiv1.NodeSpec{
					PodCIDR:  "10.100.1.0/24",
					PodCIDRs: []string{"10.100.1.0/24"},
				},
			},
			want: true,
		},
		{
			desc: "Node with PodCIDR, with empty Label",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "NodeWithEmptyLabel",
					Labels: map[string]string{
						utils.LabelNodeSubnet: "",
					},
				},
				Spec: apiv1.NodeSpec{
					PodCIDR:  "10.100.1.0/24",
					PodCIDRs: []string{"10.100.1.0/24"},
				},
			},
			want: true,
		},
		{
			desc: "Node with PodCIDR, with empty Label",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "NodeWithEmptyLabel",
					Labels: map[string]string{
						utils.LabelNodeSubnet: "",
					},
				},
				Spec: apiv1.NodeSpec{
					PodCIDR:  "10.100.1.0/24",
					PodCIDRs: []string{"10.100.1.0/24"},
				},
			},
			want: true,
		},
		{
			desc: "Node in non-default subnet",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "NodeInNonDefaultSubnet",
					Labels: map[string]string{
						utils.LabelNodeSubnet: nonDefaultTestSubnet,
					},
				},
				Spec: api_v1.NodeSpec{
					PodCIDR:  "10.100.1.0/24",
					PodCIDRs: []string{"10.100.1.0/24"},
				},
			},
			want: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			if got := isNodeInDefaultSubnet(tc.node, defaultTestSubnetURL, klog.TODO()); got != tc.want {
				t.Errorf("isNodeInDefaultSubnet(%v, %s) = %v, want %v", tc.node, defaultTestSubnetURL, got, tc.want)
			}
		})
	}
}
