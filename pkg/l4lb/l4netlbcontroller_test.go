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

package l4lb

import (
	"context"
	"fmt"
	"google.golang.org/api/googleapi"
	"math/rand"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	ga "google.golang.org/api/compute/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	ingctx "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/healthchecks"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	FwIPAddress          = "10.0.0.1"
	loadBalancerIP       = "10.0.0.10"
	usersIP              = "35.10.211.60"
	testServiceNamespace = "default"
	hcNodePort           = int32(10111)
	userAddrName         = "UserStaticAddress"
)

var (
	serviceAnnotationKeys = []string{
		annotations.FirewallRuleKey,
		annotations.BackendServiceKey,
		annotations.HealthcheckKey,
		annotations.TCPForwardingRuleKey,
		annotations.FirewallRuleForHealthcheckKey,
	}
)

func getExternalIPS() []string {
	return []string{"34.122.234.156", "34.122.234.157"}
}

func getLoadBalancerSourceRanges() []string {
	return []string{"169.13.0.0/20", "169.120.0.0/20"}
}

func getPorts() []v1.ServicePort {
	return []v1.ServicePort{
		{Name: "port1", Port: 8084, Protocol: "TCP", NodePort: 30323},
		{Name: "port2", Port: 8082, Protocol: "TCP", NodePort: 30323}}
}

func getSessionAffinityConfig() *v1.SessionAffinityConfig {
	timeoutSec := int32(10)
	return &v1.SessionAffinityConfig{ClientIP: &v1.ClientIPConfig{TimeoutSeconds: &timeoutSec}}
}

func getAnnotations() map[string]string {
	ann := make(map[string]string, 1)
	ann["some_new_annotation"] = "new_value"
	return ann
}

func addNetLBService(l4netController *L4NetLBController, svc *v1.Service) {
	l4netController.ctx.KubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
	l4netController.ctx.ServiceInformer.GetIndexer().Add(svc)
}

func updateNetLBService(lc *L4NetLBController, svc *v1.Service) {
	lc.ctx.KubeClient.CoreV1().Services(svc.Namespace).Update(context.TODO(), svc, metav1.UpdateOptions{})
	lc.ctx.ServiceInformer.GetIndexer().Update(svc)
}

func deleteNetLBService(lc *L4NetLBController, svc *v1.Service) {
	lc.ctx.KubeClient.CoreV1().Services(svc.Namespace).Delete(context.TODO(), svc.Name, metav1.DeleteOptions{})
	lc.ctx.ServiceInformer.GetIndexer().Delete(svc)
}

func checkForwardingRule(lc *L4NetLBController, svc *v1.Service, expectedPortRange string) error {
	if len(svc.Spec.Ports) == 0 {
		return fmt.Errorf("There are no ports in service!")
	}
	frName := utils.LegacyForwardingRuleName(svc)
	fwdRule, err := composite.GetForwardingRule(lc.ctx.Cloud, meta.RegionalKey(frName, lc.ctx.Cloud.Region()), meta.VersionGA)
	if err != nil {
		return fmt.Errorf("Error getting forwarding rule: %v", err)
	}
	if fwdRule.PortRange != expectedPortRange {
		return fmt.Errorf("Port Range Mismatch %v != %v", expectedPortRange, fwdRule.PortRange)
	}
	return nil
}

func createAndSyncNetLBSvc(t *testing.T) (svc *v1.Service, lc *L4NetLBController) {
	lc = newL4NetLBServiceController()
	svc = test.NewL4NetLBRBSService(8080)
	addNetLBService(lc, svc)
	key, _ := common.KeyFunc(svc)
	err := lc.sync(key)
	if err != nil {
		t.Errorf("Failed to sync newly added service %s, err %v", svc.Name, err)
	}
	svc, err = lc.ctx.KubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err %v", svc.Name, err)
	}
	validateNetLBSvcStatus(svc, t)
	return
}

func createAndSyncLegacyNetLBSvc(t *testing.T) (svc *v1.Service, lc *L4NetLBController) {
	lc = newL4NetLBServiceController()
	svc = test.NewL4LegacyNetLBService(8080, 30234)
	addNetLBService(lc, svc)
	svc, err := lc.ctx.KubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err %v", svc.Name, err)
	}
	return
}

func checkBackendService(lc *L4NetLBController, svc *v1.Service) error {

	backendServiceLink, bs, err := getBackend(lc, svc)
	if err != nil {
		return fmt.Errorf("Failed to fetch backend service, err %v", err)
	}
	if bs.SelfLink != backendServiceLink {
		return fmt.Errorf("Backend Service SelfLink mismatch: %s != %s", bs.SelfLink, backendServiceLink)
	}
	if bs.LoadBalancingScheme != string(cloud.SchemeExternal) {
		return fmt.Errorf("Load Balancing Scheme mismatch: EXTERNAL != %s", bs.LoadBalancingScheme)
	}
	if len(bs.Backends) == 0 {
		return fmt.Errorf("Error no backends in BackendService")
	}
	igName := lc.namer.InstanceGroup()
	for _, b := range bs.Backends {
		if !strings.Contains(b.Group, igName) {
			return fmt.Errorf("Backend Ingstance Group Link mismatch: %s != %s", igName, b.Group)
		}
	}
	ig, err := lc.ctx.Cloud.GetInstanceGroup(igName, testGCEZone)
	if err != nil {
		return fmt.Errorf("Error getting Instance Group, err %v", err)
	}
	if ig == nil {
		return fmt.Errorf("Instance Group does not exist")
	}
	return nil
}

