/*
Copyright 2023 The Kubernetes Authors.

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

package metricscollector

import (
	"fmt"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/gke-enterprise-mt/pkg/mtmetrics"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/prometheus/client_golang/prometheus"
	dto_pb "github.com/prometheus/client_model/go"
	"k8s.io/ingress-gce/pkg/neg/types"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

func TestUpdateMigrationStartAndEndTime(t *testing.T) {
	syncerKey := syncerKey(1)
	testCases := []struct {
		name                        string
		migrationCount              int
		dualStackMigrationStartTime map[types.NegSyncerKey]time.Time
		dualStackMigrationEndTime   map[types.NegSyncerKey]time.Time
		wantChangedStartTime        bool
		wantChangedEndTime          bool
	}{
		{
			name:                        "start time should get set since this is the first time when migration starts",
			migrationCount:              5,
			dualStackMigrationStartTime: map[types.NegSyncerKey]time.Time{},
			dualStackMigrationEndTime:   map[types.NegSyncerKey]time.Time{},
			wantChangedStartTime:        true,
		},
		{
			name:           "start time is already present so no action required",
			migrationCount: 5,
			dualStackMigrationStartTime: map[types.NegSyncerKey]time.Time{
				syncerKey: time.Now(),
			},
			dualStackMigrationEndTime: map[types.NegSyncerKey]time.Time{},
			wantChangedStartTime:      false,
		},
		{
			name:           "end time should get unset because migration is still in progress",
			migrationCount: 5,
			dualStackMigrationStartTime: map[types.NegSyncerKey]time.Time{
				syncerKey: time.Now(),
			},
			dualStackMigrationEndTime: map[types.NegSyncerKey]time.Time{
				syncerKey: time.Now(),
			},
			wantChangedEndTime: true,
		},
		{
			name:           "end time should get set because it's not currently set",
			migrationCount: 0,
			dualStackMigrationStartTime: map[types.NegSyncerKey]time.Time{
				syncerKey: time.Now(),
			},
			dualStackMigrationEndTime: map[types.NegSyncerKey]time.Time{},
			wantChangedEndTime:        true,
		},
		{
			name:           "should not set new end time if end time already present",
			migrationCount: 0,
			dualStackMigrationStartTime: map[types.NegSyncerKey]time.Time{
				syncerKey: time.Now(),
			},
			dualStackMigrationEndTime: map[types.NegSyncerKey]time.Time{
				syncerKey: time.Now(),
			},
		},
		{
			name:                        "migration was not in progress so end time should not get set",
			migrationCount:              0,
			dualStackMigrationStartTime: map[types.NegSyncerKey]time.Time{},
			dualStackMigrationEndTime:   map[types.NegSyncerKey]time.Time{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sm := &SyncerMetrics{
				dualStackMigrationStartTime: tc.dualStackMigrationStartTime,
				dualStackMigrationEndTime:   tc.dualStackMigrationEndTime,
				clock:                       clock.RealClock{},
			}

			clonedStartTime := tc.dualStackMigrationStartTime[syncerKey]
			clonedEndTime := tc.dualStackMigrationEndTime[syncerKey]

			sm.updateMigrationStartAndEndTime(syncerKey, tc.migrationCount)

			if tc.wantChangedStartTime == (sm.dualStackMigrationStartTime[syncerKey] == clonedStartTime) {
				t.Errorf("updateMigrationStartAndEndTime(%v, %v): startTimeBefore=%v, startTimeAfter=%v; want change=%v", syncerKey, tc.migrationCount, clonedStartTime.UnixNano(), sm.dualStackMigrationStartTime[syncerKey].UnixNano(), tc.wantChangedStartTime)
			}
			if tc.wantChangedEndTime == (sm.dualStackMigrationEndTime[syncerKey] == clonedEndTime) {
				t.Errorf("updateMigrationStartAndEndTime(%v, %v): endTimeBefore=%v, endTimeAfter=%v; want change=%v", syncerKey, tc.migrationCount, clonedEndTime.UnixNano(), sm.dualStackMigrationStartTime[syncerKey].UnixNano(), tc.wantChangedEndTime)
			}
		})
	}
}

func TestUpdateEndpointsCountPerType(t *testing.T) {
	syncerKey := syncerKey(1)
	inputCommittedEndpoints := map[negtypes.NEGLocation]types.NetworkEndpointSet{
		{Zone: "zone1"}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
			{IP: "ipv4only-1"}, {IP: "ipv4only-2"}, {IP: "ipv4only-3"},
			{IPv6: "ipv6only-1"},
			{IP: "dualStack-1a", IPv6: "dualStack-1b"}, {IP: "dualStack-2a", IPv6: "dualStack-2b"},
		}...),
		{Zone: "zone2"}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
			{IP: "ipv4only-4"},
			{IP: "dualStack-3a", IPv6: "dualStack-3b"},
		}...),
	}
	inputMigrationCount := 10
	inputEndpointsCountPerType := map[types.NegSyncerKey]map[string]int{
		syncerKey: {
			ipv4EndpointType:      10,
			ipv6EndpointType:      9,
			dualStackEndpointType: 15,
			migrationEndpointType: 7,
		},
	}
	sm := &SyncerMetrics{
		endpointsCountPerType: inputEndpointsCountPerType,
	}

	wantEndpointsCountPerType := map[types.NegSyncerKey]map[string]int{
		syncerKey: {
			ipv4EndpointType:      4,
			ipv6EndpointType:      1,
			dualStackEndpointType: 3,
			migrationEndpointType: 10,
		},
	}

	sm.updateEndpointsCountPerType(syncerKey, inputCommittedEndpoints, inputMigrationCount)

	if diff := cmp.Diff(wantEndpointsCountPerType, sm.endpointsCountPerType); diff != "" {
		t.Errorf("updateEndpointsCountPerType(...) = Unexpected diff in endpointsCountPerType: (-want +got):\n%s", diff)
	}
}

func TestComputeDualStackMigrationDurations(t *testing.T) {
	curTime := time.Unix(50, 0)
	inputDualStackMigrationStartTime := map[types.NegSyncerKey]time.Time{
		syncerKey(1): time.Unix(5, 0),
		syncerKey(2): time.Unix(7, 0),
		syncerKey(3): time.Unix(6, 0),
		syncerKey(4): time.Unix(10, 0),
	}
	inputDualStackMigrationEndTime := map[types.NegSyncerKey]time.Time{
		syncerKey(1): time.Unix(10, 0),
		syncerKey(3): time.Unix(9, 0),
	}
	sm := &SyncerMetrics{
		dualStackMigrationStartTime: inputDualStackMigrationStartTime,
		dualStackMigrationEndTime:   inputDualStackMigrationEndTime,
		clock:                       &fakeClock{curTime: curTime},
	}

	wantFinishedDurations := []int{
		int(time.Unix(10, 0).Sub(time.Unix(5, 0)).Seconds()), // 10 - 5
		int(time.Unix(9, 0).Sub(time.Unix(6, 0)).Seconds()),  // 9 - 6
	}
	wantLongestUnfinishedDuration := int(curTime.Sub(time.Unix(7, 0)).Seconds()) // 50 - 7

	gotFinishedDurations, gotLongestUnfinishedDuration := sm.computeDualStackMigrationDurations()

	sortSlices := cmpopts.SortSlices(func(a, b int) bool { return a < b })
	if diff := cmp.Diff(wantFinishedDurations, gotFinishedDurations, sortSlices); diff != "" {
		t.Errorf("computeDualStackMigrationDurations() = Unexpected diff in finishedDurations: (-want +got):\n%s", diff)
	}
	if gotLongestUnfinishedDuration != wantLongestUnfinishedDuration {
		t.Errorf("computeDualStackMigrationDurations() returned longestUnfinishedDuration=%v; want=%v", gotLongestUnfinishedDuration, wantLongestUnfinishedDuration)
	}

	// Ensure that finished durations are not returned more than once but
	// longestUnfinishedDuration duration is returned until it completes.
	gotFinishedDurations, gotLongestUnfinishedDuration = sm.computeDualStackMigrationDurations()
	if len(gotFinishedDurations) != 0 {
		t.Errorf("computeDualStackMigrationDurations() returned non-empty finishedDurations; want finishedDurations to be empty if computeDualStackMigrationDurations is invoked more than once.")
	}
	if gotLongestUnfinishedDuration != wantLongestUnfinishedDuration {
		t.Errorf("computeDualStackMigrationDurations() returned longestUnfinishedDuration=%v; want=%v", gotLongestUnfinishedDuration, wantLongestUnfinishedDuration)
	}
}

func TestComputeDualStackMigrationCounts(t *testing.T) {
	inputEndpointsCountPerType := map[types.NegSyncerKey]map[string]int{
		syncerKeyWithPort(1, 8080): {ipv4EndpointType: 1, ipv6EndpointType: 5, dualStackEndpointType: 9, migrationEndpointType: 13},
		syncerKeyWithPort(2, 8080): {ipv6EndpointType: 6, migrationEndpointType: 14},
		syncerKeyWithPort(1, 8443): {ipv4EndpointType: 3, ipv6EndpointType: 7, dualStackEndpointType: 11},
		syncerKeyWithPort(2, 8443): {ipv6EndpointType: 8, dualStackEndpointType: 12, migrationEndpointType: 16},
		syncerKeyWithPort(3, 80):   {ipv4EndpointType: 10, ipv6EndpointType: 20, dualStackEndpointType: 30},
	}
	sm := &SyncerMetrics{
		endpointsCountPerType: inputEndpointsCountPerType,
	}

	wantSyncerCountByEndpointType := map[string]int{
		ipv4EndpointType:      3,
		ipv6EndpointType:      5,
		dualStackEndpointType: 4,
		migrationEndpointType: 3,
	}
	wantMigrationEndpointCount := 13 + 14 + 16
	wantMigrationServicesCount := 2

	gotSyncerCountByEndpointType, gotMigrationEndpointCount, gotMigrationServicesCount := sm.computeDualStackMigrationCounts()

	if diff := cmp.Diff(wantSyncerCountByEndpointType, gotSyncerCountByEndpointType); diff != "" {
		t.Errorf("computeDualStackMigrationCounts() = Unexpected diff in negCountByEndpointType: (-want +got):\n%s", diff)
	}
	if gotMigrationEndpointCount != wantMigrationEndpointCount {
		t.Errorf("computeDualStackMigrationCounts() returned migrationEndpointCount=%v: want=%v", gotMigrationEndpointCount, wantMigrationEndpointCount)
	}
	if gotMigrationServicesCount != wantMigrationServicesCount {
		t.Errorf("computeDualStackMigrationCounts() returned migrationServicesCount=%v: want=%v", gotMigrationServicesCount, wantMigrationServicesCount)
	}
}

func TestComputeLabelMetrics(t *testing.T) {
	collector := FakeSyncerMetrics()
	syncer1 := negtypes.NegSyncerKey{
		Namespace:        "ns1",
		Name:             "svc-1",
		NegName:          "neg-1",
		NegType:          negtypes.VmIpPortEndpointType,
		EpCalculatorMode: negtypes.L7Mode,
	}
	syncer2 := negtypes.NegSyncerKey{
		Namespace:        "ns1",
		Name:             "svc-2",
		NegName:          "neg-2",
		NegType:          negtypes.VmIpPortEndpointType,
		EpCalculatorMode: negtypes.L7Mode,
	}
	for _, tc := range []struct {
		desc                       string
		syncerLabelProagationStats map[negtypes.NegSyncerKey]LabelPropagationStats
		expect                     LabelPropagationMetrics
	}{
		{
			desc: "Empty Data",
			syncerLabelProagationStats: map[negtypes.NegSyncerKey]LabelPropagationStats{
				syncer1: {},
			},
			expect: LabelPropagationMetrics{
				EndpointsWithAnnotation: 0,
				NumberOfEndpoints:       0,
			},
		},
		{
			desc: "All endpoints have annotations",
			syncerLabelProagationStats: map[negtypes.NegSyncerKey]LabelPropagationStats{
				syncer1: {
					EndpointsWithAnnotation: 10,
					NumberOfEndpoints:       10,
				},
			},
			expect: LabelPropagationMetrics{
				EndpointsWithAnnotation: 10,
				NumberOfEndpoints:       10,
			},
		},
		{
			desc: "Test with 2 syncers",
			syncerLabelProagationStats: map[negtypes.NegSyncerKey]LabelPropagationStats{
				syncer1: {
					EndpointsWithAnnotation: 10,
					NumberOfEndpoints:       10,
				},
				syncer2: {
					EndpointsWithAnnotation: 5,
					NumberOfEndpoints:       10,
				},
			},
			expect: LabelPropagationMetrics{
				EndpointsWithAnnotation: 15,
				NumberOfEndpoints:       20,
			},
		},
	} {
		collector.syncerLabelProagationStats = tc.syncerLabelProagationStats
		out := collector.computeLabelMetrics()
		if diff := cmp.Diff(out, tc.expect); diff != "" {
			t.Errorf("For test case %s,  (-want +got):\n%s", tc.desc, diff)
		}
	}
}

func TestComputeNegCounts(t *testing.T) {
	collector := FakeSyncerMetrics()
	l7Syncer1 := negtypes.NegSyncerKey{
		Namespace:        "ns1",
		Name:             "svc-1",
		NegName:          "neg-l7-1",
		NegType:          negtypes.VmIpPortEndpointType,
		EpCalculatorMode: negtypes.L7Mode,
	}
	l7Syncer2 := negtypes.NegSyncerKey{
		Namespace:        "ns1",
		Name:             "svc-2",
		NegName:          "neg-l7-2",
		NegType:          negtypes.VmIpPortEndpointType,
		EpCalculatorMode: negtypes.L7Mode,
	}
	l4Syncer1 := negtypes.NegSyncerKey{
		Namespace:        "ns2",
		Name:             "svc-1",
		NegName:          "neg-l4-1",
		NegType:          negtypes.VmIpEndpointType,
		EpCalculatorMode: negtypes.L7Mode,
	}
	l4Syncer2 := negtypes.NegSyncerKey{
		Namespace:        "ns2",
		Name:             "svc-2",
		NegName:          "neg-l4-2",
		NegType:          negtypes.VmIpEndpointType,
		EpCalculatorMode: negtypes.L7Mode,
	}
	for _, tc := range []struct {
		desc           string
		syncerNegCount map[negtypes.NegSyncerKey]map[string]int
		expect         map[negLocTypeKey]int
	}{
		{
			desc:           "Empty Data",
			syncerNegCount: map[negtypes.NegSyncerKey]map[string]int{},
			expect:         map[negLocTypeKey]int{},
		},
		{
			desc: "Single syncers for each type",
			syncerNegCount: map[negtypes.NegSyncerKey]map[string]int{
				l7Syncer1: {
					"zone1": 1,
					"zone2": 1,
				},
				l4Syncer1: {
					"zone1": 1,
					"zone3": 1,
				},
			},
			expect: map[negLocTypeKey]int{
				{location: "zone1", endpointType: string(negtypes.VmIpPortEndpointType)}: 1,
				{location: "zone2", endpointType: string(negtypes.VmIpPortEndpointType)}: 1,
				{location: "zone1", endpointType: string(negtypes.VmIpEndpointType)}:     1,
				{location: "zone3", endpointType: string(negtypes.VmIpEndpointType)}:     1,
			},
		},
		{
			desc: "Multiple syncers per type",
			syncerNegCount: map[negtypes.NegSyncerKey]map[string]int{
				l7Syncer1: {
					"zone1": 1,
					"zone2": 1,
				},
				l7Syncer2: {
					"zone1": 1,
					"zone4": 1,
				},
				l4Syncer1: {
					"zone1": 1,
					"zone3": 1,
				},
				l4Syncer2: {
					"zone2": 1,
					"zone3": 1,
				},
			},
			expect: map[negLocTypeKey]int{
				{location: "zone1", endpointType: string(negtypes.VmIpPortEndpointType)}: 2,
				{location: "zone2", endpointType: string(negtypes.VmIpPortEndpointType)}: 1,
				{location: "zone4", endpointType: string(negtypes.VmIpPortEndpointType)}: 1,
				{location: "zone1", endpointType: string(negtypes.VmIpEndpointType)}:     1,
				{location: "zone2", endpointType: string(negtypes.VmIpEndpointType)}:     1,
				{location: "zone3", endpointType: string(negtypes.VmIpEndpointType)}:     2,
			},
		},
	} {
		collector.syncerNegCount = tc.syncerNegCount
		out := collector.computeNegCounts()
		if diff := cmp.Diff(out, tc.expect); diff != "" {
			t.Errorf("For test case %s,  (-got +want):\n%s", tc.desc, diff)
		}
	}
}

func TestComputeNegMetrics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc           string
		negStates      []NegServiceState
		expectNegCount map[feature]int
	}{
		{
			"empty input",
			[]NegServiceState{},
			map[feature]int{
				standaloneNeg:     0,
				ingressNeg:        0,
				neg:               0,
				vmIpNeg:           0,
				vmIpNegLocal:      0,
				vmIpNegCluster:    0,
				customNamedNeg:    0,
				preprovisionedNeg: 0,
				negBindingNeg:     0,
				negInSuccess:      0,
				negInError:        0,
			},
		},
		{
			"one neg service",
			[]NegServiceState{
				newNegState(0, 0, 0, 0, 1, 0, nil),
			},
			map[feature]int{
				standaloneNeg:     0,
				ingressNeg:        0,
				neg:               0,
				vmIpNeg:           0,
				vmIpNegLocal:      0,
				vmIpNegCluster:    0,
				customNamedNeg:    0,
				preprovisionedNeg: 0,
				negBindingNeg:     0,
				negInSuccess:      1,
				negInError:        0,
			},
		},
		{
			"vm primary ip neg in traffic policy cluster mode",
			[]NegServiceState{
				newNegState(0, 0, 0, 0, 1, 0, &VmIpNegType{trafficPolicyLocal: false}),
			},
			map[feature]int{
				standaloneNeg:     0,
				ingressNeg:        0,
				neg:               1,
				vmIpNeg:           1,
				vmIpNegLocal:      0,
				vmIpNegCluster:    1,
				customNamedNeg:    0,
				preprovisionedNeg: 0,
				negBindingNeg:     0,
				negInSuccess:      1,
				negInError:        0,
			},
		},
		{
			"custom named neg",
			[]NegServiceState{
				newNegState(1, 0, 1, 0, 1, 0, nil),
			},
			map[feature]int{
				standaloneNeg:     1,
				ingressNeg:        0,
				neg:               1,
				vmIpNeg:           0,
				vmIpNegLocal:      0,
				vmIpNegCluster:    0,
				customNamedNeg:    1,
				preprovisionedNeg: 0,
				negBindingNeg:     0,
				negInSuccess:      1,
				negInError:        0,
			},
		},
		{
			"preprovisioning neg",
			[]NegServiceState{
				newNegState(1, 0, 0, 1, 1, 0, nil),
			},
			map[feature]int{
				standaloneNeg:     1,
				ingressNeg:        0,
				neg:               1,
				vmIpNeg:           0,
				vmIpNegLocal:      0,
				vmIpNegCluster:    0,
				customNamedNeg:    0,
				preprovisionedNeg: 1,
				negBindingNeg:     0,
				negInSuccess:      1,
				negInError:        0,
			},
		},
		{
			"many neg services",
			[]NegServiceState{
				newNegState(0, 0, 0, 0, 1, 0, nil),
				newNegState(0, 1, 0, 0, 0, 1, &VmIpNegType{trafficPolicyLocal: false}),
				newNegState(5, 0, 0, 2, 3, 2, &VmIpNegType{trafficPolicyLocal: true}),
				newNegState(5, 3, 0, 0, 2, 3, nil),
			},
			map[feature]int{
				standaloneNeg:     10,
				ingressNeg:        4,
				neg:               16,
				vmIpNeg:           2,
				vmIpNegLocal:      1,
				vmIpNegCluster:    1,
				customNamedNeg:    0,
				preprovisionedNeg: 2,
				negBindingNeg:     0,
				negInSuccess:      6,
				negInError:        6,
			},
		},
		{
			"negbinding neg",
			[]NegServiceState{
				{
					NegBindingNeg:        2,
					BindingSuccessfulNeg: 1,
					BindingErrorNeg:      1,
				},
			},
			map[feature]int{
				standaloneNeg:     0,
				ingressNeg:        0,
				neg:               2,
				vmIpNeg:           0,
				vmIpNegLocal:      0,
				vmIpNegCluster:    0,
				customNamedNeg:    0,
				preprovisionedNeg: 0,
				negBindingNeg:     2,
				negInSuccess:      1,
				negInError:        1,
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			newMetrics := FakeSyncerMetrics()
			for i, negState := range tc.negStates {
				newMetrics.SetNegService(fmt.Sprint(i), negState)
			}

			gotNegCount := newMetrics.computeNegMetrics()
			if diff := cmp.Diff(tc.expectNegCount, gotNegCount); diff != "" {
				t.Errorf("Got diff for NEG counts (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSetNegBindingService(t *testing.T) {
	svcKey := "test-ns/test-svc"

	testCases := []struct {
		desc         string
		initialState map[string]NegServiceState
		inputKey     string
		inputState   NegServiceState
		expectExist  bool
		expectState  NegServiceState
	}{
		{
			desc:         "NegBindingNeg=0 on non-existing entry does not create entry",
			initialState: nil,
			inputKey:     "test-ns/non-existing",
			inputState:   NegServiceState{NegBindingNeg: 0},
			expectExist:  false,
		},
		{
			desc:         "creates new entry when svcKey does not exist in negMap",
			initialState: nil,
			inputKey:     svcKey,
			inputState:   NegServiceState{NegBindingNeg: 1, BindingSuccessfulNeg: 1, BindingErrorNeg: 0},
			expectExist:  true,
			expectState:  NegServiceState{NegBindingNeg: 1, BindingSuccessfulNeg: 1, BindingErrorNeg: 0},
		},
		{
			desc: "updates existing entry while preserving standalone/ingress fields",
			initialState: map[string]NegServiceState{
				svcKey: {StandaloneNeg: 2, SuccessfulNeg: 1, ErrorNeg: 0},
			},
			inputKey:    svcKey,
			inputState:  NegServiceState{NegBindingNeg: 3, BindingSuccessfulNeg: 2, BindingErrorNeg: 1},
			expectExist: true,
			expectState: NegServiceState{StandaloneNeg: 2, SuccessfulNeg: 1, ErrorNeg: 0, NegBindingNeg: 3, BindingSuccessfulNeg: 2, BindingErrorNeg: 1},
		},
		{
			desc: "overwrites existing NegBindingNeg fields while preserving standalone fields",
			initialState: map[string]NegServiceState{
				svcKey: {StandaloneNeg: 2, SuccessfulNeg: 1, ErrorNeg: 0, NegBindingNeg: 1, BindingSuccessfulNeg: 1, BindingErrorNeg: 0},
			},
			inputKey:    svcKey,
			inputState:  NegServiceState{NegBindingNeg: 3, BindingSuccessfulNeg: 1, BindingErrorNeg: 2},
			expectExist: true,
			expectState: NegServiceState{StandaloneNeg: 2, SuccessfulNeg: 1, ErrorNeg: 0, NegBindingNeg: 3, BindingSuccessfulNeg: 1, BindingErrorNeg: 2},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			sm := FakeSyncerMetrics()
			for k, v := range tc.initialState {
				sm.negMap[k] = v
			}
			sm.SetNegBindingService(tc.inputKey, tc.inputState)

			state, ok := sm.GetNegService(tc.inputKey)
			if ok != tc.expectExist {
				t.Fatalf("GetNegService(%q) exist = %v, want %v", tc.inputKey, ok, tc.expectExist)
			}
			if tc.expectExist {
				if diff := cmp.Diff(tc.expectState, state); diff != "" {
					t.Errorf("GetNegService(%q) diff (-want +got):\n%s", tc.inputKey, diff)
				}
			}
		})
	}
}

func TestDeleteNegBindingService(t *testing.T) {
	svcKey := "test-ns/test-svc"

	testCases := []struct {
		desc         string
		initialState map[string]NegServiceState
		inputKey     string
		expectExist  bool
		expectState  NegServiceState
	}{
		{
			desc:         "non-existing key returns safely",
			initialState: nil,
			inputKey:     "test-ns/non-existing",
			expectExist:  false,
		},
		{
			desc: "deletes entry when only NegBindingNeg fields were present",
			initialState: map[string]NegServiceState{
				svcKey: {NegBindingNeg: 2, BindingSuccessfulNeg: 2, BindingErrorNeg: 0},
			},
			inputKey:    svcKey,
			expectExist: false,
		},
		{
			desc: "resets NegBindingNeg fields to zero while preserving standalone and ingress NEGs",
			initialState: map[string]NegServiceState{
				svcKey: {StandaloneNeg: 2, IngressNeg: 1, NegBindingNeg: 3, BindingSuccessfulNeg: 3},
			},
			inputKey:    svcKey,
			expectExist: true,
			expectState: NegServiceState{StandaloneNeg: 2, IngressNeg: 1, NegBindingNeg: 0, BindingSuccessfulNeg: 0, BindingErrorNeg: 0},
		},
		{
			desc: "resets NegBindingNeg fields to zero while preserving VmIpNeg",
			initialState: map[string]NegServiceState{
				svcKey: {VmIpNeg: &VmIpNegType{}, NegBindingNeg: 1, BindingSuccessfulNeg: 1},
			},
			inputKey:    svcKey,
			expectExist: true,
			expectState: NegServiceState{VmIpNeg: &VmIpNegType{}, NegBindingNeg: 0, BindingSuccessfulNeg: 0, BindingErrorNeg: 0},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			sm := FakeSyncerMetrics()
			for k, v := range tc.initialState {
				sm.negMap[k] = v
			}
			sm.DeleteNegBindingService(tc.inputKey)

			state, ok := sm.GetNegService(tc.inputKey)
			if ok != tc.expectExist {
				t.Fatalf("GetNegService(%q) exist = %v, want %v", tc.inputKey, ok, tc.expectExist)
			}
			if tc.expectExist {
				if diff := cmp.Diff(tc.expectState, state, cmpopts.IgnoreUnexported(VmIpNegType{})); diff != "" {
					t.Errorf("GetNegService(%q) diff (-want +got):\n%s", tc.inputKey, diff)
				}
			}
		})
	}
}

type fakeClock struct {
	clock.Clock
	curTime time.Time
}

func (f *fakeClock) Since(t time.Time) time.Duration {
	return f.curTime.Sub(t)
}

func syncerKey(i int32) types.NegSyncerKey {
	return syncerKeyWithPort(i, i)
}

func syncerKeyWithPort(i int32, port int32) types.NegSyncerKey {
	return types.NegSyncerKey{
		Namespace: fmt.Sprintf("ns-%v", i),
		Name:      fmt.Sprintf("name-%v", i),
		NegName:   fmt.Sprintf("neg-name-%v", i),
		PortTuple: types.SvcPortTuple{Port: port},
	}
}

func newNegState(standalone, ingress, customNamed, preprovisioning, success, err int, negType *VmIpNegType) NegServiceState {
	return NegServiceState{
		IngressNeg:        ingress,
		StandaloneNeg:     standalone,
		VmIpNeg:           negType,
		CustomNamedNeg:    customNamed,
		PreprovisionedNeg: preprovisioning,
		SuccessfulNeg:     success,
		ErrorNeg:          err,
	}
}

func findCollectorMetricFamily(gatherer prometheus.Gatherer, name string) *dto_pb.MetricFamily {
	mfs, err := gatherer.Gather()
	if err != nil {
		return nil
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf
		}
	}
	return nil
}

func getCollectorMetricLabel(m *dto_pb.Metric, labelName string) string {
	for _, lp := range m.GetLabel() {
		if lp.GetName() == labelName {
			return lp.GetValue()
		}
	}
	return ""
}

// TestMultiTenantSyncerMetrics_TenantUIDTaggingAndExport verifies that SyncerMetrics
// created via MTMetricFactory has tenant_uid tagged on metrics in the tenant registry.
func TestMultiTenantSyncerMetrics_TenantUIDTaggingAndExport(t *testing.T) {
	globalReg := prometheus.NewRegistry()
	tracker := mtmetrics.NewGlobalMetricsTracker()

	tenant1UID := "tenant-sync-1"
	tenant2UID := "tenant-sync-2"

	factory1 := mtmetrics.NewMTMetricFactory(tenant1UID, globalReg, tracker)
	defer factory1.Cleanup()
	sm1, err := NewNegMetricsCollectorWithFactory(5*time.Second, factory1, klog.TODO())
	if err != nil {
		t.Fatalf("failed to create SyncerMetrics 1: %v", err)
	}

	factory2 := mtmetrics.NewMTMetricFactory(tenant2UID, globalReg, tracker)
	defer factory2.Cleanup()
	sm2, err := NewNegMetricsCollectorWithFactory(5*time.Second, factory2, klog.TODO())
	if err != nil {
		t.Fatalf("failed to create SyncerMetrics 2: %v", err)
	}

	key1 := syncerKey(1)
	key2 := syncerKey(2)

	sm1.UpdateSyncerStatusInMetrics(key1, nil, false)
	sm1.UpdateSyncerEPMetrics(key1, types.StateCountMap{types.Duplicate: 3}, types.StateCountMap{})
	sm1.export()

	sm2.UpdateSyncerStatusInMetrics(key2, nil, false)
	sm2.UpdateSyncerEPMetrics(key2, types.StateCountMap{types.Duplicate: 7}, types.StateCountMap{})
	sm2.export()

	// Verify tenant1 metrics have tenant_uid = tenant-sync-1
	mf1 := findCollectorMetricFamily(factory1.Registry(), "neg_controller_syncer_endpoint_state")
	if mf1 == nil || len(mf1.GetMetric()) == 0 {
		t.Fatalf("expected neg_controller_syncer_endpoint_state in tenant1 registry, got nil or empty")
	}
	for _, m := range mf1.GetMetric() {
		if uid := getCollectorMetricLabel(m, "tenant_uid"); uid != tenant1UID {
			t.Errorf("expected tenant_uid=%q in tenant1 metric, got %q", tenant1UID, uid)
		}
	}

	// Verify tenant2 metrics have tenant_uid = tenant-sync-2
	mf2 := findCollectorMetricFamily(factory2.Registry(), "neg_controller_syncer_endpoint_state")
	if mf2 == nil || len(mf2.GetMetric()) == 0 {
		t.Fatalf("expected neg_controller_syncer_endpoint_state in tenant2 registry, got nil or empty")
	}
	for _, m := range mf2.GetMetric() {
		if uid := getCollectorMetricLabel(m, "tenant_uid"); uid != tenant2UID {
			t.Errorf("expected tenant_uid=%q in tenant2 metric, got %q", tenant2UID, uid)
		}
	}
}

// TestMultiTenantSyncerMetrics_DualStackMigrationLongestDurationMaxStrategy verifies
// that dual_stack_migration_longest_unfinished_duration_seconds uses StrategyMax across tenants.
func TestMultiTenantSyncerMetrics_DualStackMigrationLongestDurationMaxStrategy(t *testing.T) {
	globalReg := prometheus.NewRegistry()
	tracker := mtmetrics.NewGlobalMetricsTracker()
	tracker.SetAggregationStrategy("neg_controller_dual_stack_migration_longest_unfinished_duration_seconds", mtmetrics.StrategyMax)

	tenant1UID := "tenant-ds-1"
	tenant2UID := "tenant-ds-2"

	factory1 := mtmetrics.NewMTMetricFactory(tenant1UID, globalReg, tracker)
	defer factory1.Cleanup()
	sm1, err := NewNegMetricsCollectorWithFactory(5*time.Second, factory1, klog.TODO())
	if err != nil {
		t.Fatalf("failed to create SyncerMetrics 1: %v", err)
	}

	factory2 := mtmetrics.NewMTMetricFactory(tenant2UID, globalReg, tracker)
	defer factory2.Cleanup()
	sm2, err := NewNegMetricsCollectorWithFactory(5*time.Second, factory2, klog.TODO())
	if err != nil {
		t.Fatalf("failed to create SyncerMetrics 2: %v", err)
	}

	now := time.Now()
	sm1.clock = &fakeClock{curTime: now}
	sm2.clock = &fakeClock{curTime: now}

	// Syncer 1 migration started 100 seconds ago
	sm1.dualStackMigrationStartTime[syncerKey(1)] = now.Add(-100 * time.Second)
	// Syncer 2 migration started 250 seconds ago (longer)
	sm2.dualStackMigrationStartTime[syncerKey(2)] = now.Add(-250 * time.Second)

	sm1.export()
	sm2.export()

	mf := findCollectorMetricFamily(globalReg, "neg_controller_dual_stack_migration_longest_unfinished_duration_seconds")
	if mf == nil || len(mf.GetMetric()) == 0 {
		t.Fatalf("expected neg_controller_dual_stack_migration_longest_unfinished_duration_seconds in globalReg, got nil or empty")
	}
	gotVal := mf.GetMetric()[0].GetGauge().GetValue()
	// Should be 250, NOT 350 (sum)
	if gotVal != 250 {
		t.Errorf("expected global longest_unfinished_duration to be max (250), got %v", gotVal)
	}

	// Update syncer 2 to have migration started 50s ago, making syncer 1 (100s) the longest
	sm2.dualStackMigrationStartTime[syncerKey(2)] = now.Add(-50 * time.Second)
	sm2.export()

	mf = findCollectorMetricFamily(globalReg, "neg_controller_dual_stack_migration_longest_unfinished_duration_seconds")
	gotVal = mf.GetMetric()[0].GetGauge().GetValue()
	if gotVal != 100 {
		t.Errorf("expected updated global longest_unfinished_duration to be max (100), got %v", gotVal)
	}

	// When syncer 1 finishes migration and is cleaned up, syncer 2 duration (50s) remains
	factory1.Cleanup()

	mf = findCollectorMetricFamily(globalReg, "neg_controller_dual_stack_migration_longest_unfinished_duration_seconds")
	gotVal = mf.GetMetric()[0].GetGauge().GetValue()
	if gotVal != 50 {
		t.Errorf("expected reverted global longest_unfinished_duration to be (50), got %v", gotVal)
	}
}

// TestDefaultGlobalTracker_DualStackLongestDurationStrategy verifies that init() correctly
// registered StrategyMax for neg_controller_dual_stack_migration_longest_unfinished_duration_seconds
// in DefaultGlobalTracker.
func TestDefaultGlobalTracker_DualStackLongestDurationStrategy(t *testing.T) {
	globalReg := prometheus.NewRegistry()
	tracker := mtmetrics.DefaultGlobalTracker

	factory1 := mtmetrics.NewMTMetricFactory("tenant-ds-def-1", globalReg, tracker)
	defer factory1.Cleanup()
	sm1, err := NewNegMetricsCollectorWithFactory(5*time.Second, factory1, klog.TODO())
	if err != nil {
		t.Fatalf("failed to create SyncerMetrics 1: %v", err)
	}

	factory2 := mtmetrics.NewMTMetricFactory("tenant-ds-def-2", globalReg, tracker)
	defer factory2.Cleanup()
	sm2, err := NewNegMetricsCollectorWithFactory(5*time.Second, factory2, klog.TODO())
	if err != nil {
		t.Fatalf("failed to create SyncerMetrics 2: %v", err)
	}

	now := time.Now()
	sm1.clock = &fakeClock{curTime: now}
	sm2.clock = &fakeClock{curTime: now}

	sm1.dualStackMigrationStartTime[syncerKey(1)] = now.Add(-120 * time.Second)
	sm2.dualStackMigrationStartTime[syncerKey(2)] = now.Add(-400 * time.Second)

	sm1.export()
	sm2.export()

	mf := findCollectorMetricFamily(globalReg, "neg_controller_dual_stack_migration_longest_unfinished_duration_seconds")
	if mf == nil || len(mf.GetMetric()) == 0 {
		t.Fatalf("expected neg_controller_dual_stack_migration_longest_unfinished_duration_seconds in globalReg, got nil or empty")
	}
	gotVal := mf.GetMetric()[0].GetGauge().GetValue()
	if gotVal != 400 {
		t.Errorf("DefaultGlobalTracker strategy for longest_unfinished_duration: got %v, want max 400", gotVal)
	}
}
