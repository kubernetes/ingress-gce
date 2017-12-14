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
	"sort"
	"strconv"
	"time"

	"github.com/golang/glog"

	compute "google.golang.org/api/compute/v1"

	api_v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/utils"
)

// isGCEIngress returns true if the given Ingress either doesn't specify the
// ingress.class annotation, or it's set to "gce".
func isGCEIngress(ing *extensions.Ingress) bool {
	class := annotations.IngAnnotations(ing.ObjectMeta.Annotations).IngressClass()
	return class == "" || class == annotations.GceIngressClass
}

// isGCEMultiClusterIngress returns true if the given Ingress has
// ingress.class annotation set to "gce-multi-cluster".
func isGCEMultiClusterIngress(ing *extensions.Ingress) bool {
	class := annotations.IngAnnotations(ing.ObjectMeta.Annotations).IngressClass()
	return class == annotations.GceMultiIngressClass
}

// errorNodePortNotFound is an implementation of error.
type errorNodePortNotFound struct {
	backend extensions.IngressBackend
	origErr error
}

func (e errorNodePortNotFound) Error() string {
	return fmt.Sprintf("Could not find nodeport for backend %+v: %v",
		e.backend, e.origErr)
}

type errorSvcAppProtosParsing struct {
	svc     *api_v1.Service
	origErr error
}

func (e errorSvcAppProtosParsing) Error() string {
	return fmt.Sprintf("could not parse %v annotation on Service %v/%v, err: %v", annotations.ServiceApplicationProtocolKey, e.svc.Namespace, e.svc.Name, e.origErr)
}

// taskQueue manages a work queue through an independent worker that
// invokes the given sync function for every work item inserted.
type taskQueue struct {
	// queue is the work queue the worker polls
	queue workqueue.RateLimitingInterface
	// sync is called for each item in the queue
	sync func(string) error
	// workerDone is closed when the worker exits
	workerDone chan struct{}
}

func (t *taskQueue) run(period time.Duration, stopCh <-chan struct{}) {
	wait.Until(t.worker, period, stopCh)
}

// enqueue enqueues ns/name of the given api object in the task queue.
func (t *taskQueue) enqueue(obj interface{}) {
	key, err := keyFunc(obj)
	if err != nil {
		glog.Infof("Couldn't get key for object %+v: %v", obj, err)
		return
	}
	t.queue.Add(key)
}

// worker processes work in the queue through sync.
func (t *taskQueue) worker() {
	for {
		key, quit := t.queue.Get()
		if quit {
			close(t.workerDone)
			return
		}
		glog.V(3).Infof("Syncing %v", key)
		if err := t.sync(key.(string)); err != nil {
			glog.Errorf("Requeuing %v, err %v", key, err)
			t.queue.AddRateLimited(key)
		} else {
			t.queue.Forget(key)
		}
		t.queue.Done(key)
	}
}

// shutdown shuts down the work queue and waits for the worker to ACK
func (t *taskQueue) shutdown() {
	t.queue.ShutDown()
	<-t.workerDone
}

// NewTaskQueue creates a new task queue with the given sync function.
// The sync function is called for every element inserted into the queue.
func NewTaskQueue(syncFn func(string) error) *taskQueue {
	return &taskQueue{
		queue:      workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		sync:       syncFn,
		workerDone: make(chan struct{}),
	}
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

// StoreToEndpointLister makes a Store that lists Endpoints.
type StoreToEndpointLister struct {
	cache.Indexer
}

// List returns a list of all pods based on selector
func (s *StoreToPodLister) List(selector labels.Selector) (ret []*api_v1.Pod, err error) {
	err = ListAll(s.Indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*api_v1.Pod))
	})
	return ret, err
}

// ListAll iterates a store and passes selected item to a func
func ListAll(store cache.Store, selector labels.Selector, appendFn cache.AppendFunc) error {
	for _, m := range store.List() {
		metadata, err := meta.Accessor(m)
		if err != nil {
			return err
		}
		if selector.Matches(labels.Set(metadata.GetLabels())) {
			appendFn(m)
		}
	}
	return nil
}

// ListAll lists all Ingress' in the store (both single and multi cluster ingresses).
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

// GCETranslator helps with kubernetes -> gce api conversion.
type GCETranslator struct {
	*LoadBalancerController
}

