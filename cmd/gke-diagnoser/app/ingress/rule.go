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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/ingress-gce/cmd/gke-diagnoser/app/report"
)

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
