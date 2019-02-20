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
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/kube-openapi/pkg/common"
)

// CRDMeta contains version, validation and API information for a CRD.
type CRDMeta struct {
	groupName  string
	versions   []apiextensionsv1beta1.CustomResourceDefinitionVersion
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
func NewCRDMeta(versions []apiextensionsv1beta1.CustomResourceDefinitionVersion, groupName, kind, listKind, singular, plural string, shortNames ...string) *CRDMeta {
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

// AddValidationInfo adds information that is needed to ensure validation is
// properly added to a CRD when CRDHandler.EnsureCRD is called.
func (m *CRDMeta) AddValidationInfo(typeSource string, fn common.GetOpenAPIDefinitions) {
	m.typeSource = typeSource
	m.fn = fn
}
