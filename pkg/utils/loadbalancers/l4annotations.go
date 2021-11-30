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
	"reflect"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/annotations"
)

var L4LBResourceAnnotationKeys = []string{
	annotations.BackendServiceKey,
	annotations.TCPForwardingRuleKey,
	annotations.UDPForwardingRuleKey,
	annotations.HealthcheckKey,
	annotations.FirewallRuleKey,
	annotations.FirewallRuleForHealthcheckKey}

// ComputeNewAnnotationsIfNeeded checks if new annotations should be added to service.
// If needed creates new service meta object.
// This function is used by External and Internal L4 LB controllers.
func ComputeNewAnnotationsIfNeeded(svc *v1.Service, newAnnotations map[string]string) *metav1.ObjectMeta {
	newObjectMeta := svc.ObjectMeta.DeepCopy()
	newObjectMeta.Annotations = mergeAnnotations(newObjectMeta.Annotations, newAnnotations)
	if reflect.DeepEqual(svc.Annotations, newObjectMeta.Annotations) {
		return nil
	}
	return newObjectMeta
}

// mergeAnnotations merges the new set of l4lb resource annotations with the pre-existing service annotations.
// Existing L4 resource annotation values will be replaced with the values in the new map.
func mergeAnnotations(existing, lbAnnotations map[string]string) map[string]string {
	if existing == nil {
		existing = make(map[string]string)
	} else {
		// Delete existing L4 annotations.
		for _, key := range L4LBResourceAnnotationKeys {
			delete(existing, key)
		}
	}
	// merge existing annotations with the newly added annotations
	for key, val := range lbAnnotations {
		existing[key] = val
	}
	return existing
}
