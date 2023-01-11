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

package test

import (
	"context"
	"fmt"
	"net/http"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
)

const (
	FwIPAddress = "10.0.0.1"
	// backend-service url, was created based on project and region set in test function DefaultTestClusterValues().
	// We need whole url for forwarding rule validation.
	bsUrl = "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/backendServices/k8s2-axyqjz2d-default-netbtest-hgray14h"
)

func ListErrorHook(ctx context.Context, zone string, fl *filter.F, m *cloud.MockInstanceGroups) (bool, []*compute.InstanceGroup, error) {
	return true, nil, fmt.Errorf("ListErrorHook")
}
func ListInstancesWithErrorHook(context.Context, *meta.Key, *compute.InstanceGroupsListInstancesRequest, *filter.F, *cloud.MockInstanceGroups) ([]*compute.InstanceWithNamedPorts, error) {
	return nil, fmt.Errorf("ListInstancesWithErrorHook")
}

func AddInstancesErrorHook(context.Context, *meta.Key, *compute.InstanceGroupsAddInstancesRequest, *cloud.MockInstanceGroups) error {
	return fmt.Errorf("AddInstancesErrorHook")
}

func GetErrorInstanceGroupHook(ctx context.Context, key *meta.Key, m *cloud.MockInstanceGroups) (bool, *compute.InstanceGroup, error) {
	return true, nil, fmt.Errorf("GetErrorInstanceGroupHook")
}

func InsertErrorHook(ctx context.Context, key *meta.Key, obj *compute.InstanceGroup, m *cloud.MockInstanceGroups) (bool, error) {
	return true, fmt.Errorf("InsertErrorHook")
}

func SetNamedPortsErrorHook(context.Context, *meta.Key, *compute.InstanceGroupsSetNamedPortsRequest, *cloud.MockInstanceGroups) error {
	return fmt.Errorf("SetNamedPortsErrorHook")
}

func InsertForwardingRuleHook(ctx context.Context, key *meta.Key, obj *compute.ForwardingRule, m *cloud.MockForwardingRules) (b bool, e error) {
	if obj.IPAddress == "" {
		obj.IPAddress = FwIPAddress
	}
	return false, nil
}

func InsertForwardingRuleErrorHook(err error) func(ctx context.Context, key *meta.Key, obj *compute.ForwardingRule, m *cloud.MockForwardingRules) (b bool, e error) {
	return func(ctx context.Context, key *meta.Key, obj *compute.ForwardingRule, m *cloud.MockForwardingRules) (b bool, e error) {
		return true, err
	}
}

func DeleteForwardingRulesErrorHook(ctx context.Context, key *meta.Key, m *cloud.MockForwardingRules) (bool, error) {
	return true, fmt.Errorf("DeleteForwardingRulesErrorHook")
}

func DeleteAddressErrorHook(ctx context.Context, key *meta.Key, m *cloud.MockAddresses) (bool, error) {
	return true, fmt.Errorf("DeleteAddressErrorHook")
}

func DeleteFirewallsErrorHook(ctx context.Context, key *meta.Key, m *cloud.MockFirewalls) (bool, error) {
	return true, fmt.Errorf("DeleteFirewallsErrorHook")
}

func DeleteBackendServicesErrorHook(ctx context.Context, key *meta.Key, m *cloud.MockRegionBackendServices) (bool, error) {
	return true, fmt.Errorf("DeleteBackendServicesErrorHook")
}

func UpdateRegionBackendServiceWithErrorHookUpdate(context.Context, *meta.Key, *compute.BackendService, *cloud.MockRegionBackendServices) error {
	return fmt.Errorf("Undefined error")
}

func DeleteHealthCheckErrorHook(ctx context.Context, key *meta.Key, m *cloud.MockRegionHealthChecks) (bool, error) {
	return true, fmt.Errorf("DeleteHealthCheckErrorHook")
}

func DeleteRegionalHealthCheckResourceInUseErrorHook(ctx context.Context, key *meta.Key, m *cloud.MockRegionHealthChecks) (bool, error) {
	return true, &googleapi.Error{Code: http.StatusBadRequest, Message: "Cannot delete health check resource being used by another service"}
}
func DeleteHealthCheckResourceInUseErrorHook(ctx context.Context, key *meta.Key, m *cloud.MockHealthChecks) (bool, error) {
	return true, &googleapi.Error{Code: http.StatusBadRequest, Message: "Cannot delete health check resource being used by another service"}
}

func GetLegacyForwardingRule(ctx context.Context, key *meta.Key, m *cloud.MockForwardingRules) (bool, *compute.ForwardingRule, error) {
	fwRule := compute.ForwardingRule{Target: "some_target", LoadBalancingScheme: string(cloud.SchemeExternal), NetworkTier: cloud.NetworkTierDefault.ToGCEValue()}
	return true, &fwRule, nil
}

func GetRBSForwardingRule(ctx context.Context, key *meta.Key, m *cloud.MockForwardingRules) (bool, *compute.ForwardingRule, error) {
	fwRule := compute.ForwardingRule{BackendService: bsUrl, LoadBalancingScheme: string(cloud.SchemeExternal), NetworkTier: cloud.NetworkTierDefault.ToGCEValue()}
	return true, &fwRule, nil
}

func GetRBSForwardingRuleInStandardTier(ctx context.Context, key *meta.Key, m *cloud.MockForwardingRules) (bool, *compute.ForwardingRule, error) {
	fwRule := compute.ForwardingRule{BackendService: bsUrl, LoadBalancingScheme: string(cloud.SchemeExternal), NetworkTier: cloud.NetworkTierStandard.ToGCEValue()}
	return true, &fwRule, nil
}

func InsertAddressErrorHook(ctx context.Context, key *meta.Key, obj *compute.Address, m *cloud.MockAddresses) (bool, error) {
	return true, &googleapi.Error{Code: http.StatusBadRequest, Message: "Cannot reserve region address, the resource already exist"}
}

func InsertAddressNetworkErrorHook(ctx context.Context, key *meta.Key, obj *compute.Address, m *cloud.MockAddresses) (bool, error) {
	return true, &googleapi.Error{Code: http.StatusBadRequest, Message: "The network tier of external IP is STANDARD, that of Address must be the same."}
}

func InsertAddressNotAllocatedToProjectErrorHook(ctx context.Context, key *meta.Key, obj *compute.Address, m *cloud.MockAddresses) (bool, error) {
	return true, &googleapi.Error{Code: http.StatusBadRequest, Message: "Specified IP address is not allocated to the project or does not belong to the specified scope."}
}