func updateRegionBackendServiceWithLockHook(ctx context.Context, key *meta.Key, obj *ga.BackendService, m *cloud.MockRegionBackendServices) error {
	_, err := m.Get(ctx, key)
	if err != nil {
		return err
	}
	obj.Name = key.Name
	projectID := m.ProjectRouter.ProjectID(ctx, "ga", "backendServices")
	obj.SelfLink = cloud.SelfLink(meta.VersionGA, projectID, "backendServices", key)

	m.Lock.Lock()
	defer m.Lock.Unlock()
	m.Objects[*key] = &cloud.MockRegionBackendServicesObj{Obj: obj}
	return nil
}

func getBackend(l4netController *L4NetLBController, svc *v1.Service) (string, *composite.BackendService, error) {
	backendServiceName, _ := l4netController.namer.L4Backend(svc.Namespace, svc.Name)
	key := meta.RegionalKey(backendServiceName, l4netController.ctx.Cloud.Region())
	backendServiceLink := cloud.SelfLink(meta.VersionGA, l4netController.ctx.Cloud.ProjectID(), "backendServices", key)
	bs, err := composite.GetBackendService(l4netController.ctx.Cloud, key, meta.VersionGA)
	return backendServiceLink, bs, err
}

func getFakeGCECloud(vals gce.TestClusterValues) *gce.Cloud {
	fakeGCE := gce.NewFakeGCECloud(vals)
	(fakeGCE.Compute().(*cloud.MockGCE)).MockForwardingRules.InsertHook = loadbalancers.InsertForwardingRuleHook
	(fakeGCE.Compute().(*cloud.MockGCE)).MockRegionBackendServices.UpdateHook = mock.UpdateRegionBackendServiceHook
	return fakeGCE
}

func buildContext(vals gce.TestClusterValues) *ingctx.ControllerContext {
	fakeGCE := getFakeGCECloud(vals)
	kubeClient := fake.NewSimpleClientset()
	namer := namer.NewNamer(clusterUID, "")

	ctxConfig := ingctx.ControllerContextConfig{
		Namespace:    v1.NamespaceAll,
		ResyncPeriod: 1 * time.Minute,
		NumL4Workers: 5,
	}
	return ingctx.NewControllerContext(nil, kubeClient, nil, nil, nil, nil, nil, fakeGCE, namer, "" /*kubeSystemUID*/, ctxConfig)
}

func newL4NetLBServiceController() *L4NetLBController {
	stopCh := make(chan struct{})
	vals := gce.DefaultTestClusterValues()
	ctx := buildContext(vals)
	nodes, err := test.CreateAndInsertNodes(ctx.Cloud, []string{"instance-1", "instance-2"}, vals.ZoneName)
	if err != nil {
		klog.Fatalf("Failed to add new nodes, err %v", err)
	}
	for _, n := range nodes {
		ctx.NodeInformer.GetIndexer().Add(n)
	}

	return NewL4NetLBController(ctx, stopCh)
}

func validateNetLBSvcStatus(svc *v1.Service, t *testing.T) {
	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP != FwIPAddress {
		t.Fatalf("Invalid LoadBalancer status field in service - %+v", svc.Status.LoadBalancer)
	}
}

func validateAnnotations(svc *v1.Service) error {
	missingKeys := []string{}
	for _, key := range serviceAnnotationKeys {
		if _, ok := svc.Annotations[key]; !ok {
			missingKeys = append(missingKeys, key)
		}
	}
	if len(missingKeys) > 0 {
		return fmt.Errorf("Cannot find annotations %v in ELB service, Got %v", missingKeys, svc.Annotations)
	}
	return nil
}

func validateAnnotationsDeleted(svc *v1.Service) error {
	unexpectedKeys := []string{}
	for _, key := range serviceAnnotationKeys {
		if _, exists := svc.Annotations[key]; exists {
			unexpectedKeys = append(unexpectedKeys, key)
		}
	}
	if len(unexpectedKeys) != 0 {
		return fmt.Errorf("Unexpeceted annotations: %v, Service annotations %v", unexpectedKeys, svc.Annotations)
	}
	return nil
}

func TestProcessMultipleNetLBServices(t *testing.T) {
	backoff := retry.DefaultRetry
	backoff.Duration = 1 * time.Second
	for _, onlyLocal := range []bool{true, false} {
		t.Run(fmt.Sprintf("L4 with LocalMode=%v", onlyLocal), func(t *testing.T) {
			lc := newL4NetLBServiceController()
			(lc.ctx.Cloud.Compute().(*cloud.MockGCE)).MockRegionBackendServices.UpdateHook = updateRegionBackendServiceWithLockHook
			go lc.Run()
			var svcNames []string
			for port := 8000; port < 8020; port++ {
				newSvc := test.NewL4NetLBRBSService(port)
				newSvc.Name = newSvc.Name + fmt.Sprintf("-%d", port)
				newSvc.UID = types.UID(newSvc.Name)
				svcNames = append(svcNames, newSvc.Name)
				addNetLBService(lc, newSvc)
				lc.svcQueue.Enqueue(newSvc)
			}
			if err := retry.OnError(backoff, func(error) bool { return true }, func() error {
				for _, name := range svcNames {
					newSvc, err := lc.ctx.KubeClient.CoreV1().Services(testServiceNamespace).Get(context.TODO(), name, metav1.GetOptions{})
					if err != nil {
						return fmt.Errorf("Failed to lookup service %s, err: %v", name, err)
					}
					if len(newSvc.Status.LoadBalancer.Ingress) == 0 {
						return fmt.Errorf("Waiting for valid IP for service %q. Got Status - %+v", newSvc.Name, newSvc.Status)
					}
				}
				return nil
			}); err != nil {
				t.Error(err)
			}
			// Perform a full validation of the service once it is ready.
			for _, name := range svcNames {
				svc, _ := lc.ctx.KubeClient.CoreV1().Services(testServiceNamespace).Get(context.TODO(), name, metav1.GetOptions{})
				validateNetLBSvcStatus(svc, t)
				if err := checkBackendService(lc, svc); err != nil {
					t.Errorf("Check backend service err: %v", err)
				}
				if err := validateAnnotations(svc); err != nil {
					t.Errorf("%v", err)
				}
				expectedPortRange := fmt.Sprintf("%d-%d", svc.Spec.Ports[0].Port, svc.Spec.Ports[0].Port)
				if err := checkForwardingRule(lc, svc, expectedPortRange); err != nil {
					t.Errorf("Check forwarding rule error: %v", err)
				}
				deleteNetLBService(lc, svc)
			}

		})
	}
}

