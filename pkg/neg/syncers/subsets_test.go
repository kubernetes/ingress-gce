/*
Copyright 2019 The Kubernetes Authors.

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

package syncers

import (
	"fmt"
	"strings"
	"testing"

	"k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBasicSubset(t *testing.T) {
	t.Parallel()
	nodes := []*v1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node0"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node73"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node986"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node25"}},
	}
	count := 3
	subset1 := pickSubsetsMinRemovals(nodes, "svc123", count, nil)
	if len(subset1) < 3 {
		t.Errorf("Expected %d subsets, got only %d - %v", count, len(subset1), subset1)
	}
	if !validateSubset(subset1, nodes) {
		t.Errorf("Invalid subset list %v from %v", subset1, nodes)
	}
	subset2 := pickSubsetsMinRemovals(nodes, "svc345", count, nil)
	subset3 := pickSubsetsMinRemovals(nodes, "svc56", count, nil)
	t.Logf("Subset2 is %s", nodeNames(subset2))
	t.Logf("Subset3 is %s", nodeNames(subset3))
	if isIdentical(subset1, subset2) || isIdentical(subset3, subset2) || isIdentical(subset1, subset3) {
		t.Errorf("2 out of 3 subsets are identical")
	}
}

func TestEmptyNodes(t *testing.T) {
	t.Parallel()
	count := 3
	subset1 := pickSubsetsMinRemovals(nil, "svc123", count, nil)
	if len(subset1) != 0 {
		t.Errorf("Expected empty subset, got - %s", nodeNames(subset1))
	}
}

// Tests the case where there are fewer nodes than subsets
func TestFewerNodes(t *testing.T) {
	t.Parallel()
	nodes := makeNodes(0, 5)
	count := 10
	subset1 := pickSubsetsMinRemovals(nodes, "svc123", count, nil)
	if len(subset1) != len(nodes) {
		t.Errorf("Expected subset of length %d, got %d, subsets - %s", len(nodes), len(subset1), nodeNames(subset1))
	}
	if !isIdentical(nodes, subset1) {
		t.Errorf("Subset list is different from list of nodes, subsets - %s", nodeNames(subset1))
	}
}

// Tests the case where there is uneven distribution of nodes in various zones. The goal is to select as many nodes as
// possible in all cases.
func TestUnevenNodesInZones(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		description   string
		nodesMap      map[string][]*v1.Node
		svcKey        string
		subsetLimit   int
		expectedCount int
		// expectEmpty indicates that some zones can have empty subsets
		expectEmpty bool
	}{
		{
			description: "Total number of nodes > limit(250), some zones have only a couple of nodes.",
			nodesMap: map[string][]*v1.Node{
				"zone1": makeNodes(1, 1),
				"zone2": makeNodes(2, 5),
				"zone3": makeNodes(7, 10),
				"zone4": makeNodes(17, 250),
			},
			svcKey:        "svc123",
			subsetLimit:   maxSubsetSizeLocal,
			expectedCount: maxSubsetSizeLocal,
		},
		{
			description: "Total number of nodes > limit(250), 3 zones, some zones have only a couple of nodes.",
			nodesMap: map[string][]*v1.Node{
				"zone1": makeNodes(1, 1),
				"zone2": makeNodes(2, 5),
				"zone4": makeNodes(7, 250),
			},
			svcKey:        "svc123",
			subsetLimit:   maxSubsetSizeLocal,
			expectedCount: maxSubsetSizeLocal,
		},
		{
			description: "Total number of nodes > limit(250), all zones have 100 nodes.",
			nodesMap: map[string][]*v1.Node{
				"zone1": makeNodes(1, 100),
				"zone2": makeNodes(100, 100),
				"zone3": makeNodes(200, 100),
				"zone4": makeNodes(300, 100),
			},
			svcKey:        "svc123",
			subsetLimit:   maxSubsetSizeLocal,
			expectedCount: maxSubsetSizeLocal,
		},
		{
			description: "Total number of nodes > limit(250), 3 zones, all zones have 100 nodes.",
			nodesMap: map[string][]*v1.Node{
				"zone1": makeNodes(1, 100),
				"zone2": makeNodes(100, 100),
				"zone3": makeNodes(200, 100),
			},
			svcKey:        "svc123",
			subsetLimit:   maxSubsetSizeLocal,
			expectedCount: maxSubsetSizeLocal,
		},
		{
			description: "Total number of nodes < limit(250), some have only a couple of nodes.",
			nodesMap: map[string][]*v1.Node{
				"zone1": makeNodes(1, 1),
				"zone2": makeNodes(2, 5),
				"zone3": makeNodes(7, 10),
				"zone4": makeNodes(17, 33),
			},
			svcKey:      "svc123",
			subsetLimit: maxSubsetSizeLocal,
			// All the nodes should be picked
			expectedCount: 49,
		},
		{
			description: "Total number of nodes < limit(250), all have only a couple of nodes.",
			nodesMap: map[string][]*v1.Node{
				"zone1": makeNodes(1, 1),
				"zone2": makeNodes(2, 5),
				"zone3": makeNodes(7, 3),
				"zone4": makeNodes(10, 4),
			},
			svcKey:      "svc123",
			subsetLimit: maxSubsetSizeLocal,
			// All the nodes should be picked
			expectedCount: 13,
		},
		{
			description: "Total number of nodes > limit(25), some zones have only a couple of nodes.",
			nodesMap: map[string][]*v1.Node{
				"zone1": makeNodes(1, 1),
				"zone2": makeNodes(2, 5),
				"zone3": makeNodes(7, 10),
				"zone4": makeNodes(17, 250),
			},
			svcKey:        "svc123",
			subsetLimit:   maxSubsetSizeDefault,
			expectedCount: maxSubsetSizeDefault,
		},
		{
			description: "Total number of nodes > limit(25), one zone has no nodes.",
			nodesMap: map[string][]*v1.Node{
				"zone1": makeNodes(1, 1),
				"zone2": makeNodes(2, 5),
				"zone3": nil,
				"zone4": makeNodes(17, 250),
			},
			svcKey:        "svc123",
			subsetLimit:   maxSubsetSizeDefault,
			expectedCount: maxSubsetSizeDefault,
			expectEmpty:   true,
		},
	}
	for _, tc := range testCases {
		subsetMap, err := getSubsetPerZone(tc.nodesMap, tc.subsetLimit, tc.svcKey, nil, klog.TODO())
		if err != nil {
			t.Errorf("Failed to get subset for test '%s', err %v", tc.description, err)
		}
		if len(subsetMap) != len(tc.nodesMap) {
			t.Errorf("Not all input zones were included in the subset.  subset map - %v, nodesMap %v, test '%s'",
				subsetMap, tc.nodesMap, tc.description)
		}
		totalSubsetSize := 0
		for zone, subset := range subsetMap {
			if subset.Len() == 0 && !tc.expectEmpty {
				t.Errorf("Got empty subset in zone %s for test '%s'", zone, tc.description)
			}
			totalSubsetSize += subset.Len()
		}
		if totalSubsetSize != tc.expectedCount {
			t.Errorf("Expected %d nodes in subset, Got %d for test '%s'", maxSubsetSizeLocal, totalSubsetSize,
				tc.description)
		}
	}
}

func makeNodes(startIndex, count int) []*v1.Node {
	nodes := []*v1.Node{}
	for i := startIndex; i < startIndex+count; i++ {
		nodes = append(nodes, &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("node%d", i)}})
	}
	return nodes
}

func TestNoRemovals(t *testing.T) {
	t.Parallel()
	// pick a random startIndex which is used to construct nodeName.
	nodes := makeNodes(78, 5)
	count := 5
	subset1 := pickSubsetsMinRemovals(nodes, "svc123", count, nil)
	if len(subset1) < 5 {
		t.Errorf("Expected %d subsets, got only %d - %v", count, len(subset1), subset1)
	}
	// nodeName abcd shows up 2nd in the sorted list for the given salt. So picking a subset of 5 will remove one of the
	// existing nodes.
	nodes = append(nodes, &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node:abcd"}})
	subset2 := pickSubsetsMinRemovals(nodes, "svc123", count, nil)
	if len(subset2) < 5 {
		t.Errorf("Expected %d subsets, got only %d - %v", count, len(subset2), subset2)
	}
	if isIdentical(subset1, subset2) {
		t.Errorf("Got identical subsets %+v", subset1)
	}
	existingEp := []types.NetworkEndpoint{}
	for _, node := range subset1 {
		existingEp = append(existingEp, types.NetworkEndpoint{Node: node.Name})
	}
	subset3 := pickSubsetsMinRemovals(nodes, "svc123", count, existingEp)
	if len(subset3) < 5 {
		t.Errorf("Expected %d subsets, got only %d - %v", count, len(subset3), subset3)
	}
	if !isIdentical(subset1, subset3) {
		t.Errorf("Got subsets %+v and %+v, expected identical subsets %+v", subset1, subset3, subset1)
	}
}

func validateSubset(subset []*v1.Node, nodes []*v1.Node) bool {
	for _, val := range subset {
		found := false
		for _, node := range nodes {
			if val == node {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func nodeNames(subset []*v1.Node) string {
	names := []string{}
	for _, n := range subset {
		names = append(names, n.Name)
	}
	return strings.Join(names, " ")
}

func isIdentical(subset1, subset2 []*v1.Node) bool {
	foundCount := 0
	if len(subset1) != len(subset2) {
		return false
	}
	for _, node1 := range subset1 {
		found := false
		for _, node2 := range subset2 {
			if node1 == node2 {
				found = true
				break
			}
		}
		if found {
			foundCount = foundCount + 1
		}
	}
	return foundCount == len(subset1)
}
