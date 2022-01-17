/*
Copyright 2022 The Kubernetes Authors.

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
	compute "google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
)

// MultiIGInstances implements NodePool Interface. It can use multiple instance groups belonging to cluster but it does not
// create/delete them. This is a job for separate controller (MultiIGNodeController).
type MultiIGInstances struct {
	*Instances
}

func NewMultiIGInstances(cloud InstanceGroups, namer namer.BackendNamer, recorders recorderSource, basePath string, zl ZoneLister) *MultiIGInstances {
	multiIGInstances := &MultiIGInstances{
		Instances: &Instances{
			cloud:              cloud,
			namer:              namer,
			recorder:           recorders.Recorder(""), // No namespace
			instanceLinkFormat: basePath + "zones/%s/instances/%s",
			ZoneLister:         zl,
		},
	}
	return multiIGInstances
}

func (igc *MultiIGInstances) DeleteInstanceGroup(name string) error {
	klog.Infof("DeleteInstanceGroup is a no-op. Instance groups will be deleted when the cluster is deleted.")
	return nil
}

func (igc *MultiIGInstances) Sync(nodeNames []string) error {
	klog.Infof("Sync is a no-op. Instance groups will be synced in the separate controller.")
	return nil
}

func (igc *MultiIGInstances) Get(name, zone string) ([]*compute.InstanceGroup, error) {
	var igs []*compute.InstanceGroup
	igsForZone, err := igc.cloud.ListInstanceGroups(zone)
	if err != nil {
		return nil, err
	}
	for _, ig := range igsForZone {
		if igc.namer.NameBelongsToCluster(ig.Name) {
			igs = append(igs, ig)
		}
	}
	if len(igs) == 0 {
		return nil, fmt.Errorf("no Instance Groups belong to cluster")
	}
	return igs, nil
}

func (igc *MultiIGInstances) EnsureInstanceGroupsAndPorts(name string, ports []int64) (igs []*compute.InstanceGroup, err error) {
	// Instance groups need to be created only in zones that have ready nodes.
	zones, err := igc.Instances.ListZones(utils.CandidateNodesPredicate)
	if err != nil {
		return nil, err
	}

	for _, zone := range zones {

		ig, err := igc.Get(name, zone)
		if err != nil {
			return nil, err
		}
		err = igc.setPorts(ig, name, zone, ports)
		if err != nil {
			return nil, err
		}
		igs = append(igs, ig...)
	}
	return igs, nil
}
