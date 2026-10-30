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
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"k8s.io/ingress-gce/pkg/flags"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4/address"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
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
		wantUpdate l4utils.ResourceSyncStatus
	}{
		{
			desc:       "create for XLB",
			existingHC: nil,
			port:       80,
			shared:     false,
			scope:      meta.Global,
			l4Type:     utils.XLB,
			wantHC:     newL4HealthCheck(hcName, namespacedName, false, hcDefaultPath, 80, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantUpdate: l4utils.ResourceUpdate,
		},
		{
			desc:       "create for ILB",
			existingHC: nil,
			port:       80,
			shared:     true,
			scope:      meta.Regional,
			l4Type:     utils.ILB,
			wantHC:     newL4HealthCheck(hcName, namespacedName, true, hcDefaultPath, 80, utils.ILB, meta.Regional, testClusterValues.Region, klog.TODO()),
			wantUpdate: l4utils.ResourceUpdate,
		},
		{
			desc:       "no update",
			existingHC: newL4HealthCheck(hcName, namespacedName, false, hcDefaultPath, 80, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			port:       80,
			shared:     false,
			scope:      meta.Global,
			l4Type:     utils.XLB,
			wantHC:     newL4HealthCheck(hcName, namespacedName, false, hcDefaultPath, 80, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantUpdate: l4utils.ResourceResync,
		},
		{
			desc:       "update ILB to XLB",
			existingHC: newL4HealthCheck(hcName, namespacedName, false, hcDefaultPath, 80, utils.ILB, meta.Regional, testClusterValues.Region, klog.TODO()),
			port:       80,
			shared:     false,
			scope:      meta.Global,
			l4Type:     utils.XLB,
			wantHC:     newL4HealthCheck(hcName, namespacedName, false, hcDefaultPath, 80, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantUpdate: l4utils.ResourceUpdate,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, false)

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
	svcETPCluster := svc.DeepCopy()
	svcETPCluster.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyCluster
	svcETPCluster.Spec.HealthCheckNodePort = 0
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
	fwSharedDescription, err := utils.MakeL4LBFirewallDescription(utils.ServiceKeyFunc(svc.Namespace, svc.Name), "", meta.VersionGA, true)
	if err != nil {
		t.Errorf("utils.MakeL4LBFirewallDescription() err=%v", err)
	}
	expectedSharedFw := &compute.Firewall{
		Name:         l4Namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, true),
		Description:  fwSharedDescription,
		Network:      testClusterValues.NetworkURL,
		SourceRanges: gce.L4LoadBalancerSrcRanges(),
		TargetTags:   []string{"k8s-test"},
		Allowed: []*compute.FirewallAllowed{
			{
				IPProtocol: "TCP",
				Ports:      []string{strconv.Itoa(int(gce.GetNodesHealthCheckPort()))},
			},
		},
		Priority: 999,
	}

	secondaryNetwork := &network.NetworkInfo{IsDefault: false, K8sNetwork: "secondary", NetworkURL: "secondaryNetworkURL", SubnetworkURL: "secondarySubnetworkURL"}
	expectedMultinetFw := &compute.Firewall{
		Name:         l4Namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, false),
		Description:  fwDescription,
		Network:      secondaryNetwork.NetworkURL,
		SourceRanges: gce.L4LoadBalancerSrcRanges(),
		TargetTags:   []string{"k8s-test"},
		Allowed: []*compute.FirewallAllowed{
			{
				IPProtocol: "TCP",
				Ports:      []string{strconv.Itoa(int(gce.GetNodesHealthCheckPort()))},
			},
		},
		Priority: 999,
	}

	testCases := []struct {
		desc             string
		existingHC       *composite.HealthCheck
		existingFirewall *compute.Firewall
		svc              *corev1.Service
		svcNetwork       *network.NetworkInfo
		wantHC           *composite.HealthCheck
		wantFirewall     *compute.Firewall
		needIPv6         bool
		wantUpdate       l4utils.ResourceSyncStatus
		wantUpdateFw     l4utils.ResourceSyncStatus
	}{
		{
			desc:         "create",
			svc:          svc,
			wantHC:       newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false), namespacedName, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantFirewall: expectedFw,
			wantUpdate:   l4utils.ResourceUpdate,
			wantUpdateFw: l4utils.ResourceUpdate,
		},
		{
			desc:         "create cluster mode",
			svc:          svcETPCluster,
			wantHC:       newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, true), namespacedName, true, hcDefaultPath, gce.GetNodesHealthCheckPort(), utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantFirewall: expectedSharedFw,
			wantUpdate:   l4utils.ResourceUpdate,
			wantUpdateFw: l4utils.ResourceUpdate,
		},
		{
			desc:         "create cluster mode multinet",
			svc:          svcETPCluster,
			svcNetwork:   secondaryNetwork,
			wantHC:       newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, true), namespacedName, true, hcDefaultPath, gce.GetNodesHealthCheckPort(), utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantFirewall: expectedMultinetFw,
			wantUpdate:   l4utils.ResourceUpdate,
			wantUpdateFw: l4utils.ResourceUpdate,
		},
		{
			desc:             "no update",
			svc:              svc,
			existingHC:       newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false), namespacedName, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			existingFirewall: expectedFw,
			wantHC:           newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false), namespacedName, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantFirewall:     expectedFw,
			wantUpdate:       l4utils.ResourceResync,
			wantUpdateFw:     l4utils.ResourceResync,
		},
		{
			desc:             "update only FW",
			svc:              svc,
			existingHC:       newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false), namespacedName, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			existingFirewall: fwThatNeedsUpdate,
			wantHC:           newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false), namespacedName, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantFirewall:     expectedFw,
			wantUpdate:       l4utils.ResourceResync,
			wantUpdateFw:     l4utils.ResourceUpdate,
		},
		{
			desc:             "add ipv6",
			svc:              svc,
			existingHC:       newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false), namespacedName, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			existingFirewall: expectedFw,
			needIPv6:         true,
			wantHC:           newL4HealthCheck(l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false), namespacedName, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantFirewall:     expectedFw,
			wantUpdate:       l4utils.ResourceResync,
			wantUpdateFw:     l4utils.ResourceUpdate,
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
			wantUpdate:   l4utils.ResourceResync,
			wantUpdateFw: l4utils.ResourceUpdate,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(testClusterValues)
			nodeNames := []string{"k8s-test-node"}
			createVMInstanceWithTag(t, fakeGCE, "k8s-test")
			svcNetwork := tc.svcNetwork
			if svcNetwork == nil {
				svcNetwork = network.DefaultNetwork(fakeGCE)
			}
			hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, false)
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

			sharedHC := !helpers.RequestsOnlyLocalTraffic(tc.svc)
			result := hcs.EnsureHealthCheckWithDualStackFirewalls(svc, l4Namer, sharedHC, meta.Global, utils.XLB, nodeNames, true, tc.needIPv6, *svcNetwork, klog.TODO())
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