// toURLMap converts an ingress to a map of subdomain: url-regex: gce backend.
func (t *GCETranslator) toURLMap(ing *extensions.Ingress) (utils.GCEURLMap, error) {
	hostPathBackend := utils.GCEURLMap{}
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			glog.Errorf("Ignoring non http Ingress rule")
			continue
		}
		pathToBackend := map[string]*compute.BackendService{}
		for _, p := range rule.HTTP.Paths {
			backend, err := t.toGCEBackend(&p.Backend, ing.Namespace)
			if err != nil {
				// If a service doesn't have a nodeport we can still forward traffic
				// to all other services under the assumption that the user will
				// modify nodeport.
				if _, ok := err.(errorNodePortNotFound); ok {
					t.recorder.Eventf(ing, api_v1.EventTypeWarning, "Service", err.(errorNodePortNotFound).Error())
					continue
				}

				// If a service doesn't have a backend, there's nothing the user
				// can do to correct this (the admin might've limited quota).
				// So keep requeuing the l7 till all backends exist.
				return utils.GCEURLMap{}, err
			}
			// The Ingress spec defines empty path as catch-all, so if a user
			// asks for a single host and multiple empty paths, all traffic is
			// sent to one of the last backend in the rules list.
			path := p.Path
			if path == "" {
				path = loadbalancers.DefaultPath
			}
			pathToBackend[path] = backend
		}
		// If multiple hostless rule sets are specified, last one wins
		host := rule.Host
		if host == "" {
			host = loadbalancers.DefaultHost
		}
		hostPathBackend[host] = pathToBackend
	}
	var defaultBackend *compute.BackendService
	if ing.Spec.Backend != nil {
		var err error
		defaultBackend, err = t.toGCEBackend(ing.Spec.Backend, ing.Namespace)
		if err != nil {
			msg := fmt.Sprintf("%v", err)
			if _, ok := err.(errorNodePortNotFound); ok {
				msg = fmt.Sprintf("couldn't find nodeport for %v/%v", ing.Namespace, ing.Spec.Backend.ServiceName)
			}
			t.recorder.Eventf(ing, api_v1.EventTypeWarning, "Service", fmt.Sprintf("failed to identify user specified default backend, %v, using system default", msg))
		} else if defaultBackend != nil {
			t.recorder.Eventf(ing, api_v1.EventTypeNormal, "Service", fmt.Sprintf("default backend set to %v:%v", ing.Spec.Backend.ServiceName, defaultBackend.Port))
		}
	} else {
		t.recorder.Eventf(ing, api_v1.EventTypeNormal, "Service", "no user specified default backend, using system default")
	}
	hostPathBackend.PutDefaultBackend(defaultBackend)
	return hostPathBackend, nil
}

func (t *GCETranslator) toGCEBackend(be *extensions.IngressBackend, ns string) (*compute.BackendService, error) {
	if be == nil {
		return nil, nil
	}
	port, err := t.getServiceNodePort(*be, ns)
	if err != nil {
		return nil, err
	}
	backend, err := t.CloudClusterManager.backendPool.Get(port.Port)
	if err != nil {
		return nil, fmt.Errorf("no GCE backend exists for port %v, kube backend %+v", port, be)
	}
	return backend, nil
}

// getServiceNodePort looks in the svc store for a matching service:port,
// and returns the nodeport.
func (t *GCETranslator) getServiceNodePort(be extensions.IngressBackend, namespace string) (backends.ServicePort, error) {
	obj, exists, err := t.svcLister.Indexer.Get(
		&api_v1.Service{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      be.ServiceName,
				Namespace: namespace,
			},
		})
	if !exists {
		return backends.ServicePort{}, errorNodePortNotFound{be, fmt.Errorf("service %v/%v not found in store", namespace, be.ServiceName)}
	}
	if err != nil {
		return backends.ServicePort{}, errorNodePortNotFound{be, err}
	}
	svc := obj.(*api_v1.Service)
	appProtocols, err := annotations.SvcAnnotations(svc.GetAnnotations()).ApplicationProtocols()
	if err != nil {
		return backends.ServicePort{}, errorSvcAppProtosParsing{svc, err}
	}

	var port *api_v1.ServicePort
