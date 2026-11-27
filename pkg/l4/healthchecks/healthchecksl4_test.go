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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
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

// longIPv6HCSvcName is a Service name long enough for the truncated and hashed IPv6 health
// check firewall names to differ (the truncated name loses the namespace/name hash).
const longIPv6HCSvcName = "extremely-long-name-that-has-a-different-ending-that-gets-truncated"

// newIPv6HCFirewallTestEnv returns a fake GCE cloud and a long-named ILB Service with
// ETP=Local, for which the truncated and hashed IPv6 health check firewall names differ.
func newIPv6HCFirewallTestEnv(t *testing.T, vals gce.TestClusterValues) (*gce.Cloud, *corev1.Service) {
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

// newXPNIPv6HCFirewallTestEnv is newIPv6HCFirewallTestEnv on a Shared VPC (XPN) cluster.
func newXPNIPv6HCFirewallTestEnv(t *testing.T) (*gce.Cloud, *corev1.Service) {
	t.Helper()
	vals := gce.DefaultTestClusterValues()
	vals.OnXPN = true
	return newIPv6HCFirewallTestEnv(t, vals)
}

// ipv6HCFirewallNames returns the truncated and hashed IPv6 health check firewall names of
// svc, failing the test if they are equal.
func ipv6HCFirewallNames(t *testing.T, l4Namer namer.L4ResourcesNamer, svc *corev1.Service) (truncated, hashed string) {
	t.Helper()
	truncated = l4Namer.L4IPv6HealthCheckFirewall(svc.Namespace, svc.Name, false)
	hashed = l4Namer.L4IPv6HealthCheckFirewallWithHash(svc.Namespace, svc.Name, false)
	if truncated == hashed {
		t.Fatalf("test requires different truncated and hashed names, got %q for both", truncated)
	}
	return truncated, hashed
}

func ensureIPv6OnlyHC(hcs *l4HealthChecks, l4Namer namer.L4ResourcesNamer, svc *corev1.Service, fakeGCE *gce.Cloud) *EnsureHealthCheckResult {
	svcNetwork := network.DefaultNetwork(fakeGCE)
	return hcs.EnsureHealthCheckWithDualStackFirewalls(svc, l4Namer, false, meta.Global, utils.ILB, []string{"k8s-test-node"}, false, true, *svcNetwork, klog.TODO())
}

func deleteIPv6OnlyHC(hcs *l4HealthChecks, l4Namer namer.L4ResourcesNamer, svc *corev1.Service) (string, error) {
	return hcs.DeleteHealthCheckWithDualStackFirewalls(svc, l4Namer, false, meta.Global, utils.ILB, klog.TODO())
}

// syncIPv6HCFirewall syncs svc with hashed IPv6 health check firewall names enabled or
// disabled, which creates the rule named with that naming schema the way the controller
// does.
func syncIPv6HCFirewall(t *testing.T, fakeGCE *gce.Cloud, l4Namer namer.L4ResourcesNamer, svc *corev1.Service, useHashedNames bool) {
	t.Helper()
	hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, useHashedNames)
	if res := ensureIPv6OnlyHC(hcs, l4Namer, svc, fakeGCE); res.Err != nil {
		t.Fatalf("sync of %s with useHashedNames=%t: err=%v", svc.Name, useHashedNames, res.Err)
	}
}

