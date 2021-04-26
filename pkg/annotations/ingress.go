/*
Copyright 2017 The Kubernetes Authors.

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

package annotations

import (
	"errors"
	"strconv"

	"k8s.io/api/networking/v1beta1"
)

const (
	// StatusPrefix is the prefix used in annotations used to record
	// debug information in the Ingress annotations.
	StatusPrefix = "ingress.kubernetes.io"

	// AllowHTTPKey tells the Ingress controller to allow/block HTTP access.
	// If either unset or set to true, the controller will create a
	// forwarding-rule for port 80, and any additional rules based on the TLS
	// section of the Ingress. If set to false, the controller will only create
	// rules for port 443 based on the TLS section.
	AllowHTTPKey = "kubernetes.io/ingress.allow-http"

	// GlobalStaticIPNameKey tells the Ingress controller to use a specific GCE
	// static ip for its forwarding rules. If specified, the Ingress controller
	// assigns the static ip by this name to the forwarding rules of the given
	// Ingress. The controller *does not* manage this ip, it is the users
	// responsibility to create/delete it.
	GlobalStaticIPNameKey = "kubernetes.io/ingress.global-static-ip-name"

	// RegionalStaticIPNameKey tells the Ingress controller to use a specific GCE
	// internal static ip for its forwarding rules. If specified, the Ingress controller
	// assigns the static ip by this name to the forwarding rules of the given
	// Ingress. The controller *does not* manage this ip, it is the users
	// responsibility to create/delete it.
	RegionalStaticIPNameKey = "kubernetes.io/ingress.regional-static-ip-name"

	// PreSharedCertKey represents the specific pre-shared SSL
	// certificate for the Ingress controller to use. The controller *does not*
	// manage this certificate, it is the users responsibility to create/delete it.
	// In GCP, the Ingress controller assigns the SSL certificate with this name
	// to the target proxies of the Ingress.
	PreSharedCertKey = "ingress.gcp.kubernetes.io/pre-shared-cert"

	// IngressClassKey picks a specific "class" for the Ingress. The controller
	// only processes Ingresses with this annotation either unset, or set
	// to either gceIngressClass or the empty string.
	IngressClassKey      = "kubernetes.io/ingress.class"
	GceIngressClass      = "gce"
	GceMultiIngressClass = "gce-multi-cluster"
	GceL7ILBIngressClass = "gce-internal"

	// Label key to denote which GCE zone a Kubernetes node is in.
	ZoneKey     = "failure-domain.beta.kubernetes.io/zone"
	DefaultZone = ""

	// InstanceGroupsAnnotationKey is the annotation key used by controller to
	// specify the name and zone of instance groups created for the ingress.
	// This is read only for users. Controller will overrite any user updates.
	// This is only set for ingresses with ingressClass = "gce-multi-cluster"
	InstanceGroupsAnnotationKey = "ingress.gcp.kubernetes.io/instance-groups"

	// SuppressFirewallXPNErrorKey is the annotation key used by firewall
	// controller whether to suppress firewallXPNError.
	SuppressFirewallXPNErrorKey = "networking.gke.io/suppress-firewall-xpn-error"

	// FrontendConfigKey is the annotation key used by controller to specify
	// the FrontendConfig resource which should be associated with the Ingress.
	// The value of the annotation is the name of the FrontendConfig resource.
	// Examples:
	// - annotations:
	//     networking.gke.io/v1beta1.FrontendConfig: 'my-frontendconfig'
	FrontendConfigKey = "networking.gke.io/v1beta1.FrontendConfig"

	// UrlMapKey is the annotation key used by controller to record GCP URL map.
	UrlMapKey = StatusPrefix + "/url-map"
	// UrlMapKey is the annotation key used by controller to record GCP URL map used for Https Redirects only.
	RedirectUrlMapKey = StatusPrefix + "/redirect-url-map"
	// HttpForwardingRuleKey is the annotation key used by controller to record
	// GCP http forwarding rule.
	HttpForwardingRuleKey = StatusPrefix + "/forwarding-rule"
	// HttpsForwardingRuleKey is the annotation key used by controller to record
	// GCP https forwarding rule.
	HttpsForwardingRuleKey = StatusPrefix + "/https-forwarding-rule"
	// TargetHttpProxyKey is the annotation key used by controller to record
	// GCP target http proxy.
	TargetHttpProxyKey = StatusPrefix + "/target-proxy"
	// TargetHttpsProxyKey is the annotation key used by controller to record
	// GCP target https proxy.
	TargetHttpsProxyKey = StatusPrefix + "/https-target-proxy"
	// SSLCertKey is the annotation key used by controller to record GCP ssl cert.
	SSLCertKey = StatusPrefix + "/ssl-cert"
	// StaticIPKey is the annotation key used by controller to record GCP static ip.
	StaticIPKey = StatusPrefix + "/static-ip"
)

// Ingress represents ingress annotations.
type Ingress struct {
	v map[string]string
}

// FromIngress extracts the annotations from an Ingress definition.
func FromIngress(ing *v1beta1.Ingress) *Ingress {
	result := &Ingress{}
	if ing != nil {
		result.v = ing.Annotations
	}
	return result
}

// AllowHTTP returns the allowHTTP flag. True by default.
func (ing *Ingress) AllowHTTP() bool {
	val, ok := ing.v[AllowHTTPKey]
	if !ok {
		return true
	}
	v, err := strconv.ParseBool(val)
	if err != nil {
		return true
	}
	return v
}

// UseNamedTLS returns the name of the GCE SSL certificate. Empty by default.
func (ing *Ingress) UseNamedTLS() string {
	val, ok := ing.v[PreSharedCertKey]
	if !ok {
		return ""
	}

	return val
}

func (ing *Ingress) StaticIPName() (string, error) {
	globalIp := ing.GlobalStaticIPName()
	regionalIp := ing.RegionalStaticIPName()

	if globalIp != "" && regionalIp != "" {
		return "", errors.New("Error: both global-static-ip and regional-static-ip cannot be specified")
	}

	if regionalIp != "" {
		return regionalIp, nil
	}

	return globalIp, nil
}

func (ing *Ingress) GlobalStaticIPName() string {
	val, ok := ing.v[GlobalStaticIPNameKey]
	if !ok {
		return ""
	}
	return val
}

func (ing *Ingress) RegionalStaticIPName() string {
	val, ok := ing.v[RegionalStaticIPNameKey]
	if !ok {
		return ""
	}
	return val
}

func (ing *Ingress) IngressClass() string {
	val, ok := ing.v[IngressClassKey]
	if !ok {
		return ""
	}
	return val
}

// SuppressFirewallXPNError returns the SuppressFirewallXPNErrorKey flag.
// False by default.
func (ing *Ingress) SuppressFirewallXPNError() bool {
	val, ok := ing.v[SuppressFirewallXPNErrorKey]
	if !ok {
		return false
	}
	v, err := strconv.ParseBool(val)
	if err != nil {
		return false
	}
	return v
}

func (ing *Ingress) FrontendConfig() string {
	val, ok := ing.v[FrontendConfigKey]
	if !ok {
		return ""
	}
	return val
}
