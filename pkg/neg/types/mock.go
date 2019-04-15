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

	"fmt"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/googleapi"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
	"net/http"
)

type NetworkEndpointEntry struct {
	NetworkEndpoint *compute.NetworkEndpoint
	Healths         []*compute.HealthStatusForNetworkEndpoint
}

type NetworkEndpointStore map[meta.Key][]NetworkEndpointEntry

func MockNetworkEndpointAPIs(fakeGCE *gce.Cloud) {
	m := (fakeGCE.Compute().(*cloud.MockGCE))
	m.MockBetaNetworkEndpointGroups.X = NetworkEndpointStore{}
	m.MockBetaNetworkEndpointGroups.AttachNetworkEndpointsHook = MockAttachNetworkEndpointsHook
	m.MockBetaNetworkEndpointGroups.DetachNetworkEndpointsHook = MockDetachNetworkEndpointsHook
	m.MockBetaNetworkEndpointGroups.ListNetworkEndpointsHook = MockListNetworkEndpointsHook
}

func MockListNetworkEndpointsHook(ctx context.Context, key *meta.Key, obj *compute.NetworkEndpointGroupsListEndpointsRequest, filter *filter.F, m *cloud.MockBetaNetworkEndpointGroups) ([]*compute.NetworkEndpointWithHealthStatus, error) {
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

func MockAttachNetworkEndpointsHook(ctx context.Context, key *meta.Key, obj *compute.NetworkEndpointGroupsAttachEndpointsRequest, m *cloud.MockBetaNetworkEndpointGroups) error {
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
		for _, oldEp := range m.X.(NetworkEndpointStore)[*key] {
			if isNetworkEndpointsEqual(oldEp.NetworkEndpoint, newEp) {
				found = true
				break
			}
		}
		if !found {
			newList = append(newList, generateNetworkEndpointEntry(newEp))
		}
	}
	m.X.(NetworkEndpointStore)[*key] = newList
	return nil
}

func MockDetachNetworkEndpointsHook(ctx context.Context, key *meta.Key, obj *compute.NetworkEndpointGroupsDetachEndpointsRequest, m *cloud.MockBetaNetworkEndpointGroups) error {
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
		for _, right := range m.X.(NetworkEndpointStore)[*key] {
			if isNetworkEndpointsEqual(left, right.NetworkEndpoint) {
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

	newList := []*compute.NetworkEndpoint{}
	for _, ep := range m.X.(NetworkEndpointStore)[*key] {
		found := false
		for _, del := range obj.NetworkEndpoints {
			if isNetworkEndpointsEqual(ep.NetworkEndpoint, del) {
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

func isNetworkEndpointsEqual(left, right *compute.NetworkEndpoint) bool {
	if left.IpAddress == right.IpAddress && left.Port == right.Port && left.Instance == right.Instance {
		return true
	}
	return false
}

func generateNetworkEndpointEntry(networkEndpoint *compute.NetworkEndpoint) NetworkEndpointEntry {
	return NetworkEndpointEntry{
		NetworkEndpoint: networkEndpoint,
		Healths:         []*compute.HealthStatusForNetworkEndpoint{},
	}
}

func generateNetworkEndpointEntryList(networkEndpoints []*compute.NetworkEndpoint) []NetworkEndpointEntry {
	ret := []NetworkEndpointEntry{}
	for _, ne := range networkEndpoints {
		ret = append(ret, generateNetworkEndpointEntry(ne))
	}
	return ret
}

func generateNetworkEndpointWithHealthStatus(networkEndpointEntry NetworkEndpointEntry) *compute.NetworkEndpointWithHealthStatus {
	return &compute.NetworkEndpointWithHealthStatus{
		NetworkEndpoint: networkEndpointEntry.NetworkEndpoint,
		Healths:         networkEndpointEntry.Healths,
	}
}

func generateNetworkEndpointWithHealthStatusList(networkEndpointEntryList []NetworkEndpointEntry) []*compute.NetworkEndpointWithHealthStatus {
	ret := []*compute.NetworkEndpointWithHealthStatus{}
	for _, ne := range networkEndpointEntryList {
		ret = append(ret, generateNetworkEndpointWithHealthStatus(ne))
	}
	return ret
}
