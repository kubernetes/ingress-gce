/*
Copyright 2015 The Kubernetes Authors.

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
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud"
)

const (
	// Add used to record additions in a sync pool.
	Add = iota
	// Remove used to record removals from a sync pool.
	Remove
	// Sync used to record syncs of a sync pool.
	Sync
	// Get used to record Get from a sync pool.
	Get
	// Create used to record creations in a sync pool.
	Create
	// Update used to record updates in a sync pool.
	Update
	// Delete used to record deltions from a sync pool.
	Delete
	// AddInstances used to record a call to AddInstances.
	AddInstances
	// RemoveInstances used to record a call to RemoveInstances.
	RemoveInstances
)

// FakeGoogleAPIForbiddenErr creates a Forbidden error with type googleapi.Error
func FakeGoogleAPIForbiddenErr() *googleapi.Error {
	return &googleapi.Error{Code: http.StatusForbidden}
}

// FakeGoogleAPINotFoundErr creates a NotFound error with type googleapi.Error
func FakeGoogleAPINotFoundErr() *googleapi.Error {
	return &googleapi.Error{Code: http.StatusNotFound}
}

// IsHTTPErrorCode checks if the given error matches the given HTTP Error code.
// For this to work the error must be a googleapi Error.
func IsHTTPErrorCode(err error, code int) bool {
	apiErr, ok := err.(*googleapi.Error)
	return ok && apiErr.Code == code
}

// ToNamespacedName returns a types.NamespacedName struct parsed from namespace/name.
func ToNamespacedName(s string) (r types.NamespacedName, err error) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return r, fmt.Errorf("service should take the form 'namespace/name': %q", s)
	}
	return types.NamespacedName{
		Namespace: parts[0],
		Name:      parts[1],
	}, nil
}

// IgnoreHTTPNotFound returns the passed err if it's not a GoogleAPI error
// with a NotFound status code.
func IgnoreHTTPNotFound(err error) error {
	if err != nil && IsHTTPErrorCode(err, http.StatusNotFound) {
		return nil
	}
	return err
}

// IsInUsedByError returns true if the resource is being used by another GCP resource
func IsInUsedByError(err error) bool {
	apiErr, ok := err.(*googleapi.Error)
	if !ok || apiErr.Code != http.StatusBadRequest {
		return false
	}
	return strings.Contains(apiErr.Message, "being used by")
}

// IsNotFoundError returns true if the resource does not exist
func IsNotFoundError(err error) bool {
	return IsHTTPErrorCode(err, http.StatusNotFound)
}

// IsForbiddenError returns true if the operation was forbidden
func IsForbiddenError(err error) bool {
	return IsHTTPErrorCode(err, http.StatusForbidden)
}

// trimFieldsEvenly trims the fields evenly and keeps the total length
// <= max. Truncation is spread in ratio with their original length,
// meaning smaller fields will be truncated less than longer ones.
func trimFieldsEvenly(max int, fields ...string) []string {
	if max <= 0 {
		return fields
	}
	total := 0
	for _, s := range fields {
		total += len(s)
	}
	if total <= max {
		return fields
	}
	// Distribute truncation evenly among the fields.
	excess := total - max
	remaining := max
	var lengths []int
	for _, s := range fields {
		// Scale truncation to shorten longer fields more than ones that are already short.
		l := len(s) - len(s)*excess/total - 1
		lengths = append(lengths, l)
		remaining -= l
	}
	// Add fractional space that was rounded down.
	for i := 0; i < remaining; i++ {
		lengths[i]++
	}

	var ret []string
	for i, l := range lengths {
		ret = append(ret, fields[i][:l])
	}

	return ret
}

// PrettyJson marshals an object in a human-friendly format.
func PrettyJson(data interface{}) (string, error) {
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetIndent("", "\t")

	err := encoder.Encode(data)
	if err != nil {
		return "", err
	}
	return buffer.String(), nil
}

// KeyName returns the name portion from a full or partial GCP resource URL.
// Example:
//  Input:  https://googleapis.com/v1/compute/projects/my-project/global/backendServices/my-backend
//  Output: my-backend
func KeyName(url string) (string, error) {
	id, err := cloud.ParseResourceURL(url)
	if err != nil {
		return "", err
	}

	if id.Key == nil {
		// Resource is projects
		return id.ProjectID, nil
	}

	return id.Key.Name, nil
}

// RelativeResourceName returns the project, location, resource, and name from a full/partial GCP
// resource URL. This removes the endpoint prefix and version.
// Example:
//  Input:  https://googleapis.com/v1/compute/projects/my-project/global/backendServices/my-backend
//  Output: projects/my-project/global/backendServices/my-backend
func RelativeResourceName(url string) (string, error) {
	resID, err := cloud.ParseResourceURL(url)
	if err != nil {
		return "", err
	}
	return resID.RelativeResourceName(), nil
}

// ResourcePath returns the location, resource and name portion from a
// full or partial GCP resource URL. This removes the endpoint prefix, version, and project.
// Example:
//  Input:  https://googleapis.com/v1/compute/projects/my-project/global/backendServices/my-backend
//  Output: global/backendServices/my-backend
func ResourcePath(url string) (string, error) {
	resID, err := cloud.ParseResourceURL(url)
	if err != nil {
		return "", err
	}
	return resID.ResourcePath(), nil
}

// EqualResourcePaths returns true if a and b have equal ResourcePaths. Resource paths
// entail the location, resource type, and resource name.
func EqualResourcePaths(a, b string) bool {
	aPath, err := ResourcePath(a)
	if err != nil {
		return false
	}

	bPath, err := ResourcePath(b)
	if err != nil {
		return false
	}

	return aPath == bPath
}

// EqualResourceIDs returns true if a and b have equal ResourceIDs which entail the project,
// location, resource type, and resource name.
func EqualResourceIDs(a, b string) bool {
	aId, err := cloud.ParseResourceURL(a)
	if err != nil {
		return false
	}

	bId, err := cloud.ParseResourceURL(b)
	if err != nil {
		return false
	}

	return aId.Equal(bId)
}

// IGLinks returns a list of links extracted from the passed in list of
// compute.InstanceGroup's.
func IGLinks(igs []*compute.InstanceGroup) (igLinks []string) {
	for _, ig := range igs {
		igLinks = append(igLinks, ig.SelfLink)
	}
	return
}