func TestForwardingRuleWithPortRange(t *testing.T) {
	lc := newL4NetLBServiceController()
	for _, tc := range []struct {
		svcName           string
		ports             []int32
		expectedPortRange string
	}{
		{
			svcName:           "SvcContinuousRange",
			ports:             []int32{80, 123, 8080},
			expectedPortRange: "80-8080",
		},
		{
			svcName:           "SinglePort",
			ports:             []int32{80},
			expectedPortRange: "80-80",
		},
		{
			svcName:           "PortsDescending",
			ports:             []int32{8081, 8080, 123},
			expectedPortRange: "123-8081",
		},
		{
			svcName:           "PortsMixedOrder",
			ports:             []int32{8081, 80, 8080, 123},
			expectedPortRange: "80-8081",
		},
	} {
		svc := test.NewL4NetLBRBSServiceMultiplePorts(tc.svcName, tc.ports)
		svc.UID = types.UID(svc.Name + fmt.Sprintf("-%d", rand.Intn(1001)))
		addNetLBService(lc, svc)
		key, _ := common.KeyFunc(svc)
		if err := lc.sync(key); err != nil {
			t.Errorf("Failed to sync service %s, err: %v", key, err)
		}

		newSvc, err := lc.ctx.KubeClient.CoreV1().Services(testServiceNamespace).Get(context.TODO(), tc.svcName, metav1.GetOptions{})
		if err != nil {
			t.Errorf("Failed to lookup service %s, err: %v", tc.svcName, err)
		}
		if len(newSvc.Status.LoadBalancer.Ingress) == 0 {
			t.Errorf("Waiting for valid IP for service %q. Got Status - %+v", tc.svcName, newSvc.Status)
		}

		if err := checkBackendService(lc, svc); err != nil {
			t.Errorf("Check backend service err: %v", err)
		}
		if err := checkForwardingRule(lc, newSvc, tc.expectedPortRange); err != nil {
			t.Errorf("Check forwarding rule error: %v", err)
		}
		deleteNetLBService(lc, svc)
	}
}

func TestProcessServiceCreate(t *testing.T) {
	lc := newL4NetLBServiceController()
	svc := test.NewL4NetLBRBSService(8080)
	addNetLBService(lc, svc)
	prevMetrics, err := test.GetL4NetLBLatencyMetric()
	if err != nil {
		t.Errorf("Error getting L4 NetLB latency metrics err: %v", err)
	}
	if prevMetrics == nil {
		t.Fatalf("Cannot get prometheus metrics for L4NetLB latency")
	}
	key, _ := common.KeyFunc(svc)
	err = lc.sync(key)
	if err != nil {
		t.Errorf("Failed to sync newly added service %s, err %v", svc.Name, err)
	}
	svc, err = lc.ctx.KubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err %v", svc.Name, err)
	}
	currMetrics, metricErr := test.GetL4NetLBLatencyMetric()
	if metricErr != nil {
		t.Errorf("Error getting L4 NetLB latency metrics err: %v", metricErr)
	}
	prevMetrics.ValidateDiff(currMetrics, &test.L4LBLatencyMetricInfo{CreateCount: 1, UpperBoundSeconds: 1}, t)

	validateNetLBSvcStatus(svc, t)
	if err := checkBackendService(lc, svc); err != nil {
		t.Errorf("UnexpectedError %v", err)
	}
	if err := validateAnnotations(svc); err != nil {
		t.Errorf("%v", err)
	}
	deleteNetLBService(lc, svc)
}

func TestProcessServiceCreateWithUsersProvidedIP(t *testing.T) {
	lc := newL4NetLBServiceController()

	lc.ctx.Cloud.Compute().(*cloud.MockGCE).MockAddresses.InsertHook = test.InsertAddressErrorHook
	svc := test.NewL4NetLBRBSService(8080)
	svc.Spec.LoadBalancerIP = usersIP
	addNetLBService(lc, svc)
	key, _ := common.KeyFunc(svc)
	if err := lc.sync(key); err == nil {
		t.Errorf("Expected sync error when address reservation fails.")
	}
	addUsersStaticAddress(lc, cloud.NetworkTierDefault)
	if err := lc.sync(key); err != nil {
		t.Errorf("Un expected Error when trying to sync service with user's address, err: %v", err)
	}
	svc, err := lc.ctx.KubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err %v", svc.Name, err)
	}
	if len(svc.Status.LoadBalancer.Ingress) == 0 {
		t.Fatalf("Invalid LoadBalancer Status %+v", svc.Status.LoadBalancer.Ingress)
	}
	if svc.Status.LoadBalancer.Ingress[0].IP != usersIP {
		t.Fatalf("Invalid LoadBalancer IP Address %v should be %s ", svc.Status.LoadBalancer.Ingress[0].IP, usersIP)
	}
	// Mark the service for deletion by updating timestamp
	svc.DeletionTimestamp = &metav1.Time{}
	updateNetLBService(lc, svc)
	if err := lc.sync(key); err != nil {
		t.Errorf("Unexpected Error when trying to sync service after deletion, err: %v", err)
	}
	adr, err := lc.ctx.Cloud.GetRegionAddress(userAddrName, lc.ctx.Cloud.Region())
	if err != nil {
		t.Errorf("Unexpected error when trying to get regiona address, err: %v", err)
	}
	if adr == nil {
		t.Errorf("Address should not be deleted after service deletion")
	}
	deleteNetLBService(lc, svc)
}

