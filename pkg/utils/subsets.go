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
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"k8s.io/api/core/v1"
)

// NodeInfo stores node metadata used to sort nodes and pick a subset.
type NodeInfo struct {
	index      int
	hashedName string
}

// PickSubsets takes a list of nodes, hash salt, count and retuns a
// subset of size - 'count'. If the input list is smaller than the
// desired subset count, the entire list is returned. The hash salt
// is used so that a different subset is returned even when the same
// node list is passed in, for a different salt value.
func PickSubsets(nodes []*v1.Node, salt string, count int) []*v1.Node {
	if len(nodes) < count {
		return nodes
	}
	subsets := make([]*v1.Node, 0, count)
	info := make([]*NodeInfo, len(nodes))
	for i, node := range nodes {
		hashSum := sha256.Sum256([]byte(node.Name + ":" + salt))
		info[i] = &NodeInfo{i, hex.EncodeToString(hashSum[:])}
	}
	// sort alphabetically, based on the hashed string
	sort.Slice(info, func(i, j int) bool {
		return info[i].hashedName < info[j].hashedName
	})
	for _, val := range info {
		subsets = append(subsets, nodes[val.index])
		if len(subsets) == count {
			break
		}
	}
	return subsets
}
