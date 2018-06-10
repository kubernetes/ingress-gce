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

package fuzz

import (
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// NewService is a helper function for creating a simple Service spec.
func NewService(name, ns string, port int) *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Port:       int32(port),
					TargetPort: intstr.FromInt(port),
				},
			},
		},
	}
}

// HostPath maps an entry in Ingress to a specific service. Host == "" and
// Path == "" denotes the default backend.
type HostPath struct {
	Host string
	Path string
}

// ServiceMap is a map of (host, path) to the appropriate BackendConfig(s).
type ServiceMap map[HostPath]*v1beta1.IngressBackend

// ServiceMapFromIngress creates a service map from the Ingress object. Note:
// duplicate entries (e.g. invalid configurations) will result in the first
// entry to be chosen.
func ServiceMapFromIngress(ing *v1beta1.Ingress) ServiceMap {
	ret := ServiceMap{}

	if defaultBackend := ing.Spec.Backend; defaultBackend != nil {
		ret[HostPath{}] = defaultBackend
	}

	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			hp := HostPath{Host: rule.Host, Path: path.Path}
			if _, ok := ret[hp]; !ok {
				ret[hp] = &path.Backend
			}
		}
	}
	return ret
}

// IngressBuilder is syntactic sugar for creating Ingress specs for testing
// purposes.
//
//   ing := NewIngressBuilder("ns1", "name1", "127.0.0.1").Build()
type IngressBuilder struct {
	ing *v1beta1.Ingress
}

// NewIngressBuilder instantiates a new IngressBuilder.
func NewIngressBuilder(ns, name, vip string) *IngressBuilder {
	var ing = &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	}
	if vip != "" {
		ing.Status = v1beta1.IngressStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{
					{IP: "127.0.0.1"},
				},
			},
		}
	}
	return &IngressBuilder{ing: ing}
}

// NewIngressBuilderFromExisting creates a IngressBuilder from an existing
// Ingress object. The Ingress object will be copied.
func NewIngressBuilderFromExisting(i *v1beta1.Ingress) *IngressBuilder {
	return &IngressBuilder{ing: i.DeepCopy()}
}

// Build returns a constructed Ingress. The Ingress is a copy, so the Builder
// can be reused to construct multiple Ingress definitions.
func (i *IngressBuilder) Build() *v1beta1.Ingress {
	return i.ing.DeepCopy()
}

// DefaultBackend sets the default backend.
func (i *IngressBuilder) DefaultBackend(service string, port intstr.IntOrString) *IngressBuilder {
	i.ing.Spec.Backend = &v1beta1.IngressBackend{
		ServiceName: service,
		ServicePort: port,
	}
	return i
}

// AddHost adds a rule for a host entry if it did not yet exist.
func (i *IngressBuilder) AddHost(host string) *IngressBuilder {
	i.Host(host)
	return i
}

// Host returns the rule for a host and creates it if it did not exist.
func (i *IngressBuilder) Host(host string) *v1beta1.IngressRule {
	for idx := range i.ing.Spec.Rules {
		if i.ing.Spec.Rules[idx].Host == host {
			return &i.ing.Spec.Rules[idx]
		}
	}
	i.ing.Spec.Rules = append(i.ing.Spec.Rules, v1beta1.IngressRule{
		Host: host,
		IngressRuleValue: v1beta1.IngressRuleValue{
			HTTP: &v1beta1.HTTPIngressRuleValue{},
		},
	})
	return &i.ing.Spec.Rules[len(i.ing.Spec.Rules)-1]
}

// AddPath a new path for the given host if it did not already exist.
func (i *IngressBuilder) AddPath(host, path, service string, port intstr.IntOrString) *IngressBuilder {
	i.Path(host, path, service, port)
	return i
}

// Path returns the Path matching the (host, path), appending the entry if
// it does not already exist.
func (i *IngressBuilder) Path(host, path, service string, port intstr.IntOrString) *v1beta1.HTTPIngressPath {
	h := i.Host(host)
	for idx := range h.HTTP.Paths {
		if h.HTTP.Paths[idx].Path == path {
			return &h.HTTP.Paths[idx]
		}
	}
	h.HTTP.Paths = append(h.HTTP.Paths, v1beta1.HTTPIngressPath{
		Path: path,
		Backend: v1beta1.IngressBackend{
			ServiceName: service,
			ServicePort: port,
		},
	})
	return &h.HTTP.Paths[len(h.HTTP.Paths)-1]
}

// AddTLS adds a TLS secret reference.
func (i *IngressBuilder) AddTLS(hosts []string, secretName string) *IngressBuilder {
	i.ing.Spec.TLS = append(i.ing.Spec.TLS, v1beta1.IngressTLS{
		Hosts:      hosts,
		SecretName: secretName,
	})
	return i
}