PortLoop:
	for _, p := range svc.Spec.Ports {
		np := p
		switch be.ServicePort.Type {
		case intstr.Int:
			if p.Port == be.ServicePort.IntVal {
				port = &np
				break PortLoop
			}
		default:
			if p.Name == be.ServicePort.StrVal {
				port = &np
				break PortLoop
			}
		}
	}

	if port == nil {
		return backends.ServicePort{}, errorNodePortNotFound{be, fmt.Errorf("could not find matching nodeport from service")}
	}

	proto := utils.ProtocolHTTP
	if protoStr, exists := appProtocols[port.Name]; exists {
		proto = utils.AppProtocol(protoStr)
	}

	p := backends.ServicePort{
		Port:          int64(port.NodePort),
		Protocol:      proto,
		SvcName:       types.NamespacedName{Namespace: namespace, Name: be.ServiceName},
		SvcPort:       be.ServicePort,
		SvcTargetPort: port.TargetPort.String(),
		NEGEnabled:    t.negEnabled && annotations.SvcAnnotations(svc.GetAnnotations()).NEGEnabled(),
	}
	return p, nil
}

// toNodePorts is a helper method over ingressToNodePorts to process a list of ingresses.
func (t *GCETranslator) toNodePorts(ings *extensions.IngressList) []backends.ServicePort {
	var knownPorts []backends.ServicePort
	for _, ing := range ings.Items {
		knownPorts = append(knownPorts, t.ingressToNodePorts(&ing)...)
	}
	return knownPorts
}

// ingressToNodePorts converts a pathlist to a flat list of nodeports for the given ingress.
func (t *GCETranslator) ingressToNodePorts(ing *extensions.Ingress) []backends.ServicePort {
	var knownPorts []backends.ServicePort
	defaultBackend := ing.Spec.Backend
	if defaultBackend != nil {
		port, err := t.getServiceNodePort(*defaultBackend, ing.Namespace)
		if err != nil {
			glog.Infof("%v", err)
		} else {
			knownPorts = append(knownPorts, port)
		}
	}
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			glog.Errorf("ignoring non http Ingress rule")
			continue
		}
		for _, path := range rule.HTTP.Paths {
			port, err := t.getServiceNodePort(path.Backend, ing.Namespace)
			if err != nil {
				glog.Infof("%v", err)
				continue
			}
			knownPorts = append(knownPorts, port)
		}
	}
	return knownPorts
}

func getZone(n *api_v1.Node) string {
	zone, ok := n.Labels[annotations.ZoneKey]
	if !ok {
		return annotations.DefaultZone
	}
	return zone
}

// GetZoneForNode returns the zone for a given node by looking up its zone label.
func (t *GCETranslator) GetZoneForNode(name string) (string, error) {
	nodes, err := listers.NewNodeLister(t.nodeLister.Indexer).ListWithPredicate(getNodeReadyPredicate())
	if err != nil {
		return "", err
	}
	for _, n := range nodes {
		if n.Name == name {
			// TODO: Make this more resilient to label changes by listing
			// cloud nodes and figuring out zone.
			return getZone(n), nil
		}
	}
	return "", fmt.Errorf("node not found %v", name)
}

// ListZones returns a list of zones this Kubernetes cluster spans.
func (t *GCETranslator) ListZones() ([]string, error) {
	zones := sets.String{}
	readyNodes, err := listers.NewNodeLister(t.nodeLister.Indexer).ListWithPredicate(getNodeReadyPredicate())
	if err != nil {
		return zones.List(), err
	}
	for _, n := range readyNodes {
		zones.Insert(getZone(n))
	}
	return zones.List(), nil
}

// geHTTPProbe returns the http readiness probe from the first container
// that matches targetPort, from the set of pods matching the given labels.
func (t *GCETranslator) getHTTPProbe(svc api_v1.Service, targetPort intstr.IntOrString, protocol utils.AppProtocol) (*api_v1.Probe, error) {
	l := svc.Spec.Selector

	// Lookup any container with a matching targetPort from the set of pods
	// with a matching label selector.
	pl, err := t.podLister.List(labels.SelectorFromSet(labels.Set(l)))
	if err != nil {
		return nil, err
	}

	// If multiple endpoints have different health checks, take the first
	sort.Sort(PodsByCreationTimestamp(pl))

	for _, pod := range pl {
		if pod.Namespace != svc.Namespace {
			continue
		}
		logStr := fmt.Sprintf("Pod %v matching service selectors %v (targetport %+v)", pod.Name, l, targetPort)
		for _, c := range pod.Spec.Containers {
			if !isSimpleHTTPProbe(c.ReadinessProbe) || string(protocol) != string(c.ReadinessProbe.HTTPGet.Scheme) {
				continue
			}

			for _, p := range c.Ports {
				if (targetPort.Type == intstr.Int && targetPort.IntVal == p.ContainerPort) ||
					(targetPort.Type == intstr.String && targetPort.StrVal == p.Name) {

					readinessProbePort := c.ReadinessProbe.Handler.HTTPGet.Port
					switch readinessProbePort.Type {
					case intstr.Int:
						if readinessProbePort.IntVal == p.ContainerPort {
							return c.ReadinessProbe, nil
						}
					case intstr.String:
						if readinessProbePort.StrVal == p.Name {
							return c.ReadinessProbe, nil
						}
					}

					glog.Infof("%v: found matching targetPort on container %v, but not on readinessProbe (%+v)",
						logStr, c.Name, c.ReadinessProbe.Handler.HTTPGet.Port)
				}
			}
		}
		glog.V(4).Infof("%v: lacks a matching HTTP probe for use in health checks.", logStr)
	}
	return nil, nil
}

