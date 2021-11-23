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
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	frontendconfig "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	backendconfigutil "k8s.io/ingress-gce/pkg/backendconfig"
	translatorutil "k8s.io/ingress-gce/pkg/controller/translator"
)

// ServiceForPath returns the Service and ServicePort associated with the given path.
func ServiceForPath(host, path string, ing *networkingv1.Ingress, env ValidatorEnv) (*v1.Service, *v1.ServicePort, error) {
	sm := ServiceMapFromIngress(ing)
	if path == pathForDefaultBackend {
		path = ""
	}
	hp := HostPath{Host: host, Path: path}
	b, ok := sm[hp]
	if !ok {
		return nil, nil, fmt.Errorf("HostPath %v not found in Ingress", hp)
	}
	serviceMap, err := env.Services()
	if err != nil {
		return nil, nil, err
	}
	service, ok := serviceMap[b.Service.Name]
	if !ok {
		return nil, nil, fmt.Errorf("service %q not found in environment", b.Service.Name)
	}
	servicePort := translatorutil.ServicePort(*service, b.Service.Port)
	if servicePort == nil {
		return nil, nil, fmt.Errorf("port %+v in Service %q not found", b.Service.Port, b.Service.Name)
	}

	return service, servicePort, nil
}

// BackendConfigForPath returns the BackendConfig associated with the given path.
// Note: This function returns an empty object (not nil pointer) if a BackendConfig
// did not exist in the given environment.
func BackendConfigForPath(host, path string, ing *networkingv1.Ingress, env ValidatorEnv) (*backendconfig.BackendConfig, error) {
	service, servicePort, err := ServiceForPath(host, path, ing, env)
	if err != nil {
		return nil, err
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

// ServiceMap is a map of (host, path) to the appropriate backend.
type ServiceMap map[HostPath]*networkingv1.IngressBackend

// ServiceMapFromIngress creates a service map from the Ingress object. Note:
// duplicate entries (e.g. invalid configurations) will result in the first
// entry to be chosen.
func ServiceMapFromIngress(ing *networkingv1.Ingress) ServiceMap {
	ret := ServiceMap{}

	if defaultBackend := ing.Spec.DefaultBackend; defaultBackend != nil {
		ret[HostPath{}] = defaultBackend
	}

	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			hp := HostPath{Host: rule.Host, Path: path.Path}
			if _, ok := ret[hp]; !ok {
				// Copy the value over to a new struct so that we won't be
				// saving the same pointer.
				cloneBackend := path.Backend
				ret[hp] = &cloneBackend
			}
		}
	}
	return ret
}

func FrontendConfigForIngress(ing *networkingv1.Ingress, env ValidatorEnv) (*frontendconfig.FrontendConfig, error) {
	name := annotations.FromIngress(ing).FrontendConfig()
	if name != "" {
		fcMap, err := env.FrontendConfigs()
		if err != nil {
			return nil, err
		}

		if fc, ok := fcMap[name]; ok {
			return fc, nil
		}
	}

	// No FrontendConfig found
	return nil, nil
}

// IngressBuilder is syntactic sugar for creating Ingress specs for testing
// purposes.
//
//   ing := NewIngressBuilder("ns1", "name1", "127.0.0.1").Build()
type IngressBuilder struct {
	ing *networkingv1.Ingress
}

