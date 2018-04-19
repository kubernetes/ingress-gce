/*
Copyright 2016 The Kubernetes Authors.

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

package controller

import (
	"reflect"
	"testing"
	"time"

	compute "google.golang.org/api/compute/v1"

	api_v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/flags"
)

// Pods created in loops start from this time, for routines that
// sort on timestamp.
var firstPodCreationTime = time.Date(2006, 01, 02, 15, 04, 05, 0, time.UTC)

func TestZoneListing(t *testing.T) {
	cm := NewFakeClusterManager(flags.DefaultClusterUID, DefaultFirewallName)
	lbc := newLoadBalancerController(t, cm)
	zoneToNode := map[string][]string{
		"zone-1": {"n1"},
		"zone-2": {"n2"},
	}
	addNodes(lbc, zoneToNode)
	zones, err := lbc.Translator.ListZones()
	if err != nil {
		t.Errorf("Failed to list zones: %v", err)
	}
	for expectedZone := range zoneToNode {
		found := false
		for _, gotZone := range zones {
			if gotZone == expectedZone {
				found = true
			}
		}
		if !found {
			t.Fatalf("Expected zones %v; Got zones %v", zoneToNode, zones)
		}
	}
}

func TestInstancesAddedToZones(t *testing.T) {
	cm := NewFakeClusterManager(flags.DefaultClusterUID, DefaultFirewallName)
	lbc := newLoadBalancerController(t, cm)
	zoneToNode := map[string][]string{
		"zone-1": {"n1", "n2"},
		"zone-2": {"n3"},
	}
	addNodes(lbc, zoneToNode)

	// Create 2 igs, one per zone.
	testIG := "test-ig"
	lbc.CloudClusterManager.instancePool.EnsureInstanceGroupsAndPorts(testIG, []int64{int64(3001)})

	// node pool syncs kube-nodes, this will add them to both igs.
	lbc.CloudClusterManager.instancePool.Sync([]string{"n1", "n2", "n3"})
	gotZonesToNode := cm.fakeIGs.GetInstancesByZone()

	for z, nodeNames := range zoneToNode {
		if ig, err := cm.fakeIGs.GetInstanceGroup(testIG, z); err != nil {
			t.Errorf("Failed to find ig %v in zone %v, found %+v: %v", testIG, z, ig, err)
		}
		expNodes := sets.NewString(nodeNames...)
		gotNodes := sets.NewString(gotZonesToNode[z]...)
		if !gotNodes.Equal(expNodes) {
			t.Errorf("Nodes not added to zones, expected %+v got %+v", expNodes, gotNodes)
		}
	}
}

func addNodes(lbc *LoadBalancerController, zoneToNode map[string][]string) {
	for zone, nodes := range zoneToNode {
		for _, node := range nodes {
			n := &api_v1.Node{
				ObjectMeta: meta_v1.ObjectMeta{
					Name: node,
					Labels: map[string]string{
						annotations.ZoneKey: zone,
					},
				},
				Status: api_v1.NodeStatus{
					Conditions: []api_v1.NodeCondition{
						{Type: api_v1.NodeReady, Status: api_v1.ConditionTrue},
					},
				},
			}
			lbc.nodeLister.Add(n)
		}
	}
	lbc.CloudClusterManager.instancePool.Init(lbc.Translator)
}

func getProbePath(p *api_v1.Probe) string {
	return p.Handler.HTTPGet.Path
}

func TestAddInstanceGroupsAnnotation(t *testing.T) {
	testCases := []struct {
		Igs                []*compute.InstanceGroup
		ExpectedAnnotation string
	}{
		{
			// Single zone.
			[]*compute.InstanceGroup{{
				Name: "ig-name",
				Zone: "https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-b",
			}},
			`[{"Name":"ig-name","Zone":"https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-b"}]`,
		},
		{
			// Multiple zones.
			[]*compute.InstanceGroup{
				{
					Name: "ig-name-1",
					Zone: "https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-b",
				},
				{
					Name: "ig-name-2",
					Zone: "https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-a",
				},
			},
			`[{"Name":"ig-name-1","Zone":"https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-b"},{"Name":"ig-name-2","Zone":"https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-a"}]`,
		},
	}
	for _, c := range testCases {
		ingAnnotations := map[string]string{}
		err := setInstanceGroupsAnnotation(ingAnnotations, c.Igs)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if ingAnnotations[annotations.InstanceGroupsAnnotationKey] != c.ExpectedAnnotation {
			t.Fatalf("Unexpected annotation value: %s, expected: %s", ingAnnotations[annotations.InstanceGroupsAnnotationKey], c.ExpectedAnnotation)
		}
	}
}

func TestInstanceGroupsAnnotation(t *testing.T) {
	testCases := []struct {
		ing      *extensions.Ingress
		expected []InstanceGroupsAnnotationValue
	}{
		{
			&extensions.Ingress{
				ObjectMeta: meta_v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.InstanceGroupsAnnotationKey: `[{"Name":"k8s-ig--1","Zone":"https://www.googleapis.com/compute/v1/projects/rramkumar-gke-dev/zones/us-central1-c"}]`,
					},
				},
			},
			[]InstanceGroupsAnnotationValue{InstanceGroupsAnnotationValue{Name: "k8s-ig--1", Zone: "https://www.googleapis.com/compute/v1/projects/rramkumar-gke-dev/zones/us-central1-c"}},
		},
	}

	for _, testCase := range testCases {
		res, _ := instanceGroupsAnnotation(testCase.ing)
		if !reflect.DeepEqual(testCase.expected, res) {
			t.Errorf("Expected instance group annotation values %+v not equal to result %+v", testCase.expected, res)
		}
	}
}

func TestNodeStatusChanged(t *testing.T) {
	testCases := []struct {
		desc   string
		mutate func(node *api_v1.Node)
		expect bool
	}{
		{
			"no change",
			func(node *api_v1.Node) {},
			false,
		},
		{
			"unSchedulable changes",
			func(node *api_v1.Node) {
				node.Spec.Unschedulable = true
			},
			true,
		},
		{
			"readiness changes",
			func(node *api_v1.Node) {
				node.Status.Conditions[0].Status = api_v1.ConditionFalse
				node.Status.Conditions[0].LastTransitionTime = meta_v1.NewTime(time.Now())
			},
			true,
		},
		{
			"new heartbeat",
			func(node *api_v1.Node) {
				node.Status.Conditions[0].LastHeartbeatTime = meta_v1.NewTime(time.Now())
			},
			false,
		},
	}

	for _, tc := range testCases {
		node := testNode()
		tc.mutate(node)
		res := nodeStatusChanged(testNode(), node)
		if res != tc.expect {
			t.Fatalf("Test case %q got: %v, expected: %v", tc.desc, res, tc.expect)
		}
	}
}

func testNode() *api_v1.Node {
	return &api_v1.Node{
		ObjectMeta: meta_v1.ObjectMeta{
			Namespace: "ns",
			Name:      "node",
			Annotations: map[string]string{
				"key1": "value1",
			},
		},
		Spec: api_v1.NodeSpec{
			Unschedulable: false,
		},
		Status: api_v1.NodeStatus{
			Conditions: []api_v1.NodeCondition{
				{
					Type:               api_v1.NodeReady,
					Status:             api_v1.ConditionTrue,
					LastHeartbeatTime:  meta_v1.NewTime(time.Date(2000, 01, 1, 1, 0, 0, 0, time.UTC)),
					LastTransitionTime: meta_v1.NewTime(time.Date(2000, 01, 1, 1, 0, 0, 0, time.UTC)),
				},
			},
		},
	}
}