// createIPv6HCFirewallAsSecurityAdmin creates the firewall rule name directly in the fake
// cloud, as the security admin of a Shared VPC host project does when the controller lacks
// firewall permissions. Rules that the controller creates are set up with
// syncIPv6HCFirewall.
func createIPv6HCFirewallAsSecurityAdmin(t *testing.T, fakeGCE *gce.Cloud, name, port string) {
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

// assertFirewallRules checks that the fake cloud has exactly the firewall rules want.
func assertFirewallRules(t *testing.T, fakeGCE *gce.Cloud, want ...string) {
	t.Helper()
	fws, err := fakeGCE.Compute().Firewalls().List(context.Background(), filter.None)
	if err != nil {
		t.Fatalf("Firewalls().List() err=%v", err)
	}
	var got []string
	for _, fw := range fws {
		got = append(got, fw.Name)
	}
	if diff := cmp.Diff(want, got, cmpopts.SortSlices(func(a, b string) bool { return a < b }), cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("firewall rules diff (-want +got):\n%s", diff)
	}
}

// firewallDeleteRecorder records the names of all firewall DELETE calls on the fake cloud.
type firewallDeleteRecorder struct {
	names []string
	// failWith, if set, makes the DELETE of the named rule fail with the returned error.
	failWith func(name string) error
}

func (r *firewallDeleteRecorder) hook(_ context.Context, key *meta.Key, _ *cloud.MockFirewalls, _ ...cloud.Option) (bool, error) {
	r.names = append(r.names, key.Name)
	if r.failWith != nil {
		if err := r.failWith(key.Name); err != nil {
			return true, err
		}
	}
	return false, nil
}

func recordFirewallDeletes(fakeGCE *gce.Cloud) *firewallDeleteRecorder {
	r := &firewallDeleteRecorder{}
	fakeGCE.Compute().(*cloud.MockGCE).MockFirewalls.DeleteHook = r.hook
	return r
}

// failFirstFirewallGet makes the first GET of the firewall rule name on the fake cloud fail.
func failFirstFirewallGet(fakeGCE *gce.Cloud, name string) {
	failed := false
	fakeGCE.Compute().(*cloud.MockGCE).MockFirewalls.GetHook = func(_ context.Context, key *meta.Key, _ *cloud.MockFirewalls, _ ...cloud.Option) (bool, *compute.Firewall, error) {
		if failed || key.Name != name {
			return false, nil, nil
		}
		failed = true
		return true, nil, &googleapi.Error{Code: http.StatusInternalServerError, Message: "injected error"}
	}
}

// failFirstFirewallDelete makes the first DELETE of the firewall rule name on the fake
// cloud fail.
func failFirstFirewallDelete(fakeGCE *gce.Cloud, name string) {
	failed := false
	fakeGCE.Compute().(*cloud.MockGCE).MockFirewalls.DeleteHook = func(_ context.Context, key *meta.Key, _ *cloud.MockFirewalls, _ ...cloud.Option) (bool, error) {
		if failed || key.Name != name {
			return false, nil
		}
		failed = true
		return true, &googleapi.Error{Code: http.StatusInternalServerError, Message: "injected error"}
	}
}

// drainEvents returns all events emitted to recorder so far.
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

// TestEnsureHealthCheckIPv6FirewallNoOtherRule verifies that when no rule with the other
// naming schema exists, the sync ensures the rule in use and issues no DELETE (the other
// rule is looked up with a GET first).
func TestEnsureHealthCheckIPv6FirewallNoOtherRule(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))

	for _, tc := range []struct {
		desc           string
		useHashedNames bool
	}{
		{desc: "truncated names"},
		{desc: "hashed names", useHashedNames: true},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fakeGCE, svc := newIPv6HCFirewallTestEnv(t, gce.DefaultTestClusterValues())
			deletes := recordFirewallDeletes(fakeGCE)
			hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, tc.useHashedNames)
			wantFwName := hcs.ipv6HealthCheckFirewallName(l4Namer, svc, false)

			result := ensureIPv6OnlyHC(hcs, l4Namer, svc, fakeGCE)
			if result.Err != nil {
				t.Fatalf("EnsureHealthCheckWithDualStackFirewalls() err=%v", result.Err)
			}
			if result.HCFirewallRuleIPv6Name != wantFwName {
				t.Errorf("HCFirewallRuleIPv6Name = %q, want %q", result.HCFirewallRuleIPv6Name, wantFwName)
			}
			assertFirewallRules(t, fakeGCE, wantFwName)
			if len(deletes.names) != 0 {
				t.Errorf("deleted firewalls %v, want none", deletes.names)
			}
		})
	}
}

