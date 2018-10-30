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

// Package equal provides utility functions for checking equality.
package equal

import (
	"reflect"
	"sort"

	api "github.com/GoogleCloudPlatform/gke-managed-certs/pkg/apis/gke.googleapis.com/v1alpha1"
	compute "google.golang.org/api/compute/v0.alpha"
)

// Certificates compares Managed Certificate and SslCertificate for equality, i. e. if their domain sets are equal.
func Certificates(mcrt api.ManagedCertificate, sslCert compute.SslCertificate) bool {
	mcrtDomains := make([]string, len(mcrt.Spec.Domains))
	copy(mcrtDomains, mcrt.Spec.Domains)
	sort.Strings(mcrtDomains)

	sslCertDomains := make([]string, len(sslCert.Managed.Domains))
	copy(sslCertDomains, sslCert.Managed.Domains)
	sort.Strings(sslCertDomains)

	return reflect.DeepEqual(mcrtDomains, sslCertDomains)
}
