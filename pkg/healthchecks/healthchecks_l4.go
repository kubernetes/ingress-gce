/*
Copyright 2020 The Kubernetes Authors.

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

package healthchecks

import (
	"fmt"

	cloudprovider "github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

const (

	// L4 Load Balancer parameters
	gceHcCheckIntervalSeconds = int64(8)
	gceHcTimeoutSeconds       = int64(1)
	// Start sending requests as soon as one healthcheck succeeds.
	gceHcHealthyThreshold = int64(1)
	// Defaults to 3 * 8 = 24 seconds before the LB will steer traffic away.
	gceHcUnhealthyThreshold = int64(3)
)

// EnsureL4HealthCheck creates a new HTTP health check for an L4 LoadBalancer service, based on the parameters provided.
// If the healthcheck already exists, it is updated as needed.
func EnsureL4HealthCheck(cloud *gce.Cloud, name string, svcName types.NamespacedName, shared bool, path string, port int32) (*composite.HealthCheck, string, error) {
	selfLink := ""
	key, err := composite.CreateKey(cloud, name, meta.Global)
	if err != nil {
		return nil, selfLink, fmt.Errorf("Failed to create composite key for healthcheck %s - %w", name, err)
	}
	hc, err := composite.GetHealthCheck(cloud, key, meta.VersionGA)
	if err != nil {
		if !utils.IsNotFoundError(err) {
			return nil, selfLink, err
		}
	}
	expectedHC := NewL4HealthCheck(name, svcName, shared, path, port)
	if hc == nil {
		// Create the healthcheck
		klog.V(2).Infof("Creating healthcheck %s for service %s, shared = %v", name, svcName, shared)
		err = composite.CreateHealthCheck(cloud, key, expectedHC)
		if err != nil {
			return nil, selfLink, err
		}
		selfLink = cloudprovider.SelfLink(meta.VersionGA, cloud.ProjectID(), "healthChecks", key)
		return expectedHC, selfLink, nil
	}
	selfLink = hc.SelfLink
	if !needToUpdateHealthChecks(hc, expectedHC) {
		// nothing to do
		return hc, selfLink, nil
	}
	mergeHealthChecks(hc, expectedHC)
	klog.V(2).Infof("Updating healthcheck %s for service %s", name, svcName)
	err = composite.UpdateHealthCheck(cloud, key, expectedHC)
	if err != nil {
		return nil, selfLink, err
	}
	return expectedHC, selfLink, err
}

func DeleteHealthCheck(cloud *gce.Cloud, name string) error {
	key, err := composite.CreateKey(cloud, name, meta.Global)
	if err != nil {
		return fmt.Errorf("Failed to create composite key for healthcheck %s - %w", name, err)
	}
	return composite.DeleteHealthCheck(cloud, key, meta.VersionGA)
}

func NewL4HealthCheck(name string, svcName types.NamespacedName, shared bool, path string, port int32) *composite.HealthCheck {
	httpSettings := composite.HTTPHealthCheck{
		Port:        int64(port),
		RequestPath: path,
	}

	desc, err := utils.MakeL4ILBServiceDescription(svcName.String(), "", meta.VersionGA, shared)
	if err != nil {
		klog.Warningf("Failed to generate description for L4HealthCheck %s, err %v", name, err)
	}
	return &composite.HealthCheck{
		Name:               name,
		CheckIntervalSec:   gceHcCheckIntervalSeconds,
		TimeoutSec:         gceHcTimeoutSeconds,
		HealthyThreshold:   gceHcHealthyThreshold,
		UnhealthyThreshold: gceHcUnhealthyThreshold,
		HttpHealthCheck:    &httpSettings,
		Type:               "HTTP",
		Description:        desc,
	}
}

// mergeHealthChecks reconciles HealthCheck config to be no smaller than
// the default values. newHC is assumed to have defaults,
// since it is created by the NewL4HealthCheck call.
// E.g. old health check interval is 2s, new has the default of 8.
// The HC interval will be reconciled to 8 seconds.
// If the existing health check values are larger than the default interval,
// the existing configuration will be kept.
func mergeHealthChecks(hc, newHC *composite.HealthCheck) {
	if hc.CheckIntervalSec > newHC.CheckIntervalSec {
		newHC.CheckIntervalSec = hc.CheckIntervalSec
	}
	if hc.TimeoutSec > newHC.TimeoutSec {
		newHC.TimeoutSec = hc.TimeoutSec
	}
	if hc.UnhealthyThreshold > newHC.UnhealthyThreshold {
		newHC.UnhealthyThreshold = hc.UnhealthyThreshold
	}
	if hc.HealthyThreshold > newHC.HealthyThreshold {
		newHC.HealthyThreshold = hc.HealthyThreshold
	}
}

// needToUpdateHealthChecks checks whether the healthcheck needs to be updated.
func needToUpdateHealthChecks(hc, newHC *composite.HealthCheck) bool {
	return hc.HttpHealthCheck == nil ||
		newHC.HttpHealthCheck == nil ||
		hc.HttpHealthCheck.Port != newHC.HttpHealthCheck.Port ||
		hc.HttpHealthCheck.RequestPath != newHC.HttpHealthCheck.RequestPath ||
		hc.Description != newHC.Description ||
		hc.CheckIntervalSec < newHC.CheckIntervalSec ||
		hc.TimeoutSec < newHC.TimeoutSec ||
		hc.UnhealthyThreshold < newHC.UnhealthyThreshold ||
		hc.HealthyThreshold < newHC.HealthyThreshold
}
