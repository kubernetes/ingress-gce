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
	"encoding/json"
	"fmt"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/neg/types/shared"
)

const (
	maxRetries = 15
)

// PatchPodStatus patches pod status with given patchBytes
func PatchPodStatus(c clientset.Interface, namespace, name string, patchBytes []byte) (*v1.Pod, []byte, error) {
	updatedPod, err := c.CoreV1().Pods(namespace).Patch(name, types.StrategicMergePatchType, patchBytes, "status")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to patch status %q for pod %q/%q: %v", patchBytes, namespace, name, err)
	}
	return updatedPod, patchBytes, nil
}

// GetNEGReadinessStatus retrieves the status of the NEG readiness condition if it exists.
func GetNegReadinessConditionStatus(pod *v1.Pod) (v1.PodCondition, bool) {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == shared.NegReadinessGate {
			return condition, true
		}
	}
	return v1.PodCondition{}, false
}

// evalNegReadinessGate returns true if the pod readiness gate includes the NEG readiness condition and the condition status is true
func evalNegReadinessGate(pod *v1.Pod) (negReady bool, readinessGateExists bool) {
	for _, gate := range pod.Spec.ReadinessGates {
		if gate.ConditionType == shared.NegReadinessGate {
			readinessGateExists = true
		}
	}
	if condition, ok := GetNegReadinessConditionStatus(pod); ok {
		if condition.Status == v1.ConditionTrue {
			negReady = true
		}
	}
	return
}

// SetNegReadinessConditionStatus sets the status of the NEG readiness condition
func SetNegReadinessConditionStatus(pod *v1.Pod, condition v1.PodCondition) {
	found := false
	index := 0
	for i, condition := range pod.Status.Conditions {
		if condition.Type == shared.NegReadinessGate {
			found = true
			index = i
			break
		}
	}
	if found {
		pod.Status.Conditions[index] = condition
	} else {
		pod.Status.Conditions = append(pod.Status.Conditions, condition)
	}
}

func preparePatchBytesforPodStatus(namespace, name string, oldPodStatus, newPodStatus v1.PodStatus) ([]byte, error) {
	oldData, err := json.Marshal(v1.Pod{
		Status: oldPodStatus,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to Marshal oldData for pod %q/%q: %v", namespace, name, err)
	}

	newData, err := json.Marshal(v1.Pod{
		Status: newPodStatus,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to Marshal newData for pod %q/%q: %v", namespace, name, err)
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, v1.Pod{})
	if err != nil {
		return nil, fmt.Errorf("failed to CreateTwoWayMergePatch for pod %q/%q: %v", namespace, name, err)
	}
	return patchBytes, nil
}

func keyFunc(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

func getPodFromStore(podLister cache.Indexer, namespace, name string) (*v1.Pod, bool, error) {
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

func getServiceFromStore(svcLister cache.Indexer, namespace, name string) (*v1.Service, bool, error) {
	if svcLister == nil {
		return nil, false, fmt.Errorf("svcLister is nil")
	}
	key := keyFunc(namespace, name)
	obj, exists, err := svcLister.GetByKey(key)
	if err != nil {
		return nil, false, fmt.Errorf("failed to retrieve service %q from store: %v", key, err)
	}

	if !exists {
		return nil, false, nil
	}

	svc, ok := obj.(*v1.Service)
	if !ok {
		return nil, false, fmt.Errorf("Failed to convert obj type %T to *v1.Service", obj)
	}
	return svc, true, nil
}
