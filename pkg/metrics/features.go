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

package metrics

import (
	"fmt"
	"strconv"

	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	frontendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

type feature string

func (f feature) String() string {
	return string(f)
}

const (
	// WARNING: Please keep the following constants in sync with
	// pkg/annotations/ingress.go
	// allowHTTPKey tells the Ingress controller to allow/block HTTP access.
	allowHTTPKey         = "kubernetes.io/ingress.allow-http"
	ingressClassKey      = "kubernetes.io/ingress.class"
	gceIngressClass      = "gce"
	gceMultiIngressClass = "gce-multi-cluster"
	gceL7ILBIngressClass = "gce-internal"
	// preSharedCertKey represents the specific pre-shared SSL
	// certificate for the Ingress controller to use.
	preSharedCertKey = "ingress.gcp.kubernetes.io/pre-shared-cert"
	managedCertKey   = "networking.gke.io/managed-certificates"
	// StaticGlobalIPNameKey tells the Ingress controller to use a specific GCE
	// static ip for its forwarding rules. If specified, the Ingress controller
	// assigns the static ip by this name to the forwarding rules of the given
	// Ingress. The controller *does not* manage this ip, it is the users
	// responsibility to create/delete it.
	StaticGlobalIPNameKey = "kubernetes.io/ingress.global-static-ip-name"
	// staticIPKey is the annotation key used by controller to record GCP static ip.
	staticIPKey = "ingress.kubernetes.io/static-ip"
	// SSLCertKey is the annotation key used by controller to record GCP ssl cert.
	SSLCertKey = "ingress.kubernetes.io/ssl-cert"

	ingress                   = feature("Ingress")
	externalIngress           = feature("ExternalIngress")
	internalIngress           = feature("InternalIngress")
	httpEnabled               = feature("HTTPEnabled")
	hostBasedRouting          = feature("HostBasedRouting")
	pathBasedRouting          = feature("PathBasedRouting")
	tlsTermination            = feature("TLSTermination")
	secretBasedCertsForTLS    = feature("SecretBasedCertsForTLS")
	preSharedCertsForTLS      = feature("PreSharedCertsForTLS")
	managedCertsForTLS        = feature("ManagedCertsForTLS")
	staticGlobalIP            = feature("StaticGlobalIP")
	managedStaticGlobalIP     = feature("ManagedStaticGlobalIP")
	specifiedStaticGlobalIP   = feature("SpecifiedStaticGlobalIP")
	specifiedStaticRegionalIP = feature("SpecifiedStaticRegionalIP")

	servicePort         = feature("L7LBServicePort")
	externalServicePort = feature("L7XLBServicePort")
	internalServicePort = feature("L7ILBServicePort")
	neg                 = feature("NEG")

	// BackendConfig Features
	cloudCDN                  = feature("CloudCDN")
	cloudArmor                = feature("CloudArmor")
	cloudIAP                  = feature("CloudIAP")
	backendTimeout            = feature("BackendTimeout")
	backendConnectionDraining = feature("BackendConnectionDraining")
	clientIPAffinity          = feature("ClientIPAffinity")
	cookieAffinity            = feature("CookieAffinity")
	customRequestHeaders      = feature("CustomRequestHeaders")
	customHealthChecks        = feature("CustomHealthChecks")

	// FrontendConfig Features
	sslPolicy      = feature("SSLPolicy")
	httpsRedirects = feature("HTTPSRedirects")

	standaloneNeg  = feature("StandaloneNEG")
	ingressNeg     = feature("IngressNEG")
	asmNeg         = feature("AsmNEG")
	vmIpNeg        = feature("VmIpNEG")
	vmIpNegLocal   = feature("VmIpNegLocal")
	vmIpNegCluster = feature("VmIpNegCluster")
	customNamedNeg = feature("CustomNamedNEG")
	// negInSuccess feature specifies that syncers were created for the Neg
	negInSuccess = feature("NegInSuccess")
	// negInError feature specifies that an error occuring in ensuring Neg Syncer
	negInError = feature("NegInError")

	l4ILBService      = feature("L4ILBService")
	l4ILBGlobalAccess = feature("L4ILBGlobalAccess")
	l4ILBCustomSubnet = feature("L4ILBCustomSubnet")
	// l4ILBInSuccess feature specifies that ILB VIP is configured.
	l4ILBInSuccess = feature("L4ILBInSuccess")
	// l4ILBInInError feature specifies that an error had occurred while creating/
	// updating GCE Load Balancer.
	l4ILBInError = feature("L4ILBInError")

	// PSC Features
	sa          = feature("ServiceAttachments")
	saInSuccess = feature("ServiceAttachmentInSuccess")
	saInError   = feature("ServiceAttachmentInError")
)

// featuresForIngress returns the list of features for given ingress.
func featuresForIngress(ing *v1.Ingress, fc *frontendconfigv1beta1.FrontendConfig) []feature {
	features := []feature{ingress}

	ingKey := fmt.Sprintf("%s/%s", ing.Namespace, ing.Name)
	klog.V(4).Infof("Listing features for Ingress %s", ingKey)
	ingAnnotations := ing.Annotations

	// Determine the type of ingress based on ingress class.
	ingClass := ingAnnotations[ingressClassKey]
	klog.V(6).Infof("Ingress class value for ingress %s: %s", ingKey, ingClass)
	switch ingClass {
	case "", gceIngressClass, gceMultiIngressClass:
		features = append(features, externalIngress)
	case gceL7ILBIngressClass:
		features = append(features, internalIngress)
	}

	// Determine if http is enabled.
	if val, ok := ingAnnotations[allowHTTPKey]; !ok {
		klog.V(6).Infof("Annotation %s does not exist for ingress %s", allowHTTPKey, ingKey)
		features = append(features, httpEnabled)
	} else {
		klog.V(6).Infof("User specified value for annotation %s on ingress %s: %s", allowHTTPKey, ingKey, val)
		v, err := strconv.ParseBool(val)
		if err != nil {
			klog.Errorf("Failed to parse %s for annotation %s on ingress %s", val, allowHTTPKey, ingKey)
		}
		if err == nil && v {
			features = append(features, httpEnabled)
		}
	}

	// An ingress without a host or http-path is ignored.
	hostBased, pathBased := false, false
	if len(ing.Spec.Rules) == 0 {
		klog.V(6).Infof("Neither host-based nor path-based routing rules are setup for ingress %s", ingKey)
	}
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP != nil && len(rule.HTTP.Paths) > 0 {
			klog.V(6).Infof("User specified http paths for ingress %s: %v", ingKey, rule.HTTP.Paths)
			pathBased = true
		}
		if rule.Host != "" {
			klog.V(6).Infof("User specified host for ingress %s: %v", ingKey, rule.Host)
			hostBased = true
		}
		if pathBased && hostBased {
			break
		}
	}
	if hostBased {
		features = append(features, hostBasedRouting)
	}
	if pathBased {
		features = append(features, pathBasedRouting)
	}

	// SSL certificate based features.
	// If user specifies a SSL certificate and controller does not add SSL certificate
	// annotation on ingress, then SSL cert is invalid in some sense. Ignore reporting
	// these SSL certificates.
	if cert, ok := ingAnnotations[SSLCertKey]; ok {
		klog.V(6).Infof("Configured SSL certificate for ingress %s: %v", ingKey, cert)
		features = append(features, tlsTermination)

		certSpecified := false
		if val, ok := ingAnnotations[preSharedCertKey]; ok {
			klog.V(6).Infof("Specified pre-shared certs for ingress %s: %v", ingKey, val)
			certSpecified = true
			features = append(features, preSharedCertsForTLS)
		}
		if val, ok := ingAnnotations[managedCertKey]; ok {
			klog.V(6).Infof("Specified google managed certs for ingress %s: %v", ingKey, val)
			certSpecified = true
			features = append(features, managedCertsForTLS)
		}
		if hasSecretBasedCerts(ing) {
			certSpecified = true
			features = append(features, secretBasedCertsForTLS)
		}
		if !certSpecified {
			klog.Errorf("Unexpected TLS termination(cert: %s) for ingress %s", cert, ingKey)
		}
	}

	// Both user specified and ingress controller managed global static ips are reported.
	if val, ok := ingAnnotations[staticIPKey]; ok && val != "" {
		klog.V(6).Infof("Static IP for ingress %s: %s", ingKey, val)
		features = append(features, staticGlobalIP)
		// Check if user specified static ip annotation exists.
		if val, ok = ingAnnotations[StaticGlobalIPNameKey]; ok {
			klog.V(6).Infof("User specified static IP for ingress %s: %s", ingKey, val)
			features = append(features, specifiedStaticGlobalIP)
		} else {
			features = append(features, managedStaticGlobalIP)
		}
	}

	// Check for regional static IP
	// We do this separately from the global static IP because Controller does not
	// populate StaticIPKey annotation when processing Regional static IP.
	if val, ok := ingAnnotations[annotations.RegionalStaticIPNameKey]; ok && val != "" {
		features = append(features, specifiedStaticRegionalIP)
	}

	// FrontendConfig Features
	if fc != nil {
		if fc.Spec.SslPolicy != nil && *fc.Spec.SslPolicy != "" {
			features = append(features, sslPolicy)
		}
		if fc.Spec.RedirectToHttps != nil && fc.Spec.RedirectToHttps.Enabled {
			features = append(features, httpsRedirects)
		}
	}

	klog.V(4).Infof("Features for ingress %s: %v", ingKey, features)
	return features
}

