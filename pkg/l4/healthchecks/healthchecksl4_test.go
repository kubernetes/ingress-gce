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
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

func TestMergeHealthChecks(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc                   string
		checkIntervalSec       int64
		timeoutSec             int64
		shared                 bool
		healthyThreshold       int64
		unhealthyThreshold     int64
		wantCheckIntervalSec   int64
		wantTimeoutSec         int64
		wantHealthyThreshold   int64
		wantUnhealthyThreshold int64
	}{
		{
			desc:                   "local - unchanged",
			checkIntervalSec:       gceLocalHcCheckIntervalSeconds,
			timeoutSec:             gceHcTimeoutSeconds,
			healthyThreshold:       gceHcHealthyThreshold,
			unhealthyThreshold:     gceLocalHcUnhealthyThreshold,
			wantCheckIntervalSec:   gceLocalHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceLocalHcUnhealthyThreshold,
		},
		{
			desc:                   "local - interval - too small - should reconcile",
			checkIntervalSec:       gceLocalHcCheckIntervalSeconds - 1,
			timeoutSec:             gceHcTimeoutSeconds,
			healthyThreshold:       gceHcHealthyThreshold,
			unhealthyThreshold:     gceLocalHcUnhealthyThreshold,
			wantCheckIntervalSec:   gceLocalHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceLocalHcUnhealthyThreshold,
		},
		{
			desc:                   "local - timeout - too small - should reconcile",
			checkIntervalSec:       gceLocalHcCheckIntervalSeconds,
			timeoutSec:             gceHcTimeoutSeconds - 1,
			healthyThreshold:       gceHcHealthyThreshold,
			unhealthyThreshold:     gceLocalHcUnhealthyThreshold,
			wantCheckIntervalSec:   gceLocalHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceLocalHcUnhealthyThreshold,
		},
		{
			desc:                   "local - healthy threshold - too small - should reconcil",
			checkIntervalSec:       gceLocalHcCheckIntervalSeconds,
			timeoutSec:             gceHcTimeoutSeconds,
			healthyThreshold:       gceHcHealthyThreshold - 1,
			unhealthyThreshold:     gceLocalHcUnhealthyThreshold,
			wantCheckIntervalSec:   gceLocalHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceLocalHcUnhealthyThreshold,
		},
		{
			desc:                   "local - unhealthy threshold - too small - should reconcile",
			checkIntervalSec:       gceLocalHcCheckIntervalSeconds,
			timeoutSec:             gceHcTimeoutSeconds,
			healthyThreshold:       gceHcHealthyThreshold,
			unhealthyThreshold:     gceLocalHcUnhealthyThreshold - 1,
			wantCheckIntervalSec:   gceLocalHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceLocalHcUnhealthyThreshold,
		},
		{
			desc:                   "local - interval - user configured - should keep",
			checkIntervalSec:       gceLocalHcCheckIntervalSeconds + 1,
			timeoutSec:             gceHcTimeoutSeconds,
			healthyThreshold:       gceHcHealthyThreshold,
			unhealthyThreshold:     gceLocalHcUnhealthyThreshold,
			wantCheckIntervalSec:   gceLocalHcCheckIntervalSeconds + 1,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceLocalHcUnhealthyThreshold,
		},
		{
			desc:                   "local - timeout - user configured - should keep",
			checkIntervalSec:       gceLocalHcCheckIntervalSeconds,
			timeoutSec:             gceHcTimeoutSeconds + 1,
			healthyThreshold:       gceHcHealthyThreshold,
			unhealthyThreshold:     gceLocalHcUnhealthyThreshold,
			wantCheckIntervalSec:   gceLocalHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds + 1,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceLocalHcUnhealthyThreshold,
		},
		{
			desc:                   "local - healthy threshold - user configured - should keep",
			checkIntervalSec:       gceLocalHcCheckIntervalSeconds,
			timeoutSec:             gceHcTimeoutSeconds,
			healthyThreshold:       gceHcHealthyThreshold + 1,
			unhealthyThreshold:     gceLocalHcUnhealthyThreshold,
			wantCheckIntervalSec:   gceLocalHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold + 1,
			wantUnhealthyThreshold: gceLocalHcUnhealthyThreshold,
		},
		{
			desc:                   "local - unhealthy threshold - user configured - should keep",
			checkIntervalSec:       gceLocalHcCheckIntervalSeconds,
			timeoutSec:             gceHcTimeoutSeconds,
			healthyThreshold:       gceHcHealthyThreshold,
			unhealthyThreshold:     gceLocalHcUnhealthyThreshold + 1,
			wantCheckIntervalSec:   gceLocalHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceLocalHcUnhealthyThreshold + 1,
		},
		{
			desc:                   "shared - unchanged",
			checkIntervalSec:       gceLocalHcCheckIntervalSeconds,
			timeoutSec:             gceHcTimeoutSeconds,
			healthyThreshold:       gceHcHealthyThreshold,
			unhealthyThreshold:     gceSharedHcUnhealthyThreshold,
			wantCheckIntervalSec:   gceLocalHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceSharedHcUnhealthyThreshold,
		},
		{
			desc:                   "shared - old values - new values",
			checkIntervalSec:       gceLocalHcCheckIntervalSeconds,
			timeoutSec:             gceHcTimeoutSeconds,
			healthyThreshold:       gceHcHealthyThreshold,
			unhealthyThreshold:     gceLocalHcUnhealthyThreshold,
			shared:                 true,
			wantCheckIntervalSec:   gceSharedHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceSharedHcUnhealthyThreshold,
		},
		{
			desc:                   "shared - interval - too small - should reconcile",
			checkIntervalSec:       gceSharedHcCheckIntervalSeconds - 1,
			timeoutSec:             gceHcTimeoutSeconds,
			healthyThreshold:       gceHcHealthyThreshold,
			unhealthyThreshold:     gceSharedHcUnhealthyThreshold,
			shared:                 true,
			wantCheckIntervalSec:   gceSharedHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceSharedHcUnhealthyThreshold,
		},
		{
			desc:                   "shared - timeout - too small - should reconcile",
			checkIntervalSec:       gceSharedHcCheckIntervalSeconds,
			timeoutSec:             gceHcTimeoutSeconds - 1,
			healthyThreshold:       gceHcHealthyThreshold,
			unhealthyThreshold:     gceSharedHcUnhealthyThreshold,
			shared:                 true,
			wantCheckIntervalSec:   gceSharedHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceSharedHcUnhealthyThreshold,
		},
		{
			desc:                   "shared - healthy threshold - too small - should reconcil",
			checkIntervalSec:       gceSharedHcCheckIntervalSeconds,
			timeoutSec:             gceHcTimeoutSeconds,
			healthyThreshold:       gceHcHealthyThreshold - 1,
			unhealthyThreshold:     gceSharedHcUnhealthyThreshold,
			shared:                 true,
			wantCheckIntervalSec:   gceSharedHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceSharedHcUnhealthyThreshold,
		},
		{
			desc:                   "shared - unhealthy threshold - too small - should reconcile",
			checkIntervalSec:       gceSharedHcCheckIntervalSeconds,
			timeoutSec:             gceHcTimeoutSeconds,
			healthyThreshold:       gceHcHealthyThreshold,
			unhealthyThreshold:     gceSharedHcUnhealthyThreshold - 1,
			shared:                 true,
			wantCheckIntervalSec:   gceSharedHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceSharedHcUnhealthyThreshold,
		},
		{
			desc:                   "shared - interval - user configured - should keep",
			checkIntervalSec:       gceSharedHcCheckIntervalSeconds + 1,
			timeoutSec:             gceHcTimeoutSeconds,
			healthyThreshold:       gceHcHealthyThreshold,
			unhealthyThreshold:     gceSharedHcUnhealthyThreshold,
			shared:                 true,
			wantCheckIntervalSec:   gceSharedHcCheckIntervalSeconds + 1,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceSharedHcUnhealthyThreshold,
		},
		{
			desc:                   "shared - timeout - user configured - should keep",
			checkIntervalSec:       gceSharedHcCheckIntervalSeconds,
			timeoutSec:             gceHcTimeoutSeconds + 1,
			healthyThreshold:       gceHcHealthyThreshold,
			unhealthyThreshold:     gceSharedHcUnhealthyThreshold,
			shared:                 true,
			wantCheckIntervalSec:   gceSharedHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds + 1,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceSharedHcUnhealthyThreshold,
		},
		{
			desc:                   "shared - healthy threshold - user configured - should keep",
			checkIntervalSec:       gceSharedHcCheckIntervalSeconds,
			timeoutSec:             gceHcTimeoutSeconds,
			healthyThreshold:       gceHcHealthyThreshold + 1,
			unhealthyThreshold:     gceSharedHcUnhealthyThreshold,
			shared:                 true,
			wantCheckIntervalSec:   gceSharedHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold + 1,
			wantUnhealthyThreshold: gceSharedHcUnhealthyThreshold,
		},
		{
			desc:                   "shared - unhealthy threshold - user configured - should keep",
			checkIntervalSec:       gceSharedHcCheckIntervalSeconds,
			timeoutSec:             gceHcTimeoutSeconds,
			healthyThreshold:       gceHcHealthyThreshold,
			unhealthyThreshold:     gceSharedHcUnhealthyThreshold + 1,
			shared:                 true,
			wantCheckIntervalSec:   gceSharedHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceSharedHcUnhealthyThreshold + 1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			// healthcheck intervals and thresholds are common for Global and Regional healthchecks. Hence testing only Global case.
			wantHC := newL4HealthCheck("hc", types.NamespacedName{Name: "svc", Namespace: "default"}, tc.shared, "/", 12345, utils.ILB, meta.Global, "", klog.TODO())
			hc := &composite.HealthCheck{
				CheckIntervalSec:   tc.checkIntervalSec,
				TimeoutSec:         tc.timeoutSec,
				HealthyThreshold:   tc.healthyThreshold,
				UnhealthyThreshold: tc.unhealthyThreshold,
			}
			mergeHealthChecks(hc, wantHC)
			if wantHC.CheckIntervalSec != tc.wantCheckIntervalSec {
				t.Errorf("wantHC.CheckIntervalSec = %d; want %d", wantHC.CheckIntervalSec, tc.checkIntervalSec)
			}
			if wantHC.TimeoutSec != tc.wantTimeoutSec {
				t.Errorf("wantHC.TimeoutSec = %d; want %d", wantHC.TimeoutSec, tc.timeoutSec)
			}
			if wantHC.HealthyThreshold != tc.wantHealthyThreshold {
				t.Errorf("wantHC.HealthyThreshold = %d; want %d", wantHC.HealthyThreshold, tc.healthyThreshold)
			}
			if wantHC.UnhealthyThreshold != tc.wantUnhealthyThreshold {
				t.Errorf("wantHC.UnhealthyThreshold = %d; want %d", wantHC.UnhealthyThreshold, tc.unhealthyThreshold)
			}
		})
	}
}

