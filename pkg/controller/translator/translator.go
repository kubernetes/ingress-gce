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

	"k8s.io/klog"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/backendconfig"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller/errors"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/utils"
)

// NewTranslator returns a new Translator.
func NewTranslator(ctx *context.ControllerContext) *Translator {
	return &Translator{ctx}
}

// Translator helps with kubernetes -> gce api conversion.
type Translator struct {
	ctx *context.ControllerContext
}

// getServicePort looks in the svc store for a matching service:port,
// and returns the nodeport.
func (t *Translator) getServicePort(id utils.ServicePortID) (*utils.ServicePort, error) {
	// We periodically add information to this ServicePort to ensure that we
	// always return as much as possible, rather than nil, if there was a non-fatal error.
	var svcPort *utils.ServicePort

	obj, exists, err := t.ctx.ServiceInformer.GetIndexer().Get(
		&api_v1.Service{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      id.Service.Name,
				Namespace: id.Service.Namespace,
			},
		})
	if !exists {
		// This is a fatal error.
		return nil, errors.ErrSvcNotFound{Service: id.Service}
	}
	if err != nil {
		// This is a fatal error.
		return nil, fmt.Errorf("error retrieving service %q: %v", id.Service, err)
	}

	svc := obj.(*api_v1.Service)
	port := ServicePort(*svc, id.Port)
	if port == nil {
		// This is a fatal error.
		return nil, errors.ErrSvcPortNotFound{ServicePortID: id}
	}

	var negEnabled bool
	negAnnotation, ok, err := annotations.FromService(svc).NEGAnnotation()
	if ok && err == nil {
		negEnabled = negAnnotation.NEGEnabledForIngress()
	}
	if !negEnabled && svc.Spec.Type != api_v1.ServiceTypeNodePort &&
		svc.Spec.Type != api_v1.ServiceTypeLoadBalancer {
		// This is a fatal error.
		return nil, errors.ErrBadSvcType{Service: id.Service, ServiceType: svc.Spec.Type}
	}
	svcPort = &utils.ServicePort{
		ID:         id,
		NodePort:   int64(port.NodePort),
		Port:       int32(port.Port),
		TargetPort: port.TargetPort.String(),
		NEGEnabled: negEnabled,
	}

	appProtocols, err := annotations.FromService(svc).ApplicationProtocols()
	if err != nil {
		return svcPort, errors.ErrSvcAppProtosParsing{Service: id.Service, Err: err}
	}

	proto := annotations.ProtocolHTTP
	if protoStr, exists := appProtocols[port.Name]; exists {
		proto = annotations.AppProtocol(protoStr)
	}
	svcPort.Protocol = proto

	var beConfig *backendconfigv1beta1.BackendConfig
	beConfig, err = backendconfig.GetBackendConfigForServicePort(t.ctx.BackendConfigInformer.GetIndexer(), svc, port)
	if err != nil {
		// If we could not find a backend config name for the current
		// service port, then do not return an error. Removing a reference
		// to a backend config from the service annotation is a valid
		// step that a user could take.
		if err != backendconfig.ErrNoBackendConfigForPort {
			return svcPort, errors.ErrSvcBackendConfig{ServicePortID: id, Err: err}
		}
	}
	// Object in cache could be changed in-flight. Deepcopy to
	// reduce race conditions.
	beConfig = beConfig.DeepCopy()
	if err = backendconfig.Validate(t.ctx.KubeClient, beConfig); err != nil {
		return svcPort, errors.ErrBackendConfigValidation{BackendConfig: *beConfig, Err: err}
	}
	svcPort.BackendConfig = beConfig

	return svcPort, nil
}

// TranslateIngress converts an Ingress into our internal UrlMap representation.
func (t *Translator) TranslateIngress(ing *v1beta1.Ingress, systemDefaultBackend utils.ServicePortID) (*utils.GCEURLMap, []error) {
	var errs []error
	urlMap := utils.NewGCEURLMap()
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}

		pathRules := []utils.PathRule{}
		for _, p := range rule.HTTP.Paths {
			svcPort, err := t.getServicePort(utils.BackendToServicePortID(p.Backend, ing.Namespace))
			if err != nil {
				errs = append(errs, err)
			}
			if svcPort != nil {
				// The Ingress spec defines empty path as catch-all, so if a user
				// asks for a single host and multiple empty paths, all traffic is
				// sent to one of the last backend in the rules list.
				path := p.Path
				if path == "" {
					path = loadbalancers.DefaultPath
				}
				pathRules = append(pathRules, utils.PathRule{Path: path, Backend: *svcPort})
			}
		}
		host := rule.Host
		if host == "" {
			host = loadbalancers.DefaultHost
		}
		urlMap.PutPathRulesForHost(host, pathRules)
	}

	if ing.Spec.Backend != nil {
		svcPort, err := t.getServicePort(utils.BackendToServicePortID(*ing.Spec.Backend, ing.Namespace))
		if err == nil {
			urlMap.DefaultBackend = svcPort
			return urlMap, errs
		}

		errs = append(errs, err)
		return urlMap, errs
	}

	svcPort, err := t.getServicePort(systemDefaultBackend)
	if err == nil {
		urlMap.DefaultBackend = svcPort
		return urlMap, errs
	}

	errs = append(errs, fmt.Errorf("failed to retrieve the system default backend service %q with port %q: %v", systemDefaultBackend.Service.String(), systemDefaultBackend.Port.String(), err))
	return urlMap, errs
}

