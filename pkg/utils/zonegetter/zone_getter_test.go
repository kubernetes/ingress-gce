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
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

func TestListZones(t *testing.T) {
	t.Parallel()

	nodeInformer := FakeNodeInformer()
	PopulateFakeNodeInformer(nodeInformer, false)
	zoneGetter := NewFakeZoneGetter(nodeInformer, defaultTestSubnetURL, false)
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
			expectLen: 3,
		},
		{
			desc:      "List with CandidateAndUnreadyNodesFilter",
			filter:    CandidateAndUnreadyNodesFilter,
			expectLen: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			for _, enableMultiSubnetCluster := range []bool{true, false} {
				zoneGetter.onlyIncludeDefaultSubnetNodes = enableMultiSubnetCluster
				zones, _ := zoneGetter.ListZones(tc.filter, klog.TODO())
				if len(zones) != tc.expectLen {
					t.Errorf("For test case %q with onlyIncludeDefaultSubnetNodes = %v, got %d zones, want %d zones", tc.desc, enableMultiSubnetCluster, len(zones), tc.expectLen)
				}
				for _, zone := range zones {
					if zone == "" {
						t.Errorf("For test case %q with onlyIncludeDefaultSubnetNodes = %v, got an empty zone,", tc.desc, enableMultiSubnetCluster)
					}
				}
			}
		})
	}
}

func TestListZonesMultipleSubnets(t *testing.T) {
	t.Parallel()

	nodeInformer := FakeNodeInformer()
	PopulateFakeNodeInformer(nodeInformer, true)
	zoneGetter := NewFakeZoneGetter(nodeInformer, defaultTestSubnetURL, true)

	testCases := []struct {
		desc      string
		filter    Filter
		expectLen int
	}{
		{
			desc:      "List with AllNodesFilter",
			filter:    AllNodesFilter,
			expectLen: 6,
		},
		{
			desc:      "List with CandidateNodesFilter",
			filter:    CandidateNodesFilter,
			expectLen: 5,
		},
		{
			desc:      "List with CandidateAndUnreadyNodesFilter",
			filter:    CandidateAndUnreadyNodesFilter,
			expectLen: 5,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			zones, _ := zoneGetter.ListZones(tc.filter, klog.TODO())
			if len(zones) != tc.expectLen {
				t.Errorf("For test case %q with multi subnet cluster enabled, got %d zones, want %d zones", tc.desc, len(zones), tc.expectLen)
			}
			for _, zone := range zones {
				if zone == "" {
					t.Errorf("For test case %q with multi subnet cluster enabled, got an empty zone,", tc.desc)
				}
			}
		})
	}
}

func TestListNodes(t *testing.T) {
	t.Parallel()

	nodeInformer := FakeNodeInformer()
	PopulateFakeNodeInformer(nodeInformer, false)
	zoneGetter := NewFakeZoneGetter(nodeInformer, defaultTestSubnetURL, false)

	testCases := []struct {
		desc      string
		filter    Filter
		expectLen int
	}{
		{
			desc:      "List with AllNodesFilter",
			filter:    AllNodesFilter,
			expectLen: 13,
		},
		{
			desc:      "List with CandidateNodesFilter",
			filter:    CandidateNodesFilter,
			expectLen: 11,
		},
		{
			desc:      "List with CandidateAndUnreadyNodesFilter",
			filter:    CandidateAndUnreadyNodesFilter,
			expectLen: 9,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			for _, enableMultiSubnetCluster := range []bool{true, false} {
				zoneGetter.onlyIncludeDefaultSubnetNodes = enableMultiSubnetCluster
				nodes, _ := zoneGetter.ListNodes(tc.filter, klog.TODO())
				if len(nodes) != tc.expectLen {
					t.Errorf("For test case %q with onlyIncludeDefaultSubnetNodes = %v, got %d nodes, want %d,", tc.desc, enableMultiSubnetCluster, len(nodes), tc.expectLen)
				}
			}
		})
	}
}