// NewIngressBuilder instantiates a new IngressBuilder.
func NewIngressBuilder(ns, name, vip string) *IngressBuilder {
	var ing = &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	}
	if vip != "" {
		ing.Status = networkingv1.IngressStatus{
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
func NewIngressBuilderFromExisting(i *networkingv1.Ingress) *IngressBuilder {
	return &IngressBuilder{ing: i.DeepCopy()}
}

// Build returns a constructed Ingress. The Ingress is a copy, so the Builder
// can be reused to construct multiple Ingress definitions.
func (i *IngressBuilder) Build() *networkingv1.Ingress {
	return i.ing.DeepCopy()
}

// DefaultBackend sets the default backend.
func (i *IngressBuilder) DefaultBackend(service string, port networkingv1.ServiceBackendPort) *IngressBuilder {
	i.ing.Spec.DefaultBackend = &networkingv1.IngressBackend{
		Service: &networkingv1.IngressServiceBackend{
			Name: service,
			Port: port,
		},
	}
	return i
}

// AddHost adds a rule for a host entry if it did not yet exist.
func (i *IngressBuilder) AddHost(host string) *IngressBuilder {
	i.Host(host)
	return i
}

// Host returns the rule for a host and creates it if it did not exist.
func (i *IngressBuilder) Host(host string) *networkingv1.IngressRule {
	for idx := range i.ing.Spec.Rules {
		if i.ing.Spec.Rules[idx].Host == host {
			return &i.ing.Spec.Rules[idx]
		}
	}
	i.ing.Spec.Rules = append(i.ing.Spec.Rules, networkingv1.IngressRule{
		Host: host,
		IngressRuleValue: networkingv1.IngressRuleValue{
			HTTP: &networkingv1.HTTPIngressRuleValue{},
		},
	})
	return &i.ing.Spec.Rules[len(i.ing.Spec.Rules)-1]
}

// AddPath a new path for the given host if it did not already exist.
func (i *IngressBuilder) AddPath(host, path, service string, port networkingv1.ServiceBackendPort) *IngressBuilder {
	i.Path(host, path, service, port)
	return i
}

// Path returns the Path matching the (host, path), appending the entry if
// it does not already exist.
func (i *IngressBuilder) Path(host, path, service string, port networkingv1.ServiceBackendPort) *networkingv1.HTTPIngressPath {
	h := i.Host(host)
	for idx := range h.HTTP.Paths {
		if h.HTTP.Paths[idx].Path == path {
			return &h.HTTP.Paths[idx]
		}
	}
	pathType := networkingv1.PathTypeImplementationSpecific
	h.HTTP.Paths = append(h.HTTP.Paths, networkingv1.HTTPIngressPath{
		Path:     path,
		PathType: &pathType,
		Backend: networkingv1.IngressBackend{
			Service: &networkingv1.IngressServiceBackend{
				Name: service,
				Port: port,
			},
		},
	})
	return &h.HTTP.Paths[len(h.HTTP.Paths)-1]
}

// SetTLS sets TLS certs to given list.
func (i *IngressBuilder) SetTLS(tlsCerts []networkingv1.IngressTLS) *IngressBuilder {
	i.ing.Spec.TLS = tlsCerts
	return i
}

// AddTLS adds a TLS secret reference.
func (i *IngressBuilder) AddTLS(hosts []string, secretName string) *IngressBuilder {
	i.ing.Spec.TLS = append(i.ing.Spec.TLS, networkingv1.IngressTLS{
		Hosts:      hosts,
		SecretName: secretName,
	})
	return i
}

// AddPresharedCerts adds preshared certs via the annotation. Note that a value
// added in a previous call to this function will be overwritten.
func (i *IngressBuilder) AddPresharedCerts(names []string) *IngressBuilder {
	if i.ing.Annotations == nil {
		i.ing.Annotations = make(map[string]string)
	}
	i.ing.Annotations[annotations.PreSharedCertKey] = strings.Join(names, ",")
	return i
}

// AddStaticIP adds the name of an address that exists in GCP via the annotation.
// Note that a value added in a previous call to this function will be overwritten.
func (i *IngressBuilder) AddStaticIP(name string, regional bool) *IngressBuilder {
	if i.ing.Annotations == nil {
		i.ing.Annotations = make(map[string]string)
	}

	if regional {
		i.ing.Annotations[annotations.RegionalStaticIPNameKey] = name
	} else {
		i.ing.Annotations[annotations.GlobalStaticIPNameKey] = name
	}
	return i
}

// SetIngressClass sets Ingress class to given name.
func (i *IngressBuilder) SetIngressClass(name string) *IngressBuilder {
	if i.ing.Annotations == nil {
		i.ing.Annotations = make(map[string]string)
	}
	i.ing.Annotations[annotations.IngressClassKey] = name
	return i
}

// Configure for ILB adds the ILB ingress class annotation
func (i *IngressBuilder) ConfigureForILB() *IngressBuilder {
	if i.ing.Annotations == nil {
		i.ing.Annotations = make(map[string]string)
	}
	i.ing.Annotations[annotations.IngressClassKey] = annotations.GceL7ILBIngressClass
	return i
}

// SetAllowHttp sets the AllowHTTP annotation on the ingress
func (i *IngressBuilder) SetAllowHttp(val bool) *IngressBuilder {
	if i.ing.Annotations == nil {
		i.ing.Annotations = make(map[string]string)
	}
	i.ing.Annotations[annotations.AllowHTTPKey] = strconv.FormatBool(val)
	return i
}

// SetFrontendConfig sets the FrontendConfig annotation on the ingress
func (i *IngressBuilder) SetFrontendConfig(name string) *IngressBuilder {
	if i.ing.Annotations == nil {
		i.ing.Annotations = make(map[string]string)
	}
	i.ing.Annotations[annotations.FrontendConfigKey] = name
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

// SetSessionAffinity specifies the session affinity on the BackendConfig.
func (b *BackendConfigBuilder) SetSessionAffinity(affinity string) *BackendConfigBuilder {
	if b.backendConfig.Spec.SessionAffinity == nil {
		b.backendConfig.Spec.SessionAffinity = &backendconfig.SessionAffinityConfig{}
	}
	b.backendConfig.Spec.SessionAffinity.AffinityType = affinity
	return b
}

// SetAffinityCookieTtlSec specifies the session affinity cookie TTL on the BackendConfig.
func (b *BackendConfigBuilder) SetAffinityCookieTtlSec(ttl int64) *BackendConfigBuilder {
	if b.backendConfig.Spec.SessionAffinity == nil {
		b.backendConfig.Spec.SessionAffinity = &backendconfig.SessionAffinityConfig{}
	}
	b.backendConfig.Spec.SessionAffinity.AffinityCookieTtlSec = &ttl
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

// SetCDNConfig specifies the cache policy on the BackendConfig.
func (b *BackendConfigBuilder) SetCDNConfig(cdnConfig *backendconfig.CDNConfig) *BackendConfigBuilder {
	b.backendConfig.Spec.Cdn = cdnConfig
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

// SetSecurityPolicy sets security policy on the BackendConfig.
func (b *BackendConfigBuilder) SetSecurityPolicy(securityPolicy string) *BackendConfigBuilder {
	if b.backendConfig.Spec.SecurityPolicy == nil {
		b.backendConfig.Spec.SecurityPolicy = &backendconfig.SecurityPolicyConfig{}
	}
	b.backendConfig.Spec.SecurityPolicy.Name = securityPolicy
	return b
}

// SetTimeout defines the BackendConfig's connection timeout
func (b *BackendConfigBuilder) SetTimeout(timeout int64) *BackendConfigBuilder {
	b.backendConfig.Spec.TimeoutSec = &timeout
	return b
}

// SetConnectionDrainingTimeout defines the BackendConfig's draining timeout
func (b *BackendConfigBuilder) SetConnectionDrainingTimeout(timeout int64) *BackendConfigBuilder {
	if b.backendConfig.Spec.ConnectionDraining == nil {
		b.backendConfig.Spec.ConnectionDraining = &backendconfig.ConnectionDrainingConfig{}
	}
	b.backendConfig.Spec.ConnectionDraining.DrainingTimeoutSec = timeout
	return b
}

// AddCustomRequestHeader adds a custom request header to the BackendConfig.
func (b *BackendConfigBuilder) AddCustomRequestHeader(header string) *BackendConfigBuilder {
	if b.backendConfig.Spec.CustomRequestHeaders == nil {
		b.backendConfig.Spec.CustomRequestHeaders = &backendconfig.CustomRequestHeadersConfig{}
	}
	b.backendConfig.Spec.CustomRequestHeaders.Headers = append(b.backendConfig.Spec.CustomRequestHeaders.Headers, header)
	return b
}

// SetHealthCheckPath adds a health check path override.
func (b *BackendConfigBuilder) SetHealthCheckPath(path string) *BackendConfigBuilder {
	if b.backendConfig.Spec.HealthCheck == nil {
		b.backendConfig.Spec.HealthCheck = &backendconfig.HealthCheckConfig{}
	}
	b.backendConfig.Spec.HealthCheck.RequestPath = &path
	return b
}

// EnableLogging enables or disables access logging.
func (b *BackendConfigBuilder) EnableLogging(enabled bool) *BackendConfigBuilder {
	if b.backendConfig.Spec.Logging == nil {
		b.backendConfig.Spec.Logging = &backendconfig.LogConfig{}
	}
	b.backendConfig.Spec.Logging.Enable = enabled
	return b
}

// SetSampleRate sets log sampling rate.
func (b *BackendConfigBuilder) SetSampleRate(sampleRate *float64) *BackendConfigBuilder {
	if b.backendConfig.Spec.Logging == nil {
		b.backendConfig.Spec.Logging = &backendconfig.LogConfig{}
	}
	b.backendConfig.Spec.Logging.SampleRate = sampleRate
	return b
}

// FrontendConfigBuilder is syntactic sugar for creating FrontendConfig specs for testing
// purposes.
//
//   frontendConfig := NewFrontendConfigBuilder("ns1", "name1").Build()
type FrontendConfigBuilder struct {
	frontendConfig *frontendconfig.FrontendConfig
}

// NewFrontendConfigBuilder instantiates a new FrontendConfig.
func NewFrontendConfigBuilder(ns, name string) *FrontendConfigBuilder {
	var fc = &frontendconfig.FrontendConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: frontendconfig.FrontendConfigSpec{},
	}
	return &FrontendConfigBuilder{frontendConfig: fc}
}

// NewFrontendConfigBuilderFromExisting creates a FrontendConfigBuilder from an existing
// FrontendConfig object. The FrontendConfigBuilder object will be copied.
func NewFrontendConfigBuilderFromExisting(f *frontendconfig.FrontendConfig) *FrontendConfigBuilder {
	return &FrontendConfigBuilder{frontendConfig: f.DeepCopy()}
}

// Build returns a constructed FrontendConfig. The FrontendConfig is a copy, so the Builder
// can be reused to construct multiple FrontendConfig definitions.
func (f *FrontendConfigBuilder) Build() *frontendconfig.FrontendConfig {
	return f.frontendConfig.DeepCopy()
}

// SetSslPolicy Sets ths SslPolicy on the FrontendConfig.
func (f *FrontendConfigBuilder) SetSslPolicy(policy string) *FrontendConfigBuilder {
	f.frontendConfig.Spec.SslPolicy = &policy
	return f
}
