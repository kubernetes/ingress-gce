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
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	apisbackendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

const (
	OAuthClientIDKey     = "client_id"
	OAuthClientSecretKey = "client_secret"
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

type overridableSpec string

const (
	// These features are added in v1.8+ versions which means that a malformed spec
	// can be added for these in v1.7 and below. Upgrading to 1.8 with these
	// malformed resource will crash the ingress controller.
	// These values should match json tags in pkg/backendconfig/apis/v1/types.go
	customResourceHeaders overridableSpec = "customResourceHeaders"
	logging               overridableSpec = "logging"
	healthCheck           overridableSpec = "healthCheck"

	spec = "spec"
)

var groupVersionResource = schema.GroupVersionResource{
	Group:    apisbackendconfig.GroupName,
	Version:  "v1",
	Resource: "backendconfigs",
}

// OverrideUnsupportedSpec deletes the feature spec that may cause the ingress
// controller to crash.
// Usually these crashes should not occur but there are some unique sequence of
// steps that lead to a crash,
// 1. Create a BackendConfig resource with malformed spec for a unsupported
// feature. (Ex. custom resource headers in v1.6)
// 2. Upgrade to a ingress version that supports this feature. The malformed
// backendconfig resource will cause the generated client library to panic
// and crash the controller.
// Note that beginning v1.9 the unsupported fields are automatically dropped so
// we will not run into this problem.
func OverrideUnsupportedSpec(dynamicClient dynamic.Interface) error {
	unstructuredBCList, err := dynamicClient.Resource(groupVersionResource).List(context.TODO(), meta_v1.ListOptions{})
	if err != nil {
		return err
	}

	var errs []error
	for _, existingBC := range unstructuredBCList.Items {
		updatedBC := *existingBC.DeepCopy()
		bcKey := fmt.Sprintf("%s/%s", existingBC.GetNamespace(), existingBC.GetName())
		// All objects must have spec and the spec should be of map type.
		// Skipping in case of these unusual errors.
		bcSpec, ok := existingBC.Object[spec]
		if !ok {
			continue
		}
		typedBCSpec, ok := bcSpec.(map[string]interface{})
		if !ok {
			continue
		}

		var needsPatch bool
		for _, specKey := range []overridableSpec{customResourceHeaders, logging, healthCheck} {
			// Skip if overridable feature spec is not present.
			oSpec, ok := typedBCSpec[string(specKey)]
			if !ok {
				continue
			}

			marshalledSpec, err := json.Marshal(oSpec)
			if err != nil {
				klog.Warningf("Failed to marshall %s spec %v for backendconfig %s: %v", specKey, oSpec, bcKey, err)
				continue
			}

			switch specKey {
			case customResourceHeaders:
				err = json.Unmarshal(marshalledSpec, &backendconfigv1.CustomRequestHeadersConfig{})
			case logging:
				err = json.Unmarshal(marshalledSpec, &backendconfigv1.LogConfig{})
			case healthCheck:
				err = json.Unmarshal(marshalledSpec, &backendconfigv1.HealthCheckConfig{})
			default:
			}

			if err != nil {
				klog.V(0).Infof("Deleting malformed %s spec %s for backendconfig %s, unmarshall error: %v", specKey, marshalledSpec, bcKey, err)
				unstructured.RemoveNestedField(updatedBC.Object, spec, string(specKey))
				needsPatch = true
			}
		}

		if needsPatch {
			if err := patchUnstructuredResource(dynamicClient, existingBC, updatedBC); err != nil {
				errs = append(errs, fmt.Errorf("failed to patch backendconfig %s: %v", bcKey, err))
			}
		}
	}
	if errs != nil {
		return utils.JoinErrs(errs)
	}
	return nil
}

func patchUnstructuredResource(dynamicClient dynamic.Interface, old, new unstructured.Unstructured) error {
	oldData, err := json.Marshal(old.Object)
	if err != nil {
		return fmt.Errorf("failed to Marshal old object: %v", err)
	}

	newData, err := json.Marshal(new.Object)
	if err != nil {
		return fmt.Errorf("failed to Marshal new object: %v", err)
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return fmt.Errorf("failed to create json merge patch: %v", err)
	}

	if _, err := dynamicClient.Resource(groupVersionResource).Namespace(old.GetNamespace()).Patch(context.TODO(), old.GetName(), types.MergePatchType, patchBytes, meta_v1.PatchOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
