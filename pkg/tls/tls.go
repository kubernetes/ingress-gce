/*
Copyright 2015 The Kubernetes Authors.

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

package tls

import (
	"fmt"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/api/networking/v1beta1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/translator"
)

// TlsLoader is the interface for loading the relevant TLSCerts for a given ingress.
type TlsLoader interface {
	// Load loads the relevant TLSCerts based on ing.Spec.TLS
	Load(ing *v1beta1.Ingress) ([]*loadbalancers.TLSCerts, error)
}

// TLSCertsFromSecretsLoader loads TLS certs from kubernetes secrets.
type TLSCertsFromSecretsLoader struct {
	Client kubernetes.Interface
}

// Ensure that TLSCertsFromSecretsLoader implements TlsLoader interface.
var _ TlsLoader = &TLSCertsFromSecretsLoader{}

func (t *TLSCertsFromSecretsLoader) Load(ing *v1beta1.Ingress) ([]*loadbalancers.TLSCerts, error) {
	env, err := translator.NewEnv(ing, t.Client, "", "", "")
	if err != nil {
		return nil, fmt.Errorf("error initializing translator env: %v", err)
	}

	var certs []*loadbalancers.TLSCerts

	secrets, err := translator.Secrets(env)
	if err != nil {
		return nil, fmt.Errorf("error getting secrets for Ingress: %v", err)
	}
	for _, secret := range secrets {
		cert := string(secret.Data[api_v1.TLSCertKey])
		newCert := &loadbalancers.TLSCerts{
			Key:      string(secret.Data[api_v1.TLSPrivateKeyKey]),
			Cert:     cert,
			Name:     secret.Name,
			CertHash: loadbalancers.GetCertHash(cert),
		}
		certs = append(certs, newCert)
	}
	return certs, nil
}

// TODO: Add support for file loading so we can support HTTPS default backends.

// FakeTLSSecretLoader fakes out TLS loading.
type FakeTLSSecretLoader struct {
	FakeCerts map[string]*loadbalancers.TLSCerts
}

// Ensure that FakeTLSSecretLoader implements TlsLoader interface.
var _ TlsLoader = &FakeTLSSecretLoader{}

func (f *FakeTLSSecretLoader) Load(ing *v1beta1.Ingress) ([]*loadbalancers.TLSCerts, error) {
	if len(ing.Spec.TLS) == 0 {
		return nil, nil
	}
	var certs []*loadbalancers.TLSCerts
	for name, cert := range f.FakeCerts {
		for _, secret := range ing.Spec.TLS {
			if secret.SecretName == name {
				certs = append(certs, cert)
			}
		}
	}
	if len(certs) == 0 {
		return certs, fmt.Errorf("couldn't find secret for ingress %v", ing.Name)
	}
	return certs, nil
}
