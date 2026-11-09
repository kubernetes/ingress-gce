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
	"fmt"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/gke-enterprise-mt/pkg/mtmetrics"
	"github.com/prometheus/client_golang/prometheus"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
)

// l4ServiceNodeBuckets: 1, 2, 4 ... 65536. GKE clusters can have up to 65,000
// nodes.
var l4ServiceNodeBuckets = prometheus.ExponentialBuckets(1, 2, 17)

// l4ServicePodBuckets: 1, 2, 4 ... 262144. A service cannot have more ready
// pods than its cluster, and GKE allows at most 200,000 pods per cluster.
var l4ServicePodBuckets = prometheus.ExponentialBuckets(1, 2, 19)

// registerL4PodChurnMetrics creates the L4 pod churn metrics on sm using the
// given factory.
func registerL4PodChurnMetrics(sm *SyncerMetrics, factory mtmetrics.MetricFactory) error {
	var err error

	if sm.l4PodChurnEvents, err = factory.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: negControllerSubsystem,
		Name:      "l4_pod_churn_events",
		Help:      "Histogram of per-node ready pod count changes per sync for L4 services, bucketed by the number of nodes with at least one ready pod (for ETP=Local, only nodes in the service's GCE_VM_IP NEG)",
		Buckets:   l4ServiceNodeBuckets,
	}, []string{"load_balancer_type", "external_traffic_policy"}); err != nil {
		return fmt.Errorf("failed to create l4_pod_churn_events: %w", err)
	}

	if sm.l4PodChurnZeroTransitionEvents, err = factory.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: negControllerSubsystem,
		Name:      "l4_pod_churn_zero_transition_events",
		Help:      "Histogram of per-node ready pod count transitions to or from zero per sync for L4 services, bucketed by the number of nodes with at least one ready pod (for ETP=Local, only nodes in the service's GCE_VM_IP NEG)",
		Buckets:   l4ServiceNodeBuckets,
	}, []string{"load_balancer_type", "external_traffic_policy"}); err != nil {
		return fmt.Errorf("failed to create l4_pod_churn_zero_transition_events: %w", err)
	}

	if sm.l4ServicesByNodes, err = factory.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: negControllerSubsystem,
		Name:      "l4_services_by_nodes",
		Help:      "Number of L4 services, grouped by load balancer type, externalTrafficPolicy, and the number of nodes with at least one ready pod (for ETP=Local, only nodes in the service's GCE_VM_IP NEG)",
	}, []string{"load_balancer_type", "external_traffic_policy", "node_count_bucket"}); err != nil {
		return fmt.Errorf("failed to create l4_services_by_nodes: %w", err)
	}

	if sm.l4ServicesByPods, err = factory.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: negControllerSubsystem,
		Name:      "l4_services_by_pods",
		Help:      "Number of L4 services, grouped by load balancer type, externalTrafficPolicy, and the number of ready pods (for ETP=Local, only pods on nodes in the service's GCE_VM_IP NEG)",
	}, []string{"load_balancer_type", "external_traffic_policy", "pod_count_bucket"}); err != nil {
		return fmt.Errorf("failed to create l4_services_by_pods: %w", err)
	}

	return nil
}

type l4ChurnLabels struct{ controller, etp string }

func l4ChurnLabelsFor(key negtypes.NegSyncerKey) l4ChurnLabels {
	etp := "cluster"
	if key.EpCalculatorMode == negtypes.L4LocalMode {
		etp = "local"
	}

	return l4ChurnLabels{controller: strings.ToLower(string(key.L4LBType)), etp: etp}
}

// podChurnDiff compares the ready pod count per node between two syncs.
// A node missing from a map has 0 ready pods. changed is the number of nodes
// whose count differs; zeroTransitions is the subset
// where the count went from 0 to non-zero or from non-zero to 0.
func podChurnDiff(prev, curr map[string]int) (changed, zeroTransitions int) {
	for node, c := range curr {
		p := prev[node]
		if c == p {
			continue
		}
		changed++
		if p == 0 || c == 0 {
			zeroTransitions++
		}
	}
	for node, p := range prev {
		if _, ok := curr[node]; !ok && p > 0 {
			changed++
			zeroTransitions++
		}
	}

	return changed, zeroTransitions
}

// UpdateL4BackendPods records the ready pod count per node for the L4 service
// behind the given syncer and emits churn events by comparing it with the
// previous call for the same syncer. The map only contains nodes with at least
// one ready pod (for ETP=Local, only nodes in the service's NEG).
//
// The first call for a syncer only records a baseline, so controller restarts
// and newly created services are not reported as churn. Events are observed
// with the number of nodes with at least one ready pod, using the larger of
// the previous and current counts so that scaling to zero is attributed to the
// size the service had.
func (sm *SyncerMetrics) UpdateL4BackendPods(key negtypes.NegSyncerKey, curr map[string]int) {
	sm.mu.Lock()
	prev, seen := sm.l4BackendPods[key]
	sm.l4BackendPods[key] = curr
	sm.mu.Unlock()

	if !seen {
		return
	}

	changed, zero := podChurnDiff(prev, curr)
	if changed == 0 {
		return
	}

	numNodes := max(len(prev), len(curr))

	l := l4ChurnLabelsFor(key)
	events := sm.l4PodChurnEvents.WithLabelValues(l.controller, l.etp)
	zeroEvents := sm.l4PodChurnZeroTransitionEvents.WithLabelValues(l.controller, l.etp)
	for range changed {
		events.Observe(float64(numNodes))
	}
	for range zero {
		zeroEvents.Observe(float64(numNodes))
	}
}

// bucketLabel returns the upper bound of the first bucket that contains value,
// formatted as a label value, "0" for 0, or "+Inf" if value exceeds all buckets.
func bucketLabel(value int, buckets []float64) string {
	if value <= 0 {
		return "0"
	}
	for _, upper := range buckets {
		if float64(value) <= upper {
			return strconv.FormatFloat(upper, 'f', -1, 64)
		}
	}
	return "+Inf"
}

// exportL4ServicesSize publishes l4_services_by_nodes and
// l4_services_by_pods from the state recorded by UpdateL4BackendPods.
func (sm *SyncerMetrics) exportL4ServicesSize() {
	type key struct {
		l4ChurnLabels
		le string
	}
	nodeCounts := make(map[key]int)
	podCounts := make(map[key]int)

	sm.mu.Lock()
	for syncerKey, pods := range sm.l4BackendPods {
		labels := l4ChurnLabelsFor(syncerKey)
		nodeCounts[key{labels, bucketLabel(len(pods), l4ServiceNodeBuckets)}]++

		totalPods := 0
		for _, c := range pods {
			totalPods += c
		}
		podCounts[key{labels, bucketLabel(totalPods, l4ServicePodBuckets)}]++
	}
	sm.mu.Unlock()

	sm.l4ServicesByNodes.Reset()
	for k, c := range nodeCounts {
		sm.l4ServicesByNodes.WithLabelValues(k.controller, k.etp, k.le).Set(float64(c))
	}
	sm.l4ServicesByPods.Reset()
	for k, c := range podCounts {
		sm.l4ServicesByPods.WithLabelValues(k.controller, k.etp, k.le).Set(float64(c))
	}
}
