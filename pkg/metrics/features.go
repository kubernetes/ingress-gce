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
	"k8s.io/klog/v2"
)

type feature string

func (f feature) String() string {
	return string(f)
}

const (
	// WARNING: Please keep the following constants in sync with
	// pkg/annotations/ingress.go
	// allowHTTPKey tells the Ingress controller to allow/block HTTP access.
	allowHTTPKey                      = "kubernetes.io/ingress.allow-http"
	ingressClassKey                   = "kubernetes.io/ingress.class"
	gceIngressClass                   = "gce"
	gceMultiIngressClass              = "gce-multi-cluster"
	gceL7ILBIngressClass              = "gce-internal"
	gceL7RegionalExternalIngressClass = "gce-regional-external"
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
	regionalExternalIngress   = feature("RegionalExternalIngress")
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

	servicePort                 = feature("L7LBServicePort")
	externalServicePort         = feature("L7XLBServicePort")
	internalServicePort         = feature("L7ILBServicePort")
	regionalExternalServicePort = feature("L7RegionalXLBServicePort")
	neg                         = feature("NEG")

	// BackendConfig Features
	cloudCDN                  = feature("CloudCDN")
	cloudArmor                = feature("CloudArmor")
	cloudArmorSet             = feature("CloudArmorSet")
	cloudArmorEmpty           = feature("CloudArmorEmptyString")
	cloudArmorNil             = feature("CloudArmorNil")
	cloudIAP                  = feature("CloudIAP")
	backendTimeout            = feature("BackendTimeout")
	backendConnectionDraining = feature("BackendConnectionDraining")
	clientIPAffinity          = feature("ClientIPAffinity")
	cookieAffinity            = feature("CookieAffinity")
	customRequestHeaders      = feature("CustomRequestHeaders")
	customHealthChecks        = feature("CustomHealthChecks")
	cloudIAPCredentials       = feature("CloudIAPCredentials")
	cloudIAPEmpty             = feature("CloudIAPEmpty")

	// Transparent Health Checks feature
	transparentHealthChecks = feature("TransparentHC")

	// FrontendConfig Features
	sslPolicy      = feature("SSLPolicy")
	httpsRedirects = feature("HTTPSRedirects")

	// PSC Features
	sa          = feature("ServiceAttachments")
	saInSuccess = feature("ServiceAttachmentInSuccess")
	saInError   = feature("ServiceAttachmentInError")
	services    = feature("Services")
)

