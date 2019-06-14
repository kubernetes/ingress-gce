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

package utils

import (
	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"fmt"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
)

// ServicePortID contains the Service and Port fields.
type ServicePortID struct {
	Service types.NamespacedName
	Port    intstr.IntOrString
}

func (id ServicePortID) String() string {
	return fmt.Sprintf("%v/%v", id.Service.String(), id.Port.String())
}

// ServicePort maintains configuration for a single backend.
type ServicePort struct {
	// Ingress backend-specified service name and port
	ID ServicePortID

	NodePort int64
	// Numerical port of the Service, retrieved from the Service
	Port          int32
	Protocol      annotations.AppProtocol
	TargetPort    string
	NEGEnabled    bool
	ILBEnabled    bool
	BackendConfig *backendconfigv1beta1.BackendConfig
}

// GetDescription returns a Description for this ServicePort.
func (sp ServicePort) GetDescription() Description {
	return Description{
		ServiceName: sp.ID.Service.String(),
		ServicePort: sp.ID.Port.String(),
	}
}

// BackendName returns the name of the backend which would be used for this ServicePort.
func (sp ServicePort) BackendName(namer *Namer) string {
	if !sp.NEGEnabled {
		return namer.IGBackend(sp.NodePort)
	}

	return namer.NEG(sp.ID.Service.Namespace, sp.ID.Service.Name, sp.Port)
}

// BackendToServicePortID creates a ServicePortID from a given IngressBackend and namespace.
func BackendToServicePortID(be extensions.IngressBackend, namespace string) ServicePortID {
	return ServicePortID{
		Service: types.NamespacedName{
			Name:      be.ServiceName,
			Namespace: namespace,
		},
		Port: be.ServicePort,
	}
}

// NewServicePortWithID returns a ServicePort with only ID.
func NewServicePortWithID(svcName, svcNamespace string, port intstr.IntOrString) ServicePort {
	return ServicePort{
		ID: ServicePortID{
			Service: types.NamespacedName{
				Name:      svcName,
				Namespace: svcNamespace,
			},
			Port: port,
		},
	}
}
