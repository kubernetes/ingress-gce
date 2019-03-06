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
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog"
)

const (
	echoheadersImage = "gcr.io/k8s-ingress-image-push/ingress-gce-echo-amd64:master"
)

// CreateEchoService creates the pod and service serving echoheaders.
func CreateEchoService(s *Sandbox, name string, annotations map[string]string) (*v1.Pod, *v1.Service, error) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"app": name},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "web",
					Image: echoheadersImage,
					Ports: []v1.ContainerPort{{ContainerPort: 8080, Name: "web"}},
				},
			},
		},
	}
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{{
				Name:       "web",
				Protocol:   v1.ProtocolTCP,
				Port:       80,
				TargetPort: intstr.FromString("web"),
			}},
			Selector: map[string]string{"app": name},
			Type:     v1.ServiceTypeNodePort,
		},
	}
	var err error
	if pod, err = s.f.Clientset.Core().Pods(s.Namespace).Create(pod); err != nil {
		return nil, nil, err
	}
	if service, err = s.f.Clientset.Core().Services(s.Namespace).Create(service); err != nil {
		return nil, nil, err
	}
	klog.V(2).Infof("Echo service %q:%q created", s.Namespace, name)

	return pod, service, nil
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
	if secret, err = s.f.Clientset.Core().Secrets(s.Namespace).Create(secret); err != nil {
		return nil, err
	}
	klog.V(2).Infof("Secret %q:%q created", s.Namespace, name)

	return secret, nil
}