// featuresForIngress returns the list of features for given ingress.
func featuresForIngress(ing *v1.Ingress, fc *frontendconfigv1beta1.FrontendConfig, logger klog.Logger) []feature {
	features := []feature{ingress}

	ingKey := fmt.Sprintf("%s/%s", ing.Namespace, ing.Name)
	logger.V(4).Info("Listing features for Ingress", "ingressKey", ingKey)
	ingAnnotations := ing.Annotations

	// Determine the type of ingress based on ingress class.
	ingClass := ingAnnotations[ingressClassKey]
	logger.V(6).Info("Got Ingress class value for ingress", "ingressKey", ingKey, "ingressClass", ingClass)
	switch ingClass {
	case "", gceIngressClass, gceMultiIngressClass:
		features = append(features, externalIngress)
	case gceL7ILBIngressClass:
		features = append(features, internalIngress)
	case gceL7RegionalExternalIngressClass:
		features = append(features, regionalExternalIngress)
	}

	// Determine if http is enabled.
	if val, ok := ingAnnotations[allowHTTPKey]; !ok {
		logger.V(6).Info("Annotation does not exist for ingress", "annotation", allowHTTPKey, "ingressKey", ingKey)
		features = append(features, httpEnabled)
	} else {
		logger.V(6).Info("User specified value for annotation on ingress", "annotation", allowHTTPKey, "ingressKey", ingKey, "annotationValue", val)
		v, err := strconv.ParseBool(val)
		if err != nil {
			logger.Error(nil, "Failed to parse value for annotation on ingress", "annotationValue", val, "annotation", allowHTTPKey, "ingressKey", ingKey)
		}
		if err == nil && v {
			features = append(features, httpEnabled)
		}
	}

	// An ingress without a host or http-path is ignored.
	hostBased, pathBased := false, false
	if len(ing.Spec.Rules) == 0 {
		logger.V(6).Info("Neither host-based nor path-based routing rules are setup for ingress", "ingressKey", ingKey)
	}
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP != nil && len(rule.HTTP.Paths) > 0 {
			logger.V(6).Info("User specified http paths for ingress", "ingressKey", ingKey, "httpPaths", rule.HTTP.Paths)
			pathBased = true
		}
		if rule.Host != "" {
			logger.V(6).Info("User specified host for ingress", "ingressKey", ingKey, "host", rule.Host)
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
		logger.V(6).Info("Configured SSL certificate for ingress", "ingressKey", ingKey, "sslCert", cert)
		features = append(features, tlsTermination)

		certSpecified := false
		if val, ok := ingAnnotations[preSharedCertKey]; ok {
			logger.V(6).Info("Specified pre-shared certs for ingress", "ingressKey", ingKey, "preSharedCert", val)
			certSpecified = true
			features = append(features, preSharedCertsForTLS)
		}
		if val, ok := ingAnnotations[managedCertKey]; ok {
			logger.V(6).Info("Specified google managed certs for ingress", "ingressKey", ingKey, "managedCert", val)
			certSpecified = true
			features = append(features, managedCertsForTLS)
		}
		if hasSecretBasedCerts(ing, logger) {
			certSpecified = true
			features = append(features, secretBasedCertsForTLS)
		}
		if !certSpecified {
			logger.Error(nil, "Unexpected TLS termination for ingress", "cert", cert, "ingressKey", ingKey)
		}
	}

	// Both user specified and ingress controller managed global static ips are reported.
	if val, ok := ingAnnotations[staticIPKey]; ok && val != "" {
		logger.V(6).Info("Static IP for ingress", "ingressKey", ingKey, "staticIp", val)
		features = append(features, staticGlobalIP)
		// Check if user specified static ip annotation exists.
		if val, ok = ingAnnotations[StaticGlobalIPNameKey]; ok {
			logger.V(6).Info("User specified static IP for ingress", "ingressKey", ingKey, "userStaticIp", val)
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

	logger.V(4).Info("Features for ingress", "ingressKey", ingKey, "ingressFeatures", features)
	return features
}

// hasSecretBasedCerts returns true if ingress spec contains a secret based cert.
func hasSecretBasedCerts(ing *v1.Ingress, logger klog.Logger) bool {
	for _, tlsSecret := range ing.Spec.TLS {
		if tlsSecret.SecretName == "" {
			continue
		}
		logger.V(6).Info("User specified secret for ingress", "ingressKey", klog.KRef(ing.Namespace, ing.Name), "tlsSecret", tlsSecret.SecretName)
		return true
	}
	return false
}

// featuresForServicePort returns the list of features for given service port.
func featuresForServicePort(sp utils.ServicePort, logger klog.Logger) []feature {
	features := []feature{servicePort}
	svcPortKey := newServicePortKey(sp).string()
	logger.V(4).Info("Listing features for service port", "servicePortKey", svcPortKey)
	if sp.L7ILBEnabled {
		logger.V(6).Info("L7 ILB is enabled for service port", "servicePortKey", svcPortKey)
		features = append(features, internalServicePort)
	} else if sp.L7XLBRegionalEnabled {
		logger.V(6).Info("L7 Regional XLB is enabled for service port", "servicePortKey", svcPortKey)
		features = append(features, regionalExternalServicePort)
	} else {
		features = append(features, externalServicePort)
	}
	if sp.NEGEnabled {
		logger.V(6).Info("NEG is enabled for service port", "servicePortKey", svcPortKey)
		features = append(features, neg)
	}
	if sp.THCConfiguration.THCOptInOnSvc {
		logger.V(6).Info("Transparent Health Checks configured for service port", "servicePortKey", svcPortKey)
		features = append(features, transparentHealthChecks)
	}
	if sp.BackendConfig == nil {
		logger.V(4).Info("Features for Service port", "servicePortKey", svcPortKey, "features", features)
		return features
	}

	beConfig := fmt.Sprintf("%s/%s", sp.BackendConfig.Namespace, sp.BackendConfig.Name)
	logger.V(6).Info("Backend config specified for service port", "servicePortKey", svcPortKey, "backendConfig", beConfig)

	if sp.BackendConfig.Spec.Cdn != nil && sp.BackendConfig.Spec.Cdn.Enabled {
		logger.V(6).Info("Cloud CDN is enabled for service port", "servicePortKey", svcPortKey)
		features = append(features, cloudCDN)
	}
	if sp.BackendConfig.Spec.Iap != nil && sp.BackendConfig.Spec.Iap.Enabled {
		iapConfiguration := cloudIAPCredentials
		if sp.BackendConfig.Spec.Iap.OAuthClientCredentials == nil {
			iapConfiguration = cloudIAPEmpty
		}
		logger.V(6).Info("Cloud IAP is enabled for service port", "servicePortKey", svcPortKey, "IAPCredentials", iapConfiguration)
		features = append(features, cloudIAP, iapConfiguration)
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
		logger.V(6).Info("Session affinity is configured for service port", "affinityType", affinityType, "servicePortKey", svcPortKey)
	}

	if sp.BackendConfig.Spec.SecurityPolicy != nil {
		logger.V(6).Info("Security policy is configured for service port", "securityPolicy", sp.BackendConfig.Spec.SecurityPolicy, "servicePortKey", svcPortKey)
		features = append(features, cloudArmor)
	}

	// Detailed metrics about cloud armor.
	if sp.BackendConfig.Spec.SecurityPolicy != nil {
		var caFeature feature
		if sp.BackendConfig.Spec.SecurityPolicy.Name == "" {
			caFeature = cloudArmorEmpty
		} else {
			caFeature = cloudArmorSet
		}
		features = append(features, caFeature)
		logger.V(6).Info("Security policy is configured for service port (cloud armor feature)", "securityPolicy", sp.BackendConfig.Spec.SecurityPolicy, "servicePortKey", svcPortKey, "cloudArmorFeature", caFeature)
	} else {
		features = append(features, cloudArmorNil)
		logger.V(6).Info("Security policy is configured to nil for service port", "servicePortKey", svcPortKey)
	}

	if sp.BackendConfig.Spec.TimeoutSec != nil {
		logger.V(6).Info("Backend timeout is configured for service port", "backendTimeoutSeconds", sp.BackendConfig.Spec.TimeoutSec, "servicePortKey", svcPortKey)
		features = append(features, backendTimeout)
	}
	if sp.BackendConfig.Spec.ConnectionDraining != nil {
		logger.V(6).Info("Backend connection draining is configured for service port", "backendConnectionDrainingSeconds", sp.BackendConfig.Spec.ConnectionDraining.DrainingTimeoutSec, "servicePortKey", svcPortKey)
		features = append(features, backendConnectionDraining)
	}
	if sp.BackendConfig.Spec.CustomRequestHeaders != nil {
		logger.V(6).Info("Custom request headers configured for service port", "servicePortKey", svcPortKey, "requestHeaders", sp.BackendConfig.Spec.CustomRequestHeaders.Headers)
		features = append(features, customRequestHeaders)
	}
	if sp.BackendConfig.Spec.HealthCheck != nil {
		logger.V(6).Info("Custom health check configured for service port", "servicePortKey", svcPortKey, "healthCheck", sp.BackendConfig.Spec.HealthCheck)
		features = append(features, customHealthChecks)
	}

	logger.V(4).Info("Features for Service port", "servicePortKey", svcPortKey, "features", features)
	return features
}
