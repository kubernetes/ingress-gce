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

package frontendconfig

import (
	"errors"

	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	apisfrontendconfig "k8s.io/ingress-gce/pkg/apis/frontendconfig"
	frontendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/common/operator"
	"k8s.io/ingress-gce/pkg/crd"
)

var (
	ErrFrontendConfigDoesNotExist = errors.New("no FrontendConfig for Ingress exists.")
)

func CRDMeta() *crd.CRDMeta {
	meta := crd.NewCRDMeta(
		apisfrontendconfig.GroupName,
		"FrontendConfig",
		"FrontendConfigList",
		"frontendconfig",
		"frontendconfigs",
		[]*crd.Version{
			crd.NewVersion("v1beta1", "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1.FrontendConfig", frontendconfigv1beta1.GetOpenAPIDefinitions),
		},
	)
	return meta
}

// FrontendConfigForIngress returns the corresponding FrontendConfig for the given Ingress if one was specified.
func FrontendConfigForIngress(feConfigs []*frontendconfigv1beta1.FrontendConfig, ing *v1.Ingress) (*frontendconfigv1beta1.FrontendConfig, error) {
	frontendConfigName := annotations.FromIngress(ing).FrontendConfig()
	if frontendConfigName == "" {
		// If the user did not provide the annotation at all, then we
		// do not want to return an error.
		return nil, nil
	}

	matches := operator.FrontendConfigs(feConfigs).ReferencedByIngress(ing).AsList()
	if len(matches) == 0 {
		return nil, ErrFrontendConfigDoesNotExist
	}

	// Note: Theoretically, this list should never have more than 1 item. That
	// would mean we have a bug somewhere in the operator or annotation processing.
	return matches[0], nil
}