// hasSecretBasedCerts returns true if ingress spec contains a secret based cert.
func hasSecretBasedCerts(ing *v1.Ingress) bool {
	for _, tlsSecret := range ing.Spec.TLS {
		if tlsSecret.SecretName == "" {
			continue
		}
		klog.V(6).Infof("User specified secret for ingress %s/%s: %s", ing.Namespace, ing.Name, tlsSecret.SecretName)
		return true
	}
	return false
}

// featuresForServicePort returns the list of features for given service port.
func featuresForServicePort(sp utils.ServicePort) []feature {
	features := []feature{servicePort}
	svcPortKey := newServicePortKey(sp).string()
	klog.V(4).Infof("Listing features for service port %s", svcPortKey)
	if sp.L7ILBEnabled {
		klog.V(6).Infof("L7 ILB is enabled for service port %s", svcPortKey)
		features = append(features, internalServicePort)
	} else {
		features = append(features, externalServicePort)
	}
	if sp.NEGEnabled {
		klog.V(6).Infof("NEG is enabled for service port %s", svcPortKey)
		features = append(features, neg)
	}
	if sp.BackendConfig == nil {
		klog.V(4).Infof("Features for Service port %s: %v", svcPortKey, features)
		return features
	}

	beConfig := fmt.Sprintf("%s/%s", sp.BackendConfig.Namespace, sp.BackendConfig.Name)
	klog.V(6).Infof("Backend config specified for service port %s: %s", svcPortKey, beConfig)

	if sp.BackendConfig.Spec.Cdn != nil && sp.BackendConfig.Spec.Cdn.Enabled {
		klog.V(6).Infof("Cloud CDN is enabled for service port %s", svcPortKey)
		features = append(features, cloudCDN)
	}
	if sp.BackendConfig.Spec.Iap != nil && sp.BackendConfig.Spec.Iap.Enabled {
		klog.V(6).Infof("Cloud IAP is enabled for service port %s", svcPortKey)
		features = append(features, cloudIAP)
	}
	// Possible list of Affinity types:
	// NONE, CLIENT_IP, GENERATED_COOKIE, CLIENT_IP_PROTO, or CLIENT_IP_PORT_PROTO.
	if sp.BackendConfig.Spec.SessionAffinity != nil {
		affinityType := sp.BackendConfig.Spec.SessionAffinity.AffinityType
		switch affinityType {
		case "GENERATED_COOKIE":
			features = append(features, cookieAffinity)
		case "CLIENT_IP", "CLIENT_IP_PROTO", "CLIENT_IP_PORT_PROTO":
			features = append(features, clientIPAffinity)
		}
		klog.V(6).Infof("Session affinity %s is configured for service port %s", affinityType, svcPortKey)
	}
	if sp.BackendConfig.Spec.SecurityPolicy != nil {
		klog.V(6).Infof("Security policy %s is configured for service port %s", sp.BackendConfig.Spec.SecurityPolicy, svcPortKey)
		features = append(features, cloudArmor)
	}
	if sp.BackendConfig.Spec.TimeoutSec != nil {
		klog.V(6).Infof("Backend timeout(%v secs) is configured for service port %s", sp.BackendConfig.Spec.TimeoutSec, svcPortKey)
		features = append(features, backendTimeout)
	}
	if sp.BackendConfig.Spec.ConnectionDraining != nil {
		klog.V(6).Infof("Backend connection draining(%v secs) is configured for service port %s", sp.BackendConfig.Spec.ConnectionDraining.DrainingTimeoutSec, svcPortKey)
		features = append(features, backendConnectionDraining)
	}
	if sp.BackendConfig.Spec.CustomRequestHeaders != nil {
		klog.V(6).Infof("Custom request headers configured for service port %s: %v", svcPortKey, sp.BackendConfig.Spec.CustomRequestHeaders.Headers)
		features = append(features, customRequestHeaders)
	}
	if sp.BackendConfig.Spec.HealthCheck != nil {
		klog.V(6).Infof("Custom health check configured for service port %s: %v", svcPortKey, sp.BackendConfig.Spec.HealthCheck)
		features = append(features, customHealthChecks)
	}

	klog.V(4).Infof("Features for Service port %s: %v", svcPortKey, features)
	return features
}
