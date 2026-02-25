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

package l4lb

import (
	context2 "context"
	"fmt"
	"net/http"
	"testing"
	"time"

	computebeta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"k8s.io/ingress-gce/pkg/l4/metrics"
	"k8s.io/ingress-gce/pkg/l4annotations"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/retry"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider-gcp/providers/gce"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/flags"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
)

const (
	clusterUID = "aaaaa"
	// This is one of the zones used in gce_fake.go
	testGCEZone = "us-central1-b"
)

var (
	ilbCommonAnnotationKeys = []string{
		l4annotations.BackendServiceKey,
		l4annotations.HealthcheckKey,
	}
	ilbIPv4AnnotationKeys = []string{
		l4annotations.FirewallRuleKey,
		l4annotations.TCPForwardingRuleKey,
		l4annotations.FirewallRuleForHealthcheckKey,
	}
	ilbIPv6AnnotationKeys = []string{
		l4annotations.FirewallRuleIPv6Key,
		l4annotations.TCPForwardingRuleIPv6Key,
		l4annotations.FirewallRuleForHealthcheckIPv6Key,
	}
)

// TestProcessCreateOrUpdate verifies the processing loop in L4Controller.
// This test adds a new service, then performs a valid update and then modifies the service type to External and ensures
// that the status field is as expected in each case.
func TestProcessCreateOrUpdate(t *testing.T) {
	testCases := []struct {
		desc                string
		readOnlyModeEnabled bool
	}{
		{
			desc:                "Create Update and Delete ILB",
			readOnlyModeEnabled: false,
		},
		{
			desc:                "[ReadOnly] Create Update and Delete ILB",
			readOnlyModeEnabled: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			l4c, _ := newServiceController(t, newFakeGCE(), tc.readOnlyModeEnabled)
			prevMetrics, err := test.GetL4ILBLatencyMetric()
			if err != nil {
				t.Errorf("Error getting L4 ILB latency metrics err: %v", err)
			}
			newSvc := test.NewL4ILBService(false, 8080)
			addILBService(l4c, newSvc)
			addNEGAndSvcNegL4Controller(l4c, newSvc)

			err = l4c.sync(getKeyForSvc(newSvc, t), klog.TODO())
			if err != nil {
				t.Errorf("Failed to sync newly added service %s, err %v", newSvc.Name, err)
			}
			// List the service and ensure that it contains the finalizer as well as Status field.
			newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
			}
			if tc.readOnlyModeEnabled {
				verifyILBServiceNotProvisioned(t, newSvc)
			} else {
				verifyILBServiceProvisioned(t, newSvc)
			}
			currMetrics, metricErr := test.GetL4ILBLatencyMetric()
			if metricErr != nil {
				t.Errorf("Error getting L4 ILB latency metrics err: %v", metricErr)
			}
			if tc.readOnlyModeEnabled {
				prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 0, UpperBoundSeconds: 1}, t)
			} else {
				prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 1, UpperBoundSeconds: 1}, t)
			}

			// set the TrafficPolicy of the service to Local
			newSvc.Spec.ExternalTrafficPolicy = api_v1.ServiceExternalTrafficPolicyTypeLocal
			updateILBService(l4c, newSvc)
			err = l4c.sync(getKeyForSvc(newSvc, t), klog.TODO())
			if err != nil {
				t.Errorf("Failed to sync updated service %s, err %v", newSvc.Name, err)
			}
			// List the service and ensure that it contains the finalizer as well as Status field.
			newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
			}
			if tc.readOnlyModeEnabled {
				verifyILBServiceNotProvisioned(t, newSvc)
			} else {
				verifyILBServiceProvisioned(t, newSvc)
			}
			currMetrics, metricErr = test.GetL4ILBLatencyMetric()
			if metricErr != nil {
				t.Errorf("Error getting L4 ILB latency metrics err: %v", metricErr)
			}
			if tc.readOnlyModeEnabled {
				prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 0, UpdateCount: 0, UpperBoundSeconds: 1}, t)

			} else {
				prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 1, UpdateCount: 1, UpperBoundSeconds: 1}, t)

			}
			// Remove the Internal LoadBalancer annotation, this should trigger a cleanup.
			delete(newSvc.Annotations, gce.ServiceAnnotationLoadBalancerType)
			updateILBService(l4c, newSvc)
			err = l4c.sync(getKeyForSvc(newSvc, t), klog.TODO())
			if err != nil {
				t.Errorf("Failed to sync updated service %s, err %v", newSvc.Name, err)
			}

			// List the service and ensure that it doesn't contain the finalizer as well as Status field.
			newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
			}
			verifyILBServiceNotProvisioned(t, newSvc)
			currMetrics, metricErr = test.GetL4ILBLatencyMetric()
			if metricErr != nil {
				t.Errorf("Error getting L4 ILB latency metrics err: %v", metricErr)
			}

			if tc.readOnlyModeEnabled {
				prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 0, UpdateCount: 0, DeleteCount: 0, UpperBoundSeconds: 1}, t)
			} else {
				prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 1, UpdateCount: 1, DeleteCount: 1, UpperBoundSeconds: 1}, t)
			}

			newSvc.DeletionTimestamp = &v1.Time{}
			updateILBService(l4c, newSvc)
			key, _ := common.KeyFunc(newSvc)
			if err = l4c.sync(key, klog.TODO()); err != nil {
				t.Errorf("Failed to sync deleted service %s, err %v", key, err)
			}
			for _, isShared := range []bool{true, false} {
				hcName := l4c.namer.L4HealthCheck(newSvc.Namespace, newSvc.Name, isShared)
				if !isHealthCheckDeleted(l4c.ctx.Cloud, hcName, klog.TODO()) {
					t.Errorf("Health check %s should be deleted", hcName)
				}
			}

		})
	}

}

