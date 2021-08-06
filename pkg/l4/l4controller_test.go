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
	"fmt"
	"testing"
	"time"

	"k8s.io/ingress-gce/pkg/loadbalancers"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	api_v1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/retry"
	"k8s.io/cloud-provider"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
	"net/http"
)

const (
	clusterUID = "aaaaa"
	// This is one of the zones used in gce_fake.go
	testGCEZone = "us-central1-b"
)

func newServiceController(t *testing.T, fakeGCE *gce.Cloud) *L4Controller {
	kubeClient := fake.NewSimpleClientset()

	vals := gce.DefaultTestClusterValues()
	namer := namer.NewNamer(clusterUID, "")

	stopCh := make(chan struct{})
	ctxConfig := context.ControllerContextConfig{
		Namespace:    api_v1.NamespaceAll,
		ResyncPeriod: 1 * time.Minute,
		NumL4Workers: 5,
	}
	ctx := context.NewControllerContext(nil, kubeClient, nil, nil, nil, nil, nil, fakeGCE, namer, "" /*kubeSystemUID*/, ctxConfig)
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

func newFakeGCE() *gce.Cloud {
	vals := gce.DefaultTestClusterValues()
	fakeGCE := gce.NewFakeGCECloud(vals)
	(fakeGCE.Compute().(*cloud.MockGCE)).MockForwardingRules.InsertHook = loadbalancers.InsertForwardingRuleHook
	return fakeGCE
}

func newFakeGCEWithInsertError() *gce.Cloud {
	vals := gce.DefaultTestClusterValues()
	fakeGCE := gce.NewFakeGCECloud(vals)
	(fakeGCE.Compute().(*cloud.MockGCE)).MockForwardingRules.InsertHook = mock.InsertForwardingRulesInternalErrHook
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
		annotations.TCPForwardingRuleKey, annotations.FirewallRuleForHealthcheckKey}

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
	if err = composite.CreateForwardingRule(cloud, key, existingFwdRule); err != nil {
		t.Errorf("Failed to create fake forwarding rule %s, err %v", frName, err)
	}
}

// TestProcessCreateOrUpdate verifies the processing loop in L4Controller.
// This test adds a new service, then performs a valid update and then modifies the service type to External and ensures
// that the status field is as expected in each case.
func TestProcessCreateOrUpdate(t *testing.T) {
	l4c := newServiceController(t, newFakeGCE())
	prevMetrics := test.GetL4LatencyMetric(t)
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
	prevMetrics.ValidateDiff(test.GetL4LatencyMetric(t), &test.L4ILBLatencyMetricInfo{CreateCount: 1, UpperBoundSeconds: 1}, t)

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
	prevMetrics.ValidateDiff(test.GetL4LatencyMetric(t), &test.L4ILBLatencyMetricInfo{CreateCount: 1, UpdateCount: 1, UpperBoundSeconds: 1}, t)
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
	prevMetrics.ValidateDiff(test.GetL4LatencyMetric(t), &test.L4ILBLatencyMetricInfo{CreateCount: 1, UpdateCount: 1, DeleteCount: 1, UpperBoundSeconds: 1}, t)
}

func TestProcessDeletion(t *testing.T) {
	l4c := newServiceController(t, newFakeGCE())
	prevMetrics := test.GetL4LatencyMetric(t)
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
	prevMetrics.ValidateDiff(test.GetL4LatencyMetric(t), &test.L4ILBLatencyMetricInfo{CreateCount: 1, UpperBoundSeconds: 1}, t)

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
	prevMetrics.ValidateDiff(test.GetL4LatencyMetric(t), &test.L4ILBLatencyMetricInfo{CreateCount: 1, DeleteCount: 1, UpperBoundSeconds: 1}, t)
	deleteILBService(l4c, newSvc)
	newSvc, err = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if newSvc != nil {
		t.Errorf("Expected service to be deleted, but was found - %v", newSvc)
	}
}

func TestProcessCreateLegacyService(t *testing.T) {
	l4c := newServiceController(t, newFakeGCE())
	prevMetrics := test.GetL4LatencyMetric(t)
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
	prevMetrics.ValidateDiff(test.GetL4LatencyMetric(t), &test.L4ILBLatencyMetricInfo{}, t)
}

func TestProcessCreateServiceWithLegacyInternalForwardingRule(t *testing.T) {
	l4c := newServiceController(t, newFakeGCE())
	prevMetrics := test.GetL4LatencyMetric(t)
	newSvc := test.NewL4ILBService(false, 8080)
	addILBService(l4c, newSvc)
	// Mimic addition of NEG. This will not actually happen, but this test verifies that sync is skipped
	// even if a NEG got added.
	addNEG(l4c, newSvc)
	// Create legacy forwarding rule to mimic service controller.
	// A service can have the v1 finalizer reset due to a buggy script/manual operation.
	// Subsetting controller should only process the service if it doesn't already have a forwarding rule.
	createLegacyForwardingRule(t, newSvc, l4c.ctx.Cloud, string(cloud.SchemeInternal))
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
	prevMetrics.ValidateDiff(test.GetL4LatencyMetric(t), &test.L4ILBLatencyMetricInfo{}, t)
}

