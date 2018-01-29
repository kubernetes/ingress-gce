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
	"testing"
	"time"

	compute "google.golang.org/api/compute/v1"

	api_v1 "k8s.io/api/core/v1"
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
