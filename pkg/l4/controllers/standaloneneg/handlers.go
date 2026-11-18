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
	v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/l4/annotations"
)

func wantsStandaloneNEGLB(svc *v1.Service) bool {
	if svc == nil {
		return false
	}
	if svc.Spec.Type != v1.ServiceTypeLoadBalancer {
		return false
	}
	return svc.Spec.LoadBalancerClass != nil && *svc.Spec.LoadBalancerClass == annotations.StandalonePassthroughNegLoadBalancerClass
}

func (c *Controller) enqueue(svc *v1.Service) {
	c.svcQueue.Enqueue(svc)
}