// TestEnsureHealthCheckIPv6FirewallMigratesToHashedName verifies that enabling hashed names
// migrates the rule in a single sync: the hashed rule is ensured, then the truncated rule is
// deleted. A second sync issues no DELETE.
func TestEnsureHealthCheckIPv6FirewallMigratesToHashedName(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))
	fakeGCE, svc := newIPv6HCFirewallTestEnv(t, gce.DefaultTestClusterValues())
	truncatedFwName, hashedFwName := ipv6HCFirewallNames(t, l4Namer, svc)
	syncIPv6HCFirewall(t, fakeGCE, l4Namer, svc, false)
	deletes := recordFirewallDeletes(fakeGCE)
	hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, true)

	result := ensureIPv6OnlyHC(hcs, l4Namer, svc, fakeGCE)
	if result.Err != nil {
		t.Fatalf("EnsureHealthCheckWithDualStackFirewalls() err=%v", result.Err)
	}
	if result.HCFirewallRuleIPv6Name != hashedFwName {
		t.Errorf("HCFirewallRuleIPv6Name = %q, want %q", result.HCFirewallRuleIPv6Name, hashedFwName)
	}
	assertFirewallRules(t, fakeGCE, hashedFwName)
	if diff := cmp.Diff([]string{truncatedFwName}, deletes.names); diff != "" {
		t.Errorf("deleted firewalls diff (-want +got):\n%s", diff)
	}

	deletes.names = nil
	if result := ensureIPv6OnlyHC(hcs, l4Namer, svc, fakeGCE); result.Err != nil {
		t.Fatalf("second EnsureHealthCheckWithDualStackFirewalls() err=%v", result.Err)
	}
	if len(deletes.names) != 0 {
		t.Errorf("second sync deleted firewalls %v, want none", deletes.names)
	}
}

// TestEnsureHealthCheckIPv6FirewallRollsBackToTruncatedName verifies that disabling hashed
// names rolls the rule back in a single sync: the truncated rule is ensured, then the hashed
// rule is deleted. A second sync issues no DELETE.
func TestEnsureHealthCheckIPv6FirewallRollsBackToTruncatedName(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))
	fakeGCE, svc := newIPv6HCFirewallTestEnv(t, gce.DefaultTestClusterValues())
	truncatedFwName, hashedFwName := ipv6HCFirewallNames(t, l4Namer, svc)
	syncIPv6HCFirewall(t, fakeGCE, l4Namer, svc, true)
	deletes := recordFirewallDeletes(fakeGCE)
	hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, false)

	result := ensureIPv6OnlyHC(hcs, l4Namer, svc, fakeGCE)
	if result.Err != nil {
		t.Fatalf("EnsureHealthCheckWithDualStackFirewalls() err=%v", result.Err)
	}
	if result.HCFirewallRuleIPv6Name != truncatedFwName {
		t.Errorf("HCFirewallRuleIPv6Name = %q, want %q", result.HCFirewallRuleIPv6Name, truncatedFwName)
	}
	assertFirewallRules(t, fakeGCE, truncatedFwName)
	if diff := cmp.Diff([]string{hashedFwName}, deletes.names); diff != "" {
		t.Errorf("deleted firewalls diff (-want +got):\n%s", diff)
	}

	deletes.names = nil
	if result := ensureIPv6OnlyHC(hcs, l4Namer, svc, fakeGCE); result.Err != nil {
		t.Fatalf("second EnsureHealthCheckWithDualStackFirewalls() err=%v", result.Err)
	}
	if len(deletes.names) != 0 {
		t.Errorf("second sync deleted firewalls %v, want none", deletes.names)
	}
}