// gatherFirewallPorts returns all ports needed for open for ingress.
// It gathers both node ports (for IG backends) and target ports (for NEG backends).
func (t *GCETranslator) gatherFirewallPorts(svcPorts []backends.ServicePort, includeDefaultBackend bool) []int64 {
	// TODO: Manage default backend and its firewall rule in a centralized way.
	// DefaultBackend is managed in l7 pool, which doesn't understand instances,
	// which the firewall rule requires.
	if includeDefaultBackend {
		svcPorts = append(svcPorts, t.CloudClusterManager.defaultBackendNodePort)
	}
	portMap := map[int64]bool{}
	for _, p := range svcPorts {
		if p.NEGEnabled {
			// For NEG backend, need to open firewall to all endpoint target ports
			// TODO(mixia): refactor firewall syncing into a separate go routine with different trigger.
			// With NEG, endpoint changes may cause firewall ports to be different if user specifies inconsistent backends.
			endpointPorts := t.endpointLister.ListEndpointTargetPorts(p.SvcName.Namespace, p.SvcName.Name, p.SvcTargetPort)
			for _, ep := range endpointPorts {
				portMap[int64(ep)] = true
			}
		} else {
			// For IG backend, need to open service node port.
			portMap[p.Port] = true
		}
	}

	var np []int64
	for p := range portMap {
		np = append(np, p)
	}
	return np
}

// isSimpleHTTPProbe returns true if the given Probe is:
// - an HTTPGet probe, as opposed to a tcp or exec probe
// - has no special host or headers fields, except for possibly an HTTP Host header
func isSimpleHTTPProbe(probe *api_v1.Probe) bool {
	return (probe != nil && probe.Handler.HTTPGet != nil && probe.Handler.HTTPGet.Host == "" &&
		(len(probe.Handler.HTTPGet.HTTPHeaders) == 0 ||
			(len(probe.Handler.HTTPGet.HTTPHeaders) == 1 && probe.Handler.HTTPGet.HTTPHeaders[0].Name == "Host")))
}

// GetProbe returns a probe that's used for the given nodeport
func (t *GCETranslator) GetProbe(port backends.ServicePort) (*api_v1.Probe, error) {
	sl := t.svcLister.List()

	// Find the label and target port of the one service with the given nodePort
	var service api_v1.Service
	var svcPort api_v1.ServicePort
	var found bool
OuterLoop:
	for _, as := range sl {
		service = *as.(*api_v1.Service)
		for _, sp := range service.Spec.Ports {
			svcPort = sp
			// only one Service can match this nodePort, try and look up
			// the readiness probe of the pods behind it
			if int32(port.Port) == sp.NodePort {
				found = true
				break OuterLoop
			}
		}
	}

	if !found {
		return nil, fmt.Errorf("unable to find nodeport %v in any service", port)
	}

	return t.getHTTPProbe(service, svcPort.TargetPort, port.Protocol)
}

// PodsByCreationTimestamp sorts a list of Pods by creation timestamp, using their names as a tie breaker.
type PodsByCreationTimestamp []*api_v1.Pod

func (o PodsByCreationTimestamp) Len() int      { return len(o) }
func (o PodsByCreationTimestamp) Swap(i, j int) { o[i], o[j] = o[j], o[i] }

func (o PodsByCreationTimestamp) Less(i, j int) bool {
	if o[i].CreationTimestamp.Equal(&o[j].CreationTimestamp) {
		return o[i].Name < o[j].Name
	}
	return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
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