func TestListNodesMultipleSubnets(t *testing.T) {
	t.Parallel()

	nodeInformer := FakeNodeInformer()
	PopulateFakeNodeInformer(nodeInformer, true)
	zoneGetter := NewFakeZoneGetter(nodeInformer, defaultTestSubnetURL, true)

	testCases := []struct {
		desc      string
		filter    Filter
		expectLen int
	}{
		{
			desc:      "List with AllNodesFilter",
			filter:    AllNodesFilter,
			expectLen: 15,
		},
		{
			desc:      "List with CandidateNodesFilter",
			filter:    CandidateNodesFilter,
			expectLen: 13,
		},
		{
			desc:      "List with CandidateAndUnreadyNodesFilter",
			filter:    CandidateAndUnreadyNodesFilter,
			expectLen: 11,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			nodes, _ := zoneGetter.ListNodes(tc.filter, klog.TODO())
			if len(nodes) != tc.expectLen {
				t.Errorf("For test case %q with multi-subnet cluster enabled, got %d nodes, want %d,", tc.desc, len(nodes), tc.expectLen)
			}
		})
	}
}

func TestZoneAndSubnetForNode(t *testing.T) {
	nodeInformer := FakeNodeInformer()
	PopulateFakeNodeInformer(nodeInformer, false)
	zoneGetter := NewFakeZoneGetter(nodeInformer, defaultTestSubnetURL, false)

	testCases := []struct {
		desc           string
		nodeName       string
		expectZone     string
		expectedSubnet string
		expectErr      error
	}{
		{
			desc:           "Node not found",
			nodeName:       "fooNode",
			expectZone:     "",
			expectedSubnet: "",
			expectErr:      ErrNodeNotFound,
		},
		{
			desc:           "Node with valid provider ID",
			nodeName:       "instance1",
			expectZone:     "zone1",
			expectedSubnet: defaultTestSubnet,
			expectErr:      nil,
		},
		{
			desc:           "Node with invalid provider ID",
			nodeName:       "instance-invalid-providerID",
			expectZone:     "",
			expectedSubnet: "",
			expectErr:      ErrSplitProviderID,
		},
		{
			desc:           "Node with no provider ID",
			nodeName:       "instance-empty-providerID",
			expectZone:     "",
			expectedSubnet: "",
			expectErr:      ErrProviderIDNotFound,
		},
		{
			desc:           "Node with empty zone in providerID",
			nodeName:       "instance-empty-zone-providerID",
			expectZone:     "",
			expectedSubnet: "",
			expectErr:      ErrSplitProviderID,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			for _, enableMultiSubnetCluster := range []bool{true, false} {
				zoneGetter.onlyIncludeDefaultSubnetNodes = enableMultiSubnetCluster
				zone, _, err := zoneGetter.ZoneAndSubnetForNode(tc.nodeName, klog.TODO())
				if zone != tc.expectZone {
					t.Errorf("For test case %q with onlyIncludeDefaultSubnetNodes = %v , got zone: %s, want: %s,", tc.desc, enableMultiSubnetCluster, zone, tc.expectZone)
				}

				if !errors.Is(err, tc.expectErr) {
					t.Errorf("For test case %q with onlyIncludeDefaultSubnetNodes = %v, got error: %s, want: %s,", tc.desc, enableMultiSubnetCluster, err, tc.expectErr)
				}
			}

			for _, enableMultiSubnetClusterPhase1 := range []bool{true, false} {
				flags.F.EnableMultiSubnetClusterPhase1 = enableMultiSubnetClusterPhase1
				zone, subnet, err := zoneGetter.ZoneAndSubnetForNode(tc.nodeName, klog.TODO())
				if zone != tc.expectZone {
					t.Errorf("For test case %q with EnableMultiSubnetClusterPhase1 = %v , got zone: %s, want: %s,", tc.desc, enableMultiSubnetClusterPhase1, zone, tc.expectZone)
				}
				if enableMultiSubnetClusterPhase1 && subnet != tc.expectedSubnet {
					t.Errorf("For test case %q with EnableMultiSubnetClusterPhase1 = %v , got subnet: %s, want: %s,", tc.desc, enableMultiSubnetClusterPhase1, subnet, tc.expectedSubnet)
				}
				if !errors.Is(err, tc.expectErr) {
					t.Errorf("For test case %q with EnableMultiSubnetClusterPhase1 = %v, got error: %s, want: %s,", tc.desc, enableMultiSubnetClusterPhase1, err, tc.expectErr)
				}
			}
		})
	}
}

