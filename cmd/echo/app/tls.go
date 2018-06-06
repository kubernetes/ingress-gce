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

	"github.com/golang/glog"
)

// createCert creates a certificate and key in temporary files and returns their paths.
func createCert() (certFilePath string, keyFilepath string) {
	cert, key, err := generateInsecureCertAndKey("echo", time.Now(), F.CertificateLifeSpan)
	if err != nil {
		glog.Fatal(err)
	}

	tmpCert, err := ioutil.TempFile("", "server.crt")
	if err != nil {
		glog.Fatal(err)
	}

	tmpKey, err := ioutil.TempFile("", "server.key")
	if err != nil {
		glog.Fatal(err)
	}

	if err := ioutil.WriteFile(tmpCert.Name(), cert, 0644); err != nil {
		glog.Fatal(err)
	}

	if err := ioutil.WriteFile(tmpKey.Name(), key, 0644); err != nil {
		glog.Fatal(err)
	}

	return tmpCert.Name(), tmpKey.Name()
}

const rsaBits = 2048

// https://golang.org/src/crypto/tls/generate_cert.go
func generateInsecureCertAndKey(organization string, validFrom time.Time, validFor time.Duration) (cert, key []byte, err error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		glog.Fatalf("failed to generate serial number: %s", err)
	}

	validUntill := validFrom.Add(validFor)

	priv, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		glog.Fatalf("failed to generate private key: %s", err)
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
		glog.Fatalf("Failed to create certificate: %s", err)
	}
	var certBytes bytes.Buffer
	pem.Encode(&certBytes, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	var keyBytes bytes.Buffer
	pb := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}
	pem.Encode(&keyBytes, pb)

	return certBytes.Bytes(), keyBytes.Bytes(), nil
}
