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

package backendconfig

import (
	"context"
	"fmt"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
)

const (
	OAuthClientIDKey      = "client_id"
	OAuthClientSecretKey  = "client_secret"
	SignedUrlKeySecretKey = "key_value"
)

var supportedAffinities = map[string]bool{
	"NONE":             true,
	"CLIENT_IP":        true,
	"GENERATED_COOKIE": true,
}

func Validate(kubeClient kubernetes.Interface, beConfig *backendconfigv1.BackendConfig) error {
	if beConfig == nil {
		return nil
	}

	if err := validateIAP(kubeClient, beConfig); err != nil {
		return err
	}

	if err := validateCDN(kubeClient, beConfig); err != nil {
		return err
	}

	if err := validateSessionAffinity(kubeClient, beConfig); err != nil {
		return err
	}

	if err := validateLogging(beConfig); err != nil {
		return err
	}

	return nil
}

// TODO(rramkumar): Return errors as constants so that the unit tests can distinguish
// between which error is returned.
func validateIAP(kubeClient kubernetes.Interface, beConfig *backendconfigv1.BackendConfig) error {
	// If IAP settings are not found or IAP is not enabled then don't bother continuing.
	if beConfig.Spec.Iap == nil || beConfig.Spec.Iap.Enabled == false {
		return nil
	}
	// If necessary, get the OAuth credentials stored in the K8s secret.
	if beConfig.Spec.Iap.OAuthClientCredentials != nil && beConfig.Spec.Iap.OAuthClientCredentials.SecretName != "" {
		secretName := beConfig.Spec.Iap.OAuthClientCredentials.SecretName
		secret, err := kubeClient.CoreV1().Secrets(beConfig.Namespace).Get(context.TODO(), secretName, meta_v1.GetOptions{})
		if err != nil {
			return fmt.Errorf("error retrieving secret %v: %v", secretName, err)
		}
		clientID, ok := secret.Data[OAuthClientIDKey]
		if !ok {
			return fmt.Errorf("secret %v missing %v data", secretName, OAuthClientIDKey)
		}
		clientSecret, ok := secret.Data[OAuthClientSecretKey]
		if !ok {
			return fmt.Errorf("secret %v missing %v data'", secretName, OAuthClientSecretKey)
		}
		beConfig.Spec.Iap.OAuthClientCredentials.ClientID = string(clientID)
		beConfig.Spec.Iap.OAuthClientCredentials.ClientSecret = string(clientSecret)
	}

	if beConfig.Spec.Cdn != nil && beConfig.Spec.Cdn.Enabled {
		return fmt.Errorf("iap and cdn cannot be enabled at the same time")
	}
	return nil
}

func validateSessionAffinity(kubeClient kubernetes.Interface, beConfig *backendconfigv1.BackendConfig) error {
	if beConfig.Spec.SessionAffinity == nil {
		return nil
	}

	if beConfig.Spec.SessionAffinity.AffinityType != "" {
		if _, ok := supportedAffinities[beConfig.Spec.SessionAffinity.AffinityType]; !ok {
			return fmt.Errorf("unsupported AffinityType: %s, should be one of NONE, CLIENT_IP, or GENERATED_COOKIE",
				beConfig.Spec.SessionAffinity.AffinityType)
		}
	}

	if beConfig.Spec.SessionAffinity.AffinityCookieTtlSec != nil {
		if *beConfig.Spec.SessionAffinity.AffinityCookieTtlSec < 0 || *beConfig.Spec.SessionAffinity.AffinityCookieTtlSec > 86400 {
			return fmt.Errorf("unsupported AffinityCookieTtlSec: %d, should be between 0 and 86400",
				*beConfig.Spec.SessionAffinity.AffinityCookieTtlSec)
		}
	}

	return nil
}

func validateLogging(beConfig *backendconfigv1.BackendConfig) error {
	if beConfig.Spec.Logging == nil || beConfig.Spec.Logging.SampleRate == nil {
		return nil
	}

	if *beConfig.Spec.Logging.SampleRate < 0.0 || *beConfig.Spec.Logging.SampleRate > 1.0 {
		return fmt.Errorf("unsupported SampleRate: %f, should be between 0.0 and 1.0",
			*beConfig.Spec.Logging.SampleRate)
	}

	return nil
}

func validateCDN(kubeClient kubernetes.Interface, beConfig *backendconfigv1.BackendConfig) error {
	if beConfig.Spec.Cdn == nil || beConfig.Spec.Cdn.Enabled == false {
		return nil
	}

	for _, key := range beConfig.Spec.Cdn.SignedUrlKeys {
		if key.SecretName != "" {
			secret, err := kubeClient.CoreV1().Secrets(beConfig.Namespace).Get(context.TODO(), key.SecretName, meta_v1.GetOptions{})
			if err != nil {
				return fmt.Errorf("error retrieving secret %v: %v", key.SecretName, err)
			}
			keyValue, ok := secret.Data[SignedUrlKeySecretKey]
			if !ok {
				return fmt.Errorf("secret %v missing %v data", key.SecretName, SignedUrlKeySecretKey)
			}
			key.KeyValue = string(keyValue)
		}
	}

	return nil
}
