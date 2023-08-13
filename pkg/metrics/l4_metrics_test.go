/*
Copyright 2022 The Kubernetes Authors.

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

func TestExportILBMetric(t *testing.T) {
	newMetrics := FakeControllerMetrics()
	pastPersistentErrorThresholdTime := time.Now().Add(-1*persistentErrorThresholdTime - time.Minute)
	notExceedingPersistentErrorThresholdTime := time.Now().Add(-1*persistentErrorThresholdTime + 5*time.Minute)

	newMetrics.SetL4ILBService("s1", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: true},
		Status:                  StatusSuccess,
	})
	newMetrics.SetL4ILBService("s2", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: true},
		Status:                  StatusSuccess,
	})
	newMetrics.SetL4ILBService("s3", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false},
		Status:                  StatusUserError,
	})
	newMetrics.SetL4ILBService("s4", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false},
		Status:                  StatusError,
		FirstSyncErrorTime:      &notExceedingPersistentErrorThresholdTime,
	})
	newMetrics.SetL4ILBService("s5", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false},
		Status:                  StatusError,
		FirstSyncErrorTime:      &pastPersistentErrorThresholdTime,
	})
	// check that updating later does not move FirstSyncErrorTime
	newMetrics.SetL4ILBService("s5", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false},
		Status:                  StatusError,
		FirstSyncErrorTime:      &notExceedingPersistentErrorThresholdTime,
	})
	newMetrics.SetL4ILBService("s6", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false},
		L4DualStackServiceLabels: L4DualStackServiceLabels{
			IPFamilies:     "IPv4",
			IPFamilyPolicy: "SingleStack",
		},
		Status:             StatusError,
		FirstSyncErrorTime: &notExceedingPersistentErrorThresholdTime,
	})

	newMetrics.exportL4ILBsMetrics()

	verifyL4ILBMetric(t, 2, StatusSuccess, "true")
	verifyL4ILBMetric(t, 1, StatusUserError, "false")
	verifyL4ILBMetric(t, 2, StatusError, "false")
	verifyL4ILBMetric(t, 1, StatusPersistentError, "false")
}

func verifyL4ILBMetric(t *testing.T, expectedCount int, status L4ServiceStatus, multinet string) {
	countFloat := testutil.ToFloat64(l4ILBCount.With(prometheus.Labels{l4LabelStatus: string(status), l4LabelMultinet: multinet}))
	actualCount := int(math.Round(countFloat))
	if expectedCount != actualCount {
		t.Errorf("expected value %d but got %d", expectedCount, actualCount)
	}
}

func TestExportNetLBMetric(t *testing.T) {
	newMetrics := FakeControllerMetrics()
	pastPersistentErrorThresholdTime := time.Now().Add(-1*persistentErrorThresholdTime - time.Minute)
	notExceedingPersistentErrorThresholdTime := time.Now().Add(-1*persistentErrorThresholdTime + 5*time.Minute)

	newMetrics.SetL4NetLBService("s1", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: true},
		Status:                  StatusSuccess,
	})
	newMetrics.SetL4NetLBService("s2", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: true},
		Status:                  StatusSuccess,
	})
	newMetrics.SetL4NetLBService("s3", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false},
		Status:                  StatusUserError,
	})
	newMetrics.SetL4NetLBService("s4", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false},
		Status:                  StatusError,
		FirstSyncErrorTime:      &notExceedingPersistentErrorThresholdTime,
	})
	newMetrics.SetL4NetLBService("s5", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false},
		Status:                  StatusError,
		FirstSyncErrorTime:      &pastPersistentErrorThresholdTime,
	})
	// check that updating later does not move FirstSyncErrorTime
	newMetrics.SetL4NetLBService("s5", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false},
		Status:                  StatusError,
		FirstSyncErrorTime:      &notExceedingPersistentErrorThresholdTime,
	})
	newMetrics.SetL4NetLBService("s6", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false},
		L4DualStackServiceLabels: L4DualStackServiceLabels{
			IPFamilies:     "IPv4",
			IPFamilyPolicy: "SingleStack",
		},
		Status:             StatusError,
		FirstSyncErrorTime: &notExceedingPersistentErrorThresholdTime,
	})

	newMetrics.exportL4NetLBsMetrics()

	verifyL4NetLBMetric(t, 2, StatusSuccess, "true")
	verifyL4NetLBMetric(t, 1, StatusUserError, "false")
	verifyL4NetLBMetric(t, 2, StatusError, "false")
	verifyL4NetLBMetric(t, 1, StatusPersistentError, "false")
}

func verifyL4NetLBMetric(t *testing.T, expectedCount int, status L4ServiceStatus, multinet string) {
	countFloat := testutil.ToFloat64(l4NetLBCount.With(prometheus.Labels{l4LabelStatus: string(status), l4LabelMultinet: multinet}))
	actualCount := int(math.Round(countFloat))
	if expectedCount != actualCount {
		t.Errorf("expected value %d but got %d", expectedCount, actualCount)
	}
}
