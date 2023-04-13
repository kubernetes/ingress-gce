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

package metrics

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"
)

func TestComputeLabelMetrics(t *testing.T) {
	collector := NewNegMetricsCollector(10*time.Second, klog.TODO())
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
