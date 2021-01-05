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

package l4

import (
	context2 "context"
	"testing"
	"time"

	"k8s.io/ingress-gce/pkg/loadbalancers"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	api_v1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	clusterUID = "aaaaa"
	// This is one of the zones used in gce_fake.go
	testGCEZone = "us-central1-b"
)

func newServiceController(t *testing.T) *L4Controller {
	kubeClient := fake.NewSimpleClientset()
	vals := gce.DefaultTestClusterValues()
	fakeGCE := gce.NewFakeGCECloud(vals)
	(fakeGCE.Compute().(*cloud.MockGCE)).MockForwardingRules.InsertHook = loadbalancers.InsertForwardingRuleHook

	namer := namer.NewNamer(clusterUID, "")

	stopCh := make(chan struct{})
	ctxConfig := context.ControllerContextConfig{
		Namespace:    api_v1.NamespaceAll,
		ResyncPeriod: 1 * time.Minute,
	}
	ctx := context.NewControllerContext(nil, kubeClient, nil, nil, nil, nil, fakeGCE, namer, "" /*kubeSystemUID*/, ctxConfig)
	// Add some nodes so that NEG linker kicks in during ILB creation.
	nodes, err := test.CreateAndInsertNodes(ctx.Cloud, []string{"instance-1"}, vals.ZoneName)
	if err != nil {
		t.Errorf("Failed to add new nodes, err  %v", err)
	}
	for _, n := range nodes {
		ctx.NodeInformer.GetIndexer().Add(n)
	}
	return NewController(ctx, stopCh)
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

func addNEG(l4c *L4Controller, svc *api_v1.Service) {
	// Also create a fake NEG for this service since the sync code will try to link the backend service to NEG
	negName, _ := l4c.namer.VMIPNEG(svc.Namespace, svc.Name)
	neg := &composite.NetworkEndpointGroup{Name: negName}
	key := meta.ZonalKey(negName, testGCEZone)
	composite.CreateNetworkEndpointGroup(l4c.ctx.Cloud, key, neg)
}

func getKeyForSvc(svc *api_v1.Service, t *testing.T) string {
	key, err := common.KeyFunc(svc)
	if err != nil {
		t.Fatalf("Failed to get key for service %v, err : %v", svc, err)
	}
	return key
}

func validateSvcStatus(svc *api_v1.Service, expectStatus bool, t *testing.T) {
	if common.HasGivenFinalizer(svc.ObjectMeta, common.ILBFinalizerV2) != expectStatus {
		t.Fatalf("Expected L4 finalizer present to be %v, but it was %v", expectStatus, !expectStatus)
	}
	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
		if expectStatus {
			t.Fatalf("Invalid LoadBalancer status field in service - %+v", svc.Status.LoadBalancer)
		}
	}
	if len(svc.Status.LoadBalancer.Ingress) > 0 && !expectStatus {
		t.Fatalf("Expected LoadBalancer status to be empty, Got %v", svc.Status.LoadBalancer)
	}

	expectedAnnotationKeys := []string{annotations.FirewallRuleKey, annotations.BackendServiceKey, annotations.HealthcheckKey,
		annotations.TCPForwardingRuleKey}

	missingKeys := []string{}
	for _, key := range expectedAnnotationKeys {
		if _, ok := svc.Annotations[key]; !ok {
			missingKeys = append(missingKeys, key)
		}
	}
	if expectStatus {
		// All annotations are expected to be present in this case
		if len(missingKeys) > 0 {
			t.Fatalf("Cannot find annotations %v in ILB service, Got %v", missingKeys, svc.Annotations)
		}
	} else {
		//None of the ILB keys should be present since the ILB has been deleted.
		if len(missingKeys) != len(expectedAnnotationKeys) {
			t.Fatalf("Unexpected ILB annotations still present, Got %v", svc.Annotations)
		}
	}
}

type latencyMetricInfo struct {
	createCount       uint64
	deleteCount       uint64
	updateCount       uint64
	createSum         float64
	updateSum         float64
	deleteSum         float64
	upperBoundSeconds float64
}

