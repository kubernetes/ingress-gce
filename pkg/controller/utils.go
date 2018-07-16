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
	"errors"
	"fmt"
	"strings"

	compute "google.golang.org/api/compute/v1"

	api_v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/utils"
)

func joinErrs(errs []error) error {
	var errStrs []string
	for _, e := range errs {
		errStrs = append(errStrs, e.Error())
	}
	return errors.New(strings.Join(errStrs, "; "))
}

// isGCEIngress returns true if the Ingress matches the class managed by this
// controller.
func isGCEIngress(ing *extensions.Ingress) bool {
	class := annotations.FromIngress(ing).IngressClass()
	if flags.F.IngressClass == "" {
		return class == "" || class == annotations.GceIngressClass
	}
	return class == flags.F.IngressClass
}

// isGCEMultiClusterIngress returns true if the given Ingress has
// ingress.class annotation set to "gce-multi-cluster".
func isGCEMultiClusterIngress(ing *extensions.Ingress) bool {
	class := annotations.FromIngress(ing).IngressClass()
	return class == annotations.GceMultiIngressClass
}

// StoreToIngressLister makes a Store that lists Ingress.
// TODO: Move this to cache/listers post 1.1.
type StoreToIngressLister struct {
	cache.Store
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
func (s *StoreToIngressLister) GetServiceIngress(svc *api_v1.Service, systemDefaultBackend utils.ServicePortID) (ings []extensions.Ingress, err error) {
IngressLoop:
	for _, m := range s.Store.List() {
		ing := *m.(*extensions.Ingress)

		// Check if system default backend is involved
		if ing.Spec.Backend == nil && systemDefaultBackend.Service.Name == svc.Name && systemDefaultBackend.Service.Namespace == svc.Namespace {
			ings = append(ings, ing)
			continue
		}

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
func uniq(svcPorts []utils.ServicePort) []utils.ServicePort {
	portMap := map[string]utils.ServicePort{}
	for _, p := range svcPorts {
		portMap[fmt.Sprintf("%q-%d", p.ID.Service.String(), p.Port)] = p
	}
	svcPorts = make([]utils.ServicePort, 0, len(portMap))
	for _, sp := range portMap {
		svcPorts = append(svcPorts, sp)
	}
	return svcPorts
}

// getReadyNodeNames returns names of schedulable, ready nodes from the node lister.
func getReadyNodeNames(lister listers.NodeLister) ([]string, error) {
	var nodeNames []string
	nodes, err := lister.ListWithPredicate(utils.NodeIsReady)
	if err != nil {
		return nodeNames, err
	}
	for _, n := range nodes {
		if n.Spec.Unschedulable {
			continue
		}
		nodeNames = append(nodeNames, n.Name)
	}
	return nodeNames, nil
}

func nodeStatusChanged(old, cur *api_v1.Node) bool {
	if old.Spec.Unschedulable != cur.Spec.Unschedulable {
		return true
	}
	if utils.NodeIsReady(old) != utils.NodeIsReady(cur) {
		return true
	}
	return false
}
