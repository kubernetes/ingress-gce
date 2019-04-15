/*
Copyright 2018 The Kubernetes Authors.

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

package e2e

// Put common test fixtures (e.g. resources to be created) here.

import (
	"context"
	"fmt"
	"reflect"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	compute "google.golang.org/api/compute/v1"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog"
)

const (
	echoheadersImage = "gcr.io/k8s-ingress-image-push/ingress-gce-echo-amd64:master"
)

// CreateEchoService creates the pod and service serving echoheaders
// Todo: (shance) remove this and replace uses with EnsureEchoService()
func CreateEchoService(s *Sandbox, name string, annotations map[string]string) (*v1.Service, error) {
	return EnsureEchoService(s, name, annotations, v1.ServiceTypeNodePort, 1)
}

// Ensures that the Echo service with the given description is set up
func EnsureEchoService(s *Sandbox, name string, annotations map[string]string, svcType v1.ServiceType, numReplicas int32) (*v1.Service, error) {
	expectedSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:       "http-port",
					Protocol:   v1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromInt(8080),
				},
				{
					Name:       "https-port",
					Protocol:   v1.ProtocolTCP,
					Port:       443,
					TargetPort: intstr.FromInt(8443),
				},
			},
			Selector: map[string]string{"app": name},
			Type:     svcType,
		},
	}

	podTemplate := v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"app": name},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "echoheaders",
					Image: echoheadersImage,
					Ports: []v1.ContainerPort{
						{ContainerPort: 8080, Name: "http-port"},
						{ContainerPort: 8443, Name: "https-port"},
					},
				},
			},
		},
	}

	deployment := &v1beta1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1beta1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: podTemplate,
		},
	}

	scale := &v1beta1.Scale{
		Spec: v1beta1.ScaleSpec{
			Replicas: numReplicas,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: s.Namespace,
		},
	}

	svc, err := s.f.Clientset.CoreV1().Services(s.Namespace).Get(name, metav1.GetOptions{})

	if svc == nil || err != nil {
		if deployment, err = s.f.Clientset.ExtensionsV1beta1().Deployments(s.Namespace).Create(deployment); err != nil {
			return nil, err
		}
		if svc, err = s.f.Clientset.CoreV1().Services(s.Namespace).Create(expectedSvc); err != nil {
			return nil, err
		}
		return svc, err
	}

	if _, err = s.f.Clientset.ExtensionsV1beta1().Deployments(s.Namespace).UpdateScale(name, scale); err != nil {
		return nil, fmt.Errorf("Error updating deployment scale: %v", err)
	}

	if !reflect.DeepEqual(svc.Spec, expectedSvc.Spec) {
		// Update the fields individually since we don't want to override everything
		svc.ObjectMeta.Annotations = expectedSvc.ObjectMeta.Annotations
		svc.Spec.Ports = expectedSvc.Spec.Ports
		svc.Spec.Type = expectedSvc.Spec.Type

		if svc, err := s.f.Clientset.CoreV1().Services(s.Namespace).Update(svc); err != nil {
			return nil, fmt.Errorf("svc: %v\nexpectedSvc: %v\nerr: %v", svc, expectedSvc, err)
		}
	}

	return svc, nil
}

// CreateSecret creates a secret from the given data.
func CreateSecret(s *Sandbox, name string, data map[string][]byte) (*v1.Secret, error) {
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Data: data,
	}
	var err error
	if secret, err = s.f.Clientset.CoreV1().Secrets(s.Namespace).Create(secret); err != nil {
		return nil, err
	}
	klog.V(2).Infof("Secret %q:%q created", s.Namespace, name)

	return secret, nil
}

// DeleteSecret deletes a secret.
func DeleteSecret(s *Sandbox, name string) error {
	if err := s.f.Clientset.CoreV1().Secrets(s.Namespace).Delete(name, &metav1.DeleteOptions{}); err != nil {
		return err
	}
	klog.V(2).Infof("Secret %q:%q created", s.Namespace, name)

	return nil
}

// EnsureIngress creates a new Ingress or updates an existing one.
func EnsureIngress(s *Sandbox, ing *v1beta1.Ingress) (*v1beta1.Ingress, error) {
	currentIng, err := s.f.Clientset.ExtensionsV1beta1().Ingresses(s.Namespace).Get(ing.ObjectMeta.Name, metav1.GetOptions{})

	if currentIng == nil || err != nil {
		return s.f.Clientset.ExtensionsV1beta1().Ingresses(s.Namespace).Create(ing)
	}

	// Update ingress spec if they are not equal
	if !reflect.DeepEqual(ing.Spec, currentIng.Spec) {
		return s.f.Clientset.ExtensionsV1beta1().Ingresses(s.Namespace).Update(ing)
	}

	return currentIng, nil
}

// NewGCPAddress reserves a global static IP address with the given name.
func NewGCPAddress(s *Sandbox, name string) error {
	addr := &compute.Address{Name: name}
	if err := s.f.Cloud.GlobalAddresses().Insert(context.Background(), meta.GlobalKey(addr.Name), addr); err != nil {
		return err
	}
	klog.V(2).Infof("Global static IP %s created", name)

	return nil
}

// DeleteGCPAddress deletes a global static IP address with the given name.
func DeleteGCPAddress(s *Sandbox, name string) error {
	if err := s.f.Cloud.GlobalAddresses().Delete(context.Background(), meta.GlobalKey(name)); err != nil {
		return err
	}
	klog.V(2).Infof("Global static IP %s deleted", name)

	return nil
}
