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
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestExportStandaloneNEGMetric_Success(t *testing.T) {
	newMetrics := NewFakeCollector()

	newMetrics.SetL4StandaloneNEGService("svc-success-external", L4StandaloneNEGServiceState{
		Status:           StatusSuccess,
		LBSchemeExternal: true,
	})
	newMetrics.SetL4StandaloneNEGService("svc-success-external-passthrough", L4StandaloneNEGServiceState{
		Status:                      StatusSuccess,
		LBSchemeExternalPassthrough: true,
	})
	newMetrics.SetL4StandaloneNEGService("svc-success-internal", L4StandaloneNEGServiceState{
		Status:           StatusSuccess,
		LBSchemeInternal: true,
	})
	newMetrics.SetL4StandaloneNEGService("svc-success-multiple-schemes", L4StandaloneNEGServiceState{
		Status:           StatusSuccess,
		LBSchemeExternal: true,
		LBSchemeInternal: true,
	})

	newMetrics.exportStandaloneNEG()

	verifyL4StandaloneNEGMetric(t, 1, StatusSuccess, "true", "false", "false")
	verifyL4StandaloneNEGMetric(t, 1, StatusSuccess, "false", "true", "false")
	verifyL4StandaloneNEGMetric(t, 1, StatusSuccess, "false", "false", "true")
	verifyL4StandaloneNEGMetric(t, 1, StatusSuccess, "true", "false", "true")
}

func TestExportStandaloneNEGMetric_UserError(t *testing.T) {
	newMetrics := NewFakeCollector()

	newMetrics.SetL4StandaloneNEGService("svc-user-error", L4StandaloneNEGServiceState{
		Status:           StatusUserError,
		LBSchemeExternal: true,
	})

	newMetrics.exportStandaloneNEG()

	verifyL4StandaloneNEGMetric(t, 1, StatusUserError, "true", "false", "false")
}

func TestExportStandaloneNEGMetric_Error(t *testing.T) {
	newMetrics := NewFakeCollector()
	notExceedingPersistentErrorThresholdTime := time.Now().Add(-1*persistentErrorThresholdTime + 5*time.Minute)

	newMetrics.SetL4StandaloneNEGService("svc-error", L4StandaloneNEGServiceState{
		Status:             StatusError,
		LBSchemeExternal:   true,
		FirstSyncErrorTime: &notExceedingPersistentErrorThresholdTime,
	})

	newMetrics.exportStandaloneNEG()

	verifyL4StandaloneNEGMetric(t, 1, StatusError, "true", "false", "false")
}

func TestExportStandaloneNEGMetric_PersistentError(t *testing.T) {
	newMetrics := NewFakeCollector()
	pastPersistentErrorThresholdTime := time.Now().Add(-1*persistentErrorThresholdTime - time.Minute)

	newMetrics.SetL4StandaloneNEGService("svc-persistent-error", L4StandaloneNEGServiceState{
		Status:             StatusError,
		LBSchemeExternal:   true,
		FirstSyncErrorTime: &pastPersistentErrorThresholdTime,
	})

	newMetrics.exportStandaloneNEG()

	verifyL4StandaloneNEGMetric(t, 1, StatusPersistentError, "true", "false", "false")
}

func TestExportStandaloneNEGMetric_Deletion(t *testing.T) {
	newMetrics := NewFakeCollector()

	// Setup initial state
	newMetrics.SetL4StandaloneNEGService("svc-success-external", L4StandaloneNEGServiceState{
		Status:           StatusSuccess,
		LBSchemeExternal: true,
	})
	newMetrics.exportStandaloneNEG()
	verifyL4StandaloneNEGMetric(t, 1, StatusSuccess, "true", "false", "false")

	// Act
	newMetrics.DeleteL4StandaloneNEGService("svc-success-external")
	newMetrics.exportStandaloneNEG()

	// Assert
	verifyL4StandaloneNEGMetric(t, 0, StatusSuccess, "true", "false", "false")
}

func verifyL4StandaloneNEGMetric(t *testing.T, expectedCount int, status L4ServiceStatus, external, externalPassthrough, internal string) {
	countFloat := testutil.ToFloat64(l4StandaloneNEGCount.With(prometheus.Labels{
		"status":                         string(status),
		"lb_scheme_external":             external,
		"lb_scheme_external_passthrough": externalPassthrough,
		"lb_scheme_internal":             internal,
	}))
	actualCount := int(math.Round(countFloat))
	if expectedCount != actualCount {
		t.Errorf("expected value %d but got %d for status: %q, external: %q, externalPassthrough: %q, internal: %q",
			expectedCount, actualCount, status, external, externalPassthrough, internal)
	}
}
