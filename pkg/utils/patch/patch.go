/*
Copyright 2018 The Kubernetes Authors.
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

package patch

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	svchelpers "k8s.io/cloud-provider/service/helpers"
)

// StrategicMergePatchBytes returns a patch between the old and new object using a strategic merge patch.
// Note: refStruct is a empty struct of the type which the patch is being generated for.
func StrategicMergePatchBytes(old, cur, refStruct interface{}) ([]byte, error) {
	oldBytes, err := json.Marshal(old)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal old object: %v", err)
	}

	newBytes, err := json.Marshal(cur)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal new object: %v", err)
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldBytes, newBytes, refStruct)
	if err != nil {
		return nil, fmt.Errorf("failed to create patch: %v", err)
	}

	return patchBytes, nil
}

// MergePatchBytes returns a patch between the old and new object using a strategic merge patch.
// Note: refStruct is a empty struct of the type which the patch is being generated for.
func MergePatchBytes(old, cur interface{}) ([]byte, error) {
	oldBytes, err := json.Marshal(old)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal old object: %v", err)
	}

	newBytes, err := json.Marshal(cur)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal new object: %v", err)
	}

	patchBytes, err := jsonmergepatch.CreateThreeWayJSONMergePatch(oldBytes, newBytes, oldBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create patch: %v", err)
	}

	return patchBytes, nil
}

// PatchServiceObjectMetadata patches the given service's metadata based on new
// service metadata.
func PatchServiceObjectMetadata(client coreclient.CoreV1Interface, svc *corev1.Service, newObjectMetadata *metav1.ObjectMeta) error {
	return PatchServiceLoadBalancerInformation(client, svc, nil, newObjectMetadata)
}

// PatchServiceLoadBalancerInformation patches the given service's LoadBalancerStatus and ObjectMetadata
// based on new service's load-balancer status and metadata.
func PatchServiceLoadBalancerInformation(client coreclient.CoreV1Interface, svc *corev1.Service, newStatus *corev1.LoadBalancerStatus, newObjectMetadata *metav1.ObjectMeta) error {
	if client == nil || svc == nil {
		return fmt.Errorf("it was not possible to upload service information, nil client or service provided")
	}

	newSvc := svc.DeepCopy()
	if newStatus != nil {
		newSvc.Status.LoadBalancer = *newStatus
	}

	if newObjectMetadata != nil {
		newSvc.ObjectMeta = *newObjectMetadata
	}

	_, err := svchelpers.PatchService(client, svc, newSvc)
	return err
}
