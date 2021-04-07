/*
Copyright 2019 The Kubernetes Authors.

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
	"context"

	"k8s.io/ingress-gce/pkg/composite"

	"fmt"
	"net/http"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"k8s.io/legacy-cloud-providers/gce"
)

type NetworkEndpointEntry struct {
	NetworkEndpoint *composite.NetworkEndpoint
	Healths         []*composite.HealthStatusForNetworkEndpoint
}

type NetworkEndpointStore map[meta.Key][]NetworkEndpointEntry

func (s NetworkEndpointStore) AddNetworkEndpointHealthStatus(key meta.Key, entries []NetworkEndpointEntry) {
	s[key] = entries
}

// GetNetworkEndpointStore is a helper function to access the NetworkEndpointStore of the mock NEG cloud
func GetNetworkEndpointStore(negCloud NetworkEndpointGroupCloud) NetworkEndpointStore {
	adapter := negCloud.(*cloudProviderAdapter)
	mockedCloud := adapter.c.Compute().(*cloud.MockGCE)
	ret := mockedCloud.MockNetworkEndpointGroups.X.(NetworkEndpointStore)
	return ret
}

func MockNetworkEndpointAPIs(fakeGCE *gce.Cloud) {
	m := (fakeGCE.Compute().(*cloud.MockGCE))
	m.MockNetworkEndpointGroups.X = NetworkEndpointStore{}
	m.MockNetworkEndpointGroups.AttachNetworkEndpointsHook = MockAttachNetworkEndpointsHook
	m.MockNetworkEndpointGroups.DetachNetworkEndpointsHook = MockDetachNetworkEndpointsHook
	m.MockNetworkEndpointGroups.ListNetworkEndpointsHook = MockListNetworkEndpointsHook
	m.MockNetworkEndpointGroups.AggregatedListHook = MockAggregatedListNetworkEndpointGroupHook
}

// TODO: move this logic into code gen
// TODO: make AggregateList return map[meta.Key]Object
func MockAggregatedListNetworkEndpointGroupHook(ctx context.Context, fl *filter.F, m *cloud.MockNetworkEndpointGroups) (bool, map[string][]*compute.NetworkEndpointGroup, error) {
	objs := map[string][]*compute.NetworkEndpointGroup{}
	for _, obj := range m.Objects {
		res, err := cloud.ParseResourceURL(obj.ToGA().SelfLink)
		if err != nil {
			return false, nil, err
		}
		if !fl.Match(obj.ToGA()) {
			continue
		}
		var location string
		switch res.Key.Type() {
		case meta.Regional:
			location = fmt.Sprintf("regions/%s", res.Key.Region)
			break
		case meta.Zonal:
			location = fmt.Sprintf("zones/%s", res.Key.Zone)
			break
		case meta.Global:
			location = string(meta.Global)
		}
		objs[location] = append(objs[location], obj.ToGA())
	}
	// Always return global
	if _, ok := objs[meta.Global]; !ok {
		objs[meta.Global] = []*compute.NetworkEndpointGroup{}
	}
	return true, objs, nil
}

func MockListNetworkEndpointsHook(ctx context.Context, key *meta.Key, obj *compute.NetworkEndpointGroupsListEndpointsRequest, filter *filter.F, m *cloud.MockNetworkEndpointGroups) ([]*compute.NetworkEndpointWithHealthStatus, error) {
	_, err := m.Get(ctx, key)
	if err != nil {
		return nil, &googleapi.Error{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("Key: %s was not found in NetworkEndpointGroup", key.String()),
		}
	}

	m.Lock.Lock()
	defer m.Lock.Unlock()
	if _, ok := m.X.(NetworkEndpointStore)[*key]; !ok {
		m.X.(NetworkEndpointStore)[*key] = []NetworkEndpointEntry{}
	}
	return generateNetworkEndpointWithHealthStatusList(m.X.(NetworkEndpointStore)[*key]), nil
}

func MockAttachNetworkEndpointsHook(ctx context.Context, key *meta.Key, obj *compute.NetworkEndpointGroupsAttachEndpointsRequest, m *cloud.MockNetworkEndpointGroups) error {
	_, err := m.Get(ctx, key)
	if err != nil {
		return &googleapi.Error{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("Key: %s was not found in NetworkEndpointGroup", key.String()),
		}
	}

	m.Lock.Lock()
	defer m.Lock.Unlock()

	if _, ok := m.X.(NetworkEndpointStore)[*key]; !ok {
		m.X.(NetworkEndpointStore)[*key] = []NetworkEndpointEntry{}
	}

	newList := m.X.(NetworkEndpointStore)[*key]
	for _, newEp := range obj.NetworkEndpoints {
		found := false
		newComposite, err := composite.GAToNetworkEndpoint(newEp)
		if err != nil {
			return err
		}
		for _, oldEp := range m.X.(NetworkEndpointStore)[*key] {
			if isNetworkEndpointsEqual(oldEp.NetworkEndpoint, newComposite) {
				found = true
				break
			}
		}
		if !found {
			newList = append(newList, generateNetworkEndpointEntry(newComposite))
		}
	}
	m.X.(NetworkEndpointStore)[*key] = newList
	return nil
}

func MockDetachNetworkEndpointsHook(ctx context.Context, key *meta.Key, obj *compute.NetworkEndpointGroupsDetachEndpointsRequest, m *cloud.MockNetworkEndpointGroups) error {
	_, err := m.Get(ctx, key)
	if err != nil {
		return &googleapi.Error{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("Key: %s was not found in NetworkEndpointGroup", key.String()),
		}
	}

	m.Lock.Lock()
	defer m.Lock.Unlock()

	if _, ok := m.X.(NetworkEndpointStore)[*key]; !ok {
		m.X.(NetworkEndpointStore)[*key] = []NetworkEndpointEntry{}
	}

	for _, left := range obj.NetworkEndpoints {
		found := false
		leftComposite, err := composite.GAToNetworkEndpoint(left)
		if err != nil {
			return err
		}
		for _, right := range m.X.(NetworkEndpointStore)[*key] {
			if isNetworkEndpointsEqual(leftComposite, right.NetworkEndpoint) {
				found = true
				break
			}
		}

		if !found {
			return &googleapi.Error{
				Code:    http.StatusNotFound,
				Message: fmt.Sprintf("Endpoint %v was not found in NetworkEndpointGroup %q", left, key.String()),
			}
		}
	}

	newList := []*composite.NetworkEndpoint{}
	for _, ep := range m.X.(NetworkEndpointStore)[*key] {
		found := false
		for _, del := range obj.NetworkEndpoints {
			delComposite, err := composite.GAToNetworkEndpoint(del)
			if err != nil {
				return err
			}
			if isNetworkEndpointsEqual(ep.NetworkEndpoint, delComposite) {
				found = true
				break
			}
		}

		if !found {
			newList = append(newList, ep.NetworkEndpoint)
		}
	}

	m.X.(NetworkEndpointStore)[*key] = generateNetworkEndpointEntryList(newList)
	return nil
}

func isNetworkEndpointsEqual(left, right *composite.NetworkEndpoint) bool {
	if left.IpAddress == right.IpAddress && left.Port == right.Port && left.Instance == right.Instance {
		return true
	}
	return false
}

func generateNetworkEndpointEntry(networkEndpoint *composite.NetworkEndpoint) NetworkEndpointEntry {
	return NetworkEndpointEntry{
		NetworkEndpoint: networkEndpoint,
		Healths:         []*composite.HealthStatusForNetworkEndpoint{},
	}
}

func generateNetworkEndpointEntryList(networkEndpoints []*composite.NetworkEndpoint) []NetworkEndpointEntry {
	ret := []NetworkEndpointEntry{}
	for _, ne := range networkEndpoints {
		ret = append(ret, generateNetworkEndpointEntry(ne))
	}
	return ret
}

func generateNetworkEndpointWithHealthStatus(networkEndpointEntry NetworkEndpointEntry) *compute.NetworkEndpointWithHealthStatus {
	ret := &compute.NetworkEndpointWithHealthStatus{}
	ret.NetworkEndpoint, _ = networkEndpointEntry.NetworkEndpoint.ToGA()

	for _, health := range networkEndpointEntry.Healths {
		h, _ := health.ToGA()
		ret.Healths = append(ret.Healths, h)
	}
	return ret
}

func generateNetworkEndpointWithHealthStatusList(networkEndpointEntryList []NetworkEndpointEntry) []*compute.NetworkEndpointWithHealthStatus {
	ret := []*compute.NetworkEndpointWithHealthStatus{}
	for _, ne := range networkEndpointEntryList {
		ret = append(ret, generateNetworkEndpointWithHealthStatus(ne))
	}
	return ret
}