// TestEnsureHealthCheckIPv6FirewallCollidingServices verifies that two Services whose
// truncated IPv6 health check firewall names collide get distinct hashed firewall rules
// allowing their respective HealthCheckNodePorts, and that the colliding rule is deleted.
func TestEnsureHealthCheckIPv6FirewallCollidingServices(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))
	fakeGCE, svcA := newIPv6HCFirewallTestEnv(t, gce.DefaultTestClusterValues())
	svcB := svcA.DeepCopy()
	svcB.Name = "extremely-long-name-that-has-a-different-ending-that-gets-shortened"
	svcB.UID = types.UID("2")
	svcB.Spec.HealthCheckNodePort = 5678

	truncatedFwName, hashedFwNameA := ipv6HCFirewallNames(t, l4Namer, svcA)
	if got := l4Namer.L4IPv6HealthCheckFirewall(svcB.Namespace, svcB.Name, false); got != truncatedFwName {
		t.Fatalf("expected svcA and svcB to collide on truncated firewall name %q, got %q", truncatedFwName, got)
	}
	hashedFwNameB := l4Namer.L4IPv6HealthCheckFirewallWithHash(svcB.Namespace, svcB.Name, false)
	if hashedFwNameA == hashedFwNameB {
		t.Fatalf("expected distinct hashed names for svcA and svcB, got %q", hashedFwNameA)
	}
	// With truncated names both Services share one rule, which allows the port of the
	// Service synced last.
	syncIPv6HCFirewall(t, fakeGCE, l4Namer, svcA, false)
	syncIPv6HCFirewall(t, fakeGCE, l4Namer, svcB, false)
	hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, true)

	for _, svc := range []*corev1.Service{svcA, svcB} {
		if res := ensureIPv6OnlyHC(hcs, l4Namer, svc, fakeGCE); res.Err != nil {
			t.Fatalf("sync of %s err=%v", svc.Name, res.Err)
		}
	}

	assertFirewallRules(t, fakeGCE, hashedFwNameA, hashedFwNameB)
	for fwName, port := range map[string]string{hashedFwNameA: "1234", hashedFwNameB: "5678"} {
		fw, err := fakeGCE.GetFirewall(fwName)
		if err != nil {
			t.Fatalf("GetFirewall(%q) err=%v", fwName, err)
		}
		wantAllowed := []*compute.FirewallAllowed{{IPProtocol: string(corev1.ProtocolTCP), Ports: []string{port}}}
		if diff := cmp.Diff(wantAllowed, fw.Allowed); diff != "" {
			t.Errorf("firewall %q Allowed diff (-want +got):\n%s", fwName, diff)
		}
	}
}

// TestEnsureHealthCheckIPv6FirewallCleanupRetriesAfterError verifies that a failing cleanup
// of the truncated rule fails the sync with the IPv6 health check firewall as the resource
// in error after the hashed rule has been ensured, and that the next sync completes the
// migration.
func TestEnsureHealthCheckIPv6FirewallCleanupRetriesAfterError(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))

	for _, tc := range []struct {
		desc     string
		failOnce func(fakeGCE *gce.Cloud, name string)
	}{
		{desc: "GET of the truncated rule fails", failOnce: failFirstFirewallGet},
		{desc: "DELETE of the truncated rule fails", failOnce: failFirstFirewallDelete},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fakeGCE, svc := newIPv6HCFirewallTestEnv(t, gce.DefaultTestClusterValues())
			truncatedFwName, hashedFwName := ipv6HCFirewallNames(t, l4Namer, svc)
			syncIPv6HCFirewall(t, fakeGCE, l4Namer, svc, false)
			tc.failOnce(fakeGCE, truncatedFwName)
			hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, true)

			result := ensureIPv6OnlyHC(hcs, l4Namer, svc, fakeGCE)
			if result.Err == nil {
				t.Fatalf("EnsureHealthCheckWithDualStackFirewalls() err=nil, want error")
			}
			if result.GceResourceInError != annotations.FirewallForHealthcheckIPv6Resource {
				t.Errorf("GceResourceInError = %q, want %q", result.GceResourceInError, annotations.FirewallForHealthcheckIPv6Resource)
			}
			assertFirewallRules(t, fakeGCE, hashedFwName, truncatedFwName)

			result = ensureIPv6OnlyHC(hcs, l4Namer, svc, fakeGCE)
			if result.Err != nil {
				t.Fatalf("retry EnsureHealthCheckWithDualStackFirewalls() err=%v", result.Err)
			}
			if result.HCFirewallRuleIPv6Name != hashedFwName {
				t.Errorf("HCFirewallRuleIPv6Name = %q, want %q", result.HCFirewallRuleIPv6Name, hashedFwName)
			}
			assertFirewallRules(t, fakeGCE, hashedFwName)
		})
	}
}