func TestProcessCreateServiceWithLegacyExternalForwardingRule(t *testing.T) {
	l4c := newServiceController(t, newFakeGCE())
	prevMetrics := test.GetL4LatencyMetric(t)
	newSvc := test.NewL4ILBService(false, 8080)
	addILBService(l4c, newSvc)
	// Mimic addition of NEG. This will happen in parallel with ILB sync, by the NEG controller.
	addNEG(l4c, newSvc)
	// Create legacy external forwarding rule to mimic transition from external to internal LB.
	// Service processing should succeed in that case. The external forwarding rule will be deleted
	// by service controller.
	createLegacyForwardingRule(t, newSvc, l4c.ctx.Cloud, string(cloud.SchemeExternal))
	err := l4c.sync(getKeyForSvc(newSvc, t))
	if err != nil {
		t.Errorf("Failed to sync newly added service %s, err %v", newSvc.Name, err)
	}
	// List the service and ensure that the status is updated.
	svc, err := l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	validateSvcStatus(svc, true, t)
	prevMetrics.ValidateDiff(test.GetL4LatencyMetric(t), &test.L4ILBLatencyMetricInfo{CreateCount: 1, UpperBoundSeconds: 1}, t)
}

func TestProcessUpdateClusterIPToILBService(t *testing.T) {
	l4c := newServiceController(t, newFakeGCE())
	prevMetrics := test.GetL4LatencyMetric(t)
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
	prevMetrics.ValidateDiff(test.GetL4LatencyMetric(t), &test.L4ILBLatencyMetricInfo{CreateCount: 1, UpperBoundSeconds: 1}, t)
}

func TestProcessMultipleServices(t *testing.T) {
	backoff := retry.DefaultRetry
	// Increase the duration since updates take longer on prow.
	backoff.Duration = 1 * time.Second
	for _, onlyLocal := range []bool{true, false} {
		t.Run(fmt.Sprintf("L4 with LocalMode=%v", onlyLocal), func(t *testing.T) {
			l4c := newServiceController(t, newFakeGCE())
			prevMetrics := test.GetL4LatencyMetric(t)
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
				addNEG(l4c, newSvc)
				l4c.svcQueue.Enqueue(newSvc)
			}
			if err := retry.OnError(backoff, func(error) bool { return true }, func() error {
				for _, name := range svcNames {
					newSvc, err := l4c.client.CoreV1().Services(testNs).Get(context2.TODO(), name, v1.GetOptions{})
					if err != nil {
						return fmt.Errorf("Failed to lookup service %s, err: %v", name, err)
					}
					if len(newSvc.Status.LoadBalancer.Ingress) == 0 || newSvc.Annotations[annotations.FirewallRuleKey] == "" {
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
				validateSvcStatus(newSvc, true, t)
			}
			// this will be a create metric since an ILB IP is being assigned for the first time.
			prevMetrics.ValidateDiff(test.GetL4LatencyMetric(t), &test.L4ILBLatencyMetricInfo{CreateCount: 20, UpperBoundSeconds: 1}, t)

		})
	}
}

func TestProcessServiceWithDelayedNEGAdd(t *testing.T) {
	l4c := newServiceController(t, newFakeGCE())
	go l4c.Run()
	newSvc := test.NewL4ILBService(false, 8080)
	addILBService(l4c, newSvc)
	l4c.svcQueue.Enqueue(newSvc)

	if err := retry.OnError(retry.DefaultRetry, func(error) bool { return true }, func() error {
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
	addNEG(l4c, newSvc)

	var svcErr error
	backoff := retry.DefaultRetry
	// Increase the duration since the requeue time for failed events increases exponentially.
	backoff.Duration = 10 * time.Second
	if err := retry.OnError(backoff, func(error) bool { return true }, func() error {
		if newSvc, svcErr = l4c.client.CoreV1().Services(newSvc.Namespace).Get(context2.TODO(), newSvc.Name, v1.GetOptions{}); svcErr != nil {
			return fmt.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, svcErr)
		}
		// wait until an IP is assigned and resource annotations are available.
		if len(newSvc.Status.LoadBalancer.Ingress) > 0 && newSvc.Annotations[annotations.FirewallRuleKey] != "" {
			return nil
		}
		return fmt.Errorf("waiting for valid IP and/or resource annotations. Got Status - %+v, Annotations - %v", newSvc.Status, newSvc.Annotations)
	}); err != nil {
		t.Error(err)
	}
	validateSvcStatus(newSvc, true, t)
}

func TestProcessServiceOnError(t *testing.T) {
	l4c := newServiceController(t, newFakeGCEWithInsertError())
	prevMetrics := test.GetL4ILBErrorMetric(t)
	newSvc := test.NewL4ILBService(false, 8080)
	addILBService(l4c, newSvc)
	addNEG(l4c, newSvc)
	err := l4c.sync(getKeyForSvc(newSvc, t))
	if err == nil {
		t.Fatalf("Failed to generate error when syncing service %s", newSvc.Name)
	}
	expectMetrics := &test.L4ILBErrorMetricInfo{
		ByGCEResource: map[string]uint64{annotations.ForwardingRuleResource: 1},
		ByErrorType:   map[string]uint64{http.StatusText(http.StatusInternalServerError): 1}}
	prevMetrics.ValidateDiff(test.GetL4ILBErrorMetric(t), expectMetrics, t)
}
