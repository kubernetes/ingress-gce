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

	computebeta "google.golang.org/api/compute/v0.beta"

	gcecloud "github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"

	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

// EnsureSecurityPolicy ensures the security policy link on backend service.
// TODO(mrhohn): Emit event when attach/detach security policy to backend service.
func EnsureSecurityPolicy(cloud *gce.Cloud, sp utils.ServicePort, be *composite.BackendService, beName string) error {
	if sp.BackendConfig.Spec.SecurityPolicy == nil {
		return nil
	}

	needsUpdate, policyRef := securityPolicyNeedsUpdate(cloud, be.SecurityPolicy, sp.BackendConfig.Spec.SecurityPolicy.Name)
	if !needsUpdate {
		return nil
	}

	klog.V(2).Infof("Setting security policy %q for backend service %s (%s:%s)", policyRef, beName, sp.ID.Service.String(), sp.ID.Port.String())
	if err := cloud.SetSecurityPolicyForBetaGlobalBackendService(beName, policyRef); err != nil {
		return fmt.Errorf("failed to set security policy %q for backend service %s (%s:%s): %v", policyRef, beName, sp.ID.Service.String(), sp.ID.Port.String(), err)
	}
	return nil
}

// securityPolicyNeedsUpdate checks if security policy needs update and
// returns the desired policy reference.
func securityPolicyNeedsUpdate(cloud *gce.Cloud, currentLink, desiredName string) (bool, *computebeta.SecurityPolicyReference) {
	currentName, _ := utils.KeyName(currentLink)
	if currentName == desiredName {
		return false, nil
	}
	var policyRef *computebeta.SecurityPolicyReference
	if desiredName != "" {
		policyRef = &computebeta.SecurityPolicyReference{
			SecurityPolicy: gcecloud.SelfLink(meta.VersionBeta, cloud.ProjectID(), "securityPolicies", meta.GlobalKey(desiredName)),
		}
	}
	return true, policyRef
}
