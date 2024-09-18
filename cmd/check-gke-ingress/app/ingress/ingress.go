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
	"fmt"
	"os"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/ingress-gce/cmd/check-gke-ingress/app/report"
	beconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	feconfigclient "k8s.io/ingress-gce/pkg/frontendconfig/client/clientset/versioned"
)

func CheckAllIngresses(namespace string, client kubernetes.Interface, beconfigClient beconfigclient.Interface, feConfigClient feconfigclient.Interface) report.Report {
	ingressList, err := client.NetworkingV1().Ingresses(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing ingresses: %v", err)
		os.Exit(1)
	}
	return RunChecks(ingressList.Items, client, beconfigClient, feConfigClient)
}

func CheckIngress(ingressName, namespace string, client kubernetes.Interface, beconfigClient beconfigclient.Interface, feConfigClient feconfigclient.Interface) report.Report {
	ingress, err := client.NetworkingV1().Ingresses(namespace).Get(context.TODO(), ingressName, metav1.GetOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting ingress %s/%s: %v", namespace, ingressName, err)
		os.Exit(1)
	}
	return RunChecks([]networkingv1.Ingress{*ingress}, client, beconfigClient, feConfigClient)
}

func RunChecks(ingresses []networkingv1.Ingress, client kubernetes.Interface, beconfigClient beconfigclient.Interface, feConfigClient feconfigclient.Interface) report.Report {
	output := report.Report{
		Resources: []*report.Resource{},
	}

	ingressChecks := []ingressCheckFunc{
		CheckIngressRule,
		CheckL7ILBFrontendConfig,
		CheckRuleHostOverwrite,
	}

	serviceChecks := []serviceCheckFunc{
		CheckServiceExistence,
		CheckBackendConfigAnnotation,
		CheckAppProtocolAnnotation,
		CheckL7ILBNegAnnotation,
	}

	feconfigChecks := []frontendConfigCheckFunc{CheckFrontendConfigExistence}

	beconfigChecks := []backendConfigCheckFunc{
		CheckBackendConfigExistence,
		CheckHealthCheckTimeout,
	}

	for _, ingress := range ingresses {

		// Ingress related checks
		ingressRes := &report.Resource{
			Kind:      "Ingress",
			Namespace: ingress.Namespace,
			Name:      ingress.Name,
			Checks:    []*report.Check{},
		}
		ingressChecker := &IngressChecker{
			ingress: &ingress,
		}

		for _, check := range ingressChecks {
			checkName, res, msg := check(ingressChecker)
			addCheckResult(ingressRes, checkName, msg, res)
		}

		feConfigName, ok := getFrontendConfigAnnotation(&ingress)
		if ok {
			// FrontendConfig related checks
			feconfigChecker := &FrontendConfigChecker{
				client:    feConfigClient,
				namespace: ingress.Namespace,
				name:      feConfigName,
			}

			for _, check := range feconfigChecks {
				checkName, res, msg := check(feconfigChecker)
				addCheckResult(ingressRes, checkName, msg, res)
			}
		}

		// Get the names of the services referenced by the ingress.
		svcNames := []string{}
		if ingress.Spec.DefaultBackend != nil {
			svcNames = append(svcNames, ingress.Spec.DefaultBackend.Service.Name)
		}
		if ingress.Spec.Rules != nil {
			for _, rule := range ingress.Spec.Rules {
				if rule.HTTP != nil {
					for _, path := range rule.HTTP.Paths {
						if path.Backend.Service != nil {
							svcNames = append(svcNames, path.Backend.Service.Name)
						}
					}
				}
			}
		}

		// Service related checks
		for _, svcName := range svcNames {
			serviceChecker := &ServiceChecker{
				namespace: ingress.Namespace,
				name:      svcName,
				isL7ILB:   isL7ILB(&ingress),
				client:    client,
			}

			for _, check := range serviceChecks {
				checkName, res, msg := check(serviceChecker)
				addCheckResult(ingressRes, checkName, msg, res)
			}

			// Get all the BackendConfigs referenced by the service.
			beconfigNames := []string{}
			if serviceChecker.beConfigs != nil {
				if serviceChecker.beConfigs.Default != "" {
					beconfigNames = append(beconfigNames, serviceChecker.beConfigs.Default)
				}
				for _, beconfig := range serviceChecker.beConfigs.Ports {
					beconfigNames = append(beconfigNames, beconfig)
				}
			}
			// BackendConfig related rules
			for _, beconfigName := range beconfigNames {
				beconfigChecker := &BackendConfigChecker{
					namespace:   ingress.Namespace,
					name:        beconfigName,
					client:      beconfigClient,
					serviceName: svcName,
				}

				for _, check := range beconfigChecks {
					checkName, res, msg := check(beconfigChecker)
					addCheckResult(ingressRes, checkName, msg, res)
				}
			}
		}
		output.Resources = append(output.Resources, ingressRes)
	}

	return output
}

func addCheckResult(ingressRes *report.Resource, checkName, msg, res string) {
	ingressRes.Checks = append(ingressRes.Checks, &report.Check{
		Name:    checkName,
		Message: msg,
		Result:  res,
	})
}
