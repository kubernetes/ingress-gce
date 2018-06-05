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
	"k8s.io/ingress-gce/pkg/utils"
)

func (l *L7) checkSSLCert() error {
	// Handle Pre-Shared cert and early return if used
	if used, err := l.usePreSharedCert(); used {
		return err
	}

	// Get updated value of certificate for comparison
	if err := l.populateSSLCert(); err != nil {
		return err
	}

	existingCertsMap := getMapfromCertList(l.sslCerts)
	l.oldSSLCerts = l.sslCerts
	l.sslCerts = make([]*compute.SslCertificate, 0)

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
				l.sslCerts = append(l.sslCerts, cert)
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
		l.sslCerts = append(l.sslCerts, cert)
	}

	// Save the old certs for cleanup after we update the target proxy.
	if len(failedCerts) > 0 {
		return fmt.Errorf("Cert creation failures - %s", strings.Join(failedCerts, ","))
	}
	return nil
}

func (l *L7) usePreSharedCert() (bool, error) {
	// Use the named GCE cert when it is specified by the annotation.
	preSharedCertName := l.runtimeInfo.TLSName
	if preSharedCertName == "" {
		return false, nil
	}
	preSharedCerts := strings.Split(preSharedCertName, ",")
	if len(preSharedCerts) > TargetProxyCertLimit {
		glog.Warningf("Specified %d preshared certs, limit is %d, rest will be ignored",
			len(preSharedCerts), TargetProxyCertLimit)
		preSharedCerts = preSharedCerts[:TargetProxyCertLimit]
	}

	l.sslCerts = make([]*compute.SslCertificate, 0, len(preSharedCerts))
	var failedCerts []string

	for _, sharedCert := range preSharedCerts {
		// Ask GCE for the cert, checking for problems and existence.
		sharedCert = strings.TrimSpace(sharedCert)
		cert, err := l.cloud.GetSslCertificate(sharedCert)
		if err != nil {
			failedCerts = append(failedCerts, sharedCert+" Error: "+err.Error())
			continue
		}
		if cert == nil {
			failedCerts = append(failedCerts, sharedCert+" Error: unable to find existing sslCertificate")
			continue
		}

		glog.V(2).Infof("Using existing sslCertificate %v for %v", sharedCert, l.Name)
		l.sslCerts = append(l.sslCerts, cert)
	}
	if len(failedCerts) != 0 {
		return true, fmt.Errorf("PreSharedCert errors - %s", strings.Join(failedCerts, ","))
	}
	return true, nil
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

func (l *L7) populateSSLCert() error {
	l.sslCerts = make([]*compute.SslCertificate, 0)
	// Currently we list all certs available in gcloud and filter the ones managed by this loadbalancer instance. This is
	// to make sure we garbage collect any old certs that this instance might have lost track of due to crashes.
	// Can be a performance issue if there are too many global certs, default quota is only 10.
	certs, err := l.cloud.ListSslCertificates()
	if err != nil {
		return utils.IgnoreHTTPNotFound(err)
	}
	for _, c := range certs {
		if l.namer.IsCertUsedForLB(l.Name, c.Name) {
			glog.V(4).Infof("Populating ssl cert %s for l7 %s", c.Name, l.Name)
			l.sslCerts = append(l.sslCerts, c)
		}
	}
	if len(l.sslCerts) == 0 {
		// Check for legacy cert since that follows a different naming convention
		glog.V(4).Infof("Looking for legacy ssl certs")
		expectedCertLinks := l.getSslCertLinkInUse()
		for _, link := range expectedCertLinks {
			// Retrieve the certificate and ignore error if certificate wasn't found
			name, err := utils.KeyName(link)
			if err != nil {
				glog.Warningf("error getting certificate name: %v", link)
				continue
			}

			if !l.namer.IsLegacySSLCert(l.Name, name) {
				continue
			}
			cert, _ := l.cloud.GetSslCertificate(name)
			if cert != nil {
				glog.V(4).Infof("Populating legacy ssl cert %s for l7 %s", cert.Name, l.Name)
				l.sslCerts = append(l.sslCerts, cert)
			}
		}
	}
	return nil
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
			glog.Warningf("Cannot get cert name from URL: %v", link)
			return false
		}

		if cert, ok := certsMap[certName]; !ok {
			glog.V(4).Infof("Cannot find cert with name %s in certsMap %+v", certName, certsMap)
			return false
		} else if ok && !utils.EqualResourceID(link, cert.SelfLink) {
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
