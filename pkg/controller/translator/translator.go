/*
Copyright 2017 The Kubernetes Authors.

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

package translator

import (
	"fmt"
	"sort"
	"strconv"

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
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/annotations"
	serviceextensionv1alpha1 "k8s.io/ingress-gce/pkg/apis/serviceextension/v1alpha1"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/controller/errors"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/serviceextension"
	"k8s.io/ingress-gce/pkg/utils"
)

// BackendInfo is an interface to return information about the backends.
type BackendInfo interface {
	BackendServiceForPort(port int64) (*compute.BackendService, error)
	DefaultBackendNodePort() *backends.ServicePort
}

type recorderSource interface {
	Recorder(ns string) record.EventRecorder
}

// New returns a new ControllerContext.
func New(recorders recorderSource, bi BackendInfo, svcLister, nodeLister, podLister, endpointLister, svcExtensionLister cache.Indexer, negEnabled bool) *GCE {
	gce := &GCE{
		recorders:          recorders,
		bi:                 bi,
		svcLister:          svcLister,
		nodeLister:         nodeLister,
		podLister:          podLister,
		endpointLister:     endpointLister,
		svcExtensionLister: svcExtensionLister,
		negEnabled:         negEnabled,
	}
	if svcExtensionLister != nil {
		gce.svcExtensionEnabled = true
	}
	return gce
}

// GCE helps with kubernetes -> gce api conversion.
type GCE struct {
	recorders recorderSource

	bi                 BackendInfo
	svcLister          cache.Indexer
	nodeLister         cache.Indexer
	podLister          cache.Indexer
	endpointLister     cache.Indexer
	svcExtensionLister cache.Indexer

	negEnabled          bool
	svcExtensionEnabled bool
}

// ToURLMap converts an ingress to a map of subdomain: url-regex: gce backend.
func (t *GCE) ToURLMap(ing *extensions.Ingress) (utils.GCEURLMap, error) {
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
				if _, ok := err.(errors.ErrNodePortNotFound); ok {
					t.recorders.Recorder(ing.Namespace).Eventf(ing, api_v1.EventTypeWarning, "Service", err.(errors.ErrNodePortNotFound).Error())
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
			if _, ok := err.(errors.ErrNodePortNotFound); ok {
				msg = fmt.Sprintf("couldn't find nodeport for %v/%v", ing.Namespace, ing.Spec.Backend.ServiceName)
			}
			msg = fmt.Sprintf("failed to identify user specified default backend, %v, using system default", msg)
			t.recorders.Recorder(ing.Namespace).Eventf(ing, api_v1.EventTypeWarning, "Service", msg)
		} else if defaultBackend != nil {
			msg := fmt.Sprintf("default backend set to %v:%v", ing.Spec.Backend.ServiceName, defaultBackend.Port)
			t.recorders.Recorder(ing.Namespace).Eventf(ing, api_v1.EventTypeNormal, "Service", msg)
		}
	} else {
		t.recorders.Recorder(ing.Namespace).Eventf(ing, api_v1.EventTypeNormal, "Service", "no user specified default backend, using system default")
	}
	hostPathBackend.PutDefaultBackend(defaultBackend)
	return hostPathBackend, nil
}

func (t *GCE) toGCEBackend(be *extensions.IngressBackend, ns string) (*compute.BackendService, error) {
	if be == nil {
		return nil, nil
	}
	_, port, err := t.getServiceNodePort(*be, ns)
	if err != nil {
		return nil, err
	}
	backend, err := t.bi.BackendServiceForPort(int64(port.NodePort))
	if err != nil {
		return nil, fmt.Errorf("no GCE backend exists for port %v, kube backend %+v", port, be)
	}
	return backend, nil
}

// getServiceNodePort looks in the service store for a matching service:port,
// and returns the service and nodeport.
func (t *GCE) getServiceNodePort(be extensions.IngressBackend, namespace string) (*api_v1.Service, *api_v1.ServicePort, error) {
	obj, exists, err := t.svcLister.Get(
		&api_v1.Service{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      be.ServiceName,
				Namespace: namespace,
			},
		})
	if !exists {
		return nil, nil, errors.ErrNodePortNotFound{be, fmt.Errorf("service %v/%v not found in store", namespace, be.ServiceName)}
	}
	if err != nil {
		return nil, nil, errors.ErrNodePortNotFound{be, err}
	}
	svc := obj.(*api_v1.Service)

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
		return nil, nil, errors.ErrNodePortNotFound{be, fmt.Errorf("could not find matching nodeport from service")}
	}
	return svc, port, nil
}

// getBackendsServicePort returns a backends.ServicePort constructed from
// service:port and/or serviceExtension.
func (t *GCE) getBackendsServicePort(be extensions.IngressBackend, namespace string) (backends.ServicePort, error) {
	svc, port, err := t.getServiceNodePort(be, namespace)
	if err != nil {
		return backends.ServicePort{}, err
	}

	var svcExt *serviceextensionv1alpha1.ServiceExtension
	if t.svcExtensionEnabled {
		svcExt, err = serviceextension.GetServiceExtensionForService(t.svcExtensionLister, svc)
		if err != nil {
			err = errors.ErrServiceExtension{svc, err}
			t.recorders.Recorder(svc.Namespace).Eventf(svc, api_v1.EventTypeWarning, "Service", err.Error())
			return backends.ServicePort{}, err
		}
	}

	appProtocols, err := annotations.FromService(svc).ApplicationProtocols()
	if err != nil {
		return backends.ServicePort{}, errors.ErrSvcAppProtosParsing{svc, err}
	}

	proto := annotations.ProtocolHTTP
	if protoStr, exists := appProtocols[port.Name]; exists {
		proto = annotations.AppProtocol(protoStr)
	}

	p := backends.ServicePort{
		NodePort:      int64(port.NodePort),
		Protocol:      proto,
		SvcName:       types.NamespacedName{Namespace: namespace, Name: be.ServiceName},
		SvcPort:       be.ServicePort,
		SvcTargetPort: port.TargetPort.String(),
		NEGEnabled:    t.negEnabled && annotations.FromService(svc).NEGEnabled(),
		SvcExtension:  svcExt,
	}
	return p, nil
}

// ToNodePorts is a helper method over ingressToNodePorts to process a list of ingresses.
func (t *GCE) ToNodePorts(ings *extensions.IngressList) []backends.ServicePort {
	var knownPorts []backends.ServicePort
	for _, ing := range ings.Items {
		knownPorts = append(knownPorts, t.ingressToNodePorts(&ing)...)
	}
	return knownPorts
}

// ingressToNodePorts converts a pathlist to a flat list of nodeports for the given ingress.
func (t *GCE) ingressToNodePorts(ing *extensions.Ingress) []backends.ServicePort {
	var knownPorts []backends.ServicePort
	defaultBackend := ing.Spec.Backend
	if defaultBackend != nil {
		port, err := t.getBackendsServicePort(*defaultBackend, ing.Namespace)
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
			port, err := t.getBackendsServicePort(path.Backend, ing.Namespace)
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
func (t *GCE) GetZoneForNode(name string) (string, error) {
	nodes, err := listers.NewNodeLister(t.nodeLister).ListWithPredicate(utils.NodeIsReady)
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
func (t *GCE) ListZones() ([]string, error) {
	zones := sets.String{}
	readyNodes, err := listers.NewNodeLister(t.nodeLister).ListWithPredicate(utils.NodeIsReady)
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
func (t *GCE) getHTTPProbe(svc api_v1.Service, targetPort intstr.IntOrString, protocol annotations.AppProtocol) (*api_v1.Probe, error) {
	l := svc.Spec.Selector

	// Lookup any container with a matching targetPort from the set of pods
	// with a matching label selector.
	pl, err := listPodsBySelector(t.podLister, labels.SelectorFromSet(labels.Set(l)))
	if err != nil {
		return nil, err
	}

	// If multiple endpoints have different health checks, take the first
	sort.Sort(orderedPods(pl))

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
		glog.V(5).Infof("%v: lacks a matching HTTP probe for use in health checks.", logStr)
	}
	return nil, nil
}

// GatherEndpointPorts returns all ports needed to open NEG endpoints.
func (t *GCE) GatherEndpointPorts(svcPorts []backends.ServicePort) []string {
	portMap := map[int64]bool{}
	for _, p := range svcPorts {
		if t.negEnabled && p.NEGEnabled {
			// For NEG backend, need to open firewall to all endpoint target ports
			// TODO(mixia): refactor firewall syncing into a separate go routine with different trigger.
			// With NEG, endpoint changes may cause firewall ports to be different if user specifies inconsistent backends.
			endpointPorts := listEndpointTargetPorts(t.endpointLister, p.SvcName.Namespace, p.SvcName.Name, p.SvcTargetPort)
			for _, ep := range endpointPorts {
				portMap[int64(ep)] = true
			}
		}
	}
	var portStrs []string
	for p := range portMap {
		portStrs = append(portStrs, strconv.FormatInt(p, 10))
	}
	return portStrs
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
func (t *GCE) GetProbe(port backends.ServicePort) (*api_v1.Probe, error) {
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
			if int32(port.NodePort) == sp.NodePort {
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

// listPodsBySelector returns a list of all pods based on selector
func listPodsBySelector(indexer cache.Indexer, selector labels.Selector) (ret []*api_v1.Pod, err error) {
	err = listAll(indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*api_v1.Pod))
	})
	return ret, err
}

// listAll iterates a store and passes selected item to a func
func listAll(store cache.Store, selector labels.Selector, appendFn cache.AppendFunc) error {
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

func listEndpointTargetPorts(indexer cache.Indexer, namespace, name, targetPort string) []int {
	// if targetPort is integer, no need to translate to endpoint ports
	if i, err := strconv.Atoi(targetPort); err == nil {
		return []int{i}
	}

	ep, exists, err := indexer.Get(
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

// orderedPods sorts a list of Pods by creation timestamp, using their names as a tie breaker.
type orderedPods []*api_v1.Pod

func (o orderedPods) Len() int      { return len(o) }
func (o orderedPods) Swap(i, j int) { o[i], o[j] = o[j], o[i] }
func (o orderedPods) Less(i, j int) bool {
	if o[i].CreationTimestamp.Equal(&o[j].CreationTimestamp) {
		return o[i].Name < o[j].Name
	}
	return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
}
