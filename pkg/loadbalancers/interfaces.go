/*
Copyright 2015 The Kubernetes Authors.

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

package loadbalancers

import (
	"k8s.io/api/networking/v1beta1"
)

// LoadBalancerPool is an interface to manage the cloud resources associated
// with a gce loadbalancer.
type LoadBalancerPool interface {
	// Ensure ensures a loadbalancer and its resources given the RuntimeInfo.
	Ensure(ri *L7RuntimeInfo) (*L7, error)
	// GCv2 garbage collects loadbalancer associated with given ingress using v2 naming scheme.
	GCv2(ing *v1beta1.Ingress) error
	// GCv1 garbage collects loadbalancers not in the input list using v1 naming scheme.
	GCv1(names []string) error
	// Shutdown deletes all loadbalancers for given list of ingresses.
	Shutdown(ings []*v1beta1.Ingress) error
	// HasUrlMap returns true if an URL map exists in GCE for given ingress.
	HasUrlMap(ing *v1beta1.Ingress) (bool, error)
}
