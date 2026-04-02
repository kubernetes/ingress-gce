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
	"k8s.io/ingress-gce/pkg/instancegroups"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// RegionalInstanceGroupLinker handles linking backends to InstanceGroups.
type RegionalInstanceGroupLinker struct {
	instancePool instancegroups.Manager
	backendPool  *Pool

	logger klog.Logger
}

// NewRegionalInstanceGroupLinker creates an instance of RegionalInstanceGroupLinker
func NewRegionalInstanceGroupLinker(instancePool instancegroups.Manager, backendPool *Pool, logger klog.Logger) *RegionalInstanceGroupLinker {
	return &RegionalInstanceGroupLinker{
		instancePool: instancePool,
		backendPool:  backendPool,
		logger:       logger.WithName("RegionalInstanceGroupLinker"),
	}
}

// Link performs linking instance groups to regional backend service
func (linker *RegionalInstanceGroupLinker) Link(sp utils.ServicePort, projectID string, zones []string) error {
	linker.logger.V(2).Info("Link", "servicePort", sp, "projectID", projectID, "zones", zones)

	var igLinks []string
	for _, zone := range zones {
		key := meta.ZonalKey(sp.IGName(), zone)
		igSelfLink := cloudprovider.SelfLink(meta.VersionGA, projectID, "instanceGroups", key)
		igLinks = append(igLinks, igSelfLink)
	}
	// TODO(cheungdavid): Create regional ig linker logger that contains backendName,
	// backendVersion, and backendScope before passing to backendPool.Get().
	// See example in backendSyncer.ensureBackendService().
	bs, err := linker.backendPool.Get(sp.BackendName(), meta.VersionGA, meta.Regional, linker.logger)
	if err != nil {
		return err
	}
	addIGs, removeIGs, err := getInstanceGroupsToAddAndRemove(bs, igLinks, linker.logger)
	if err != nil {
		return err
	}
	if len(addIGs) == 0 && len(removeIGs) == 0 {
		linker.logger.V(3).Info("No backends to add or remove, skipping update", "backendName", sp.BackendName())
		return nil
	}

	if len(removeIGs) != 0 {
		var backendsWithoutRemoved []*composite.Backend
		for _, b := range bs.Backends {
			path, err := utils.RelativeResourceName(b.Group)
			if err != nil {
				return err
			}
			if !removeIGs.Has(path) {
				backendsWithoutRemoved = append(backendsWithoutRemoved, b)
			}
		}
		bs.Backends = backendsWithoutRemoved
	}

	for _, igLink := range addIGs {
		b := &composite.Backend{
			Group: igLink,
		}
		bs.Backends = append(bs.Backends, b)
	}

	linker.logger.V(3).Info("Update Backend", "backendName", sp.BackendName(), "addedBackends", len(addIGs), "totalBackends", len(bs.Backends))
	if err := linker.backendPool.Update(bs, linker.logger); err != nil {
		return fmt.Errorf("updating backend service %s for IG failed, err:%w", sp.BackendName(), err)
	}
	return nil
}

func getInstanceGroupsToAddAndRemove(be *composite.BackendService, igLinks []string, logger klog.Logger) ([]string, sets.String, error) {
	existingIGs := sets.String{}
	for _, existingBe := range be.Backends {
		path, err := utils.RelativeResourceName(existingBe.Group)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse instance group: %w", err)
		}
		existingIGs.Insert(path)
	}

	wantIGs := sets.String{}
	for _, igLink := range igLinks {
		path, err := utils.RelativeResourceName(igLink)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse instance group: %w", err)
		}
		wantIGs.Insert(path)
	}

	missingIGs := wantIGs.Difference(existingIGs)
	removeIGs := existingIGs.Difference(wantIGs)
	if missingIGs.Len() > 0 || removeIGs.Len() > 0 {
		logger.V(2).Info(fmt.Sprintf("Backend service has instance groups %+v, want %+v", existingIGs.List(), wantIGs.List()), "backendService", be.Name)
	}
	return missingIGs.List(), removeIGs, nil
}