// TestProcessUpdateExternalTrafficPolicy verifies the processing loop in L4Controller.
// In this test we check that when ExternalTrafficPolicy is updated new health check will be created.
// If health check is not shared among services then there is a leak.
// When service is deleted all health checks should be cleaned up to prevent the leak.
func TestProcessUpdateExternalTrafficPolicy(t *testing.T) {
	l4c, _ := newServiceController(t, newFakeGCE(), false)
	// Create svc with ExternalTrafficPolicy Local.
	svc := test.NewL4ILBService(true, 8080)
	addILBService(l4c, svc)
	addNEGAndSvcNegL4Controller(l4c, svc)
	err := l4c.sync(getKeyForSvc(svc, t), klog.TODO())
	if err != nil {
		t.Errorf("Failed to sync newly added service %s, err %v", svc.Name, err)
	}
	// List the service and ensure that it contains the finalizer as well as Status field.
	svc, err = l4c.client.CoreV1().Services(svc.Namespace).Get(context2.TODO(), svc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", svc.Name, err)
	}
	verifyILBServiceProvisioned(t, svc)

	// Set ExternalTrafficPolicy to Cluster.
	svc.Spec.ExternalTrafficPolicy = api_v1.ServiceExternalTrafficPolicyTypeCluster
	updateILBService(l4c, svc)
	err = l4c.sync(getKeyForSvc(svc, t), klog.TODO())
	if err != nil {
		t.Errorf("Failed to sync updated service %s, err %v", svc.Name, err)
	}
	// List the service and ensure that it contains the finalizer as well as Status field.
	svc, err = l4c.client.CoreV1().Services(svc.Namespace).Get(context2.TODO(), svc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", svc.Name, err)
	}
	verifyILBServiceProvisioned(t, svc)
	// Verify that both health checks were created.
	for _, isShared := range []bool{true, false} {
		hcName := l4c.namer.L4HealthCheck(svc.Namespace, svc.Name, isShared)
		if isHealthCheckDeleted(l4c.ctx.Cloud, hcName, klog.TODO()) {
			t.Errorf("Health check %s should be created", hcName)
		}
	}
	// Delete service.
	svc.DeletionTimestamp = &v1.Time{}
	updateILBService(l4c, svc)
	key, _ := common.KeyFunc(svc)
	if err = l4c.sync(key, klog.TODO()); err != nil {
		t.Errorf("Failed to sync deleted service %s, err %v", key, err)
	}
	// Verify that both health checks were deleted.
	for _, isShared := range []bool{true, false} {
		hcName := l4c.namer.L4HealthCheck(svc.Namespace, svc.Name, isShared)
		if !isHealthCheckDeleted(l4c.ctx.Cloud, hcName, klog.TODO()) {
			t.Errorf("Health check %s should be deleted", hcName)
		}
	}
}

func TestProcessDeletion(t *testing.T) {
	testCases := []struct {
		desc                string
		readOnlyModeEnabled bool
	}{
		{
			desc:                "Processe Deletion",
			readOnlyModeEnabled: false,
		},
		{
			desc:                "[ReadOnly] Processe Deletion",
			readOnlyModeEnabled: true,
		},
	}

	for _, tc := range testCases {
		l4c, _ := newServiceController(t, newFakeGCE(), tc.readOnlyModeEnabled)
		prevMetrics, err := test.GetL4ILBLatencyMetric()
		if err != nil {
			t.Errorf("Error getting L4 ILB latency metrics err: %v", err)
		}
		newSvc := test.NewL4ILBService(false, 8080)
		addILBService(l4c, newSvc)
		addNEGAndSvcNegL4Controller(l4c, newSvc)
		err = l4c.sync(getKeyForSvc(newSvc, t), klog.TODO())
		if err != nil {
			t.Errorf("Failed to sync newly added service %s, err %v", newSvc.Name, err)
		}
		// List the service and ensure that it contains the finalizer and the status field
		newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
		if err != nil {
			t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
		}
		if tc.readOnlyModeEnabled {
			verifyILBServiceNotProvisioned(t, newSvc)
		} else {
			verifyILBServiceProvisioned(t, newSvc)
		}
		currMetrics, metricErr := test.GetL4ILBLatencyMetric()
		if metricErr != nil {
			t.Errorf("Error getting L4 ILB latency metrics err: %v", metricErr)
		}

		if tc.readOnlyModeEnabled {
			prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 0, UpperBoundSeconds: 1}, t)
		} else {
			prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 1, UpperBoundSeconds: 1}, t)
		}

		// Mark the service for deletion by updating timestamp. Use svc instead of newSvc since that has the finalizer.
		newSvc.DeletionTimestamp = &v1.Time{}
		updateILBService(l4c, newSvc)
		if !tc.readOnlyModeEnabled && !l4c.needsDeletion(newSvc) {
			t.Errorf("Incorrectly marked service %v as not needing ILB deletion", newSvc)
		}
		if tc.readOnlyModeEnabled && l4c.needsDeletion(newSvc) {
			t.Errorf("Incorrectly marked service %v as needing ILB deletion", newSvc)
		}

		err = l4c.sync(getKeyForSvc(newSvc, t), klog.TODO())
		if err != nil {
			t.Errorf("Failed to sync updated service %s, err %v", newSvc.Name, err)
		}

		// List the service and ensure that it does not contain the finalizer or the status field
		newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
		if err != nil {
			t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
		}
		verifyILBServiceNotProvisioned(t, newSvc)
		currMetrics, metricErr = test.GetL4ILBLatencyMetric()
		if metricErr != nil {
			t.Errorf("Error getting L4 ILB latency metrics err: %v", metricErr)
		}
		if tc.readOnlyModeEnabled {
			prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 0, DeleteCount: 0, UpperBoundSeconds: 1}, t)
		} else {
			prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 1, DeleteCount: 1, UpperBoundSeconds: 1}, t)
		}
		deleteILBService(l4c, newSvc)
		newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
		if !errors.IsNotFound(err) {
			t.Errorf("Expected to get not found error, but got %v, service: %+v", err, newSvc)
		}
	}
}

func TestProcessCreateLegacyService(t *testing.T) {
	l4c, _ := newServiceController(t, newFakeGCE(), false)
	prevMetrics, err := test.GetL4ILBLatencyMetric()
	if err != nil {
		t.Errorf("Error getting L4 ILB latency metrics err: %v", err)
	}
	newSvc := test.NewL4ILBService(false, 8080)
	// Set the legacy finalizer
	newSvc.Finalizers = append(newSvc.Finalizers, common.LegacyILBFinalizer)
	addILBService(l4c, newSvc)
	err = l4c.sync(getKeyForSvc(newSvc, t), klog.TODO())
	if err != nil {
		t.Errorf("Failed to sync newly added service %s, err %v", newSvc.Name, err)
	}
	// List the service and ensure that the status field is not updated.
	svc, err := l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	verifyILBServiceNotProvisioned(t, svc)
	currMetrics, metricErr := test.GetL4ILBLatencyMetric()
	if metricErr != nil {
		t.Errorf("Error getting L4 ILB latency metrics err: %v", metricErr)
	}
	prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{}, t)
}

func TestProcessCreateServiceWithLegacyInternalForwardingRule(t *testing.T) {
	l4c, _ := newServiceController(t, newFakeGCE(), false)
	prevMetrics, err := test.GetL4ILBLatencyMetric()
	if err != nil {
		t.Errorf("Error getting L4 ILB latency metrics err: %v", err)
	}
	newSvc := test.NewL4ILBService(false, 8080)
	addILBService(l4c, newSvc)
	// Mimic addition of NEG. This will not actually happen, but this test verifies that sync is skipped
	// even if a NEG got added.
	addNEGAndSvcNegL4Controller(l4c, newSvc)
	// Create legacy forwarding rule to mimic service controller.
	// A service can have the v1 finalizer reset due to a buggy script/manual operation.
	// Subsetting controller should only process the service if it doesn't already have a forwarding rule.
	createLegacyForwardingRule(t, newSvc, l4c.ctx.Cloud, string(cloud.SchemeInternal))
	err = l4c.sync(getKeyForSvc(newSvc, t), klog.TODO())
	if err != nil {
		t.Errorf("Failed to sync newly added service %s, err %v", newSvc.Name, err)
	}
	// List the service and ensure that the status field is not updated.
	svc, err := l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	verifyILBServiceNotProvisioned(t, svc)
	currMetrics, metricErr := test.GetL4ILBLatencyMetric()
	if metricErr != nil {
		t.Errorf("Error getting L4 ILB latency metrics err: %v", metricErr)
	}
	prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{}, t)
}

