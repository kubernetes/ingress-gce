/*
Copyright 2021 The Kubernetes Authors.

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
package loadbalancers

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/metrics"
)

const (
	SyncTypeCreate = "create"
	SyncTypeUpdate = "update"
	SyncTypeDelete = "delete"
)

// L4LBSyncResult contains information about the outcome of an L4 LB sync. It stores the list of resource name annotations,
// sync error, the GCE resource that hit the error along with the error type and more fields.
type L4LBSyncResult struct {
	Annotations        map[string]string
	Error              error
	GCEResourceInError string
	Status             *corev1.LoadBalancerStatus
	//TODO(kl52752) change metrics to support ILB and XLB
	MetricsState metrics.L4ILBServiceState
	SyncType     string
	StartTime    time.Time
}

var L4LBResourceAnnotationKeys = []string{
	annotations.BackendServiceKey,
	annotations.TCPForwardingRuleKey,
	annotations.UDPForwardingRuleKey,
	annotations.HealthcheckKey,
	annotations.FirewallRuleKey,
	annotations.FirewallRuleForHealthcheckKey}