// TestDeleteHealthCheckIPv6FirewallDeletesOtherRule verifies that a Service deleted after
// the naming flag was flipped and before it was resynced has its IPv6 health check firewall
// rule with the other naming schema deleted too.
func TestDeleteHealthCheckIPv6FirewallDeletesOtherRule(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))

	for _, tc := range []struct {
		desc           string
		useHashedNames bool
	}{
		{desc: "hashed names, truncated rule left behind", useHashedNames: true},
		{desc: "truncated names, hashed rule left behind", useHashedNames: false},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fakeGCE, svc := newIPv6HCFirewallTestEnv(t, gce.DefaultTestClusterValues())
			syncIPv6HCFirewall(t, fakeGCE, l4Namer, svc, !tc.useHashedNames)
			deletes := recordFirewallDeletes(fakeGCE)
			hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, tc.useHashedNames)
			fwName := hcs.ipv6HealthCheckFirewallName(l4Namer, svc, false)
			otherFwName := hcs.otherSchemaIPv6HealthCheckFirewallName(l4Namer, svc, false)

			resourceInError, err := deleteIPv6OnlyHC(hcs, l4Namer, svc)
			if err != nil {
				t.Fatalf("DeleteHealthCheckWithDualStackFirewalls() err=%v", err)
			}
			if resourceInError != "" {
				t.Errorf("DeleteHealthCheckWithDualStackFirewalls() resource in error = %q, want none", resourceInError)
			}
			assertFirewallRules(t, fakeGCE)
			ipv4FwName := l4Namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, false)
			if diff := cmp.Diff([]string{ipv4FwName, fwName, otherFwName}, deletes.names); diff != "" {
				t.Errorf("deleted firewalls diff (-want +got):\n%s", diff)
			}
		})
	}
}

// TestDeleteHealthCheckIPv6FirewallSkipsMissingOtherRule verifies that on Service deletion
// no DELETE is issued for the rule with the other naming schema when it doesn't exist (it
// is looked up with a GET first).
func TestDeleteHealthCheckIPv6FirewallSkipsMissingOtherRule(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))
	fakeGCE, svc := newIPv6HCFirewallTestEnv(t, gce.DefaultTestClusterValues())
	_, hashedFwName := ipv6HCFirewallNames(t, l4Namer, svc)
	syncIPv6HCFirewall(t, fakeGCE, l4Namer, svc, true)
	deletes := recordFirewallDeletes(fakeGCE)
	hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, true)

	if _, err := deleteIPv6OnlyHC(hcs, l4Namer, svc); err != nil {
		t.Fatalf("DeleteHealthCheckWithDualStackFirewalls() err=%v", err)
	}
	assertFirewallRules(t, fakeGCE)
	ipv4FwName := l4Namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, false)
	if diff := cmp.Diff([]string{ipv4FwName, hashedFwName}, deletes.names); diff != "" {
		t.Errorf("deleted firewalls diff (-want +got):\n%s", diff)
	}
}

// TestDeleteHealthCheckIPv6FirewallShortNameSkipsOtherRuleLookup verifies that deleting a
// Service whose truncated and hashed names are equal doesn't look up a rule with the other
// naming schema.
func TestDeleteHealthCheckIPv6FirewallShortNameSkipsOtherRuleLookup(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))
	fakeGCE, svc := newIPv6HCFirewallTestEnv(t, gce.DefaultTestClusterValues())
	svc.Name = "short-name"
	if truncated, hashed := l4Namer.L4IPv6HealthCheckFirewall(svc.Namespace, svc.Name, false), l4Namer.L4IPv6HealthCheckFirewallWithHash(svc.Namespace, svc.Name, false); truncated != hashed {
		t.Fatalf("test requires equal truncated and hashed names, got %q and %q", truncated, hashed)
	}
	syncIPv6HCFirewall(t, fakeGCE, l4Namer, svc, true)
	var gets []string
	fakeGCE.Compute().(*cloud.MockGCE).MockFirewalls.GetHook = func(_ context.Context, key *meta.Key, _ *cloud.MockFirewalls, _ ...cloud.Option) (bool, *compute.Firewall, error) {
		gets = append(gets, key.Name)
		return false, nil, nil
	}
	hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, true)

	if _, err := deleteIPv6OnlyHC(hcs, l4Namer, svc); err != nil {
		t.Fatalf("DeleteHealthCheckWithDualStackFirewalls() err=%v", err)
	}
	assertFirewallRules(t, fakeGCE)
	if len(gets) != 0 {
		t.Errorf("Service deletion looked up firewalls %v, want none", gets)
	}
}

