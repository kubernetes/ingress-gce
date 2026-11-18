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

package standaloneneg

import (
	"errors"
	"fmt"
	"slices"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/composite"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
)

var (
	// supportedLBSchemes is a list of supported LB schemes. Only Forwarding rules
	// with these schemes can be referenced in a L4 standalone NEG service.
	supportedLBSchemes = []string{
		string(cloud.SchemeExternal),
		"EXTERNAL_PASSTHROUGH",
	}
	// supportedFRProtocols is a list of supported protocols. Only Forwarding
	// rules with these protocols can be referenced in a L4 standalone NEG
	// service.
	supportedFRProtocols = []string{
		"TCP",
		"UDP",
		"L3_DEFAULT",
	}
)

func isSchemeSupported(lbScheme string) bool {
	return slices.Contains(supportedLBSchemes, lbScheme)
}

func isFRProtocolSupported(protocol string) bool {
	return slices.Contains(supportedFRProtocols, protocol)
}

func validateForwardingRule(fr *composite.ForwardingRule, frName string) error {
	var errs []error
	if !isSchemeSupported(fr.LoadBalancingScheme) {
		errs = append(errs, l4utils.NewUnsupportedLoadBalancingSchemeError(frName, fr.LoadBalancingScheme, supportedLBSchemes))
	}
	if !isFRProtocolSupported(fr.IPProtocol) {
		errs = append(errs, l4utils.NewUnsupportedProtocolError(frName, fr.IPProtocol, supportedFRProtocols))
	}
	if len(fr.IPAddresses) > 2 {
		errs = append(errs, fmt.Errorf("forwarding rule %s has more than 2 IP addresses", frName))
	}
	return errors.Join(errs...)
}

// backendServiceHasNEGAttached checks if the backend service has any of
// the targetNEGs attached.
func backendServiceHasNEGAttached(bs *composite.BackendService, targetNEGs sets.Set[cloud.ResourceMapKey]) bool {
	hasMatchingNEG := false
	for _, be := range bs.Backends {
		if be == nil || be.Group == "" {
			continue
		}
		groupKey, err := cloud.ParseResourceURL(be.Group)

		if err == nil && groupKey != nil && targetNEGs.Has(groupKey.MapKey()) {
			hasMatchingNEG = true
			break
		}
	}
	return hasMatchingNEG
}
