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

	"github.com/golang/glog"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

// EnsureIAP reads the IAP configuration specified in the BackendConfig
// and applies it to the BackendService if it is stale. It returns true
// if there were existing settings on the BackendService that were overwritten.
func EnsureIAP(sp utils.ServicePort, be *composite.BackendService) bool {
	if sp.BackendConfig.Spec.Iap == nil {
		return false
	}
	beTemp := &composite.BackendService{}
	applyIAPSettings(sp, beTemp)
	// We need to compare the SHA256 of the client secret instead of the client secret itself
	// since that field is redacted when getting a BackendService.
	beTemp.Iap.Oauth2ClientSecretSha256 = fmt.Sprintf("%x", sha256.Sum256([]byte(beTemp.Iap.Oauth2ClientSecret)))
	if be.Iap == nil || beTemp.Iap.Enabled != be.Iap.Enabled || beTemp.Iap.Oauth2ClientId != be.Iap.Oauth2ClientId || beTemp.Iap.Oauth2ClientSecretSha256 != be.Iap.Oauth2ClientSecretSha256 {
		applyIAPSettings(sp, be)
		glog.V(2).Infof("Updated IAP settings for service %v/%v.", sp.ID.Service.Namespace, sp.ID.Service.Name)
		return true
	}
	return false
}

// applyIAPSettings applies the IAP settings specified in the BackendConfig
// to the passed in compute.BackendService. A GCE API call still needs to be
// made to actually persist the changes.
func applyIAPSettings(sp utils.ServicePort, be *composite.BackendService) {
	beConfig := sp.BackendConfig
	// Apply the boolean switch
	be.Iap = &composite.BackendServiceIAP{Enabled: beConfig.Spec.Iap.Enabled}
	// Apply the OAuth credentials
	be.Iap.Oauth2ClientId = beConfig.Spec.Iap.OAuthClientCredentials.ClientID
	be.Iap.Oauth2ClientSecret = beConfig.Spec.Iap.OAuthClientCredentials.ClientSecret
}
