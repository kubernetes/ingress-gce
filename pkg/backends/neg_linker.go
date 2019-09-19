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
	"google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	befeatures "k8s.io/ingress-gce/pkg/backends/features"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
)

// negLinker handles linking backends to NEG's.
type negLinker struct {
	backendPool Pool
	negGetter   NEGGetter
	namer       *namer.Namer
	cloud       *gce.Cloud
}

// negLinker is a Linker
var _ Linker = (*negLinker)(nil)

func NewNEGLinker(
	backendPool Pool,
	negGetter NEGGetter,
	namer *namer.Namer,
	cloud *gce.Cloud) Linker {
	return &negLinker{
		backendPool: backendPool,
		negGetter:   negGetter,
		namer:       namer,
		cloud:       cloud,
	}
}

// Link implements Link.
func (l *negLinker) Link(sp utils.ServicePort, groups []GroupKey) error {
	var negs []*compute.NetworkEndpointGroup
	var err error
	for _, group := range groups {
		// If the group key contains a name, then use that.
		// Otherwise, generate the name using the namer.
		negName := group.Name
		if negName == "" {
			negName = sp.BackendName(l.namer)
		}
		neg, err := l.negGetter.GetNetworkEndpointGroup(negName, group.Zone)
		if err != nil {
			return err
		}
		negs = append(negs, neg)
	}

	beName := sp.BackendName(l.namer)

	version := befeatures.VersionFromServicePort(&sp)
	scope := befeatures.ScopeFromServicePort(&sp)

	key, err := composite.CreateKey(l.cloud, beName, scope)
	if err != nil {
		return err
	}
	backendService, err := composite.GetBackendService(l.cloud, key, version)
	if err != nil {
		return err
	}

	targetBackends := getBackendsForNEGs(negs)
	oldBackends := sets.NewString()
	newBackends := sets.NewString()

	// WARNING: the backend link includes api version.
	// API versions has to match, otherwise backend link will be always different.
	for _, be := range backendService.Backends {
		oldBackends.Insert(be.Group)
	}
	for _, be := range targetBackends {
		newBackends.Insert(be.Group)
	}

	if !oldBackends.Equal(newBackends) {
		backendService.Backends = targetBackends
		return composite.UpdateBackendService(l.cloud, key, backendService)
	}
	return nil
}

func getBackendsForNEGs(negs []*compute.NetworkEndpointGroup) []*composite.Backend {
	var backends []*composite.Backend
	for _, neg := range negs {
		b := &composite.Backend{
			Group:              neg.SelfLink,
			BalancingMode:      string(Rate),
			MaxRatePerEndpoint: maxRPS,
		}
		backends = append(backends, b)
	}
	return backends
}
