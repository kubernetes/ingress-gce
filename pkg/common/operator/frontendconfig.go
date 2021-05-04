package operator

import (
	"fmt"

	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	frontendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
)

// FrontendConfigs returns the wrapper
func FrontendConfigs(f []*frontendconfigv1beta1.FrontendConfig) *FrontendConfigsOperator {
	return &FrontendConfigsOperator{f: f}
}

// FrontendConfigsOperator is an operator wrapper for a list of FrontendConfigs.
type FrontendConfigsOperator struct {
	f []*frontendconfigv1beta1.FrontendConfig
}

// AsList returns the underlying list of Services
func (op *FrontendConfigsOperator) AsList() []*frontendconfigv1beta1.FrontendConfig {
	if op.f == nil {
		return []*frontendconfigv1beta1.FrontendConfig{}
	}
	return op.f
}

// ReferencedByIngress returns the FrontendConfigs that are referenced by the passed in Ingress.
func (op *FrontendConfigsOperator) ReferencedByIngress(ing *v1.Ingress) *FrontendConfigsOperator {
	dupes := map[string]bool{}

	var f []*frontendconfigv1beta1.FrontendConfig
	for _, feConfig := range op.f {
		key := fmt.Sprintf("%s/%s", feConfig.Namespace, feConfig.Name)
		if doesIngressReferenceFrontendConfig(ing, feConfig) && !dupes[key] {
			f = append(f, feConfig)
			dupes[key] = true
		}
	}
	return FrontendConfigs(f)
}

// doesIngressReferenceFrontendConfig returns true if the passed in Ingress directly references
// the passed in FrontendConfig.
func doesIngressReferenceFrontendConfig(ing *v1.Ingress, feConfig *frontendconfigv1beta1.FrontendConfig) bool {
	if ing.Namespace != feConfig.Namespace {
		return false
	}

	frontendConfigName := annotations.FromIngress(ing).FrontendConfig()
	return frontendConfigName == feConfig.Name
}
