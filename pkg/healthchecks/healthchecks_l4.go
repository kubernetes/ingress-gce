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
	cloudprovider "github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
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

// EnsureL4HealthCheck creates a new regional HTTP health check for an L4 LoadBalancer service, based on the parameters provided.
// If the healthcheck already exists, it is updated as needed.
// If ExternalTrafficPolicy is Cluster then health check is shared among all ILBs and XLBs.
// Regional healthcheck is supported by ILB and XLB.
// https://cloud.google.com/load-balancing/docs/health-check-concepts#lb_guide
func EnsureL4HealthCheck(cloud *gce.Cloud, svc *corev1.Service, namer namer.L4ResourcesNamer, sharedHC bool, l4Type utils.L4LBType) (string, string, int32, string, error) {
	hcName, hcFwName := namer.L4HealthCheck(svc.Namespace, svc.Name, sharedHC)
	hcPath, hcPort := gce.GetNodesHealthCheckPath(), gce.GetNodesHealthCheckPort()
	if !sharedHC {
		hcPath, hcPort = helpers.GetServiceHealthCheckPathPort(svc)
	}
	nn := types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}
	_, hcLink, err := ensureL4HealthCheckInternal(cloud, hcName, nn, sharedHC, hcPath, hcPort, l4Type)
	return hcLink, hcFwName, hcPort, hcName, err
}

func ensureL4HealthCheckInternal(cloud *gce.Cloud, name string, svcName types.NamespacedName, shared bool, path string, port int32, l4Type utils.L4LBType) (*composite.HealthCheck, string, error) {
	selfLink := ""
	key := meta.RegionalKey(name, cloud.Region())
	hc, err := composite.GetHealthCheck(cloud, key, meta.VersionGA)
	if err != nil {
		if !utils.IsNotFoundError(err) {
			return nil, selfLink, err
		}
	}
	expectedHC := NewL4HealthCheck(name, svcName, shared, path, port, cloud.Region())
	if hc == nil {
		// Create the healthcheck
		klog.V(2).Infof("Creating healthcheck %s for service %s of type %v, shared = %v", name, svcName, l4Type.ToString(), shared)
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
	klog.V(2).Infof("Updating healthcheck %s for service %s of type %v", name, svcName, l4Type.ToString())
	err = composite.UpdateHealthCheck(cloud, key, expectedHC)
	if err != nil {
		return nil, selfLink, err
	}
	return expectedHC, selfLink, err
}

func DeleteHealthCheck(cloud *gce.Cloud, name string) error {
	key := meta.RegionalKey(name, cloud.Region())
	return composite.DeleteHealthCheck(cloud, key, meta.VersionGA)
}

func NewL4HealthCheck(name string, svcName types.NamespacedName, shared bool, path string, port int32, region string) *composite.HealthCheck {
	httpSettings := composite.HTTPHealthCheck{
		Port:        int64(port),
		RequestPath: path,
	}

	desc, err := utils.MakeL4LBServiceDescription(svcName.String(), "", meta.VersionGA, shared)
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
		Scope:              meta.Regional,
		Region:             region,
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
