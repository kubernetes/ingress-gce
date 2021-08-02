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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	befeatures "k8s.io/ingress-gce/pkg/backends/features"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

// negLinker handles linking backends to NEG's.
type negLinker struct {
	backendPool  Pool
	negGetter    NEGGetter
	cloud        *gce.Cloud
	svcNegLister cache.Indexer
}

// negLinker is a Linker
var _ Linker = (*negLinker)(nil)

func NewNEGLinker(
	backendPool Pool,
	negGetter NEGGetter,
	cloud *gce.Cloud,
	svcNegLister cache.Indexer,
) Linker {
	return &negLinker{
		backendPool:  backendPool,
		negGetter:    negGetter,
		cloud:        cloud,
		svcNegLister: svcNegLister,
	}
}

// Link implements Link.
func (l *negLinker) Link(sp utils.ServicePort, groups []GroupKey) error {
	version := befeatures.VersionFromServicePort(&sp)
	var negSelfLinks []string
	var err error
	for _, group := range groups {
		// If the group key contains a name, then use that.
		// Otherwise, get the name from svc port.
		negName := group.Name
		if negName == "" {
			negName = sp.BackendName()
		}

		negUrl := ""
		svcNegKey := fmt.Sprintf("%s/%s", sp.ID.Service.Namespace, negName)
		negUrl, ok := getNegUrlFromSvcneg(svcNegKey, group.Zone, l.svcNegLister)
		if !ok {
			klog.V(4).Infof("Falling back to use NEG API to retreive NEG url for NEG %q", negName)
			neg, err := l.negGetter.GetNetworkEndpointGroup(negName, group.Zone, version)
			if err != nil {
				return err
			}
			negUrl = neg.SelfLink
		}
		negSelfLinks = append(negSelfLinks, negUrl)
	}

	beName := sp.BackendName()
	scope := befeatures.ScopeFromServicePort(&sp)

	key, err := composite.CreateKey(l.cloud, beName, scope)
	if err != nil {
		return err
	}
	backendService, err := composite.GetBackendService(l.cloud, key, version)
	if err != nil {
		return err
	}

	newBackends := backendsForNEGs(negSelfLinks, &sp)
	diff := diffBackends(backendService.Backends, newBackends)
	if diff.isEqual() {
		klog.V(2).Infof("No changes in backends for service port %s", sp.ID)
		return nil
	}
	klog.V(2).Infof("Backends changed for service port %s, removing: %s, adding: %s, changed: %s", sp.ID, diff.toRemove(), diff.toAdd(), diff.changed)
	backendService.Backends = newBackends

	return composite.UpdateBackendService(l.cloud, key, backendService)
}

type backendDiff struct {
	old     sets.String
	new     sets.String
	changed sets.String
}

func diffBackends(old, new []*composite.Backend) *backendDiff {
	d := &backendDiff{
		old:     sets.NewString(),
		new:     sets.NewString(),
		changed: sets.NewString(),
	}

	oldMap := map[string]*composite.Backend{}
	for _, be := range old {
		d.old.Insert(be.Group)
		oldMap[be.Group] = be
	}
	for _, be := range new {
		d.new.Insert(be.Group)

		if oldBe, ok := oldMap[be.Group]; ok {
			// Note: if you are comparing a value that has a non-zero default
			// value (e.g. CapacityScaler is 1.0), you will need to set that
			// value when creating a new Backend to avoid a false positive when
			// computing diffs.
			if flags.F.EnableTrafficScaling {
				var changed bool
				changed = changed || oldBe.MaxRatePerEndpoint != be.MaxRatePerEndpoint
				changed = changed || oldBe.CapacityScaler != be.CapacityScaler
				if changed {
					d.changed.Insert(be.Group)
				}
			}
		}
	}

	return d
}

func (d *backendDiff) isEqual() bool         { return d.old.Equal(d.new) && d.changed.Len() == 0 }
func (d *backendDiff) toRemove() sets.String { return d.old.Difference(d.new) }
func (d *backendDiff) toAdd() sets.String    { return d.new.Difference(d.old) }

func backendsForNEGs(negSelfLinks []string, sp *utils.ServicePort) []*composite.Backend {
	var backends []*composite.Backend
	for _, neg := range negSelfLinks {
		newBackend := &composite.Backend{Group: neg}

		switch getNegType(*sp) {
		case types.VmIpEndpointType:
			// Setting MaxConnectionsPerEndpoint is not supported for L4 ILB
			// https://cloud.google.com/load-balancing/docs/backend-service#target_capacity
			// hence only mode is being set.
			newBackend.BalancingMode = string(Connections)

		case types.VmIpPortEndpointType:
			// This preserves the original behavior, but really we should error
			// when there is a type we don't understand.
			fallthrough
		default:
			newBackend.BalancingMode = string(Rate)
			newBackend.MaxRatePerEndpoint = maxRPS
			newBackend.CapacityScaler = 1.0

			if flags.F.EnableTrafficScaling {
				if sp.MaxRatePerEndpoint != nil {
					newBackend.MaxRatePerEndpoint = float64(*sp.MaxRatePerEndpoint)
				}
				if sp.CapacityScaler != nil {
					newBackend.CapacityScaler = *sp.CapacityScaler
				}
			}
		}

		backends = append(backends, newBackend)
	}
	return backends
}

// getNegType returns NEG type based on service port config
func getNegType(sp utils.ServicePort) types.NetworkEndpointType {
	if sp.VMIPNEGEnabled {
		return types.VmIpEndpointType
	}
	return types.VmIpPortEndpointType
}

// getNegUrlFromSvcneg return NEG url from svcneg status if found
func getNegUrlFromSvcneg(key string, zone string, svcNegLister cache.Indexer) (string, bool) {
	obj, exists, err := svcNegLister.GetByKey(key)
	if err != nil {
		klog.Errorf("Failed to retrieve svcneg %s from cache: %v", key, err)
		return "", false
	}
	if !exists {
		return "", false
	}
	svcneg := obj.(*negv1beta1.ServiceNetworkEndpointGroup)

	for _, negRef := range svcneg.Status.NetworkEndpointGroups {
		key, err := cloud.ParseResourceURL(negRef.SelfLink)
		if err != nil {
			klog.Errorf("Failed to parse NEG SelfLink from svcneg %v: %v", svcneg, err)
			continue
		}
		if key.Key.Zone == zone {
			return negRef.SelfLink, true
		}
	}
	return "", false
}
