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
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
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
	klog.V(2).Infof("Link(%v, %q, %v)", sp, projectID, zones)

	var igLinks []string
	for _, zone := range zones {
		key := meta.ZonalKey(sp.IGName(), zone)
		igSelfLink := cloudprovider.SelfLink(meta.VersionGA, projectID, "instanceGroups", key)
		igLinks = append(igLinks, igSelfLink)
	}
	bs, err := linker.backendPool.Get(sp.BackendName(), meta.VersionGA, meta.Regional)
	if err != nil {
		return err
	}
	addIGs, err := getInstanceGroupsToAdd(bs, igLinks)
	if err != nil {
		return err
	}
	if len(addIGs) == 0 {
		klog.V(3).Infof("No backends to add for %s, skipping update.", sp.BackendName())
		return nil
	}

	var newBackends []*composite.Backend
	for _, igLink := range addIGs {
		b := &composite.Backend{
			Group: igLink,
		}
		newBackends = append(newBackends, b)
	}

	bs.Backends = newBackends
	klog.V(3).Infof("Update Backend %s, with %d backends.", sp.BackendName(), len(addIGs))
	if err := linker.backendPool.Update(bs); err != nil {
		return fmt.Errorf("updating backend service %s for IG failed, err:%w", sp.BackendName(), err)
	}
	return nil
}