func TestCreateServiceNoLegacyForwordingRule(t *testing.T) {
	fakeGCE := newFakeGCE()
	l4c, _ := newServiceController(t, fakeGCE, false)
	newSvc := test.NewL4ILBService(false, 8080)

	firstHookCall := true
	(fakeGCE.Compute().(*cloud.MockGCE)).MockForwardingRules.GetHook = func(ctx context2.Context, key *meta.Key, m *cloud.MockForwardingRules, options ...cloud.Option) (bool, *compute.ForwardingRule, error) {
		if firstHookCall {
			// change hook behaviour after first get, controller inserts and reads new rules as it runs
			firstHookCall = false
			return true, nil, &googleapi.Error{Code: http.StatusNotFound, Message: "No such fwd rule"}
		}
		return false, nil, nil
	}
	addILBService(l4c, newSvc)
	// Mimic addition of NEG. This will not actually happen, but this test verifies that sync is skipped
	// even if a NEG got added.
	addNEGAndSvcNegL4Controller(l4c, newSvc)

	// Call sync and expect service not provisioned as existing forwarding rule can not be verified
	err := l4c.sync(getKeyForSvc(newSvc, t), klog.TODO())
	if err != nil {
		t.Errorf("Failed to sync newly added service %s, err %v", newSvc.Name, err)
	}
	// List the service and ensure that the status field is not updated.
	svc, err := l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	verifyILBServiceProvisioned(t, svc)
}

func TestCreateServiceUnknownLegacyForwordingRule(t *testing.T) {
	fakeGCE := newFakeGCE()
	l4c, _ := newServiceController(t, fakeGCE, false)
	newSvc := test.NewL4ILBService(false, 8080)
	(fakeGCE.Compute().(*cloud.MockGCE)).MockForwardingRules.GetHook = func(ctx context2.Context, key *meta.Key, m *cloud.MockForwardingRules, options ...cloud.Option) (bool, *compute.ForwardingRule, error) {
		return true, nil, fmt.Errorf("NON-404 error from mock API")
	}
	addILBService(l4c, newSvc)
	// Mimic addition of NEG. This will not actually happen, but this test verifies that sync is skipped
	// even if a NEG got added.
	addNEGAndSvcNegL4Controller(l4c, newSvc)

	// Call sync and expect service not provisioned as existing forwarding rule can not be verified
	err := l4c.sync(getKeyForSvc(newSvc, t), klog.TODO())
	if err != nil {
		t.Errorf("Failed to sync newly added service %s, err %v", newSvc.Name, err)
	}
	// List the service and ensure that the status field is not updated.
	svc, err := l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	verifyILBServiceNotProvisioned(t, svc)
}

func TestProcessCreateServiceWithLegacyExternalForwardingRule(t *testing.T) {
	l4c, _ := newServiceController(t, newFakeGCE(), false)
	prevMetrics, err := test.GetL4ILBLatencyMetric()
	if err != nil {
		t.Errorf("Error getting L4 ILB latency metrics err: %v", err)
	}
	newSvc := test.NewL4ILBService(false, 8080)
	addILBService(l4c, newSvc)
	// Mimic addition of NEG. This will happen in parallel with ILB sync, by the NEG controller.
	addNEGAndSvcNegL4Controller(l4c, newSvc)
	// Create legacy external forwarding rule to mimic transition from external to internal LB.
	// Service processing should succeed in that case. The external forwarding rule will be deleted
	// by service controller.
	createLegacyForwardingRule(t, newSvc, l4c.ctx.Cloud, string(cloud.SchemeExternal))
	err = l4c.sync(getKeyForSvc(newSvc, t), klog.TODO())
	if err != nil {
		t.Errorf("Failed to sync newly added service %s, err %v", newSvc.Name, err)
	}
	// List the service and ensure that the status is updated.
	svc, err := l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	verifyILBServiceProvisioned(t, svc)
	currMetrics, metricErr := test.GetL4ILBLatencyMetric()
	if metricErr != nil {
		t.Errorf("Error getting L4 ILB latency metrics %v", metricErr)
	}
	prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 1, UpperBoundSeconds: 1}, t)
}

func TestProcessUpdateClusterIPToILBService(t *testing.T) {
	l4c, _ := newServiceController(t, newFakeGCE(), false)
	prevMetrics, err := test.GetL4ILBLatencyMetric()
	if err != nil {
		t.Errorf("Error getting L4 ILB latency metrics %v", err)
	}
	clusterSvc := &api_v1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name:      "testsvc",
			Namespace: "testns",
		},
	}
	addILBService(l4c, clusterSvc)
	if needsILB, _ := l4annotations.WantsL4ILB(clusterSvc); needsILB {
		t.Errorf("Incorrectly marked service %v as needing ILB", clusterSvc)
	}
	if l4c.needsDeletion(clusterSvc) {
		t.Errorf("Incorrectly marked service %v as needing ILB deletion", clusterSvc)
	}
	// Change to Internal LoadBalancer type
	newSvc := clusterSvc.DeepCopy()
	newSvc.Spec.Type = api_v1.ServiceTypeLoadBalancer
	newSvc.Annotations = make(map[string]string)
	newSvc.Annotations[gce.ServiceAnnotationLoadBalancerType] = string(gce.LBTypeInternal)
	updateILBService(l4c, newSvc)
	if !l4c.needsUpdate(clusterSvc, newSvc) {
		t.Errorf("Incorrectly marked service %v as not needing update", newSvc)
	}
	addNEGAndSvcNegL4Controller(l4c, newSvc)
	err = l4c.sync(getKeyForSvc(newSvc, t), klog.TODO())
	if err != nil {
		t.Errorf("Failed to sync newly updated service %s, err %v", newSvc.Name, err)
	}
	// List the service and ensure that the status field is updated.
	newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	verifyILBServiceProvisioned(t, newSvc)
	// this will be a create metric since an ILB IP is being assigned for the first time.
	currMetrics, metricErr := test.GetL4ILBLatencyMetric()
	if metricErr != nil {
		t.Errorf("Error getting L4 ILB latency metrics %v", metricErr)
	}
	prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 1, UpperBoundSeconds: 1}, t)
}