func TestCompareHealthChecks(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc        string
		modifier    func(*composite.HealthCheck)
		wantChanged bool
	}{
		{
			desc:        "unchanged",
			modifier:    nil,
			wantChanged: false,
		},
		{
			desc:        "nil HttpHealthCheck",
			modifier:    func(hc *composite.HealthCheck) { hc.HttpHealthCheck = nil },
			wantChanged: true,
		},
		{
			desc:        "desc does not match",
			modifier:    func(hc *composite.HealthCheck) { hc.Description = "bad-desc" },
			wantChanged: true,
		},
		{
			desc:        "port does not match",
			modifier:    func(hc *composite.HealthCheck) { hc.HttpHealthCheck.Port = 54321 },
			wantChanged: true,
		},
		{
			desc:        "requestPath does not match",
			modifier:    func(hc *composite.HealthCheck) { hc.HttpHealthCheck.RequestPath = "/anotherone" },
			wantChanged: true,
		},
		{
			desc:        "interval needs update",
			modifier:    func(hc *composite.HealthCheck) { hc.CheckIntervalSec = gceLocalHcCheckIntervalSeconds - 1 },
			wantChanged: true,
		},
		{
			desc:        "timeout needs update",
			modifier:    func(hc *composite.HealthCheck) { hc.TimeoutSec = gceHcTimeoutSeconds - 1 },
			wantChanged: true,
		},
		{
			desc:        "healthy threshold needs update",
			modifier:    func(hc *composite.HealthCheck) { hc.HealthyThreshold = gceHcHealthyThreshold - 1 },
			wantChanged: true,
		},
		{
			desc:        "unhealthy threshold needs update",
			modifier:    func(hc *composite.HealthCheck) { hc.UnhealthyThreshold = gceLocalHcUnhealthyThreshold - 1 },
			wantChanged: true,
		},
		{
			desc:        "interval does not need update",
			modifier:    func(hc *composite.HealthCheck) { hc.CheckIntervalSec = gceLocalHcCheckIntervalSeconds + 1 },
			wantChanged: false,
		},
		{
			desc:        "timeout does not need update",
			modifier:    func(hc *composite.HealthCheck) { hc.TimeoutSec = gceHcTimeoutSeconds + 1 },
			wantChanged: false,
		},
		{
			desc:        "healthy threshold does not need update",
			modifier:    func(hc *composite.HealthCheck) { hc.HealthyThreshold = gceHcHealthyThreshold + 1 },
			wantChanged: false,
		},
		{
			desc:        "unhealthy threshold does not need update",
			modifier:    func(hc *composite.HealthCheck) { hc.UnhealthyThreshold = gceLocalHcUnhealthyThreshold + 1 },
			wantChanged: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			// healthcheck intervals and thresholds are common for Global and Regional healthchecks. Hence testing only Global case.
			hc := newL4HealthCheck("hc", types.NamespacedName{Name: "svc", Namespace: "default"}, false, "/", 12345, utils.ILB, meta.Global, "", klog.TODO())
			wantHC := newL4HealthCheck("hc", types.NamespacedName{Name: "svc", Namespace: "default"}, false, "/", 12345, utils.ILB, meta.Global, "", klog.TODO())
			if tc.modifier != nil {
				tc.modifier(hc)
			}
			if gotChanged := needToUpdateHealthChecks(hc, wantHC); gotChanged != tc.wantChanged {
				t.Errorf("needToUpdateHealthChecks(%#v, %#v) = %t; want changed = %t", hc, wantHC, gotChanged, tc.wantChanged)
			}
		})
	}
}