func TestEnsureHealthCheckWithIPv6OnlyFirewalls(t *testing.T) {
	t.Parallel()

	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))
	testClusterValues := gce.DefaultTestClusterValues()
	hcDefaultPath := "/healthz"

	netlbClass := annotations.RegionalExternalLoadBalancerClass
	ilbClass := annotations.RegionalInternalLoadBalancerClass

	// NetLB ETP Local service template
	svcNetLBLocal := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "netlb-local", Namespace: "default", UID: types.UID("1")},
		Spec: corev1.ServiceSpec{
			Ports:                 []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
			Type:                  "LoadBalancer",
			LoadBalancerClass:     &netlbClass,
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
			HealthCheckNodePort:   1234,
		},
	}

	// NetLB ETP Cluster service template
	svcNetLBCluster := svcNetLBLocal.DeepCopy()
	svcNetLBCluster.Name = "netlb-cluster"
	svcNetLBCluster.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyCluster
	svcNetLBCluster.Spec.HealthCheckNodePort = 0

	// ILB ETP Local service template
	svcILBLocal := svcNetLBLocal.DeepCopy()
	svcILBLocal.Name = "ilb-local"
	svcILBLocal.Spec.LoadBalancerClass = &ilbClass

	// ILB ETP Cluster service template
	svcILBCluster := svcNetLBCluster.DeepCopy()
	svcILBCluster.Name = "ilb-cluster"
	svcILBCluster.Spec.LoadBalancerClass = &ilbClass

	namespacedNameNetLBLocal := types.NamespacedName{Name: svcNetLBLocal.Name, Namespace: svcNetLBLocal.Namespace}
	namespacedNameNetLBCluster := types.NamespacedName{Name: svcNetLBCluster.Name, Namespace: svcNetLBCluster.Namespace}
	namespacedNameILBLocal := types.NamespacedName{Name: svcILBLocal.Name, Namespace: svcILBLocal.Namespace}
	namespacedNameILBCluster := types.NamespacedName{Name: svcILBCluster.Name, Namespace: svcILBCluster.Namespace}

	sharedFwDescription, _ := utils.MakeL4LBFirewallDescription(utils.ServiceKeyFunc(svcNetLBCluster.Namespace, svcNetLBCluster.Name), "", meta.VersionGA, true)
	existingSharedFwWithOnlyILBRange := &compute.Firewall{
		Name:         l4Namer.L4IPv6HealthCheckFirewall(svcNetLBCluster.Namespace, svcNetLBCluster.Name, true),
		Description:  sharedFwDescription,
		Network:      testClusterValues.NetworkURL,
		SourceRanges: []string{L4ILBIPv6HCRange},
		TargetTags:   []string{"k8s-test"},
		Allowed: []*compute.FirewallAllowed{
			{
				IPProtocol: "TCP",
				Ports:      []string{strconv.Itoa(int(gce.GetNodesHealthCheckPort()))},
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
		wantUpdate       l4utils.ResourceSyncStatus
		wantUpdateFw     l4utils.ResourceSyncStatus
		wantIPv6Ranges   []string
	}{
		{
			desc:           "NetLB ETP Local IPv6-only (Isolated NetLB range)",
			svc:            svcNetLBLocal,
			wantHC:         newL4HealthCheck(l4Namer.L4HealthCheck(svcNetLBLocal.Namespace, svcNetLBLocal.Name, false), namespacedNameNetLBLocal, false, hcDefaultPath, 1234, utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantUpdate:     l4utils.ResourceUpdate,
			wantUpdateFw:   l4utils.ResourceUpdate,
			wantIPv6Ranges: []string{L4NetLBIPv6HCRange},
		},
		{
			desc:           "NetLB ETP Cluster IPv6-only (Merged ranges - Smart Unification)",
			svc:            svcNetLBCluster,
			wantHC:         newL4HealthCheck(l4Namer.L4HealthCheck(svcNetLBCluster.Namespace, svcNetLBCluster.Name, true), namespacedNameNetLBCluster, true, hcDefaultPath, gce.GetNodesHealthCheckPort(), utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantUpdate:     l4utils.ResourceUpdate,
			wantUpdateFw:   l4utils.ResourceUpdate,
			wantIPv6Ranges: []string{L4ILBIPv6HCRange, L4NetLBIPv6HCRange},
		},
		{
			desc:           "ILB ETP Local IPv6-only (Isolated ILB range)",
			svc:            svcILBLocal,
			wantHC:         newL4HealthCheck(l4Namer.L4HealthCheck(svcILBLocal.Namespace, svcILBLocal.Name, false), namespacedNameILBLocal, false, hcDefaultPath, 1234, utils.ILB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantUpdate:     l4utils.ResourceUpdate,
			wantUpdateFw:   l4utils.ResourceUpdate,
			wantIPv6Ranges: []string{L4ILBIPv6HCRange},
		},
		{
			desc:           "ILB ETP Cluster IPv6-only (Merged ranges - Smart Unification)",
			svc:            svcILBCluster,
			wantHC:         newL4HealthCheck(l4Namer.L4HealthCheck(svcILBCluster.Namespace, svcILBCluster.Name, true), namespacedNameILBCluster, true, hcDefaultPath, gce.GetNodesHealthCheckPort(), utils.ILB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantUpdate:     l4utils.ResourceUpdate,
			wantUpdateFw:   l4utils.ResourceUpdate,
			wantIPv6Ranges: []string{L4ILBIPv6HCRange, L4NetLBIPv6HCRange},
		},
		{
			desc:             "Reconcile partial shared rule to merged (NetLB ETP Cluster)",
			svc:              svcNetLBCluster,
			existingHC:       newL4HealthCheck(l4Namer.L4HealthCheck(svcNetLBCluster.Namespace, svcNetLBCluster.Name, true), namespacedNameNetLBCluster, true, hcDefaultPath, gce.GetNodesHealthCheckPort(), utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			existingFirewall: existingSharedFwWithOnlyILBRange,
			wantHC:           newL4HealthCheck(l4Namer.L4HealthCheck(svcNetLBCluster.Namespace, svcNetLBCluster.Name, true), namespacedNameNetLBCluster, true, hcDefaultPath, gce.GetNodesHealthCheckPort(), utils.XLB, meta.Global, testClusterValues.Region, klog.TODO()),
			wantUpdate:       l4utils.ResourceResync,
			wantUpdateFw:     l4utils.ResourceUpdate,
			wantIPv6Ranges:   []string{L4ILBIPv6HCRange, L4NetLBIPv6HCRange},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			fakeGCE := gce.NewFakeGCECloud(testClusterValues)
			nodeNames := []string{"k8s-test-node"}
			createVMInstanceWithTag(t, fakeGCE, "k8s-test")
			svcNetwork := network.DefaultNetwork(fakeGCE)

			hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, false)
			if tc.existingHC != nil {
				err := hcs.hcProvider.Create(tc.existingHC)
				if err != nil {
					t.Fatalf("hcProvider.Create() err=%v", err)
				}
			}
			if tc.existingFirewall != nil {
				err := fakeGCE.CreateFirewall(tc.existingFirewall)
				if err != nil {
					t.Fatalf("fakeGCE.CreateFirewall() err=%v", err)
				}
			}
			mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
			mockGCE.MockFirewalls.PatchHook = mock.UpdateFirewallHook

			l4Type := utils.XLB
			if annotations.GetLoadBalancerAnnotationType(tc.svc) == annotations.LBTypeInternal {
				l4Type = utils.ILB
			}

			sharedHC := !helpers.RequestsOnlyLocalTraffic(tc.svc)

			needsIPv4 := false
			needsIPv6 := true
			result := hcs.EnsureHealthCheckWithDualStackFirewalls(tc.svc, l4Namer, sharedHC, meta.Global, l4Type, nodeNames, needsIPv4, needsIPv6, *svcNetwork, klog.TODO())
			if result.Err != nil {
				t.Fatalf("EnsureHealthCheckWithDualStackFirewalls() err=%v", result.Err)
			}
			if result.WasUpdated != tc.wantUpdate {
				t.Errorf("result.WasUpdated want=%v, got=%v", tc.wantUpdate, result.WasUpdated)
			}
			if result.WasFirewallUpdated != tc.wantUpdateFw {
				t.Errorf("result.WasFirewallUpdated want=%v, got=%v", tc.wantUpdateFw, result.WasFirewallUpdated)
			}

			resultHC, err := hcs.hcProvider.Get(result.HCName, meta.Global)
			if err != nil {
				t.Fatalf("hcProvider.Get() err=%v", err)
			}
			if diff := cmp.Diff(tc.wantHC, resultHC, cmpopts.IgnoreFields(composite.HealthCheck{}, "SelfLink", "Region", "Scope", "Version")); diff != "" {
				t.Errorf("created HC differs: diff -want +got\n%v\n", diff)
			}

			// Verify GCE Firewall Rule exists and has correct name and merged/isolated IPv6 ranges
			if result.HCFirewallRuleIPv6Name == "" {
				t.Fatalf("Expected non-empty HCFirewallRuleIPv6Name")
			}
			ipv6Fw, err := fakeGCE.GetFirewall(result.HCFirewallRuleIPv6Name)
			if err != nil {
				t.Fatalf("fakeGCE.GetFirewall(%s) err=%v", result.HCFirewallRuleIPv6Name, err)
			}

			expectedIPv6FwName := l4Namer.L4IPv6HealthCheckFirewall(tc.svc.Namespace, tc.svc.Name, sharedHC)
			if ipv6Fw.Name != expectedIPv6FwName {
				t.Errorf("Expected IPv6 firewall name %s, got %s", expectedIPv6FwName, ipv6Fw.Name)
			}

			if !utils.EqualStringSets(tc.wantIPv6Ranges, ipv6Fw.SourceRanges) {
				t.Errorf("IPv6 Firewall SourceRanges differ: want %v, got %v", tc.wantIPv6Ranges, ipv6Fw.SourceRanges)
			}

			// Verify that IPv4 firewall rule was NOT created
			if result.HCFirewallRuleName != "" {
				t.Errorf("Expected empty HCFirewallRuleName, got %s", result.HCFirewallRuleName)
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

func TestGetHCFirewallSourceRangesWithFlags(t *testing.T) {
	tests := []struct {
		name                 string
		ipVersion            address.IPVersion
		l4Type               utils.L4LBType
		shared               bool
		ilbFlagCIDRs         string
		netlbFlagCIDRs       string
		expectedSourceRanges []string
	}{
		{
			name:                 "IPv4 Default ILB",
			ipVersion:            address.IPv4Version,
			l4Type:               utils.ILB,
			shared:               false,
			expectedSourceRanges: gce.L4LoadBalancerSrcRanges(),
		},
		{
			name:                 "IPv4 Default NetLB",
			ipVersion:            address.IPv4Version,
			l4Type:               utils.XLB,
			shared:               false,
			expectedSourceRanges: gce.L4LoadBalancerSrcRanges(),
		},
		{
			name:                 "IPv6 Default ILB",
			ipVersion:            address.IPv6Version,
			l4Type:               utils.ILB,
			shared:               false,
			expectedSourceRanges: []string{L4ILBIPv6HCRange},
		},
		{
			name:                 "IPv6 Default NetLB",
			ipVersion:            address.IPv6Version,
			l4Type:               utils.XLB,
			shared:               false,
			expectedSourceRanges: []string{L4NetLBIPv6HCRange},
		},
		{
			name:                 "IPv6 Default Shared",
			ipVersion:            address.IPv6Version,
			l4Type:               utils.ILB, // l4Type doesn't matter for shared default
			shared:               true,
			expectedSourceRanges: []string{L4ILBIPv6HCRange, L4NetLBIPv6HCRange},
		},
		{
			name:                 "IPv4 Override ILB with both IPv4 and IPv6",
			ipVersion:            address.IPv4Version,
			l4Type:               utils.ILB,
			shared:               false,
			ilbFlagCIDRs:         "10.0.0.0/8, 2001:db8::/32,  12.0.0.0/8",
			expectedSourceRanges: []string{"10.0.0.0/8", "12.0.0.0/8"},
		},
		{
			name:                 "IPv4 Override ILB with both IPv4 and IPv6",
			ipVersion:            address.IPv4Version,
			l4Type:               utils.ILB,
			shared:               false,
			ilbFlagCIDRs:         "10.0.0.0/8,2001:db8::/32",
			expectedSourceRanges: []string{"10.0.0.0/8"},
		},
		{
			name:                 "IPv6 Override ILB with both IPv4 and IPv6",
			ipVersion:            address.IPv6Version,
			l4Type:               utils.ILB,
			shared:               false,
			ilbFlagCIDRs:         "10.0.0.0/8,2001:db8::/32",
			expectedSourceRanges: []string{"2001:db8::/32"},
		},
		{
			name:                 "IPv6 Override ILB with both IPv4 and IPv6",
			ipVersion:            address.IPv6Version,
			l4Type:               utils.ILB,
			shared:               false,
			ilbFlagCIDRs:         "10.0.0.0/8, 2001:db8::/32, 12.0.0.0/8",
			expectedSourceRanges: []string{"2001:db8::/32"},
		},
		{
			name:                 "IPv4 Override NetLB with both IPv4 and IPv6",
			ipVersion:            address.IPv4Version,
			l4Type:               utils.XLB,
			shared:               false,
			netlbFlagCIDRs:       "192.168.0.0/16,fe80::/10",
			expectedSourceRanges: []string{"192.168.0.0/16"},
		},
		{
			name:                 "IPv6 Override NetLB with both IPv4 and IPv6",
			ipVersion:            address.IPv6Version,
			l4Type:               utils.XLB,
			shared:               false,
			netlbFlagCIDRs:       "192.168.0.0/16,fe80::/10",
			expectedSourceRanges: []string{"fe80::/10"},
		},
		{
			name:                 "IPv4 Shared with both ILB and NetLB Overrides",
			ipVersion:            address.IPv4Version,
			l4Type:               utils.ILB,
			shared:               true,
			ilbFlagCIDRs:         "10.0.0.0/8,2001:db8::/32",
			netlbFlagCIDRs:       "192.168.0.0/16,fe80::/10",
			expectedSourceRanges: []string{"10.0.0.0/8", "192.168.0.0/16"},
		},
		{
			name:                 "IPv6 Shared with both ILB and NetLB Overrides",
			ipVersion:            address.IPv6Version,
			l4Type:               utils.ILB,
			shared:               true,
			ilbFlagCIDRs:         "10.0.0.0/8,2001:db8::/32",
			netlbFlagCIDRs:       "192.168.0.0/16,fe80::/10",
			expectedSourceRanges: []string{"2001:db8::/32", "fe80::/10"},
		},
		{
			name:                 "IPv4 Override ILB but only IPv6 provided",
			ipVersion:            address.IPv4Version,
			l4Type:               utils.ILB,
			shared:               false,
			ilbFlagCIDRs:         "2001:db8::/32",
			expectedSourceRanges: gce.L4LoadBalancerSrcRanges(),
		},
		{
			name:                 "IPv6 Override ILB but only IPv4 provided",
			ipVersion:            address.IPv6Version,
			l4Type:               utils.ILB,
			shared:               false,
			ilbFlagCIDRs:         "10.0.0.0/8",
			expectedSourceRanges: []string{L4ILBIPv6HCRange},
		},
		{
			name:                 "IPv4 Override ILB with malformed CIDR (Fail Closed)",
			ipVersion:            address.IPv4Version,
			l4Type:               utils.ILB,
			shared:               false,
			ilbFlagCIDRs:         "192.168.1.1/999",
			expectedSourceRanges: []string{}, // Should return empty array (fail closed)
		},
		{
			name:                 "IPv6 Override ILB with malformed CIDR (Fail Closed)",
			ipVersion:            address.IPv6Version,
			l4Type:               utils.ILB,
			shared:               false,
			ilbFlagCIDRs:         "invalid-cidr",
			expectedSourceRanges: []string{}, // Should return empty array (fail closed)
		},
		{
			name:                 "IPv4 Override NetLB but only IPv6 provided",
			ipVersion:            address.IPv4Version,
			l4Type:               utils.XLB,
			shared:               false,
			netlbFlagCIDRs:       "2001:db8::/32",
			expectedSourceRanges: gce.L4LoadBalancerSrcRanges(),
		},
		{
			name:                 "IPv6 Override NetLB but only IPv4 provided",
			ipVersion:            address.IPv6Version,
			l4Type:               utils.XLB,
			shared:               false,
			netlbFlagCIDRs:       "10.0.0.0/8",
			expectedSourceRanges: []string{L4NetLBIPv6HCRange},
		},
		{
			name:                 "IPv4 Override NetLB with malformed CIDR (Fail Closed)",
			ipVersion:            address.IPv4Version,
			l4Type:               utils.XLB,
			shared:               false,
			netlbFlagCIDRs:       "10.0.0.0/8,invalid",
			expectedSourceRanges: []string{}, // The entire parse fails, so returns empty
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flags.F.OverrideL4ILBHealthCheckSourceCIDRs = tc.ilbFlagCIDRs
			flags.F.OverrideL4NetLBHealthCheckSourceCIDRs = tc.netlbFlagCIDRs

			actual := getHCFirewallSourceRanges(tc.l4Type, tc.shared, tc.ipVersion)

			// Sort both slices before comparing
			expected := make([]string, len(tc.expectedSourceRanges))
			copy(expected, tc.expectedSourceRanges)
			sort.Strings(expected)

			actualSorted := make([]string, len(actual))
			copy(actualSorted, actual)
			sort.Strings(actualSorted)

			if !reflect.DeepEqual(actualSorted, expected) {
				// Also handle empty vs nil nicely
				if len(actual) == 0 && len(tc.expectedSourceRanges) == 0 {
					return
				}
				t.Errorf("Expected %v, got %v", tc.expectedSourceRanges, actual)
			}
		})
	}

	// Reset flags
	flags.F.OverrideL4ILBHealthCheckSourceCIDRs = ""
	flags.F.OverrideL4NetLBHealthCheckSourceCIDRs = ""
}

// longIPv6HCSvcName is a Service name long enough for the V2 and V2.1 IPv6 health check
// firewall names to differ (and for the V2 name to lose the namespace/name hash).
const longIPv6HCSvcName = "extremely-long-name-that-has-a-different-ending-that-gets-truncated"

// newIPv6HCMigrationTestEnv returns a fake GCE cloud and a long-named ILB Service with
// ETP=Local, for which the V2 and V2.1 IPv6 health check firewall names differ.
func newIPv6HCMigrationTestEnv(t *testing.T, vals gce.TestClusterValues) (*gce.Cloud, *corev1.Service) {
	t.Helper()
	fakeGCE := gce.NewFakeGCECloud(vals)
	createVMInstanceWithTag(t, fakeGCE, "k8s-test")
	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	mockGCE.MockFirewalls.PatchHook = mock.UpdateFirewallHook
	ilbClass := annotations.RegionalInternalLoadBalancerClass
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: longIPv6HCSvcName, Namespace: "default", UID: types.UID("1")},
		Spec: corev1.ServiceSpec{
			Ports:                 []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
			Type:                  "LoadBalancer",
			LoadBalancerClass:     &ilbClass,
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
			HealthCheckNodePort:   1234,
		},
	}
	return fakeGCE, svc
}

// ipv6HCFirewallNames returns the V2 and V2.1 IPv6 health check firewall names of `svc`,
// failing the test if they are equal.
func ipv6HCFirewallNames(t *testing.T, l4Namer namer.L4ResourcesNamer, svc *corev1.Service) (string, string) {
	t.Helper()
	v2 := l4Namer.L4IPv6HealthCheckFirewall(svc.Namespace, svc.Name, false)
	v21 := l4Namer.L4IPv6HealthCheckFirewallV21(svc.Namespace, svc.Name, false)
	if v2 == v21 {
		t.Fatalf("test requires different V2 and V2.1 names, got %q for both", v2)
	}
	return v2, v21
}

func createIPv6HCFirewall(t *testing.T, fakeGCE *gce.Cloud, name, port string) {
	t.Helper()
	err := fakeGCE.CreateFirewall(&compute.Firewall{
		Name:         name,
		Network:      gce.DefaultTestClusterValues().NetworkURL,
		SourceRanges: []string{L4ILBIPv6HCRange},
		TargetTags:   []string{"k8s-test"},
		Allowed:      []*compute.FirewallAllowed{{IPProtocol: "TCP", Ports: []string{port}}},
	})
	if err != nil {
		t.Fatalf("fakeGCE.CreateFirewall(%q) err=%v", name, err)
	}
}

func firewallExists(t *testing.T, fakeGCE *gce.Cloud, name string) bool {
	t.Helper()
	_, err := fakeGCE.GetFirewall(name)
	if err != nil && !utils.IsNotFoundError(err) {
		t.Fatalf("fakeGCE.GetFirewall(%q) err=%v", name, err)
	}
	return err == nil
}

// recordFirewallDeletes makes the fake cloud record the names of all firewall DELETE
// calls. If `deleteErr` returns a non-nil error for a name, the DELETE fails with it.
func recordFirewallDeletes(fakeGCE *gce.Cloud, deleteErr func(name string) error) *[]string {
	var deleted []string
	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	mockGCE.MockFirewalls.DeleteHook = func(_ context.Context, key *meta.Key, _ *cloud.MockFirewalls, _ ...cloud.Option) (bool, error) {
		deleted = append(deleted, key.Name)
		if deleteErr != nil {
			if err := deleteErr(key.Name); err != nil {
				return true, err
			}
		}
		return false, nil
	}
	return &deleted
}

// drainEvents returns all events emitted to `recorder` so far.
func drainEvents(recorder *record.FakeRecorder) []string {
	var events []string
	for {
		select {
		case e := <-recorder.Events:
			events = append(events, e)
		default:
			return events
		}
	}
}

func ensureIPv6OnlyHC(hcs *l4HealthChecks, l4Namer namer.L4ResourcesNamer, svc *corev1.Service, l4Type utils.L4LBType, fakeGCE *gce.Cloud) *EnsureHealthCheckResult {
	svcNetwork := network.DefaultNetwork(fakeGCE)
	return hcs.EnsureHealthCheckWithDualStackFirewalls(svc, l4Namer, false, meta.Global, l4Type, []string{"k8s-test-node"}, false, true, *svcNetwork, klog.TODO())
}

// TestEnsureHealthCheckIPv6FirewallNamingMigration verifies that the IPv6 health check
// firewall rule is migrated between the V2 and V2.1 naming schemas in a single sync:
// the rule with the name in use is ensured first, then the rule with the other name is
// deleted. The other rule is looked up with a GET first, so no DELETE is issued when it
// doesn't exist.
func TestEnsureHealthCheckIPv6FirewallNamingMigration(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))

	testCases := []struct {
		desc            string
		useV21Naming    bool
		l4Type          utils.L4LBType
		otherRuleExists bool
	}{
		{
			desc:         "V2 naming, no V2.1 rule: V2 rule is ensured and no DELETE is issued",
			useV21Naming: false,
			l4Type:       utils.ILB,
		},
		{
			desc:         "V2.1 naming, no V2 rule: V2.1 rule is ensured and no DELETE is issued",
			useV21Naming: true,
			l4Type:       utils.ILB,
		},
		{
			desc:            "ILB migration to V2.1 naming: V2 rule is deleted",
			useV21Naming:    true,
			l4Type:          utils.ILB,
			otherRuleExists: true,
		},
		{
			desc:            "NetLB migration to V2.1 naming: V2 rule is deleted",
			useV21Naming:    true,
			l4Type:          utils.XLB,
			otherRuleExists: true,
		},
		{
			desc:            "ILB rollback to V2 naming: V2.1 rule is deleted",
			useV21Naming:    false,
			l4Type:          utils.ILB,
			otherRuleExists: true,
		},
		{
			desc:            "NetLB rollback to V2 naming: V2.1 rule is deleted",
			useV21Naming:    false,
			l4Type:          utils.XLB,
			otherRuleExists: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fakeGCE, svc := newIPv6HCMigrationTestEnv(t, gce.DefaultTestClusterValues())
			v2FwName, v21FwName := ipv6HCFirewallNames(t, l4Namer, svc)
			wantFwName, otherFwName := v2FwName, v21FwName
			if tc.useV21Naming {
				wantFwName, otherFwName = v21FwName, v2FwName
			}
			if tc.otherRuleExists {
				createIPv6HCFirewall(t, fakeGCE, otherFwName, "1234")
			}
			deleted := recordFirewallDeletes(fakeGCE, nil)
			hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, tc.useV21Naming)

			result := ensureIPv6OnlyHC(hcs, l4Namer, svc, tc.l4Type, fakeGCE)
			if result.Err != nil {
				t.Fatalf("EnsureHealthCheckWithDualStackFirewalls() err=%v", result.Err)
			}
			if result.HCFirewallRuleIPv6Name != wantFwName {
				t.Errorf("HCFirewallRuleIPv6Name = %q, want %q", result.HCFirewallRuleIPv6Name, wantFwName)
			}
			if !firewallExists(t, fakeGCE, wantFwName) {
				t.Errorf("rule %q doesn't exist after sync", wantFwName)
			}
			if firewallExists(t, fakeGCE, otherFwName) {
				t.Errorf("rule %q with the other naming schema exists after sync", otherFwName)
			}
			var wantDeleted []string
			if tc.otherRuleExists {
				wantDeleted = []string{otherFwName}
			}
			if diff := cmp.Diff(wantDeleted, *deleted); diff != "" {
				t.Errorf("deleted firewalls diff (-want +got):\n%s", diff)
			}

			// A second sync is a no-op: no DELETE is issued.
			*deleted = nil
			if result := ensureIPv6OnlyHC(hcs, l4Namer, svc, tc.l4Type, fakeGCE); result.Err != nil {
				t.Fatalf("second EnsureHealthCheckWithDualStackFirewalls() err=%v", result.Err)
			}
			if len(*deleted) != 0 {
				t.Errorf("second sync deleted firewalls %v, want none", *deleted)
			}
		})
	}
}

// TestEnsureHealthCheckIPv6FirewallShortNameUnchanged verifies that for Services with short
// names the V2.1 naming produces the V2 name, so enabling it neither creates nor deletes rules.
func TestEnsureHealthCheckIPv6FirewallShortNameUnchanged(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))
	fakeGCE, svc := newIPv6HCMigrationTestEnv(t, gce.DefaultTestClusterValues())
	svc.Name = "short"
	v2FwName := l4Namer.L4IPv6HealthCheckFirewall(svc.Namespace, svc.Name, false)
	createIPv6HCFirewall(t, fakeGCE, v2FwName, "1234")
	deleted := recordFirewallDeletes(fakeGCE, nil)
	hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, true)

	result := ensureIPv6OnlyHC(hcs, l4Namer, svc, utils.ILB, fakeGCE)
	if result.Err != nil {
		t.Fatalf("EnsureHealthCheckWithDualStackFirewalls() err=%v", result.Err)
	}
	if result.HCFirewallRuleIPv6Name != v2FwName {
		t.Errorf("HCFirewallRuleIPv6Name = %q, want %q", result.HCFirewallRuleIPv6Name, v2FwName)
	}
	if !firewallExists(t, fakeGCE, v2FwName) {
		t.Errorf("rule %q doesn't exist after sync", v2FwName)
	}
	if len(*deleted) != 0 {
		t.Errorf("deleted firewalls %v, want none", *deleted)
	}
}