func TestProcessMultipleServices(t *testing.T) {
	backoff := retry.DefaultRetry
	// Increase the duration since updates take longer on prow.
	backoff.Duration = 3 * time.Second
	for _, onlyLocal := range []bool{true, false} {
		t.Run(fmt.Sprintf("L4 with LocalMode=%v", onlyLocal), func(t *testing.T) {
			l4c, stopCh := newServiceController(t, newFakeGCE(), false)
			defer close(stopCh)
			prevMetrics, err := test.GetL4ILBLatencyMetric()
			if err != nil {
				t.Errorf("Error getting L4 ILB latency metrics %v", err)
			}
			go l4c.Run()
			var svcNames []string
			var testNs string
			for port := 8000; port < 8020; port++ {
				newSvc := test.NewL4ILBService(false, port)
				newSvc.Name = newSvc.Name + fmt.Sprintf("-%d", port)
				svcNames = append(svcNames, newSvc.Name)
				testNs = newSvc.Namespace
				addILBService(l4c, newSvc)
				// add the NEG so that link to backendService works.
				addNEGAndSvcNegL4Controller(l4c, newSvc)
				l4c.svcQueue.Enqueue(newSvc)
			}
			if err := retry.OnError(backoff, func(error) bool { return true }, func() error {
				for _, name := range svcNames {
					newSvc, err := l4c.client.CoreV1().Services(testNs).Get(context2.TODO(), name, v1.GetOptions{})
					if err != nil {
						return fmt.Errorf("Failed to lookup service %s, err: %v", name, err)
					}
					if len(newSvc.Status.LoadBalancer.Ingress) == 0 || newSvc.Annotations[l4annotations.FirewallRuleKey] == "" {
						return fmt.Errorf("waiting for valid IP and/or resource annotations for service %q. Got Status - %+v, Annotations - %v", newSvc.Name, newSvc.Status, newSvc.Annotations)
					}
				}
				return nil
			}); err != nil {
				t.Error(err)
			}
			// Perform a full validation of the service once it is ready.
			for _, name := range svcNames {
				newSvc, _ := l4c.client.CoreV1().Services(testNs).Get(context2.TODO(), name, v1.GetOptions{})
				verifyILBServiceProvisioned(t, newSvc)
			}
			// this will be a create metric since an ILB IP is being assigned for the first time.
			currMetrics, metricErr := test.GetL4ILBLatencyMetric()
			if metricErr != nil {
				t.Errorf("Error getting L4 ILB latency metrics err: %v", metricErr)
			}
			prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 20, UpperBoundSeconds: 1}, t)

		})
	}
}

func TestProcessServiceWithDelayedNEGAdd(t *testing.T) {
	l4c, stopCh := newServiceController(t, newFakeGCE(), false)
	defer close(stopCh)
	go l4c.Run()
	newSvc := test.NewL4ILBService(false, 8080)
	addILBService(l4c, newSvc)
	l4c.svcQueue.Enqueue(newSvc)

	backoff := retry.DefaultRetry
	backoff.Duration = 3 * time.Second
	if err := retry.OnError(backoff, func(error) bool { return true }, func() error {
		if numRequeues := l4c.svcQueue.NumRequeues(newSvc); numRequeues == 0 {
			return fmt.Errorf("Failed to requeue service with delayed NEG addition.")
		}
		return nil
	}); err != nil {
		t.Error(err)
	}
	time.Sleep(5 * time.Second)
	// add the NEG with a delay to simulate NEG controller delays. The L4 Controller is multi-threaded with 5 goroutines.
	// The NEG controller is single-threaded. It is possible for NEG creation to take longer, causing the L4 controller to
	// error out. This test verifies that the service eventually reaches success state.
	t.Logf("Adding NEG for service %s", newSvc.Name)
	addNEGAndSvcNegL4Controller(l4c, newSvc)

	var svcErr error
	if err := retry.OnError(backoff, func(error) bool { return true }, func() error {
		if newSvc, svcErr = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{}); svcErr != nil {
			return fmt.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, svcErr)
		}
		// wait until an IP is assigned and resource annotations are available.
		if len(newSvc.Status.LoadBalancer.Ingress) > 0 && newSvc.Annotations[l4annotations.FirewallRuleKey] != "" {
			return nil
		}
		return fmt.Errorf("waiting for valid IP and/or resource annotations. Got Status - %+v, Annotations - %v", newSvc.Status, newSvc.Annotations)
	}); err != nil {
		t.Error(err)
	}
	verifyILBServiceProvisioned(t, newSvc)
}

func TestProcessServiceOnError(t *testing.T) {
	t.Parallel()
	l4c, _ := newServiceController(t, newFakeGCEWithInsertError(), false)
	prevMetrics, err := test.GetL4ILBErrorMetric()
	if err != nil {
		t.Errorf("Error getting L4 ILB error metrics err: %v", err)
	}
	newSvc := test.NewL4ILBService(false, 8080)
	addILBService(l4c, newSvc)
	addNEGAndSvcNegL4Controller(l4c, newSvc)
	err = l4c.sync(getKeyForSvc(newSvc, t), klog.TODO())
	if err == nil {
		t.Fatalf("Failed to generate error when syncing service %s", newSvc.Name)
	}
	expectMetrics := &test.L4LBErrorMetricInfo{
		ByGCEResource: map[string]uint64{l4annotations.ForwardingRuleResource: 1},
		ByErrorType:   map[string]uint64{http.StatusText(http.StatusInternalServerError): 1}}
	currMetrics, errMetrics := test.GetL4ILBErrorMetric()
	if errMetrics != nil {
		t.Errorf("Error getting L4 ILB error metrics err: %v", errMetrics)
	}
	prevMetrics.ValidateDiff(currMetrics, expectMetrics, t)
}

func TestProcessServiceOnUserError(t *testing.T) {
	t.Parallel()
	l4c, _ := newServiceController(t, newFakeGCEWithUserInsertError(), false)
	newSvc := test.NewL4ILBService(false, 8080)
	addILBService(l4c, newSvc)
	addNEGAndSvcNegL4Controller(l4c, newSvc)
	syncResult := l4c.processServiceCreateOrUpdate(newSvc, klog.TODO())
	if syncResult.Error == nil {
		t.Fatalf("Failed to generate error when syncing service %s", newSvc.Name)
	}
	if !syncResult.MetricsLegacyState.IsUserError {
		t.Errorf("syncResult.MetricsLegacyState.IsUserError should be true, got false")
	}
	if syncResult.MetricsLegacyState.InSuccess {
		t.Errorf("syncResult.MetricsLegacyState.InSuccess should be false, got true")
	}
}