func getLatencyMetric(t *testing.T) *latencyMetricInfo {
	var createCount, updateCount, deleteCount uint64
	var createSum, updateSum, deleteSum float64
	var result latencyMetricInfo

	latencyMetric, err := test.GetPrometheusMetric("l4_ilb_sync_duration_seconds")
	if err != nil {
		t.Errorf("Failed to get L4 ILB prometheus metric 'l4_ilb_sync_duration_seconds', err: %v", err)
		return nil
	}
	for _, val := range latencyMetric.GetMetric() {
		for _, label := range val.Label {
			if label.GetName() == "sync_type" {
				switch label.GetValue() {
				case "create":
					createCount += val.GetHistogram().GetSampleCount()
					createSum += val.GetHistogram().GetSampleSum()
				case "update":
					updateCount += val.GetHistogram().GetSampleCount()
					updateSum += val.GetHistogram().GetSampleSum()
				case "delete":
					deleteCount += val.GetHistogram().GetSampleCount()
					deleteSum += val.GetHistogram().GetSampleSum()
				default:
					t.Errorf("Invalid label %s:%s", label.GetName(), label.GetValue())
				}
			}
		}
		result.createCount = createCount
		result.updateCount = updateCount
		result.deleteCount = deleteCount
		result.createSum = createSum
		result.deleteSum = deleteSum
		result.updateSum = updateSum
	}
	return &result
}

// ValidateDiff ensures that the diff between the old and the new metric is as expected.
// The test uses diff rather than absolute values since the metrics are cumulative of all test cases.
func (old *latencyMetricInfo) ValidateDiff(new, expect *latencyMetricInfo, t *testing.T) {
	new.createCount = new.createCount - old.createCount
	new.deleteCount = new.deleteCount - old.deleteCount
	new.updateCount = new.updateCount - old.updateCount
	new.createSum = new.createSum - old.createSum
	new.updateSum = new.updateSum - old.updateSum
	new.deleteSum = new.deleteSum - old.updateSum
	if new.createCount != expect.createCount || new.deleteCount != expect.deleteCount || new.updateCount != expect.updateCount {
		t.Errorf("Got createCount %d, want %d; Got deleteCount %d, want %d; Got updateCount %d, want %d",
			new.createCount, expect.createCount, new.deleteCount, expect.deleteCount, new.updateCount, expect.updateCount)
	}
	createLatency := getLatency(new.createSum, float64(new.createCount))
	deleteLatency := getLatency(new.deleteSum, float64(new.deleteCount))
	updateLatency := getLatency(new.updateSum, float64(new.updateCount))

	if createLatency > expect.upperBoundSeconds || deleteLatency > expect.upperBoundSeconds || updateLatency > expect.upperBoundSeconds {
		t.Errorf("Got createLatency %v, updateLatency %v, deleteLatency %v - atleast one of them is higher than the specified limit %v seconds", createLatency, updateLatency, deleteLatency, expect.upperBoundSeconds)
	}
}

func getLatency(latencySum, numPoints float64) float64 {
	if numPoints == 0 {
		return 0
	}
	return latencySum / numPoints
}

// TestProcessCreateOrUpdate verifies the processing loop in L4Controller.
// This test adds a new service, then performs a valid update and then modifies the service type to External and ensures
// that the status field is as expected in each case.
func TestProcessCreateOrUpdate(t *testing.T) {
	l4c := newServiceController(t)
	prevMetrics := getLatencyMetric(t)
	newSvc := test.NewL4ILBService(false, 8080)
	addILBService(l4c, newSvc)
	addNEG(l4c, newSvc)
	err := l4c.sync(getKeyForSvc(newSvc, t))
	if err != nil {
		t.Errorf("Failed to sync newly added service %s, err %v", newSvc.Name, err)
	}
	// List the service and ensure that it contains the finalizer as well as Status field.
	newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	validateSvcStatus(newSvc, true, t)
	prevMetrics.ValidateDiff(getLatencyMetric(t), &latencyMetricInfo{createCount: 1, upperBoundSeconds: 1}, t)

	// set the TrafficPolicy of the service to Local
	newSvc.Spec.ExternalTrafficPolicy = api_v1.ServiceExternalTrafficPolicyTypeLocal
	updateILBService(l4c, newSvc)
	err = l4c.sync(getKeyForSvc(newSvc, t))
	if err != nil {
		t.Errorf("Failed to sync updated service %s, err %v", newSvc.Name, err)
	}
	// List the service and ensure that it contains the finalizer as well as Status field.
	newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	validateSvcStatus(newSvc, true, t)
	prevMetrics.ValidateDiff(getLatencyMetric(t), &latencyMetricInfo{createCount: 1, updateCount: 1, upperBoundSeconds: 1}, t)
	// Remove the Internal LoadBalancer annotation, this should trigger a cleanup.
	delete(newSvc.Annotations, gce.ServiceAnnotationLoadBalancerType)
	updateILBService(l4c, newSvc)
	err = l4c.sync(getKeyForSvc(newSvc, t))
	if err != nil {
		t.Errorf("Failed to sync updated service %s, err %v", newSvc.Name, err)
	}

	// List the service and ensure that it doesn't contain the finalizer as well as Status field.
	newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	validateSvcStatus(newSvc, false, t)
	prevMetrics.ValidateDiff(getLatencyMetric(t), &latencyMetricInfo{createCount: 1, updateCount: 1, deleteCount: 1, upperBoundSeconds: 1}, t)
}

