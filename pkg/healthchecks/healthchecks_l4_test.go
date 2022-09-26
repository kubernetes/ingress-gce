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
	"testing"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/ingress-gce/pkg/composite"
)

func TestMergeHealthChecks(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc                   string
		checkIntervalSec       int64
		timeoutSec             int64
		healthyThreshold       int64
		unhealthyThreshold     int64
		wantCheckIntervalSec   int64
		wantTimeoutSec         int64
		wantHealthyThreshold   int64
		wantUnhealthyThreshold int64
	}{
		{
			desc:                   "unchanged",
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
			desc:                   "interval - too small - should reconcile",
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
			desc:                   "timeout - too small - should reconcile",
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
			desc:                   "healthy threshold - too small - should reconcil",
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
			desc:                   "unhealthy threshold - too small - should reconcile",
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
			desc:                   "interval - user configured - should keep",
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
			desc:                   "timeout - user configured - should keep",
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
			desc:                   "healthy threshold - user configured - should keep",
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
			desc:                   "unhealthy threshold - user configured - should keep",
			checkIntervalSec:       gceLocalHcCheckIntervalSeconds,
			timeoutSec:             gceHcTimeoutSeconds,
			healthyThreshold:       gceHcHealthyThreshold,
			unhealthyThreshold:     gceLocalHcUnhealthyThreshold + 1,
			wantCheckIntervalSec:   gceLocalHcCheckIntervalSeconds,
			wantTimeoutSec:         gceHcTimeoutSeconds,
			wantHealthyThreshold:   gceHcHealthyThreshold,
			wantUnhealthyThreshold: gceLocalHcUnhealthyThreshold + 1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			wantHC := NewL4HealthCheck("hc", types.NamespacedName{Name: "svc", Namespace: "default"}, false, "/", 12345)
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
			hc := NewL4HealthCheck("hc", types.NamespacedName{Name: "svc", Namespace: "default"}, false, "/", 12345)
			wantHC := NewL4HealthCheck("hc", types.NamespacedName{Name: "svc", Namespace: "default"}, false, "/", 12345)
			if tc.modifier != nil {
				tc.modifier(hc)
			}
			if gotChanged := needToUpdateHealthChecks(hc, wantHC); gotChanged != tc.wantChanged {
				t.Errorf("needToUpdateHealthChecks(%#v, %#v) = %t; want changed = %t", hc, wantHC, gotChanged, tc.wantChanged)
			}
		})
	}
}

func TestHealthcheckInterval(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc                 string
		shared               bool
		wantCheckIntervalSec int64
	}{
		{
			desc:                 "shared - check interval function",
			shared:               true,
			wantCheckIntervalSec: gceSharedHcCheckIntervalSeconds,
		},
		{
			desc:                 "local - check interval function",
			shared:               false,
			wantCheckIntervalSec: gceLocalHcCheckIntervalSeconds,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			gotInterval := healthcheckInterval(tc.shared)
			if gotInterval != tc.wantCheckIntervalSec {
				t.Errorf("got Health Check Interval: %d, want: %d", gotInterval, tc.wantCheckIntervalSec)
			}
		})
	}
}

func TestHealthcheckUnhealthyThreshold(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc                   string
		shared                 bool
		wantUnhealthyThreshold int64
	}{
		{
			desc:                   "shared - check unhealthy threshold function",
			shared:                 true,
			wantUnhealthyThreshold: gceSharedHcUnhealthyThreshold,
		},
		{
			desc:                   "local - check unhealthy threshold function",
			shared:                 false,
			wantUnhealthyThreshold: gceLocalHcUnhealthyThreshold,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			gotUnhealthyThreshold := healthcheckUnhealthyThreshold(tc.shared)
			if gotUnhealthyThreshold != tc.wantUnhealthyThreshold {
				t.Errorf("got Unhealthy Threshold: %d, want: %d", gotUnhealthyThreshold, tc.wantUnhealthyThreshold)
			}
		})
	}
}