func TestCreateDeleteDualStackService(t *testing.T) {
	testCases := []struct {
		desc       string
		ipFamilies []api_v1.IPFamily
	}{
		{
			desc:       "Create and delete IPv4 ILB",
			ipFamilies: []api_v1.IPFamily{api_v1.IPv4Protocol},
		},
		{
			desc:       "Create and delete IPv4 IPv6 ILB",
			ipFamilies: []api_v1.IPFamily{api_v1.IPv4Protocol, api_v1.IPv6Protocol},
		},
		{
			desc:       "Create and delete IPv6 ILB",
			ipFamilies: []api_v1.IPFamily{api_v1.IPv6Protocol},
		},
		{
			desc:       "Create and delete IPv6 IPv4 ILB",
			ipFamilies: []api_v1.IPFamily{api_v1.IPv6Protocol, api_v1.IPv4Protocol},
		},
		{
			desc:       "Create and delete ILB with empty IP families",
			ipFamilies: []api_v1.IPFamily{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			l4c, _ := newServiceController(t, newFakeGCE(), false)
			prevMetrics, err := test.GetL4ILBLatencyMetric()
			if err != nil {
				t.Errorf("Error getting L4 ILB latency metrics err: %v", err)
			}
			newSvc := test.NewL4ILBDualStackService(8080, api_v1.ProtocolTCP, tc.ipFamilies, api_v1.ServiceExternalTrafficPolicyTypeCluster)

			test.MustCreateDualStackClusterSubnet(t, l4c.ctx.Cloud, "INTERNAL")
			addILBService(l4c, newSvc)
			addNEGAndSvcNegL4Controller(l4c, newSvc)
			err = l4c.sync(getKeyForSvc(newSvc, t), klog.TODO())
			if err != nil {
				t.Errorf("Failed to sync newly added service %s, err %v", newSvc.Name, err)
			}
			// List the service and ensure that it contains the finalizer as well as Status field.
			newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
			}
			verifyILBServiceProvisioned(t, newSvc)
			currMetrics, metricErr := test.GetL4ILBLatencyMetric()
			if metricErr != nil {
				t.Errorf("Error getting L4 ILB latency metrics err: %v", metricErr)
			}
			prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 1, UpperBoundSeconds: 1}, t)

			// Remove the Internal LoadBalancer annotation, this should trigger a cleanup.
			delete(newSvc.Annotations, gce.ServiceAnnotationLoadBalancerType)
			updateILBService(l4c, newSvc)
			err = l4c.sync(getKeyForSvc(newSvc, t), klog.TODO())
			if err != nil {
				t.Errorf("Failed to sync updated service %s, err %v", newSvc.Name, err)
			}

			// List the service and ensure that it doesn't contain the finalizer as well as Status field.
			newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
			}
			verifyILBServiceNotProvisioned(t, newSvc)
			currMetrics, metricErr = test.GetL4ILBLatencyMetric()
			if metricErr != nil {
				t.Errorf("Error getting L4 ILB latency metrics err: %v", metricErr)
			}
			prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 1, DeleteCount: 1, UpperBoundSeconds: 1}, t)
			newSvc.DeletionTimestamp = &v1.Time{}
			updateILBService(l4c, newSvc)
			key, _ := common.KeyFunc(newSvc)
			if err = l4c.sync(key, klog.TODO()); err != nil {
				t.Errorf("Failed to sync deleted service %s, err %v", key, err)
			}
		})
	}
}

func TestProcessDualStackServiceOnUserError(t *testing.T) {
	t.Parallel()
	l4c, _ := newServiceController(t, newFakeGCE(), false)

	// Create cluster subnet with EXTERNAL ipv6 access type to trigger user error.
	test.MustCreateDualStackClusterSubnet(t, l4c.ctx.Cloud, "EXTERNAL")

	newSvc := test.NewL4ILBDualStackService(8080, api_v1.ProtocolTCP, []api_v1.IPFamily{api_v1.IPv4Protocol, api_v1.IPv6Protocol}, api_v1.ServiceExternalTrafficPolicyTypeCluster)
	addILBService(l4c, newSvc)
	addNEGAndSvcNegL4Controller(l4c, newSvc)
	syncResult := l4c.processServiceCreateOrUpdate(newSvc, klog.TODO())
	if syncResult.Error == nil {
		t.Fatalf("Failed to generate error when syncing service %s", newSvc.Name)
	}
	if !syncResult.MetricsLegacyState.IsUserError {
		t.Errorf("syncResult.MetricsLegacyState.IsUserError should be true, got false")
	}
	if syncResult.MetricsLegacyState.InSuccess {
		t.Errorf("syncResult.MetricsLegacyState.InSuccess should be false, got true")
	}
	if syncResult.MetricsState.Status != metrics.StatusUserError {
		t.Errorf("syncResult.MetricsLegacyState.Status should be %s, got %s", metrics.StatusUserError, syncResult.MetricsState.Status)
	}
}

func TestDualStackILBStatusForErrorSync(t *testing.T) {
	l4c, _ := newServiceController(t, newFakeGCE(), false)
	(l4c.ctx.Cloud.Compute().(*cloud.MockGCE)).MockForwardingRules.InsertHook = mock.InsertForwardingRulesInternalErrHook

	newSvc := test.NewL4ILBDualStackService(8080, api_v1.ProtocolTCP, []api_v1.IPFamily{api_v1.IPv4Protocol, api_v1.IPv6Protocol}, api_v1.ServiceExternalTrafficPolicyTypeCluster)
	addILBService(l4c, newSvc)
	addNEGAndSvcNegL4Controller(l4c, newSvc)
	syncResult := l4c.processServiceCreateOrUpdate(newSvc, klog.TODO())
	if syncResult.Error == nil {
		t.Fatalf("Failed to generate error when syncing service %s", newSvc.Name)
	}
	if syncResult.MetricsLegacyState.IsUserError {
		t.Errorf("syncResult.MetricsLegacyState.IsUserError should be false, got true")
	}
	if syncResult.MetricsLegacyState.InSuccess {
		t.Errorf("syncResult.MetricsLegacyState.InSuccess should be false, got true")
	}
	if syncResult.MetricsState.Status != metrics.StatusError {
		t.Errorf("syncResult.MetricsLegacyState.Status should be %s, got %s", metrics.StatusError, syncResult.MetricsState.Status)
	}
	if syncResult.MetricsState.FirstSyncErrorTime == nil {
		t.Fatalf("Metric status FirstSyncErrorTime for service %s/%s mismatch, expected: not nil, received: nil", newSvc.Namespace, newSvc.Name)
	}
}

