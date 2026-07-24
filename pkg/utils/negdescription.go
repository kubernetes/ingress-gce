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
	"errors"
	"fmt"

	"k8s.io/klog/v2"
)

var ErrNEGUsedByAnotherSyncer = errors.New("NEG is used by another syncer in the same cluster and namespace")

// NEGDescription provides an interface for serializing and verifying NEG descriptions.
type NEGDescription interface {
	String() string
	MatchesString(descString, negName, zone string) (bool, error)
}

// Description stores the description for a BackendService.
type StandardNEGDescription struct {
	ClusterUID  string `json:"cluster-uid,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	ServiceName string `json:"service-name,omitempty"`
	Port        string `json:"port,omitempty"`
}

// String returns the string representation of a Description.
func (desc StandardNEGDescription) String() string {
	descJson, err := json.Marshal(desc)
	if err != nil {
		klog.Errorf("Failed to generate neg description string: %v, falling back to empty string", err)
		return ""
	}
	return string(descJson)
}

// MatchesString returns whether the provided descString fields match description's fields.
// If an empty string or malformed description is provided, MatchesString will return true.
// When returning false, a detailed error will also be returned
func (expectDesc StandardNEGDescription) MatchesString(descString, negName, zone string) (bool, error) {
	// Return true if description string is empty
	if descString != "" {
		desc, err := NEGDescriptionFromString[StandardNEGDescription](descString)
		if err != nil {
			klog.Warningf("Error unmarshalling Neg Description %s err:%s", negName, err)
		} else {
			// Wrap the error to determine if the NEG desc conflict occurs
			// within the same cluster and namespace.
			// When there is mismatch in NEG description, and the conflict
			// occurs within the same cluster and namespace, NEG status should
			// not be updated.
			// Otherwise, NEG status should have initialized=False condition.
			if desc.ClusterUID != expectDesc.ClusterUID {
				return false, fmt.Errorf("expected description of NEG object %q/%q to be %+v, but got %+v", zone, negName, expectDesc, desc)
			} else if desc.Namespace != expectDesc.Namespace {
				return false, fmt.Errorf("expected description of NEG object %q/%q to be %+v, but got %+v", zone, negName, expectDesc, desc)
			}

			if desc.ServiceName != expectDesc.ServiceName || desc.Port != expectDesc.Port {
				return false, fmt.Errorf("%w: expected description of NEG object %q/%q to be %+v, but got %+v", ErrNEGUsedByAnotherSyncer, zone, negName, expectDesc, desc)
			}
		}
	}
	return true, nil
}

// NEGDescriptionFromString parses string into NEG description structs
func NEGDescriptionFromString[T NEGDescription](descString string) (*T, error) {
	var desc T
	if err := json.Unmarshal([]byte(descString), &desc); err != nil {
		klog.Errorf("Failed to parse neg description: %s, falling back to empty %T", descString, desc)
		return &desc, err
	}
	return &desc, nil
}
