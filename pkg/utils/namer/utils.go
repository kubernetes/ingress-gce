/*
Copyright 2019 The Kubernetes Authors.
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

package namer

import (
	"fmt"
	"regexp"

	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/klog"
)

const gceResourceNamePattern = "(?:[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?)"

// TrimFieldsEvenly trims the fields evenly and keeps the total length
// <= max. Truncation is spread in ratio with their original length,
// meaning smaller fields will be truncated less than longer ones.
func TrimFieldsEvenly(max int, fields ...string) []string {
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

// FrontendNamingScheme returns naming scheme for given ingress based on its finalizer.
func FrontendNamingScheme(ing *v1.Ingress) Scheme {
	switch {
	case common.HasGivenFinalizer(ing.ObjectMeta, common.FinalizerKeyV2):
		return V2NamingScheme
	case common.HasGivenFinalizer(ing.ObjectMeta, common.FinalizerKey):
		return V1NamingScheme
	default:
		klog.V(3).Infof("No finalizer found on Ingress %v, using v1 naming scheme", ing)
		return V1NamingScheme
	}
}

// FinalizerForNamingScheme returns finalizer corresponding to given frontend naming scheme.
func FinalizerForNamingScheme(scheme Scheme) (string, error) {
	switch scheme {
	case V1NamingScheme:
		return common.FinalizerKey, nil
	case V2NamingScheme:
		return common.FinalizerKeyV2, nil
	default:
		return "", fmt.Errorf("unexpected naming scheme: %s", scheme)
	}
}

// isValidGCEResourceName returns if given name is a valid GCE resource name.
func isValidGCEResourceName(name string) bool {
	if len(name) == 0 {
		return false
	}
	matchedString := regexp.MustCompile(gceResourceNamePattern).FindString(name)
	// Return true only if the entire string is a match.
	return matchedString == name
}