func addUsersStaticAddress(lc *L4NetLBController, netTier cloud.NetworkTier) {
	lc.ctx.Cloud.Compute().(*cloud.MockGCE).MockAddresses.InsertHook = mock.InsertAddressHook
	lc.ctx.Cloud.Compute().(*cloud.MockGCE).MockAlphaAddresses.X = mock.AddressAttributes{}
	lc.ctx.Cloud.Compute().(*cloud.MockGCE).MockAddresses.X = mock.AddressAttributes{}
	newAddr := &ga.Address{
		Name:        userAddrName,
		Description: fmt.Sprintf(`{"kubernetes.io/service-name":"%s"}`, userAddrName),
		Address:     usersIP,
		AddressType: string(cloud.SchemeExternal),
		NetworkTier: netTier.ToGCEValue(),
	}
	lc.ctx.Cloud.ReserveRegionAddress(newAddr, lc.ctx.Cloud.Region())
}

func TestProcessServiceDeletion(t *testing.T) {
	svc, lc := createAndSyncNetLBSvc(t)

	if !common.HasGivenFinalizer(svc.ObjectMeta, common.NetLBFinalizerV2) {
		t.Errorf("Expected L4 External LoadBalancer finalizer")
	}
	if lc.needsDeletion(svc) {
		t.Errorf("Service should not be marked for deletion")
	}
	// Mark the service for deletion by updating timestamp
	svc.DeletionTimestamp = &metav1.Time{}
	updateNetLBService(lc, svc)
	if !lc.needsDeletion(svc) {
		t.Errorf("Service should be marked for deletion")
	}
	key, _ := common.KeyFunc(svc)
	err := lc.sync(key)
	if err != nil {
		t.Errorf("Failed to sync service %s, err %v", svc.Name, err)
	}
	svc, err = lc.ctx.KubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err %v", svc.Name, err)
	}
	if len(svc.Status.LoadBalancer.Ingress) > 0 {
		t.Errorf("Expected LoadBalancer status be deleted - %+v", svc.Status.LoadBalancer)
	}
	if common.HasGivenFinalizer(svc.ObjectMeta, common.NetLBFinalizerV2) {
		t.Errorf("Unexpected LoadBalancer finalizer %v", svc.ObjectMeta.Finalizers)
	}

	if err = validateAnnotationsDeleted(svc); err != nil {
		t.Errorf("RBS Service annotations have NOT been deleted. Error: %v", err)
	}

	igName := lc.namer.InstanceGroup()
	_, err = lc.ctx.Cloud.GetInstanceGroup(igName, testGCEZone)
	if !utils.IsNotFoundError(err) {
		t.Errorf("Failed to delete Instance Group %v, err: %v", igName, err)
	}

	deleteNetLBService(lc, svc)
}

func TestServiceDeletionWhenInstanceGroupInUse(t *testing.T) {
	svc, lc := createAndSyncNetLBSvc(t)

	(lc.ctx.Cloud.Compute().(*cloud.MockGCE)).MockInstanceGroups.DeleteHook = func(ctx context.Context, key *meta.Key, m *cloud.MockInstanceGroups) (bool, error) {
		err := &googleapi.Error{
			Code:    http.StatusBadRequest,
			Message: "GetErrorInstanceGroupHook: Cannot delete instance group being used by another service",
		}
		return true, err
	}

	svc.DeletionTimestamp = &metav1.Time{}
	updateNetLBService(lc, svc)
	key, _ := common.KeyFunc(svc)
	err := lc.sync(key)
	if err != nil {
		t.Errorf("Failed to sync service %s, err %v", svc.Name, err)
	}
	svc, err = lc.ctx.KubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err %v", svc.Name, err)
	}
	if len(svc.Status.LoadBalancer.Ingress) > 0 {
		t.Errorf("Expected LoadBalancer status be deleted - %+v", svc.Status.LoadBalancer)
	}
	if common.HasGivenFinalizer(svc.ObjectMeta, common.NetLBFinalizerV2) {
		t.Errorf("Unexpected LoadBalancer finalizer %v", svc.ObjectMeta.Finalizers)
	}

	if err = validateAnnotationsDeleted(svc); err != nil {
		t.Errorf("RBS Service annotations have NOT been deleted. Error: %v", err)
	}

	igName := lc.namer.InstanceGroup()
	_, err = lc.ctx.Cloud.GetInstanceGroup(igName, testGCEZone)
	if err != nil {
		t.Errorf("Failed to get Instance Group named %v. Group should be present. Error: %v", igName, err)
	}
}

func TestInternalLoadBalancerShouldNotBeProcessByL4NetLBController(t *testing.T) {
	lc := newL4NetLBServiceController()
	ilbSvc := test.NewL4ILBService(false, 8080)
	addNetLBService(lc, ilbSvc)
	key, _ := common.KeyFunc(ilbSvc)
	err := lc.sync(key)
	if err != nil {
		t.Errorf("Failed to sync service %s, err %v", ilbSvc.Name, err)
	}
	ilbSvc, err = lc.ctx.KubeClient.CoreV1().Services(ilbSvc.Namespace).Get(context.TODO(), ilbSvc.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err %v", ilbSvc.Name, err)
	}

	// Mark the service for deletion by updating timestamp
	ilbSvc.DeletionTimestamp = &metav1.Time{}
	updateNetLBService(lc, ilbSvc)
	if lc.needsDeletion(ilbSvc) {
		t.Fatalf("Service should not be marked for deletion!")
	}
}

