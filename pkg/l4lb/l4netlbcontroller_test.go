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
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"github.com/google/go-cmp/cmp"
	computebeta "google.golang.org/api/compute/v0.beta"
	compute "google.golang.org/api/compute/v1"
	ga "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	networkv1 "k8s.io/cloud-provider-gcp/crd/apis/network/v1"
	netfake "k8s.io/cloud-provider-gcp/crd/client/network/clientset/versioned/fake"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/composite"
	ingctx "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/healthchecksl4"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/metrics"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

const (
	FwIPAddress          = "10.0.0.1"
	loadBalancerIP       = "10.0.0.10"
	usersIP              = "35.10.211.60"
	testServiceNamespace = "default"
	hcNodePort           = int32(10111)
	userAddrName         = "UserStaticAddress"

	shortSessionAffinityIdleTimeout = int32(20)     // 20 sec could be used for regular Session Affinity
	longSessionAffinityIdleTimeout  = int32(2 * 60) // 2 min or 120 sec for Strong Session Affinity
)

var (
	netLBCommonAnnotationKeys = []string{
		annotations.BackendServiceKey,
		annotations.HealthcheckKey,
	}
	netLBIPv4AnnotationKeys = []string{
		annotations.FirewallRuleKey,
		annotations.TCPForwardingRuleKey,
		annotations.FirewallRuleForHealthcheckKey,
	}
	netLBIPv6AnnotationKeys = []string{
		annotations.FirewallRuleIPv6Key,
		annotations.TCPForwardingRuleIPv6Key,
		annotations.FirewallRuleForHealthcheckIPv6Key,
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

func getStrongSessionAffinityAnnotations() map[string]string {
	return map[string]string{
		annotations.StrongSessionAffinityAnnotationKey: annotations.StrongSessionAffinityEnabled,
		annotations.RBSAnnotationKey:                   annotations.RBSEnabled,
	}
}

func getSessionAffinityConfig(timeoutSec int32) *v1.SessionAffinityConfig {
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
	fwdRule, err := composite.GetForwardingRule(lc.ctx.Cloud, meta.RegionalKey(frName, lc.ctx.Cloud.Region()), meta.VersionGA, klog.TODO())
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
	err := lc.sync(key, klog.TODO())
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
	bs, err := checkBackendServiceCommon(lc, svc)
	if err != nil {
		return err
	}
	igName := lc.namer.InstanceGroup()
	for _, b := range bs.Backends {
		if !strings.Contains(b.Group, igName) {
			return fmt.Errorf("Backend Instance Group Link mismatch: %s != %s", igName, b.Group)
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

func checkBackendServiceWithNEG(lc *L4NetLBController, svc *v1.Service) error {
	bs, err := checkBackendServiceCommon(lc, svc)
	if err != nil {
		return err
	}
	negName := lc.namer.L4Backend(svc.Namespace, svc.Name)
	for _, b := range bs.Backends {
		if !strings.Contains(b.Group, negName) {
			return fmt.Errorf("Backend NEG Link mismatch: %s != %s", negName, b.Group)
		}
	}
	neg, err := lc.ctx.Cloud.GetNetworkEndpointGroup(negName, testGCEZone)
	if err != nil {
		return fmt.Errorf("Error getting NEG, err %v", err)
	}
	if neg == nil {
		return fmt.Errorf("NEG does not exist")
	}
	return nil
}

// checkBackendServiceCommon verifies attributes common to InstanceGroup and NEG backed BackendServices.
func checkBackendServiceCommon(lc *L4NetLBController, svc *v1.Service) (*composite.BackendService, error) {
	backendServiceLink, bs, err := getBackend(lc, svc)
	if err != nil {
		return nil, fmt.Errorf("Failed to fetch backend service, err %v", err)
	}
	if bs.SelfLink != backendServiceLink {
		return nil, fmt.Errorf("Backend Service SelfLink mismatch: %s != %s", bs.SelfLink, backendServiceLink)
	}
	if bs.LoadBalancingScheme != string(cloud.SchemeExternal) {
		return nil, fmt.Errorf("Load Balancing Scheme mismatch: EXTERNAL != %s", bs.LoadBalancingScheme)
	}
	if len(bs.Backends) == 0 {
		return nil, fmt.Errorf("Error no backends in BackendService")
	}
	return bs, nil
}

func updateRegionBackendServiceWithLockHook(ctx context.Context, key *meta.Key, obj *ga.BackendService, m *cloud.MockRegionBackendServices, options ...cloud.Option) error {
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
	backendServiceName := l4netController.namer.L4Backend(svc.Namespace, svc.Name)
	key := meta.RegionalKey(backendServiceName, l4netController.ctx.Cloud.Region())
	backendServiceLink := cloud.SelfLink(meta.VersionGA, l4netController.ctx.Cloud.ProjectID(), "backendServices", key)
	bs, err := composite.GetBackendService(l4netController.ctx.Cloud, key, meta.VersionGA, klog.TODO())
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
	networkClient := netfake.NewSimpleClientset()

	namer := namer.NewNamer(clusterUID, "", klog.TODO())

	ctxConfig := ingctx.ControllerContextConfig{
		Namespace:         v1.NamespaceAll,
		ResyncPeriod:      1 * time.Minute,
		NumL4NetLBWorkers: 5,
		MaxIGSize:         1000,
	}
	return ingctx.NewControllerContext(nil, kubeClient, nil, nil, nil, nil, nil, nil, networkClient, kubeClient /*kube client to be used for events*/, fakeGCE, namer, "" /*kubeSystemUID*/, ctxConfig, klog.TODO())
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
	return NewL4NetLBController(ctx, stopCh, klog.TODO())
}

func validateNetLBSvcStatus(svc *v1.Service, t *testing.T) {
	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP != FwIPAddress {
		t.Fatalf("Invalid LoadBalancer status field in service - %+v", svc.Status.LoadBalancer)
	}
}

func calculateNetLBExpectedAnnotationsKeys(svc *v1.Service) []string {
	expectedAnnotations := netLBCommonAnnotationKeys
	if utils.NeedsIPv4(svc) {
		expectedAnnotations = append(expectedAnnotations, netLBIPv4AnnotationKeys...)
	}
	if utils.NeedsIPv6(svc) {
		expectedAnnotations = append(expectedAnnotations, netLBIPv6AnnotationKeys...)
	}
	return expectedAnnotations
}

func validateAnnotations(svc *v1.Service) error {
	expectedAnnotationsKeys := calculateNetLBExpectedAnnotationsKeys(svc)

	var missingKeys []string
	for _, key := range expectedAnnotationsKeys {
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
	expectedAnnotationsKeys := calculateNetLBExpectedAnnotationsKeys(svc)

	var unexpectedKeys []string
	for _, key := range expectedAnnotationsKeys {
		if _, exists := svc.Annotations[key]; exists {
			unexpectedKeys = append(unexpectedKeys, key)
		}
	}
	if len(unexpectedKeys) != 0 {
		return fmt.Errorf("Unexpected annotations: %v, Service annotations %v", unexpectedKeys, svc.Annotations)
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
		if err := lc.sync(key, klog.TODO()); err != nil {
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
	err = lc.sync(key, klog.TODO())
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

func TestProcessMultinetServiceCreate(t *testing.T) {
	lc := newL4NetLBServiceController()

	lc.networkResolver = network.NewFakeResolver(&network.NetworkInfo{
		IsDefault:     false,
		K8sNetwork:    "secondary-network",
		NetworkURL:    "vpcURL",
		SubnetworkURL: "subnetURL",
	})

	svc := test.NewL4NetLBRBSService(8080)
	// create the NEG that would be created by the NEG controller.
	neg := &computebeta.NetworkEndpointGroup{
		Name: lc.namer.L4Backend(svc.Namespace, svc.Name),
	}
	lc.ctx.Cloud.CreateNetworkEndpointGroup(neg, "us-central1-b")

	svc.Spec.Selector = make(map[string]string)
	svc.Spec.Selector[networkv1.NetworkAnnotationKey] = "secondary-network"
	addNetLBService(lc, svc)
	prevMetrics, err := test.GetL4NetLBLatencyMetric()
	if err != nil {
		t.Errorf("Error getting L4 NetLB latency metrics err: %v", err)
	}
	if prevMetrics == nil {
		t.Fatalf("Cannot get prometheus metrics for L4NetLB latency")
	}
	key, _ := common.KeyFunc(svc)
	err = lc.sync(key, klog.TODO())
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
	if err := checkBackendServiceWithNEG(lc, svc); err != nil {
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
	if err := lc.sync(key, klog.TODO()); err == nil {
		t.Errorf("Expected sync error when address reservation fails.")
	}
	addUsersStaticAddress(lc, cloud.NetworkTierDefault)
	if err := lc.sync(key, klog.TODO()); err != nil {
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
	if err := lc.sync(key, klog.TODO()); err != nil {
		t.Errorf("Unexpected Error when trying to sync service after deletion, err: %v", err)
	}
	adr, err := lc.ctx.Cloud.GetRegionAddress(userAddrName, lc.ctx.Cloud.Region())
	if err != nil {
		t.Errorf("Unexpected error when trying to get regional address, err: %v", err)
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
	if lc.needsDeletion(svc, klog.TODO()) {
		t.Errorf("Service should not be marked for deletion")
	}
	// Mark the service for deletion by updating timestamp
	svc.DeletionTimestamp = &metav1.Time{}
	updateNetLBService(lc, svc)
	if !lc.needsDeletion(svc, klog.TODO()) {
		t.Errorf("Service should be marked for deletion")
	}
	key, _ := common.KeyFunc(svc)
	err := lc.sync(key, klog.TODO())
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

func TestProcessRBSServiceTypeTransition(t *testing.T) {
	testCases := []struct {
		desc      string
		finalType v1.ServiceType
	}{
		{
			desc:      "Change from RBS to ClusterIP should delete RBS resources",
			finalType: v1.ServiceTypeClusterIP,
		},
		{
			desc:      "Change from RBS to NodePort should delete RBS resources",
			finalType: v1.ServiceTypeNodePort,
		},
		{
			desc:      "Change from RBS to ExternalName should delete RBS resources",
			finalType: v1.ServiceTypeExternalName,
		},
		{
			desc:      "Change from RBS to empty (default) type should delete RBS resources",
			finalType: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			svc, lc := createAndSyncNetLBSvc(t)
			if lc.needsDeletion(svc, klog.TODO()) {
				t.Errorf("Service should not be marked for deletion")
			}

			svc.Spec.Type = tc.finalType
			updateNetLBService(lc, svc)
			if !lc.needsDeletion(svc, klog.TODO()) {
				t.Errorf("RBS after switching to %v should be marked for deletion", tc.finalType)
			}

			key, _ := common.KeyFunc(svc)
			err := lc.sync(key, klog.TODO())
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
		})
	}
}

func TestServiceDeletionWhenInstanceGroupInUse(t *testing.T) {
	svc, lc := createAndSyncNetLBSvc(t)

	(lc.ctx.Cloud.Compute().(*cloud.MockGCE)).MockInstanceGroups.DeleteHook = func(ctx context.Context, key *meta.Key, m *cloud.MockInstanceGroups, options ...cloud.Option) (bool, error) {
		err := &googleapi.Error{
			Code:    http.StatusBadRequest,
			Message: "GetErrorInstanceGroupHook: Cannot delete instance group being used by another service",
		}
		return true, err
	}

	svc.DeletionTimestamp = &metav1.Time{}
	updateNetLBService(lc, svc)
	key, _ := common.KeyFunc(svc)
	err := lc.sync(key, klog.TODO())
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
	err := lc.sync(key, klog.TODO())
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
	if lc.needsDeletion(ilbSvc, klog.TODO()) {
		t.Fatalf("Service should not be marked for deletion!")
	}
}

func TestProcessServiceCreationFailed(t *testing.T) {
	for _, param := range []struct {
		addMockFunc   func(*cloud.MockGCE)
		expectedError string
	}{{addMockFunc: func(c *cloud.MockGCE) { c.MockInstanceGroups.GetHook = test.GetErrorInstanceGroupHook },
		expectedError: "lc.instancePool.EnsureInstanceGroupsAndPorts(k8s-ig--aaaaa, []) returned error GetErrorInstanceGroupHook"},
		{addMockFunc: func(c *cloud.MockGCE) { c.MockInstanceGroups.ListHook = test.ListErrorHook },
			expectedError: "ListErrorHook"},
		{addMockFunc: func(c *cloud.MockGCE) { c.MockInstanceGroups.InsertHook = test.InsertErrorHook },
			expectedError: "lc.instancePool.EnsureInstanceGroupsAndPorts(k8s-ig--aaaaa, []) returned error InsertErrorHook"},

		{addMockFunc: func(c *cloud.MockGCE) { c.MockInstanceGroups.AddInstancesHook = test.AddInstancesErrorHook },
			expectedError: "AddInstances: [AddInstancesErrorHook]"},
		{addMockFunc: func(c *cloud.MockGCE) { c.MockInstanceGroups.ListInstancesHook = test.ListInstancesWithErrorHook },
			expectedError: "ListInstancesWithErrorHook"},
	} {
		lc := newL4NetLBServiceController()
		param.addMockFunc((lc.ctx.Cloud.Compute().(*cloud.MockGCE)))
		svc := test.NewL4NetLBRBSService(8080)
		addNetLBService(lc, svc)
		key, _ := common.KeyFunc(svc)
		err := lc.sync(key, klog.TODO())
		if err == nil || err.Error() != param.expectedError {
			t.Errorf("Error mismatch '%v' != '%v'", err, param.expectedError)
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
	err = lc.sync(key, klog.TODO())
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
			expectedError: "Failed to delete forwarding rule a, err: DeleteForwardingRulesErrorHook"},
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
		if !lc.needsDeletion(svc, klog.TODO()) {
			t.Fatalf("Service should be marked for deletion")
		}
		param.addMockFunc((lc.ctx.Cloud.Compute().(*cloud.MockGCE)))
		key, _ := common.KeyFunc(svc)
		err := lc.sync(key, klog.TODO())
		if err == nil || err.Error() != param.expectedError {
			t.Errorf("Error mismatch '%v' != '%v'", err, param.expectedError)
		}
	}
}

func TestServiceStatusForErrorSync(t *testing.T) {
	lc := newL4NetLBServiceController()
	(lc.ctx.Cloud.Compute().(*cloud.MockGCE)).MockForwardingRules.InsertHook = mock.InsertForwardingRulesInternalErrHook

	svc := test.NewL4NetLBRBSService(8080)
	addNetLBService(lc, svc)

	syncResult := lc.syncInternal(svc, klog.TODO())
	if syncResult.Error == nil {
		t.Errorf("Expected error in sync controller")
	}
	if syncResult.MetricsLegacyState.InSuccess == true {
		t.Fatalf("Metric status InSuccess for service %s/%s mismatch, expected: true, received: false", svc.Namespace, svc.Name)
	}
	if syncResult.MetricsLegacyState.FirstSyncErrorTime == nil {
		t.Fatalf("Metric status FirstSyncErrorTime for service %s/%s mismatch, expected: not nil, received: nil", svc.Namespace, svc.Name)
	}
}

func TestServiceStatusForSuccessSync(t *testing.T) {
	lc := newL4NetLBServiceController()

	svc := test.NewL4NetLBRBSService(8080)
	addNetLBService(lc, svc)

	syncResult := lc.syncInternal(svc, klog.TODO())
	if syncResult.Error != nil {
		t.Errorf("Unexpected error in sync controller")
	}
	if syncResult.MetricsLegacyState.InSuccess != true {
		t.Fatalf("Metric status InSuccess for service %s/%s mismatch, expected: false, received: true", svc.Namespace, svc.Name)
	}
	if syncResult.MetricsLegacyState.FirstSyncErrorTime != nil {
		t.Fatalf("Metric status FirstSyncErrorTime for service %s/%s mismatch, expected: nil, received: %v", svc.Namespace, svc.Name, syncResult.MetricsLegacyState.FirstSyncErrorTime)
	}
}

func TestProcessServiceUpdate(t *testing.T) {
	for _, param := range []struct {
		Desc        string
		Update      func(*v1.Service)
		CheckResult func(*L4NetLBController, *v1.Service) error
	}{
		{
			Desc:   "Keep Service Affinity type equal to None",
			Update: func(s *v1.Service) { s.Spec.SessionAffinity = v1.ServiceAffinityNone },
			CheckResult: func(l4netController *L4NetLBController, svc *v1.Service) error {
				_, bs, err := getBackend(l4netController, svc)
				if err != nil {
					return fmt.Errorf("Failed to fetch backend service: %v", err)
				}
				if bs.SessionAffinity != utils.TranslateAffinityType(string(v1.ServiceAffinityNone), klog.TODO()) {
					return fmt.Errorf("SessionAffinity mismatch %v != %v", bs.SessionAffinity, v1.ServiceAffinityNone)
				}
				return nil
			},
		},
		{
			Desc:   "Update Source Rangers in LB Service",
			Update: func(s *v1.Service) { s.Spec.LoadBalancerSourceRanges = getLoadBalancerSourceRanges() },
			CheckResult: func(l4netController *L4NetLBController, svc *v1.Service) error {
				if len(svc.Spec.Ports) == 0 {
					return fmt.Errorf("No Ports in service")
				}
				name := (l4netController.namer.(namer.BackendNamer)).L4Backend(svc.Namespace, svc.Name)
				fw, err := l4netController.ctx.Cloud.GetFirewall(name)
				if err != nil {
					return fmt.Errorf("Failed to fetch firewall service: %v", err)
				}
				expectedRange := getLoadBalancerSourceRanges()
				sort.Strings(expectedRange)
				sort.Strings(fw.SourceRanges)
				if diff := cmp.Diff(fw.SourceRanges, expectedRange); diff != "" {
					return fmt.Errorf("SourceRanges mismatch: %v != %v", fw.SourceRanges, expectedRange)
				}
				return nil
			},
		},
		{
			Desc: "Update service with Session Affinity Config only",
			Update: func(s *v1.Service) {
				s.Spec.SessionAffinityConfig = getSessionAffinityConfig(shortSessionAffinityIdleTimeout)
			},
			CheckResult: func(l4netController *L4NetLBController, svc *v1.Service) error {
				if diff := cmp.Diff(svc.Spec.SessionAffinityConfig, getSessionAffinityConfig(shortSessionAffinityIdleTimeout)); diff != "" {
					return fmt.Errorf("SessionAffinityConfig mismatch: %s", diff)
				}
				return nil
			},
		},
		{
			Desc:   "Change External Traffic Policy Type to Local",
			Update: func(s *v1.Service) { s.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal },
			CheckResult: func(l4netController *L4NetLBController, svc *v1.Service) error {
				if svc.Spec.ExternalTrafficPolicy != v1.ServiceExternalTrafficPolicyTypeLocal {
					return fmt.Errorf("ExternalTrafficPolicy mismatch %v != %v", svc.Spec.ExternalTrafficPolicy, v1.ServiceExternalTrafficPolicyTypeLocal)
				}
				return nil
			},
		},
		{
			Desc:   "Update Legacy LoadBalancerIP parameter of the service",
			Update: func(s *v1.Service) { s.Spec.LoadBalancerIP = loadBalancerIP },
			CheckResult: func(lc *L4NetLBController, svc *v1.Service) error {
				frName := utils.LegacyForwardingRuleName(svc)
				fwdRule, err := composite.GetForwardingRule(lc.ctx.Cloud, meta.RegionalKey(frName, lc.ctx.Cloud.Region()), meta.VersionGA, klog.TODO())
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
			Desc:   "Update service with a new list of ExternalIPs",
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
			Desc:   "Update service with new list of ports",
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
			Desc:   "Change Healthcheck Node Port for the service",
			Update: func(s *v1.Service) { s.Spec.HealthCheckNodePort = hcNodePort },
			CheckResult: func(l4netController *L4NetLBController, svc *v1.Service) error {
				if svc.Spec.HealthCheckNodePort != hcNodePort {
					return fmt.Errorf("HealthCheckNodePort mismatch %v != %v", svc.Spec.HealthCheckNodePort, hcNodePort)
				}
				return nil
			},
		},
		{
			Desc:   "Update service with new (fake) annotations",
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
		t.Run(param.Desc, func(t *testing.T) {
			svc, l4netController := createAndSyncNetLBSvc(t)
			l4netController.ctx.EnableL4StrongSessionAffinity = true
			(l4netController.ctx.Cloud.Compute().(*cloud.MockGCE)).MockFirewalls.PatchHook = mock.UpdateFirewallHook
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
			err = l4netController.sync(key, klog.TODO())
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
		})
	}
}

func TestHealthCheckWhenExternalTrafficPolicyWasUpdated(t *testing.T) {
	svc, lc := createAndSyncNetLBSvc(t)
	newSvc, err := lc.ctx.KubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", svc.Name, err)
	}

	// Update ExternalTrafficPolicy to Local check if nonshared HC was created
	hcNameNonShared := lc.namer.L4HealthCheck(svc.Namespace, svc.Name, false)
	err = updateAndAssertExternalTrafficPolicy(newSvc, lc, v1.ServiceExternalTrafficPolicyTypeLocal, hcNameNonShared)
	if err != nil {
		t.Errorf("Error asserting nonshared health check %v", err)
	}
	// delete shared health check if is created, update service to Cluster and
	// check that non-shared health check was created
	hcNameShared := lc.namer.L4HealthCheck(svc.Namespace, svc.Name, true)
	healthchecksl4.Fake(lc.ctx.Cloud, lc.ctx.Recorder(svc.Namespace)).DeleteHealthCheckWithFirewall(svc, lc.namer, true, meta.Regional, utils.XLB, klog.TODO())
	// Update ExternalTrafficPolicy to Cluster check if shared HC was created
	err = updateAndAssertExternalTrafficPolicy(newSvc, lc, v1.ServiceExternalTrafficPolicyTypeCluster, hcNameShared)
	if err != nil {
		t.Errorf("Error asserting shared health check %v", err)
	}
	newSvc.DeletionTimestamp = &metav1.Time{}
	updateNetLBService(lc, newSvc)
	key, _ := common.KeyFunc(newSvc)
	if err = lc.sync(key, klog.TODO()); err != nil {
		t.Errorf("Failed to sync deleted service %s, err %v", key, err)
	}
	if !isHealthCheckDeleted(lc.ctx.Cloud, hcNameNonShared, klog.TODO()) {
		t.Errorf("Health check %s should be deleted", hcNameNonShared)
	}
	if !isHealthCheckDeleted(lc.ctx.Cloud, hcNameShared, klog.TODO()) {
		t.Errorf("Health check %s should be deleted", hcNameShared)
	}
	deleteNetLBService(lc, svc)
}

func updateAndAssertExternalTrafficPolicy(newSvc *v1.Service, lc *L4NetLBController, newPolicy v1.ServiceExternalTrafficPolicyType, hcName string) error {
	newSvc.Spec.ExternalTrafficPolicy = newPolicy
	updateNetLBService(lc, newSvc)
	key, _ := common.KeyFunc(newSvc)
	err := lc.sync(key, klog.TODO())
	if err != nil {
		return fmt.Errorf("Failed to sync updated service %s, err %v", key, err)
	}
	newSvc, err = lc.ctx.KubeClient.CoreV1().Services(newSvc.Namespace).Get(context.TODO(), newSvc.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Failed to lookup service %s, err: %v", newSvc.Name, err)
	}
	_, err = composite.GetHealthCheck(lc.ctx.Cloud, meta.RegionalKey(hcName, lc.ctx.Cloud.Region()), meta.VersionGA, klog.TODO())
	if err != nil {
		return fmt.Errorf("Error getting health check %v", err)
	}
	return nil
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
	if err := lc.sync(key, klog.TODO()); !utils.IsNetworkTierError(err) {
		t.Errorf("Expected error when trying to ensure service with wrong Network Tier, err: %v", err)
	}
	svc.Annotations[annotations.NetworkTierAnnotationKey] = string(cloud.NetworkTierStandard)
	updateNetLBService(lc, svc)
	if err := lc.sync(key, klog.TODO()); err != nil {
		t.Errorf("Unexpected error when trying to ensure service with STANDARD Network Tier, err: %v", err)
	}
}

type getForwardingRuleHook func(ctx context.Context, key *meta.Key, m *cloud.MockForwardingRules, options ...cloud.Option) (bool, *ga.ForwardingRule, error)

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
			result := controller.isRBSBasedService(svc, klog.TODO())

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
	l4ilbParams := &loadbalancers.L4ILBParams{
		Service:  ilbSvc,
		Cloud:    controller.ctx.Cloud,
		Namer:    controller.namer,
		Recorder: record.NewFakeRecorder(100),
	}
	ilbFrName := loadbalancers.NewL4Handler(l4ilbParams, klog.TODO()).GetFRName()
	ilbSvc.Annotations = map[string]string{
		annotations.TCPForwardingRuleKey: ilbFrName,
		annotations.UDPForwardingRuleKey: ilbFrName,
	}
	if controller.isRBSBasedService(ilbSvc, klog.TODO()) {
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
	if controller.isRBSBasedService(svc, klog.TODO()) {
		t.Errorf("Should not detect RBS by forwarding rule annotations without matching name. Service: %v", svc)
	}

	svc.Annotations = map[string]string{
		annotations.TCPForwardingRuleKey: frName,
	}
	if !controller.isRBSBasedService(svc, klog.TODO()) {
		t.Errorf("Should detect RBS by TCP forwarding rule annotation with matching name. Service %v", svc)
	}

	svc.Annotations = map[string]string{
		annotations.UDPForwardingRuleKey: frName,
	}
	if !controller.isRBSBasedService(svc, klog.TODO()) {
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

	svcWithLoadBalancerClass, err := l4netController.ctx.KubeClient.CoreV1().Services(legacyNetLBSvc.Namespace).Get(context.TODO(), legacyNetLBSvc.Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to lookup service %s, err: %v", legacyNetLBSvc.Name, err)
	}
	svcWithLoadBalancerClass.Annotations = map[string]string{annotations.RBSAnnotationKey: annotations.RBSEnabled}
	testLBClass := "testLBClass"
	svcWithLoadBalancerClass.Spec.LoadBalancerClass = &testLBClass

	for _, testCase := range []struct {
		oldSvc        *v1.Service
		newSvc        *v1.Service
		shouldProcess bool
		shouldResync  bool
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
			oldSvc:        nil,
			newSvc:        svcWithLoadBalancerClass,
			shouldProcess: false,
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
			shouldResync:  true,
		},
	} {
		result, isResync := l4netController.shouldProcessService(testCase.newSvc, testCase.oldSvc, klog.TODO())
		if result != testCase.shouldProcess {
			t.Errorf("Old service %v. New service %v. Expected needsUpdate: %t, got: %t", testCase.oldSvc, testCase.newSvc, testCase.shouldProcess, result)
		}
		if isResync != testCase.shouldResync {
			t.Errorf("Old service %v. New service %v. Expected needsResync: %t, got: %t", testCase.oldSvc, testCase.newSvc, testCase.shouldResync, isResync)
		}
	}
}

func TestStrongSessionAffinityServiceUpdate(t *testing.T) {
	// setup an original service
	svc, l4netController := createAndSyncNetLBSvc(t)
	l4netController.ctx.EnableL4StrongSessionAffinity = true

	// update service objects
	newSvc, _ := l4netController.ctx.KubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
	newSvc.Spec.SessionAffinity = v1.ServiceAffinityClientIP
	newSvc.Spec.SessionAffinityConfig = getSessionAffinityConfig(longSessionAffinityIdleTimeout)
	newSvc.Annotations = getStrongSessionAffinityAnnotations()

	// update in indexer
	updateNetLBService(l4netController, newSvc)

	// trigger sync
	if !l4netController.needsUpdate(svc, newSvc) {
		t.Errorf("Service should be marked for update")
	}
	key, _ := common.KeyFunc(newSvc)
	l4netController.sync(key, klog.TODO())

	svcAfterUpdate, _ := l4netController.ctx.KubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})

	// make sure changed specs are present after the sync
	if diff := cmp.Diff(svcAfterUpdate.Spec.SessionAffinity, v1.ServiceAffinityClientIP); diff != "" {
		t.Errorf("SessionAffinity type mismatch, got and expected: %s", diff)
	}
	if diff := cmp.Diff(svcAfterUpdate.Spec.SessionAffinityConfig, getSessionAffinityConfig(longSessionAffinityIdleTimeout)); diff != "" {
		t.Errorf("ServiceAffinityConfig mismatch, got and expected: %s", diff)
	}
	expAddedAnnotations := getStrongSessionAffinityAnnotations()
	for name, value := range expAddedAnnotations {
		if svcAfterUpdate.Annotations[name] != value {
			t.Errorf("Annotation got %v != expected %v", svcAfterUpdate.Annotations[name], value)
		}
	}
	deleteNetLBService(l4netController, svcAfterUpdate)
}

func TestDualStackServiceNeedsUpdate(t *testing.T) {
	testCases := []struct {
		desc              string
		initialIPFamilies []v1.IPFamily
		finalIPFamilies   []v1.IPFamily
		needsUpdate       bool
	}{
		{
			desc:              "Should update NetLB on ipv4 -> ipv4, ipv6",
			initialIPFamilies: []v1.IPFamily{v1.IPv4Protocol},
			finalIPFamilies:   []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
			needsUpdate:       true,
		},
		{
			desc:              "Should update NetLB on ipv4, ipv6 -> ipv4",
			initialIPFamilies: []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
			finalIPFamilies:   []v1.IPFamily{v1.IPv4Protocol},
			needsUpdate:       true,
		},
		{
			desc:              "Should update NetLB on ipv6 -> ipv6, ipv4",
			initialIPFamilies: []v1.IPFamily{v1.IPv6Protocol},
			finalIPFamilies:   []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol},
			needsUpdate:       true,
		},
		{
			desc:              "Should update NetLB on ipv6, ipv4 -> ipv6",
			initialIPFamilies: []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol},
			finalIPFamilies:   []v1.IPFamily{v1.IPv6Protocol},
			needsUpdate:       true,
		},
		{
			desc:              "Should not update NetLB on same IP families update",
			initialIPFamilies: []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol},
			finalIPFamilies:   []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol},
			needsUpdate:       false,
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			controller := newL4NetLBServiceController()
			controller.enableDualStack = true
			oldSvc := test.NewL4NetLBRBSService(8080)
			oldSvc.Spec.IPFamilies = tc.initialIPFamilies
			newSvc := test.NewL4NetLBRBSService(8080)
			newSvc.Spec.IPFamilies = tc.finalIPFamilies

			result := controller.needsUpdate(oldSvc, newSvc)
			if result != tc.needsUpdate {
				t.Errorf("Old service %v. New service %v. Expected needsUpdate: %t, got: %t", oldSvc, newSvc, tc.needsUpdate, result)
			}
		})
	}
}

func TestPreventTargetPoolToRBSMigration(t *testing.T) {
	testCases := []struct {
		desc                            string
		frHook                          getForwardingRuleHook
		finalizer                       string
		expectV2NetLBFinalizerAfterSync bool
		expectRBSAnnotationAfterSync    bool
	}{
		{
			desc:                            "Should not add finalizer and RBS annotation to target pool service",
			frHook:                          test.GetLegacyForwardingRule,
			expectV2NetLBFinalizerAfterSync: false,
			expectRBSAnnotationAfterSync:    false,
		},
		{
			desc:                            "Should remove finalizer and RBS annotation from target pool service with RBS finalizer. Covers race on creation",
			frHook:                          test.GetLegacyForwardingRule,
			finalizer:                       common.NetLBFinalizerV2,
			expectV2NetLBFinalizerAfterSync: false,
			expectRBSAnnotationAfterSync:    false,
		},
		{
			desc:                            "Should not remove finalizer and RBS annotation from RBS based service",
			finalizer:                       common.NetLBFinalizerV2,
			frHook:                          test.GetRBSForwardingRule,
			expectV2NetLBFinalizerAfterSync: true,
			expectRBSAnnotationAfterSync:    true,
		},
		{
			desc:                            "Should not remove finalizer and RBS annotation from RBS service without forwarding rule",
			finalizer:                       common.NetLBFinalizerV2,
			expectV2NetLBFinalizerAfterSync: true,
			expectRBSAnnotationAfterSync:    true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.desc, func(t *testing.T) {
			svc := test.NewL4NetLBRBSServiceMultiplePorts("test", []int32{30234})
			svc.ObjectMeta.Finalizers = []string{testCase.finalizer}

			controller := newL4NetLBServiceController()
			controller.ctx.Cloud.Compute().(*cloud.MockGCE).MockForwardingRules.GetHook = testCase.frHook

			addNetLBService(controller, svc)

			key, err := common.KeyFunc(svc)
			if err != nil {
				t.Fatalf("common.KeyFunc(%v) returned error %v, want nil", svc, err)
			}
			// test only preventLegacyServiceHandling
			_, err = controller.preventLegacyServiceHandling(svc, key, klog.TODO())
			if err != nil {
				t.Fatalf("controller.preventLegacyServiceHandling(%v, %s) returned error %v, want nil", svc, key, err)
			}

			resultSvc, err := controller.ctx.KubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("controller.ctx.KubeClient.CoreV1().Services(%s).Get(_, %s, _) returned error %v, want nil", svc.Namespace, svc.Name, err)
			}
			hasV2Finalizer := utils.HasL4NetLBFinalizerV2(resultSvc)
			if hasV2Finalizer != testCase.expectV2NetLBFinalizerAfterSync {
				t.Errorf("After preventLegacyServiceHandling, hasV2Finalizer = %t, testCase.expectV2NetLBFinalizerAfterSync = %t, want equal", hasV2Finalizer, testCase.expectV2NetLBFinalizerAfterSync)
			}
			hasRBSAnnotation := annotations.HasRBSAnnotation(resultSvc)
			if hasRBSAnnotation != testCase.expectRBSAnnotationAfterSync {
				t.Errorf("After preventLegacyServiceHandling, hasRBSAnnotation = %t, testCase.expectRBSAnnotationAfterSync = %t, want equal", hasRBSAnnotation, testCase.expectRBSAnnotationAfterSync)
			}

			// test that whole sync process is skipped
			svc2 := test.NewL4NetLBRBSServiceMultiplePorts("test-2", []int32{30234})
			svc2.ObjectMeta.Finalizers = []string{testCase.finalizer}
			addNetLBService(controller, svc2)

			key, err = common.KeyFunc(svc2)
			if err != nil {
				t.Fatalf("common.KeyFunc(%v) returned error %v, want nil", svc2, err)
			}

			err = controller.sync(key, klog.TODO())
			if err != nil {
				t.Fatalf("controller.sync(%s) returned error %v, want nil", key, err)
			}

			resultSvc, err = controller.ctx.KubeClient.CoreV1().Services(svc2.Namespace).Get(context.TODO(), svc2.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("controller.ctx.KubeClient.CoreV1().Services(%s).Get(_, %s, _) returned error %v, want nil", svc2.Namespace, svc2.Name, err)
			}
			hasV2Finalizer = utils.HasL4NetLBFinalizerV2(resultSvc)
			if hasV2Finalizer != testCase.expectV2NetLBFinalizerAfterSync {
				t.Errorf("After sync, hasV2NetLBFinalizer = %t, testCase.expectV2NetLBFinalizerAfterSync = %t, want equal", hasV2Finalizer, testCase.expectV2NetLBFinalizerAfterSync)
			}
			hasRBSAnnotation = annotations.HasRBSAnnotation(resultSvc)
			if hasRBSAnnotation != testCase.expectRBSAnnotationAfterSync {
				t.Errorf("After sync, hasRBSAnnotation = %t, testCase.expectRBSAnnotationAfterSync = %t, want equal", hasRBSAnnotation, testCase.expectRBSAnnotationAfterSync)
			}
		})
	}
}

func TestIsRBSBasedServiceForNonLoadBalancersType(t *testing.T) {
	testCases := []struct {
		desc    string
		ports   []v1.ServicePort
		svcType v1.ServiceType
	}{
		{
			desc: "Service ClusterIP with ports should not be marked as RBS",
			ports: []v1.ServicePort{
				{Name: "testport", Port: 8080, Protocol: "TCP", NodePort: 32999},
			},
			svcType: v1.ServiceTypeClusterIP,
		},
		{
			desc:    "Service ClusterIP with empty ports array should not be marked as RBS",
			ports:   []v1.ServicePort{},
			svcType: v1.ServiceTypeClusterIP,
		},
		{
			desc:    "Service ClusterIP without ports should not be marked as RBS",
			ports:   nil,
			svcType: v1.ServiceTypeClusterIP,
		},
		{
			desc:    "Service NodePort should not be marked as RBS",
			svcType: v1.ServiceTypeNodePort,
		},
		{
			desc:    "Service ExternalName should not be marked as RBS",
			svcType: v1.ServiceTypeExternalName,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			// Setup
			svc := &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "example-svc",
					Annotations: make(map[string]string),
				},
				Spec: v1.ServiceSpec{
					Type:  tc.svcType,
					Ports: tc.ports,
				},
			}
			controller := newL4NetLBServiceController()

			if controller.isRBSBasedService(svc, klog.TODO()) {
				t.Errorf("isRBSBasedService(%v) = true, want false", svc)
			}
		})
	}
}

func TestCreateDeleteDualStackNetLBService(t *testing.T) {
	testCases := []struct {
		desc       string
		ipFamilies []v1.IPFamily
	}{
		{
			desc:       "Create and delete IPv4 NetLB",
			ipFamilies: []v1.IPFamily{v1.IPv4Protocol},
		},
		{
			desc:       "Create and delete IPv4 IPv6 NetLB",
			ipFamilies: []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
		},
		{
			desc:       "Create and delete IPv6 NetLB",
			ipFamilies: []v1.IPFamily{v1.IPv6Protocol},
		},
		{
			desc:       "Create and delete IPv6 IPv4 NetLB",
			ipFamilies: []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol},
		},
		{
			desc:       "Create and delete NetLB with empty IP families",
			ipFamilies: []v1.IPFamily{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			controller := newL4NetLBServiceController()
			controller.enableDualStack = true
			svc := test.NewL4NetLBRBSService(8080)
			svc.Spec.IPFamilies = tc.ipFamilies
			addNetLBService(controller, svc)

			test.MustCreateDualStackClusterSubnet(t, controller.ctx.Cloud, "EXTERNAL")

			prevMetrics, err := test.GetL4NetLBLatencyMetric()
			if err != nil {
				t.Errorf("Error getting L4 NetLB latency metrics err: %v", err)
			}
			if prevMetrics == nil {
				t.Fatalf("Cannot get prometheus metrics for L4NetLB latency")
			}

			key, _ := common.KeyFunc(svc)
			err = controller.sync(key, klog.TODO())
			if err != nil {
				t.Errorf("Failed to sync newly added service %s, err %v", svc.Name, err)
			}
			svc, err = controller.ctx.KubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to lookup service %s, err %v", svc.Name, err)
			}

			expectedIngressLength := len(tc.ipFamilies)
			// For empty IP Families we should provide ipv4 address
			if expectedIngressLength == 0 {
				expectedIngressLength = 1
			}
			if len(svc.Status.LoadBalancer.Ingress) != expectedIngressLength {
				t.Errorf("expectedIngressLength = %d, got %d", expectedIngressLength, len(svc.Status.LoadBalancer.Ingress))
			}

			err = validateAnnotations(svc)
			if err != nil {
				t.Errorf("validateAnnotations(%+v) returned error %v, want nil", svc, err)
			}
			deleteNetLBService(controller, svc)
		})
	}
}
func TestProcessDualStackNetLBServiceOnUserError(t *testing.T) {
	t.Parallel()
	controller := newL4NetLBServiceController()
	controller.enableDualStack = true
	svc := test.NewL4NetLBRBSService(8080)
	svc.Spec.IPFamilies = []v1.IPFamily{v1.IPv6Protocol, v1.IPv4Protocol}
	addNetLBService(controller, svc)

	// Create cluster subnet with INTERNAL ipv6 access type to trigger user error.
	test.MustCreateDualStackClusterSubnet(t, controller.ctx.Cloud, "INTERNAL")

	syncResult := controller.syncInternal(svc, klog.TODO())
	if syncResult.Error == nil {
		t.Fatalf("Failed to generate error when syncing service %s", svc.Name)
	}
	if !syncResult.MetricsLegacyState.IsUserError {
		t.Errorf("syncResult.MetricsLegacyState.IsUserError should be true, got false")
	}
	if syncResult.MetricsLegacyState.InSuccess {
		t.Errorf("syncResult.MetricsLegacyState.InSuccess should be false, got true")
	}
	if syncResult.MetricsState.Status != metrics.StatusUserError {
		t.Errorf("syncResult.MetricsState.Status should be %s, got %s", metrics.StatusUserError, syncResult.MetricsState.Status)
	}
}

