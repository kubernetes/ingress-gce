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

package metricscollector

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
)

func TestBucketLabel(t *testing.T) {
	cases := []struct {
		value   int
		buckets []float64
		want    string
	}{
		{0, l4ServiceNodeBuckets, "0"},
		{0, l4ServicePodBuckets, "0"},
		{1, l4ServiceNodeBuckets, "1"},
		{2, l4ServiceNodeBuckets, "2"},
		{3, l4ServiceNodeBuckets, "4"},
		{250, l4ServiceNodeBuckets, "256"},
		{3000, l4ServiceNodeBuckets, "4096"},
		{4097, l4ServiceNodeBuckets, "8192"},
		{15000, l4ServiceNodeBuckets, "16384"},
		{65537, l4ServiceNodeBuckets, "+Inf"},
		{8, l4ServicePodBuckets, "8"},
		{27500, l4ServicePodBuckets, "32768"},
		{65537, l4ServicePodBuckets, "131072"},
		{200000, l4ServicePodBuckets, "262144"},
		{262145, l4ServicePodBuckets, "+Inf"},
	}
	for _, tc := range cases {
		if got := bucketLabel(tc.value, tc.buckets); got != tc.want {
			t.Errorf("bucketLabel(%d) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestExportL4ServicesSize(t *testing.T) {
	sm := FakeSyncerMetrics()
	ilbLocal := func(name string) negtypes.NegSyncerKey {
		return negtypes.NegSyncerKey{
			Namespace:        "ns",
			Name:             name,
			NegType:          negtypes.VmIpEndpointType,
			EpCalculatorMode: negtypes.L4LocalMode,
			L4LBType:         negtypes.L4InternalLB,
		}
	}

	// 2 nodes, 3 + 5 = 8 pods.
	sm.UpdateL4BackendPods(ilbLocal("a"), map[string]int{"node-a": 3, "node-b": 5})
	// 2 nodes, 1 + 1 = 2 pods.
	sm.UpdateL4BackendPods(ilbLocal("b"), map[string]int{"node-a": 1, "node-b": 1})
	// No ready pods on backend nodes: counted in the "0" bucket of both metrics.
	sm.UpdateL4BackendPods(ilbLocal("empty"), map[string]int{})
	// Same shape as "a", but NetLB with ETP=Cluster: separate label series.
	sm.UpdateL4BackendPods(negtypes.NegSyncerKey{
		Namespace:        "ns",
		Name:             "netlb",
		NegType:          negtypes.VmIpEndpointType,
		EpCalculatorMode: negtypes.L4ClusterMode,
		L4LBType:         negtypes.L4ExternalLB,
	}, map[string]int{"node-a": 3, "node-b": 5})

	sm.exportL4ServicesSize()

	// Node count: both ILB services have 2 nodes.
	if got := testutil.ToFloat64(sm.l4ServicesByNodes.WithLabelValues("internal", "local", "2")); got != 2 {
		t.Errorf("l4_services_by_nodes{internal,local,2} = %v, want 2", got)
	}
	// Pod count: "a" has 8 pods, "b" has 2 pods, so they land in different buckets.
	if got := testutil.ToFloat64(sm.l4ServicesByPods.WithLabelValues("internal", "local", "8")); got != 1 {
		t.Errorf("l4_services_by_pods{internal,local,8} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(sm.l4ServicesByPods.WithLabelValues("internal", "local", "2")); got != 1 {
		t.Errorf("l4_services_by_pods{internal,local,2} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(sm.l4ServicesByPods.WithLabelValues("external", "cluster", "8")); got != 1 {
		t.Errorf("l4_services_by_pods{external,cluster,8} = %v, want 1", got)
	}
	// "empty" is counted in the "0" bucket.
	if got := testutil.ToFloat64(sm.l4ServicesByNodes.WithLabelValues("internal", "local", "0")); got != 1 {
		t.Errorf("l4_services_by_nodes{internal,local,0} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(sm.l4ServicesByPods.WithLabelValues("internal", "local", "0")); got != 1 {
		t.Errorf("l4_services_by_pods{internal,local,0} = %v, want 1", got)
	}
	// 3 node-count series (internal/local 0 and 2, external/cluster 2) and
	// 4 pod-count series (internal/local 0, 2 and 8, external/cluster 8).
	if got := testutil.CollectAndCount(sm.l4ServicesByNodes.(prometheus.Collector)); got != 3 {
		t.Errorf("l4_services_by_nodes has %d series, want 3", got)
	}
	if got := testutil.CollectAndCount(sm.l4ServicesByPods.(prometheus.Collector)); got != 4 {
		t.Errorf("l4_services_by_pods has %d series, want 4", got)
	}

	// Deleting a syncer removes the service on the next export.
	sm.DeleteSyncer(ilbLocal("a"))
	sm.exportL4ServicesSize()
	if got := testutil.CollectAndCount(sm.l4ServicesByPods.(prometheus.Collector)); got != 3 {
		t.Errorf("after delete, l4_services_by_pods has %d series, want 3", got)
	}
}
