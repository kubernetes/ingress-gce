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

	nodetopologyv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/nodetopology/v1"
	"github.com/google/go-cmp/cmp"
	api_v1 "k8s.io/api/core/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/nodetopology"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

func TestListZones(t *testing.T) {
	t.Parallel()

	nodeInformer := FakeNodeInformer()
	PopulateFakeNodeInformer(nodeInformer, false)
	zoneGetter, err := NewFakeZoneGetter(nodeInformer, FakeNodeTopologyInformer(), defaultTestSubnetURL, false)
	if err != nil {
		t.Fatalf("failed to initialize zone getter")
	}
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
					if zone == EmptyZone {
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
	zoneGetter, err := NewFakeZoneGetter(nodeInformer, FakeNodeTopologyInformer(), defaultTestSubnetURL, true)
	if err != nil {
		t.Fatalf("failed to initialize zone getter")
	}

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
				if zone == EmptyZone {
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
	zoneGetter, err := NewFakeZoneGetter(nodeInformer, FakeNodeTopologyInformer(), defaultTestSubnetURL, false)
	if err != nil {
		t.Fatalf("failed to initialize zone getter")
	}

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
	zoneGetter, err := NewFakeZoneGetter(nodeInformer, FakeNodeTopologyInformer(), defaultTestSubnetURL, true)
	if err != nil {
		t.Fatalf("failed to initialize zone getter")
	}

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
	zoneGetter, err := NewFakeZoneGetter(nodeInformer, FakeNodeTopologyInformer(), defaultTestSubnetURL, false)
	if err != nil {
		t.Fatalf("failed to initialize zone getter")
	}

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
			expectErr:      nil,
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
	zoneGetter, err := NewFakeZoneGetter(nodeInformer, FakeNodeTopologyInformer(), defaultTestSubnetURL, true)
	if err != nil {
		t.Fatalf("failed to initialize zone getter")
	}

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
			expectZone: EmptyZone,
			expectErr:  nil,
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
		subnet, err := getSubnet(&tc.node, defaultTestSubnetURL)
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
	zoneGetter, err := NewFakeZoneGetter(fakeNodeInformer, FakeNodeTopologyInformer(), defaultTestSubnetURL, true)
	if err != nil {
		t.Fatalf("failed to initialize zone getter")
	}

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
			gotInDefaultSubnet, gotErr := isNodeInDefaultSubnet(tc.node, defaultTestSubnetURL, klog.TODO())
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

func TestListSubnets(t *testing.T) {
	prevTopologyCRName := flags.F.NodeTopologyCRName
	defer func() {
		flags.F.NodeTopologyCRName = prevTopologyCRName
	}()

	flags.F.NodeTopologyCRName = "default-topology-cr"

	defaultSubnetConfig, err := nodetopology.SubnetConfigFromSubnetURL(defaultTestSubnetURL)
	if err != nil {
		t.Fatalf("errored generating default subnet config: %v", err)
	}
	defaultSubnet := []nodetopologyv1.SubnetConfig{defaultSubnetConfig}

	testcases := []struct {
		desc               string
		nodeTopologySynced bool
		enableMSC          bool
		existingTopologyCR *nodetopologyv1.NodeTopology
		wantSubnets        []nodetopologyv1.SubnetConfig
	}{
		{
			desc:        "MSC is not enabled",
			enableMSC:   false,
			wantSubnets: defaultSubnet,
		},
		{
			desc:               "node topology crd has not synced",
			enableMSC:          true,
			nodeTopologySynced: false,
			wantSubnets:        defaultSubnet,
		},
		{
			desc:               "default topology crd doesn't exist",
			enableMSC:          true,
			nodeTopologySynced: true,
			wantSubnets:        defaultSubnet,
		},
		{
			desc:               "default topology CR exists with correct subnets",
			enableMSC:          true,
			nodeTopologySynced: true,
			existingTopologyCR: &nodetopologyv1.NodeTopology{
				ObjectMeta: metav1.ObjectMeta{
					Name: flags.F.NodeTopologyCRName,
				},
				Status: nodetopologyv1.NodeTopologyStatus{
					Subnets: []nodetopologyv1.SubnetConfig{
						defaultSubnetConfig,
						{Name: "subnet1", SubnetPath: "path/to/gcp/subnet1"},
					},
				},
			},
			wantSubnets: []nodetopologyv1.SubnetConfig{
				defaultSubnetConfig,
				{Name: "subnet1", SubnetPath: "path/to/gcp/subnet1"},
			},
		},
		{
			desc:               "should atleast return default subnet if topology CR is empty",
			enableMSC:          true,
			nodeTopologySynced: true,
			existingTopologyCR: &nodetopologyv1.NodeTopology{
				ObjectMeta: metav1.ObjectMeta{
					Name: flags.F.NodeTopologyCRName,
				},
				// Empty NodeTopology Status (without any subnets)
			},
			wantSubnets: []nodetopologyv1.SubnetConfig{
				defaultSubnetConfig,
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeTopologyInformer := FakeNodeTopologyInformer()
			zoneGetter, err := NewFakeZoneGetter(FakeNodeInformer(), fakeTopologyInformer, defaultTestSubnetURL, !tc.enableMSC)
			if err != nil {
				t.Fatalf("failed to initialize zone getter")
			}

			zoneGetter.nodeTopologyHasSynced = func() bool { return tc.nodeTopologySynced }

			if tc.existingTopologyCR != nil {
				fakeTopologyInformer.GetIndexer().Add(tc.existingTopologyCR)
			}

			gotSubnets := zoneGetter.ListSubnets(klog.TODO())

			if diff := cmp.Diff(gotSubnets, tc.wantSubnets); diff != "" {
				t.Errorf("Got mismatch for List Subnets (-got, +want)= %s", diff)
			}
		})
	}
}
