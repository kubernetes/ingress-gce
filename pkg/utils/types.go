/*
Copyright 2020 The Kubernetes Authors.

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

import apisingparams "k8s.io/ingress-gce/pkg/apis/ingparams"

const (
	// GCPIngressControllerKey indicates whether an IngressClass is a GCE Ingress Class
	// or not.
	GCPIngressControllerKey = "networking.gke.io/ingress-controller"

	// GCPIngParamsAPIGroup is the APIGroup of the GCPIngressParams CRD
	GCPIngParamsAPIGroup = apisingparams.GroupName

	// GCPIngParamsKind is the kind of the GCPIngressParams CRD
	GCPIngParamsKind = "GCPIngressParams"
)
