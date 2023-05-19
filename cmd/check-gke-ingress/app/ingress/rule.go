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

// CheckServiceExistence checks whether a service exists.
func CheckServiceExistence(namespace, name string, client clientset.Interface) (*corev1.Service, string, string) {
	svc, err := client.CoreV1().Services(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, report.Failed, fmt.Sprintf("Service %s/%s does not exist", namespace, name)
		}
		return nil, report.Failed, fmt.Sprintf("Failed to get service %s/%s: %v", namespace, name, err)
	}
	return svc, report.Passed, fmt.Sprintf("Service %s/%s found", namespace, name)
}

// CheckBackendConfigAnnotation checks the BackendConfig annotation of a
// service for:
//
//	whether the annotation is a valid BackendConfig json object.
//	whether the annotation has `default` or `ports` field.
func CheckBackendConfigAnnotation(svc *corev1.Service) (*annotations.BackendConfigs, string, string) {
	val, ok := getBackendConfigAnnotation(svc)
	if !ok {
		return nil, report.Skipped, fmt.Sprintf("Service %s/%s does not have backendconfig annotation", svc.Namespace, svc.Name)
	}
	beConfigs := &annotations.BackendConfigs{}
	if err := json.Unmarshal([]byte(val), beConfigs); err != nil {
		return nil, report.Failed, fmt.Sprintf("BackendConfig annotation is invalid in service %s/%s", svc.Namespace, svc.Name)
	}
	if beConfigs.Default == "" && beConfigs.Ports == nil {
		return nil, report.Failed, fmt.Sprintf("BackendConfig annotation is missing both `default` and `ports` field in service %s/%s", svc.Namespace, svc.Name)
	}
	return beConfigs, report.Passed, fmt.Sprintf("BackendConfig annotation is valid in service %s/%s", svc.Namespace, svc.Name)
}

// CheckBackendConfigExistence checks whether a BackendConfig exists.
func CheckBackendConfigExistence(ns string, beConfigName string, svcName string, client beconfigclient.Interface) (*beconfigv1.BackendConfig, string, string) {
	beConfig, err := client.CloudV1().BackendConfigs(ns).Get(context.TODO(), beConfigName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, report.Failed, fmt.Sprintf("BackendConfig %s/%s in service %s/%s does not exist", ns, beConfigName, ns, svcName)
		}
		return nil, report.Failed, fmt.Sprintf("Failed to get backendConfig %s/%s in service %s/%s", ns, beConfigName, ns, svcName)
	}
	return beConfig, report.Passed, fmt.Sprintf("BackendConfig %s/%s in service %s/%s found", ns, beConfigName, ns, svcName)
}

// CheckHealthCheckTimeout checks whether timeout time is smaller than check
// interval in backendconfig health check configuration.
func CheckHealthCheckTimeout(beConfig *beconfigv1.BackendConfig, svcName string) (string, string) {
	if beConfig.Spec.HealthCheck == nil {
		return report.Skipped, fmt.Sprintf("BackendConfig %s/%s in service %s/%s  does not have healthcheck specified", beConfig.Namespace, beConfig.Name, beConfig.Namespace, svcName)
	}
	if beConfig.Spec.HealthCheck.TimeoutSec == nil || beConfig.Spec.HealthCheck.CheckIntervalSec == nil {
		return report.Skipped, fmt.Sprintf("BackendConfig %s/%s in service %s/%s does not have timeoutSec or checkIntervalSec specified", beConfig.Namespace, beConfig.Name, beConfig.Namespace, svcName)
	}
	if *beConfig.Spec.HealthCheck.TimeoutSec > *beConfig.Spec.HealthCheck.CheckIntervalSec {
		return report.Failed, fmt.Sprintf("BackendConfig %s/%s in service %s/%s has healthcheck timeoutSec greater than checkIntervalSec", beConfig.Namespace, beConfig.Name, beConfig.Namespace, svcName)
	}
	return report.Passed, fmt.Sprintf("BackendConfig %s/%s in service %s/%s healthcheck configuration is valid", beConfig.Namespace, beConfig.Name, beConfig.Namespace, svcName)
}

// CheckIngressRule checks whether an ingress rule has the http field.
func CheckIngressRule(ingressRule *networkingv1.IngressRule) (*networkingv1.HTTPIngressRuleValue, string, string) {
	if ingressRule.HTTP == nil {
		return nil, report.Failed, "IngressRule has no field `http`"
	}
	return ingressRule.HTTP, report.Passed, "IngressRule has field `http`"
}

// CheckFrontendConfigExistence checks whether a FrontendConfig exists.
func CheckFrontendConfigExistence(namespace, name string, client feconfigclient.Interface) (*feconfigv1beta1.FrontendConfig, string, string) {
	feConfig, err := client.NetworkingV1beta1().FrontendConfigs(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, report.Failed, fmt.Sprintf("FrontendConfig %s/%s does not exist", namespace, name)
		}
		return nil, report.Failed, fmt.Sprintf("Failed to get frontendConfig %s/%s", namespace, name)
	}
	return feConfig, report.Passed, fmt.Sprintf("FrontendConfig %s/%s found", namespace, name)
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
