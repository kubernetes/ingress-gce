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

package healthchecksl4

import (
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/apimachinery/pkg/types"
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
			wantHC := newL4HealthCheck("hc", types.NamespacedName{Name: "svc", Namespace: "default"}, tc.shared, "/", 12345, utils.ILB, meta.Global, "")
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
			hc := newL4HealthCheck("hc", types.NamespacedName{Name: "svc", Namespace: "default"}, false, "/", 12345, utils.ILB, meta.Global, "")
			wantHC := newL4HealthCheck("hc", types.NamespacedName{Name: "svc", Namespace: "default"}, false, "/", 12345, utils.ILB, meta.Global, "")
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
			gotHC := newL4HealthCheck("hc", types.NamespacedName{Name: "svc", Namespace: "default"}, tc.shared, "/", 12345, utils.ILB, meta.Global, "")
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
		hc := newL4HealthCheck("hc", namespaceName, false, "/", 12345, utils.ILB, v.scope, v.region)
		if hc.Region != v.region {
			t.Errorf("HealthCheck Region mismatch! %v != %v", hc.Region, v.region)
		}
		if hc.Scope != v.scope {
			t.Errorf("HealthCheck Scope mismatch! %v != %v", hc.Scope, v.scope)
		}
	}
}
