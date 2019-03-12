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
	"fmt"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	backendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
)

// TODO(rramkumar): Per-feature validation should be moved to the feature-specific files (e.g pkg/backends/features/cdn.go)

const (
	OAuthClientIDKey     = "client_id"
	OAuthClientSecretKey = "client_secret"
)

var (
	supportedAffinities = map[string]bool{
		"NONE":             true,
		"CLIENT_IP":        true,
		"GENERATED_COOKIE": true,
	}

	supportedHealthChecks = map[string]bool{
		"HTTP":  true,
		"HTTPS": true,
		"HTTP2": true,
	}
)

func Validate(kubeClient kubernetes.Interface, beConfig *backendconfigv1beta1.BackendConfig) error {
	if beConfig == nil {
		return nil
	}

	if err := validateIAP(kubeClient, beConfig); err != nil {
		return err
	}

	if err := validateSessionAffinity(kubeClient, beConfig); err != nil {
		return err
	}

	if err := validateHealthCheck(kubeClient, beConfig); err != nil {
		return err
	}

	return nil
}

// TODO(rramkumar): Return errors as constants so that the unit tests can distinguish
// between which error is returned.
func validateIAP(kubeClient kubernetes.Interface, beConfig *backendconfigv1beta1.BackendConfig) error {
	// If IAP settings are not found or IAP is not enabled then don't bother continuing.
	if beConfig.Spec.Iap == nil || beConfig.Spec.Iap.Enabled == false {
		return nil
	}
	// If necessary, get the OAuth credentials stored in the K8s secret.
	if beConfig.Spec.Iap.OAuthClientCredentials != nil && beConfig.Spec.Iap.OAuthClientCredentials.SecretName != "" {
		secretName := beConfig.Spec.Iap.OAuthClientCredentials.SecretName
		secret, err := kubeClient.Core().Secrets(beConfig.Namespace).Get(secretName, meta_v1.GetOptions{})
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

func validateSessionAffinity(kubeClient kubernetes.Interface, beConfig *backendconfigv1beta1.BackendConfig) error {
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

// TODO(rramkumar): When we migrate to kubebuilder this sort of validation will be done in the Go type definition.
func validateHealthCheck(kubeClient kubernetes.Interface, beConfig *backendconfigv1beta1.BackendConfig) error {
	if beConfig.Spec.HealthCheck == nil {
		return nil
	}

	hcType := beConfig.Spec.HealthCheck.Type
	if hcType != nil  {
		if _, ok := supportedHealthChecks[*hcType]; !ok {
			return fmt.Errorf("unsupported health check type: %s", *hcType)
		}
	}

	port := beConfig.Spec.HealthCheck.Port
	if port != nil {
		if *port <= 0 || *port > 65535 {
			return fmt.Errorf("health check port must be between 0 and 65535, got %d", *port)
		}
	}

	healthyThreshold := beConfig.Spec.HealthCheck.HealthyThreshold
	if healthyThreshold != nil {
		if *healthyThreshold <= 0 || *healthyThreshold > 10 {
			return fmt.Errorf("health check healthiness threshold must be greater than 0 and less than 11, got %d", *healthyThreshold)
		}
	}

	unhealthyThreshold := beConfig.Spec.HealthCheck.UnhealthyThreshold
	if unhealthyThreshold != nil {
		if *unhealthyThreshold <= 0 || *unhealthyThreshold > 10 {
			return fmt.Errorf("health check unhealthiness threshold must be greater than 0 and less than 11, got %d", *unhealthyThreshold)
		}
	}

	timeout := beConfig.Spec.HealthCheck.TimeoutSec
	if timeout != nil {
		if *timeout <= 0 {
			return fmt.Errorf("health check timeout %d must be greater than 0", *timeout)
		}
	}

	checkInterval := beConfig.Spec.HealthCheck.CheckIntervalSec
	if checkInterval != nil {
		if *checkInterval <= 0 || *checkInterval > 300 {
			return fmt.Errorf("health check interval must be greater than 0 and less than or equal to 300, got %d", *checkInterval)
		}
	}

	return nil
}
