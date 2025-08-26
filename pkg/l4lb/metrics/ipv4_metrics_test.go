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
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
)

const (
	isMultinetwork                = "true"
	notMultinetwork               = "false"
	enabledStrongSessionAffinity  = "true"
	disabledStrongSessionAffinity = "false"
	isWeightedLBPodsPerNode       = "true"
	notWeightedLBPodsPerNode      = "false"
	enabledZonalAffinity          = "true"
	disabledZonalAffinity         = "false"
)

func TestExportILBMetric(t *testing.T) {
	newMetrics := NewFakeCollector()
	pastPersistentErrorThresholdTime := time.Now().Add(-1*persistentErrorThresholdTime - time.Minute)
	notExceedingPersistentErrorThresholdTime := time.Now().Add(-1*persistentErrorThresholdTime + 5*time.Minute)

	newMetrics.SetL4ILBService("svc-success-all-labels", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: true, WeightedLBPodsPerNode: true, ZonalAffinity: true},
		Status:                  StatusSuccess,
	})
	newMetrics.SetL4ILBService("svc-success-multinet-1", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: true, WeightedLBPodsPerNode: false, ZonalAffinity: false},
		Status:                  StatusSuccess,
	})
	newMetrics.SetL4ILBService("svc-success-multinet-2", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: true, WeightedLBPodsPerNode: false, ZonalAffinity: false},
		Status:                  StatusSuccess,
	})
	newMetrics.SetL4ILBService("svc-success-weightedlb", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false, WeightedLBPodsPerNode: true, ZonalAffinity: false},
		Status:                  StatusSuccess,
	})
	newMetrics.SetL4ILBService("svc-success-zonalaffinity", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false, WeightedLBPodsPerNode: false, ZonalAffinity: true},
		Status:                  StatusSuccess,
	})
	newMetrics.SetL4ILBService("svc-user-error", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false, WeightedLBPodsPerNode: false, ZonalAffinity: false},
		Status:                  StatusUserError,
	})
	newMetrics.SetL4ILBService("svc-error", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false, WeightedLBPodsPerNode: false, ZonalAffinity: false},
		Status:                  StatusError,
		FirstSyncErrorTime:      &notExceedingPersistentErrorThresholdTime,
	})
	newMetrics.SetL4ILBService("svc-threshold-check", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false, WeightedLBPodsPerNode: false, ZonalAffinity: false},
		Status:                  StatusError,
		FirstSyncErrorTime:      &pastPersistentErrorThresholdTime,
	})
	// check that updating later does not move FirstSyncErrorTime
	newMetrics.SetL4ILBService("svc-threshold-check", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false, WeightedLBPodsPerNode: false, ZonalAffinity: false},
		Status:                  StatusError,
		FirstSyncErrorTime:      &notExceedingPersistentErrorThresholdTime,
	})
	newMetrics.SetL4ILBService("svc-single-stack", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false, WeightedLBPodsPerNode: false, ZonalAffinity: false},
		L4DualStackServiceLabels: L4DualStackServiceLabels{
			IPFamilies:     "IPv4",
			IPFamilyPolicy: "SingleStack",
		},
		Status:             StatusError,
		FirstSyncErrorTime: &notExceedingPersistentErrorThresholdTime,
	})

	newMetrics.exportL4ILBsMetrics()

	verifyL4ILBMetric(t, 1, StatusSuccess, isMultinetwork, isWeightedLBPodsPerNode, enabledZonalAffinity)
	verifyL4ILBMetric(t, 2, StatusSuccess, isMultinetwork, notWeightedLBPodsPerNode, disabledZonalAffinity)
	verifyL4ILBMetric(t, 1, StatusSuccess, notMultinetwork, isWeightedLBPodsPerNode, disabledZonalAffinity)
	verifyL4ILBMetric(t, 1, StatusSuccess, notMultinetwork, notWeightedLBPodsPerNode, enabledZonalAffinity)
	verifyL4ILBMetric(t, 1, StatusUserError, notMultinetwork, notWeightedLBPodsPerNode, disabledZonalAffinity)
	verifyL4ILBMetric(t, 2, StatusError, notMultinetwork, notWeightedLBPodsPerNode, disabledZonalAffinity)
	verifyL4ILBMetric(t, 1, StatusPersistentError, notMultinetwork, notWeightedLBPodsPerNode, disabledZonalAffinity)
}

