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

package loadbalancers

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/golang/glog"
	compute "google.golang.org/api/compute/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/utils"
)

const SslCertificateMissing = "SslCertificateMissing"

func (l *L7) checkSSLCert() error {
	// Use both pre-shared and secret-based certs if available,
	// combining encountered errors.
	errs := []error{}

	// Handle annotation pre-shared-cert
	preSharedSslCerts, err := l.getPreSharedCertificates()
	if err != nil {
		errs = append(errs, err)
	}
	l.sslCerts = preSharedSslCerts

	// Get updated value of certificate for comparison
	existingSecretsSslCerts, err := l.getIngressManagedSslCerts()
	if err != nil {
		errs = append(errs, err)
		// Do not continue if getIngressManagedSslCerts() failed.
		return utils.JoinErrs(errs)
	}

	l.oldSSLCerts = existingSecretsSslCerts
	secretsSslCerts, err := l.createSslCertificates(existingSecretsSslCerts)
	if err != nil {
		errs = append(errs, err)
	}
	l.sslCerts = append(l.sslCerts, secretsSslCerts...)
	glog.V(2).Infof("Using %v pre-shared certificates and %v certificates from secrets", len(preSharedSslCerts), len(secretsSslCerts))
	if len(errs) > 0 {
		return utils.JoinErrs(errs)
	}
	return nil
}

// createSslCertificates creates SslCertificates based on kubernetes secrets in Ingress configuration.
func (l *L7) createSslCertificates(existingCerts []*compute.SslCertificate) ([]*compute.SslCertificate, error) {
	var result []*compute.SslCertificate

	existingCertsMap := getMapfromCertList(existingCerts)

	// mapping of currently configured certs
	visitedCertMap := make(map[string]string)
	var failedCerts []string

	for _, tlsCert := range l.runtimeInfo.TLS {
		ingCert := tlsCert.Cert
		ingKey := tlsCert.Key
		gcpCertName := l.namer.SSLCertName(l.Name, tlsCert.CertHash)

		if addedBy, exists := visitedCertMap[gcpCertName]; exists {
			glog.V(3).Infof("Secret %q has a certificate already used by %v", tlsCert.Name, addedBy)
			continue
		}

		// PrivateKey is write only, so compare certs alone. We're assuming that
		// no one will change just the key. We can remember the key and compare,
		// but a bug could end up leaking it, which feels worse.
		// If the cert contents have changed, its hash would be different, so would be the cert name. So it is enough
		// to check if this cert name exists in the map.
		if existingCertsMap != nil {
			if cert, ok := existingCertsMap[gcpCertName]; ok {
				glog.V(3).Infof("Secret %q already exists as certificate %q", tlsCert.Name, gcpCertName)
				visitedCertMap[gcpCertName] = fmt.Sprintf("certificate:%q", gcpCertName)
				result = append(result, cert)
				continue
			}
		}
		// Controller needs to create the certificate, no need to check if it exists and delete. If it did exist, it
		// would have been listed in the populateSSLCert function and matched in the check above.
		glog.V(2).Infof("Creating new sslCertificate %q for LB %q", gcpCertName, l.Name)
		cert, err := l.cloud.CreateSslCertificate(&compute.SslCertificate{
			Name:        gcpCertName,
			Certificate: ingCert,
			PrivateKey:  ingKey,
		})
		if err != nil {
			glog.Errorf("Failed to create new sslCertificate %q for %q - %v", gcpCertName, l.Name, err)
			failedCerts = append(failedCerts, gcpCertName+" Error:"+err.Error())
			continue
		}
		visitedCertMap[gcpCertName] = fmt.Sprintf("secret:%q", tlsCert.Name)
		result = append(result, cert)
	}

	// Save the old certs for cleanup after we update the target proxy.
	if len(failedCerts) > 0 {
		return result, fmt.Errorf("Cert creation failures - %s", strings.Join(failedCerts, ","))
	}
	return result, nil
}

// getSslCertificates fetches GCE SslCertificate resources by names.
func (l *L7) getSslCertificates(names []string) ([]*compute.SslCertificate, error) {
	var result []*compute.SslCertificate
	var failedCerts []string
	for _, name := range names {
		// Ask GCE for the cert, checking for problems and existence.
		cert, err := l.cloud.GetSslCertificate(name)
		if err != nil {
			failedCerts = append(failedCerts, name+": "+err.Error())
			l.recorder.Eventf(l.runtimeInfo.Ingress, corev1.EventTypeNormal, SslCertificateMissing, err.Error())
			continue
		}
		if cert == nil {
			failedCerts = append(failedCerts, name+": unable to find existing SslCertificate")
			continue
		}

		glog.V(2).Infof("Using existing SslCertificate %v for %v", name, l.Name)
		result = append(result, cert)
	}
	if len(failedCerts) != 0 {
		return result, fmt.Errorf("errors - %s", strings.Join(failedCerts, ","))
	}

	return result, nil
}