// TestEnsureHealthCheckIPv6FirewallCollidingServices verifies that two Services whose V2
// IPv6 health check firewall names collide get distinct V2.1 firewall rules allowing their
// respective HealthCheckNodePorts, and that the colliding V2 rule is deleted.
func TestEnsureHealthCheckIPv6FirewallCollidingServices(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))
	fakeGCE, svcA := newIPv6HCMigrationTestEnv(t, gce.DefaultTestClusterValues())
	svcB := svcA.DeepCopy()
	svcB.Name = "extremely-long-name-that-has-a-different-ending-that-gets-shortened"
	svcB.UID = types.UID("2")
	svcB.Spec.HealthCheckNodePort = 5678

	v2FwName, v21FwNameA := ipv6HCFirewallNames(t, l4Namer, svcA)
	if got := l4Namer.L4IPv6HealthCheckFirewall(svcB.Namespace, svcB.Name, false); got != v2FwName {
		t.Fatalf("expected svcA and svcB to collide on V2 firewall name %q, got %q", v2FwName, got)
	}
	v21FwNameB := l4Namer.L4IPv6HealthCheckFirewallV21(svcB.Namespace, svcB.Name, false)
	if v21FwNameA == v21FwNameB {
		t.Fatalf("expected distinct V2.1 names for svcA and svcB, got %q", v21FwNameA)
	}
	// The colliding V2 rule allows the port of whichever Service synced last.
	createIPv6HCFirewall(t, fakeGCE, v2FwName, "5678")
	hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, true)

	for _, svc := range []*corev1.Service{svcA, svcB} {
		if res := ensureIPv6OnlyHC(hcs, l4Namer, svc, utils.ILB, fakeGCE); res.Err != nil {
			t.Fatalf("sync of %s err=%v", svc.Name, res.Err)
		}
	}

	if firewallExists(t, fakeGCE, v2FwName) {
		t.Errorf("colliding V2 rule %q still exists after both Services migrated", v2FwName)
	}
	for _, want := range []struct {
		fwName string
		port   string
	}{
		{fwName: v21FwNameA, port: "1234"},
		{fwName: v21FwNameB, port: "5678"},
	} {
		fw, err := fakeGCE.GetFirewall(want.fwName)
		if err != nil {
			t.Fatalf("GetFirewall(%q) err=%v", want.fwName, err)
		}
		if len(fw.Allowed) != 1 || len(fw.Allowed[0].Ports) != 1 || fw.Allowed[0].Ports[0] != want.port {
			t.Errorf("firewall %q Allowed = %+v, want port %s", want.fwName, fw.Allowed, want.port)
		}
	}
}

