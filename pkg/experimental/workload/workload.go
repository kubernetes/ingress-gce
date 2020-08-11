package workload

import (
	apisworkload "k8s.io/ingress-gce/pkg/apis/workload"
	workloadv1a1 "k8s.io/ingress-gce/pkg/apis/workload/v1alpha1"
	"k8s.io/ingress-gce/pkg/crd"
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
	meta.AddValidationInfo("k8s.io/ingress-gce/pkg/apis/workload/v1alpha1.Workload", workloadv1a1.GetOpenAPIDefinitions)
	return meta
}
