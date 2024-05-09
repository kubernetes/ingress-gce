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
	"strings"
	"testing"

	"k8s.io/klog/v2"

	"google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
)

const (
	defaultTestZone = "default-zone"
	basePath        = "/basepath/projects/project-id/"

	defaultTestSubnetURL = "https://www.googleapis.com/compute/v1/projects/proj/regions/us-central1/subnetworks/default"
)

var defaultNamer = namer.NewNamer("uid1", "fw1", klog.TODO())

func newNodePool(f *FakeInstanceGroups, zone string, maxIGSize int) Manager {
	nodeInformer := zonegetter.FakeNodeInformer()
	fakeZoneGetter := zonegetter.NewZoneGetter(nodeInformer, defaultTestSubnetURL)

	pool := NewManager(&ManagerConfig{
		Cloud:      f,
		Namer:      defaultNamer,
		Recorders:  &test.FakeRecorderSource{},
		BasePath:   basePath,
		ZoneGetter: fakeZoneGetter,
		MaxIGSize:  maxIGSize,
	}, klog.TODO())
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

		pool := newNodePool(fakeGCEInstanceGroups, defaultTestZone, maxIGSize)
		for _, kubeNode := range testCase.kubeNodes.List() {
			manager := pool.(*manager)
			zonegetter.AddFakeNodes(manager.ZoneGetter, defaultTestZone, kubeNode)
		}

		igName := defaultNamer.InstanceGroup()
		ports := []int64{80}
		_, err := pool.EnsureInstanceGroupsAndPorts(igName, ports)
		if err != nil {
			t.Fatalf("pool.EnsureInstanceGroupsAndPorts(%s, %v) returned error %v, want nil", igName, ports, err)
		}

		// run sync with expected kubeNodes
		apiCallsCountBeforeSync := len(fakeGCEInstanceGroups.calls)
		err = pool.Sync(testCase.kubeNodes.List())
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
		err = pool.Sync(testCase.kubeNodes.List())
		if err != nil {
			t.Fatalf("pool.Sync(%v) returned error %v, want nil", testCase.kubeNodes.List(), err)
		}
		apiCallsCountAfterSync = len(fakeGCEInstanceGroups.calls)
		if apiCallsCountBeforeSync != apiCallsCountAfterSync {
			t.Errorf("Should skip sync if called second time with the same kubeNodes. apiCallsCountBeforeSync = %d, apiCallsCountAfterSync = %d", apiCallsCountBeforeSync, apiCallsCountAfterSync)
		}
	}
}

func TestSetNamedPorts(t *testing.T) {
	maxIGSize := 1000
	zonesToIGs := map[string]IGsToInstances{
		defaultTestZone: {
			&compute.InstanceGroup{Name: "ig"}: sets.NewString("ig"),
		},
	}
	fakeIGs := NewFakeInstanceGroups(zonesToIGs, maxIGSize)
	pool := newNodePool(fakeIGs, defaultTestZone, maxIGSize)
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
		igs, err := pool.EnsureInstanceGroupsAndPorts("ig", testCase.activePorts)
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
	pool := newNodePool(NewFakeInstanceGroups(zonesToIGs, maxIGSize), defaultTestZone, maxIGSize)
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