// TestEnsureHealthCheckIPv6FirewallCleanupErrors verifies that errors while cleaning up the
// rule with the other naming schema fail the sync with the IPv6 health check firewall as the
// resource in error, after the rule in use has been ensured, and that the next sync retries.
func TestEnsureHealthCheckIPv6FirewallCleanupErrors(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))

	for _, tc := range []struct {
		desc       string
		failGet    bool
		failDelete bool
	}{
		{desc: "GET of the other rule fails", failGet: true},
		{desc: "DELETE of the other rule fails", failDelete: true},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fakeGCE, svc := newIPv6HCMigrationTestEnv(t, gce.DefaultTestClusterValues())
			v2FwName, v21FwName := ipv6HCFirewallNames(t, l4Namer, svc)
			createIPv6HCFirewall(t, fakeGCE, v2FwName, "1234")

			fail := true
			injectedErr := &googleapi.Error{Code: http.StatusInternalServerError, Message: "injected error"}
			mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
			if tc.failGet {
				mockGCE.MockFirewalls.GetHook = func(_ context.Context, key *meta.Key, _ *cloud.MockFirewalls, _ ...cloud.Option) (bool, *compute.Firewall, error) {
					if fail && key.Name == v2FwName {
						return true, nil, injectedErr
					}
					return false, nil, nil
				}
			}
			recordFirewallDeletes(fakeGCE, func(name string) error {
				if fail && tc.failDelete && name == v2FwName {
					return injectedErr
				}
				return nil
			})
			hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, true)

			result := ensureIPv6OnlyHC(hcs, l4Namer, svc, utils.ILB, fakeGCE)
			if result.Err == nil {
				t.Fatalf("EnsureHealthCheckWithDualStackFirewalls() err=nil, want error")
			}
			if result.GceResourceInError != annotations.FirewallForHealthcheckIPv6Resource {
				t.Errorf("GceResourceInError = %q, want %q", result.GceResourceInError, annotations.FirewallForHealthcheckIPv6Resource)
			}
			if !firewallExists(t, fakeGCE, v21FwName) {
				t.Errorf("rule %q in use wasn't ensured before the cleanup", v21FwName)
			}
			fail = false
			if !firewallExists(t, fakeGCE, v2FwName) {
				t.Errorf("rule %q was deleted although the cleanup failed", v2FwName)
			}

			result = ensureIPv6OnlyHC(hcs, l4Namer, svc, utils.ILB, fakeGCE)
			if result.Err != nil {
				t.Fatalf("retry EnsureHealthCheckWithDualStackFirewalls() err=%v", result.Err)
			}
			if result.HCFirewallRuleIPv6Name != v21FwName {
				t.Errorf("HCFirewallRuleIPv6Name = %q, want %q", result.HCFirewallRuleIPv6Name, v21FwName)
			}
			if firewallExists(t, fakeGCE, v2FwName) {
				t.Errorf("rule %q still exists after the retry", v2FwName)
			}
		})
	}
}