// Checks that newL4HealthCheck() returns correct CheckInterval and UnhealthyThreshold
func TestSharedHealthChecks(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc                   string
		shared                 bool
		wantCheckIntervalSec   int64
		wantUnhealthyThreshold int64
	}{
		{
			desc:                   "shared - check interval and unhealthy threshold",
			shared:                 true,
			wantCheckIntervalSec:   gceSharedHcCheckIntervalSeconds,
			wantUnhealthyThreshold: gceSharedHcUnhealthyThreshold,
		},
		{
			desc:                   "local - check interval and unhealthy threshold",
			shared:                 false,
			wantCheckIntervalSec:   gceLocalHcCheckIntervalSeconds,
			wantUnhealthyThreshold: gceLocalHcUnhealthyThreshold,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			// healthcheck intervals and thresholds are common for Global and Regional healthchecks. Hence testing only Global case.
			gotHC := newL4HealthCheck("hc", types.NamespacedName{Name: "svc", Namespace: "default"}, tc.shared, "/", 12345, utils.ILB, meta.Global, "", klog.TODO())
			if gotHC.CheckIntervalSec != tc.wantCheckIntervalSec {
				t.Errorf("gotHC.CheckIntervalSec = %d; want %d", gotHC.CheckIntervalSec, tc.wantCheckIntervalSec)
			}
			if gotHC.UnhealthyThreshold != tc.wantUnhealthyThreshold {
				t.Errorf("gotHC.UnhealthyThreshold = %d; want %d", gotHC.UnhealthyThreshold, tc.wantUnhealthyThreshold)
			}
		})
	}
}

