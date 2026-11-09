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

package syncers

import (
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
)

// readyPodsPerNode counts the ready pods of a service on each node. Pods are
// deduplicated by name because a dual-stack service has separate IPv4 and IPv6
// EndpointSlices listing the same pod.
func readyPodsPerNode(eds []negtypes.EndpointsData) map[string]int {
	result := make(map[string]int)
	seenPods := make(map[string]bool)
	for _, ed := range eds {
		for _, addr := range ed.Addresses {
			if addr.NodeName == nil || addr.TargetRef == nil || !addr.Ready {
				continue
			}
			if seenPods[addr.TargetRef.Name] {
				continue
			}
			seenPods[addr.TargetRef.Name] = true
			result[*addr.NodeName]++
		}
	}
	return result
}

// l4BackendPods returns the number of ready pods on each node that serves the
// service's load balancer traffic. Nodes with no ready pods are omitted.
//
// With ETP=Local a node forwards load balancer traffic only to its own pods, so
// only nodes in the service's NEG (targetMap) are counted.
//
// With ETP=Cluster the NEG is a node subset picked independently of pod
// placement and any ready pod can receive traffic, so all nodes with ready pods
// are counted.
func l4BackendPods(mode negtypes.EndpointsCalculatorMode, eds []negtypes.EndpointsData, targetMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet) map[string]int {
	pods := readyPodsPerNode(eds)
	if mode != negtypes.L4LocalMode {
		return pods
	}

	backends := make(map[string]int)
	for _, endpoints := range targetMap {
		for ep := range endpoints {
			if count := pods[ep.Node]; count > 0 {
				backends[ep.Node] = count
			}
		}
	}

	return backends
}