func TestProcessServiceCreationFailed(t *testing.T) {
	for _, param := range []struct {
		addMockFunc   func(*cloud.MockGCE)
		expectedError string
	}{{addMockFunc: func(c *cloud.MockGCE) { c.MockInstanceGroups.GetHook = test.GetErrorInstanceGroupHook },
		expectedError: "GetErrorInstanceGroupHook"},
		{addMockFunc: func(c *cloud.MockGCE) { c.MockInstanceGroups.ListHook = test.ListErrorHook },
			expectedError: "ListErrorHook"},
		{addMockFunc: func(c *cloud.MockGCE) { c.MockInstanceGroups.InsertHook = test.InsertErrorHook },
			expectedError: "InsertErrorHook"},

		{addMockFunc: func(c *cloud.MockGCE) { c.MockInstanceGroups.AddInstancesHook = test.AddInstancesErrorHook },
			expectedError: "AddInstances: [AddInstancesErrorHook]"},
		{addMockFunc: func(c *cloud.MockGCE) { c.MockInstanceGroups.ListInstancesHook = test.ListInstancesWithErrorHook },
			expectedError: "ListInstancesWithErrorHook"},
		{addMockFunc: func(c *cloud.MockGCE) { c.MockInstanceGroups.SetNamedPortsHook = test.SetNamedPortsErrorHook },
			expectedError: "SetNamedPortsErrorHook"},
	} {
		lc := newL4NetLBServiceController()
		param.addMockFunc((lc.ctx.Cloud.Compute().(*cloud.MockGCE)))
		svc := test.NewL4NetLBRBSService(8080)
		addNetLBService(lc, svc)
		key, _ := common.KeyFunc(svc)
		err := lc.sync(key)
		if err == nil || err.Error() != param.expectedError {
			t.Errorf("Error mismatch '%v' != '%v'", err.Error(), param.expectedError)
		}
	}
}

func TestMetricsWithSyncError(t *testing.T) {
	lc := newL4NetLBServiceController()
	(lc.ctx.Cloud.Compute().(*cloud.MockGCE)).MockForwardingRules.InsertHook = mock.InsertForwardingRulesInternalErrHook
	prevMetrics, err := test.GetL4NetLBErrorMetric()
	if err != nil {
		t.Errorf("Error getting L4 NetLB error metrics err: %v", err)
	}
	svc := test.NewL4NetLBRBSService(8080)
	addNetLBService(lc, svc)

	key, _ := common.KeyFunc(svc)
	err = lc.sync(key)
	if err == nil {
		t.Errorf("Expected error in sync controller")
	}
	expectMetrics := &test.L4LBErrorMetricInfo{
		ByGCEResource: map[string]uint64{annotations.ForwardingRuleResource: 1},
		ByErrorType:   map[string]uint64{http.StatusText(http.StatusInternalServerError): 1}}
	received, errMetrics := test.GetL4NetLBErrorMetric()
	if errMetrics != nil {
		t.Errorf("Error getting L4 NetLB error metrics err: %v", errMetrics)
	}
	prevMetrics.ValidateDiff(received, expectMetrics, t)
}

func TestProcessServiceDeletionFailed(t *testing.T) {
	for _, param := range []struct {
		addMockFunc   func(*cloud.MockGCE)
		expectedError string
	}{
		{addMockFunc: func(c *cloud.MockGCE) { c.MockForwardingRules.DeleteHook = test.DeleteForwardingRulesErrorHook },
			expectedError: "DeleteForwardingRulesErrorHook"},
		{addMockFunc: func(c *cloud.MockGCE) { c.MockAddresses.DeleteHook = test.DeleteAddressErrorHook },
			expectedError: "DeleteAddressErrorHook"},
		{addMockFunc: func(c *cloud.MockGCE) { c.MockFirewalls.DeleteHook = test.DeleteFirewallsErrorHook },
			expectedError: "DeleteFirewallsErrorHook"},
		{addMockFunc: func(c *cloud.MockGCE) { c.MockRegionBackendServices.DeleteHook = test.DeleteBackendServicesErrorHook },
			expectedError: "DeleteBackendServicesErrorHook"},
		{addMockFunc: func(c *cloud.MockGCE) { c.MockRegionHealthChecks.DeleteHook = test.DeleteHealthCheckErrorHook },
			expectedError: "DeleteHealthCheckErrorHook"},
	} {
		svc, lc := createAndSyncNetLBSvc(t)
		if !common.HasGivenFinalizer(svc.ObjectMeta, common.NetLBFinalizerV2) {
			t.Fatalf("Expected L4 External LoadBalancer finalizer")
		}
		svc.DeletionTimestamp = &metav1.Time{}
		updateNetLBService(lc, svc)
		if !lc.needsDeletion(svc) {
			t.Fatalf("Service should be marked for deletion")
		}
		param.addMockFunc((lc.ctx.Cloud.Compute().(*cloud.MockGCE)))
		key, _ := common.KeyFunc(svc)
		err := lc.sync(key)
		if err == nil || err.Error() != param.expectedError {
			t.Errorf("Error mismatch '%v' != '%v'", err, param.expectedError)
		}
	}
}

