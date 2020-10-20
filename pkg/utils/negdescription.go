/*
Copyright 2020 The Kubernetes Authors.

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

	"k8s.io/klog"
)

// Description stores the description for a BackendService.
type NegDescription struct {
	ClusterUID  string `json:"cluster-uid,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	ServiceName string `json:"service-name,omitempty"`
	Port        string `json:"port,omitempty"`
}

// String returns the string representation of a Description.
func (desc NegDescription) String() string {
	descJson, err := json.Marshal(desc)
	if err != nil {
		klog.Errorf("Failed to generate neg description string: %v, falling back to empty string", err)
		return ""
	}
	return string(descJson)
}

// DescriptionFromString gets a Description from string,
func NegDescriptionFromString(descString string) (*NegDescription, error) {
	var desc NegDescription
	if err := json.Unmarshal([]byte(descString), &desc); err != nil {
		klog.Errorf("Failed to parse neg description: %s, falling back to empty list", descString)
		return &NegDescription{}, err
	}
	return &desc, nil
}

// VerifyDescription returns whether the provided descString fields match Neg Description expectDesc.
// If an empty string or malformed description is provided, VerifyDescription will return true.
// When returning false, a detailed error will also be returned
func VerifyDescription(expectDesc NegDescription, descString, negName, zone string) (bool, error) {
	// Return true if description string is empty
	if descString != "" {
		desc, err := NegDescriptionFromString(descString)
		if err != nil {
			klog.Warningf("Error unmarshalling Neg Description %s err:%s", negName, err)
		} else {
			if desc.ClusterUID != expectDesc.ClusterUID || desc.Namespace != expectDesc.Namespace || desc.ServiceName != expectDesc.ServiceName || desc.Port != expectDesc.Port {
				return false, fmt.Errorf("expected description of NEG object %q/%q to be %+v, but got %+v", zone, negName, expectDesc, desc)
			}
		}
	}
	return true, nil
}
