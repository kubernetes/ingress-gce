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
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
)

// serviceNEGLinks returns the keys of the NEGs of the service, read from the
// ServiceNetworkEndpointGroup of the service. It returns nil if the
// ServiceNetworkEndpointGroup does not exist.
func (c *Controller) serviceNEGLinks(svc *v1.Service) (sets.Set[cloud.ResourceMapKey], error) {
	if svc == nil {
		return sets.New[cloud.ResourceMapKey](), nil
	}
	negName, err := serviceNEGName(svc, c.namer)
	if err != nil {
		return nil, err
	}
	if c.ctx.SvcNegInformer == nil {
		return sets.New[cloud.ResourceMapKey](), nil
	}

	obj, exists, err := c.ctx.SvcNegInformer.GetIndexer().GetByKey(fmt.Sprintf("%s/%s", svc.Namespace, negName))
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	svcNeg, ok := obj.(*negv1beta1.ServiceNetworkEndpointGroup)
	if !ok || svcNeg == nil {
		return nil, fmt.Errorf("failed to retrieve ServiceNetworkEndpointGroup")
	}
	return serviceNEGKeys(svc, svcNeg)
}

// serviceNEGName returns the name of the ServiceNetworkEndpointGroup of the
// service, which is also the name of its NEGs.
func serviceNEGName(svc *v1.Service, l4Namer namer.L4ResourcesNamer) (string, error) {
	if flags.F.EnableL4CustomStandaloneNEGNames {
		customName, err := annotations.StandaloneNEGName(svc)
		if err != nil {
			return "", l4utils.NewUserError(err)
		}
		if customName != "" {
			return customName, nil
		}
	}
	return l4Namer.L4Backend(svc.Namespace, svc.Name), nil
}

// serviceNEGKeys returns the keys of the NEGs listed in the status of the
// ServiceNetworkEndpointGroup. The ServiceNetworkEndpointGroup must be
// controlled by the service.
func serviceNEGKeys(svc *v1.Service, svcNeg *negv1beta1.ServiceNetworkEndpointGroup) (sets.Set[cloud.ResourceMapKey], error) {
	if !metav1.IsControlledBy(svcNeg, svc) {
		err := fmt.Errorf("retrieved ServiceNetworkEndpointGroup %s is not controller by the service %s", svcNeg.Name, svc.Name)
		return nil, l4utils.NewUserError(err)
	}
	negKeys := sets.New[cloud.ResourceMapKey]()
	for _, negRef := range svcNeg.Status.NetworkEndpointGroups {
		if negRef.SelfLink == "" {
			continue
		}
		negID, err := cloud.ParseResourceURL(negRef.SelfLink)
		if err != nil {
			continue
		}
		negKeys.Insert(negID.MapKey())
	}
	return negKeys, nil
}
