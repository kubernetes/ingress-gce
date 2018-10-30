/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package translate translates SslCertificate object to ManagedCertificate object.
package translate

import (
	"fmt"

	compute "google.golang.org/api/compute/v0.alpha"

	api "github.com/GoogleCloudPlatform/gke-managed-certs/pkg/apis/gke.googleapis.com/v1alpha1"
)

const (
	sslActive                              = "ACTIVE"
	sslFailedNotVisible                    = "FAILED_NOT_VISIBLE"
	sslFailedCaaChecking                   = "FAILED_CAA_CHECKING"
	sslFailedCaaForbidden                  = "FAILED_CAA_FORBIDDEN"
	sslFailedRateLimited                   = "FAILED_RATE_LIMITED"
	sslManagedCertificateStatusUnspecified = "MANAGED_CERTIFICATE_STATUS_UNSPECIFIED"
	sslProvisioning                        = "PROVISIONING"
	sslProvisioningFailed                  = "PROVISIONING_FAILED"
	sslProvisioningFailedPermanently       = "PROVISIONING_FAILED_PERMANENTLY"
	sslRenewalFailed                       = "RENEWAL_FAILED"
)

// Certificate translates an SslCertificate object to ManagedCertificate object.
func Certificate(sslCert compute.SslCertificate, mcrt *api.ManagedCertificate) error {
	status, err := status(sslCert.Managed.Status)
	if err != nil {
		return fmt.Errorf("Failed to translate status of SslCertificate %v, err: %s", sslCert, err.Error())
	}
	mcrt.Status.CertificateStatus = status

	// Initialize with non-nil value to avoid ManagedCertificate CRD validation warnings
	domainStatuses := make([]api.DomainStatus, 0)
	for domain, status := range sslCert.Managed.DomainStatus {
		translatedStatus, err := domainStatus(status)
		if err != nil {
			return err
		}

		domainStatuses = append(domainStatuses, api.DomainStatus{
			Domain: domain,
			Status: translatedStatus,
		})
	}
	mcrt.Status.DomainStatus = domainStatuses

	mcrt.Status.CertificateName = sslCert.Name
	mcrt.Status.ExpireTime = sslCert.ExpireTime

	return nil
}

// domainStatus translates an SslCertificate domain status to ManagedCertificate domain status.
func domainStatus(status string) (string, error) {
	switch status {
	case sslProvisioning:
		return "Provisioning", nil
	case sslFailedNotVisible:
		return "FailedNotVisible", nil
	case sslFailedCaaChecking:
		return "FailedCaaChecking", nil
	case sslFailedCaaForbidden:
		return "FailedCaaForbidden", nil
	case sslFailedRateLimited:
		return "FailedRateLimited", nil
	case sslActive:
		return "Active", nil
	default:
		return "", fmt.Errorf("Unexpected status %s", status)
	}
}

// status translates an SslCertificate status to ManagedCertificate status.
func status(status string) (string, error) {
	switch status {
	case sslActive:
		return "Active", nil
	case sslManagedCertificateStatusUnspecified, "":
		return "", nil
	case sslProvisioning:
		return "Provisioning", nil
	case sslProvisioningFailed:
		return "ProvisioningFailed", nil
	case sslProvisioningFailedPermanently:
		return "ProvisioningFailedPermanently", nil
	case sslRenewalFailed:
		return "RenewalFailed", nil
	default:
		return "", fmt.Errorf("Unexpected status %s", status)
	}
}
