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

package fuzz

import (
	v1 "k8s.io/api/networking/v1"
	frontendconfig "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
)

// WhiteboxTest represents a whitebox test than can be run for an Ingress.
// The test validates a part of the Ingress spec against GCE resources.
type WhiteboxTest interface {
	// Name of the test.
	Name() string
	// Test is the test to run.
	Test(ing *v1.Ingress, fc *frontendconfig.FrontendConfig, gclb *GCLB) error
}
