/*
Copyright 2021 The Kubernetes Authors.
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

package backends

import (
	"fmt"

	cloudprovider "github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/utils"
)

// RegionalInstanceGroupLinker handles linking backends to InstanceGroups.
type RegionalInstanceGroupLinker struct {
	instancePool instances.NodePool
	backendPool  Pool
}

func NewRegionalInstanceGroupLinker(instancePool instances.NodePool, backendPool Pool) *RegionalInstanceGroupLinker {
	return &RegionalInstanceGroupLinker{
		instancePool: instancePool,
		backendPool:  backendPool,
	}
}

// Link performs linking instance groups to regional backend service
func (linker *RegionalInstanceGroupLinker) Link(sp utils.ServicePort, projectID string, zones []string) error {
	var igLinks []string

	for _, zone := range zones {
		key := meta.ZonalKey(sp.IGName(), zone)
		//TODO (cezarygerard): link all IGs by reusing []*compute.InstanceGroup returned from instancepool in the l4 controller
		igSelfLink := cloudprovider.SelfLink(meta.VersionGA, projectID, "instanceGroups", key)
		igLinks = append(igLinks, igSelfLink)
	}

	addIGs := sets.String{}
	for _, igLink := range igLinks {
		path, err := utils.RelativeResourceName(igLink)
		if err != nil {
			return fmt.Errorf("failed to parse instance group %s: %w", igLink, err)
		}
		addIGs.Insert(path)
	}
	if len(addIGs) == 0 {
		return nil
	}
	// TODO(kl52752) Check for existing links and add only new one
	var newBackends []*composite.Backend
	for _, igLink := range addIGs.List() {
		b := &composite.Backend{
			Group: igLink,
		}
		newBackends = append(newBackends, b)
	}
	be, err := linker.backendPool.Get(sp.BackendName(), meta.VersionGA, meta.Regional)
	if err != nil {
		return err
	}
	be.Backends = newBackends

	if err := linker.backendPool.Update(be); err != nil {
		return fmt.Errorf("updating backend service %s for IG failed, err:%w", sp.BackendName(), err)
	}
	return nil
}
