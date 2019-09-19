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
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/utils/namer"
)

const defaultZone = "default-zone"

var defaultNamer = namer.NewNamer("uid1", "fw1")

func newNodePool(f *FakeInstanceGroups, zone string) NodePool {
	pool := NewNodePool(f, defaultNamer)
	pool.Init(&FakeZoneLister{[]string{zone}})
	return pool
}

func TestNodePoolSync(t *testing.T) {
	f := NewFakeInstanceGroups(sets.NewString([]string{"n1", "n2"}...), defaultNamer)
	pool := newNodePool(f, defaultZone)
	pool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{80})

	// KubeNodes: n1
	// GCENodes: n1, n2
	// Remove n2 from the instance group.

	f.calls = []int{}
	kubeNodes := sets.NewString([]string{"n1"}...)
	pool.Sync(kubeNodes.List())
	if f.instances.Len() != kubeNodes.Len() || !kubeNodes.IsSuperset(f.instances) {
		t.Fatalf("%v != %v", kubeNodes, f.instances)
	}

	// KubeNodes: n1, n2
	// GCENodes: n1
	// Try to add n2 to the instance group.

	f = NewFakeInstanceGroups(sets.NewString([]string{"n1"}...), defaultNamer)
	pool = newNodePool(f, defaultZone)
	pool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{80})

	f.calls = []int{}
	kubeNodes = sets.NewString([]string{"n1", "n2"}...)
	pool.Sync(kubeNodes.List())
	if f.instances.Len() != kubeNodes.Len() ||
		!kubeNodes.IsSuperset(f.instances) {
		t.Fatalf("%v != %v", kubeNodes, f.instances)
	}

	// KubeNodes: n1, n2
	// GCENodes: n1, n2
	// Do nothing.

	f = NewFakeInstanceGroups(sets.NewString([]string{"n1", "n2"}...), defaultNamer)
	pool = newNodePool(f, defaultZone)
	pool.EnsureInstanceGroupsAndPorts(defaultNamer.InstanceGroup(), []int64{80})

	f.calls = []int{}
	kubeNodes = sets.NewString([]string{"n1", "n2"}...)
	pool.Sync(kubeNodes.List())
	if len(f.calls) != 0 {
		t.Fatalf(
			"Did not expect any calls, got %+v", f.calls)
	}
}

func TestSetNamedPorts(t *testing.T) {
	f := NewFakeInstanceGroups(sets.NewString([]string{"ig"}...), defaultNamer)
	pool := newNodePool(f, defaultZone)

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
	for _, test := range testCases {
		igs, err := pool.EnsureInstanceGroupsAndPorts("ig", test.activePorts)
		if err != nil {
			t.Fatalf("unexpected error in setting ports %v to instance group: %s", test.activePorts, err)
		}
		if len(igs) != 1 {
			t.Fatalf("expected a single instance group, got: %v", igs)
		}
		actualPorts := igs[0].NamedPorts
		if len(actualPorts) != len(test.expectedPorts) {
			t.Fatalf("unexpected named ports on instance group. expected: %v, got: %v", test.expectedPorts, actualPorts)
		}
		for i, p := range igs[0].NamedPorts {
			if p.Port != test.expectedPorts[i] {
				t.Fatalf("unexpected named ports on instance group. expected: %v, got: %v", test.expectedPorts, actualPorts)
			}
		}
	}
}
