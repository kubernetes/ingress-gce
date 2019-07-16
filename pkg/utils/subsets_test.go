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

package utils

import (
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
	subset1 := PickSubsets(nodes, "svc123", count)
	if len(subset1) < 3 {
		t.Errorf("Expected %d subsets, got only %d - %v", count, len(subset1), subset1)
	}
	if !validateSubset(subset1, nodes) {
		t.Errorf("Invalid subset list %v from %v", subset1, nodes)
	}
	subset2 := PickSubsets(nodes, "svc345", count)
	subset3 := PickSubsets(nodes, "svc56", count)
	t.Logf("Subset2 is %s", nodeNames(subset2))
	t.Logf("Subset3 is %s", nodeNames(subset3))
	if isIdentical(subset1, subset2) || isIdentical(subset3, subset2) || isIdentical(subset1, subset3) {
		t.Errorf("2 out of 3 subsets are identical")
	}
}

func TestEmptyNodes(t *testing.T) {
	t.Parallel()
	count := 3
	subset1 := PickSubsets(nil, "svc123", count)
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
	subset1 := PickSubsets(nodes, "svc123", count)
	if len(subset1) != len(nodes) {
		t.Errorf("Expected subset of length %d, got %d, subsets - %s", len(nodes), len(subset1), nodeNames(subset1))
	}
	if !isIdentical(nodes, subset1) {
		t.Errorf("Subset list is different from list of nodes, subsets - %s", nodeNames(subset1))
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
