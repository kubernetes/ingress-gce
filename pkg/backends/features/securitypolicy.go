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

package features

import (
	"fmt"

	"k8s.io/klog/v2"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/cloud-provider-gcp/providers/gce"

	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

// EnsureSecurityPolicy ensures the security policy link on backend service.
// TODO(mrhohn): Emit event when attach/detach security policy to backend service.
func EnsureSecurityPolicy(cloud *gce.Cloud, sp utils.ServicePort, be *composite.BackendService) error {
	// It is too dangerous to remove user's security policy that may have been
	// configured via the UI or gcloud directly rather than via Kubernetes.
	// Treat nil security policy -> ignored
	// Treat empty string security policy name -> remove
	if sp.BackendConfig.Spec.SecurityPolicy == nil {
		klog.V(2).Infof("Ignoring nil Security Policy on backend service %s (%s:%s)", be.Name, sp.ID.Service.String(), sp.ID.Port.String())
		return nil
	}

	if be.Scope != meta.Global {
		err := fmt.Errorf("cloud armor security policies not supported for %s backend service %s", be.Scope, be.Name)
		klog.Errorf("EnsureSecurityPolicy() = %v", err)
		return err
	}

	existingPolicyName, err := utils.KeyName(be.SecurityPolicy)
	// The parser returns error for empty values.
	if be.SecurityPolicy != "" && err != nil {
		err := fmt.Errorf("failed to parse existing security policy name %q: %v", existingPolicyName, err)
		klog.Errorf("EnsureSecurityPolicy() = %v", err)
		return err
	}

	desiredPolicyName := sp.BackendConfig.Spec.SecurityPolicy.Name
	klog.V(2).Infof("Current security policy: %q, desired security policy: %q", existingPolicyName, desiredPolicyName)
	if existingPolicyName == desiredPolicyName {
		klog.V(2).Infof("SecurityPolicy on backend service is not changed %s (%s:%s): %q", be.Name, sp.ID.Service.String(), sp.ID.Port.String(), desiredPolicyName)
		return nil
	}

	if desiredPolicyName != "" {
		klog.V(2).Infof("Set security policy in backend service %s (%s:%s) from %q to %q", be.Name, sp.ID.Service.String(), sp.ID.Port.String(), existingPolicyName, desiredPolicyName)
		if err := composite.SetSecurityPolicy(cloud, be, desiredPolicyName); err != nil {
			err := fmt.Errorf("failed to set security policy from %q to %q for backend service %s (%s:%s): %v", existingPolicyName, desiredPolicyName, be.Name, sp.ID.Service.String(), sp.ID.Port.String(), err)
			klog.Errorf("SetSecurityPolicy() = %v", err)
			return err
		}
		klog.V(2).Infof("Successfully set security policy in backend service %s (%s:%s) from %q to %q", be.Name, sp.ID.Service.String(), sp.ID.Port.String(), existingPolicyName, desiredPolicyName)
		return nil
	}
	klog.V(2).Infof("Removing security policy %q in backend service %s (%s:%s)", existingPolicyName, be.Name, sp.ID.Service.String(), sp.ID.Port.String())
	if err := composite.SetSecurityPolicy(cloud, be, desiredPolicyName); err != nil {
		err := fmt.Errorf("failed to remove security policy %q for backend service %s (%s:%s): %v", existingPolicyName, be.Name, sp.ID.Service.String(), sp.ID.Port.String(), err)
		klog.Errorf("SetSecurityPolicy() = %v", err)
		return err
	}
	klog.V(2).Infof("Successfully removed security policy %q in backend service %s (%s:%s)", existingPolicyName, be.Name, sp.ID.Service.String(), sp.ID.Port.String())
	return nil
}
