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
	"testing"

	"github.com/google/go-cmp/cmp"
	apiv1 "k8s.io/api/core/v1"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
)

func TestReadyPodsPerNode(t *testing.T) {
	node1 := "node-1"
	node2 := "node-2"
	eds := []negtypes.EndpointsData{
		{
			Addresses: []negtypes.AddressData{
				{
					NodeName:  &node1,
					TargetRef: &apiv1.ObjectReference{Name: "pod-1"},
					Ready:     true,
				},
				{
					NodeName:  &node1,
					TargetRef: &apiv1.ObjectReference{Name: "pod-2"},
					Ready:     true,
				},
				{
					NodeName:  &node1,
					TargetRef: &apiv1.ObjectReference{Name: "pod-unready"},
					Ready:     false,
				},
				{
					NodeName:  &node2,
					TargetRef: &apiv1.ObjectReference{Name: "pod-3"},
					Ready:     true,
				},
				{
					NodeName:  nil,
					TargetRef: &apiv1.ObjectReference{Name: "pod-no-node"},
					Ready:     true,
				},
				{
					NodeName:  &node1,
					TargetRef: nil,
					Ready:     true,
				},
			},
		},
		{
			// Dual-stack IPv6 EndpointSlice referencing the same pod-1 on node-1
			Addresses: []negtypes.AddressData{
				{
					NodeName:  &node1,
					TargetRef: &apiv1.ObjectReference{Name: "pod-1"},
					Ready:     true,
				},
			},
		},
	}

	got := readyPodsPerNode(eds)
	expected := map[string]int{
		"node-1": 2,
		"node-2": 1,
	}
	if diff := cmp.Diff(expected, got); diff != "" {
		t.Errorf("readyPodsPerNode() returned unexpected diff (-want +got):\n%s", diff)
	}
}

func TestL4BackendPods(t *testing.T) {
	node1 := "node-1"
	node2 := "node-2"
	nodeExcluded := "node-master"
	eds := []negtypes.EndpointsData{
		{
			Addresses: []negtypes.AddressData{
				{
					NodeName:  &node1,
					TargetRef: &apiv1.ObjectReference{Name: "pod-1"},
					Ready:     true,
				},
				{
					NodeName:  &node1,
					TargetRef: &apiv1.ObjectReference{Name: "pod-2"},
					Ready:     true,
				},
				{
					NodeName:  &node2,
					TargetRef: &apiv1.ObjectReference{Name: "pod-3"},
					Ready:     true,
				},
				{
					NodeName:  &nodeExcluded,
					TargetRef: &apiv1.ObjectReference{Name: "pod-master"},
					Ready:     true,
				},
			},
		},
	}

	loc := negtypes.NEGLocation{Zone: "us-central1-a"}
	targetMap := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
		loc: negtypes.NewNetworkEndpointSet(
			negtypes.NetworkEndpoint{Node: "node-1"},
			negtypes.NetworkEndpoint{Node: "node-2"},
			negtypes.NetworkEndpoint{Node: "node-empty"}, // backend node with 0 pods
		),
	}

	cases := []struct {
		desc string
		mode negtypes.EndpointsCalculatorMode
		want map[string]int
	}{
		{
			desc: "ETP=Local counts only nodes in the NEG",
			mode: negtypes.L4LocalMode,
			want: map[string]int{
				"node-1": 2,
				"node-2": 1,
			},
		},
		{
			desc: "ETP=Cluster counts all nodes with ready pods",
			mode: negtypes.L4ClusterMode,
			want: map[string]int{
				"node-1":      2,
				"node-2":      1,
				"node-master": 1,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := l4BackendPods(tc.mode, eds, targetMap)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("l4BackendPods() returned unexpected diff (-want +got):\n%s", diff)
			}
		})
	}
}