func verifyL4ILBMetric(t *testing.T, expectedCount int, status L4ServiceStatus, multinet string, weightedLBPodsPerNode string, zonalAffinity string) {
	countFloat := testutil.ToFloat64(l4ILBCount.With(prometheus.Labels{
		l4LabelStatus:                string(status),
		l4LabelMultinet:              multinet,
		l4LabelWeightedLBPodsPerNode: weightedLBPodsPerNode,
		l4LabelZonalAffinity:         zonalAffinity,
		l4LabelProtocol:              string(L4ProtocolTypeUnknown),
	}))
	actualCount := int(math.Round(countFloat))
	if expectedCount != actualCount {
		t.Errorf("expected value %d but got %d for status: %q, multinet: %q, weightedLB: %q, zonalAffinity: %q", expectedCount, actualCount, status, multinet, weightedLBPodsPerNode, zonalAffinity)
	}
}

func TestExportNetLBMetric(t *testing.T) {
	newMetrics := NewFakeCollector()
	pastPersistentErrorThresholdTime := time.Now().Add(-1*persistentErrorThresholdTime - time.Minute)
	notExceedingPersistentErrorThresholdTime := time.Now().Add(-1*persistentErrorThresholdTime + 5*time.Minute)

	newMetrics.SetL4NetLBService("svc-success-multinet-1", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: true, StrongSessionAffinity: false, WeightedLBPodsPerNode: false, BackendType: L4BackendTypeNEG},
		Status:                  StatusSuccess,
	})
	newMetrics.SetL4NetLBService("svc-success-all-labels", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: true, StrongSessionAffinity: true, WeightedLBPodsPerNode: true, BackendType: L4BackendTypeNEG},
		Status:                  StatusSuccess,
	})
	newMetrics.SetL4NetLBService("svc-success-multinet-2", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: true, StrongSessionAffinity: false, WeightedLBPodsPerNode: false, BackendType: L4BackendTypeNEG},
		Status:                  StatusSuccess,
	})
	newMetrics.SetL4NetLBService("svc-success-ssa", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false, StrongSessionAffinity: true, WeightedLBPodsPerNode: false, BackendType: L4BackendTypeNEG},
		Status:                  StatusSuccess,
	})
	newMetrics.SetL4NetLBService("svc-success-weightedlb", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false, StrongSessionAffinity: false, WeightedLBPodsPerNode: true, BackendType: L4BackendTypeInstanceGroup},
		Status:                  StatusSuccess,
	})
	newMetrics.SetL4NetLBService("svc-user-error-ssa", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false, StrongSessionAffinity: true, WeightedLBPodsPerNode: false, BackendType: L4BackendTypeInstanceGroup},
		Status:                  StatusUserError,
	})
	newMetrics.SetL4NetLBService("svc-error", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false, StrongSessionAffinity: false, WeightedLBPodsPerNode: false, BackendType: L4BackendTypeInstanceGroup},
		Status:                  StatusError,
		FirstSyncErrorTime:      &notExceedingPersistentErrorThresholdTime,
	})
	newMetrics.SetL4NetLBService("svc-error", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false, StrongSessionAffinity: false, WeightedLBPodsPerNode: false, BackendType: L4BackendTypeInstanceGroup},
		Status:                  StatusError,
		FirstSyncErrorTime:      &notExceedingPersistentErrorThresholdTime,
	})
	newMetrics.SetL4NetLBService("svc-threshold-check", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false, StrongSessionAffinity: false, WeightedLBPodsPerNode: false, BackendType: L4BackendTypeInstanceGroup},
		Status:                  StatusError,
		FirstSyncErrorTime:      &pastPersistentErrorThresholdTime,
	})
	// check that updating later does not move FirstSyncErrorTime
	newMetrics.SetL4NetLBService("svc-threshold-check", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false, StrongSessionAffinity: false, WeightedLBPodsPerNode: false, BackendType: L4BackendTypeInstanceGroup},
		Status:                  StatusError,
		FirstSyncErrorTime:      &notExceedingPersistentErrorThresholdTime,
	})
	newMetrics.SetL4NetLBService("svc-single-stack", L4ServiceState{
		L4FeaturesServiceLabels: L4FeaturesServiceLabels{Multinetwork: false, StrongSessionAffinity: false, WeightedLBPodsPerNode: false, BackendType: L4BackendTypeInstanceGroup},
		L4DualStackServiceLabels: L4DualStackServiceLabels{
			IPFamilies:     "IPv4",
			IPFamilyPolicy: "SingleStack",
		},
		Status:             StatusError,
		FirstSyncErrorTime: &notExceedingPersistentErrorThresholdTime,
	})

	newMetrics.exportL4NetLBsMetrics()

	verifyL4NetLBMetric(t, 2, StatusSuccess, isMultinetwork, disabledStrongSessionAffinity, notWeightedLBPodsPerNode, L4BackendTypeNEG)
	verifyL4NetLBMetric(t, 1, StatusSuccess, notMultinetwork, enabledStrongSessionAffinity, notWeightedLBPodsPerNode, L4BackendTypeNEG)
	verifyL4NetLBMetric(t, 1, StatusSuccess, notMultinetwork, disabledStrongSessionAffinity, isWeightedLBPodsPerNode, L4BackendTypeInstanceGroup)
	verifyL4NetLBMetric(t, 1, StatusUserError, notMultinetwork, enabledStrongSessionAffinity, notWeightedLBPodsPerNode, L4BackendTypeInstanceGroup)
	verifyL4NetLBMetric(t, 2, StatusError, notMultinetwork, disabledStrongSessionAffinity, notWeightedLBPodsPerNode, L4BackendTypeInstanceGroup)
	verifyL4NetLBMetric(t, 1, StatusPersistentError, notMultinetwork, disabledStrongSessionAffinity, notWeightedLBPodsPerNode, L4BackendTypeInstanceGroup)
}

