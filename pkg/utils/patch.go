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

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
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

// JSONMergePatchBytes returns a patch between the old and new object using a JSON merge patch.
func JSONMergePatchBytes(old, cur interface{}) ([]byte, error) {
	oldBytes, err := json.Marshal(old)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal old object: %v", err)
	}

	newBytes, err := json.Marshal(cur)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal new object: %v", err)
	}

	lastAppliedBytes, err := lastAppliedConfigurationBytes(old)
	if err != nil {
		return nil, fmt.Errorf("failed to get last applied config for old object: %v", err)
	}

	patchBytes, err := jsonmergepatch.CreateThreeWayJSONMergePatch(lastAppliedBytes, newBytes, oldBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create patch: %v", err)
	}

	return patchBytes, nil
}

// lastAppliedConfigurationBytes returns the bytes of the original configuration of an object
// from the annotation, or nil if no annotation is found. This is needed for the 3-way JSON merge patch.
func lastAppliedConfigurationBytes(obj interface{}) ([]byte, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}

	annotations := accessor.GetAnnotations()
	if annotations == nil {
		return nil, nil
	}

	original, ok := annotations[api_v1.LastAppliedConfigAnnotation]
	if !ok {
		return nil, nil
	}

	return []byte(original), nil
}