func TestZoneForNodeMultipleSubnets(t *testing.T) {
	t.Parallel()

	nodeInformer := FakeNodeInformer()
	PopulateFakeNodeInformer(nodeInformer, true)
	zoneGetter := NewFakeZoneGetter(nodeInformer, defaultTestSubnetURL, true)

	testCases := []struct {
		desc       string
		nodeName   string
		expectZone string
		expectErr  error
	}{
		{
			desc:       "Node with default Subnet Label",
			nodeName:   "default-subnet-label-instance",
			expectZone: "zone5",
			expectErr:  nil,
		},
		{
			desc:       "Node with empty Subnet Label",
			nodeName:   "empty-subnet-label-instance",
			expectZone: "zone6",
			expectErr:  nil,
		},
		{
			desc:       "Node without PodCIDR",
			nodeName:   "no-podcidr-instance",
			expectZone: "",
			expectErr:  ErrNodePodCIDRNotSet,
		},
		{
			desc:       "Node in non-default subnet",
			nodeName:   "non-default-subnet-instance",
			expectZone: "",
			expectErr:  ErrNodeNotInDefaultSubnet,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			zone, _, err := zoneGetter.ZoneAndSubnetForNode(tc.nodeName, klog.TODO())
			if zone != tc.expectZone {
				t.Errorf("For test case %q with multi-subnet cluster enabled, got zone: %s, want: %s,", tc.desc, zone, tc.expectZone)
			}
			if !errors.Is(err, tc.expectErr) {
				t.Errorf("For test case %q with multi-subnet cluster enabled, got error: %s, want: %s,", tc.desc, err, tc.expectErr)
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

func TestGetSubnet(t *testing.T) {
	t.Parallel()

	nodeInformer := FakeNodeInformer()
	PopulateFakeNodeInformer(nodeInformer, true)
	zoneGetter := NewFakeZoneGetter(nodeInformer, defaultTestSubnetURL, true)

	for _, tc := range []struct {
		desc         string
		node         apiv1.Node
		expectSubnet string
		expectErr    error
	}{
		{
			desc: "Node with subnet label value as defaultSubnet",
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
			},
			expectSubnet: defaultTestSubnet,
			expectErr:    nil,
		},
		{
			desc: "Node with subnet label value as empty string",
			node: apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						utils.LabelNodeSubnet: "",
					},
				},
				Spec: apiv1.NodeSpec{
					PodCIDR:  "10.100.1.0/24",
					PodCIDRs: []string{"10.100.1.0/24"},
				},
			},
			expectSubnet: defaultTestSubnet,
			expectErr:    nil,
		},
		{
			desc: "Node without subnet label",
			node: apiv1.Node{
				Spec: apiv1.NodeSpec{
					PodCIDR:  "10.100.1.0/24",
					PodCIDRs: []string{"10.100.1.0/24"},
				},
			},
			expectSubnet: defaultTestSubnet,
			expectErr:    nil,
		},
		{
			desc:         "Node without PodCIDR",
			node:         apiv1.Node{},
			expectSubnet: "",
			expectErr:    ErrNodePodCIDRNotSet,
		},
	} {
		subnet, err := zoneGetter.getSubnet(&tc.node, defaultTestSubnetURL)
		if subnet != tc.expectSubnet {
			t.Errorf("For test case %q, got subnet: %s, want: %s,", tc.desc, subnet, tc.expectSubnet)
		}
		if !errors.Is(err, tc.expectErr) {
			t.Errorf("For test case %q, got error: %s, want: %s,", tc.desc, err, tc.expectErr)
		}
	}
}

func TestNonGCPZoneGetter(t *testing.T) {
	zone := "foo"
	subnet := ""
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
		retZone, retSubnet, err := zoneGetter.ZoneAndSubnetForNode(node, klog.TODO())
		if err != nil {
			t.Errorf("expect err = nil, but got %v", err)
		}

		if retZone != zone {
			t.Errorf("expect zone = %q, but got %q", zone, retZone)
		}

		if retSubnet != subnet {
			t.Errorf("expect subnet = %q, but got %q", subnet, retSubnet)
		}
	}
	validateGetZoneForNode("foo-node")
	validateGetZoneForNode("bar-node")
}