func verifyL4NetLBMetric(t *testing.T, expectedCount int, status L4ServiceStatus, multinet string, strongSessionAffinity string, weightedLBPodsPerNode string, backendType L4BackendType) {
	countFloat := testutil.ToFloat64(l4NetLBCount.With(prometheus.Labels{
		l4LabelStatus:                string(status),
		l4LabelMultinet:              multinet,
		l4LabelStrongSessionAffinity: strongSessionAffinity,
		l4LabelWeightedLBPodsPerNode: weightedLBPodsPerNode,
		l4LabelBackendType:           string(backendType),
		l4LabelProtocol:              string(L4ProtocolTypeUnknown),
	}))
	actualCount := int(math.Round(countFloat))
	if expectedCount != actualCount {
		t.Errorf("expected value %d but got %d for status: %q, multinet: %q, ssa: %q, weightedLB: %q, backendType: %q",
			expectedCount, actualCount, status, multinet, strongSessionAffinity, weightedLBPodsPerNode, backendType)
	}
}

func TestMetricsWithProtocol(t *testing.T) {
	// Arrange
	c := NewFakeCollector()

	svcs := []*corev1.Service{
		{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
			{Port: 80}, // TCP is the default port
		}}},
		{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
			{Protocol: corev1.ProtocolTCP, Port: 80}, {Protocol: corev1.ProtocolTCP, Port: 443},
		}}},
		{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
			{Protocol: corev1.ProtocolUDP, Port: 53}, {Protocol: corev1.ProtocolTCP, Port: 53},
		}}},
		{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
			{Protocol: corev1.ProtocolUDP, Port: 12345}, {Protocol: corev1.ProtocolTCP, Port: 80},
		}}},
		{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
			{Protocol: corev1.ProtocolUDP, Port: 80},
		}}},
	}

	wants := map[L4ProtocolType]int{
		L4ProtocolTypeTCP:     2,
		L4ProtocolTypeUDP:     1,
		L4ProtocolTypeMixed:   2,
		L4ProtocolTypeUnknown: 0, // We never expect it to occur.
	}

	for i, svc := range svcs {
		// Act
		state := InitServiceMetricsState(svc, /* other fields were tested above */
			nil, false, false, false, false, L4BackendTypeInstanceGroup)
		c.SetL4ILBService(fmt.Sprintf("svc-ilb-%d", i), state)
		c.SetL4NetLBService(fmt.Sprintf("svc-netlb-%d", i), state)
	}

	c.exportL4ILBsMetrics()
	c.exportL4NetLBsMetrics()

	for protocol, want := range wants {
		t.Run(fmt.Sprintf("ILB %q", protocol), func(t *testing.T) {
			countFloat := testutil.ToFloat64(l4ILBCount.With(prometheus.Labels{
				l4LabelProtocol: string(protocol),
				/* we test those above */ l4LabelStatus: string(StatusError), l4LabelMultinet: "false", l4LabelWeightedLBPodsPerNode: "false", l4LabelZonalAffinity: "false",
			}))
			actual := int(math.Round(countFloat))
			if want != actual {
				t.Errorf("expected value %d but got %d for protocol: %q", want, actual, protocol)
			}
		})
		t.Run(fmt.Sprintf("NetLB %q", protocol), func(t *testing.T) {
			countFloat := testutil.ToFloat64(l4NetLBCount.With(prometheus.Labels{
				l4LabelProtocol: string(protocol),
				/* we test those above */ l4LabelStatus: string(StatusError), l4LabelMultinet: "false", l4LabelStrongSessionAffinity: "false", l4LabelWeightedLBPodsPerNode: "false", l4LabelBackendType: string(L4BackendTypeInstanceGroup),
			}))
			actual := int(math.Round(countFloat))
			if want != actual {
				t.Errorf("expected value %d but got %d for protocol: %q", want, actual, protocol)
			}
		})
	}
}
