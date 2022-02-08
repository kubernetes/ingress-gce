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

package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/utils/common"
)

const (
	replicas = int32(1)
)

// Test scenario
// 1. Create 2 Services with ExternalTrafficPolicy folowing Cluster and Local.
// 2. Verify that they do not share health check.
// 3. Update Service ExternalTrafficPolicy Local to Cluster.
// 4. Verify that they share health check.
// 5. Delete one service.
// 6. Verify that health check was not deleted.
// 7. Update Service from Cluster to Local.
// 8. Delete second service.
// 9. Verify that non-shared health check was deleted.
func TestL4NetLbCreateAndUpdate(t *testing.T) {
	t.Parallel()

	Framework.RunWithSandbox("L4NetLbCreateUpdate", t, func(t *testing.T, s *e2e.Sandbox) {
		svcName := fmt.Sprintf("netlb1-%x", s.RandInt)
		rbsEnabledAnn := map[string]string{annotations.RBSAnnotationKey: annotations.RBSEnabled}
		if _, err := e2e.EnsureL4NetLBEchoService(s, svcName, rbsEnabledAnn, replicas, v1.ServiceExternalTrafficPolicyTypeCluster); err != nil {
			t.Fatalf("Error ensuring echo service %s/%s: %q", s.Namespace, svcName, err)
		}
		svcName2 := fmt.Sprintf("netlb2-%x", s.RandInt)
		if _, err := e2e.EnsureL4NetLBEchoService(s, svcName2, rbsEnabledAnn, replicas, v1.ServiceExternalTrafficPolicyTypeLocal); err != nil {
			t.Fatalf("Error ensuring echo service %s/%s: %q", s.Namespace, svcName2, err)
		}
		ensureL4LbRunning(s, svcName, common.NetLBFinalizerV2, t)
		ensureL4LbRunning(s, svcName2, common.NetLBFinalizerV2, t)

		_, svc1Params := validateNetLbService(s, svcName, t)
		if svc1Params.ExternalTrafficPolicy != string(v1.ServiceExternalTrafficPolicyTypeCluster) {
			t.Fatalf("Service %s ExternalTrafficPolicy should be Cluster.", svcName)
		}
		_, svc2ParamsLocal := validateNetLbService(s, svcName2, t)
		if svc2ParamsLocal.ExternalTrafficPolicy != string(v1.ServiceExternalTrafficPolicyTypeLocal) {
			t.Fatalf("Service %s ExternalTrafficPolicy should be Local.", svcName2)
		}
		if svc1Params.HcName == svc2ParamsLocal.HcName {
			t.Fatalf("Services should not share health checks")
		}

		// Update Service ExternalTrafficPolicy to Cluster
		if _, err := e2e.EnsureL4NetLBEchoService(s, svcName2, nil, replicas, v1.ServiceExternalTrafficPolicyTypeCluster); err != nil {
			t.Fatalf("Error updating echo service %s/%s: %q", s.Namespace, svcName2, err)
		}

		// Wait for service update from Local to Cluster
		if err := e2e.WaitForSvcHealthCheckUpdate(s, svcName2, svc2ParamsLocal.HcName); err != nil {
			t.Fatalf("Error waiting service health check %s to change err: %v", svc2ParamsLocal.HcName, err)
		}
		t.Logf("Check service after Update")
		_, svc2ParamsCluster := validateNetLbService(s, svcName2, t)
		if svc2ParamsCluster.ExternalTrafficPolicy != string(v1.ServiceExternalTrafficPolicyTypeCluster) {
			t.Fatalf("Service %s ExternalTrafficPolicy should be Cluster.", svcName2)
		}
		if svc1Params.HcName != svc2ParamsCluster.HcName {
			t.Fatalf("Services should share health checks")
		}

		// Delete one service and check that shared health check was not deleted
		e2e.DeleteEchoService(s, svcName)
		if err := e2e.WaitForNetLBServiceDeletion(s, Framework.Cloud, svc1Params); !isGetServiceError(err) {
			t.Fatalf("Expected health check deleted error when checking L4 NetLB deleted err:%v", err)
		}
		validateNetLbService(s, svcName2, t)

		// Add service again and verify that regional HC is working
		if _, err := e2e.EnsureL4NetLBEchoService(s, svcName, rbsEnabledAnn, replicas, v1.ServiceExternalTrafficPolicyTypeCluster); err != nil {
			t.Fatalf("Error ensuring echo service %s/%s: %q", s.Namespace, svcName, err)
		}
		ensureL4LbRunning(s, svcName, common.NetLBFinalizerV2, t)
		validateNetLbService(s, svcName, t)

		// Delete second service and check that non-shared health check was deleted
		e2e.DeleteEchoService(s, svcName2)
		if err := e2e.WaitForNetLBServiceDeletion(s, Framework.Cloud, svc2ParamsLocal); !isGetServiceError(err) {
			t.Fatalf("Unexpected error when checking L4 NetLB deleted err:%v", err)
		}
	})
}

