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

package crd

import (
	"k8s.io/kube-openapi/pkg/common"
)

// CRDMeta contains version, validation and API information for a CRD.
type CRDMeta struct {
	groupName string
	// versions is a slice of supported API versions for the CRD.
	// The latest version should be the first element in the slice.
	versions   []*Version
	kind       string
	listKind   string
	singular   string
	plural     string
	shortNames []string
	typeSource string
	fn         common.GetOpenAPIDefinitions
}

// NewCRDMeta creates a CRDMeta type which can be passed to a CRDHandler in
// order to create/ensure a CRD.
func NewCRDMeta(groupName, kind, listKind, singular, plural string, versions []*Version, shortNames ...string) *CRDMeta {
	return &CRDMeta{
		groupName:  groupName,
		versions:   versions,
		kind:       kind,
		listKind:   listKind,
		singular:   singular,
		plural:     plural,
		shortNames: shortNames,
	}
}

// Version specifies the API version and meta information that is needed to
// generate OpenAPI schema based CRD validation.
type Version struct {
	name       string
	typeSource string
	fn         common.GetOpenAPIDefinitions
}

// NewVersion returns a CRD API version with validation metadata.
func NewVersion(name, typeSource string, fn common.GetOpenAPIDefinitions) *Version {
	return &Version{
		name:       name,
		typeSource: typeSource,
		fn:         fn,
	}
}
