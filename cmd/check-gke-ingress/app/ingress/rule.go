/*
Copyright 2023 The Kubernetes Authors.

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

package ingress

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/ingress-gce/cmd/check-gke-ingress/app/report"
	"k8s.io/ingress-gce/pkg/annotations"
	beconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	feconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	beconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	feconfigclient "k8s.io/ingress-gce/pkg/frontendconfig/client/clientset/versioned"
)

const (
	ServiceExistenceCheck        = "ServiceExistenceCheck"
	BackendConfigAnnotationCheck = "BackendConfigAnnotationCheck"
	BackendConfigExistenceCheck  = "BackendConfigExistenceCheck"
	HealthCheckTimeoutCheck      = "HealthCheckTimeoutCheck"
	IngressRuleCheck             = "IngressRuleCheck"
	FrontendConfigExistenceCheck = "FrontendConfigExistenceCheck"
	RuleHostOverwriteCheck       = "RuleHostOverwriteCheck"
	AppProtocolAnnotationCheck   = "AppProtocolAnnotationCheck"
	L7ILBFrontendConfigCheck     = "L7ILBFrontendConfigCheck"
	L7ILBNegAnnotationCheck      = "L7ILBNegAnnotationCheck"
)

type IngressChecker struct {
	// Kubernetes client
	client clientset.Interface
	// Ingress object to be checked
	ingress *networkingv1.Ingress
}

type ServiceChecker struct {
	// Kubernetes client
	client clientset.Interface
	// Service object to be checked
	service *corev1.Service
	// Backendconfig annotatation in the service
	beConfigs *annotations.BackendConfigs
	// Namespace of the service
	namespace string
	// Name of the service
	name string
	// Whether the service is referenced by an internal ingress
	isL7ILB bool
}

type BackendConfigChecker struct {
	// BackendConfig client
	client beconfigclient.Interface
	// Namespace of the backendConfig
	namespace string
	// Name of the backendConfig
	name string
	// Backendconfig object to be checked
	beConfig *beconfigv1.BackendConfig
	// Name of the service by which the backendConfig is referenced
	serviceName string
}

type FrontendConfigChecker struct {
	// FrontendConfig client
	client feconfigclient.Interface
	// Namespace of the frontendConfig
	namespace string
	// Name of the frontendConfig
	name string
	// FrontendConfig object to be checked
	feConfig *feconfigv1beta1.FrontendConfig
}

type ingressCheckFunc func(c *IngressChecker) (string, string, string)

type serviceCheckFunc func(c *ServiceChecker) (string, string, string)

type backendConfigCheckFunc func(c *BackendConfigChecker) (string, string, string)

type frontendConfigCheckFunc func(c *FrontendConfigChecker) (string, string, string)

// CheckIngressRule checks whether an ingress rule has the http field.
func CheckIngressRule(c *IngressChecker) (string, string, string) {
	for _, rule := range c.ingress.Spec.Rules {
		if rule.HTTP == nil {
			return IngressRuleCheck, report.Failed, "IngressRule has no field `http`"
		}
	}
	return IngressRuleCheck, report.Passed, "IngressRule has field `http`"
}

// CheckL7ILBFrontendConfig checks whether an internal ingress has a
// frontendConfig. It will fail if an internal ingress has a frontendConfig.
func CheckL7ILBFrontendConfig(c *IngressChecker) (string, string, string) {
	if !isL7ILB(c.ingress) {
		return L7ILBFrontendConfigCheck, report.Skipped, fmt.Sprintf("Ingress %s/%s is not for L7 internal load balancing", c.ingress.Namespace, c.ingress.Name)
	}
	if _, ok := getFrontendConfigAnnotation(c.ingress); ok {
		return L7ILBFrontendConfigCheck, report.Failed, fmt.Sprintf("Ingress %s/%s for L7 internal load balancing has a frontendConfig annotation", c.ingress.Namespace, c.ingress.Name)
	}
	return L7ILBFrontendConfigCheck, report.Passed, fmt.Sprintf("Ingress %s/%s for L7 internal load balancing does not have a frontendConfig annotation", c.ingress.Namespace, c.ingress.Name)
}

// CheckRuleHostOverwrite checks whether hosts of ingress rules are unique.
func CheckRuleHostOverwrite(c *IngressChecker) (string, string, string) {
	hostSet := make(map[string]struct{})
	for _, rule := range c.ingress.Spec.Rules {
		if _, ok := hostSet[rule.Host]; ok {
			return RuleHostOverwriteCheck, report.Failed, fmt.Sprintf("Ingress rules have identical host: %s", rule.Host)
		}
		hostSet[rule.Host] = struct{}{}
	}
	return RuleHostOverwriteCheck, report.Passed, fmt.Sprintf("Ingress rule hosts are unique")
}

// CheckServiceExistence checks whether a service exists.
func CheckServiceExistence(c *ServiceChecker) (string, string, string) {
	service, err := c.client.CoreV1().Services(c.namespace).Get(context.TODO(), c.name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ServiceExistenceCheck, report.Failed, fmt.Sprintf("Service %s/%s does not exist", c.namespace, c.name)
		}
		return ServiceExistenceCheck, report.Failed, fmt.Sprintf("Failed to get service %s/%s: %v", c.namespace, c.name, err)
	}
	c.service = service
	return ServiceExistenceCheck, report.Passed, fmt.Sprintf("Service %s/%s found", c.namespace, c.name)
}

// CheckBackendConfigAnnotation checks the BackendConfig annotation of a
// service for:
//
//	whether the annotation is a valid BackendConfig json object.
//	whether the annotation has `default` or `ports` field.
func CheckBackendConfigAnnotation(c *ServiceChecker) (string, string, string) {
	if c.service == nil {
		return BackendConfigAnnotationCheck, report.Skipped, fmt.Sprintf("Service %s/%s does not exist", c.namespace, c.name)
	}
	val, ok := getBackendConfigAnnotation(c.service)
	if !ok {
		return BackendConfigAnnotationCheck, report.Skipped, fmt.Sprintf("Service %s/%s does not have backendconfig annotation", c.namespace, c.name)
	}
	beConfigs := &annotations.BackendConfigs{}
	if err := json.Unmarshal([]byte(val), beConfigs); err != nil {
		return BackendConfigAnnotationCheck, report.Failed, fmt.Sprintf("BackendConfig annotation is invalid in service %s/%s", c.namespace, c.name)
	}
	if beConfigs.Default == "" && beConfigs.Ports == nil {
		return BackendConfigAnnotationCheck, report.Failed, fmt.Sprintf("BackendConfig annotation is missing both `default` and `ports` field in service %s/%s", c.namespace, c.name)
	}
	c.beConfigs = beConfigs
	return BackendConfigAnnotationCheck, report.Passed, fmt.Sprintf("BackendConfig annotation is valid in service %s/%s", c.namespace, c.name)
}

// CheckAppProtocolAnnotation check whether the protocal annotation specified
// in a service is in valid format and with valid protocols.
func CheckAppProtocolAnnotation(c *ServiceChecker) (string, string, string) {
	if c.service == nil {
		return AppProtocolAnnotationCheck, report.Skipped, fmt.Sprintf("Service %s/%s does not exist", c.namespace, c.name)
	}
	val, ok := getAppProtocolsAnnotation(c.service)
	if !ok {
		return AppProtocolAnnotationCheck, report.Skipped, fmt.Sprintf("Service %s/%s does not have AppProtocolAnnotation", c.namespace, c.name)
	}
	var portToProtocols map[string]annotations.AppProtocol
	if err := json.Unmarshal([]byte(val), &portToProtocols); err != nil {
		return AppProtocolAnnotationCheck, report.Failed, fmt.Sprintf("AppProtocol annotation is in invalid format in service %s/%s", c.namespace, c.name)
	}
	for _, protocol := range portToProtocols {
		if protocol != annotations.ProtocolHTTP && protocol != annotations.ProtocolHTTPS && protocol != annotations.ProtocolHTTP2 {
			return AppProtocolAnnotationCheck, report.Failed, fmt.Sprintf("Invalid port application protocol in service %s/%s: %v, must be one of [`HTTP`,`HTTPS`,`HTTP2`]", c.namespace, c.name, protocol)
		}
	}
	return AppProtocolAnnotationCheck, report.Passed, fmt.Sprintf("AppProtocol annotation is valid in service %s/%s", c.namespace, c.name)
}

// CheckL7ILBNegAnnotation check whether a service which belongs to an internal
// ingress has a correct NEG annotation.
func CheckL7ILBNegAnnotation(c *ServiceChecker) (string, string, string) {
	if c.service == nil {
		return L7ILBNegAnnotationCheck, report.Skipped, fmt.Sprintf("Service %s/%s does not exist", c.namespace, c.name)
	}
	if !c.isL7ILB {
		return L7ILBNegAnnotationCheck, report.Skipped, fmt.Sprintf("Service %s/%s is not referenced by an internal ingress", c.namespace, c.name)
	}
	val, ok := getNegAnnotation(c.service)
	if !ok {
		return L7ILBNegAnnotationCheck, report.Failed, fmt.Sprintf("No Neg annotation found in service %s/%s for internal HTTP(S) load balancing", c.namespace, c.name)
	}
	var res annotations.NegAnnotation
	if err := json.Unmarshal([]byte(val), &res); err != nil {
		return L7ILBNegAnnotationCheck, report.Failed, fmt.Sprintf("Invalid Neg annotation found in service %s/%s for internal HTTP(S) load balancing", c.namespace, c.name)
	}
	if !res.Ingress {
		return L7ILBNegAnnotationCheck, report.Failed, fmt.Sprintf("Neg annotation ingress field is not true in service %s/%s for internal HTTP(S) load balancing", c.namespace, c.name)
	}
	return L7ILBNegAnnotationCheck, report.Passed, fmt.Sprintf("Neg annotation is set correctly in service %s/%s for internal HTTP(S) load balancing", c.namespace, c.name)
}

// CheckBackendConfigExistence checks whether a BackendConfig exists.
func CheckBackendConfigExistence(c *BackendConfigChecker) (string, string, string) {
	beConfig, err := c.client.CloudV1().BackendConfigs(c.namespace).Get(context.TODO(), c.name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return BackendConfigExistenceCheck, report.Failed, fmt.Sprintf("BackendConfig %s/%s referenced by service %s/%s does not exist", c.namespace, c.name, c.namespace, c.serviceName)
		}
		return BackendConfigExistenceCheck, report.Failed, fmt.Sprintf("Failed to get backendConfig %s/%s referenced by service %s/%s", c.namespace, c.name, c.namespace, c.serviceName)
	}
	c.beConfig = beConfig
	return BackendConfigExistenceCheck, report.Passed, fmt.Sprintf("Found backendConfig %s/%s referenced by service %s/%s", c.namespace, c.name, c.namespace, c.serviceName)
}

// CheckHealthCheckTimeout checks whether timeout time is smaller than check
// interval in backendconfig health check configuration.
func CheckHealthCheckTimeout(c *BackendConfigChecker) (string, string, string) {
	if c.beConfig == nil {
		return HealthCheckTimeoutCheck, report.Skipped, fmt.Sprintf("BackendConfig %s/%s does not exist", c.namespace, c.name)
	}
	if c.beConfig.Spec.HealthCheck == nil {
		return HealthCheckTimeoutCheck, report.Skipped, fmt.Sprintf("BackendConfig %s/%s does not have healthcheck specified", c.namespace, c.name)
	}
	if c.beConfig.Spec.HealthCheck.TimeoutSec == nil || c.beConfig.Spec.HealthCheck.CheckIntervalSec == nil {
		return HealthCheckTimeoutCheck, report.Skipped, fmt.Sprintf("BackendConfig %s/%s does not have timeoutSec or checkIntervalSec specified", c.namespace, c.name)
	}
	if *c.beConfig.Spec.HealthCheck.TimeoutSec > *c.beConfig.Spec.HealthCheck.CheckIntervalSec {
		return HealthCheckTimeoutCheck, report.Failed, fmt.Sprintf("BackendConfig %s/%s has healthcheck timeoutSec greater than checkIntervalSec", c.namespace, c.name)
	}
	return HealthCheckTimeoutCheck, report.Passed, fmt.Sprintf("BackendConfig %s/%s healthcheck configuration is valid", c.namespace, c.name)
}

// CheckFrontendConfigExistence checks whether a FrontendConfig exists.
func CheckFrontendConfigExistence(c *FrontendConfigChecker) (string, string, string) {
	feConfig, err := c.client.NetworkingV1beta1().FrontendConfigs(c.namespace).Get(context.TODO(), c.name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return FrontendConfigExistenceCheck, report.Failed, fmt.Sprintf("FrontendConfig %s/%s does not exist", c.namespace, c.name)
		}
		return FrontendConfigExistenceCheck, report.Failed, fmt.Sprintf("Failed to get frontendConfig %s/%s", c.namespace, c.name)
	}
	c.feConfig = feConfig
	return FrontendConfigExistenceCheck, report.Passed, fmt.Sprintf("FrontendConfig %s/%s found", c.namespace, c.name)
}

// getBackendConfigAnnotation gets the BackendConfig annotation from a service.
func getBackendConfigAnnotation(svc *corev1.Service) (string, bool) {
	for _, bcKey := range []string{annotations.BackendConfigKey, annotations.BetaBackendConfigKey} {
		val, ok := svc.Annotations[bcKey]
		if ok {
			return val, ok
		}
	}
	return "", false
}

// getAppProtocolsAnnotation gets the AppProtocols annotation from a service.
func getAppProtocolsAnnotation(svc *corev1.Service) (string, bool) {
	for _, key := range []string{annotations.ServiceApplicationProtocolKey, annotations.GoogleServiceApplicationProtocolKey} {
		val, ok := svc.Annotations[key]
		if ok {
			return val, true
		}
	}
	return "", false
}

// isL7ILB whether an ingress is for internal load balancing.
func isL7ILB(ing *networkingv1.Ingress) bool {
	val, ok := ing.Annotations[annotations.IngressClassKey]
	if !ok {
		return false
	}
	if val != annotations.GceL7ILBIngressClass {
		return false
	}
	return true
}

// getFrontendConfigAnnotation gets the frontendConfig annotation from an
// ingress object.
func getFrontendConfigAnnotation(ing *networkingv1.Ingress) (string, bool) {
	val, ok := ing.ObjectMeta.Annotations[annotations.FrontendConfigKey]
	if !ok {
		return "", false
	}
	return val, true
}

// getNegAnnotation gets the NEG annotation from a service object.
func getNegAnnotation(svc *corev1.Service) (string, bool) {
	val, ok := svc.Annotations[annotations.NEGAnnotationKey]
	if !ok {
		return "", false
	}
	return val, true
}