// TestDeleteHealthCheckWithDualStackFirewallsDeletesBothIPv6FirewallNames verifies that on
// Service deletion the IPv6 health check firewall rules with both naming schemas are deleted,
// e.g. when the Service is deleted after the naming flag was flipped and before a resync, or
// after the Service was switched to IPv4 only. The rule with the other naming schema is looked
// up with a GET first, so no DELETE is issued for it when it doesn't exist.
func TestDeleteHealthCheckWithDualStackFirewallsDeletesBothIPv6FirewallNames(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))

	for _, tc := range []struct {
		desc            string
		useV21Naming    bool
		otherRuleExists bool
	}{
		{desc: "V2.1 naming, V2 rule exists", useV21Naming: true, otherRuleExists: true},
		{desc: "V2 naming, V2.1 rule exists", useV21Naming: false, otherRuleExists: true},
		{desc: "V2.1 naming, only V2.1 rule exists", useV21Naming: true},
		{desc: "V2 naming, only V2 rule exists", useV21Naming: false},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fakeGCE, svc := newIPv6HCMigrationTestEnv(t, gce.DefaultTestClusterValues())
			v2FwName, v21FwName := ipv6HCFirewallNames(t, l4Namer, svc)
			currentFwName, otherFwName := v2FwName, v21FwName
			if tc.useV21Naming {
				currentFwName, otherFwName = v21FwName, v2FwName
			}
			hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, tc.useV21Naming)
			if res := ensureIPv6OnlyHC(hcs, l4Namer, svc, utils.ILB, fakeGCE); res.Err != nil {
				t.Fatalf("EnsureHealthCheckWithDualStackFirewalls() err=%v", res.Err)
			}
			// Simulate a rule left behind with the other naming schema.
			if tc.otherRuleExists {
				createIPv6HCFirewall(t, fakeGCE, otherFwName, "1234")
			}
			deleted := recordFirewallDeletes(fakeGCE, nil)

			if _, err := hcs.DeleteHealthCheckWithDualStackFirewalls(svc, l4Namer, false, meta.Global, utils.ILB, klog.TODO()); err != nil {
				t.Fatalf("DeleteHealthCheckWithDualStackFirewalls() err=%v", err)
			}
			for _, fwName := range []string{v21FwName, v2FwName} {
				if firewallExists(t, fakeGCE, fwName) {
					t.Errorf("rule %q exists after Service deletion", fwName)
				}
			}
			ipv4FwName := l4Namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, false)
			wantDeleted := []string{ipv4FwName, currentFwName}
			if tc.otherRuleExists {
				wantDeleted = append(wantDeleted, otherFwName)
			}
			if diff := cmp.Diff(wantDeleted, *deleted); diff != "" {
				t.Errorf("deleted firewalls diff (-want +got):\n%s", diff)
			}
		})
	}
}

