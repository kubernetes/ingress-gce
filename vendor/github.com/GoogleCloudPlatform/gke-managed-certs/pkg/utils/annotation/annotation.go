/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package annotation provides utilities for manipulating a managed-certificates annotation.
package annotation

import (
	"strings"

	"github.com/golang/glog"
	api "k8s.io/api/extensions/v1beta1"
)

const (
	annotation = "gke.googleapis.com/managed-certificates"
	splitBy    = ","
)

/*
* Parses managed-certificates Ingress annotation to extract names of ManagedCertificate objects from it.
* Returns:
*  - a slice of names,
*  - a boolean flag which is true if Ingress is annotated with a non-empty managed-certificates annotation.
 */
func Parse(ingress *api.Ingress) ([]string, bool) {
	annotationValue, exists := ingress.Annotations[annotation]
	if !exists || annotationValue == "" {
		return nil, false
	}

	glog.Infof("Found %s annotation %q on ingress %s:%s", annotation, annotationValue, ingress.Namespace, ingress.Name)

	var result []string
	for _, value := range strings.Split(annotationValue, splitBy) {
		result = append(result, strings.TrimSpace(value))
	}

	return result, true
}
