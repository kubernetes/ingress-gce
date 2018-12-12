package operator

import (
	"fmt"

	v1beta1 "k8s.io/ingress-gce/pkg/apis/cloud/v1beta1"

	api_v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
)

// Ingresses returns the wrapper
func Ingresses(i []*extensions.Ingress) *IngressesOperator {
	return &IngressesOperator{i: i}
}

// IngressesOperator is an operator wrapper for a list of Ingresses.
type IngressesOperator struct {
	i []*extensions.Ingress
}

// AsList returns the underlying list of Ingresses.
func (op *IngressesOperator) AsList() []*extensions.Ingress {
	if op.i == nil {
		return []*extensions.Ingress{}
	}
	return op.i
}

// Filter the list of Ingresses based on a predicate.
func (op *IngressesOperator) Filter(f func(*extensions.Ingress) bool) *IngressesOperator {
	var i []*extensions.Ingress
	for _, ing := range op.i {
		if f(ing) {
			i = append(i, ing)
		}
	}
	return Ingresses(i)
}

// ReferencesService returns the Ingresses that references the given Service.
func (op *IngressesOperator) ReferencesService(svc *api_v1.Service) *IngressesOperator {
	dupes := map[string]bool{}

	var i []*extensions.Ingress
	for _, ing := range op.i {
		key := fmt.Sprintf("%s/%s", ing.Namespace, ing.Name)
		if doesIngressReferenceService(ing, svc) && !dupes[key] {
			i = append(i, ing)
			dupes[key] = true
		}
	}
	return Ingresses(i)
}

// ReferencesBackendConfig returns the Ingresses that references the given BackendConfig.
func (op *IngressesOperator) ReferencesBackendConfig(beConfig *v1beta1.BackendConfig, svcsOp *ServicesOperator) *IngressesOperator {
	dupes := map[string]bool{}

	var i []*extensions.Ingress
	svcs := svcsOp.ReferencesBackendConfig(beConfig).AsList()
	for _, ing := range op.i {
		for _, svc := range svcs {
			key := fmt.Sprintf("%s/%s", ing.Namespace, ing.Name)
			if doesIngressReferenceService(ing, svc) && !dupes[key] {
				i = append(i, ing)
				dupes[key] = true
			}
		}
	}
	return Ingresses(i)
}
