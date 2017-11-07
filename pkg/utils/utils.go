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
	"fmt"
	"net/http"
	"strings"

	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
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

	// K8sAnnotationPrefix is the prefix used in annotations used to record
	// debug information in the Ingress annotations.
	K8sAnnotationPrefix = "ingress.kubernetes.io"

	// ProtocolHTTP protocol for a service
	ProtocolHTTP AppProtocol = "HTTP"
	// ProtocolHTTPS protocol for a service
	ProtocolHTTPS AppProtocol = "HTTPS"
)

type AppProtocol string

// GCEURLMap is a nested map of hostname->path regex->backend
type GCEURLMap map[string]map[string]*compute.BackendService

// GetDefaultBackend performs a destructive read and returns the default
// backend of the urlmap.
func (g GCEURLMap) GetDefaultBackend() *compute.BackendService {
	var d *compute.BackendService
	var exists bool
	if h, ok := g[DefaultBackendKey]; ok {
		if d, exists = h[DefaultBackendKey]; exists {
			delete(h, DefaultBackendKey)
		}
		delete(g, DefaultBackendKey)
	}
	return d
}

// String implements the string interface for the GCEURLMap.
func (g GCEURLMap) String() string {
	msg := ""
	for host, um := range g {
		msg += fmt.Sprintf("%v\n", host)
		for url, be := range um {
			msg += fmt.Sprintf("\t%v: ", url)
			if be == nil {
				msg += fmt.Sprintf("No backend\n")
			} else {
				msg += fmt.Sprintf("%v\n", be.Name)
			}
		}
	}
	return msg
}

// PutDefaultBackend performs a destructive write replacing the
// default backend of the url map with the given backend.
func (g GCEURLMap) PutDefaultBackend(d *compute.BackendService) {
	g[DefaultBackendKey] = map[string]*compute.BackendService{
		DefaultBackendKey: d,
	}
}

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

// CompareLinks returns true if the 2 self links are equal.
func CompareLinks(l1, l2 string) bool {
	// TODO: These can be partial links
	return l1 == l2 && l1 != ""
}

// FakeIngressRuleValueMap is a convenience type used by multiple submodules
// that share the same testing methods.
type FakeIngressRuleValueMap map[string]string

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
