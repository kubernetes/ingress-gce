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
	"net/url"
	"strings"

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

// CustomL4PassthroughNEGDescription is the description of a GCE_VM_IP NEG with a user
// provided name (L4 Standalone passthrough NEG services). It serializes the same way as
// the wrapped StandardNEGDescription, but its MatchesString is stricter, since the NEG
// could have been created outside of the cluster and the description is the only proof
// that the NEG is owned by the service.
type CustomL4PassthroughNEGDescription struct {
	Desc StandardNEGDescription
}

// String returns the string representation of the description.
func (desc CustomL4PassthroughNEGDescription) String() string {
	return desc.Desc.String()
}

// MatchesString returns whether the provided descString proves the NEG is owned by the service.
// Unlike StandardNEGDescription.MatchesString, an empty or malformed description is a mismatch.
// A missing port and port "0" are considered equal, since the port is meaningless for
// GCE_VM_IP NEGs and users pre-creating NEGs may omit it from the description.
// When returning false, a detailed error will also be returned.
func (expectDesc CustomL4PassthroughNEGDescription) MatchesString(descString, negName, zone string) (bool, error) {
	if descString == "" {
		return false, fmt.Errorf("NEG object %q/%q has an empty description, expected %+v", zone, negName, expectDesc.Desc)
	}
	// Unmarshal directly instead of NEGDescriptionFromString, which logs an error. Malformed
	// descriptions are expected here for NEGs created outside of the cluster.
	var desc StandardNEGDescription
	if err := json.Unmarshal([]byte(descString), &desc); err != nil {
		return false, fmt.Errorf("NEG object %q/%q has a malformed description %q, expected %+v: %w", zone, negName, descString, expectDesc.Desc, err)
	}
	if desc.ClusterUID != expectDesc.Desc.ClusterUID || desc.Namespace != expectDesc.Desc.Namespace {
		return false, fmt.Errorf("expected description of NEG object %q/%q to be %+v, but got %+v", zone, negName, expectDesc.Desc, desc)
	}
	// Same as for StandardNEGDescription, a conflict within the same cluster and namespace wraps
	// ErrNEGUsedByAnotherSyncer, so that the NEG CR status is not updated. For the default subnet
	// NEG the CR is named after the NEG and is owned by this service, so the conflict is reported
	// with the NegNameConflict event only. It is still needed for NEGs in non-default subnets,
	// where "<name>-<subnet hash>" may collide with the custom name of another service.
	if desc.ServiceName != expectDesc.Desc.ServiceName || !portsMatch(expectDesc.Desc.Port, desc.Port) {
		return false, fmt.Errorf("%w: expected description of NEG object %q/%q to be %+v, but got %+v", ErrNEGUsedByAnotherSyncer, zone, negName, expectDesc.Desc, desc)
	}
	return true, nil
}

// UserFacingString returns the description users should set on a pre-created NEG.
// The port is omitted since it is meaningless for GCE_VM_IP NEGs.
func (desc CustomL4PassthroughNEGDescription) UserFacingString() string {
	withoutPort := desc.Desc
	withoutPort.Port = ""
	return withoutPort.String()
}

// portsMatch treats a missing port and port "0" as equal.
func portsMatch(expected, got string) bool {
	if expected == got {
		return true
	}
	isUnset := func(port string) bool { return port == "" || port == "0" }
	return isUnset(expected) && isUnset(got)
}

// BoundNEGDescription stores the description for a NEG managed for NEGBinding CR.
type BoundNEGDescription struct {
	ClusterURL string `json:"cluster-url,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	BackendRef string `json:"backend-ref,omitempty"`
}

// String returns the string representation of a BoundNEGDescription.
func (desc BoundNEGDescription) String() string {
	descJson, err := json.Marshal(desc)
	if err != nil {
		klog.Errorf("Failed to generate neg description string: %v, falling back to empty string", err)
		return ""
	}
	return string(descJson)
}

type parsedClusterURL struct {
	scheme      string
	host        string
	projectName string
	location    string
	clusterName string
}

func parseClusterURL(rawURL string) (parsedClusterURL, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return parsedClusterURL{}, fmt.Errorf("invalid cluster URL %q", rawURL)
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 7 || parts[0] == "" || parts[1] != "projects" || parts[2] == "" ||
		(parts[3] != "locations" && parts[3] != "zones") || parts[4] == "" ||
		parts[5] != "clusters" || parts[6] == "" {
		return parsedClusterURL{}, fmt.Errorf("invalid cluster URL path in %q", rawURL)
	}
	return parsedClusterURL{
		scheme:      u.Scheme,
		host:        u.Host,
		projectName: parts[2],
		location:    parts[4],
		clusterName: parts[6],
	}, nil
}

func equalClusterURLs(a, b string) bool {
	parsedA, err := parseClusterURL(a)
	if err != nil {
		return false
	}
	parsedB, err := parseClusterURL(b)
	if err != nil {
		return false
	}
	return parsedA == parsedB
}

// MatchesString returns whether the provided descString fields match description's fields.
// Unlike StandardNEGDescription if descString can't be unmarshalled into description it's considered as invalid description of NEG.
func (expectDesc BoundNEGDescription) MatchesString(descString, negName, zone string) (bool, error) {
	desc, err := NEGDescriptionFromString[BoundNEGDescription](descString)
	if err != nil {
		klog.Warningf("Error unmarshalling NEG description %s err:%v", negName, err)
		return false, fmt.Errorf("error unmarshalling NEG description %s err:%w", negName, err)
	}

	if !equalClusterURLs(desc.ClusterURL, expectDesc.ClusterURL) ||
		desc.Namespace != expectDesc.Namespace ||
		desc.BackendRef != expectDesc.BackendRef {
		return false, fmt.Errorf("expected description of NEG object %q/%q to be %+v, but got %+v", zone, negName, expectDesc, desc)
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