func TestProcessServiceUpdate(t *testing.T) {
	for _, param := range []struct {
		Update      func(*v1.Service)
		CheckResult func(*L4NetLBController, *v1.Service) error
	}{
		{
			Update: func(s *v1.Service) { s.Spec.SessionAffinity = v1.ServiceAffinityNone },
			CheckResult: func(l4netController *L4NetLBController, svc *v1.Service) error {
				_, bs, err := getBackend(l4netController, svc)
				if err != nil {
					return fmt.Errorf("Failed to fetch backend service: %v", err)
				}
				if bs.SessionAffinity != utils.TranslateAffinityType(string(v1.ServiceAffinityNone)) {
					return fmt.Errorf("SessionAffinity mismatch %v != %v", bs.SessionAffinity, v1.ServiceAffinityNone)
				}
				return nil
			},
		},
		{
			Update: func(s *v1.Service) { s.Spec.LoadBalancerSourceRanges = getLoadBalancerSourceRanges() },
			CheckResult: func(l4netController *L4NetLBController, svc *v1.Service) error {
				if len(svc.Spec.Ports) == 0 {
					return fmt.Errorf("No Ports in service")
				}
				name, _ := (l4netController.namer.(namer.BackendNamer)).L4Backend(svc.Namespace, svc.Name)
				fw, err := l4netController.ctx.Cloud.GetFirewall(name)
				if err != nil {
					return fmt.Errorf("Failed to fetch firewall service: %v", err)
				}
				expectedRange := getLoadBalancerSourceRanges()
				sort.Strings(expectedRange)
				sort.Strings(fw.SourceRanges)
				if !reflect.DeepEqual(fw.SourceRanges, expectedRange) {
					return fmt.Errorf("SourceRanges mismatch: %v != %v", fw.SourceRanges, expectedRange)
				}
				return nil
			},
		},
		{
			Update: func(s *v1.Service) { s.Spec.SessionAffinityConfig = getSessionAffinityConfig() },
			CheckResult: func(l4netController *L4NetLBController, svc *v1.Service) error {
				if !reflect.DeepEqual(svc.Spec.SessionAffinityConfig, getSessionAffinityConfig()) {
					return fmt.Errorf("SessionAffinityConfig mismatch %v != %v", svc.Spec.SessionAffinityConfig, getSessionAffinityConfig())
				}
				return nil
			},
		},
		{
			Update: func(s *v1.Service) { s.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal },
			CheckResult: func(l4netController *L4NetLBController, svc *v1.Service) error {
				if svc.Spec.ExternalTrafficPolicy != v1.ServiceExternalTrafficPolicyTypeLocal {
					return fmt.Errorf("ExternalTrafficPolicy mismatch %v != %v", svc.Spec.ExternalTrafficPolicy, v1.ServiceExternalTrafficPolicyTypeLocal)
				}
				return nil
			},
		},
		{
			Update: func(s *v1.Service) { s.Spec.LoadBalancerIP = loadBalancerIP },
			CheckResult: func(lc *L4NetLBController, svc *v1.Service) error {
				frName := utils.LegacyForwardingRuleName(svc)
				fwdRule, err := composite.GetForwardingRule(lc.ctx.Cloud, meta.RegionalKey(frName, lc.ctx.Cloud.Region()), meta.VersionGA)
				if err != nil {
					return fmt.Errorf("Error getting forwarding rule %v", err)
				}
				if fwdRule == nil {
					return fmt.Errorf("Fr rule is nil")
				}
				if fwdRule.IPAddress != loadBalancerIP {
					return fmt.Errorf("LoadBalancerIP mismatch %v != %v", fwdRule.IPAddress, loadBalancerIP)
				}
				return nil
			},
		},
		{
			Update: func(s *v1.Service) { s.Spec.ExternalIPs = getExternalIPS() },
			CheckResult: func(l4netController *L4NetLBController, svc *v1.Service) error {
				expectedIPs := getExternalIPS()
				for i := range expectedIPs {
					if svc.Spec.ExternalIPs[i] != expectedIPs[i] {
						return fmt.Errorf("ExternalIPs mismatch %v != %v", svc.Spec.ExternalIPs, expectedIPs)
					}
				}
				return nil
			},
		},
		{
			Update: func(s *v1.Service) { s.Spec.Ports = getPorts() },
			CheckResult: func(l4netController *L4NetLBController, svc *v1.Service) error {
				expectedPorts := getPorts()
				for i := range expectedPorts {
					if svc.Spec.Ports[i] != expectedPorts[i] {
						return fmt.Errorf("Ports mismatch %v != %v", svc.Spec.Ports, expectedPorts)
					}
				}
				return nil
			},
		},
		{
			Update: func(s *v1.Service) { s.Spec.HealthCheckNodePort = hcNodePort },
			CheckResult: func(l4netController *L4NetLBController, svc *v1.Service) error {
				if svc.Spec.HealthCheckNodePort != hcNodePort {
					return fmt.Errorf("HealthCheckNodePort mismatch %v != %v", svc.Spec.HealthCheckNodePort, hcNodePort)
				}
				return nil
			},
		},
		{
			Update: func(s *v1.Service) { s.Annotations = getAnnotations() },
			CheckResult: func(l4netController *L4NetLBController, svc *v1.Service) error {
				expAddedAnnotations := getAnnotations()
				for name, value := range expAddedAnnotations {
					if svc.Annotations[name] != value {
						return fmt.Errorf("Annotation mismatch %v != %v", svc.Annotations[name], value)
					}
				}
				return nil
			},
		},
	} {
		svc, l4netController := createAndSyncNetLBSvc(t)
		(l4netController.ctx.Cloud.Compute().(*cloud.MockGCE)).MockFirewalls.UpdateHook = mock.UpdateFirewallHook
		(l4netController.ctx.Cloud.Compute().(*cloud.MockGCE)).MockRegionBackendServices.UpdateHook = mock.UpdateRegionBackendServiceHook
		newSvc, err := l4netController.ctx.KubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
		if err != nil {
			t.Errorf("Failed to lookup service %s, err: %v", svc.Name, err)
		}

		param.Update(newSvc)
		updateNetLBService(l4netController, newSvc)

		if !l4netController.needsUpdate(svc, newSvc) {
			t.Errorf("Service should be marked for update")
		}

		key, _ := common.KeyFunc(newSvc)
		err = l4netController.sync(key)
		if err != nil {
			t.Errorf("Failed to sync newly added service %s, err %v", svc.Name, err)
		}
		newSvc, err = l4netController.ctx.KubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
		if err != nil {
			t.Errorf("Failed to lookup service %s, err: %v", svc.Name, err)
		}

		if err = param.CheckResult(l4netController, newSvc); err != nil {
			t.Errorf("Error Checking Update: %v", err)
		}
		deleteNetLBService(l4netController, svc)
	}

}