// getPreSharedCertificates fetches SslCertificates specified via pre-shared-cert annotation.
func (l *L7) getPreSharedCertificates() ([]*compute.SslCertificate, error) {
	if l.runtimeInfo.TLSName == "" {
		return nil, nil
	}
	sslCerts, err := l.getSslCertificates(utils.SplitAnnotation(l.runtimeInfo.TLSName))

	if err != nil {
		return sslCerts, fmt.Errorf("pre-shared-cert errors: %s", err.Error())
	}

	return sslCerts, nil
}

func getMapfromCertList(certs []*compute.SslCertificate) map[string]*compute.SslCertificate {
	if len(certs) == 0 {
		return nil
	}
	certMap := make(map[string]*compute.SslCertificate)
	for _, cert := range certs {
		certMap[cert.Name] = cert
	}
	return certMap
}

// getIngressManagedSslCerts fetches SslCertificate resources created and managed by this load balancer
// instance. These SslCertificate resources were created based on kubernetes secrets in Ingress
// configuration.
func (l *L7) getIngressManagedSslCerts() ([]*compute.SslCertificate, error) {
	var result []*compute.SslCertificate

	// Currently we list all certs available in gcloud and filter the ones managed by this loadbalancer instance. This is
	// to make sure we garbage collect any old certs that this instance might have lost track of due to crashes.
	// Can be a performance issue if there are too many global certs, default quota is only 10.
	certs, err := l.cloud.ListSslCertificates()
	if err != nil {
		return nil, err
	}
	for _, c := range certs {
		if l.namer.IsCertUsedForLB(l.Name, c.Name) {
			glog.V(4).Infof("Populating ssl cert %s for l7 %s", c.Name, l.Name)
			result = append(result, c)
		}
	}
	if len(result) == 0 {
		// Check for legacy cert since that follows a different naming convention
		glog.V(4).Infof("Looking for legacy ssl certs")
		expectedCertLinks, err := l.getSslCertLinkInUse()
		if err != nil {
			// Return nil if target proxy doesn't exist.
			return nil, utils.IgnoreHTTPNotFound(err)
		}
		for _, link := range expectedCertLinks {
			// Retrieve the certificate and ignore error if certificate wasn't found
			name, err := utils.KeyName(link)
			if err != nil {
				glog.Warningf("error parsing cert name: %v", err)
				continue
			}

			if !l.namer.IsLegacySSLCert(l.Name, name) {
				continue
			}
			cert, _ := l.cloud.GetSslCertificate(name)
			if cert != nil {
				glog.V(4).Infof("Populating legacy ssl cert %s for l7 %s", cert.Name, l.Name)
				result = append(result, cert)
			}
		}
	}
	return result, nil
}

func (l *L7) deleteOldSSLCerts() {
	if len(l.oldSSLCerts) == 0 {
		return
	}
	certsMap := getMapfromCertList(l.sslCerts)
	for _, cert := range l.oldSSLCerts {
		if !l.namer.IsCertUsedForLB(l.Name, cert.Name) && !l.namer.IsLegacySSLCert(l.Name, cert.Name) {
			// retain cert if it is managed by GCE(non-ingress)
			continue
		}
		if _, ok := certsMap[cert.Name]; ok {
			// cert found in current map
			continue
		}
		glog.V(3).Infof("Cleaning up old SSL Certificate %s", cert.Name)
		if certErr := utils.IgnoreHTTPNotFound(l.cloud.DeleteSslCertificate(cert.Name)); certErr != nil {
			glog.Errorf("Old cert delete failed - %v", certErr)
		}
	}
}

// Returns true if the input array of certs is identical to the certs in the L7 config.
// Returns false if there is any mismatch
func (l *L7) compareCerts(certLinks []string) bool {
	certsMap := getMapfromCertList(l.sslCerts)
	if len(certLinks) != len(certsMap) {
		glog.V(4).Infof("Loadbalancer has %d certs, target proxy has %d certs", len(certsMap), len(certLinks))
		return false
	}

	for _, link := range certLinks {
		certName, err := utils.KeyName(link)
		if err != nil {
			glog.Warningf("Cannot get cert name: %v", err)
			return false
		}

		if cert, ok := certsMap[certName]; !ok {
			glog.V(4).Infof("Cannot find cert with name %s in certsMap %+v", certName, certsMap)
			return false
		} else if ok && !utils.EqualResourceIDs(link, cert.SelfLink) {
			glog.V(4).Infof("Selflink compare failed for certs - %s in loadbalancer, %s in targetproxy", cert.SelfLink, link)
			return false
		}
	}
	return true
}

func GetCertHash(contents string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(contents)))[:16]
}

func toCertNames(certs []*compute.SslCertificate) (names []string) {
	for _, v := range certs {
		names = append(names, v.Name)
	}
	return names
}