func getZone(n *api_v1.Node) string {
	zone, ok := n.Labels[annotations.ZoneKey]
	if !ok {
		return annotations.DefaultZone
	}
	return zone
}

// GetZoneForNode returns the zone for a given node by looking up its zone label.
func (t *Translator) GetZoneForNode(name string) (string, error) {
	nodeLister := t.ctx.NodeInformer.GetIndexer()
	nodes, err := listers.NewNodeLister(nodeLister).List(labels.Everything())
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
func (t *Translator) ListZones() ([]string, error) {
	zones := sets.String{}
	nodeLister := t.ctx.NodeInformer.GetIndexer()
	readyNodes, err := listers.NewNodeLister(nodeLister).ListWithPredicate(utils.GetNodeConditionPredicate())
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
func (t *Translator) getHTTPProbe(svc api_v1.Service, targetPort intstr.IntOrString, protocol annotations.AppProtocol) (*backends.ServicePortAndProbe, error) {
	l := svc.Spec.Selector

	// Lookup any container with a matching targetPort from the set of pods
	// with a matching label selector.
	pl, err := listPodsBySelector(t.ctx.PodInformer.GetIndexer(), labels.SelectorFromSet(labels.Set(l)))
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

			if !isSimpleHTTPProbe(c.ReadinessProbe) {
				continue
			}
			for _, np := range svc.Spec.Ports {
				if ((np.TargetPort.Type == intstr.Int && c.ReadinessProbe.HTTPGet.Port.IntValue() == np.TargetPort.IntValue() ) || 
				    (c.ReadinessProbe.HTTPGet.Port.Type == intstr.String)) {
					return &backends.ServicePortAndProbe{
						Probe:   c.ReadinessProbe,
						Service: &np,
					}, nil
				}
			}
		}
		klog.V(5).Infof("%v: lacks a matching HTTP probe for use in health checks.", logStr)
	}
	return nil, nil
}

// GatherEndpointPorts returns all ports needed to open NEG endpoints.
func (t *Translator) GatherEndpointPorts(svcPorts []utils.ServicePort) []string {
	portMap := map[int64]bool{}
	for _, p := range svcPorts {
		if p.NEGEnabled {
			// For NEG backend, need to open firewall to all endpoint target ports
			// TODO(mixia): refactor firewall syncing into a separate go routine with different trigger.
			// With NEG, endpoint changes may cause firewall ports to be different if user specifies inconsistent backends.
			endpointPorts := listEndpointTargetPorts(t.ctx.EndpointInformer.GetIndexer(), p.ID.Service.Namespace, p.ID.Service.Name, p.TargetPort)
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

// getProbeScheme returns the Kubernetes API URL scheme corresponding to the
// protocol.
func getProbeScheme(protocol annotations.AppProtocol) api_v1.URIScheme {
	if protocol == annotations.ProtocolHTTP2 {
		return api_v1.URISchemeHTTPS
	}
	return api_v1.URIScheme(string(protocol))
}

// GetProbe returns a probe that's used for the given nodeport
func (t *Translator) GetProbe(port utils.ServicePort) (*backends.ServicePortAndProbe, error) {
	sl := t.ctx.ServiceInformer.GetIndexer().List()

	// Find the label and target port of the one service with the given nodePort
	var service api_v1.Service
	var svcPort api_v1.ServicePort
	var found bool
OuterLoop:
	for _, as := range sl {
		service = *as.(*api_v1.Service)
		for _, sp := range service.Spec.Ports {
			svcPort = sp
			// If service is NEG enabled, compare the service name and namespace instead
			// This is because NEG enabled service is not required to have nodePort
			if port.NEGEnabled && port.ID.Service.Namespace == service.Namespace && port.ID.Service.Name == service.Name && port.Port == sp.Port {
				found = true
				break OuterLoop
			}
			// only one Service can match this nodePort, try and look up
			// the readiness probe of the pods behind it
			if port.NodePort != 0 && int32(port.NodePort) == sp.NodePort {
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
		klog.Errorf("Endpoint object %v/%v does not exist.", namespace, name)
		return []int{}
	}
	if err != nil {
		klog.Errorf("Failed to retrieve endpoint object %v/%v: %v", namespace, name, err)
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
