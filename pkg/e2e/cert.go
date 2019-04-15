/*
Copyright 2019 The Kubernetes Authors.

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

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	compute "google.golang.org/api/compute/v1"
	"k8s.io/api/core/v1"
	"k8s.io/klog"
)

// CertType indicates the intended environment in which this cert is created.
type CertType int

const (
	// GCPCert will be created as an SslCertificate resource in GCP.
	GCPCert CertType = iota
	// K8sCert will be created as a Secret in K8s.
	K8sCert CertType = iota
)

// Cert is a convenience type for representing an SSL certificate.
type Cert struct {
	Host string
	Name string
	Type CertType

	cert []byte
	key  []byte
}

// NewCert returns a cert initialized with data but not yet created in the
// appropriate environment.
func NewCert(name string, host string, typ CertType) (*Cert, error) {
	cert, key, err := generateRSACert(host)
	if err != nil {
		return nil, fmt.Errorf("error generating cert + key: %v", err)
	}
	return &Cert{
		Host: host,
		Name: name,
		Type: typ,
		cert: cert,
		key:  key,
	}, nil
}

// Create creates a cert in its environment.
func (c *Cert) Create(s *Sandbox) error {
	if c.Type == K8sCert {
		return createSecretCert(s, c)
	}

	return createGCPCert(s, c)
}

// Delete deletes the cert from its environment.
func (c *Cert) Delete(s *Sandbox) error {
	if c.Type == K8sCert {
		return DeleteSecret(s, c.Name)
	}

	return deleteGCPCert(s, c)
}

// createSecretCert creates a k8s secret for the provided Cert.
func createSecretCert(s *Sandbox, c *Cert) error {
	data := map[string][]byte{
		v1.TLSCertKey:       c.cert,
		v1.TLSPrivateKeyKey: c.key,
	}
	if _, err := CreateSecret(s, c.Name, data); err != nil {
		return err
	}

	return nil
}

// createGCPCert creates a SslCertificate in GCP based on the provided Cert.
func createGCPCert(s *Sandbox, c *Cert) error {
	sslCert := &compute.SslCertificate{
		Name:        c.Name,
		Certificate: string(c.cert),
		PrivateKey:  string(c.key),
		Description: "gcp cert for ingress testing",
	}
	if err := s.f.Cloud.SslCertificates().Insert(context.Background(), meta.GlobalKey(c.Name), sslCert); err != nil {
		return err
	}
	klog.V(2).Infof("SslCertificate %q created", c.Name)

	return nil
}

// deleteGCPCert deletes the SslCertificate with the provided name.
func deleteGCPCert(s *Sandbox, c *Cert) error {
	if err := s.f.Cloud.SslCertificates().Delete(context.Background(), meta.GlobalKey(c.Name)); err != nil {
		return err
	}
	klog.V(2).Infof("SslCertificate %q deleted", c.Name)

	return nil
}

// generateRSACert generates a self-signed cert for the provided host.
func generateRSACert(host string) ([]byte, []byte, error) {
	if len(host) == 0 {
		return nil, nil, fmt.Errorf("require a non-empty host")
	}
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %v", err)
	}
	notBefore := time.Now()
	notAfter := notBefore.Add(1 * time.Hour)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate serial number: %s", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "default",
			Organization: []string{"Acme Co"},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = append(template.IPAddresses, ip)
	} else {
		template.DNSNames = append(template.DNSNames, host)
	}

	var keyOut, certOut bytes.Buffer
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate: %s", err)
	}
	if err := pem.Encode(&certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return nil, nil, fmt.Errorf("failed encoding cert: %v", err)
	}
	if err := pem.Encode(&keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}); err != nil {
		return nil, nil, fmt.Errorf("failed encoding key: %v", err)
	}

	return certOut.Bytes(), keyOut.Bytes(), nil
}
