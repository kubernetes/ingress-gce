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
	"fmt"

	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	backendconfigutil "k8s.io/ingress-gce/pkg/backendconfig"
	translatorutil "k8s.io/ingress-gce/pkg/controller/translator"
)

// BackendConfigForPath returns the BackendConfig associated with the given path.
// Note: This function returns an empty object (not nil pointer) if a BackendConfig
// did not exist in the given environment.
func BackendConfigForPath(host, path string, ing *v1beta1.Ingress, env ValidatorEnv) (*backendconfig.BackendConfig, error) {
	sm := ServiceMapFromIngress(ing)
	if path == pathForDefaultBackend {
		path = ""
	}
	hp := HostPath{Host: host, Path: path}
	b, ok := sm[hp]
	if !ok {
		return nil, fmt.Errorf("HostPath %v not found in Ingress", hp)
	}
	serviceMap, err := env.Services()
	if err != nil {
		return nil, err
	}
	service, ok := serviceMap[b.ServiceName]
	if !ok {
		return nil, fmt.Errorf("Service %q not found in environment", b.ServiceName)
	}
	servicePort := translatorutil.ServicePort(*service, b.ServicePort)
	if servicePort == nil {
		return nil, fmt.Errorf("Port %+v in Service %q not found", b.ServicePort, b.ServiceName)
	}
	bc, err := annotations.FromService(service).GetBackendConfigs()
	if err != nil {
		return nil, err
	}
	configName := backendconfigutil.BackendConfigName(*bc, *servicePort)
	backendConfigMap, err := env.BackendConfigs()
	if err != nil {
		return nil, err
	}
	backendConfig, ok := backendConfigMap[configName]
	if !ok {
		return &backendconfig.BackendConfig{}, nil
	}
	return backendConfig, nil
}

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

// BackendConfigBuilder is syntactic sugar for creating BackendConfig specs for testing
// purposes.
//
//   backendConfig := NewBackendConfigBuilder("ns1", "name1").Build()
type BackendConfigBuilder struct {
	backendConfig *backendconfig.BackendConfig
}

// NewBackendConfigBuilder instantiates a new BackendConfig.
func NewBackendConfigBuilder(ns, name string) *BackendConfigBuilder {
	var backendConfig = &backendconfig.BackendConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	}
	return &BackendConfigBuilder{backendConfig: backendConfig}
}

// NewBackendConfigBuilderFromExisting creates a BackendConfigBuilder from an existing
// BackendConfig object. The BackendConfigBuilder object will be copied.
func NewBackendConfigBuilderFromExisting(b *backendconfig.BackendConfig) *BackendConfigBuilder {
	return &BackendConfigBuilder{backendConfig: b.DeepCopy()}
}

// Build returns a constructed BackendConfig. The BackendConfig is a copy, so the Builder
// can be reused to construct multiple BackendConfig definitions.
func (b *BackendConfigBuilder) Build() *backendconfig.BackendConfig {
	return b.backendConfig.DeepCopy()
}

// EnableCDN enables or disables CDN on the BackendConfig.
func (b *BackendConfigBuilder) EnableCDN(enabled bool) *BackendConfigBuilder {
	if b.backendConfig.Spec.Cdn == nil {
		b.backendConfig.Spec.Cdn = &backendconfig.CDNConfig{}
	}
	b.backendConfig.Spec.Cdn.Enabled = enabled
	return b
}

// SetCachePolicy specifies the cache policy on the BackendConfig.
func (b *BackendConfigBuilder) SetCachePolicy(cachePolicy *backendconfig.CacheKeyPolicy) *BackendConfigBuilder {
	if b.backendConfig.Spec.Cdn == nil {
		b.backendConfig.Spec.Cdn = &backendconfig.CDNConfig{}
	}
	b.backendConfig.Spec.Cdn.CachePolicy = cachePolicy
	return b
}

// SetIAPConfig enables or disables IAP on the BackendConfig and also sets
// the secret which contains the OAuth credentials.
func (b *BackendConfigBuilder) SetIAPConfig(enabled bool, secret string) *BackendConfigBuilder {
	if b.backendConfig.Spec.Iap == nil {
		b.backendConfig.Spec.Iap = &backendconfig.IAPConfig{}
	}
	b.backendConfig.Spec.Iap.Enabled = enabled
	b.backendConfig.Spec.Iap.OAuthClientCredentials = &backendconfig.OAuthClientCredentials{SecretName: secret}
	return b
}
