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

package workload

import (
	"k8s.io/ingress-gce/pkg/crd"
	apisworkload "k8s.io/ingress-gce/pkg/experimental/apis/workload"
	workloadv1a1 "k8s.io/ingress-gce/pkg/experimental/apis/workload/v1alpha1"
)

func CRDMeta() *crd.CRDMeta {
	meta := crd.NewCRDMeta(
		apisworkload.GroupName,
		"v1alpha1",
		"Workload",
		"WorkloadList",
		"workload",
		"workloads",
		"wl",
	)
	meta.AddValidationInfo("k8s.io/ingress-gce/pkg/experimental/apis/workload/v1alpha1.Workload", workloadv1a1.GetOpenAPIDefinitions)
	return meta
}
