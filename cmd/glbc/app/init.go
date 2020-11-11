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

package app

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

// DefaultBackendServicePort returns the ServicePort which will be
// used as the default backend for load balancers.
func DefaultBackendServicePort(kubeClient kubernetes.Interface) utils.ServicePort {
	if flags.F.DefaultSvc == "" {
		klog.Fatalf("Please specify --default-backend-service")
	}

	if flags.F.DefaultSvcPortName == "" {
		klog.Fatalf("Please specify --default-backend-service-port")
	}

	name, err := utils.ToNamespacedName(flags.F.DefaultSvc)
	if err != nil {
		klog.Fatalf("Failed to parse --default-backend-service: %v", err)
	}

	svc, err := waitForServicePort(kubeClient, name, flags.F.DefaultSvcPortName)
	if err != nil {
		klog.Fatalf("Failed to verify default backend service: %v", err)
	}

	backendPort := intstr.FromString(flags.F.DefaultSvcPortName)
	svcPort := servicePortForDefaultService(svc, backendPort, name)
	if svcPort == nil {
		klog.Fatalf("could not derive service port for default service: %v", err)
	}

	return *svcPort
}

// IngressClassEnabled returns whether the IngressClass API exists on the kubernetes cluster
func IngressClassEnabled(client kubernetes.Interface) bool {
	klog.V(2).Info("Checking if Ingress Class API exists")

	err := wait.Poll(3*time.Second, 5*time.Minute, func() (bool, error) {
		var err error
		_, err = client.NetworkingV1beta1().IngressClasses().List(context.TODO(), meta_v1.ListOptions{})
		if err == nil {
			klog.V(2).Info("Ingress Class API is supported")
			return true, nil
		}

		if apierrors.IsNotFound(err) {
			klog.V(2).Infof("Ingress Class API is not supported: %s", err)
			return true, err
		}
		klog.Errorf("Errored checking for Ingress Class API: %s", err)
		return false, nil
	})

	if err != nil {
		klog.V(2).Infof("Ingress Class support disabled. Received error while checking for Ingress Class API: %s", err)
		return false
	}

	klog.V(2).Info("Ingress Class support enabled")
	return true
}

// servicePortForDefaultService returns the service port for the default service; returns nil if not found.
func servicePortForDefaultService(svc *v1.Service, svcPort intstr.IntOrString, name types.NamespacedName) *utils.ServicePort {
	// Lookup TargetPort for service port
	for _, port := range svc.Spec.Ports {
		if port.Name == svcPort.String() {
			return &utils.ServicePort{
				ID: utils.ServicePortID{
					Service: name,
					Port:    svcPort,
				},
				TargetPort: port.TargetPort.StrVal,
				Port:       port.Port,
			}
		}
	}

	return nil
}

// servicePortExists checks that the service and specified port name exists.
func waitForServicePort(client kubernetes.Interface, name types.NamespacedName, portName string) (*v1.Service, error) {
	klog.V(2).Infof("Checking existence of default backend service %q", name.String())
	var svc *v1.Service

	err := wait.Poll(3*time.Second, 5*time.Minute, func() (bool, error) {
		var err error
		svc, err = client.CoreV1().Services(name.Namespace).Get(context.TODO(), name.Name, meta_v1.GetOptions{})
		if err != nil {
			klog.V(4).Infof("Error getting service %v", name.String())
			return false, nil
		}
		for _, p := range svc.Spec.Ports {
			if p.Name == portName {
				return true, nil
			}
		}
		return false, fmt.Errorf("port %q not found in service %q", portName, name.String())
	})

	return svc, err
}
