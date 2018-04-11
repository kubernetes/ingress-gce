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

package backends

import (
	"fmt"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/annotations"
)

// ServicePorts converts an IngressBackend -> Service mapping
// to a IngressBackend -> ServicePort mapping
func ServicePorts(backendToService map[v1beta1.IngressBackend]v1.Service) (map[v1beta1.IngressBackend]ServicePort, error) {
	backendToServicePort := make(map[v1beta1.IngressBackend]ServicePort)
	var result *multierror.Error

	for ib, svc := range backendToService {
		svcPort, err := servicePort(ib, svc)
		if err != nil {
			result = multierror.Append(result, err)
		}
		backendToServicePort[ib] = svcPort
	}
	return backendToServicePort, result.ErrorOrNil()
}

// servicePort looks at the port specified in the IngressBackend and returns
// the correct ServicePort.
func servicePort(ib v1beta1.IngressBackend, svc v1.Service) (ServicePort, error) {
	// If service is not of type NodePort, return an error.
	if svc.Spec.Type != v1.ServiceTypeNodePort {
		return ServicePort{}, fmt.Errorf("service %v is type %v, expected type NodePort", svc.Name, svc.Spec.Type)
	}
	appProtocols, err := annotations.FromService(&svc).ApplicationProtocols()
	if err != nil {
		return ServicePort{}, err
	}

	var port *v1.ServicePort
PortLoop:
	for _, p := range svc.Spec.Ports {
		np := p
		switch ib.ServicePort.Type {
		case intstr.Int:
			if p.Port == ib.ServicePort.IntVal {
				port = &np
				break PortLoop
			}
		default:
			if p.Name == ib.ServicePort.StrVal {
				port = &np
				break PortLoop
			}
		}
	}

	if port == nil {
		return ServicePort{}, fmt.Errorf("could not find matching port for backend %+v and service %s/%s. Looking for port %+v in %v", ib, svc.Namespace, ib.ServiceName, ib.ServicePort, svc.Spec.Ports)
	}

	proto := annotations.ProtocolHTTP
	if protoStr, exists := appProtocols[port.Name]; exists {
		glog.V(2).Infof("service %s/%s, port %q: using protocol to %q", svc.Namespace, ib.ServiceName, port, protoStr)
		proto = annotations.AppProtocol(protoStr)
	}

	p := ServicePort{
		NodePort: int64(port.NodePort),
		Protocol: proto,
		SvcName:  types.NamespacedName{Namespace: svc.Namespace, Name: ib.ServiceName},
		SvcPort:  ib.ServicePort,
	}
	return p, nil
}
