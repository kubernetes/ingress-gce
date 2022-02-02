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

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
)

// NewEmptyFakeInstanceGroups creates a new FakeInstanceGroups without zones, igs or instances.
func NewEmptyFakeInstanceGroups() *FakeInstanceGroups {
	return &FakeInstanceGroups{
		zonesToIGsToInstances: map[string]IGsToInstances{},
	}
}

// NewFakeInstanceGroups creates a new FakeInstanceGroups.
func NewFakeInstanceGroups(zonesToIGsToInstances map[string]IGsToInstances) *FakeInstanceGroups {
	return &FakeInstanceGroups{
		zonesToIGsToInstances: zonesToIGsToInstances,
	}
}

// FakeZoneLister records zones for nodes.
type FakeZoneLister struct {
	Zones []string
}

// ListZones returns the list of zones.
func (z *FakeZoneLister) ListZones(_ utils.NodeConditionPredicate) ([]string, error) {
	return z.Zones, nil
}

// GetZoneForNode returns the only zone stored in the fake zone lister.
func (z *FakeZoneLister) GetZoneForNode(name string) (string, error) {
	// TODO: evolve as required, it's currently needed just to satisfy the
	// interface in unittests that don't care about zones. See unittests in
	// controller/util_test for actual zoneLister testing.
	return z.Zones[0], nil
}

type IGsToInstances map[*compute.InstanceGroup]sets.String

// FakeInstanceGroups fakes out the instance groups api.
type FakeInstanceGroups struct {
	getResult             *compute.InstanceGroup
	calls                 []int
	zonesToIGsToInstances map[string]IGsToInstances
}

// getInstanceGroup implements fake getting ig by name in zone
func (f *FakeInstanceGroups) getInstanceGroup(name, zone string) (*compute.InstanceGroup, error) {
	for ig := range f.zonesToIGsToInstances[zone] {
		if ig.Name == name {
			return ig, nil
		}
	}

	return nil, test.FakeGoogleAPINotFoundErr()
}

// GetInstanceGroup fakes getting an instance group from the cloud.
func (f *FakeInstanceGroups) GetInstanceGroup(name, zone string) (*compute.InstanceGroup, error) {
	f.calls = append(f.calls, utils.Get)
	return f.getInstanceGroup(name, zone)
}

// CreateInstanceGroup fakes instance group creation.
func (f *FakeInstanceGroups) CreateInstanceGroup(ig *compute.InstanceGroup, zone string) error {
	if _, ok := f.zonesToIGsToInstances[zone]; !ok {
		f.zonesToIGsToInstances[zone] = map[*compute.InstanceGroup]sets.String{}
	}
	if _, ok := f.zonesToIGsToInstances[zone][ig]; ok {
		return test.FakeGoogleAPIConflictErr()
	}

	ig.SelfLink = cloud.NewInstanceGroupsResourceID("mock-project", zone, ig.Name).SelfLink(meta.VersionGA)
	ig.Zone = zone
	f.zonesToIGsToInstances[zone][ig] = sets.NewString()
	return nil
}

// DeleteInstanceGroup fakes instance group deletion.
func (f *FakeInstanceGroups) DeleteInstanceGroup(name, zone string) error {
	ig, err := f.getInstanceGroup(name, zone)
	if err != nil {
		return err
	}
	delete(f.zonesToIGsToInstances[zone], ig)
	return nil
}

// ListInstancesInInstanceGroup fakes listing instances in an instance group.
func (f *FakeInstanceGroups) ListInstancesInInstanceGroup(name, zone string, state string) ([]*compute.InstanceWithNamedPorts, error) {
	ig, err := f.getInstanceGroup(name, zone)
	if err != nil {
		return nil, err
	}
	return getInstanceList(f.zonesToIGsToInstances[zone][ig]).Items, nil
}

// ListInstanceGroups fakes listing instance groups in a zone
func (f *FakeInstanceGroups) ListInstanceGroups(zone string) ([]*compute.InstanceGroup, error) {
	igs := []*compute.InstanceGroup{}
	for ig := range f.zonesToIGsToInstances[zone] {
		igs = append(igs, ig)
	}
	return igs, nil
}

// AddInstancesToInstanceGroup fakes adding instances to an instance group.
func (f *FakeInstanceGroups) AddInstancesToInstanceGroup(name, zone string, instanceRefs []*compute.InstanceReference) error {
	instanceNames := toInstanceNames(instanceRefs)
	f.calls = append(f.calls, utils.AddInstances)
	ig, err := f.getInstanceGroup(name, zone)
	if err != nil {
		return err
	}

	f.zonesToIGsToInstances[zone][ig].Insert(instanceNames...)
	return nil
}

// RemoveInstancesFromInstanceGroup fakes removing instances from an instance group.
func (f *FakeInstanceGroups) RemoveInstancesFromInstanceGroup(name, zone string, instanceRefs []*compute.InstanceReference) error {
	instanceNames := toInstanceNames(instanceRefs)
	f.calls = append(f.calls, utils.RemoveInstances)
	ig, err := f.getInstanceGroup(name, zone)
	if err != nil {
		return err
	}
	f.zonesToIGsToInstances[zone][ig].Delete(instanceNames...)
	return nil
}

func (f *FakeInstanceGroups) SetNamedPortsOfInstanceGroup(igName, zone string, namedPorts []*compute.NamedPort) error {
	ig, err := f.getInstanceGroup(igName, zone)
	if err != nil {
		return err
	}
	ig.NamedPorts = namedPorts
	return nil
}

// getInstanceList returns an instance list based on the given names.
// The names cannot contain a '.', the real gce api validates against this.
func getInstanceList(nodeNames sets.String) *compute.InstanceGroupsListInstances {
	instanceNames := nodeNames.List()
	computeInstances := []*compute.InstanceWithNamedPorts{}
	for _, name := range instanceNames {
		instanceLink := getInstanceUrl(name)
		computeInstances = append(
			computeInstances, &compute.InstanceWithNamedPorts{
				Instance: instanceLink})
	}
	return &compute.InstanceGroupsListInstances{
		Items: computeInstances,
	}
}

func (f *FakeInstanceGroups) ToInstanceReferences(zone string, instanceNames []string) (refs []*compute.InstanceReference) {
	for _, ins := range instanceNames {
		instanceLink := getInstanceUrl(ins)
		refs = append(refs, &compute.InstanceReference{Instance: instanceLink})
	}
	return refs
}

func getInstanceUrl(instanceName string) string {
	return fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/instances/%s",
		"project", "zone", instanceName)
}

func toInstanceNames(instanceRefs []*compute.InstanceReference) []string {
	instanceNames := make([]string, len(instanceRefs))
	for ix := range instanceRefs {
		url := instanceRefs[ix].Instance
		parts := strings.Split(url, "/")
		instanceNames[ix] = parts[len(parts)-1]
	}
	return instanceNames
}
