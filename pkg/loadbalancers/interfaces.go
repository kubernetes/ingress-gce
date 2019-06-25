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

// LoadBalancerPool is an interface to manage the cloud resources associated
// with a gce loadbalancer.
type LoadBalancerPool interface {
	Ensure(ri *L7RuntimeInfo) (*L7, error)
	Delete(name string) error
	GC(names []string) error
	Shutdown() error
	List() ([]string, error)
}
