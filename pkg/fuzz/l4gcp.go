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

package fuzz

import (
	"context"
	"fmt"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	apiv1 "google.golang.org/api/compute/v1"
	"k8s.io/klog"
)

// L4NetLB contains the resources for a L4 external load balancer
type L4NetLB struct {
	VIP string

	ForwardingRule *ForwardingRule
	BackendService *BackendService
	InstanceGroup  map[meta.Key]*InstanceGroup
	HealthCheck    *HealthCheck
}

// L4ILB contains the resources for a L4 internal load balancer
type L4ILB struct {
	VIP string

	ForwardingRule *ForwardingRule
	BackendService *BackendService
	HealthCheck    *HealthCheck
}

// NewL4NetLB returns an empty L4NetLB.
func NewL4NetLB(vip string) *L4NetLB {
	return &L4NetLB{
		VIP:           vip,
		InstanceGroup: map[meta.Key]*InstanceGroup{},
	}
}

// L4LBForSvcParams contains all L4 LB resources names.
type L4LBForSvcParams struct {
	VIP                   string
	Region                string
	Network               string
	SvcName               string
	BsName                string
	FwrName               string
	HcName                string
	HcFwRuleName          string
	FwRuleName            string
	ExternalTrafficPolicy string
}

// GetRegionalL4NetLBForService retrieves all of the resources associated with the L4 NetLB for a given VIP.
func GetRegionalL4NetLBForService(ctx context.Context, c cloud.Cloud, params *L4LBForSvcParams) (*L4NetLB, error) {
	if params.FwrName == "" {
		return nil, fmt.Errorf("Forwarding rule name is empty")
	}
	l4netlb := NewL4NetLB(params.VIP)
	frKey := meta.RegionalKey(params.FwrName, params.Region)
	fr, err := c.ForwardingRules().Get(ctx, frKey)
	if err != nil || fr == nil {
		klog.Warningf("Error listing forwarding rules: %v", err)
		return l4netlb, err
	}
	l4netlb.ForwardingRule = &ForwardingRule{GA: fr}
	if params.BsName == "" {
		return nil, fmt.Errorf("Backend service name is empty")
	}
	bsKey := meta.RegionalKey(params.BsName, params.Region)
	bs, err := c.RegionBackendServices().Get(ctx, bsKey)
	if err != nil || bs == nil {
		klog.Warningf("Error listing backend service: %v", err)
		return l4netlb, err
	}
	l4netlb.BackendService = &BackendService{GA: bs}
	if params.HcName == "" {
		return nil, fmt.Errorf("Health check name is empty")
	}
	hcKey := meta.RegionalKey(params.HcName, params.Region)
	hc, err := c.RegionHealthChecks().Get(ctx, hcKey)
	if err != nil {
		klog.Warningf("Error listing health check: %v", err)
		return l4netlb, err
	}
	l4netlb.HealthCheck = &HealthCheck{
		GA: hc,
	}
	var igKeys []*meta.Key
	for _, be := range bs.Backends {
		if strings.Contains(be.Group, IgResourceType) {
			resourceId, err := cloud.ParseResourceURL(be.Group)
			if err != nil {
				return nil, err
			}
			klog.Infof("Group ID %v, ig key %v", be.Group, resourceId.Key)
			igKeys = append(igKeys, resourceId.Key)
		}
	}

	for _, igKey := range igKeys {
		ig, err := c.InstanceGroups().Get(ctx, igKey)
		if err != nil {
			return l4netlb, err
		}
		l4netlb.InstanceGroup[*igKey] = &InstanceGroup{GA: ig}
	}
	if len(bs.Backends) == 0 {
		return l4netlb, fmt.Errorf("No backends in Backend Service")
	}
	rgr := &apiv1.ResourceGroupReference{Group: bs.Backends[0].Group}
	if bsHc, err := c.RegionBackendServices().GetHealth(ctx, bsKey, rgr); err != nil || bsHc == nil {
		klog.Warningf("Error checking backend service health: %v", err)
		return l4netlb, err
	}
	return l4netlb, err
}

// GetL4ILBForService retrieves all of the resources associated with the L4 ILB for a given VIP.
func GetL4ILBForService(ctx context.Context, c cloud.Cloud, params *L4LBForSvcParams) (*L4ILB, error) {
	if params.FwrName == "" {
		return nil, fmt.Errorf("Forwarding rule name is empty")
	}
	frKey := meta.RegionalKey(params.FwrName, params.Region)
	fr, err := c.ForwardingRules().Get(ctx, frKey)
	if err != nil || fr == nil {
		klog.Warningf("Error listing forwarding rules: %v", err)
		return nil, err
	}
	var l4ilb L4ILB
	l4ilb.ForwardingRule = &ForwardingRule{GA: fr}
	if params.BsName == "" {
		return nil, fmt.Errorf("Backend service name is empty")
	}
	bsKey := meta.RegionalKey(params.BsName, params.Region)
	bs, err := c.RegionBackendServices().Get(ctx, bsKey)
	if err != nil || bs == nil {
		klog.Warningf("Error listing backend service: %v", err)
		return &l4ilb, err
	}
	l4ilb.BackendService = &BackendService{GA: bs}
	if params.HcName == "" {
		return nil, fmt.Errorf("Health check name is empty")
	}
	hcKey := meta.GlobalKey(params.HcName)
	hc, err := c.HealthChecks().Get(ctx, hcKey)
	if err != nil {
		klog.Warningf("Error listing health check: %v", err)
		return &l4ilb, err
	}
	l4ilb.HealthCheck = &HealthCheck{
		GA: hc,
	}

	rgr := &apiv1.ResourceGroupReference{Group: bs.Backends[0].Group}
	if bsHc, err := c.RegionBackendServices().GetHealth(ctx, bsKey, rgr); err != nil || bsHc == nil {
		klog.Warningf("Error checking backend service health: %v", err)
		return &l4ilb, err
	}
	return &l4ilb, err
}

// CheckL4NetLBServiceDeletion verifies that all GCE resources were deleted
func CheckL4NetLBServiceDeletion(ctx context.Context, c cloud.Cloud, params *L4LBForSvcParams) error {
	frKey := meta.RegionalKey(params.FwrName, params.Region)
	fr, err := c.ForwardingRules().Get(ctx, frKey)
	if err == nil || fr != nil {
		klog.Warningf("Error forwarding rule should be deleted: %v", err)
		return err
	}

	bsKey := meta.RegionalKey(params.FwRuleName, params.Region)
	bs, err := c.RegionBackendServices().Get(ctx, bsKey)
	if err == nil || bs != nil {
		klog.Warningf("Error backend service should be deleted: %v", err)
		return err
	}
	hcKey := meta.RegionalKey(params.HcName, params.Region)
	hc, err := c.RegionHealthChecks().Get(ctx, hcKey)
	if err != nil || hc != nil {
		klog.Warningf("Error health check should be deleted: %v", err)
		return err
	}
	return nil
}
