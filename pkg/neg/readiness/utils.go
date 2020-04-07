/*
Copyright 2019 The Kubernetes Authors.

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

package readiness

import (
	"context"
	"fmt"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/neg/types/shared"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

// NegReadinessConditionStatus return (cond, true) if neg condition exists, otherwise (_, false)
func NegReadinessConditionStatus(pod *v1.Pod) (negCondition v1.PodCondition, exists bool) {
	if pod == nil {
		return v1.PodCondition{}, false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == shared.NegReadinessGate {
			return condition, true
		}
	}
	return v1.PodCondition{}, false
}

// evalNegReadinessGate returns if the pod readiness gate includes the NEG readiness condition and the condition status is true
func evalNegReadinessGate(pod *v1.Pod) (negReady bool, readinessGateExists bool) {
	if pod == nil {
		return false, false
	}
	for _, gate := range pod.Spec.ReadinessGates {
		if gate.ConditionType == shared.NegReadinessGate {
			readinessGateExists = true
		}
	}
	if condition, ok := NegReadinessConditionStatus(pod); ok {
		if condition.Status == v1.ConditionTrue {
			negReady = true
		}
	}
	return negReady, readinessGateExists
}

func keyFunc(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

// getPodFromStore return (pod, exists, nil) if it is able to successfully retrieve it from podLister.
func getPodFromStore(podLister cache.Indexer, namespace, name string) (pod *v1.Pod, exists bool, err error) {
	if podLister == nil {
		return nil, false, fmt.Errorf("podLister is nil")
	}
	key := keyFunc(namespace, name)
	obj, exists, err := podLister.GetByKey(key)
	if err != nil {
		return nil, false, fmt.Errorf("failed to retrieve pod %q from store: %v", key, err)
	}

	if !exists {
		return nil, false, nil
	}

	pod, ok := obj.(*v1.Pod)
	if !ok {
		return nil, false, fmt.Errorf("Failed to convert obj type %T to *v1.Pod", obj)
	}
	return pod, true, nil
}

// SetNegReadinessConditionStatus sets the status of the NEG readiness condition
func SetNegReadinessConditionStatus(pod *v1.Pod, condition v1.PodCondition) {
	if pod == nil {
		return
	}
	for i, cond := range pod.Status.Conditions {
		if cond.Type == shared.NegReadinessGate {
			pod.Status.Conditions[i] = condition
			return
		}
	}
	pod.Status.Conditions = append(pod.Status.Conditions, condition)
}

// patchPodStatus patches pod status with given patchBytes
func patchPodStatus(c clientset.Interface, namespace, name string, patchBytes []byte) (*v1.Pod, []byte, error) {
	updatedPod, err := c.CoreV1().Pods(namespace).Patch(context.TODO(), name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to patch status %q for pod %q/%q: %v", patchBytes, namespace, name, err)
	}
	return updatedPod, patchBytes, nil
}

// preparePatchBytesforPodStatus generates patch bytes based on the old and new pod status
func preparePatchBytesforPodStatus(oldPodStatus, newPodStatus v1.PodStatus) ([]byte, error) {
	patchBytes, err := utils.StrategicMergePatchBytes(v1.Pod{Status: oldPodStatus}, v1.Pod{Status: newPodStatus}, v1.Pod{})
	return patchBytes, err
}

// needToPoll filter out the network endpoint that needs to be polled based on the following conditions:
// 1. neg syncer has readiness gate enabled
// 2. the pod exists
// 3. the pod has neg readiness gate
// 4. the pod's neg readiness condition is not True
func needToPoll(syncerKey negtypes.NegSyncerKey, endpointMap negtypes.EndpointPodMap, lookup NegLookup, podLister cache.Indexer) negtypes.EndpointPodMap {
	if !lookup.ReadinessGateEnabled(syncerKey) {
		return negtypes.EndpointPodMap{}
	}
	removeIrrelevantEndpoints(endpointMap, podLister)
	return endpointMap
}

// removeIrrelevantEndpoints will filter out the endpoints that does not need health status polling from the input endpoint map
func removeIrrelevantEndpoints(endpointMap negtypes.EndpointPodMap, podLister cache.Indexer) {
	for endpoint, namespacedName := range endpointMap {
		pod, exists, err := getPodFromStore(podLister, namespacedName.Namespace, namespacedName.Name)
		if err != nil {
			klog.Warningf("Failed to retrieve pod %q from store: %v", namespacedName.String(), err)
		}
		if err == nil && exists && needToProcess(pod) {
			continue
		}
		delete(endpointMap, endpoint)
	}
}

// needToProcess check if the pod needs to be processed by readiness reflector
// If pod has neg readiness gate and its condition is False, then return true.
func needToProcess(pod *v1.Pod) bool {
	if pod == nil {
		return false
	}
	negConditionReady, readinessGateExists := evalNegReadinessGate(pod)
	return readinessGateExists && !negConditionReady
}