func TestIsNodeSelectedByFilter(t *testing.T) {
	fakeNodeInformer := FakeNodeInformer()
	zoneGetter := NewFakeZoneGetter(fakeNodeInformer, defaultTestSubnetURL, true)

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

	nodeInformer := FakeNodeInformer()
	PopulateFakeNodeInformer(nodeInformer, true)
	zoneGetter := NewFakeZoneGetter(nodeInformer, defaultTestSubnetURL, true)
	testCases := []struct {
		desc                  string
		node                  *apiv1.Node
		expectInDefaultSubnet bool
		expectNil             bool
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
			expectInDefaultSubnet: true,
			expectNil:             true,
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
			expectInDefaultSubnet: false,
			expectNil:             false,
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
			expectInDefaultSubnet: true,
			expectNil:             true,
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
			expectInDefaultSubnet: true,
			expectNil:             true,
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
			expectInDefaultSubnet: false,
			expectNil:             true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			gotInDefaultSubnet, gotErr := zoneGetter.isNodeInDefaultSubnet(tc.node, defaultTestSubnetURL, klog.TODO())
			if gotErr != nil && tc.expectNil {
				t.Errorf("isNodeInDefaultSubnet(%v, %s) = err %v, want nil", tc.node, defaultTestSubnetURL, gotErr)
			}
			if gotErr == nil && !tc.expectNil {
				t.Errorf("isNodeInDefaultSubnet(%v, %s) = err nil, want not nil", tc.node, defaultTestSubnetURL)
			}
			if gotInDefaultSubnet != tc.expectInDefaultSubnet {
				t.Errorf("isNodeInDefaultSubnet(%v, %s) = %v, want %v", tc.node, defaultTestSubnetURL, gotInDefaultSubnet, tc.expectInDefaultSubnet)
			}
		})
	}
}

func TestNewLegacyZoneGetter(t *testing.T) {
	t.Parallel()
	nodeInformer := FakeNodeInformer()
	PopulateFakeNodeInformer(nodeInformer, false)
	zoneGetter := NewLegacyZoneGetter(nodeInformer)

	if zoneGetter.mode != Legacy {
		t.Errorf("Expected zoneGetter mode to to be Legacy, but got %+v", zoneGetter.mode)
	}
	if zoneGetter.defaultSubnetURL != "" {
		t.Errorf("Expected defaultSubnetURL to be empty, but got %s", zoneGetter.defaultSubnetURL)
	}
	if !zoneGetter.onlyIncludeDefaultSubnetNodes {
		t.Errorf("Expected onlyIncludeDefaultSubnetNodes to be true, but got false")
	}
}

func TestGetSubnetLegacyMode(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc         string
		node         *apiv1.Node
		expectSubnet string
		expectErr    error
	}{
		{
			desc: "Node with subnet label",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						utils.LabelNodeSubnet: "custom-subnet",
					},
				},
				Spec: apiv1.NodeSpec{PodCIDR: "10.0.0.0/24"},
			},
			expectSubnet: "",
			expectErr:    nil,
		},
		{
			desc: "Node without subnet label",
			node: &apiv1.Node{
				Spec: apiv1.NodeSpec{PodCIDR: "10.0.0.0/24"},
			},
			expectSubnet: "",
			expectErr:    nil,
		},
		{
			desc:      "Node without PodCIDR",
			node:      &apiv1.Node{},
			expectErr: ErrNodePodCIDRNotSet,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			nodeInformer := FakeNodeInformer()
			PopulateFakeNodeInformer(nodeInformer, false)
			zoneGetter := NewLegacyZoneGetter(nodeInformer)
			subnet, err := zoneGetter.getSubnet(tc.node, "")
			if subnet != tc.expectSubnet {
				t.Errorf("getSubnet() = %s, want %s", subnet, tc.expectSubnet)
			}
			if err != tc.expectErr {
				t.Errorf("getSubnet() = %v, want %v", err, tc.expectErr)
			}
		})
	}
}