func TestProcessUpdateILBIPFamilies(t *testing.T) {
	testCases := []struct {
		desc              string
		initialIPFamilies []api_v1.IPFamily
		finalIPFamilies   []api_v1.IPFamily
		shouldUpdate      bool
	}{
		{
			desc:              "Should update ILB on ipv4 -> ipv4, ipv6",
			initialIPFamilies: []api_v1.IPFamily{api_v1.IPv4Protocol},
			finalIPFamilies:   []api_v1.IPFamily{api_v1.IPv4Protocol, api_v1.IPv6Protocol},
			shouldUpdate:      true,
		},
		{
			desc:              "Should update ILB on ipv4, ipv6 -> ipv4",
			initialIPFamilies: []api_v1.IPFamily{api_v1.IPv4Protocol, api_v1.IPv6Protocol},
			finalIPFamilies:   []api_v1.IPFamily{api_v1.IPv4Protocol},
			shouldUpdate:      true,
		},
		{
			desc:              "Should update ILB on ipv6 -> ipv6, ipv4",
			initialIPFamilies: []api_v1.IPFamily{api_v1.IPv6Protocol},
			finalIPFamilies:   []api_v1.IPFamily{api_v1.IPv6Protocol, api_v1.IPv4Protocol},
			shouldUpdate:      true,
		},
		{
			desc:              "Should update ILB on ipv6, ipv4 -> ipv6",
			initialIPFamilies: []api_v1.IPFamily{api_v1.IPv6Protocol, api_v1.IPv4Protocol},
			finalIPFamilies:   []api_v1.IPFamily{api_v1.IPv6Protocol},
			shouldUpdate:      true,
		},
		{
			desc:              "Should not update ILB on same IP families update",
			initialIPFamilies: []api_v1.IPFamily{api_v1.IPv6Protocol, api_v1.IPv4Protocol},
			finalIPFamilies:   []api_v1.IPFamily{api_v1.IPv6Protocol, api_v1.IPv4Protocol},
			shouldUpdate:      false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			l4c, _ := newServiceController(t, newFakeGCE(), false)

			test.MustCreateDualStackClusterSubnet(t, l4c.ctx.Cloud, "INTERNAL")

			svc := test.NewL4ILBDualStackService(8080, api_v1.ProtocolTCP, tc.initialIPFamilies, api_v1.ServiceExternalTrafficPolicyTypeCluster)
			addILBService(l4c, svc)
			addNEGAndSvcNegL4Controller(l4c, svc)
			err := l4c.sync(getKeyForSvc(svc, t), klog.TODO())
			if err != nil {
				t.Errorf("Failed to sync newly added service %s, err %v", svc.Name, err)
			}

			svc, err = l4c.client.CoreV1().Services(svc.Namespace).Get(context2.TODO(), svc.Name, v1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to lookup service %s, err: %v", svc.Name, err)
			}
			verifyILBServiceProvisioned(t, svc)

			newSvc := svc.DeepCopy()
			newSvc.Spec.IPFamilies = tc.finalIPFamilies
			updateILBService(l4c, newSvc)

			needsUpdate := l4c.needsUpdate(svc, newSvc)
			if needsUpdate != tc.shouldUpdate {
				t.Errorf("Service %v needsUpdate = %t, expected %t", newSvc, needsUpdate, tc.shouldUpdate)
			}

			err = l4c.sync(getKeyForSvc(newSvc, t), klog.TODO())
			if err != nil {
				t.Errorf("Failed to sync newly updated service %s, err %v", newSvc.Name, err)
			}
			// List the service and ensure that the status field is updated.
			newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
			}
			verifyILBServiceProvisioned(t, newSvc)
		})
	}
}

func TestProcessCreateServiceWithLoadBalancerClass(t *testing.T) {
	l4c, _ := newServiceController(t, newFakeGCE(), false)
	newSvc := test.NewL4ILBService(false, 8080)
	// Set the legacy finalizer
	testLBClass := "testClass"
	newSvc.Spec.LoadBalancerClass = &testLBClass
	addILBService(l4c, newSvc)
	err := l4c.sync(getKeyForSvc(newSvc, t), klog.TODO())
	if err != nil {
		t.Errorf("Failed to sync newly added service %s, err %v", newSvc.Name, err)
	}
	// List the service and ensure that the status field is not updated.
	svc, err := l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	verifyILBServiceNotProvisioned(t, svc)
}

func TestEnsureInternalLoadBalancerClass(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		desc              string
		loadBalancerClass string
		shouldProcess     bool
	}{
		{
			desc:              "Custom loadBalancerClass should not process",
			loadBalancerClass: "customLBClass",
			shouldProcess:     false,
		},
		{
			desc:              "Use ILB loadBalancerClass",
			loadBalancerClass: l4annotations.RegionalInternalLoadBalancerClass,
			shouldProcess:     true,
		},
		{
			desc:              "Use NetLB loadBalancerClass",
			loadBalancerClass: l4annotations.RegionalExternalLoadBalancerClass,
			shouldProcess:     false,
		},
		{
			desc:              "Unset loadBalancerClass",
			loadBalancerClass: "",
			shouldProcess:     true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			l4c, _ := newServiceController(t, newFakeGCE(), false)

			svc := test.NewL4LBServiceWithLoadBalancerClass(tc.loadBalancerClass)
			if tc.loadBalancerClass == "" {
				svc = test.NewL4ILBService(false, 8080)
			}

			// Create ILB and add NEGs
			addILBService(l4c, svc)
			addNEGAndSvcNegL4Controller(l4c, svc)
			key := getKeyForSvc(svc, t)
			err := l4c.sync(key, klog.TODO())
			if err != nil {
				t.Errorf("Failed to sync service %s, err %v", svc.Name, err)
			}
			svc, err = l4c.client.CoreV1().Services(svc.Namespace).Get(context2.TODO(), svc.Name, v1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to lookup service %s, err: %v", svc.Name, err)
			}

			if tc.shouldProcess {
				verifyILBServiceProvisioned(t, svc)
			} else {
				verifyILBServiceNotProvisioned(t, svc)
			}

			// Update ILB
			svc.Spec.ExternalTrafficPolicy = api_v1.ServiceExternalTrafficPolicyTypeLocal
			updateILBService(l4c, svc)
			err = l4c.sync(key, klog.TODO())
			if err != nil {
				t.Errorf("Failed to sync updated service %s, err %v", svc.Name, err)
			}
			svc, err = l4c.client.CoreV1().Services(svc.Namespace).Get(context2.TODO(), svc.Name, v1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to lookup service %s, err: %v", svc.Name, err)
			}

			if tc.shouldProcess {
				if svc.Spec.ExternalTrafficPolicy != api_v1.ServiceExternalTrafficPolicyTypeLocal {
					t.Errorf("Failed to update service. ExternalTrafficPolicy mismatch %v != %v", svc.Spec.ExternalTrafficPolicy, api_v1.ServiceExternalTrafficPolicyTypeLocal)
				}
				verifyILBServiceProvisioned(t, svc)
			} else {
				verifyILBServiceNotProvisioned(t, svc)
			}

			// Delete ILB
			svc.DeletionTimestamp = &v1.Time{}
			updateILBService(l4c, svc)
			if tc.shouldProcess {
				if !l4c.needsDeletion(svc) {
					t.Errorf("Service should be marked for deletion")
				}
			} else {
				if l4c.needsDeletion(svc) {
					t.Errorf("Service should not be marked for deletion")
				}
			}
			err = l4c.sync(key, klog.TODO())
			if err != nil {
				t.Errorf("Failed to sync updated service %s, err %v", svc.Name, err)
			}
			svc, err = l4c.client.CoreV1().Services(svc.Namespace).Get(context2.TODO(), svc.Name, v1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to lookup service %s, err: %v", svc.Name, err)
			}
			verifyILBServiceNotProvisioned(t, svc)
			deleteILBService(l4c, svc)
		})
	}
}

