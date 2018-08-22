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

package utils

import (
	"encoding/json"
	"fmt"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/kubernetes/pkg/util/slice"
)

// FinalizerKeySuffix is a suffix for finalizers added by the controller.
// A full key could be something like "ingress.finalizer.cloud.google.com"
const FinalizerKeySuffix = "finalizer.cloud.google.com"

// IsDeletionCandidate is true if the passed in meta contains the specified finalizer.
func IsDeletionCandidate(m meta_v1.ObjectMeta, key string) bool {
	return m.DeletionTimestamp != nil && hasFinalizer(m, key)
}

// NeedToAddFinalizer is true if the passed in meta does not contain the specified finalizer.
func NeedToAddFinalizer(m meta_v1.ObjectMeta, key string) bool {
	return m.DeletionTimestamp == nil && !hasFinalizer(m, key)
}

func hasFinalizer(m meta_v1.ObjectMeta, key string) bool {
	return slice.ContainsString(m.Finalizers, key, nil)
}

// GetPatchBytes returns a patch which is the difference between the old and new objects.
// Note: refStruct is a empty struct of the type which the patch is being generated for.
func GetPatchBytes(old interface{}, cur interface{}, refStruct interface{}) ([]byte, error) {
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