// TestDeleteHealthCheckIPv6FirewallRetriesAfterOtherRuleError verifies that a failing GET of
// the rule with the other naming schema fails the Service deletion with the IPv6 health
// check firewall as the resource in error, and that the retried deletion deletes the rule.
func TestDeleteHealthCheckIPv6FirewallRetriesAfterOtherRuleError(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))
	fakeGCE, svc := newIPv6HCFirewallTestEnv(t, gce.DefaultTestClusterValues())
	truncatedFwName, _ := ipv6HCFirewallNames(t, l4Namer, svc)
	syncIPv6HCFirewall(t, fakeGCE, l4Namer, svc, false)
	failFirstFirewallGet(fakeGCE, truncatedFwName)
	hcs := NewL4HealthChecks(fakeGCE, &record.FakeRecorder{}, klog.TODO(), true, true)

	resourceInError, err := deleteIPv6OnlyHC(hcs, l4Namer, svc)
	if err == nil {
		t.Fatalf("DeleteHealthCheckWithDualStackFirewalls() err=nil, want error")
	}
	if resourceInError != annotations.FirewallForHealthcheckIPv6Resource {
		t.Errorf("DeleteHealthCheckWithDualStackFirewalls() resource in error = %q, want %q", resourceInError, annotations.FirewallForHealthcheckIPv6Resource)
	}
	assertFirewallRules(t, fakeGCE, truncatedFwName)

	if _, err := deleteIPv6OnlyHC(hcs, l4Namer, svc); err != nil {
		t.Fatalf("retry DeleteHealthCheckWithDualStackFirewalls() err=%v", err)
	}
	assertFirewallRules(t, fakeGCE)
}

// xpnFirewallError returns the error GCE returns for firewall call (insert, patch or
// delete) on name on a Shared VPC cluster without firewall permissions, which the
// controller turns into an XPN event instead of failing the sync.
func xpnFirewallError(call, name string) *googleapi.Error {
	return &googleapi.Error{
		Code:    http.StatusForbidden,
		Message: fmt.Sprintf("Required 'compute.firewalls.%s' permission for 'projects/something/global/firewalls/%s'.", call, name),
	}
}

// forbidFirewallInsertAndPatch makes firewall INSERT and PATCH calls on the fake cloud
// fail with the Shared VPC permission error.
func forbidFirewallInsertAndPatch(fakeGCE *gce.Cloud) {
	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	mockGCE.MockFirewalls.InsertHook = func(_ context.Context, key *meta.Key, _ *compute.Firewall, _ *cloud.MockFirewalls, _ ...cloud.Option) (bool, error) {
		return true, xpnFirewallError("insert", key.Name)
	}
	mockGCE.MockFirewalls.PatchHook = func(_ context.Context, key *meta.Key, _ *compute.Firewall, _ *cloud.MockFirewalls, _ ...cloud.Option) error {
		return xpnFirewallError("patch", key.Name)
	}
}

// forbidFirewallDeletes makes firewall DELETE calls recorded by deletes fail with the
// Shared VPC permission error.
func forbidFirewallDeletes(deletes *firewallDeleteRecorder) {
	deletes.failWith = func(name string) error { return xpnFirewallError("delete", name) }
}

