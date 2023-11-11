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

package features

import (
	"crypto/sha256"
	"fmt"

	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// EnsureIAP reads the IAP configuration specified in the BackendConfig
// and applies it to the BackendService if it is stale. It returns true
// if there were existing settings on the BackendService that were overwritten.
func EnsureIAP(sp utils.ServicePort, be *composite.BackendService, logger klog.Logger) bool {
	// TODO: Update when context logging is enabled to ensure no duplicate keys
	logger = logger.WithName("EnsureIAP").WithValues("service", klog.KRef(sp.ID.Service.Namespace, sp.ID.Service.Name))
	if sp.BackendConfig.Spec.Iap == nil {
		return false
	}
	beTemp := &composite.BackendService{}
	applyIAPSettings(sp, beTemp)
	if diffIAP(beTemp, be, logger) {
		applyIAPSettings(sp, be)
		logger.Info("Updated IAP settings")
		return true
	}
	logger.Info("Detected no change in IAP Settings")
	return false
}

// applyIAPSettings applies the IAP settings specified in the BackendConfig
// to the passed in compute.BackendService. A GCE API call still needs to be
// made to actually persist the changes.
func applyIAPSettings(sp utils.ServicePort, be *composite.BackendService) {
	beConfig := sp.BackendConfig
	// Apply the boolean switch
	be.Iap = &composite.BackendServiceIAP{Enabled: beConfig.Spec.Iap.Enabled}

	if beConfig.Spec.Iap.OAuthClientCredentials != nil {
		// Apply the OAuth credentials
		be.Iap.Oauth2ClientId = beConfig.Spec.Iap.OAuthClientCredentials.ClientID
		be.Iap.Oauth2ClientSecret = beConfig.Spec.Iap.OAuthClientCredentials.ClientSecret
	} else {
		be.Iap.Oauth2ClientId = ""
		be.Iap.Oauth2ClientSecret = ""
	}
}

// diffIAP logs the diff between desired and current and returns true if any diff exists
func diffIAP(desired, curr *composite.BackendService, logger klog.Logger) bool {
	if curr.Iap == nil {
		logger.Info("IAP settings changing from nil policy to new policy")
		return true
	}

	if curr.Iap.Enabled != desired.Iap.Enabled {
		logger.Info("Iap `enabled` setting changed: ", "from", curr.Iap.Enabled, "to", desired.Iap.Enabled)
		return true
	}

	// We need to compare the SHA256 of the client secret instead of the client secret itself
	// since that field is redacted when getting a BackendService.
	desired.Iap.Oauth2ClientSecretSha256 = fmt.Sprintf("%x", sha256.Sum256([]byte(desired.Iap.Oauth2ClientSecret)))
	if curr.Iap.Oauth2ClientId != desired.Iap.Oauth2ClientId || curr.Iap.Oauth2ClientSecretSha256 != desired.Iap.Oauth2ClientSecretSha256 {
		if curr.Iap.Oauth2ClientId == "" {
			logger.Info("IAP Credentials are switching from default to credentials")
		} else {
			logger.Info("Iap Credentials are being updated")
		}

		return true
	}
	return false
}
