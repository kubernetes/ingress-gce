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

// Package features configures additional extra features for the Ingress.
// Examples for how to implement additional features can be found in the
// *_example.go files.
package features

import "k8s.io/ingress-gce/pkg/fuzz"

// All is the set of all features.
var All = []fuzz.Feature{
	AllowHTTP,
	PresharedCert,
}
