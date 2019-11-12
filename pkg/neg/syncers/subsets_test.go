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
	"k8s.io/ingress-gce/pkg/neg/types"
	"strings"
	"testing"

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
	subset1 := PickSubsetsNoRemovals(nodes, "svc123", count, nil)
	if len(subset1) < 3 {
		t.Errorf("Expected %d subsets, got only %d - %v", count, len(subset1), subset1)
	}
	if !validateSubset(subset1, nodes) {
		t.Errorf("Invalid subset list %v from %v", subset1, nodes)
	}
	subset2 := PickSubsetsNoRemovals(nodes, "svc345", count, nil)
	subset3 := PickSubsetsNoRemovals(nodes, "svc56", count, nil)
	t.Logf("Subset2 is %s", nodeNames(subset2))
	t.Logf("Subset3 is %s", nodeNames(subset3))
	if isIdentical(subset1, subset2) || isIdentical(subset3, subset2) || isIdentical(subset1, subset3) {
		t.Errorf("2 out of 3 subsets are identical")
	}
}

func TestEmptyNodes(t *testing.T) {
	t.Parallel()
	count := 3
	subset1 := PickSubsetsNoRemovals(nil, "svc123", count, nil)
	if len(subset1) != 0 {
		t.Errorf("Expected empty subset, got - %s", nodeNames(subset1))
	}
}

// Tests the case where there are fewer nodes than subsets
func TestFewerNodes(t *testing.T) {
	t.Parallel()
	nodes := []*v1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node0"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node73"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node986"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node25"}},
	}
	count := 10
	subset1 := PickSubsetsNoRemovals(nodes, "svc123", count, nil)
	if len(subset1) != len(nodes) {
		t.Errorf("Expected subset of length %d, got %d, subsets - %s", len(nodes), len(subset1), nodeNames(subset1))
	}
	if !isIdentical(nodes, subset1) {
		t.Errorf("Subset list is different from list of nodes, subsets - %s", nodeNames(subset1))
	}
}

func TestNoRemovals(t *testing.T) {
	t.Parallel()
	nodes := []*v1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node0"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node73"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node986"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node25"}},
	}
	count := 5
	subset1 := PickSubsetsNoRemovals(nodes, "svc123", count, nil)
	if len(subset1) < 5 {
		t.Errorf("Expected %d subsets, got only %d - %v", count, len(subset1), subset1)
	}
	// nodeName abcd shows up 2nd in the sorted list for the given salt. So picking a subset of 5 will remove one of the
	// existing nodes.
	nodes = append(nodes, &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node:abcd"}})
	subset2 := PickSubsetsNoRemovals(nodes, "svc123", count, nil)
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
	subset3 := PickSubsetsNoRemovals(nodes, "svc123", count, existingEp)
	if len(subset3) < 5 {
		t.Errorf("Expected %d subsets, got only %d - %v", count, len(subset3), subset3)
	}
	if !isIdentical(subset1, subset3) {
		t.Errorf("Got subsets %+v and %+v, expected identical subsets %+v", subset1, subset3, subset1)
	}
}

func TestGetSubsetCount(t *testing.T) {
	t.Parallel()
	zoneCount := 3
	tcs := []struct {
		desc          string
		randomize     bool
		startCount    int
		endCount      int
		expectedCount int
	}{
		{desc: "start with endpoints, drop to none", startCount: 5, endCount: 0, expectedCount: zoneCount},
		{desc: "no endpoints", startCount: 0, endCount: 0, expectedCount: zoneCount},
		{desc: "valid endpoints increase", startCount: 5, endCount: 10, expectedCount: 10},
		{desc: "valid endpoints decrease", startCount: 5, endCount: 2, expectedCount: 2},
		{desc: "start with endpoints, drop to none, random true", randomize: true, startCount: 5, endCount: 0, expectedCount: 5},
		{desc: "no endpoints, random true", randomize: true, startCount: 0, endCount: 0, expectedCount: zoneCount},
		{desc: "valid endpoints increase, random true", randomize: true, startCount: 5, endCount: 10, expectedCount: 10},
		{desc: "valid endpoints decrease, random true", randomize: true, startCount: 5, endCount: 2, expectedCount: 5},
	}
	for _, tc := range tcs {
		result := getSubsetCount(tc.startCount, tc.endCount, zoneCount, tc.randomize)
		if result != tc.expectedCount {
			t.Errorf("For test case '%s', expected subsetCount of %d, but got %d", tc.desc, tc.expectedCount, result)
		}
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
