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

// Package servicestatus updates the load balancer status and the conditions
// of the services managed by the L4 controllers.
package servicestatus

import (
	"fmt"
	"sort"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/klog/v2"
)

// Update checks if the LoadBalancer status or the conditions of the service
// changed and patches the service status if needed.
func Update(client coreclient.CoreV1Interface, svc *v1.Service, newStatus *v1.LoadBalancerStatus, newConditions []metav1.Condition, conditionsToRemove []string, svcLogger klog.Logger) error {
	svcLogger.V(2).Info("Updating service status and conditions", "newStatus", fmt.Sprintf("%+v", newStatus), "newConditions", fmt.Sprintf("%+v", newConditions), "conditionsToRemove", conditionsToRemove)

	mergedConditions := mergeConditions(svc.Status.Conditions, newConditions, conditionsToRemove)
	svcLogger.V(2).Info("Merged conditions", "mergedConditions", fmt.Sprintf("%+v", mergedConditions))

	lbStatusEqual := helpers.LoadBalancerStatusEqual(&svc.Status.LoadBalancer, newStatus)
	lbConditionsEqual := conditionsEqual(svc.Status.Conditions, mergedConditions)

	if !lbStatusEqual || !lbConditionsEqual {
		svcLogger.V(2).Info("Patching LoadBalancer status and Conditions", "newStatus", fmt.Sprintf("%+v", newStatus), "newConditions", fmt.Sprintf("%+v", newConditions))
		return patch.PatchServiceStatus(client, svc, v1.ServiceStatus{
			LoadBalancer: *newStatus,
			Conditions:   mergedConditions,
		})
	}
	svcLogger.V(3).Info("Service status not changed, skipping patch for service")
	return nil
}

// mergeConditions merges the new set of l4lb resource conditions with the preexisting service conditions.
// Existing L4 resource condition values will be replaced with the values in the new map.
// Conditions listed in conditionsToRemove will be deleted.
// Listing a condition both in newConditions and conditionsToRemove will remove
// the condition first and then add a new one. In particular, if a condition of
// this type existed it LastTransitionTime will be replaced.
func mergeConditions(existing, newConditions []metav1.Condition, conditionsToRemove []string) []metav1.Condition {
	existingMap := make(map[string]metav1.Condition)
	for _, cond := range existing {
		existingMap[cond.Type] = cond
	}

	for _, condType := range conditionsToRemove {
		delete(existingMap, condType)
	}

	for _, newCond := range newConditions {
		oldCond, exists := existingMap[newCond.Type]
		if exists && oldCond.Status == newCond.Status {
			newCond.LastTransitionTime = oldCond.LastTransitionTime
		}
		existingMap[newCond.Type] = newCond
	}

	if len(existingMap) == 0 {
		return nil
	}

	mergedConditions := make([]metav1.Condition, 0, len(existingMap))
	for _, cond := range existingMap {
		mergedConditions = append(mergedConditions, cond)
	}
	sort.Slice(mergedConditions, func(i, j int) bool {
		return mergedConditions[i].Type < mergedConditions[j].Type
	})
	return mergedConditions
}

// conditionsEqual checks if load balancer conditions are equal.
func conditionsEqual(l, r []metav1.Condition) bool {
	if len(l) != len(r) {
		return false
	}
	lMap := make(map[string]metav1.Condition)
	for _, cond := range l {
		lMap[cond.Type] = cond
	}
	for _, condR := range r {
		condL, found := lMap[condR.Type]
		if !found {
			return false
		}
		if condL.Status != condR.Status || condL.Reason != condR.Reason || condL.Message != condR.Message {
			return false
		}
	}
	return true
}
