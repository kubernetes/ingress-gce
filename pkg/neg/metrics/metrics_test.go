/*
Copyright 2026 The Kubernetes Authors.

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

package metrics

import (
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/gke-enterprise-mt/pkg/mtmetrics"
	"github.com/prometheus/client_golang/prometheus"
	dto_pb "github.com/prometheus/client_model/go"
)

func findMetricFamily(gatherer prometheus.Gatherer, name string) *dto_pb.MetricFamily {
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

func getMetricLabel(m *dto_pb.Metric, labelName string) string {
	for _, lp := range m.GetLabel() {
		if lp.GetName() == labelName {
			return lp.GetValue()
		}
	}
	return ""
}

// TestMultiTenantNegMetrics_TenantUIDTagging verifies that metrics registered
// via MTMetricFactory are properly tagged with tenant_uid in the tenant registry.
func TestMultiTenantNegMetrics_TenantUIDTagging(t *testing.T) {
	globalReg := prometheus.NewRegistry()
	tracker := mtmetrics.NewGlobalMetricsTracker()

	tenant1UID := "tenant-alpha"
	tenant2UID := "tenant-beta"

	factory1 := mtmetrics.NewMTMetricFactory(tenant1UID, globalReg, tracker)
	defer factory1.Cleanup()
	negMetrics1, err := NewNegMetricsWithFactory(factory1)
	if err != nil {
		t.Fatalf("failed to create NegMetrics for tenant1: %v", err)
	}

	factory2 := mtmetrics.NewMTMetricFactory(tenant2UID, globalReg, tracker)
	defer factory2.Cleanup()
	negMetrics2, err := NewNegMetricsWithFactory(factory2)
	if err != nil {
		t.Fatalf("failed to create NegMetrics for tenant2: %v", err)
	}

	// Publish metrics for tenant1
	negMetrics1.PublishNegOperationMetrics("Attach", "neg-v1", "v1", nil, 5, time.Now().Add(-time.Second))
	negMetrics1.PublishNegControllerErrorCountMetrics(nil, false) // no-op
	negMetrics1.PublishLabelPropagationError("test_error")

	// Publish metrics for tenant2
	negMetrics2.PublishNegOperationMetrics("Detach", "neg-v1", "v1", nil, 2, time.Now().Add(-time.Second))

	// Verify tenant1 registry has metrics with tenant_uid = tenant-alpha
	mf1 := findMetricFamily(factory1.Registry(), "neg_controller_neg_operation_endpoints")
	if mf1 == nil {
		t.Fatalf("expected neg_controller_neg_operation_endpoints in tenant1 registry, got nil")
	}
	if len(mf1.GetMetric()) == 0 {
		t.Fatalf("expected at least 1 metric in tenant1 registry, got 0")
	}
	for _, m := range mf1.GetMetric() {
		if uid := getMetricLabel(m, "tenant_uid"); uid != tenant1UID {
			t.Errorf("expected tenant_uid=%q in tenant1 metric, got %q", tenant1UID, uid)
		}
	}

	// Verify tenant2 registry has metrics with tenant_uid = tenant-beta
	mf2 := findMetricFamily(factory2.Registry(), "neg_controller_neg_operation_endpoints")
	if mf2 == nil {
		t.Fatalf("expected neg_controller_neg_operation_endpoints in tenant2 registry, got nil")
	}
	for _, m := range mf2.GetMetric() {
		if uid := getMetricLabel(m, "tenant_uid"); uid != tenant2UID {
			t.Errorf("expected tenant_uid=%q in tenant2 metric, got %q", tenant2UID, uid)
		}
	}

	// Verify MultiGatherer collects from both tenants with respective tenant_uid
	multiGatherer := mtmetrics.NewMultiGatherer()
	if err := multiGatherer.Register(tenant1UID, factory1.Registry()); err != nil {
		t.Fatalf("failed to register tenant1: %v", err)
	}
	defer multiGatherer.Unregister(tenant1UID)
	if err := multiGatherer.Register(tenant2UID, factory2.Registry()); err != nil {
		t.Fatalf("failed to register tenant2: %v", err)
	}
	defer multiGatherer.Unregister(tenant2UID)

	multiMF := findMetricFamily(multiGatherer, "neg_controller_neg_operation_endpoints")
	if multiMF == nil {
		t.Fatalf("expected neg_controller_neg_operation_endpoints in multiGatherer, got nil")
	}
	foundUIDs := make(map[string]bool)
	for _, m := range multiMF.GetMetric() {
		if uid := getMetricLabel(m, "tenant_uid"); uid != "" {
			foundUIDs[uid] = true
		}
	}
	if !foundUIDs[tenant1UID] || !foundUIDs[tenant2UID] {
		t.Errorf("expected multiGatherer to have metrics for both tenants, got %v", foundUIDs)
	}

	// Unregister tenant1 and verify only tenant2's metrics remain in multiGatherer
	multiGatherer.Unregister(tenant1UID)
	multiMFAfterT1 := findMetricFamily(multiGatherer, "neg_controller_neg_operation_endpoints")
	if multiMFAfterT1 == nil {
		t.Fatalf("expected neg_controller_neg_operation_endpoints in multiGatherer after unregistering tenant1, got nil")
	}
	for _, m := range multiMFAfterT1.GetMetric() {
		if uid := getMetricLabel(m, "tenant_uid"); uid != tenant2UID {
			t.Errorf("expected only tenant_uid=%q after unregistering tenant1, got %q", tenant2UID, uid)
		}
	}

	// Unregister tenant2 and verify no metrics remain in multiGatherer
	multiGatherer.Unregister(tenant2UID)
	if multiMFAfterT2 := findMetricFamily(multiGatherer, "neg_controller_neg_operation_endpoints"); multiMFAfterT2 != nil {
		t.Errorf("expected no neg_controller_neg_operation_endpoints in multiGatherer after unregistering both tenants, got %v", multiMFAfterT2)
	}
}

// TestMultiTenantNegMetrics_GaugeAggregationMax verifies that the gauge
// neg_controller_sync_timestamp aggregates using StrategyMax across tenants.
func TestMultiTenantNegMetrics_GaugeAggregationMax(t *testing.T) {
	globalReg := prometheus.NewRegistry()
	tracker := mtmetrics.NewGlobalMetricsTracker()

	// Set StrategyMax explicitly on tracker to test isolated aggregation
	tracker.SetAggregationStrategy("neg_controller_sync_timestamp", mtmetrics.StrategyMax)

	tenant1UID := "tenant-1"
	tenant2UID := "tenant-2"

	factory1 := mtmetrics.NewMTMetricFactory(tenant1UID, globalReg, tracker)
	defer factory1.Cleanup()
	negMetrics1, err := NewNegMetricsWithFactory(factory1)
	if err != nil {
		t.Fatalf("failed to create NegMetrics for tenant1: %v", err)
	}

	factory2 := mtmetrics.NewMTMetricFactory(tenant2UID, globalReg, tracker)
	defer factory2.Cleanup()
	negMetrics2, err := NewNegMetricsWithFactory(factory2)
	if err != nil {
		t.Fatalf("failed to create NegMetrics for tenant2: %v", err)
	}

	t1 := time.Unix(1000, 0)
	t2 := time.Unix(2500, 0) // t2 > t1

	negMetrics1.PublishLastSyncTimestamp(t1)
	negMetrics2.PublishLastSyncTimestamp(t2)

	// Global registry should reflect the MAX of the two timestamps: float64(t2.UnixNano())
	mf := findMetricFamily(globalReg, "neg_controller_sync_timestamp")
	if mf == nil || len(mf.GetMetric()) == 0 {
		t.Fatalf("expected neg_controller_sync_timestamp in globalReg, got nil or empty")
	}
	gotVal := mf.GetMetric()[0].GetGauge().GetValue()
	expectedMaxVal := float64(t2.UTC().UnixNano())
	if gotVal != expectedMaxVal {
		t.Errorf("expected global neg_controller_sync_timestamp to be max (%v), got %v", expectedMaxVal, gotVal)
	}

	// Update tenant1 to an even higher timestamp t3 > t2
	t3 := time.Unix(5000, 0)
	negMetrics1.PublishLastSyncTimestamp(t3)

	mf = findMetricFamily(globalReg, "neg_controller_sync_timestamp")
	gotVal = mf.GetMetric()[0].GetGauge().GetValue()
	expectedMaxVal = float64(t3.UTC().UnixNano())
	if gotVal != expectedMaxVal {
		t.Errorf("expected updated global neg_controller_sync_timestamp to be (%v), got %v", expectedMaxVal, gotVal)
	}

	// When tenant1 is cleaned up, the global gauge should revert to tenant2's timestamp (t2)
	factory1.Cleanup()

	mf = findMetricFamily(globalReg, "neg_controller_sync_timestamp")
	gotVal = mf.GetMetric()[0].GetGauge().GetValue()
	expectedRevertedVal := float64(t2.UTC().UnixNano())
	if gotVal != expectedRevertedVal {
		t.Errorf("expected reverted global neg_controller_sync_timestamp to be (%v), got %v", expectedRevertedVal, gotVal)
	}
}

// TestDefaultGlobalTracker_SyncTimestampStrategy verifies that init() correctly
// registered StrategyMax for neg_controller_sync_timestamp in DefaultGlobalTracker.
func TestDefaultGlobalTracker_SyncTimestampStrategy(t *testing.T) {
	globalReg := prometheus.NewRegistry()
	tracker := mtmetrics.DefaultGlobalTracker

	factory1 := mtmetrics.NewMTMetricFactory("tenant-def-1", globalReg, tracker)
	defer factory1.Cleanup()
	m1, err := NewNegMetricsWithFactory(factory1)
	if err != nil {
		t.Fatalf("failed to create NegMetrics 1: %v", err)
	}

	factory2 := mtmetrics.NewMTMetricFactory("tenant-def-2", globalReg, tracker)
	defer factory2.Cleanup()
	m2, err := NewNegMetricsWithFactory(factory2)
	if err != nil {
		t.Fatalf("failed to create NegMetrics 2: %v", err)
	}

	t1 := time.Unix(100, 0)
	t2 := time.Unix(500, 0)

	m1.PublishLastSyncTimestamp(t1)
	m2.PublishLastSyncTimestamp(t2)

	mf := findMetricFamily(globalReg, "neg_controller_sync_timestamp")
	if mf == nil || len(mf.GetMetric()) == 0 {
		t.Fatalf("expected neg_controller_sync_timestamp in globalReg, got nil or empty")
	}
	gotVal := mf.GetMetric()[0].GetGauge().GetValue()
	wantVal := float64(t2.UTC().UnixNano())
	if gotVal != wantVal {
		t.Errorf("DefaultGlobalTracker strategy for sync_timestamp: got %v, want max %v", gotVal, wantVal)
	}
}
