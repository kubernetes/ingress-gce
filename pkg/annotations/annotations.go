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
	"encoding/json"
	"fmt"
	"strconv"

	"k8s.io/ingress-gce/pkg/utils"
)

const (
	// AllowHTTPKey tells the Ingress controller to allow/block HTTP access.
	// If either unset or set to true, the controller will create a
	// forwarding-rule for port 80, and any additional rules based on the TLS
	// section of the Ingress. If set to false, the controller will only create
	// rules for port 443 based on the TLS section.
	AllowHTTPKey = "kubernetes.io/ingress.allow-http"

	// StaticIPNameKey tells the Ingress controller to use a specific GCE
	// static ip for its forwarding rules. If specified, the Ingress controller
	// assigns the static ip by this name to the forwarding rules of the given
	// Ingress. The controller *does not* manage this ip, it is the users
	// responsibility to create/delete it.
	StaticIPNameKey = "kubernetes.io/ingress.global-static-ip-name"

	// PreSharedCertKey represents the specific pre-shared SSL
	// certicate for the Ingress controller to use. The controller *does not*
	// manage this certificate, it is the users responsibility to create/delete it.
	// In GCP, the Ingress controller assigns the SSL certificate with this name
	// to the target proxies of the Ingress.
	PreSharedCertKey = "ingress.gcp.kubernetes.io/pre-shared-cert"

	// ServiceApplicationProtocolKey is a stringified JSON map of port names to
	// protocol strings. Possible values are HTTP, HTTPS
	// Example:
	// '{"my-https-port":"HTTPS","my-http-port":"HTTP"}'
	ServiceApplicationProtocolKey = "service.alpha.kubernetes.io/app-protocols"

	// IngressClassKey picks a specific "class" for the Ingress. The controller
	// only processes Ingresses with this annotation either unset, or set
	// to either gceIngessClass or the empty string.
	IngressClassKey      = "kubernetes.io/ingress.class"
	GceIngressClass      = "gce"
	GceMultiIngressClass = "gce-multi-cluster"

	// ZoneKey denotes which GCE zone a Kubernetes node is in.
	ZoneKey     = "failure-domain.beta.kubernetes.io/zone"
	DefaultZone = ""

	// InstanceGroupsAnnotationKey is the annotation key used by controller to
	// specify the name and zone of instance groups created for the ingress.
	// This is read only for users. Controller will overrite any user updates.
	// This is only set for ingresses with ingressClass = "gce-multi-cluster"
	InstanceGroupsAnnotationKey = "ingress.gcp.kubernetes.io/instance-groups"

	// NetworkEndpointGroupAlphaAnnotation is the annotation key to enable GCE NEG feature for ingress backend services.
	// To enable this feature, the value of the annotation must be "true".
	// This annotation should be specified on services that are backing ingresses.
	// WARNING: The feature will NOT be effective in the following circumstances:
	// 1. NEG feature is not enabled in feature gate.
	// 2. Service is not referenced in any ingress.
	// 3. Adding this annotation on ingress.
	NetworkEndpointGroupAlphaAnnotation = "alpha.cloud.google.com/load-balancer-neg"
)

// IngAnnotations represents ingress annotations.
type IngAnnotations map[string]string

// AllowHTTP returns the allowHTTP flag. True by default.
func (ing IngAnnotations) AllowHTTP() bool {
	val, ok := ing[AllowHTTPKey]
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
func (ing IngAnnotations) UseNamedTLS() string {
	val, ok := ing[PreSharedCertKey]
	if !ok {
		return ""
	}

	return val
}

func (ing IngAnnotations) StaticIPName() string {
	val, ok := ing[StaticIPNameKey]
	if !ok {
		return ""
	}
	return val
}

func (ing IngAnnotations) IngressClass() string {
	val, ok := ing[IngressClassKey]
	if !ok {
		return ""
	}
	return val
}

// SvcAnnotations represents Service annotations.
type SvcAnnotations map[string]string

func (svc SvcAnnotations) ApplicationProtocols() (map[string]utils.AppProtocol, error) {
	val, ok := svc[ServiceApplicationProtocolKey]
	if !ok {
		return map[string]utils.AppProtocol{}, nil
	}

	var portToProtos map[string]utils.AppProtocol
	err := json.Unmarshal([]byte(val), &portToProtos)

	// Verify protocol is an accepted value
	for _, proto := range portToProtos {
		switch proto {
		case utils.ProtocolHTTP, utils.ProtocolHTTPS:
		default:
			return nil, fmt.Errorf("invalid port application protocol: %v", proto)
		}
	}

	return portToProtos, err
}

func (svc SvcAnnotations) NEGEnabled() bool {
	v, ok := svc[NetworkEndpointGroupAlphaAnnotation]
	return ok && v == "true"
}
