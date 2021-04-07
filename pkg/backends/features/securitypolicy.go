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

	"k8s.io/klog"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/legacy-cloud-providers/gce"

	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

// EnsureSecurityPolicy ensures the security policy link on backend service.
// TODO(mrhohn): Emit event when attach/detach security policy to backend service.
func EnsureSecurityPolicy(cloud *gce.Cloud, sp utils.ServicePort, be *composite.BackendService) error {
	if sp.BackendConfig.Spec.SecurityPolicy == nil {
		return nil
	}

	if be.Scope != meta.Global {
		return fmt.Errorf("cloud armor security policies not supported for %s backend service %s", be.Scope, be.Name)
	}

	existingPolicyName, err := utils.KeyName(be.SecurityPolicy)
	// The parser returns error for empty values.
	if be.SecurityPolicy != "" && err != nil {
		return err
	}
	desiredPolicyName := sp.BackendConfig.Spec.SecurityPolicy.Name
	if existingPolicyName == desiredPolicyName {
		return nil
	}

	klog.V(2).Infof("Set security policy in backend service %s (%s:%s) to %q", be.Name, sp.ID.Service.String(), sp.ID.Port.String(), desiredPolicyName)
	if err := composite.SetSecurityPolicy(cloud, be, desiredPolicyName); err != nil {
		return fmt.Errorf("failed to set security policy %q for backend service %s (%s:%s): %v", desiredPolicyName, be.Name, sp.ID.Service.String(), sp.ID.Port.String(), err)
	}
	return nil
}
