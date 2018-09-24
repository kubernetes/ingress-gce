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
	computebeta "google.golang.org/api/compute/v0.beta"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/utils"
)

// negLinker handles linking backends to NEG's.
type negLinker struct {
	backendPool Pool
	negGetter   NEGGetter
	namer       *utils.Namer
}

// negLinker is a Linker
var _ Linker = (*negLinker)(nil)

func NewNEGLinker(
	backendPool Pool,
	negGetter NEGGetter,
	namer *utils.Namer) Linker {
	return &negLinker{
		backendPool: backendPool,
		negGetter:   negGetter,
		namer:       namer,
	}
}

// Link implements Link.
func (l *negLinker) Link(sp utils.ServicePort, groups []GroupKey) error {
	var negs []*computebeta.NetworkEndpointGroup
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

	cloud := l.backendPool.(*Backends).cloud
	beName := sp.BackendName(l.namer)
	backendService, err := cloud.GetBetaGlobalBackendService(beName)
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
		return cloud.UpdateBetaGlobalBackendService(backendService)
	}
	return nil
}

func getBackendsForNEGs(negs []*computebeta.NetworkEndpointGroup) []*computebeta.Backend {
	var backends []*computebeta.Backend
	for _, neg := range negs {
		b := &computebeta.Backend{
			Group:              neg.SelfLink,
			BalancingMode:      string(Rate),
			MaxRatePerEndpoint: maxRPS,
		}
		backends = append(backends, b)
	}
	return backends
}
