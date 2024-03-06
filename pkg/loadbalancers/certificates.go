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
	"fmt"
	"strings"

	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/translator"
	"k8s.io/ingress-gce/pkg/utils"
)

const SslCertificateMissing = "SslCertificateMissing"

func (l7 *L7) checkSSLCert() error {
	isL7ILB := utils.IsGCEL7ILBIngress(l7.runtimeInfo.Ingress)
	isL7XLBRegional := utils.IsGCEL7XLBRegionalIngress(l7.runtimeInfo.Ingress)
	tr := translator.NewTranslator(isL7ILB, isL7XLBRegional, l7.namer)
	env := &translator.Env{Region: l7.cloud.Region(), Project: l7.cloud.ProjectID()}
	translatorCerts := tr.ToCompositeSSLCertificates(env, l7.runtimeInfo.TLSName, l7.runtimeInfo.TLS, l7.Versions().SslCertificate)

	// Use both pre-shared and secret-based certs if available,
	// combining encountered errors.
	errs := []error{}

	// Get updated value of certificate for comparison
	existingSecretsSslCerts, err := l7.getIngressManagedSslCerts()
	if err != nil {
		errs = append(errs, err)
		// Do not continue if getIngressManagedSslCerts() failed.
		return utils.JoinErrs(errs)
	}

	l7.oldSSLCerts = existingSecretsSslCerts
	sslCerts, err := l7.createSslCertificates(existingSecretsSslCerts, translatorCerts)
	if err != nil {
		errs = append(errs, err)
	}
	l7.sslCerts = sslCerts
	if len(errs) > 0 {
		return utils.JoinErrs(errs)
	}
	return nil
}

// createSslCertificates creates SslCertificates based on kubernetes secrets in Ingress configuration.
func (l7 *L7) createSslCertificates(existingCerts, translatorCerts []*composite.SslCertificate) ([]*composite.SslCertificate, error) {
	var result []*composite.SslCertificate

	existingCertsMap := getMapFromCertList(existingCerts)

	// mapping of currently configured certs
	visitedCertMap := make(map[string]string)
	var failedCerts []string

	for _, translatorCert := range translatorCerts {
		// Ignore pre-shared certs here
		if translatorCert.Certificate == "" {
			result = append(result, translatorCert)
			continue
		}

		if addedBy, exists := visitedCertMap[translatorCert.Name]; exists {
			l7.logger.V(3).Info("Secret cert has a certificate already used", "certName", translatorCert.Name, "addedBy", addedBy)
			continue
		}

		// PrivateKey is write only, so compare certs alone. We're assuming that
		// no one will change just the key. We can remember the key and compare,
		// but a bug could end up leaking it, which feels worse.
		// If the cert contents have changed, its hash would be different, so would be the cert name. So it is enough
		// to check if this cert name exists in the map.
		if existingCertsMap != nil {
			if cert, ok := existingCertsMap[translatorCert.Name]; ok {
				l7.logger.V(3).Info("Secret cert already exists as another certificate", "secretCertName", translatorCert.Name, "existingCertName", cert.Name)
				visitedCertMap[translatorCert.Name] = fmt.Sprintf("certificate:%q", translatorCert.Name)
				result = append(result, cert)
				continue
			}
		}
		// Controller needs to create the certificate, no need to check if it exists and delete. If it did exist, it
		// would have been listed in the populateSSLCert function and matched in the check above.
		l7.logger.V(2).Info("Creating new sslCertificate for LB", "certName", translatorCert.Name, "l7", l7)
		translatorCert.Version = l7.Versions().SslCertificate
		key, err := l7.CreateKey(translatorCert.Name)
		if err != nil {
			l7.logger.Error(err, "l7.CreateKey", "certName", translatorCert.Name)
			return nil, err
		}
		err = composite.CreateSslCertificate(l7.cloud, key, translatorCert, l7.logger)
		if err != nil {
			l7.logger.Error(err, "Failed to create new sslCertificate for LB", "certName", translatorCert.Name, "l7", l7)
			failedCerts = append(failedCerts, translatorCert.Name+" Error:"+err.Error())
			continue
		}
		visitedCertMap[translatorCert.Name] = fmt.Sprintf("secret cert:%q", translatorCert.Certificate)

		// Get SSLCert
		cert, err := composite.GetSslCertificate(l7.cloud, key, translatorCert.Version, l7.logger)
		if err != nil {
			l7.logger.Error(err, "GetSslCertificate", "key", key, "certVersion", translatorCert.Version)
			return nil, err
		}
		result = append(result, cert)
	}

	// Save the old certs for cleanup after we update the target proxy.
	if len(failedCerts) > 0 {
		return result, fmt.Errorf("Cert creation failures - %s", strings.Join(failedCerts, ","))
	}
	return result, nil
}

