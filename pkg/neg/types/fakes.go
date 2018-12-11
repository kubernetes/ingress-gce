/*
Copyright 2017 The Kubernetes Authors.

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

package types

import (
	"fmt"
	"reflect"
	"sync"

	computebeta "google.golang.org/api/compute/v0.beta"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud/meta"
)

const (
	TestZone1     = "zone1"
	TestZone2     = "zone2"
	TestInstance1 = "instance1"
	TestInstance2 = "instance2"
	TestInstance3 = "instance3"
	TestInstance4 = "instance4"
)

type fakeZoneGetter struct {
	zoneInstanceMap map[string]sets.String
}

func NewFakeZoneGetter() *fakeZoneGetter {
	return &fakeZoneGetter{
		zoneInstanceMap: map[string]sets.String{
			TestZone1: sets.NewString(TestInstance1, TestInstance2),
			TestZone2: sets.NewString(TestInstance3, TestInstance4),
		},
	}
}

func (f *fakeZoneGetter) ListZones() ([]string, error) {
	ret := []string{}
	for key := range f.zoneInstanceMap {
		ret = append(ret, key)
	}
	return ret, nil
}
func (f *fakeZoneGetter) GetZoneForNode(name string) (string, error) {
	for zone, instances := range f.zoneInstanceMap {
		if instances.Has(name) {
			return zone, nil
		}
	}
	return "", NotFoundError
}

type FakeNetworkEndpointGroupCloud struct {
	NetworkEndpointGroups map[string][]*computebeta.NetworkEndpointGroup
	NetworkEndpoints      map[string][]*computebeta.NetworkEndpoint
	Subnetwork            string
	Network               string
	mu                    sync.Mutex
}

func NewFakeNetworkEndpointGroupCloud(subnetwork, network string) NetworkEndpointGroupCloud {
	return &FakeNetworkEndpointGroupCloud{
		Subnetwork:            subnetwork,
		Network:               network,
		NetworkEndpointGroups: map[string][]*computebeta.NetworkEndpointGroup{},
		NetworkEndpoints:      map[string][]*computebeta.NetworkEndpoint{},
	}
}

var NotFoundError = fmt.Errorf("not Found")

func (f *FakeNetworkEndpointGroupCloud) GetNetworkEndpointGroup(name string, zone string) (*computebeta.NetworkEndpointGroup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	negs, ok := f.NetworkEndpointGroups[zone]
	if ok {
		for _, neg := range negs {
			if neg.Name == name {
				return neg, nil
			}
		}
	}
	return nil, NotFoundError
}

func networkEndpointKey(name, zone string) string {
	return fmt.Sprintf("%s-%s", zone, name)
}

func (f *FakeNetworkEndpointGroupCloud) ListNetworkEndpointGroup(zone string) ([]*computebeta.NetworkEndpointGroup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.NetworkEndpointGroups[zone], nil
}

func (f *FakeNetworkEndpointGroupCloud) AggregatedListNetworkEndpointGroup() (map[string][]*computebeta.NetworkEndpointGroup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.NetworkEndpointGroups, nil
}

func (f *FakeNetworkEndpointGroupCloud) CreateNetworkEndpointGroup(neg *computebeta.NetworkEndpointGroup, zone string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	neg.SelfLink = cloud.NewNetworkEndpointGroupsResourceID("mock-project", zone, neg.Name).SelfLink(meta.VersionAlpha)
	if _, ok := f.NetworkEndpointGroups[zone]; !ok {
		f.NetworkEndpointGroups[zone] = []*computebeta.NetworkEndpointGroup{}
	}
	f.NetworkEndpointGroups[zone] = append(f.NetworkEndpointGroups[zone], neg)
	f.NetworkEndpoints[networkEndpointKey(neg.Name, zone)] = []*computebeta.NetworkEndpoint{}
	return nil
}

func (f *FakeNetworkEndpointGroupCloud) DeleteNetworkEndpointGroup(name string, zone string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.NetworkEndpoints, networkEndpointKey(name, zone))
	negs := f.NetworkEndpointGroups[zone]
	newList := []*computebeta.NetworkEndpointGroup{}
	found := false
	for _, neg := range negs {
		if neg.Name == name {
			found = true
			continue
		}
		newList = append(newList, neg)
	}
	if !found {
		return NotFoundError
	}
	f.NetworkEndpointGroups[zone] = newList
	return nil
}

func (f *FakeNetworkEndpointGroupCloud) AttachNetworkEndpoints(name, zone string, endpoints []*computebeta.NetworkEndpoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.NetworkEndpoints[networkEndpointKey(name, zone)] = append(f.NetworkEndpoints[networkEndpointKey(name, zone)], endpoints...)
	return nil
}

func (f *FakeNetworkEndpointGroupCloud) DetachNetworkEndpoints(name, zone string, endpoints []*computebeta.NetworkEndpoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	newList := []*computebeta.NetworkEndpoint{}
	for _, ne := range f.NetworkEndpoints[networkEndpointKey(name, zone)] {
		found := false
		for _, remove := range endpoints {
			if reflect.DeepEqual(*ne, *remove) {
				found = true
				break
			}
		}
		if found {
			continue
		}
		newList = append(newList, ne)
	}
	f.NetworkEndpoints[networkEndpointKey(name, zone)] = newList
	return nil
}

func (f *FakeNetworkEndpointGroupCloud) ListNetworkEndpoints(name, zone string, showHealthStatus bool) ([]*computebeta.NetworkEndpointWithHealthStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ret := []*computebeta.NetworkEndpointWithHealthStatus{}
	nes, ok := f.NetworkEndpoints[networkEndpointKey(name, zone)]
	if !ok {
		return nil, NotFoundError
	}
	for _, ne := range nes {
		ret = append(ret, &computebeta.NetworkEndpointWithHealthStatus{NetworkEndpoint: ne})
	}
	return ret, nil
}

func (f *FakeNetworkEndpointGroupCloud) NetworkURL() string {
	return f.Network
}

func (f *FakeNetworkEndpointGroupCloud) SubnetworkURL() string {
	return f.Subnetwork
}