// TestEnsureHealthCheckIPv6FirewallOnXPNKeepsOtherRuleUntilCreated verifies that on a
// Shared VPC cluster without firewall permissions, flipping the naming flag in either
// direction never deletes the rule with the other naming schema while the rule in use
// doesn't exist: the sync succeeds, raises an XPN event asking the security admin to
// create the rule in use, and issues no DELETE.
func TestEnsureHealthCheckIPv6FirewallOnXPNKeepsOtherRuleUntilCreated(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))

	for _, tc := range []struct {
		desc           string
		useHashedNames bool
	}{
		{desc: "migration to hashed names", useHashedNames: true},
		{desc: "rollback to truncated names", useHashedNames: false},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fakeGCE, svc := newXPNIPv6HCFirewallTestEnv(t)
			syncIPv6HCFirewall(t, fakeGCE, l4Namer, svc, !tc.useHashedNames)
			forbidFirewallInsertAndPatch(fakeGCE)
			deletes := recordFirewallDeletes(fakeGCE)
			recorder := record.NewFakeRecorder(100)
			hcs := NewL4HealthChecks(fakeGCE, recorder, klog.TODO(), true, tc.useHashedNames)
			fwName := hcs.ipv6HealthCheckFirewallName(l4Namer, svc, false)
			otherFwName := hcs.otherSchemaIPv6HealthCheckFirewallName(l4Namer, svc, false)

			result := ensureIPv6OnlyHC(hcs, l4Namer, svc, fakeGCE)
			if result.Err != nil {
				t.Fatalf("EnsureHealthCheckWithDualStackFirewalls() err=%v", result.Err)
			}
			if result.HCFirewallRuleIPv6Name != fwName {
				t.Errorf("HCFirewallRuleIPv6Name = %q, want %q", result.HCFirewallRuleIPv6Name, fwName)
			}
			assertFirewallRules(t, fakeGCE, otherFwName)
			if len(deletes.names) != 0 {
				t.Errorf("DELETE issued for %v, want none", deletes.names)
			}
			assertXPNEvents(t, drainEvents(recorder), []string{"create " + fwName})
		})
	}
}

// TestEnsureHealthCheckIPv6FirewallOnXPNDeletesOtherRuleOnceCreated verifies that once the
// security admin has created the hashed rule, the sync deletes the truncated rule even
// though it can't patch the hashed rule itself (an XPN event asks for the update).
func TestEnsureHealthCheckIPv6FirewallOnXPNDeletesOtherRuleOnceCreated(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))
	fakeGCE, svc := newXPNIPv6HCFirewallTestEnv(t)
	truncatedFwName, hashedFwName := ipv6HCFirewallNames(t, l4Namer, svc)
	syncIPv6HCFirewall(t, fakeGCE, l4Namer, svc, false)
	// The hashed rule created by the admin allows a stale port, so the sync needs to patch it.
	createIPv6HCFirewallAsSecurityAdmin(t, fakeGCE, hashedFwName, "9999")
	forbidFirewallInsertAndPatch(fakeGCE)
	deletes := recordFirewallDeletes(fakeGCE)
	recorder := record.NewFakeRecorder(100)
	hcs := NewL4HealthChecks(fakeGCE, recorder, klog.TODO(), true, true)

	result := ensureIPv6OnlyHC(hcs, l4Namer, svc, fakeGCE)
	if result.Err != nil {
		t.Fatalf("EnsureHealthCheckWithDualStackFirewalls() err=%v", result.Err)
	}
	assertFirewallRules(t, fakeGCE, hashedFwName)
	if diff := cmp.Diff([]string{truncatedFwName}, deletes.names); diff != "" {
		t.Errorf("deleted firewalls diff (-want +got):\n%s", diff)
	}
	assertXPNEvents(t, drainEvents(recorder), []string{"update " + hashedFwName})
}