func TestHealthCheckWhenExternalTrafficPolicyWasUpdated(t *testing.T) {
	svc, lc := createAndSyncNetLBSvc(t)
	newSvc, err := lc.ctx.KubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", svc.Name, err)
	}

	// Update ExternalTrafficPolicy to Local check if nonshared HC was created
	hcNameNonShared, _ := lc.namer.L4HealthCheck(svc.Namespace, svc.Name, false)
	err = updateAndAssertExternalTrafficPolicy(newSvc, lc, v1.ServiceExternalTrafficPolicyTypeLocal, hcNameNonShared)
	if err != nil {
		t.Errorf("Error asserthing nonshared health check %v", err)
	}
	// delete shared health check if is created, update service to Cluster and
	// check that non-shared health check was created
	hcNameShared, _ := lc.namer.L4HealthCheck(svc.Namespace, svc.Name, true)
	healthchecks.DeleteHealthCheck(lc.ctx.Cloud, hcNameShared, meta.Regional)
	// Update ExternalTrafficPolicy to Cluster check if shared HC was created
	err = updateAndAssertExternalTrafficPolicy(newSvc, lc, v1.ServiceExternalTrafficPolicyTypeCluster, hcNameShared)
	if err != nil {
		t.Errorf("Error asserthing shared health check %v", err)
	}
	newSvc.DeletionTimestamp = &metav1.Time{}
	updateNetLBService(lc, newSvc)
	key, _ := common.KeyFunc(newSvc)
	if err = lc.sync(key); err != nil {
		t.Errorf("Failed to sync deleted service %s, err %v", key, err)
	}
	if !isHealthCheckDeleted(lc.ctx.Cloud, hcNameNonShared) {
		t.Errorf("Health check %s should be deleted", hcNameNonShared)
	}
	if !isHealthCheckDeleted(lc.ctx.Cloud, hcNameShared) {
		t.Errorf("Health check %s should be deleted", hcNameShared)
	}
	deleteNetLBService(lc, svc)
}

func updateAndAssertExternalTrafficPolicy(newSvc *v1.Service, lc *L4NetLBController, newPolicy v1.ServiceExternalTrafficPolicyType, hcName string) error {
	newSvc.Spec.ExternalTrafficPolicy = newPolicy
	updateNetLBService(lc, newSvc)
	key, _ := common.KeyFunc(newSvc)
	err := lc.sync(key)
	if err != nil {
		return fmt.Errorf("Failed to sync updated service %s, err %v", key, err)
	}
	newSvc, err = lc.ctx.KubeClient.CoreV1().Services(newSvc.Namespace).Get(context.TODO(), newSvc.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	_, err = composite.GetHealthCheck(lc.ctx.Cloud, meta.RegionalKey(hcName, lc.ctx.Cloud.Region()), meta.VersionGA)
	if err != nil {
		return fmt.Errorf("Error getting health check %v", err)
	}
	return nil
}

func isWrongNetworkTierError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "does not have the expected network tier")
}

func TestControllerUserIPWithStandardNetworkTier(t *testing.T) {
	// Network Tier from User Static Address should match network tier from forwarding rule.
	// Premium Network Tier is default for creating forwarding rule so if User wants to use Standard Network Tier for Static Address
	// they should include network tier annotation in Service.

	lc := newL4NetLBServiceController()

	svc := test.NewL4NetLBRBSService(8080)
	svc.Spec.LoadBalancerIP = usersIP
	addNetLBService(lc, svc)
	key, _ := common.KeyFunc(svc)
	addUsersStaticAddress(lc, cloud.NetworkTierStandard)
	// Sync should return error that Network Tier mismatch because we cannot tear User Managed Address.
	if err := lc.sync(key); !isWrongNetworkTierError(err) {
		t.Errorf("Expected error when trying to ensure service with wrong Network Tier, err: %v", err)
	}
	svc.Annotations[annotations.NetworkTierAnnotationKey] = string(cloud.NetworkTierStandard)
	updateNetLBService(lc, svc)
	if err := lc.sync(key); err != nil {
		t.Errorf("Unexpected error when trying to ensure service with STANDARD Network Tier, err: %v", err)
	}
}

type getForwardingRuleHook func(ctx context.Context, key *meta.Key, m *cloud.MockForwardingRules) (bool, *ga.ForwardingRule, error)

func TestIsRBSBasedService(t *testing.T) {
	testCases := []struct {
		desc             string
		finalizers       []string
		annotations      map[string]string
		frHook           getForwardingRuleHook
		expectRBSService bool
	}{
		{
			desc:             "Service without finalizers, annotations and forwarding rule should not be marked as RBS",
			expectRBSService: false,
		},
		{
			desc:             "Legacy service should not be marked as RBS",
			finalizers:       []string{helpers.LoadBalancerCleanupFinalizer},
			expectRBSService: false,
		},
		{
			desc:             "Should detect RBS by finalizer",
			finalizers:       []string{common.NetLBFinalizerV2},
			expectRBSService: true,
		},
		{
			desc:             "Should detect RBS by finalizer when service contains both legacy and NetLB finalizers",
			finalizers:       []string{helpers.LoadBalancerCleanupFinalizer, common.NetLBFinalizerV2},
			expectRBSService: true,
		},
		{
			desc:             "Should detect RBS by annotation",
			annotations:      map[string]string{annotations.RBSAnnotationKey: annotations.RBSEnabled},
			expectRBSService: true,
		},
		{
			desc:             "Should detect RBS by forwarding rule",
			frHook:           test.GetRBSForwardingRule,
			expectRBSService: true,
		},
		{
			desc:             "Should not detect RBS by forwarding rule pointed to target pool",
			frHook:           test.GetLegacyForwardingRule,
			expectRBSService: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.desc, func(t *testing.T) {
			// Setup
			svc := test.NewL4LegacyNetLBService(8080, 30234)
			controller := newL4NetLBServiceController()
			svc.Annotations = testCase.annotations
			svc.ObjectMeta.Finalizers = testCase.finalizers
			controller.ctx.Cloud.Compute().(*cloud.MockGCE).MockForwardingRules.GetHook = testCase.frHook
			addNetLBService(controller, svc)
			// When
			result := controller.isRBSBasedService(svc)

			// Then
			if result != testCase.expectRBSService {
				t.Errorf("isRBSBasedService(%v) = %v, want %v", svc, result, testCase.expectRBSService)
			}
		})
	}
}

