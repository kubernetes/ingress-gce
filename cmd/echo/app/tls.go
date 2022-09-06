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

package app

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io/ioutil"
	"math/big"
	"time"

	"k8s.io/klog/v2"
)

// createCert creates a certificate and key in temporary files and returns their paths.
func createCert() (certFilePath string, keyFilepath string) {
	cert, key, err := generateInsecureCertAndKey("echo", time.Now(), F.CertificateLifeSpan)
	if err != nil {
		klog.Fatal(err)
	}

	tmpCert, err := ioutil.TempFile("", "server.crt")
	if err != nil {
		klog.Fatal(err)
	}

	tmpKey, err := ioutil.TempFile("", "server.key")
	if err != nil {
		klog.Fatal(err)
	}

	if err := ioutil.WriteFile(tmpCert.Name(), cert, 0644); err != nil {
		klog.Fatal(err)
	}

	if err := ioutil.WriteFile(tmpKey.Name(), key, 0644); err != nil {
		klog.Fatal(err)
	}

	return tmpCert.Name(), tmpKey.Name()
}

const rsaBits = 2048

// https://golang.org/src/crypto/tls/generate_cert.go
func generateInsecureCertAndKey(organization string, validFrom time.Time, validFor time.Duration) (cert, key []byte, err error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		klog.Fatalf("failed to generate serial number: %s", err)
	}

	validUntill := validFrom.Add(validFor)

	priv, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		klog.Fatalf("failed to generate private key: %s", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{organization},
		},
		NotBefore: validFrom,
		NotAfter:  validUntill,

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		klog.Fatalf("Failed to create certificate: %s", err)
	}
	var certBytes bytes.Buffer
	if err := pem.Encode(&certBytes, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		klog.Fatalf("Failed to encode certificate: %v", err)
	}

	var keyBytes bytes.Buffer
	pb := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}
	if err := pem.Encode(&keyBytes, pb); err != nil {
		klog.Fatalf("Failed to encode RSA Key: %v", err)
	}

	return certBytes.Bytes(), keyBytes.Bytes(), nil
}
