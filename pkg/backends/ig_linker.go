/*
Copyright 2018 The Kubernetes Authors.
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
	"net/http"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
)

// BalancingMode represents the loadbalancing configuration of an individual
// Backend in a BackendService. This is *effectively* a cluster wide setting
// since you can't mix modes across Backends pointing to the same IG, and you
// can't have a single node in more than 1 loadbalanced IG.
type BalancingMode string

const (
	// Rate balances incoming requests based on observed RPS.
	// As of this writing, it's the only balancing mode supported by GCE's
	// internal LB. This setting doesn't make sense for Kubernetes clusters
	// because requests can get proxied between instance groups in different
	// zones by kube-proxy without GCE even knowing it. Setting equal RPS on
	// all IGs should achieve roughly equal distribution of requests.
	Rate BalancingMode = "RATE"
	// Utilization balances incoming requests based on observed utilization.
	// This mode is only useful if you want to divert traffic away from IGs
	// running other compute intensive workloads. Utilization statistics are
	// aggregated per instances, not per container, and requests can get proxied
	// between instance groups in different zones by kube-proxy without GCE even
	// knowing about it.
	Utilization BalancingMode = "UTILIZATION"
	// Connections balances incoming requests based on a connection counter.
	// This setting currently doesn't make sense for Kubernetes clusters,
	// because we use NodePort Services as HTTP LB backends, so GCE's connection
	// counters don't accurately represent connections per container.
	Connections BalancingMode = "CONNECTION"
)

// maxRPS is the RPS setting for all Backends with BalancingMode RATE. The exact
// value doesn't matter, as long as it's the same for all Backends. Requests
// received by GCLB above this RPS are NOT dropped, GCLB continues to distribute
// them across IGs.
// TODO: Should this be math.MaxInt64?
const maxRPS = 1

// instanceGroupLinker handles linking backends to InstanceGroup's.
type instanceGroupLinker struct {
	instancePool instances.NodePool
	backendPool  Pool
	namer        *namer.Namer
}

// instanceGroupLinker is a Linker
var _ Linker = (*instanceGroupLinker)(nil)

func NewInstanceGroupLinker(
	instancePool instances.NodePool,
	backendPool Pool,
	namer *namer.Namer) Linker {
	return &instanceGroupLinker{
		instancePool: instancePool,
		backendPool:  backendPool,
		namer:        namer,
	}
}

// Link implements Link.
func (l *instanceGroupLinker) Link(sp utils.ServicePort, groups []GroupKey) error {
	var igLinks []string
	for _, group := range groups {
		ig, err := l.instancePool.Get(l.namer.InstanceGroup(), group.Zone)
		if err != nil {
			return fmt.Errorf("error retrieving IG for linking with backend %+v: %v", sp, err)
		}
		igLinks = append(igLinks, ig.SelfLink)
	}

	// ig_linker only supports L7 HTTP(s) External Load Balancer
	// Hardcoded here since IGs are not supported for non GA-Global right now
	// TODO(shance): find a way to remove hardcoded values
	be, err := l.backendPool.Get(sp.BackendName(l.namer), meta.VersionGA, meta.Global)
	if err != nil {
		return err
	}

	addIGs, err := getInstanceGroupsToAdd(be, igLinks)
	if err != nil {
		return err
	}

	if len(addIGs) == 0 {
		return nil
	}

	originalIGBackends := []*composite.Backend{}
	for _, backend := range be.Backends {
		// Backend service is not able to point to NEG and IG at the same time.
		// Filter IG backends here.
		if strings.Contains(backend.Group, "instanceGroups") {
			originalIGBackends = append(originalIGBackends, backend)
		}
	}

	// We first try to create the backend with balancingMode=RATE.  If this	+ return addIGs
	// fails, it's mostly likely because there are existing backends with
	// balancingMode=UTILIZATION. This failure mode throws a googleapi error
	// which wraps a HTTP 400 status code. We handle it in the loop below
	// and come around to retry with the right balancing mode. The goal is to
	// switch everyone to using RATE.
	var errs []string
	for _, bm := range []BalancingMode{Rate, Utilization} {
		// Generate backends with given instance groups with a specific mode
		newBackends := getBackendsForIGs(addIGs, bm)
		be.Backends = append(originalIGBackends, newBackends...)

		if err := l.backendPool.Update(be); err != nil {
			if utils.IsHTTPErrorCode(err, http.StatusBadRequest) {
				klog.V(2).Infof("Updating backend service backends with balancing mode %v failed, will try another mode. err:%v", bm, err)
				errs = append(errs, err.Error())
				// This is probably a failure because we tried to create the backend
				// with balancingMode=RATE when there are already backends with
				// balancingMode=UTILIZATION. Just ignore it and retry setting
				// balancingMode=UTILIZATION (b/35102911).
				continue
			}
			klog.V(2).Infof("Error updating backend service backends with balancing mode %v:%v", bm, err)
			return err
		}
		// Successfully updated Backends, no need to Update the BackendService again
		return nil
	}
	return fmt.Errorf("received errors when updating backend service: %v", strings.Join(errs, "\n"))
}

func getBackendsForIGs(igLinks []string, bm BalancingMode) []*composite.Backend {
	var backends []*composite.Backend
	for _, igLink := range igLinks {
		b := &composite.Backend{
			Group:         igLink,
			BalancingMode: string(bm),
		}
		switch bm {
		case Rate:
			b.MaxRatePerInstance = maxRPS
		default:
			// TODO: Set utilization and connection limits when we accept them
			// as valid fields.
		}

		backends = append(backends, b)
	}
	return backends
}

func getInstanceGroupsToAdd(be *composite.BackendService, igLinks []string) ([]string, error) {
	existingIGs := sets.String{}
	for _, existingBe := range be.Backends {
		path, err := utils.RelativeResourceName(existingBe.Group)
		if err != nil {
			return nil, fmt.Errorf("failed to parse instance group: %v", err)
		}
		existingIGs.Insert(path)
	}

	wantIGs := sets.String{}
	for _, igLink := range igLinks {
		path, err := utils.RelativeResourceName(igLink)
		if err != nil {
			return nil, fmt.Errorf("failed to parse instance group: %v", err)
		}
		wantIGs.Insert(path)
	}

	missingIGs := wantIGs.Difference(existingIGs)
	if missingIGs.Len() > 0 {
		klog.V(2).Infof("Backend service %q has instance groups %+v, want %+v",
			be.Name, existingIGs.List(), wantIGs.List())
	}

	return missingIGs.List(), nil
}