// TestEnsureHealthCheckIPv6FirewallOnXPNForbiddenDeleteRaisesEvent verifies that when the
// hashed rule exists but DELETE is forbidden, the sync still succeeds and raises an XPN
// event asking the security admin to delete the truncated rule.
func TestEnsureHealthCheckIPv6FirewallOnXPNForbiddenDeleteRaisesEvent(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))
	fakeGCE, svc := newXPNIPv6HCFirewallTestEnv(t)
	truncatedFwName, hashedFwName := ipv6HCFirewallNames(t, l4Namer, svc)
	syncIPv6HCFirewall(t, fakeGCE, l4Namer, svc, false)
	// The hashed rule created by the admin allows a stale port, so the sync needs to patch it.
	createIPv6HCFirewallAsSecurityAdmin(t, fakeGCE, hashedFwName, "9999")
	forbidFirewallInsertAndPatch(fakeGCE)
	deletes := recordFirewallDeletes(fakeGCE)
	forbidFirewallDeletes(deletes)
	recorder := record.NewFakeRecorder(100)
	hcs := NewL4HealthChecks(fakeGCE, recorder, klog.TODO(), true, true)

	result := ensureIPv6OnlyHC(hcs, l4Namer, svc, fakeGCE)
	if result.Err != nil {
		t.Fatalf("EnsureHealthCheckWithDualStackFirewalls() err=%v", result.Err)
	}
	assertFirewallRules(t, fakeGCE, hashedFwName, truncatedFwName)
	if diff := cmp.Diff([]string{truncatedFwName}, deletes.names); diff != "" {
		t.Errorf("deleted firewalls diff (-want +got):\n%s", diff)
	}
	assertXPNEvents(t, drainEvents(recorder), []string{"update " + hashedFwName, "delete " + truncatedFwName})
}

// TestDeleteHealthCheckIPv6FirewallOnXPNRaisesEventPerRule verifies that on a Shared VPC
// cluster without firewall permissions, Service deletion succeeds and raises an XPN event
// for each rule it can't delete, including the rule with the other naming schema.
func TestDeleteHealthCheckIPv6FirewallOnXPNRaisesEventPerRule(t *testing.T) {
	t.Parallel()
	l4Namer := namer.NewL4Namer("test", namer.NewNamer("testCluster", "testFirewall", klog.TODO()))
	fakeGCE, svc := newXPNIPv6HCFirewallTestEnv(t)
	truncatedFwName, hashedFwName := ipv6HCFirewallNames(t, l4Namer, svc)
	syncIPv6HCFirewall(t, fakeGCE, l4Namer, svc, false)
	deletes := recordFirewallDeletes(fakeGCE)
	forbidFirewallDeletes(deletes)
	recorder := record.NewFakeRecorder(100)
	hcs := NewL4HealthChecks(fakeGCE, recorder, klog.TODO(), true, true)

	resourceInError, err := deleteIPv6OnlyHC(hcs, l4Namer, svc)
	if err != nil {
		t.Fatalf("DeleteHealthCheckWithDualStackFirewalls() err=%v", err)
	}
	if resourceInError != "" {
		t.Errorf("DeleteHealthCheckWithDualStackFirewalls() resource in error = %q, want none", resourceInError)
	}
	ipv4FwName := l4Namer.L4HealthCheckFirewall(svc.Namespace, svc.Name, false)
	assertXPNEvents(t, drainEvents(recorder), []string{"delete " + ipv4FwName, "delete " + hashedFwName, "delete " + truncatedFwName})
}

// assertXPNEvents checks that events are exactly XPN events asking the security admin to
// run `gcloud compute firewall-rules <cmd>` for each of wantCmds ("<verb> <rule name>"), in
// order.
func assertXPNEvents(t *testing.T, events []string, wantCmds []string) {
	t.Helper()
	if len(events) != len(wantCmds) {
		t.Errorf("got %d events %v, want %d XPN events for %v", len(events), events, len(wantCmds), wantCmds)
		return
	}
	for i, e := range events {
		want := corev1.EventTypeNormal + " XPN Firewall change required by security admin: `gcloud compute firewall-rules " + wantCmds[i] + " "
		if !strings.HasPrefix(e, want) {
			t.Errorf("event[%d] = %q, want prefix %q", i, e, want)
		}
	}
}