// fakeNEGLinker is a fake to be used in tests in place of the NEGLinker.
type fakeNEGLinker struct {
	called bool
	sp     utils.ServicePort
	groups []backends.GroupKey
}

func (l *fakeNEGLinker) Link(sp utils.ServicePort, groups []backends.GroupKey) error {
	l.called = true
	l.sp = sp
	l.groups = groups
	return nil
}

func TestEnsureBackendLinkingWithNEGs(t *testing.T) {
	controller := newL4NetLBServiceController()
	linker := &fakeNEGLinker{}
	controller.negLinker = linker
	svc := test.NewL4NetLBRBSService(8080)

	err := controller.ensureBackendLinking(svc, negLink, klog.TODO())
	if err != nil {
		t.Fatalf("ensureBackendLinking() failed, err=%v", err)
	}
	namespacedName := types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}
	spID := utils.ServicePortID{Service: namespacedName}

	if diff := cmp.Diff(linker.sp.ID, spID); diff != "" {
		t.Errorf("ServicePort.ID mismatch (-want +got):\n%s", diff)
	}

	if !linker.sp.L4RBSEnabled {
		t.Errorf("RBS was not enabled in the Service Port, got=%+v", linker.sp)
	}

	if !linker.sp.VMIPNEGEnabled {
		t.Errorf("VMIPNEGEnabled was not enabled in the Service Port, got=%+v", linker.sp)
	}
}

func TestEnsureBackendLinkingWithInstanceGroups(t *testing.T) {
	controller := newL4NetLBServiceController()
	svc := test.NewL4NetLBRBSService(8080)
	// set the fake NEG linker just to verify that it's not used in this scenario.
	negLinker := &fakeNEGLinker{}
	controller.negLinker = negLinker

	backendService := &compute.BackendService{Name: controller.namer.L4Backend(svc.Namespace, svc.Name)}
	err := controller.ctx.Cloud.CreateRegionBackendService(backendService, "us-central1")
	if err != nil {
		t.Fatalf("CreateRegionBackendService() failed, err=%v", err)
	}

	err = controller.ensureBackendLinking(svc, instanceGroupLink, klog.TODO())
	if err != nil {
		t.Fatalf("ensureBackendLinking() failed, err=%v", err)
	}

	if negLinker.called {
		t.Errorf("IG linking should not use NEG linker")
	}
}
