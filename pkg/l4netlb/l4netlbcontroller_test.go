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

package l4netlb

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"

	"testing"

	ga "google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/composite"
	ingctx "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/healthchecks"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/namer"
)

const (
	clusterUID           = "aaaaa"
	testGCEZone          = "us-central1-b"
	FwIPAddress          = "10.0.0.1"
	loadBalancerIP       = "10.0.0.10"
	testServiceNamespace = "default"
	defaultNodePort      = int32(30234)
	hcNodePort           = int32(10111)
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
	frName := lc.namer.L4ForwardingRule(svc.Namespace, svc.Name, strings.ToLower(string(svc.Spec.Ports[0].Protocol)))
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
	svc = test.NewL4NetLBService(8080, defaultNodePort)
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
	validateSvcStatus(svc, t)
	return
}

func checkBackendService(lc *L4NetLBController, nodePort int32) error {

	backendServiceLink, bs, err := getBackend(lc, nodePort)
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

func getBackend(l4netController *L4NetLBController, nodePort int32) (string, *composite.BackendService, error) {
	backendServiceName := l4netController.namer.IGBackend(int64(nodePort))
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

	lc := NewL4NetLBController(ctx, stopCh)
	lc.Init()
	return lc
}

func validateSvcStatus(svc *v1.Service, t *testing.T) {
	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP != FwIPAddress {
		t.Fatalf("Invalid LoadBalancer status field in service - %+v", svc.Status.LoadBalancer)
	}
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
				nodePort := int32(30000 + port)
				newSvc := test.NewL4NetLBService(port, nodePort)
				newSvc.Name = newSvc.Name + fmt.Sprintf("-%d", port)
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
				validateSvcStatus(svc, t)
				if err := checkBackendService(lc, svc.Spec.Ports[0].NodePort); err != nil {
					t.Errorf("Check backend service err: %v", err)
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
		svc := test.NewL4NetLBServiceMultiplePorts(tc.svcName, tc.ports)
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

		if err := checkBackendService(lc, svc.Spec.Ports[0].NodePort); err != nil {
			t.Errorf("Check backend service err: %v", err)
		}
		if err := checkForwardingRule(lc, newSvc, tc.expectedPortRange); err != nil {
			t.Errorf("Check forwarding rule error: %v", err)
		}
		deleteNetLBService(lc, svc)
	}
}

func TestProcessServiceCreate(t *testing.T) {
	svc, lc := createAndSyncNetLBSvc(t)
	if err := checkBackendService(lc, defaultNodePort); err != nil {
		t.Errorf("UnexpectedError %v", err)
	}
	deleteNetLBService(lc, svc)
}

func TestProcessServiceDeletion(t *testing.T) {
	svc, lc := createAndSyncNetLBSvc(t)
	if !common.HasGivenFinalizer(svc.ObjectMeta, common.NetLBFinalizerV2) {
		t.Fatalf("Expected L4 External LoadBalancer finalizer")
	}
	if needsDeletion(svc) {
		t.Fatalf("Service should not be marked for deletion")
	}
	// Mark the service for deletion by updating timestamp
	svc.DeletionTimestamp = &metav1.Time{}
	updateNetLBService(lc, svc)
	if !needsDeletion(svc) {
		t.Fatalf("Service should be marked for deletion")
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
		t.Fatalf("Expected LoadBalancer status be deleted - %+v", svc.Status.LoadBalancer)
	}
	if common.HasGivenFinalizer(svc.ObjectMeta, common.NetLBFinalizerV2) {
		t.Fatalf("Unexpected LoadBalancer finalizer %v", svc.ObjectMeta.Finalizers)
	}
	deleteNetLBService(lc, svc)
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
	if needsDeletion(ilbSvc) {
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
		svc := test.NewL4NetLBService(8080, defaultNodePort)
		addNetLBService(lc, svc)
		key, _ := common.KeyFunc(svc)
		err := lc.sync(key)
		if err == nil || err.Error() != param.expectedError {
			t.Errorf("Error mismatch '%v' != '%v'", err.Error(), param.expectedError)
		}
	}
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
		if !needsDeletion(svc) {
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
				_, bs, err := getBackend(l4netController, defaultNodePort)
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
				name := (l4netController.namer.(namer.BackendNamer)).IGBackend(int64(svc.Spec.Ports[0].NodePort))
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
				frName := lc.namer.L4ForwardingRule(svc.Namespace, svc.Name, strings.ToLower(string(svc.Spec.Ports[0].Protocol)))
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
				if !reflect.DeepEqual(svc.Annotations, getAnnotations()) {
					return fmt.Errorf("Annotations mismatch %v != %v", svc.Annotations, getAnnotations())
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
	healthchecks.DeleteHealthCheck(lc.ctx.Cloud, hcNameShared)
	// Update ExternalTrafficPolicy to Cluster check if shared HC was created
	err = updateAndAssertExternalTrafficPolicy(newSvc, lc, v1.ServiceExternalTrafficPolicyTypeCluster, hcNameShared)
	if err != nil {
		t.Errorf("Error asserthing shared health check %v", err)
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

func TestControllerShouldNotProcessServicesWithLegacyFwR(t *testing.T) {
	svc, l4netController := createAndSyncNetLBSvc(t)
	// Add Forwarding Rule pointing to Target
	l4netController.ctx.Cloud.Compute().(*cloud.MockGCE).MockForwardingRules.GetHook = test.GetLegacyForwardingRule

	newSvc, err := l4netController.ctx.KubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", svc.Name, err)
	}
	newSvc.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal
	updateNetLBService(l4netController, svc)
	if !l4netController.hasLegacyForwardingRule(newSvc) {
		t.Errorf("Service should detect legacy forwarding rule")
	}
	if l4netController.shouldProcessService(svc, newSvc) {
		t.Errorf("Service should not be marked for update")
	}
}
