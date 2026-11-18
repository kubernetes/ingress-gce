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
	"net/netip"
	"slices"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
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

// validateForwardingRule checks that the forwarding rule is supported.
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

// backendServiceKey returns the key of the backend service referenced by the
// forwarding rule.
func backendServiceKey(fr *composite.ForwardingRule) (*meta.Key, error) {
	bsURL := fr.BackendService
	if bsURL == "" {
		return nil, l4utils.NewUserError(fmt.Errorf("the service NEGs are not attached to the load balancer, forwarding rule is missing the backend service reference"))
	}
	resourceID, err := cloud.ParseResourceURL(bsURL)
	if err != nil {
		return nil, l4utils.NewUserError(fmt.Errorf("failed to parse backend service URL %s: %w", bsURL, err))
	}
	if resourceID == nil || resourceID.Key == nil || resourceID.Key.Name == "" {
		return nil, l4utils.NewUserError(fmt.Errorf("invalid backend service URL %s: missing resource key", bsURL))
	}
	return resourceID.Key, nil
}

// validateBackendServiceAttachment checks that the backend service has any of
// the service NEGs attached.
func validateBackendServiceAttachment(bs *composite.BackendService, serviceNEGs sets.Set[cloud.ResourceMapKey]) error {
	for _, be := range bs.Backends {
		if be == nil || be.Group == "" {
			continue
		}
		groupID, err := cloud.ParseResourceURL(be.Group)
		if err == nil && groupID != nil && serviceNEGs.Has(groupID.MapKey()) {
			return nil
		}
	}
	return l4utils.NewUserError(l4utils.NewBackendNotAttachedError(bs.Name))
}

// frAddresses returns the addresses of the forwarding rule. The IPAddress
// field is used by regional forwarding rules, the IPAddresses field by global
// forwarding rules.
func frAddresses(fr *composite.ForwardingRule) []string {
	if len(fr.IPAddresses) > 0 {
		return fr.IPAddresses
	}
	if fr.IPAddress != "" {
		return []string{fr.IPAddress}
	}
	return nil
}

// forwardingRuleVIPs returns the IPs of the forwarding rule, as written in
// the service status.
func forwardingRuleVIPs(fr *composite.ForwardingRule) []string {
	var vips []string
	for _, address := range frAddresses(fr) {
		if address == "" {
			continue
		}
		// GCP IPv6 forwarding rules provide the IP in CIDR form (e.g. /96 range), the base IP
		// is extracted and made canonical to avoid warnings from k8s apiserver.
		vip := strings.Split(address, "/")[0]
		if ipAddr, err := netip.ParseAddr(vip); err == nil {
			vip = ipAddr.String()
		}
		if vip != "" {
			vips = append(vips, vip)
		}
	}
	return vips
}

// healthCheckKey returns the key of the health check of the backend service.
// The backend service must have a health check.
func healthCheckKey(bs *composite.BackendService) (*meta.Key, error) {
	hcURL := bs.HealthChecks[0]
	hcID, err := cloud.ParseResourceURL(hcURL)
	if err != nil || hcID.Key == nil || hcID.Resource != "healthChecks" {
		return nil, fmt.Errorf("invalid health check URL %q of backend service %s", hcURL, bs.Name)
	}
	return hcID.Key, nil
}

// httpHealthCheckPort returns the port of the HTTP health check. If the health
// check is not an HTTP health check with a fixed port, it returns the reason
// why it can't be used instead.
func httpHealthCheckPort(hc *composite.HealthCheck, hcURL string) (port int64, notUsableReason string) {
	switch {
	case hc.Type != "HTTP" || hc.HttpHealthCheck == nil:
		return 0, fmt.Sprintf("health check %s is of type %s, not HTTP", hcURL, hc.Type)
	case hc.HttpHealthCheck.Port == 0:
		return 0, fmt.Sprintf("HTTP health check %s has no fixed port (port specification %q)", hcURL, hc.HttpHealthCheck.PortSpecification)
	}
	return hc.HttpHealthCheck.Port, ""
}