func TestL4NetLbServicesNetworkTierAnnotationChanged(t *testing.T) {
	t.SkipNow()
	t.Parallel()

	Framework.RunWithSandbox("L4NetLbNetworkTier", t, func(t *testing.T, s *e2e.Sandbox) {
		replicas := int32(1)
		svcName := fmt.Sprintf("netlb-network-tier-%x", s.RandInt)
		rbsStandardTierAnn := map[string]string{annotations.RBSAnnotationKey: annotations.RBSEnabled, annotations.NetworkTierAnnotationKey: string(cloud.NetworkTierStandard)}
		if _, err := e2e.EnsureL4NetLBEchoService(s, svcName, rbsStandardTierAnn, replicas, v1.ServiceExternalTrafficPolicyTypeCluster); err != nil {
			t.Fatalf("error ensuring echo service %s/%s: %q", s.Namespace, svcName, err)
		}
		ensureL4LbRunning(s, svcName, common.NetLBFinalizerV2, t)
		l4netlb, err := validateNetLbService(s, svcName, t)
		if err != nil {
			t.Errorf("Error validating NetLB params err: %v", err)
		}
		if l4netlb.ForwardingRule.GA.NetworkTier != cloud.NetworkTierStandard.ToGCEValue() {
			t.Fatalf("Network tier mismatch %v != %v", l4netlb.ForwardingRule.GA.NetworkTier, cloud.NetworkTierStandard.ToGCEValue())
		}
		// Update network tier to premium
		rbsPremiumTierAnn := map[string]string{annotations.RBSAnnotationKey: annotations.RBSEnabled, annotations.NetworkTierAnnotationKey: string(cloud.NetworkTierPremium)}
		if _, err := e2e.EnsureL4NetLBEchoService(s, svcName, rbsPremiumTierAnn, replicas, v1.ServiceExternalTrafficPolicyTypeCluster); err != nil {
			t.Fatalf("error ensuring echo service %s/%s: %q", s.Namespace, svcName, err)
		}
		ensureL4LbRunning(s, svcName, common.NetLBFinalizerV2, t)
		l4netlb, err = validateNetLbService(s, svcName, t)
		if err != nil {
			t.Errorf("Error validating NetLB params err: %v", err)
		}
		if l4netlb.ForwardingRule.GA.NetworkTier != cloud.NetworkTierPremium.ToGCEValue() {
			t.Fatalf("Network tier mismatch %v != %v", l4netlb.ForwardingRule.GA.NetworkTier, cloud.NetworkTierPremium.ToGCEValue())
		}
	})
}

func isHealthCheckDeletedError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Error health check should be deleted")
}

func isGetServiceError(err error) bool {
	return err != nil || strings.Contains(err.Error(), "Failed to get service")
}

func validateNetLbService(s *e2e.Sandbox, svcName string, t *testing.T) (*fuzz.L4NetLB, *fuzz.L4LBForSvcParams) {
	svcParams, err := e2e.GetL4LBForSvcParams(s, svcName)
	if err != nil {
		t.Fatalf("Error getting svc params: Err %q", err)
	}
	svcParams.Region = Framework.Region
	svcParams.Network = Framework.Network
	t.Logf("Service: %s", svcName)
	t.Logf("Health check name: %s", svcParams.HcName)
	t.Logf("Health check firewall rule name: %s", svcParams.HcFwRuleName)
	t.Logf("Backend name: %s", svcParams.BsName)
	t.Logf("Forwarding rule name: %s", svcParams.FwrName)
	t.Logf("Forwarding rule firewall name: %s", svcParams.FwRuleName)
	t.Logf("Region: %s", svcParams.Region)
	t.Logf("VIP: %s", svcParams.VIP)
	l4netlb, err := fuzz.GetRegionalL4NetLBForService(context.Background(), Framework.Cloud, svcParams)
	if err != nil {
		t.Fatalf("Error checking L4 NetLB resources %v:", err)
	}
	return l4netlb, svcParams
}

func ensureL4LbRunning(s *e2e.Sandbox, svcName, finalizer string, t *testing.T) {
	if err := e2e.WaitForEchoDeploymentStable(s, svcName); err != nil {
		t.Errorf("Echo deployment failed to become stable: %v", err)
	}
	t.Logf("ensured echo service %s/%s", s.Namespace, svcName)
	if err := e2e.WaitForSvcFinalilzer(s, svcName, finalizer); err != nil {
		t.Fatalf("Errore waiting for svc finalizer: %q", err)
	}
	t.Logf("finalizer is present %s/%s", s.Namespace, svcName)
	if err := e2e.WaitForNetLbAnnotations(s, svcName); err != nil {
		t.Fatalf("Expected service annotations")
	}
}
