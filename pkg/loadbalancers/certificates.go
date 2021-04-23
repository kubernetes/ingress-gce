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
	"k8s.io/klog"
)

const SslCertificateMissing = "SslCertificateMissing"

func (l *L7) checkSSLCert() error {
	isL7ILB := utils.IsGCEL7ILBIngress(l.runtimeInfo.Ingress)
	tr := translator.NewTranslator(isL7ILB, l.namer)
	env := &translator.Env{Region: l.cloud.Region(), Project: l.cloud.ProjectID()}
	translatorCerts := tr.ToCompositeSSLCertificates(env, l.runtimeInfo.TLSName, l.runtimeInfo.TLS, l.Versions().SslCertificate)

	// Use both pre-shared and secret-based certs if available,
	// combining encountered errors.
	errs := []error{}

	// Get updated value of certificate for comparison
	existingSecretsSslCerts, err := l.getIngressManagedSslCerts()
	if err != nil {
		errs = append(errs, err)
		// Do not continue if getIngressManagedSslCerts() failed.
		return utils.JoinErrs(errs)
	}

	l.oldSSLCerts = existingSecretsSslCerts
	sslCerts, err := l.createSslCertificates(existingSecretsSslCerts, translatorCerts)
	if err != nil {
		errs = append(errs, err)
	}
	l.sslCerts = sslCerts
	if len(errs) > 0 {
		return utils.JoinErrs(errs)
	}
	return nil
}

// createSslCertificates creates SslCertificates based on kubernetes secrets in Ingress configuration.
func (l *L7) createSslCertificates(existingCerts, translatorCerts []*composite.SslCertificate) ([]*composite.SslCertificate, error) {
	var result []*composite.SslCertificate

	existingCertsMap := getMapfromCertList(existingCerts)

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
			klog.V(3).Infof("Secret cert %q has a certificate already used by %v", translatorCert.Certificate, addedBy)
			continue
		}

		// PrivateKey is write only, so compare certs alone. We're assuming that
		// no one will change just the key. We can remember the key and compare,
		// but a bug could end up leaking it, which feels worse.
		// If the cert contents have changed, its hash would be different, so would be the cert name. So it is enough
		// to check if this cert name exists in the map.
		if existingCertsMap != nil {
			if cert, ok := existingCertsMap[translatorCert.Name]; ok {
				klog.V(3).Infof("Secret cert %q already exists as certificate %q", cert.Certificate, cert.Name)
				visitedCertMap[translatorCert.Name] = fmt.Sprintf("certificate:%q", translatorCert.Name)
				result = append(result, cert)
				continue
			}
		}
		// Controller needs to create the certificate, no need to check if it exists and delete. If it did exist, it
		// would have been listed in the populateSSLCert function and matched in the check above.
		klog.V(2).Infof("Creating new sslCertificate %q for LB %q", translatorCert.Name, l)
		translatorCert.Version = l.Versions().SslCertificate
		key, err := l.CreateKey(translatorCert.Name)
		if err != nil {
			klog.Errorf("l.CreateKey(%s) = %v", translatorCert.Name, err)
			return nil, err
		}
		err = composite.CreateSslCertificate(l.cloud, key, translatorCert)
		if err != nil {
			klog.Errorf("Failed to create new sslCertificate %q for %q - %v", translatorCert.Name, l, err)
			failedCerts = append(failedCerts, translatorCert.Name+" Error:"+err.Error())
			continue
		}
		visitedCertMap[translatorCert.Name] = fmt.Sprintf("secret cert:%q", translatorCert.Certificate)

		// Get SSLCert
		cert, err := composite.GetSslCertificate(l.cloud, key, translatorCert.Version)
		if err != nil {
			klog.Errorf("GetSslCertificate(_, %v, %v) = %v", key, translatorCert.Version, err)
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

func getMapfromCertList(certs []*composite.SslCertificate) map[string]*composite.SslCertificate {
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
func (l *L7) getIngressManagedSslCerts() ([]*composite.SslCertificate, error) {
	var result []*composite.SslCertificate

	// Currently we list all certs available in gcloud and filter the ones managed by this loadbalancer instance. This is
	// to make sure we garbage collect any old certs that this instance might have lost track of due to crashes.
	// Can be a performance issue if there are too many global certs, default quota is only 10.
	// Use an empty name parameter since we only care about the scope
	// TODO: (shance) refactor this so we don't need an empty arg
	key, err := l.CreateKey("")
	if err != nil {
		return nil, err
	}
	version := l.Versions().SslCertificate
	certs, err := composite.ListSslCertificates(l.cloud, key, version)
	if err != nil {
		return nil, err
	}
	for _, c := range certs {
		if l.namer.IsCertNameForLB(c.Name) {
			klog.V(4).Infof("Populating ssl cert %s for l7 %s", c.Name, l)
			result = append(result, c)
		}
	}
	if len(result) == 0 {
		// Check for legacy cert since that follows a different naming convention
		klog.V(4).Infof("Looking for legacy ssl certs")
		expectedCertLinks, err := l.getSslCertLinkInUse()
		if err != nil {
			// Return nil if target proxy doesn't exist.
			return nil, utils.IgnoreHTTPNotFound(err)
		}
		for _, link := range expectedCertLinks {
			// Retrieve the certificate and ignore error if certificate wasn't found
			name, err := utils.KeyName(link)
			if err != nil {
				klog.Warningf("error parsing cert name: %v", err)
				continue
			}

			if !l.namer.IsLegacySSLCert(name) {
				continue
			}
			key, err := l.CreateKey(name)
			if err != nil {
				return nil, err
			}
			cert, _ := composite.GetSslCertificate(l.cloud, key, version)
			if cert != nil {
				klog.V(4).Infof("Populating legacy ssl cert %s for l7 %s", cert.Name, l)
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
		if !l.namer.IsCertNameForLB(cert.Name) && !l.namer.IsLegacySSLCert(cert.Name) {
			// retain cert if it is managed by GCE(non-ingress)
			continue
		}
		if _, ok := certsMap[cert.Name]; ok {
			// cert found in current map
			continue
		}
		klog.V(3).Infof("Cleaning up old SSL Certificate %s", cert.Name)
		key, _ := l.CreateKey(cert.Name)
		if certErr := utils.IgnoreHTTPNotFound(composite.DeleteSslCertificate(l.cloud, key, l.Versions().SslCertificate)); certErr != nil {
			klog.Errorf("Old cert %s delete failed - %v", cert.Name, certErr)
		}
	}
}

// Returns true if the input array of certs is identical to the certs in the L7 config.
// Returns false if there is any mismatch
func (l *L7) compareCerts(certLinks []string) bool {
	certsMap := getMapfromCertList(l.sslCerts)
	if len(certLinks) != len(certsMap) {
		klog.V(4).Infof("Loadbalancer has %d certs, target proxy has %d certs", len(certsMap), len(certLinks))
		return false
	}

	for _, link := range certLinks {
		certName, err := utils.KeyName(link)
		if err != nil {
			klog.Warningf("Cannot get cert name: %v", err)
			return false
		}

		if cert, ok := certsMap[certName]; !ok {
			klog.V(4).Infof("Cannot find cert with name %s in certsMap %+v", certName, certsMap)
			return false
		} else if ok && !utils.EqualResourceIDs(link, cert.SelfLink) {
			klog.V(4).Infof("Selflink compare failed for certs - %s in loadbalancer, %s in targetproxy", cert.SelfLink, link)
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