func newServiceController(t *testing.T, fakeGCE *gce.Cloud, readOnlyMode bool) (*L4Controller, chan struct{}) {
	kubeClient := fake.NewSimpleClientset()
	svcNegClient := svcnegclient.NewSimpleClientset()

	vals := test.DefaultTestClusterValues()
	namer := namer.NewNamer(clusterUID, "", klog.TODO())

	stopCh := make(chan struct{})
	ctxConfig := context.ControllerContextConfig{
		Namespace:              api_v1.NamespaceAll,
		ResyncPeriod:           1 * time.Minute,
		NumL4Workers:           5,
		ReadOnlyMode:           readOnlyMode,
		EnableL4ILBDualStack:   true,
		EnableL4NetLBDualStack: true,
	}
	ctx, err := context.NewControllerContext(kubeClient, nil, nil, nil, svcNegClient, nil, nil, nil, nil, kubeClient /*kube client to be used for events*/, fakeGCE, namer, "" /*kubeSystemUID*/, ctxConfig, klog.TODO())
	if err != nil {
		t.Fatalf("failed to initialize controller context: %v", err)
	}
	ctx.ZoneGetter, err = zonegetter.NewFakeZoneGetter(ctx.NodeInformer, zonegetter.FakeNodeTopologyInformer(), test.DefaultTestSubnetURL, false)
	if err != nil {
		t.Fatalf("failed to initialize zone getter: %v", err)
	}
	// Add some nodes so that NEG linker kicks in during ILB creation.
	nodes, err := test.CreateAndInsertNodes(ctx.Cloud, []string{"instance-1"}, vals.ZoneName)
	if err != nil {
		t.Errorf("Failed to add new nodes, err  %v", err)
	}
	for _, n := range nodes {
		ctx.NodeInformer.GetIndexer().Add(n)
	}
	l4c := NewILBController(ctx, stopCh, klog.TODO())
	l4c.hasSynced = func() bool { return true }
	return l4c, stopCh
}

func newFakeGCE() *gce.Cloud {
	vals := test.DefaultTestClusterValues()
	fakeGCE := gce.NewFakeGCECloud(vals)
	(fakeGCE.Compute().(*cloud.MockGCE)).MockForwardingRules.InsertHook = loadbalancers.InsertForwardingRuleHook
	return fakeGCE
}

func newFakeGCEWithInsertError() *gce.Cloud {
	vals := test.DefaultTestClusterValues()
	fakeGCE := gce.NewFakeGCECloud(vals)
	(fakeGCE.Compute().(*cloud.MockGCE)).MockForwardingRules.InsertHook = mock.InsertForwardingRulesInternalErrHook
	return fakeGCE
}

func newFakeGCEWithUserInsertError() *gce.Cloud {
	vals := test.DefaultTestClusterValues()
	fakeGCE := gce.NewFakeGCECloud(vals)
	(fakeGCE.Compute().(*cloud.MockGCE)).MockForwardingRules.InsertHook = test.InsertForwardingRuleErrorHook(&googleapi.Error{Code: http.StatusConflict, Message: "IP_IN_USE_BY_ANOTHER_RESOURCE - IP '1.1.1.1' is already being used by another resource."})
	return fakeGCE
}

func addILBService(l4c *L4Controller, svc *api_v1.Service) {
	l4c.ctx.KubeClient.CoreV1().Services(svc.Namespace).Create(context2.TODO(), svc, v1.CreateOptions{})
	l4c.ctx.ServiceInformer.GetIndexer().Add(svc)
}

func updateILBService(l4c *L4Controller, svc *api_v1.Service) {
	l4c.ctx.KubeClient.CoreV1().Services(svc.Namespace).Update(context2.TODO(), svc, v1.UpdateOptions{})
	l4c.ctx.ServiceInformer.GetIndexer().Update(svc)
}

func deleteILBService(l4c *L4Controller, svc *api_v1.Service) {
	l4c.ctx.KubeClient.CoreV1().Services(svc.Namespace).Delete(context2.TODO(), svc.Name, v1.DeleteOptions{})
	l4c.ctx.ServiceInformer.GetIndexer().Delete(svc)
}

func addNEGAndSvcNegL4Controller(l4c *L4Controller, svc *api_v1.Service) {
	// Also create a fake NEG for this service since the sync code will try to link the backend service to NEG
	negName := l4c.namer.L4Backend(svc.Namespace, svc.Name)
	neg := &computebeta.NetworkEndpointGroup{Name: negName}
	l4c.ctx.Cloud.CreateNetworkEndpointGroup(neg, testGCEZone)

	newSvcNeg := test.NewSvcNeg(types.NamespacedName{
		Namespace: svc.Namespace, Name: negName,
	}, negv1beta1.ServiceNetworkEndpointGroupStatus{
		NetworkEndpointGroups: []negv1beta1.NegObjectReference{
			{SelfLink: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/mock-project/zones/%s/networkEndpointGroups/%s", testGCEZone, negName)},
		},
	})
	addSvcNegL4Controller(l4c, newSvcNeg)
}

func addSvcNegL4Controller(l4c *L4Controller, svcneg *negv1beta1.ServiceNetworkEndpointGroup) {
	l4c.ctx.SvcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svcneg.Namespace).Create(context2.TODO(), svcneg, v1.CreateOptions{})
	l4c.ctx.SvcNegInformer.GetIndexer().Add(svcneg)
}

func getKeyForSvc(svc *api_v1.Service, t *testing.T) string {
	key, err := common.KeyFunc(svc)
	if err != nil {
		t.Fatalf("Failed to get key for service %v, err : %v", svc, err)
	}
	return key
}

func calculateExpectedAnnotationsKeys(svc *api_v1.Service) []string {
	expectedAnnotations := ilbCommonAnnotationKeys
	if utils.NeedsIPv4(svc) {
		expectedAnnotations = append(expectedAnnotations, ilbIPv4AnnotationKeys...)
	}
	if utils.NeedsIPv6(svc) {
		expectedAnnotations = append(expectedAnnotations, ilbIPv6AnnotationKeys...)
	}
	return expectedAnnotations
}

