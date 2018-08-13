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

	"github.com/golang/glog"
)

// Description stores the description for a BackendService.
type Description struct {
	ServiceName string   `json:"kubernetes.io/service-name"`
	ServicePort string   `json:"kubernetes.io/service-port"`
	XFeatures   []string `json:"x-features,omitempty"`
}

// String returns the string representation of a Description.
func (desc Description) String() string {
	if desc.ServiceName == "" || desc.ServicePort == "" {
		return ""
	}

	descJson, err := json.Marshal(desc)
	if err != nil {
		glog.Errorf("Failed to generate description string: %v, falling back to empty string", err)
		return ""
	}
	return string(descJson)
}

// DescriptionFromString gets a Description from string,
func DescriptionFromString(descString string) *Description {
	if descString == "" {
		return &Description{}
	}
	var desc Description
	if err := json.Unmarshal([]byte(descString), &desc); err != nil {
		glog.Errorf("Failed to parse description: %s, falling back to empty list", descString)
		return &Description{}
	}
	return &desc
}