func TestIsNodeInDefaultSubnetLegacyMode(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc                  string
		node                  *apiv1.Node
		expectInDefaultSubnet bool
		expectErr             error
	}{
		{
			desc: "Node with non-default subnet label",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						utils.LabelNodeSubnet: "non-default-subnet",
					},
				},
				Spec: apiv1.NodeSpec{PodCIDR: "10.0.0.0/24"},
			},
			expectInDefaultSubnet: true,
			expectErr:             nil,
		},
		{
			desc: "Node without any labels",
			node: &apiv1.Node{
				Spec: apiv1.NodeSpec{PodCIDR: "10.0.0.0/24"},
			},
			expectInDefaultSubnet: true,
			expectErr:             nil,
		},
		{
			desc:                  "Node without PodCIDR",
			node:                  &apiv1.Node{},
			expectInDefaultSubnet: false,
			expectErr:             ErrNodePodCIDRNotSet,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			nodeInformer := FakeNodeInformer()
			PopulateFakeNodeInformer(nodeInformer, false)
			zoneGetter := NewLegacyZoneGetter(nodeInformer)
			inSubnet, err := zoneGetter.isNodeInDefaultSubnet(tc.node, "", klog.TODO())
			if inSubnet != tc.expectInDefaultSubnet {
				t.Errorf("isNodeInDefaultSubnet() = %v, want %v", inSubnet, tc.expectInDefaultSubnet)
			}
			if err != tc.expectErr {
				t.Errorf("isNodeInDefaultSubnet() = %v, want %v", err, tc.expectErr)
			}
		})
	}
}

func TestNodeFilteringInLegacyMode(t *testing.T) {
	t.Parallel()

	nodeInformer := FakeNodeInformer()
	// Populate the informer with a mix of nodes.
	// In legacy mode, the subnet label should be ignored.
	nodes := []*apiv1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "healthy-node-default-subnet"},
			Spec:       apiv1.NodeSpec{ProviderID: "gce://project/zone1/healthy-node-default-subnet", PodCIDR: "10.0.1.0/24"},
			Status:     apiv1.NodeStatus{Conditions: []apiv1.NodeCondition{{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue}}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "healthy-node-other-subnet", Labels: map[string]string{utils.LabelNodeSubnet: "other-subnet"}},
			Spec:       apiv1.NodeSpec{ProviderID: "gce://project/zone2/healthy-node-other-subnet", PodCIDR: "10.0.2.0/24"},
			Status:     apiv1.NodeStatus{Conditions: []apiv1.NodeCondition{{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue}}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "unready-node"},
			Spec:       apiv1.NodeSpec{ProviderID: "gce://project/zone1/unready-node", PodCIDR: "10.0.3.0/24"},
			Status:     apiv1.NodeStatus{Conditions: []apiv1.NodeCondition{{Type: apiv1.NodeReady, Status: apiv1.ConditionFalse}}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "tainted-node"},
			Spec: apiv1.NodeSpec{
				ProviderID: "gce://project/zone1/tainted-node",
				PodCIDR:    "10.0.4.0/24",
				Taints:     []apiv1.Taint{{Key: utils.ToBeDeletedTaint, Effect: apiv1.TaintEffectNoSchedule}},
			},
			Status: apiv1.NodeStatus{Conditions: []apiv1.NodeCondition{{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue}}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "excluded-node", Labels: map[string]string{utils.LabelNodeRoleExcludeBalancer: ""}},
			Spec:       apiv1.NodeSpec{ProviderID: "gce://project/zone1/excluded-node", PodCIDR: "10.0.5.0/24"},
			Status:     apiv1.NodeStatus{Conditions: []apiv1.NodeCondition{{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue}}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "no-podcidr-node"},
			Spec:       apiv1.NodeSpec{ProviderID: "gce://project/zone1/no-podcidr-node"},
			Status:     apiv1.NodeStatus{Conditions: []apiv1.NodeCondition{{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue}}},
		},
	}
	for _, n := range nodes {
		nodeInformer.GetIndexer().Add(n)
	}

	zoneGetter := NewLegacyZoneGetter(nodeInformer)
	listedNodes, err := zoneGetter.ListNodes(CandidateNodesFilter, klog.TODO())
	if err != nil {
		t.Fatalf("zoneGetter.ListNodes(CandidateNodesFilter) = _, %v; want nil", err)
	}

	if len(listedNodes) != 2 {
		t.Errorf("len(listedNodes) = %d; want 2", len(listedNodes))
	}

	expectedNodes := map[string]bool{
		"healthy-node-default-subnet": true,
		"healthy-node-other-subnet":   true,
	}
	for _, node := range listedNodes {
		if !expectedNodes[node.Name] {
			t.Errorf("listed unexpected node %q", node.Name)
		}
	}
}

