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
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	svchelpers "k8s.io/cloud-provider/service/helpers"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	providerconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned"
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
func PatchServiceObjectMetadata(client coreclient.CoreV1Interface, svc *corev1.Service, newObjectMetadata metav1.ObjectMeta) error {
	newSvc := svc.DeepCopy()
	newSvc.ObjectMeta = newObjectMetadata
	_, err := svchelpers.PatchService(client, svc, newSvc)
	return err
}

// PatchServiceLoadBalancerStatus patches the given service's LoadBalancerStatus
// based on new service's load-balancer status.
func PatchServiceLoadBalancerStatus(client coreclient.CoreV1Interface, svc *corev1.Service, newStatus corev1.LoadBalancerStatus) error {
	newSvc := svc.DeepCopy()
	newSvc.Status.LoadBalancer = newStatus
	_, err := svchelpers.PatchService(client, svc, newSvc)
	return err
}

// PatchProviderConfigObjectMetadata patches the given ProviderConfig's metadata based on new metadata.
func PatchProviderConfigObjectMetadata(client providerconfigclient.Interface, pc *providerconfig.ProviderConfig, newObjectMetadata metav1.ObjectMeta) error {
	// Reset Spec to ensure only ObjectMeta is patched.
	newPC := pc.DeepCopy()

	newPC.ObjectMeta = newObjectMetadata

	patchBytes, err := getProviderConfigPatchBytes(pc, newPC)
	if err != nil {
		return err
	}

	_, err = client.FlagsV1().ProviderConfigs(newPC.Namespace).Patch(context.Background(), newPC.Name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	return err
}

// getProviderConfigPatchBytes generates the patch bytes for updating a ProviderConfig.
func getProviderConfigPatchBytes(oldCS, newCS *providerconfig.ProviderConfig) ([]byte, error) {
	oldData, err := json.Marshal(oldCS)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal old object: %v", err)
	}

	newData, err := json.Marshal(newCS)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal new object: %v", err)
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, providerconfig.ProviderConfig{})
	if err != nil {
		return nil, fmt.Errorf("failed to create patch: %v", err)
	}

	return patchBytes, nil
}