func TestProcessDeletion(t *testing.T) {
	l4c := newServiceController(t)
	prevMetrics := getLatencyMetric(t)
	newSvc := test.NewL4ILBService(false, 8080)
	addILBService(l4c, newSvc)
	addNEG(l4c, newSvc)
	err := l4c.sync(getKeyForSvc(newSvc, t))
	if err != nil {
		t.Errorf("Failed to sync newly added service %s, err %v", newSvc.Name, err)
	}
	// List the service and ensure that it contains the finalizer and the status field
	newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	validateSvcStatus(newSvc, true, t)
	prevMetrics.ValidateDiff(getLatencyMetric(t), &latencyMetricInfo{createCount: 1, upperBoundSeconds: 1}, t)

	// Mark the service for deletion by updating timestamp. Use svc instead of newSvc since that has the finalizer.
	newSvc.DeletionTimestamp = &v1.Time{}
	updateILBService(l4c, newSvc)
	if !needsDeletion(newSvc) {
		t.Errorf("Incorrectly marked service %v as not needing ILB deletion", newSvc)
	}
	err = l4c.sync(getKeyForSvc(newSvc, t))
	if err != nil {
		t.Errorf("Failed to sync updated service %s, err %v", newSvc.Name, err)
	}

	// List the service and ensure that it does not contain the finalizer or the status field
	newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	validateSvcStatus(newSvc, false, t)
	prevMetrics.ValidateDiff(getLatencyMetric(t), &latencyMetricInfo{createCount: 1, deleteCount: 1, upperBoundSeconds: 1}, t)
	deleteILBService(l4c, newSvc)
	newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if newSvc != nil {
		t.Errorf("Expected service to be deleted, but was found - %v", newSvc)
	}
}

func TestProcessCreateLegacyService(t *testing.T) {
	l4c := newServiceController(t)
	prevMetrics := getLatencyMetric(t)
	newSvc := test.NewL4ILBService(false, 8080)
	// Set the legacy finalizer
	newSvc.Finalizers = append(newSvc.Finalizers, common.LegacyILBFinalizer)
	addILBService(l4c, newSvc)
	err := l4c.sync(getKeyForSvc(newSvc, t))
	if err != nil {
		t.Errorf("Failed to sync newly added service %s, err %v", newSvc.Name, err)
	}
	// List the service and ensure that the status field is not updated.
	svc, err := l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	validateSvcStatus(svc, false, t)
	prevMetrics.ValidateDiff(getLatencyMetric(t), &latencyMetricInfo{}, t)
}

func TestProcessUpdateClusterIPToILBService(t *testing.T) {
	l4c := newServiceController(t)
	prevMetrics := getLatencyMetric(t)
	clusterSvc := &api_v1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name:      "testsvc",
			Namespace: "testns",
		},
	}
	addILBService(l4c, clusterSvc)
	if needsILB, _ := annotations.WantsL4ILB(clusterSvc); needsILB {
		t.Errorf("Incorrectly marked service %v as needing ILB", clusterSvc)
	}
	if needsDeletion(clusterSvc) {
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
	addNEG(l4c, newSvc)
	err := l4c.sync(getKeyForSvc(newSvc, t))
	if err != nil {
		t.Errorf("Failed to sync newly updated service %s, err %v", newSvc.Name, err)
	}
	// List the service and ensure that the status field is updated.
	newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	validateSvcStatus(newSvc, true, t)
	// this will be a create metric since an ILB IP is being assigned for the first time.
	prevMetrics.ValidateDiff(getLatencyMetric(t), &latencyMetricInfo{createCount: 1, upperBoundSeconds: 1}, t)
}

func TestCheckHealth(t *testing.T) {
	l4c := newServiceController(t)
	if err := l4c.checkHealth(); err != nil {
		// controller should be healthy since no enqueues or syncs have happened.
		t.Errorf("checkHealth returned error - %v, expected nil", err)
	}
	l4c.enqueueTracker.Set(time.Now())
	l4c.syncTracker.Set(time.Now().Add(2 * time.Minute))
	if err := l4c.checkHealth(); err != nil {
		// controller should be healthy since sync happened 2 mins after enqueue, less than the 15 min threshold.
		t.Errorf("checkHealth returned error - %v, expected nil", err)
	}
	l4c.enqueueTracker.Set(time.Now())
	delayedSyncTime := time.Now().Add(enqueueToSyncDelayThreshold)
	delayedSyncTime.Add(1 * time.Minute)
	l4c.syncTracker.Set(delayedSyncTime)
	if err := l4c.checkHealth(); err == nil {
		// controller should error out since sync happened 1 minute after the threshold.
		t.Errorf("checkHealth returned nil, expected error")
	}

}