func TestZoneAndSubnetForNodeLegacyMode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		node           *apiv1.Node
		expectZone     string
		expectSubnet   string
		expectErr      error
		expectNilError bool
	}{
		{
			name: "with PodCIDR",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node-with-podcidr"},
				Spec:       apiv1.NodeSpec{ProviderID: "gce://project/zone-for-legacy-mode/test-node-with-podcidr", PodCIDR: "10.0.0.0/24"},
			},
			expectZone:     "zone-for-legacy-mode",
			expectSubnet:   "",
			expectNilError: true,
		},
		{
			name: "without PodCIDR",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node-without-podcidr"},
				Spec:       apiv1.NodeSpec{ProviderID: "gce://project/zone-for-legacy-mode/test-node-without-podcidr"},
			},
			expectErr: ErrNodePodCIDRNotSet,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			nodeInformer := FakeNodeInformer()
			nodeInformer.GetIndexer().Add(tc.node)
			zoneGetter := NewLegacyZoneGetter(nodeInformer)

			zone, subnet, err := zoneGetter.ZoneAndSubnetForNode(tc.node.Name, klog.TODO())

			if tc.expectNilError && err != nil {
				t.Fatalf("ZoneAndSubnetForNode(%q) = _, _, %v; want nil", tc.node.Name, err)
			}
			if !tc.expectNilError && !errors.Is(err, tc.expectErr) {
				t.Fatalf("ZoneAndSubnetForNode(%q) err = %v; want %v", tc.node.Name, err, tc.expectErr)
			}
			if zone != tc.expectZone {
				t.Errorf("ZoneAndSubnetForNode(%q) zone = %q; want %q", tc.node.Name, zone, tc.expectZone)
			}
			if subnet != tc.expectSubnet {
				t.Errorf("ZoneAndSubnetForNode(%q) subnet = %q; want %q", tc.node.Name, subnet, tc.expectSubnet)
			}
		})
	}
}

func TestListZonesLegacyMode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		desc          string
		filter        Filter
		expectedZones []string
	}{
		{
			desc:          "AllNodesFilter should list zones for all nodes with PodCIDR, ignoring readiness and subnet labels",
			filter:        AllNodesFilter,
			expectedZones: []string{"zone-1", "zone-2", "zone-3"},
		},
		{
			desc:          "CandidateNodesFilter should list zones for ready nodes with PodCIDR, ignoring subnet labels",
			filter:        CandidateNodesFilter,
			expectedZones: []string{"zone-1", "zone-2"},
		},
		{
			desc:          "CandidateAndUnreadyNodesFilter should list zones for ready and unready nodes with PodCIDR",
			filter:        CandidateAndUnreadyNodesFilter,
			expectedZones: []string{"zone-1", "zone-2", "zone-3"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			nodeInformer := FakeNodeInformer()
			nodes := []*apiv1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{utils.LabelNodeSubnet: "default-subnet"}},
					Spec:       apiv1.NodeSpec{ProviderID: "gce://project/zone-1/node-1", PodCIDR: "10.1.0.0/24"},
					Status:     apiv1.NodeStatus{Conditions: []apiv1.NodeCondition{{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue}}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-2", Labels: map[string]string{utils.LabelNodeSubnet: "other-subnet"}},
					Spec:       apiv1.NodeSpec{ProviderID: "gce://project/zone-2/node-2", PodCIDR: "10.2.0.0/24"},
					Status:     apiv1.NodeStatus{Conditions: []apiv1.NodeCondition{{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue}}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-3"},
					Spec:       apiv1.NodeSpec{ProviderID: "gce://project/zone-3/node-3", PodCIDR: "10.3.0.0/24"},
					Status:     apiv1.NodeStatus{Conditions: []apiv1.NodeCondition{{Type: apiv1.NodeReady, Status: apiv1.ConditionFalse}}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "node-4"},
					Spec:       apiv1.NodeSpec{ProviderID: "gce://project/zone-4/node-4"}, // No PodCIDR
					Status:     apiv1.NodeStatus{Conditions: []apiv1.NodeCondition{{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue}}},
				},
			}
			for _, n := range nodes {
				nodeInformer.GetIndexer().Add(n)
			}
			zoneGetter := NewLegacyZoneGetter(nodeInformer)

			zones, err := zoneGetter.ListZones(tc.filter, klog.TODO())
			if err != nil {
				t.Fatalf("ListZones() with filter %q returned error %v, want nil", tc.filter, err)
			}

			if !reflect.DeepEqual(zones, tc.expectedZones) {
				t.Errorf("ListZones() with filter %q = %v, want %v", tc.filter, zones, tc.expectedZones)
			}
		})
	}
}

