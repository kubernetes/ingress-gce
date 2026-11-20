/*
Copyright 2026 The Kubernetes Authors.

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

package standaloneneg

import (
	"fmt"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
)

// serviceNEGs are the NEGs of the service, read from the GCE_VM_IP
// ServiceNetworkEndpointGroups controlled by the service.
type serviceNEGs struct {
	// active are the NEGs synced by the NEG controller.
	active sets.Set[cloud.ResourceMapKey]
	// draining are the NEGs that still exist but are no longer synced by the
	// NEG controller: the NEGs of ServiceNetworkEndpointGroups being deleted,
	// e.g. after the NEG name of the service changed, and the NEGs in the
	// TO_BE_DELETED state, e.g. of a removed subnet. They are accepted, with a
	// warning, to allow switching the load balancer to the new NEGs without
	// disruption.
	draining sets.Set[cloud.ResourceMapKey]
	// multipleActiveSvcNegs is set if the service has multiple
	// ServiceNetworkEndpointGroups that are not being deleted. Only one of them
	// is synced, the ones of a previous NEG name are not always marked as being
	// deleted, e.g. if the NEG controller restarted while the NEG name changed.
	multipleActiveSvcNegs bool
}

// listServiceNEGs returns the NEGs of the service, read from the GCE_VM_IP
// ServiceNetworkEndpointGroups controlled by the service. The
// ServiceNetworkEndpointGroups are found by ownership rather than by name, so
// that the NEG naming (e.g. custom NEG names) stays within the NEG controller.
func (c *Controller) listServiceNEGs(svc *v1.Service) (serviceNEGs, error) {
	negs := serviceNEGs{
		active:   sets.New[cloud.ResourceMapKey](),
		draining: sets.New[cloud.ResourceMapKey](),
	}
	objs, err := c.ctx.SvcNegInformer.GetIndexer().ByIndex(negtypes.SvcNegServiceIndex, negtypes.SvcNegServiceIndexKey(svc.Namespace, svc.Name))
	if err != nil {
		return negs, fmt.Errorf("failed to list the ServiceNetworkEndpointGroups of the service: %w", err)
	}
	numActiveSvcNegs := 0
	for _, obj := range objs {
		svcNeg, ok := obj.(*negv1beta1.ServiceNetworkEndpointGroup)
		if !ok || !isServiceVMIPSvcNeg(svc, svcNeg) {
			continue
		}
		svcNegDeleted := !svcNeg.DeletionTimestamp.IsZero()
		if !svcNegDeleted {
			numActiveSvcNegs++
		}
		for _, negRef := range svcNeg.Status.NetworkEndpointGroups {
			negKey, ok := vmIPNEGKey(negRef)
			if !ok {
				continue
			}
			if svcNegDeleted || negRef.State == negv1beta1.ToBeDeletedState {
				negs.draining.Insert(negKey)
			} else {
				negs.active.Insert(negKey)
			}
		}
	}
	negs.multipleActiveSvcNegs = numActiveSvcNegs > 1
	return negs, nil
}

// isServiceVMIPSvcNeg checks if the ServiceNetworkEndpointGroup is of GCE_VM_IP
// NEGs and is controlled by the service. The owner reference UID is checked,
// so ServiceNetworkEndpointGroups of a previous service with the same name are
// not matched.
func isServiceVMIPSvcNeg(svc *v1.Service, svcNeg *negv1beta1.ServiceNetworkEndpointGroup) bool {
	return metav1.IsControlledBy(svcNeg, svc) && svcNeg.Labels[negtypes.NegCRServicePortKey] == negtypes.NegCRServicePortVmIp
}

// vmIPNEGKey returns the key of the NEG of the reference. References that are
// invalid or of another NEG type are skipped.
func vmIPNEGKey(negRef negv1beta1.NegObjectReference) (cloud.ResourceMapKey, bool) {
	if negRef.NetworkEndpointType != "" && negRef.NetworkEndpointType != negv1beta1.VmIpEndpointType {
		return cloud.ResourceMapKey{}, false
	}
	negID, err := cloud.ParseResourceURL(negRef.SelfLink)
	if err != nil || negID.Key == nil {
		return cloud.ResourceMapKey{}, false
	}
	return negID.MapKey(), true
}
