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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// lbResourceCache stores the results of GCE resource retrievals done during
// a single sync, so that resources shared by multiple forwarding rules are
// retrieved only once. Errors are cached as well.
type lbResourceCache struct {
	// backendServices is keyed by the backend service URL.
	backendServices map[string]backendServiceResult
	// hcPorts is keyed by the health check URL.
	hcPorts map[string]hcPortResult
}

type backendServiceResult struct {
	bs  *composite.BackendService
	err error
}

type hcPortResult struct {
	port int64
	ok   bool
	err  error
}

func newLBResourceCache() *lbResourceCache {
	return &lbResourceCache{
		backendServices: make(map[string]backendServiceResult),
		hcPorts:         make(map[string]hcPortResult),
	}
}

func (c *Controller) forwardingRule(frRawName string, key *meta.Key, svcLogger klog.Logger) (*composite.ForwardingRule, error) {
	fr, err := composite.GetForwardingRule(c.ctx.Cloud, key, meta.VersionBeta, svcLogger)
	if err != nil {
		if utils.IsNotFoundError(err) {
			svcLogger.Error(err, "failed to get forwarding rule", "frName", frRawName)
			err = l4utils.NewUserError(err)
		}
		return nil, err
	}
	return fr, nil
}

// backendService retrieves the backend service referenced by the forwarding rule.
func (c *Controller) backendService(fr *composite.ForwardingRule, svcLogger klog.Logger) (*composite.BackendService, error) {
	bsURL := fr.BackendService
	if bsURL == "" {
		return nil, l4utils.NewUserError(fmt.Errorf("the service NEGs are not attached to the load balancer, forwarding rule is missing the backend service reference"))
	}

	resourceID, err := cloud.ParseResourceURL(bsURL)
	if err != nil {
		return nil, l4utils.NewUserError(fmt.Errorf("failed to parse backend service URL %s: %w", bsURL, err))
	}
	if resourceID == nil || resourceID.Key == nil || resourceID.Key.Name == "" {
		return nil, l4utils.NewUserError(fmt.Errorf("invalid backend service URL %s: missing resource key", bsURL))
	}

	bs, err := composite.GetBackendService(c.ctx.Cloud, resourceID.Key, meta.VersionBeta, svcLogger)
	if err != nil {
		wrappedError := fmt.Errorf("failed to get backend service %s: %w", bsURL, err)
		if utils.IsNotFoundError(err) {
			return nil, l4utils.NewUserError(wrappedError)
		}
		return nil, wrappedError
	}
	return bs, nil
}

func (c *Controller) serviceNEGLinks(svc *v1.Service) (sets.Set[cloud.ResourceMapKey], error) {
	negLinks := sets.New[cloud.ResourceMapKey]()
	if svc == nil {
		return negLinks, nil
	}

	negName := c.namer.L4Backend(svc.Namespace, svc.Name)
	if flags.F.EnableL4CustomStandaloneNEGNames {
		if customName, err := annotations.StandaloneNEGName(svc); err != nil {
			return nil, l4utils.NewUserError(err)
		} else if customName != "" {
			negName = customName
		}
	}
	svcNegKey := fmt.Sprintf("%s/%s", svc.Namespace, negName)

	if c.ctx != nil && c.ctx.SvcNegInformer != nil {
		obj, exists, err := c.ctx.SvcNegInformer.GetIndexer().GetByKey(svcNegKey)
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
		if !metav1.IsControlledBy(svcNeg, svc) {
			err := fmt.Errorf("retrieved ServiceNetworkEndpointGroup %s is not controller by the service %s", svcNeg.Name, svc.Name)
			return nil, l4utils.NewUserError(err)
		}

		for _, negRef := range svcNeg.Status.NetworkEndpointGroups {
			if negRef.SelfLink != "" {
				resourceKey, err := cloud.ParseResourceURL(negRef.SelfLink)
				if err != nil {
					continue
				}
				negLinks.Insert(resourceKey.MapKey())
			}
		}
	}
	return negLinks, nil
}
