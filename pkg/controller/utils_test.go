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

	"google.golang.org/api/compute/v1"

	api_v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	//"k8s.io/apimachinery/pkg/util/sets"

	"reflect"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils"
)

// Pods created in loops start from this time, for routines that
// sort on timestamp.
var firstPodCreationTime = time.Date(2006, 01, 02, 15, 04, 05, 0, time.UTC)

func TestZoneListing(t *testing.T) {
	lbc := newLoadBalancerController()
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

/*
* TODO(rramkumar): Move to pkg/instances in another PR
func TestInstancesAddedToZones(t *testing.T) {
	lbc := newLoadBalancerController()
	zoneToNode := map[string][]string{
		"zone-1": {"n1", "n2"},
		"zone-2": {"n3"},
	}
	addNodes(lbc, zoneToNode)

	// Create 2 igs, one per zone.
	testIG := "test-ig"
	lbc.instancePool.EnsureInstanceGroupsAndPorts(testIG, []int64{int64(3001)})

	// node pool syncs kube-nodes, this will add them to both igs.
	lbc.instancePool.Sync([]string{"n1", "n2", "n3"})
	gotZonesToNode := lbc.instancePool.GetInstancesByZone()

	for z, nodeNames := range zoneToNode {
		if ig, err := lbc.instancePool.GetInstanceGroup(testIG, z); err != nil {
			t.Errorf("Failed to find ig %v in zone %v, found %+v: %v", testIG, z, ig, err)
		}
		expNodes := sets.NewString(nodeNames...)
		gotNodes := sets.NewString(gotZonesToNode[z]...)
		if !gotNodes.Equal(expNodes) {
			t.Errorf("Nodes not added to zones, expected %+v got %+v", expNodes, gotNodes)
		}
	}
}
*/

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
	lbc.instancePool.Init(lbc.Translator)
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

func TestUniq(t *testing.T) {
	testCases := []struct {
		desc   string
		input  []utils.ServicePort
		expect []utils.ServicePort
	}{
		{
			"Empty",
			[]utils.ServicePort{},
			[]utils.ServicePort{},
		},
		{
			"Two service ports",
			[]utils.ServicePort{
				testServicePort("ns", "name", "80", 80, 30080, true),
				testServicePort("ns", "name", "443", 443, 30443, true),
			},
			[]utils.ServicePort{
				testServicePort("ns", "name", "80", 80, 30080, true),
				testServicePort("ns", "name", "443", 443, 30443, true),
			},
		},
		{
			"Two service ports with different names",
			[]utils.ServicePort{
				testServicePort("ns", "name1", "80", 80, 30080, true),
				testServicePort("ns", "name2", "80", 80, 30880, true),
			},
			[]utils.ServicePort{
				testServicePort("ns", "name1", "80", 80, 30080, true),
				testServicePort("ns", "name2", "80", 80, 30880, true),
			},
		},
		{
			"Two duplicate service ports",
			[]utils.ServicePort{
				testServicePort("ns", "name", "80", 80, 30080, true),
				testServicePort("ns", "name", "80", 80, 30080, true),
			},
			[]utils.ServicePort{
				testServicePort("ns", "name", "80", 80, 30080, true),
			},
		},
		{
			"Two services without nodeports",
			[]utils.ServicePort{
				testServicePort("ns", "name", "80", 80, 0, true),
				testServicePort("ns", "name", "443", 443, 0, true),
			},
			[]utils.ServicePort{
				testServicePort("ns", "name", "80", 80, 0, true),
				testServicePort("ns", "name", "443", 443, 0, true),
			},
		},
		{
			"2 out of 3 are duplicates",
			[]utils.ServicePort{
				testServicePort("ns", "name", "80", 80, 0, true),
				testServicePort("ns", "name", "443", 443, 0, true),
				testServicePort("ns", "name", "443", 443, 0, true),
			},
			[]utils.ServicePort{
				testServicePort("ns", "name", "80", 80, 0, true),
				testServicePort("ns", "name", "443", 443, 0, true),
			},
		},
		{
			"mix of named port and port number references",
			[]utils.ServicePort{
				testServicePort("ns", "name", "http", 80, 0, true),
				testServicePort("ns", "name", "https", 443, 0, true),
				testServicePort("ns", "name", "443", 443, 0, true),
			},
			[]utils.ServicePort{
				testServicePort("ns", "name", "http", 80, 0, true),
				testServicePort("ns", "name", "443", 443, 0, true),
			},
		},
	}

	for _, tc := range testCases {
		res := uniq(tc.input)
		if len(res) != len(tc.expect) {
			t.Errorf("Test case %q: Expect %d, got %d", tc.desc, len(tc.expect), len(res))
		}
		for _, svcPort := range tc.expect {
			found := false
			for _, sp := range res {
				if svcPort == sp {
					found = true
				}
			}
			if !found {
				t.Errorf("Test case %q: Expect service port %v to be present. But not found", tc.desc, svcPort)
			}
		}
	}

}

func TestGetNodePortsUsedByIngress(t *testing.T) {
	testCases := []struct {
		desc        string
		svcPorts    []utils.ServicePort
		expectPorts []int64
	}{
		{
			"empty input",
			[]utils.ServicePort{},
			[]int64{},
		},
		{
			" all NEG enabled",
			[]utils.ServicePort{
				testServicePort("ns", "name", "80", 80, 30080, true),
				testServicePort("ns", "name", "443", 443, 30443, true),
			},
			[]int64{},
		},
		{
			" all nonNEG enabled",
			[]utils.ServicePort{
				testServicePort("ns", "name", "80", 80, 30080, false),
				testServicePort("ns", "name", "443", 443, 30443, false),
			},
			[]int64{30080, 30443},
		},
		{
			" mixed SvcPorts",
			[]utils.ServicePort{
				testServicePort("ns", "name", "80", 80, 30080, false),
				testServicePort("ns", "name", "443", 443, 30443, true),
			},
			[]int64{30080},
		},
		{
			" mixed SvcPorts with duplicates",
			[]utils.ServicePort{
				testServicePort("ns", "name", "80", 80, 30080, false),
				testServicePort("ns", "name", "80", 80, 30080, false),
				testServicePort("ns", "name", "443", 443, 30443, false),
			},
			[]int64{30080, 30443},
		},
	}

	for _, tc := range testCases {
		res := nodePorts(tc.svcPorts)
		for _, p := range res {
			found := false
			for _, ep := range tc.expectPorts {
				if reflect.DeepEqual(ep, p) {
					found = true
				}
			}
			if !found {
				t.Errorf("For case %q, expect %v, but got %v", tc.desc, tc.expectPorts, res)
				break
			}
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

func testServicePort(namespace, name, port string, servicePort, nodePort int, enableNEG bool) utils.ServicePort {
	return utils.ServicePort{
		ID: utils.ServicePortID{
			Service: types.NamespacedName{
				Namespace: namespace,
				Name:      name,
			},
			Port: networkingv1.ServiceBackendPort{Name: port},
		},
		Port:       int32(servicePort),
		NodePort:   int64(nodePort),
		NEGEnabled: enableNEG,
	}
}