// TestEnsureHealthCheckIPv6FirewallMigrationOnXPN verifies the migration between naming
// schemas on Shared VPC (XPN) clusters where the controller lacks some firewall permissions:
// syncs and deletions succeed, an XPN event with the gcloud command is raised for each change
// the security admin has to make, and the rule with the other naming schema is never deleted
// while the rule in use doesn't exist.
func TestEnsureHealthCheckIPv6FirewallMigrationOnXPN(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))

	type fwNames struct{ ipv4, current, other string }
	testCases := []struct {
		desc            string
		useV21Naming    bool
		forbidInsert    bool
		forbidPatch     bool
		forbidDelete    bool
		currentExists   bool
		otherRuleExists bool
		// wantOtherDeleteAttempted is whether a DELETE of the other rule is issued on sync.
		wantOtherDeleteAttempted bool
		wantOtherAfterSync       bool
		// wantSyncEvents returns the gcloud commands expected in XPN events on sync.
		wantSyncEvents func(n fwNames) []string
		// wantDeleteEvents returns the gcloud commands expected in XPN events on deletion.
		wantDeleteEvents func(n fwNames) []string
	}{
		{
			desc:         "V2 naming, all mutations forbidden",
			useV21Naming: false,
			forbidInsert: true, forbidPatch: true, forbidDelete: true,
			wantSyncEvents: func(n fwNames) []string {
				return []string{"firewall-rules create " + n.ipv4, "firewall-rules create " + n.current}
			},
			wantDeleteEvents: func(n fwNames) []string {
				return []string{"firewall-rules delete " + n.ipv4, "firewall-rules delete " + n.current}
			},
		},
		{
			desc:         "migration to V2.1, all mutations forbidden: V2 rule is kept without a DELETE",
			useV21Naming: true,
			forbidInsert: true, forbidPatch: true, forbidDelete: true,
			otherRuleExists:    true,
			wantOtherAfterSync: true,
			wantSyncEvents: func(n fwNames) []string {
				return []string{"firewall-rules create " + n.ipv4, "firewall-rules create " + n.current}
			},
			wantDeleteEvents: func(n fwNames) []string {
				return []string{"firewall-rules delete " + n.ipv4, "firewall-rules delete " + n.current, "firewall-rules delete " + n.other}
			},
		},
		{
			desc:         "rollback to V2, all mutations forbidden: V2.1 rule is kept without a DELETE",
			useV21Naming: false,
			forbidInsert: true, forbidPatch: true, forbidDelete: true,
			otherRuleExists:    true,
			wantOtherAfterSync: true,
			wantSyncEvents: func(n fwNames) []string {
				return []string{"firewall-rules create " + n.ipv4, "firewall-rules create " + n.current}
			},
			wantDeleteEvents: func(n fwNames) []string {
				return []string{"firewall-rules delete " + n.ipv4, "firewall-rules delete " + n.current, "firewall-rules delete " + n.other}
			},
		},
		{
			desc:               "migration to V2.1, insert forbidden but delete allowed: V2 rule is kept",
			useV21Naming:       true,
			forbidInsert:       true,
			otherRuleExists:    true,
			wantOtherAfterSync: true,
			wantSyncEvents: func(n fwNames) []string {
				return []string{"firewall-rules create " + n.ipv4, "firewall-rules create " + n.current}
			},
			wantDeleteEvents: func(n fwNames) []string { return nil },
		},
		{
			desc:         "migration to V2.1 after the admin created the V2.1 rule, patch and delete forbidden",
			useV21Naming: true,
			forbidInsert: true, forbidPatch: true, forbidDelete: true,
			currentExists:            true,
			otherRuleExists:          true,
			wantOtherDeleteAttempted: true,
			wantOtherAfterSync:       true,
			wantSyncEvents: func(n fwNames) []string {
				return []string{"firewall-rules create " + n.ipv4, "firewall-rules update " + n.current, "firewall-rules delete " + n.other}
			},
			wantDeleteEvents: func(n fwNames) []string {
				return []string{"firewall-rules delete " + n.ipv4, "firewall-rules delete " + n.current, "firewall-rules delete " + n.other}
			},
		},
		{
			desc:         "migration to V2.1 after the admin created the V2.1 rule, only delete allowed: V2 rule is deleted",
			useV21Naming: true,
			forbidInsert: true, forbidPatch: true,
			currentExists:            true,
			otherRuleExists:          true,
			wantOtherDeleteAttempted: true,
			wantOtherAfterSync:       false,
			wantSyncEvents: func(n fwNames) []string {
				return []string{"firewall-rules create " + n.ipv4, "firewall-rules update " + n.current}
			},
			wantDeleteEvents: func(n fwNames) []string { return nil },
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			vals := gce.DefaultTestClusterValues()
			vals.OnXPN = true
			fakeGCE, svc := newIPv6HCMigrationTestEnv(t, vals)
			v2FwName, v21FwName := ipv6HCFirewallNames(t, l4Namer, svc)
			n := fwNames{ipv4: l4Namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, false), current: v2FwName, other: v21FwName}
			if tc.useV21Naming {
				n.current, n.other = v21FwName, v2FwName
			}
			// Rules created by the security admin. The current rule allows a stale port so
			// that the sync needs to patch it.
			if tc.currentExists {
				createIPv6HCFirewall(t, fakeGCE, n.current, "9999")
			}
			if tc.otherRuleExists {
				createIPv6HCFirewall(t, fakeGCE, n.other, "1234")
			}

			xpnErr := func(call, name string) *googleapi.Error {
				return &googleapi.Error{
					Code:    http.StatusForbidden,
					Message: fmt.Sprintf("Required 'compute.firewalls.%s' permission for 'projects/something/global/firewalls/%s'.", call, name),
				}
			}
			mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
			if tc.forbidInsert {
				mockGCE.MockFirewalls.InsertHook = func(_ context.Context, key *meta.Key, _ *compute.Firewall, _ *cloud.MockFirewalls, _ ...cloud.Option) (bool, error) {
					return true, xpnErr("insert", key.Name)
				}
			}
			if tc.forbidPatch {
				mockGCE.MockFirewalls.PatchHook = func(_ context.Context, key *meta.Key, _ *compute.Firewall, _ *cloud.MockFirewalls, _ ...cloud.Option) error {
					return xpnErr("patch", key.Name)
				}
				mockGCE.MockFirewalls.UpdateHook = mockGCE.MockFirewalls.PatchHook
			}
			deleted := recordFirewallDeletes(fakeGCE, func(name string) error {
				if tc.forbidDelete {
					return xpnErr("delete", name)
				}
				return nil
			})

			recorder := record.NewFakeRecorder(100)
			hcs := NewL4HealthChecks(fakeGCE, recorder, klog.TODO(), true, tc.useV21Naming)
			svcNetwork := network.DefaultNetwork(fakeGCE)

			res := hcs.EnsureHealthCheckWithDualStackFirewalls(svc, l4Namer, false, meta.Regional, utils.ILB, []string{"k8s-test-node"}, true, true, *svcNetwork, klog.TODO())
			if res.Err != nil {
				t.Fatalf("EnsureHealthCheckWithDualStackFirewalls() unexpected err: %v", res.Err)
			}
			if res.HCFirewallRuleIPv6Name != n.current {
				t.Errorf("HCFirewallRuleIPv6Name = %q, want %q", res.HCFirewallRuleIPv6Name, n.current)
			}
			if got := firewallExists(t, fakeGCE, n.current); got != tc.currentExists {
				t.Errorf("rule %q in use exists = %v, want %v", n.current, got, tc.currentExists)
			}
			if got := firewallExists(t, fakeGCE, n.other); got != tc.wantOtherAfterSync {
				t.Errorf("rule %q with the other naming schema exists = %v, want %v", n.other, got, tc.wantOtherAfterSync)
			}
			if got := slices.Contains(*deleted, n.other); got != tc.wantOtherDeleteAttempted {
				t.Errorf("DELETE of rule %q issued = %v, want %v (deleted: %v)", n.other, got, tc.wantOtherDeleteAttempted, *deleted)
			}
			assertXPNEvents(t, drainEvents(recorder), tc.wantSyncEvents(n))

			hcName := l4Namer.L4HealthCheck(svc.Namespace, svc.Name, false)
			if hc, err := hcs.hcProvider.Get(hcName, meta.Regional); err != nil || hc == nil {
				t.Fatalf("expected health check %q to be created, got hc=%v, err=%v", hcName, hc, err)
			}

			if _, err := hcs.DeleteHealthCheckWithDualStackFirewalls(svc, l4Namer, false, meta.Regional, utils.ILB, klog.TODO()); err != nil {
				t.Fatalf("DeleteHealthCheckWithDualStackFirewalls() unexpected err: %v", err)
			}
			if hc, err := hcs.hcProvider.Get(hcName, meta.Regional); err != nil || hc != nil {
				t.Errorf("expected health check %q to be deleted, got hc=%v, err=%v", hcName, hc, err)
			}
			assertXPNEvents(t, drainEvents(recorder), tc.wantDeleteEvents(n))
		})
	}
}

// assertXPNEvents checks that `events` are exactly XPN events with the gcloud commands
// containing `wantCmds`, in order.
func assertXPNEvents(t *testing.T, events []string, wantCmds []string) {
	t.Helper()
	if len(events) != len(wantCmds) {
		t.Errorf("got %d events %v, want %d XPN events for %v", len(events), events, len(wantCmds), wantCmds)
		return
	}
	for i, e := range events {
		if !strings.HasPrefix(e, corev1.EventTypeNormal+" XPN ") || !strings.Contains(e, wantCmds[i]+" ") {
			t.Errorf("event[%d] = %q, want XPN event with %q", i, e, wantCmds[i])
		}
	}
}