func TestIsRBSBasedServiceWithILBServices(t *testing.T) {
	controller := newL4NetLBServiceController()
	ilbSvc := test.NewL4ILBService(false, 8080)
	ilbFrName := loadbalancers.NewL4Handler(ilbSvc, controller.ctx.Cloud, meta.Regional, controller.namer, record.NewFakeRecorder(100), &sync.Mutex{}).GetFRName()
	ilbSvc.Annotations = map[string]string{
		annotations.TCPForwardingRuleKey: ilbFrName,
		annotations.UDPForwardingRuleKey: ilbFrName,
	}
	if controller.isRBSBasedService(ilbSvc) {
		t.Errorf("isRBSBasedService should not detect RBS in ILB services. Service: %v", ilbSvc)
	}
}

func TestIsRBSBasedServiceByForwardingRuleAnnotation(t *testing.T) {
	svc := test.NewL4LegacyNetLBService(8080, 30234)
	controller := newL4NetLBServiceController()
	addNetLBService(controller, svc)
	frName := utils.LegacyForwardingRuleName(svc)

	svc.Annotations = map[string]string{
		annotations.UDPForwardingRuleKey: "fr-1",
		annotations.TCPForwardingRuleKey: "fr-2",
	}
	if controller.isRBSBasedService(svc) {
		t.Errorf("Should not detect RBS by forwarding rule annotations without matching name. Service: %v", svc)
	}

	svc.Annotations = map[string]string{
		annotations.TCPForwardingRuleKey: frName,
	}
	if !controller.isRBSBasedService(svc) {
		t.Errorf("Should detect RBS by TCP forwarding rule annotation with matching name. Service %v", svc)
	}

	svc.Annotations = map[string]string{
		annotations.UDPForwardingRuleKey: frName,
	}
	if !controller.isRBSBasedService(svc) {
		t.Errorf("Should detect RBS by UDP forwarding rule annotation with matching name. Service %v", svc)
	}
}

func TestShouldProcessService(t *testing.T) {
	legacyNetLBSvc, l4netController := createAndSyncLegacyNetLBSvc(t)

	svcWithRBSFinalizer, err := l4netController.ctx.KubeClient.CoreV1().Services(legacyNetLBSvc.Namespace).Get(context.TODO(), legacyNetLBSvc.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", legacyNetLBSvc.Name, err)
	}
	svcWithRBSFinalizer.ObjectMeta.Finalizers = append(svcWithRBSFinalizer.ObjectMeta.Finalizers, common.NetLBFinalizerV2)

	svcWithRBSAnnotation, err := l4netController.ctx.KubeClient.CoreV1().Services(legacyNetLBSvc.Namespace).Get(context.TODO(), legacyNetLBSvc.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", legacyNetLBSvc.Name, err)
	}
	svcWithRBSAnnotation.Annotations = map[string]string{annotations.RBSAnnotationKey: annotations.RBSEnabled}

	svcWithRBSAnnotationAndFinalizer, err := l4netController.ctx.KubeClient.CoreV1().Services(legacyNetLBSvc.Namespace).Get(context.TODO(), legacyNetLBSvc.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", legacyNetLBSvc.Name, err)
	}
	svcWithRBSAnnotationAndFinalizer.ObjectMeta.Finalizers = append(svcWithRBSAnnotationAndFinalizer.ObjectMeta.Finalizers, common.NetLBFinalizerV2)
	svcWithRBSAnnotationAndFinalizer.Annotations = map[string]string{annotations.RBSAnnotationKey: annotations.RBSEnabled}

	for _, testCase := range []struct {
		oldSvc        *v1.Service
		newSvc        *v1.Service
		shouldProcess bool
	}{
		{
			oldSvc:        nil,
			newSvc:        legacyNetLBSvc,
			shouldProcess: false,
		},
		{
			oldSvc:        nil,
			newSvc:        svcWithRBSFinalizer,
			shouldProcess: true,
		},
		{
			oldSvc:        nil,
			newSvc:        svcWithRBSAnnotation,
			shouldProcess: true,
		},
		{
			oldSvc:        nil,
			newSvc:        svcWithRBSAnnotationAndFinalizer,
			shouldProcess: true,
		},
		{
			// We do not support migration only by finalizer
			oldSvc:        legacyNetLBSvc,
			newSvc:        svcWithRBSFinalizer,
			shouldProcess: false,
		},
		{
			oldSvc:        legacyNetLBSvc,
			newSvc:        svcWithRBSAnnotation,
			shouldProcess: true,
		},
		{
			oldSvc:        legacyNetLBSvc,
			newSvc:        svcWithRBSAnnotationAndFinalizer,
			shouldProcess: true,
		},
		{
			oldSvc:        svcWithRBSAnnotationAndFinalizer,
			newSvc:        svcWithRBSAnnotationAndFinalizer,
			shouldProcess: true,
		},
	} {
		result := l4netController.shouldProcessService(testCase.newSvc, testCase.oldSvc)
		if result != testCase.shouldProcess {
			t.Errorf("Old service %v. New service %v. Expected shouldProcess: %t, got: %t", testCase.oldSvc, testCase.newSvc, testCase.shouldProcess, result)
		}
	}
}