func TestIsNodeSelectedByFilterLegacyMode(t *testing.T) {
	t.Parallel()
	nodeInformer := FakeNodeInformer()
	PopulateFakeNodeInformer(nodeInformer, false)
	zoneGetter := NewLegacyZoneGetter(nodeInformer)

	testCases := []struct {
		name             string
		node             *apiv1.Node
		filter           Filter
		expectIsSelected bool
	}{
		{
			name: "Node with non-default subnet label should be selected by CandidateNodesFilter",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{utils.LabelNodeSubnet: "other-subnet"}},
				Spec:       apiv1.NodeSpec{PodCIDR: "10.0.0.0/24"},
				Status:     apiv1.NodeStatus{Conditions: []apiv1.NodeCondition{{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue}}},
			},
			filter:           CandidateNodesFilter,
			expectIsSelected: true,
		},
		{
			name: "Node without PodCIDR should not be selected by AllNodesFilter",
			node: &apiv1.Node{
				Status: apiv1.NodeStatus{Conditions: []apiv1.NodeCondition{{Type: apiv1.NodeReady, Status: apiv1.ConditionTrue}}},
			},
			filter:           AllNodesFilter,
			expectIsSelected: false,
		},
		{
			name: "Unready node with PodCIDR should not be selected by CandidateNodesFilter",
			node: &apiv1.Node{
				Spec:   apiv1.NodeSpec{PodCIDR: "10.0.0.0/24"},
				Status: apiv1.NodeStatus{Conditions: []apiv1.NodeCondition{{Type: apiv1.NodeReady, Status: apiv1.ConditionFalse}}},
			},
			filter:           CandidateNodesFilter,
			expectIsSelected: false,
		},
		{
			name: "Unready node with PodCIDR should be selected by AllNodesFilter",
			node: &apiv1.Node{
				Spec:   apiv1.NodeSpec{PodCIDR: "10.0.0.0/24"},
				Status: apiv1.NodeStatus{Conditions: []apiv1.NodeCondition{{Type: apiv1.NodeReady, Status: apiv1.ConditionFalse}}},
			},
			filter:           AllNodesFilter,
			expectIsSelected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			isSelected := zoneGetter.IsNodeSelectedByFilter(tc.node, tc.filter, klog.TODO())
			if isSelected != tc.expectIsSelected {
				t.Errorf("IsNodeSelectedByFilter() = %v, want %v", isSelected, tc.expectIsSelected)
			}
		})
	}
}

func TestIsDefaultSubnetNodeLegacyMode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                  string
		node                  *apiv1.Node
		expectInDefaultSubnet bool
		expectErr             error
	}{
		{
			name: "Node with PodCIDR is considered in default subnet",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node-with-podcidr"},
				Spec:       apiv1.NodeSpec{PodCIDR: "10.0.0.0/24"},
			},
			expectInDefaultSubnet: true,
			expectErr:             nil,
		},
		{
			name: "Node with PodCIDR and non-default subnet label is still in default subnet",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node-with-other-subnet", Labels: map[string]string{utils.LabelNodeSubnet: "other-subnet"}},
				Spec:       apiv1.NodeSpec{PodCIDR: "10.0.0.0/24"},
			},
			expectInDefaultSubnet: true,
			expectErr:             nil,
		},
		{
			name: "Node without PodCIDR returns error",
			node: &apiv1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node-no-podcidr"},
			},
			expectInDefaultSubnet: false,
			expectErr:             ErrNodePodCIDRNotSet,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			nodeInformer := FakeNodeInformer()
			nodeInformer.GetIndexer().Add(tc.node)
			zoneGetter := NewLegacyZoneGetter(nodeInformer)

			inSubnet, err := zoneGetter.IsDefaultSubnetNode(tc.node.Name, klog.TODO())

			if inSubnet != tc.expectInDefaultSubnet {
				t.Errorf("IsDefaultSubnetNode() inSubnet = %v, want %v", inSubnet, tc.expectInDefaultSubnet)
			}
			if !errors.Is(err, tc.expectErr) {
				t.Errorf("IsDefaultSubnetNode() err = %v, want %v", err, tc.expectErr)
			}
		})
	}
}
