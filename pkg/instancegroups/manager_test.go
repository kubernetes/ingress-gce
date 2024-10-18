/*
Copyright 2015 The Kubernetes Authors.

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

package instancegroups

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/utils"

	"google.golang.org/api/googleapi"
	"k8s.io/klog/v2"

	"google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
)

const (
	defaultTestZone = "default-zone"
	testZoneA       = "dark-moon1-a"
	testZoneB       = "dark-moon1-b"
	testZoneC       = "dark-moon1-c"
	basePath        = "/basepath/projects/project-id/"

	defaultTestSubnetURL = "https://www.googleapis.com/compute/v1/projects/mock-project/regions/test-region/subnetworks/default"
)

var defaultNamer = namer.NewNamer("uid1", "fw1", klog.TODO())

func newNodePool(f Provider, maxIGSize int) Manager {
	nodeInformer := zonegetter.FakeNodeInformer()
	fakeZoneGetter := zonegetter.NewFakeZoneGetter(nodeInformer, zonegetter.FakeNodeTopologyInformer(), defaultTestSubnetURL, false)

	pool := NewManager(&ManagerConfig{
		Cloud:      f,
		Namer:      defaultNamer,
		Recorders:  &test.FakeRecorderSource{},
		BasePath:   basePath,
		ZoneGetter: fakeZoneGetter,
		MaxIGSize:  maxIGSize,
	})
	return pool
}

func TestNodePoolSync(t *testing.T) {
	maxIGSize := 1000

	names1001 := make([]string, maxIGSize+1)
	for i := 1; i <= maxIGSize+1; i++ {
		names1001[i-1] = fmt.Sprintf("n%d", i)
	}

	testCases := []struct {
		gceNodes       sets.String
		kubeNodes      sets.String
		shouldSkipSync bool
	}{
		{
			gceNodes:  sets.NewString("n1"),
			kubeNodes: sets.NewString("n1", "n2"),
		},
		{
			gceNodes:  sets.NewString("n1", "n2"),
			kubeNodes: sets.NewString("n1"),
		},
		{
			gceNodes:       sets.NewString("n1", "n2"),
			kubeNodes:      sets.NewString("n1", "n2"),
			shouldSkipSync: true,
		},
		{
			gceNodes:  sets.NewString(),
			kubeNodes: sets.NewString(names1001...),
		},
		{
			gceNodes:  sets.NewString("n0", "n1"),
			kubeNodes: sets.NewString(names1001...),
		},
	}

	for _, testCase := range testCases {
		// create fake gce node pool with existing gceNodes
		ig := &compute.InstanceGroup{Name: defaultNamer.InstanceGroup()}
		zonesToIGs := map[string]IGsToInstances{
			defaultTestZone: {
				ig: testCase.gceNodes,
			},
		}
		fakeGCEInstanceGroups := NewFakeInstanceGroups(zonesToIGs, maxIGSize)

		pool := newNodePool(fakeGCEInstanceGroups, maxIGSize)
		for _, kubeNode := range testCase.kubeNodes.List() {
			manager := pool.(*manager)
			zonegetter.AddFakeNodes(manager.ZoneGetter, defaultTestZone, kubeNode)
		}

		igName := defaultNamer.InstanceGroup()
		ports := []int64{80}
		_, err := pool.EnsureInstanceGroupsAndPorts(igName, ports, klog.TODO())
		if err != nil {
			t.Fatalf("pool.EnsureInstanceGroupsAndPorts(%s, %v) returned error %v, want nil", igName, ports, err)
		}

		// run sync with expected kubeNodes
		apiCallsCountBeforeSync := len(fakeGCEInstanceGroups.calls)
		err = pool.Sync(testCase.kubeNodes.List(), klog.TODO())
		if err != nil {
			t.Fatalf("pool.Sync(%v) returned error %v, want nil", testCase.kubeNodes.List(), err)
		}

		// run assertions
		apiCallsCountAfterSync := len(fakeGCEInstanceGroups.calls)
		if testCase.shouldSkipSync && apiCallsCountBeforeSync != apiCallsCountAfterSync {
			t.Errorf("Should skip sync. apiCallsCountBeforeSync = %d, apiCallsCountAfterSync = %d", apiCallsCountBeforeSync, apiCallsCountAfterSync)
		}

		instancesList, err := fakeGCEInstanceGroups.ListInstancesInInstanceGroup(ig.Name, defaultTestZone, allInstances)
		if err != nil {
			t.Fatalf("fakeGCEInstanceGroups.ListInstancesInInstanceGroup(%s, %s, %s) returned error %v, want nil", ig.Name, defaultTestZone, allInstances, err)
		}
		instances, err := test.InstancesListToNameSet(instancesList)
		if err != nil {
			t.Fatalf("test.InstancesListToNameSet(%v) returned error %v, want nil", ig, err)
		}

		expectedInstancesSize := testCase.kubeNodes.Len()
		if testCase.kubeNodes.Len() > maxIGSize {
			// If kubeNodes bigger than maximum instance group size, resulted instances
			// should be truncated to flags.F.MaxIgSize
			expectedInstancesSize = maxIGSize
		}
		if instances.Len() != expectedInstancesSize {
			t.Errorf("instances.Len() = %d not equal expectedInstancesSize = %d", instances.Len(), expectedInstancesSize)
		}
		if !testCase.kubeNodes.IsSuperset(instances) {
			t.Errorf("kubeNodes = %v is not superset of instances = %v", testCase.kubeNodes, instances)
		}

		// call sync one more time and check that it will be no-op and will not cause any api calls
		apiCallsCountBeforeSync = len(fakeGCEInstanceGroups.calls)
		err = pool.Sync(testCase.kubeNodes.List(), klog.TODO())
		if err != nil {
			t.Fatalf("pool.Sync(%v) returned error %v, want nil", testCase.kubeNodes.List(), err)
		}
		apiCallsCountAfterSync = len(fakeGCEInstanceGroups.calls)
		if apiCallsCountBeforeSync != apiCallsCountAfterSync {
			t.Errorf("Should skip sync if called second time with the same kubeNodes. apiCallsCountBeforeSync = %d, apiCallsCountAfterSync = %d", apiCallsCountBeforeSync, apiCallsCountAfterSync)
		}
	}
}

func TestInstanceAlreadyMemberOfIG(t *testing.T) {
	maxIGSize := 1000
	kubeNodes := sets.NewString("n1", "n2")

	fakeInstanceGroups := new(fakeIGAlreadyExists)
	fakeInstanceGroups.FakeInstanceGroups = NewFakeInstanceGroups(map[string]IGsToInstances{}, maxIGSize)

	pool := newNodePool(fakeInstanceGroups, maxIGSize)
	for _, kubeNode := range kubeNodes.List() {
		manager := pool.(*manager)
		zonegetter.AddFakeNodes(manager.ZoneGetter, defaultTestZone, kubeNode)
	}

	igName := defaultNamer.InstanceGroup()
	ports := []int64{80}
	_, err := pool.EnsureInstanceGroupsAndPorts(igName, ports, klog.TODO())
	if err != nil {
		t.Fatalf("pool.EnsureInstanceGroupsAndPorts(%s, %v) returned error %v, want nil", igName, ports, err)
	}

	// run sync with 2 times, expect not error despite fakeInstanceGroups will return 'memberAlreadyExists'
	err = pool.Sync(kubeNodes.List(), klog.TODO())
	if err != nil {
		t.Fatalf("pool.Sync() returned error %v, want nil", err)
	}
	err = pool.Sync(kubeNodes.List(), klog.TODO())
	if err != nil {
		t.Fatalf("pool.Sync() returned error %v, want nil", err)
	}
}

type fakeIGAlreadyExists struct {
	*FakeInstanceGroups
}

func (fakeIG *fakeIGAlreadyExists) AddInstancesToInstanceGroup(name, zone string, instanceRefs []*compute.InstanceReference) error {
	return &googleapi.Error{
		Code:    http.StatusBadRequest,
		Message: fmt.Sprintf("Resource: %v is already a member of %v", instanceRefs, name),
		Errors: []googleapi.ErrorItem{
			{
				Reason: "memberAlreadyExists",
			},
		},
	}
}

func TestNodePoolSyncHugeCluster(t *testing.T) {
	// for sake of easier debugging cap instance group size to 3
	maxIGSize := 3

	testCases := []struct {
		description    string
		gceNodesZoneA  sets.String
		gceNodesZoneB  sets.String
		gceNodesZoneC  sets.String
		kubeNodesZoneA sets.String
		kubeNodesZoneB sets.String
		kubeNodesZoneC sets.String
	}{
		{
			description:    "too many kube nodes in one 1 of 3 zone",
			gceNodesZoneA:  getNodeSlice("nodes-zone-a", maxIGSize),
			gceNodesZoneB:  getNodeSlice("nodes-zone-b", 2*maxIGSize),
			gceNodesZoneC:  getNodeSlice("nodes-zone-c", maxIGSize),
			kubeNodesZoneA: getNodeSlice("nodes-zone-a", maxIGSize),
			kubeNodesZoneB: getNodeSlice("nodes-zone-b", 2*maxIGSize),
			kubeNodesZoneC: getNodeSlice("nodes-zone-c", maxIGSize),
		},
		{
			description:    "too many kube nodes in 2 of 3 zones",
			gceNodesZoneA:  getNodeSlice("nodes-zone-a", maxIGSize),
			gceNodesZoneB:  getNodeSlice("nodes-zone-b", 2*maxIGSize),
			gceNodesZoneC:  getNodeSlice("nodes-zone-c", 2*maxIGSize),
			kubeNodesZoneA: getNodeSlice("nodes-zone-a", maxIGSize),
			kubeNodesZoneB: getNodeSlice("nodes-zone-b", 2*maxIGSize+1),
			kubeNodesZoneC: getNodeSlice("nodes-zone-c", 2*maxIGSize+2),
		},
		{
			description:    "too many kube nodes in 3 of 3 zones",
			gceNodesZoneA:  getNodeSlice("nodes-zone-a", 2*maxIGSize),
			gceNodesZoneB:  getNodeSlice("nodes-zone-b", 2*maxIGSize+1),
			gceNodesZoneC:  getNodeSlice("nodes-zone-c", 2*maxIGSize+2),
			kubeNodesZoneA: getNodeSlice("nodes-zone-a", 2*maxIGSize),
			kubeNodesZoneB: getNodeSlice("nodes-zone-b", 2*maxIGSize),
			kubeNodesZoneC: getNodeSlice("nodes-zone-c", 2*maxIGSize),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {

			igName := defaultNamer.InstanceGroup()
			fakeGCEInstanceGroups := NewFakeInstanceGroups(map[string]IGsToInstances{}, maxIGSize)
			pool := newNodePool(fakeGCEInstanceGroups, maxIGSize)
			manager := pool.(*manager)
			zonegetter.AddFakeNodes(manager.ZoneGetter, testZoneA, tc.gceNodesZoneA.List()...)
			zonegetter.AddFakeNodes(manager.ZoneGetter, testZoneB, tc.gceNodesZoneB.List()...)
			zonegetter.AddFakeNodes(manager.ZoneGetter, testZoneC, tc.gceNodesZoneC.List()...)

			ports := []int64{80}
			_, err := pool.EnsureInstanceGroupsAndPorts(igName, ports, klog.TODO())
			if err != nil {
				t.Fatalf("pool.EnsureInstanceGroupsAndPorts(%s, %v) returned error %v, want nil", igName, ports, err)
			}
			allKubeNodes := append(tc.kubeNodesZoneA.List(), tc.kubeNodesZoneB.List()...)
			allKubeNodes = append(allKubeNodes, tc.kubeNodesZoneC.List()...)

			// Execute manager's main instance group sync function
			err = pool.Sync(allKubeNodes, klog.TODO())
			if err != nil {
				t.Fatalf("pool.Sync(_) returned error %v, want nil", err)
			}

			// Check that instance group in each zone has only `maxIGSize` number of nodes
			// including zones with 2*maxIGSize nodes
			for _, zone := range []string{testZoneA, testZoneB, testZoneC} {
				numberOfIGsInZone := len(fakeGCEInstanceGroups.zonesToIGsToInstances[zone])
				if numberOfIGsInZone != 1 {
					t.Errorf("Unexpected instance group added, got %v, want: 1", numberOfIGsInZone)
				}
				for _, igToInstances := range fakeGCEInstanceGroups.zonesToIGsToInstances[zone] {
					t.Logf("number of nodes in instance group from zone: %v, got %v", zone, len(igToInstances))
					if len(igToInstances) > maxIGSize {
						t.Errorf("unexpected number of nodes in instance group from zone: %v, got %v, want: %v", zone, len(igToInstances), maxIGSize)
					}
				}
			}

			apiCallsCountBeforeSync := len(fakeGCEInstanceGroups.calls)
			err = pool.Sync(allKubeNodes, klog.TODO())
			if err != nil {
				t.Fatalf("pool.Sync(_) returned error %v, want nil", err)
			}
			apiCallsCountAfterSync := len(fakeGCEInstanceGroups.calls)
			if apiCallsCountBeforeSync != apiCallsCountAfterSync {
				t.Errorf("Should skip sync if called second time with the same kubeNodes. apiCallsCountBeforeSync = %d, apiCallsCountAfterSync = %d", apiCallsCountBeforeSync, apiCallsCountAfterSync)
			}
		})
	}
}

// TestInstanceTruncatingOrder verifies if nodes over maxIGSize are truncated from the last one (alphabetically)
func TestInstanceTruncatingOrder(t *testing.T) {

	maxIGSize := 3
	gceNodesZoneA := []string{"d-node", "c-node", "b-node", "a-node"}
	kubeNodesZoneA := []string{"d-node", "c-node", "b-node", "a-node"}

	igName := defaultNamer.InstanceGroup()
	fakeGCEInstanceGroups := NewFakeInstanceGroups(map[string]IGsToInstances{}, maxIGSize)
	pool := newNodePool(fakeGCEInstanceGroups, maxIGSize)
	manager := pool.(*manager)
	zonegetter.AddFakeNodes(manager.ZoneGetter, testZoneA, gceNodesZoneA...)

	ports := []int64{80}
	_, err := pool.EnsureInstanceGroupsAndPorts(igName, ports, klog.TODO())
	if err != nil {
		t.Fatalf("pool.EnsureInstanceGroupsAndPorts(%s, %v) returned error %v, want nil", igName, ports, err)
	}

	// Execute manager's main instance group sync function
	err = pool.Sync(kubeNodesZoneA, klog.TODO())
	if err != nil {
		t.Fatalf("pool.Sync(_) returned error %v, want nil", err)
	}

	numberOfIGsInZone := len(fakeGCEInstanceGroups.zonesToIGsToInstances[testZoneA])
	if numberOfIGsInZone != 1 {
		t.Errorf("Unexpected instance group added, got %v, want: 1", numberOfIGsInZone)
	}
	for _, instancesSet := range fakeGCEInstanceGroups.zonesToIGsToInstances[testZoneA] {
		if instancesSet.Has("d-node") {
			t.Errorf("Last nodes (alphabetically) should be truncated first.")
		}

	}
}

// TestEnsureInstanceGroupsAndPortsCreatesGroupForAllNodes verifies if instance groups are ensured for all zones, even if all nodes in a zone are unready or otherwise unfit for LB.
func TestEnsureInstanceGroupsAndPortsCreatesGroupForAllNodes(t *testing.T) {

	maxIGSize := 3
	gceNodesZoneA := []string{"a-node"}

	igName := defaultNamer.InstanceGroup()
	fakeGCEInstanceGroups := NewFakeInstanceGroups(map[string]IGsToInstances{}, maxIGSize)
	pool := newNodePool(fakeGCEInstanceGroups, maxIGSize)
	manager := pool.(*manager)
	zonegetter.AddFakeNodes(manager.ZoneGetter, testZoneA, gceNodesZoneA...)
	// add an unready node in the zone B
	zonegetter.AddFakeNode(manager.ZoneGetter, &apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "abc",
			Labels: map[string]string{
				utils.LabelNodeSubnet: "default",
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: fmt.Sprintf("gce://foo-project/%s/instance1", testZoneB),
			PodCIDR:    "10.100.5.0/24",
			PodCIDRs:   []string{"10.100.5.0/24"},
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionFalse,
				},
			},
		},
	})

	ports := []int64{80}
	_, err := pool.EnsureInstanceGroupsAndPorts(igName, ports, klog.TODO())
	if err != nil {
		t.Fatalf("pool.EnsureInstanceGroupsAndPorts(%s, %v) returned error %v, want nil", igName, ports, err)
	}

	igZoneA, err := fakeGCEInstanceGroups.ListInstanceGroups(testZoneA)
	if err != nil {
		t.Errorf("Error listing IG from zone %q", testZoneA)
	}
	if len(igZoneA) != 1 {
		t.Errorf("Expected 1 instance group to be created in zone %s but got %d, %+v", testZoneA, len(igZoneA), igZoneA)
	}

	igZoneB, err := fakeGCEInstanceGroups.ListInstanceGroups(testZoneB)
	if err != nil {
		t.Errorf("Error listing IG from zone %q", testZoneB)
	}
	if len(igZoneB) != 1 {
		t.Errorf("Expected 1 instance group to be created in zone %s but got %d, %+v", testZoneB, len(igZoneB), igZoneB)
	}
}

func getNodeSlice(prefix string, size int) sets.String {
	nodes := make([]string, size)
	for i := 0; i < size; i++ {
		nodes[i] = fmt.Sprintf("%s-%d", prefix, i)
	}
	return sets.NewString(nodes...)
}

func TestSetNamedPorts(t *testing.T) {
	maxIGSize := 1000
	zonesToIGs := map[string]IGsToInstances{
		defaultTestZone: {
			&compute.InstanceGroup{Name: "ig"}: sets.NewString("ig"),
		},
	}
	fakeIGs := NewFakeInstanceGroups(zonesToIGs, maxIGSize)
	pool := newNodePool(fakeIGs, maxIGSize)
	manager := pool.(*manager)
	zonegetter.AddFakeNodes(manager.ZoneGetter, defaultTestZone, "test-node")

	testCases := []struct {
		activePorts   []int64
		expectedPorts []int64
	}{
		{
			// Verify setting a port works as expected.
			[]int64{80},
			[]int64{80},
		},
		{
			// Utilizing multiple new ports
			[]int64{81, 82},
			[]int64{80, 81, 82},
		},
		{
			// Utilizing existing ports
			[]int64{80, 82},
			[]int64{80, 81, 82},
		},
		{
			// Utilizing a new port and an old port
			[]int64{80, 83},
			[]int64{80, 81, 82, 83},
		},
		// TODO: Add tests to remove named ports when we support that.
	}
	for _, testCase := range testCases {
		igs, err := pool.EnsureInstanceGroupsAndPorts("ig", testCase.activePorts, klog.TODO())
		if err != nil {
			t.Fatalf("unexpected error in setting ports %v to instance group: %s", testCase.activePorts, err)
		}
		if len(igs) != 1 {
			t.Fatalf("expected a single instance group, got: %v", igs)
		}
		actualPorts := igs[0].NamedPorts
		if len(actualPorts) != len(testCase.expectedPorts) {
			t.Fatalf("unexpected named ports on instance group. expected: %v, got: %v", testCase.expectedPorts, actualPorts)
		}
		for i, p := range igs[0].NamedPorts {
			if p.Port != testCase.expectedPorts[i] {
				t.Fatalf("unexpected named ports on instance group. expected: %v, got: %v", testCase.expectedPorts, actualPorts)
			}
		}
	}
}

func TestGetInstanceReferences(t *testing.T) {
	maxIGSize := 1000
	zonesToIGs := map[string]IGsToInstances{
		defaultTestZone: {
			&compute.InstanceGroup{Name: "ig"}: sets.NewString("ig"),
		},
	}
	pool := newNodePool(NewFakeInstanceGroups(zonesToIGs, maxIGSize), maxIGSize)
	instances := pool.(*manager)

	nodeNames := []string{"node-1", "node-2", "node-3", "node-4.region.zone"}

	expectedRefs := map[string]struct{}{}
	for _, nodeName := range nodeNames {
		name := strings.Split(nodeName, ".")[0]
		expectedRefs[fmt.Sprintf("%szones/%s/instances/%s", basePath, defaultTestZone, name)] = struct{}{}
	}

	refs := instances.getInstanceReferences(defaultTestZone, nodeNames)
	for _, ref := range refs {
		if _, ok := expectedRefs[ref.Instance]; !ok {
			t.Errorf("found unexpected reference: %s, expected only %+v", ref.Instance, expectedRefs)
		}
	}
}