func getMapFromCertList(certs []*composite.SslCertificate) map[string]*composite.SslCertificate {
	if len(certs) == 0 {
		return nil
	}
	certMap := make(map[string]*composite.SslCertificate)
	for _, cert := range certs {
		certMap[cert.Name] = cert
	}
	return certMap
}

// getIngressManagedSslCerts fetches SslCertificate resources created and managed by this load balancer
// instance. These SslCertificate resources were created based on kubernetes secrets in Ingress
// configuration.
func (l7 *L7) getIngressManagedSslCerts() ([]*composite.SslCertificate, error) {
	var result []*composite.SslCertificate

	// Currently we list all certs available in gcloud and filter the ones managed by this loadbalancer instance. This is
	// to make sure we garbage collect any old certs that this instance might have lost track of due to crashes.
	// Can be a performance issue if there are too many global certs, default quota is only 10.
	// Use an empty name parameter since we only care about the scope
	// TODO: (shance) refactor this so we don't need an empty arg
	key, err := l7.CreateKey("")
	if err != nil {
		return nil, err
	}
	version := l7.Versions().SslCertificate
	certs, err := composite.ListSslCertificates(l7.cloud, key, version, l7.logger)
	if err != nil {
		return nil, err
	}
	for _, c := range certs {
		if l7.namer.IsCertNameForLB(c.Name) {
			l7.logger.V(4).Info("Populating ssl cert for l7", "certName", c.Name, "l7", l7)
			result = append(result, c)
		}
	}
	if len(result) == 0 {
		// Check for legacy cert since that follows a different naming convention
		l7.logger.V(4).Info("Looking for legacy ssl certs")
		expectedCertLinks, err := l7.getSslCertLinkInUse()
		if err != nil {
			// Return nil if target proxy doesn't exist.
			return nil, utils.IgnoreHTTPNotFound(err)
		}
		for _, link := range expectedCertLinks {
			// Retrieve the certificate and ignore error if certificate wasn't found
			name, err := utils.KeyName(link)
			if err != nil {
				l7.logger.Info("Error parsing cert name", "err", err)
				continue
			}

			if !l7.namer.IsLegacySSLCert(name) {
				continue
			}
			key, err := l7.CreateKey(name)
			if err != nil {
				return nil, err
			}
			cert, _ := composite.GetSslCertificate(l7.cloud, key, version, l7.logger)
			if cert != nil {
				l7.logger.V(4).Info("Populating legacy ssl cert for l7", "certName", cert.Name, "l7", l7)
				result = append(result, cert)
			}
		}
	}
	return result, nil
}

func (l7 *L7) deleteOldSSLCerts() {
	if len(l7.oldSSLCerts) == 0 {
		return
	}
	certsMap := getMapFromCertList(l7.sslCerts)
	for _, cert := range l7.oldSSLCerts {
		if !l7.namer.IsCertNameForLB(cert.Name) && !l7.namer.IsLegacySSLCert(cert.Name) {
			// retain cert if it is managed by GCE(non-ingress)
			continue
		}
		if _, ok := certsMap[cert.Name]; ok {
			// cert found in current map
			continue
		}
		l7.logger.V(3).Info("Cleaning up old SSL Certificate", "certName", cert.Name)
		key, _ := l7.CreateKey(cert.Name)
		if certErr := utils.IgnoreHTTPNotFound(composite.DeleteSslCertificate(l7.cloud, key, l7.Versions().SslCertificate, l7.logger)); certErr != nil {
			l7.logger.Error(certErr, "Old cert delete failed", "certName", cert.Name)
		}
	}
}

// Returns true if the input array of certs is identical to the certs in the L7 config.
// Returns false if there is any mismatch
func (l7 *L7) compareCerts(certLinks []string) bool {
	certsMap := getMapFromCertList(l7.sslCerts)
	if len(certLinks) != len(certsMap) {
		l7.logger.V(4).Info("Loadbalancer and target proxy have different number of certs", "lbCertsCount", len(certsMap), "targetProxyCertsCount", len(certLinks))
		return false
	}

	for _, link := range certLinks {
		certName, err := utils.KeyName(link)
		if err != nil {
			l7.logger.Info("Cannot get cert name", "err", err)
			return false
		}

		if cert, ok := certsMap[certName]; !ok {
			l7.logger.V(4).Info("Cannot find cert in certsMap", "certName", certName, "certsMap", fmt.Sprintf("%+v", certsMap))
			return false
		} else if ok && !utils.EqualResourceIDs(link, cert.SelfLink) {
			l7.logger.V(4).Info("Selflink compare failed for certs", "lbSelfLink", cert.SelfLink, "targetProxySelfLink", link)
			return false
		}
	}
	return true
}

func toCertNames(certs []*composite.SslCertificate) (names []string) {
	for _, v := range certs {
		names = append(names, v.Name)
	}
	return names
}