func verifyILBServiceProvisioned(t *testing.T, svc *api_v1.Service) {
	t.Helper()

	if !common.HasGivenFinalizer(svc.ObjectMeta, common.ILBFinalizerV2) {
		t.Errorf("ILB v2 finalizer is not found, expected to exist, svc %+v", svc)
	}

	ingressIPs := svc.Status.LoadBalancer.Ingress
	expectedIPsLen := len(svc.Spec.IPFamilies)
	// non dualstack tests do not set IPFamilies,
	if expectedIPsLen == 0 {
		expectedIPsLen = 1
	}
	if len(ingressIPs) != expectedIPsLen {
		t.Errorf("Expected len(ingressIPs) = %d, got %d", expectedIPsLen, len(ingressIPs))
	}
	for _, ingress := range ingressIPs {
		if ingress.IP == "" {
			t.Errorf("Ingress VIP not assigned to service")
		}
	}

	expectedAnnotationsKeys := calculateExpectedAnnotationsKeys(svc)
	var missingKeys []string
	for _, key := range expectedAnnotationsKeys {
		if _, ok := svc.Annotations[key]; !ok {
			missingKeys = append(missingKeys, key)
		}
	}
	if len(missingKeys) > 0 {
		t.Errorf("Cannot find annotations %v in ILB service, Got %v", missingKeys, svc.Annotations)
	}
}

func verifyILBServiceNotProvisioned(t *testing.T, svc *api_v1.Service) {
	t.Helper()

	if common.HasGivenFinalizer(svc.ObjectMeta, common.ILBFinalizerV2) {
		t.Errorf("ILB v2 finalizer should not exist on service %+v", svc)
	}

	if len(svc.Status.LoadBalancer.Ingress) > 0 {
		t.Errorf("Expected LoadBalancer status to be empty, Got %v", svc.Status.LoadBalancer)
	}

	expectedAnnotationsKeys := calculateExpectedAnnotationsKeys(svc)
	var missingKeys []string
	for _, key := range expectedAnnotationsKeys {
		if _, ok := svc.Annotations[key]; !ok {
			missingKeys = append(missingKeys, key)
		}
	}
	if len(missingKeys) != len(expectedAnnotationsKeys) {
		t.Errorf("Unexpected ILB annotations present, Got %v", svc.Annotations)
	}
}

func createLegacyForwardingRule(t *testing.T, svc *api_v1.Service, cloud *gce.Cloud, scheme string) {
	t.Helper()
	frName := cloudprovider.DefaultLoadBalancerName(svc)
	key, err := composite.CreateKey(cloud, frName, meta.Regional)
	if err != nil {
		t.Errorf("Unexpected error when creating key - %v", err)
	}
	var ip string
	if len(svc.Status.LoadBalancer.Ingress) > 0 {
		ip = svc.Status.LoadBalancer.Ingress[0].IP
	}
	existingFwdRule := &composite.ForwardingRule{
		Name:                frName,
		IPAddress:           ip,
		Ports:               []string{"123"},
		IPProtocol:          "TCP",
		LoadBalancingScheme: scheme,
	}
	if err = composite.CreateForwardingRule(cloud, key, existingFwdRule, klog.TODO()); err != nil {
		t.Errorf("Failed to create fake forwarding rule %s, err %v", frName, err)
	}
}

func TestMultipleServicesProcessingWithLegacyHeadStartTime(t *testing.T) {
	origL4ILBLegacyHeadStartTime := flags.F.L4ILBLegacyHeadStartTime
	flags.F.L4ILBLegacyHeadStartTime = 3 * time.Second
	defer func() {
		flags.F.L4ILBLegacyHeadStartTime = origL4ILBLegacyHeadStartTime
	}()
	backoff := retry.DefaultRetry

	testCases := []struct {
		desc            string
		backoffDuration time.Duration
		svcFinalizers   []string
		shouldProcess   bool
	}{
		{
			desc:            "New service without finalizers after head start",
			backoffDuration: 5 * time.Second,
			shouldProcess:   true,
		},
		{
			desc:            "New service without finalizers, head start for CCM",
			backoffDuration: 3 * time.Second,
			shouldProcess:   false,
		},
		{
			desc:            "Existing service with finalizer V2",
			backoffDuration: 3 * time.Second,
			svcFinalizers:   []string{common.ILBFinalizerV2},
			shouldProcess:   true,
		},
		{
			desc:            "Existing service with finalizer V1",
			backoffDuration: 3 * time.Second,
			svcFinalizers:   []string{common.LegacyILBFinalizer},
			shouldProcess:   false,
		},
	}
	for _, tc := range testCases {
		// Increase the duration including the wait before fetching the service.
		backoff.Duration = tc.backoffDuration
		for _, onlyLocal := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s - L4 with LocalMode=%v", tc.desc, onlyLocal), func(t *testing.T) {
				l4c, stopCh := newServiceController(t, newFakeGCE(), false)
				defer close(stopCh)
				prevMetrics, err := test.GetL4ILBLatencyMetric()
				if err != nil {
					t.Errorf("Error getting L4 ILB latency metrics %v", err)
				}
				go l4c.Run()
				var svcNames []string
				var testNs string
				for port := 8000; port < 8020; port++ {
					newSvc := test.NewL4ILBService(onlyLocal, port)
					if len(tc.svcFinalizers) > 0 {
						newSvc.ObjectMeta.Finalizers = tc.svcFinalizers
					}
					newSvc.Name = newSvc.Name + fmt.Sprintf("-%d", port)
					svcNames = append(svcNames, newSvc.Name)
					testNs = newSvc.Namespace
					addILBService(l4c, newSvc)
					// add the NEG so that link to backendService works.
					addNEGAndSvcNegL4Controller(l4c, newSvc)
					l4c.svcQueue.Enqueue(newSvc)
					defer deleteILBService(l4c, newSvc)
				}
				if err := retry.OnError(backoff, func(error) bool { return true }, func() error {
					for _, name := range svcNames {
						newSvc, err := l4c.client.CoreV1().Services(testNs).Get(context2.TODO(), name, v1.GetOptions{})
						if err != nil {
							return fmt.Errorf("failed to lookup service %s, err: %v", name, err)
						}
						if tc.shouldProcess && (len(newSvc.Status.LoadBalancer.Ingress) == 0 || newSvc.Annotations[l4annotations.FirewallRuleKey] == "") {
							return fmt.Errorf("waiting for valid IP and/or resource annotations for service %q. Got Status - %+v, Annotations - %v", newSvc.Name, newSvc.Status, newSvc.Annotations)
						}
					}
					return nil
				}); err != nil {
					t.Error(err)
				}
				if tc.shouldProcess {
					// Perform a full validation of the service once it is ready.
					for _, name := range svcNames {
						newSvc, _ := l4c.client.CoreV1().Services(testNs).Get(context2.TODO(), name, v1.GetOptions{})
						verifyILBServiceProvisioned(t, newSvc)
					}
				}
				// this will be a create metric since an ILB IP is being assigned for the first time.
				currMetrics, metricErr := test.GetL4ILBLatencyMetric()
				if metricErr != nil {
					t.Errorf("Error getting L4 ILB latency metrics err: %v", metricErr)
				}
				if tc.shouldProcess {
					prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 20, UpperBoundSeconds: 1}, t)
				} else {
					prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 0, UpperBoundSeconds: 1}, t)
				}
			})
		}
	}
}