func TestNewHealthCheck(t *testing.T) {
	t.Parallel()
	namespaceName := types.NamespacedName{Name: "svc", Namespace: "default"}

	for _, v := range []struct {
		scope  meta.KeyType
		region string
	}{
		{meta.Global, ""},
		{meta.Regional, "us-central1"},
	} {
		hc := newL4HealthCheck("hc", namespaceName, false, "/", 12345, utils.ILB, v.scope, v.region, klog.TODO())
		if hc.Region != v.region {
			t.Errorf("HealthCheck Region mismatch! %v != %v", hc.Region, v.region)
		}
		if hc.Scope != v.scope {
			t.Errorf("HealthCheck Scope mismatch! %v != %v", hc.Scope, v.scope)
		}
	}
}

func TestEnsureHealthCheck(t *testing.T) {
	namespacedName := types.NamespacedName{Name: "svcName", Namespace: "svcNamespace"}
	testClusterValues := gce.DefaultTestClusterValues()
	hcName := "testHCname"
	hcDefaultPath := "/healthz"
	testCases := []struct {
		desc       string
		existingHC *composite.HealthCheck
		port       int32
		shared     bool
		scope      meta.KeyType
		l4Type     utils.L4LBType
		wantHC     *composite.HealthCheck
		wantUpdate utils.ResourceSyncStatus
	}{
		{
			desc:       "create for XLB",
			existingHC: nil,
			port:       80,
			shared:     false,
			scope:      meta.Global,
			l4Type:     utils.XLB,
			wantHC:     newL4HealthCheck(hcName, namespacedName, false, hcDefaultPath, 80, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantUpdate: utils.ResourceUpdate,
		},
		{
			desc:       "create for ILB",
			existingHC: nil,
			port:       80,
			shared:     true,
			scope:      meta.Regional,
			l4Type:     utils.ILB,
			wantHC:     newL4HealthCheck(hcName, namespacedName, true, hcDefaultPath, 80, utils.ILB, meta.Regional, testClusterValues.Region, klog.TODO()),
			wantUpdate: utils.ResourceUpdate,
		},
		{
			desc:       "no update",
			existingHC: newL4HealthCheck(hcName, namespacedName, false, hcDefaultPath, 80, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			port:       80,
			shared:     false,
			scope:      meta.Global,
			l4Type:     utils.XLB,
			wantHC:     newL4HealthCheck(hcName, namespacedName, false, hcDefaultPath, 80, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantUpdate: utils.ResourceResync,
		},
		{
			desc:       "update ILB to XLB",
			existingHC: newL4HealthCheck(hcName, namespacedName, false, hcDefaultPath, 80, utils.ILB, meta.Regional, testClusterValues.Region, klog.TODO()),
			port:       80,
			shared:     false,
			scope:      meta.Global,
			l4Type:     utils.XLB,
			wantHC:     newL4HealthCheck(hcName, namespacedName, false, hcDefaultPath, 80, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantUpdate: utils.ResourceUpdate,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true)

			if tc.existingHC != nil {
				err := hcs.hcProvider.Create(tc.existingHC)
				if err != nil {
					t.Errorf("hcProvider.Create() err=%v", err)
				}
			}

			_, updated, err := hcs.ensureHealthCheck(hcName, namespacedName, tc.shared, hcDefaultPath, tc.port, tc.scope, tc.l4Type, klog.TODO())
			if err != nil {
				t.Errorf("ensureHealthCheck() err=%v", err)
			}
			if updated != tc.wantUpdate {
				t.Errorf("ensureHealthCheck() unexpected 'updated' value, want=%v, got=%v", tc.wantUpdate, updated)
			}
			resultHC, err := hcs.hcProvider.Get(hcName, tc.scope)
			if err != nil {
				t.Errorf("hcProvider.Get() err=-%v", err)
			}
			if diff := cmp.Diff(tc.wantHC, resultHC, cmpopts.IgnoreFields(composite.HealthCheck{}, "SelfLink", "Region", "Scope", "Version")); diff != "" {
				t.Errorf("created HC differs: diff -want +got\n%v\n", diff)
			}
		})
	}
}

func TestEnsureHealthCheckWithDualStackFirewalls(t *testing.T) {
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))
	testClusterValues := gce.DefaultTestClusterValues()
	hcDefaultPath := "/healthz"
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "serviceName", Namespace: "serviceNamespace", UID: types.UID("1")},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:     8080,
					Protocol: corev1.ProtocolTCP,
				},
			},
			Type:                  "LoadBalancer",
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
			HealthCheckNodePort:   1234,
		},
	}
	namespacedName := types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}
	fwDescription, err := utils.MakeL4LBFirewallDescription(utils.ServiceKeyFunc(svc.Namespace, svc.Name), "", meta.VersionGA, false)
	if err != nil {
		t.Errorf("utils.MakeL4LBFirewallDescription() err=%v", err)
	}
	expectedFw := &compute.Firewall{
		Name:         l4Namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, false),
		Description:  fwDescription,
		Network:      testClusterValues.NetworkURL,
		SourceRanges: gce.L4LoadBalancerSrcRanges(),
		TargetTags:   []string{"k8s-test"},
		Allowed: []*compute.FirewallAllowed{
			{
				IPProtocol: "TCP",
				Ports:      []string{"1234"},
			},
		},
		Priority: 999,
	}
	fwThatNeedsUpdate := &compute.Firewall{
		Name:         l4Namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, false),
		Description:  fwDescription,
		Network:      testClusterValues.NetworkURL,
		SourceRanges: []string{"10.0.0.0/16"},
		TargetTags:   []string{"k8s-test"},
		Allowed: []*compute.FirewallAllowed{
			{
				IPProtocol: "TCP",
				Ports:      []string{"1234"},
			},
		},
		Priority: 999,
	}

	testCases := []struct {
		desc             string
		existingHC       *composite.HealthCheck
		existingFirewall *compute.Firewall
		svc              *corev1.Service
		wantHC           *composite.HealthCheck
		wantFirewall     *compute.Firewall
		needIPv6         bool
		wantUpdate       utils.ResourceSyncStatus
		wantUpdateFw     utils.ResourceSyncStatus
	}{
		{
			desc:         "create",
			svc:          svc,
			wantHC:       newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false), namespacedName, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantFirewall: expectedFw,
			wantUpdate:   utils.ResourceUpdate,
			wantUpdateFw: utils.ResourceUpdate,
		},
		{
			desc:             "no update",
			svc:              svc,
			existingHC:       newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false), namespacedName, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			existingFirewall: expectedFw,
			wantHC:           newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false), namespacedName, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantFirewall:     expectedFw,
			wantUpdate:       utils.ResourceResync,
			wantUpdateFw:     utils.ResourceResync,
		},
		{
			desc:             "update only FW",
			svc:              svc,
			existingHC:       newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false), namespacedName, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			existingFirewall: fwThatNeedsUpdate,
			wantHC:           newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false), namespacedName, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantFirewall:     expectedFw,
			wantUpdate:       utils.ResourceResync,
			wantUpdateFw:     utils.ResourceUpdate,
		},
		{
			desc:             "add ipv6",
			svc:              svc,
			existingHC:       newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false), namespacedName, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			existingFirewall: expectedFw,
			needIPv6:         true,
			wantHC:           newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false), namespacedName, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantFirewall:     expectedFw,
			wantUpdate:       utils.ResourceResync,
			wantUpdateFw:     utils.ResourceUpdate,
		},
		{
			desc:       "change fw priority to 999",
			svc:        svc,
			existingHC: newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false), namespacedName, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			existingFirewall: &compute.Firewall{
				Name:         l4Namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, false),
				Description:  fwDescription,
				Network:      testClusterValues.NetworkURL,
				SourceRanges: gce.L4LoadBalancerSrcRanges(),
				TargetTags:   []string{"k8s-test"},
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"1234"},
					},
				},
				Priority: 1000,
			},
			wantHC:       newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false), namespacedName, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantFirewall: expectedFw,
			wantUpdate:   utils.ResourceResync,
			wantUpdateFw: utils.ResourceUpdate,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(testClusterValues)
			nodeNames := []string{"k8s-test-node"}
			createVMInstanceWithTag(t, fakeGCE, "k8s-test")
			defaultNetwork := network.DefaultNetwork(fakeGCE)
			hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true)
			if tc.existingHC != nil {
				err := hcs.hcProvider.Create(tc.existingHC)
				if err != nil {
					t.Errorf("hcProvider.Create() err=%v", err)
				}
			}
			if tc.existingFirewall != nil {
				fakeGCE.CreateFirewall(tc.existingFirewall)
			}
			mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
			mockGCE.MockFirewalls.PatchHook = mock.UpdateFirewallHook

			result := hcs.EnsureHealthCheckWithDualStackFirewalls(svc, l4Namer, false, meta.Global, utils.XLB, nodeNames, true, tc.needIPv6, *defaultNetwork, klog.TODO())
			if result.Err != nil {
				t.Errorf("hcs.EnsureHealthCheckWithDualStackFirewalls() err=%v", result.Err)
			}
			if result.WasUpdated != tc.wantUpdate {
				t.Errorf("result.WasUpdated want=%v, got=%v", tc.wantUpdate, result.WasUpdated)
			}
			if result.WasFirewallUpdated != tc.wantUpdateFw {
				t.Errorf("result.WasFirewallUpdated want=%v, got=%v", tc.wantUpdateFw, result.WasFirewallUpdated)
			}
			resultHC, err := hcs.hcProvider.Get(result.HCName, meta.Global)
			if err != nil {
				t.Errorf("hcProvider.Get() err=-%v", err)
			}
			if diff := cmp.Diff(tc.wantHC, resultHC, cmpopts.IgnoreFields(composite.HealthCheck{}, "SelfLink", "Region", "Scope", "Version")); diff != "" {
				t.Errorf("created HC differs: diff -want +got\n%v\n", diff)
			}
			firewall, err := fakeGCE.GetFirewall(result.HCFirewallRuleName)
			if err != nil {
				t.Errorf("GetFirewall() err=-%v", err)
			}
			if diff := cmp.Diff(tc.wantFirewall, firewall, cmpopts.IgnoreFields(compute.Firewall{}, "SelfLink", "SourceRanges")); diff != "" {
				t.Errorf("created Firewall differs: diff -want +got\n%v\n", diff)
			}
		})
	}
}

func createVMInstanceWithTag(t *testing.T, fakeGCE *gce.Cloud, tag string) {
	err := fakeGCE.Compute().Instances().Insert(context.Background(),
		meta.ZonalKey("k8s-test-node", fakeGCE.LocalZone()),
		&compute.Instance{
			Name: "test-node",
			Zone: "us-central1-b",
			Tags: &compute.Tags{
				Items: []string{tag},
			},
		})
	if err != nil {
		t.Errorf("failed to create instance err=%v", err)
	}
}
