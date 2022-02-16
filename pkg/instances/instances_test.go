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

package instances

import (
	"fmt"
	"strings"
	"testing"

	"google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils/namer"
)

const (
	defaultZone = "default-zone"
	basePath    = "/basepath/projects/project-id/"
)

var defaultNamer = namer.NewNamer("uid1", "fw1")

func newNodePool(f *FakeInstanceGroups, zone string) NodePool {
	fakeZL := &FakeZoneLister{[]string{zone}}
	pool := NewNodePool(f, defaultNamer, &test.FakeRecorderSource{}, basePath, fakeZL)
	return pool
}

func TestNodePoolSync(t *testing.T) {
	ig := &compute.InstanceGroup{Name: defaultNamer.InstanceGroup()}
	fakeIGs := NewFakeInstanceGroups(map[string]IGsToInstances{
		defaultZone: {
			ig: sets.NewString("n1", "n2"),
		},
	})
	pool := newNodePool(fakeIGs, defaultZone)
	pool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{80})

	// KubeNodes: n1
	// GCENodes: n1, n2
	// Remove n2 from the instance group.

	fakeIGs.calls = []int{}
	kubeNodes := sets.NewString("n1")
	pool.Sync(kubeNodes.List())
	instancesList, err := fakeIGs.ListInstancesInInstanceGroup(ig.Name, defaultZone, allInstances)
	if err != nil {
		t.Fatalf("Error while listing instances in instance group: %v", err)
	}
	instances, err := test.InstancesListToNameSet(instancesList)
	if err != nil {
		t.Fatalf("Error while getting instances in instance group. IG: %v Error: %v", ig, err)
	}
	if instances.Len() != kubeNodes.Len() || !kubeNodes.IsSuperset(instances) {
		t.Fatalf("%v != %v", kubeNodes, instances)
	}

	// KubeNodes: n1, n2
	// GCENodes: n1
	// Try to add n2 to the instance group.

	fakeIGs = NewFakeInstanceGroups(map[string]IGsToInstances{
		defaultZone: {
			ig: sets.NewString("n1"),
		},
	})
	pool = newNodePool(fakeIGs, defaultZone)
	pool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{80})

	fakeIGs.calls = []int{}
	kubeNodes = sets.NewString("n1", "n2")
	pool.Sync(kubeNodes.List())
	instancesList, err = fakeIGs.ListInstancesInInstanceGroup(ig.Name, defaultZone, allInstances)
	if err != nil {
		t.Fatalf("Error while listing instances in instance group: %v", err)
	}
	instances, err = test.InstancesListToNameSet(instancesList)
	if err != nil {
		t.Fatalf("Error while getting instances in instance group. IG: %v Error: %v", ig, err)
	}
	if instances.Len() != kubeNodes.Len() ||
		!kubeNodes.IsSuperset(instances) {
		t.Fatalf("%v != %v", kubeNodes, instances)
	}

	// KubeNodes: n1, n2
	// GCENodes: n1, n2
	// Do nothing.

	fakeIGs = NewFakeInstanceGroups(map[string]IGsToInstances{
		defaultZone: {
			ig: sets.NewString("n1", "n2"),
		},
	})
	pool = newNodePool(fakeIGs, defaultZone)
	pool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{80})

	fakeIGs.calls = []int{}
	kubeNodes = sets.NewString("n1", "n2")
	pool.Sync(kubeNodes.List())
	if len(fakeIGs.calls) != 0 {
		t.Fatalf(
			"Did not expect any calls, got %+v", fakeIGs.calls)
	}
}

func TestSetNamedPorts(t *testing.T) {
	fakeIGs := NewFakeInstanceGroups(map[string]IGsToInstances{
		defaultZone: {
			&compute.InstanceGroup{Name: "ig"}: sets.NewString("ig"),
		},
	})
	pool := newNodePool(fakeIGs, defaultZone)

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
	pool := newNodePool(NewFakeInstanceGroups(map[string]IGsToInstances{
		defaultZone: {
			&compute.InstanceGroup{Name: "ig"}: sets.NewString("ig"),
		},
	}), defaultZone)
	instances := pool.(*Instances)

	nodeNames := []string{"node-1", "node-2", "node-3", "node-4.region.zone"}

	expectedRefs := map[string]struct{}{}
	for _, nodeName := range nodeNames {
		name := strings.Split(nodeName, ".")[0]
		expectedRefs[fmt.Sprintf("%szones/%s/instances/%s", basePath, defaultZone, name)] = struct{}{}
	}

	refs := instances.getInstanceReferences(defaultZone, nodeNames)
	for _, ref := range refs {
		if _, ok := expectedRefs[ref.Instance]; !ok {
			t.Errorf("found unexpected reference: %s, expected only %+v", ref.Instance, expectedRefs)
		}
	}
}
