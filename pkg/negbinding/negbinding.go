/*
Copyright 2026 The Kubernetes Authors.

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
package negbinding

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apis "k8s.io/ingress-gce/pkg/apis/negbinding"
	"k8s.io/ingress-gce/pkg/apis/negbinding/v1beta1"
	"k8s.io/ingress-gce/pkg/crd"
)

func CRDMeta() *crd.CRDMeta {
	meta := crd.NewCRDMeta(
		apis.GroupName,
		"NetworkEndpointGroupBinding",
		"NetworkEndpointGroupBindingList",
		"networkendpointgroupbinding",
		"networkendpointgroupbindings",
		[]*crd.Version{
			crd.NewVersion(
				"v1beta1",
				"k8s.io/ingress-gce/pkg/apis/negbinding/v1beta1.NetworkEndpointGroupBinding",
				v1beta1.GetOpenAPIDefinitions,
				&apiextensionsv1.CustomResourceSubresources{
					Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
				},
				false,
			),
		},
		"negbinding",
	)
	return meta
}
