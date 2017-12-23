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

package controller

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/golang/glog"

	compute "google.golang.org/api/compute/v1"

	api_v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
)

// isGCEIngress returns true if the given Ingress either doesn't specify the
// ingress.class annotation, or it's set to "gce".
func isGCEIngress(ing *extensions.Ingress) bool {
	class := annotations.FromIngress(ing).IngressClass()
	return class == "" || class == annotations.GceIngressClass
}

// isGCEMultiClusterIngress returns true if the given Ingress has
// ingress.class annotation set to "gce-multi-cluster".
func isGCEMultiClusterIngress(ing *extensions.Ingress) bool {
	class := annotations.FromIngress(ing).IngressClass()
	return class == annotations.GceMultiIngressClass
}

// ErrNodePortNotFound is an implementation of error.
type ErrNodePortNotFound struct {
	backend extensions.IngressBackend
	origErr error
}

func (e ErrNodePortNotFound) Error() string {
	return fmt.Sprintf("Could not find nodeport for backend %+v: %v",
		e.backend, e.origErr)
}

type ErrSvcAppProtosParsing struct {
	svc     *api_v1.Service
	origErr error
}

func (e ErrSvcAppProtosParsing) Error() string {
	return fmt.Sprintf("could not parse %v annotation on Service %v/%v, err: %v", annotations.ServiceApplicationProtocolKey, e.svc.Namespace, e.svc.Name, e.origErr)
}

// compareLinks returns true if the 2 self links are equal.
func compareLinks(l1, l2 string) bool {
	// TODO: These can be partial links
	return l1 == l2 && l1 != ""
}

// StoreToIngressLister makes a Store that lists Ingress.
// TODO: Move this to cache/listers post 1.1.
type StoreToIngressLister struct {
	cache.Store
}

// StoreToNodeLister makes a Store that lists Node.
type StoreToNodeLister struct {
	cache.Indexer
}

// StoreToServiceLister makes a Store that lists Service.
type StoreToServiceLister struct {
	cache.Indexer
}

// StoreToPodLister makes a Store that lists Pods.
type StoreToPodLister struct {
	cache.Indexer
}

// StoreToPodLister makes a Store that lists Endpoints.
type StoreToEndpointLister struct {
	cache.Indexer
}

// List lists all Ingress' in the store (both single and multi cluster ingresses).
func (s *StoreToIngressLister) ListAll() (ing extensions.IngressList, err error) {
	for _, m := range s.Store.List() {
		newIng := m.(*extensions.Ingress)
		if isGCEIngress(newIng) || isGCEMultiClusterIngress(newIng) {
			ing.Items = append(ing.Items, *newIng)
		}
	}
	return ing, nil
}

// ListGCEIngresses lists all GCE Ingress' in the store.
func (s *StoreToIngressLister) ListGCEIngresses() (ing extensions.IngressList, err error) {
	for _, m := range s.Store.List() {
		newIng := m.(*extensions.Ingress)
		if isGCEIngress(newIng) {
			ing.Items = append(ing.Items, *newIng)
		}
	}
	return ing, nil
}

// GetServiceIngress gets all the Ingress' that have rules pointing to a service.
// Note that this ignores services without the right nodePorts.
func (s *StoreToIngressLister) GetServiceIngress(svc *api_v1.Service) (ings []extensions.Ingress, err error) {
IngressLoop:
	for _, m := range s.Store.List() {
		ing := *m.(*extensions.Ingress)
		if ing.Namespace != svc.Namespace {
			continue
		}

		// Check service of default backend
		if ing.Spec.Backend != nil && ing.Spec.Backend.ServiceName == svc.Name {
			ings = append(ings, ing)
			continue
		}

		// Check the target service for each path rule
		for _, rule := range ing.Spec.Rules {
			if rule.IngressRuleValue.HTTP == nil {
				continue
			}
			for _, p := range rule.IngressRuleValue.HTTP.Paths {
				if p.Backend.ServiceName == svc.Name {
					ings = append(ings, ing)
					// Skip the rest of the rules to avoid duplicate ingresses in list
					continue IngressLoop
				}
			}
		}
	}
	if len(ings) == 0 {
		err = fmt.Errorf("no ingress for service %v", svc.Name)
	}
	return
}

func (s *StoreToEndpointLister) ListEndpointTargetPorts(namespace, name, targetPort string) []int {
	// if targetPort is integer, no need to translate to endpoint ports
	if i, err := strconv.Atoi(targetPort); err == nil {
		return []int{i}
	}

	ep, exists, err := s.Indexer.Get(
		&api_v1.Endpoints{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		},
	)

	if !exists {
		glog.Errorf("Endpoint object %v/%v does not exist.", namespace, name)
		return []int{}
	}
	if err != nil {
		glog.Errorf("Failed to retrieve endpoint object %v/%v: %v", namespace, name, err)
		return []int{}
	}

	ret := []int{}
	for _, subset := range ep.(*api_v1.Endpoints).Subsets {
		for _, port := range subset.Ports {
			if port.Protocol == api_v1.ProtocolTCP && port.Name == targetPort {
				ret = append(ret, int(port.Port))
			}
		}
	}
	return ret
}

// setInstanceGroupsAnnotation sets the instance-groups annotation with names of the given instance groups.
func setInstanceGroupsAnnotation(existing map[string]string, igs []*compute.InstanceGroup) error {
	type Value struct {
		Name string
		Zone string
	}
	var instanceGroups []Value
	for _, ig := range igs {
		instanceGroups = append(instanceGroups, Value{Name: ig.Name, Zone: ig.Zone})
	}
	jsonValue, err := json.Marshal(instanceGroups)
	if err != nil {
		return err
	}
	existing[annotations.InstanceGroupsAnnotationKey] = string(jsonValue)
	return nil
}

// uniq returns an array of unique service ports from the given array.
func uniq(nodePorts []backends.ServicePort) []backends.ServicePort {
	portMap := map[int64]backends.ServicePort{}
	for _, p := range nodePorts {
		portMap[p.Port] = p
	}
	nodePorts = make([]backends.ServicePort, 0, len(portMap))
	for _, sp := range portMap {
		nodePorts = append(nodePorts, sp)
	}
	return nodePorts
}
