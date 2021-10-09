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
	"strings"

	"k8s.io/ingress-gce/pkg/utils/endpointslices"
	"k8s.io/klog"

	api_v1 "k8s.io/api/core/v1"
	discoveryapi "k8s.io/api/discovery/v1beta1"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/backendconfig"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller/errors"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/utils"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
)

const (
	// DefaultHost is the host used if none is specified. It is a valid value
	// for the "Host" field recognized by GCE.
	DefaultHost = "*"

	// DefaultPath is the path used if none is specified. It is a valid path
	// recognized by GCE.
	DefaultPath = "/*"
)

// getServicePortParams allows for passing parameters to getServicePort()
type getServicePortParams struct {
	isL7ILB bool
}

// NewTranslator returns a new Translator.
func NewTranslator(ctx *context.ControllerContext) *Translator {
	return &Translator{ctx}
}

// Translator helps with kubernetes -> gce api conversion.
type Translator struct {
	ctx *context.ControllerContext
}

func (t *Translator) getCachedService(id utils.ServicePortID) (*api_v1.Service, error) {
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

	svc, ok := obj.(*api_v1.Service)
	if !ok {
		return nil, fmt.Errorf("cannot convert to Service (%T)", svc)
	}
	return svc, nil
}

// maybeEnableNEG enables NEG on the service port if necessary
func maybeEnableNEG(sp *utils.ServicePort, svc *api_v1.Service) error {
	negAnnotation, ok, err := annotations.FromService(svc).NEGAnnotation()
	if ok && err == nil {
		sp.NEGEnabled = negAnnotation.NEGEnabledForIngress()
	}

	if !sp.NEGEnabled && svc.Spec.Type != api_v1.ServiceTypeNodePort &&
		svc.Spec.Type != api_v1.ServiceTypeLoadBalancer {
		// This is a fatal error.
		return errors.ErrBadSvcType{Service: sp.ID.Service, ServiceType: svc.Spec.Type}
	}

	if sp.L7ILBEnabled {
		// L7-ILB Requires NEGs
		sp.NEGEnabled = true
	}

	return nil
}

// setAppProtocol sets the app protocol on the service port
func setAppProtocol(sp *utils.ServicePort, svc *api_v1.Service, port *api_v1.ServicePort) error {
	appProtocols, err := annotations.FromService(svc).ApplicationProtocols()
	if err != nil {
		return errors.ErrSvcAppProtosParsing{Service: sp.ID.Service, Err: err}
	}

	proto := annotations.ProtocolHTTP
	if protoStr, exists := appProtocols[port.Name]; exists {
		proto = annotations.AppProtocol(protoStr)
	}
	sp.Protocol = proto

	return nil
}

func setTrafficScaling(sp *utils.ServicePort, svc *api_v1.Service) error {
	const maxRatePerEndpointKey = "networking.gke.io/max-rate-per-endpoint"
	if s, ok := svc.Annotations[maxRatePerEndpointKey]; ok {
		val, err := strconv.ParseFloat(s, 64)
		if err != nil || val < 0.0 {
			return fmt.Errorf(`invalid value for Service annotation %s, should be an integer > 0, got %q`, maxRatePerEndpointKey, s)
		}
		sp.MaxRatePerEndpoint = &val
	}

	const capacityScalerKey = "networking.gke.io/capacity-scaler"
	if s, ok := svc.Annotations[capacityScalerKey]; ok {
		val, err := strconv.ParseFloat(s, 64)
		if err != nil || (val < 0.0 || val > 1.0) {
			return fmt.Errorf(`invalid value for Service annotation %s, should be a number >= 0.0 and <= 1.0, got %q`, capacityScalerKey, s)
		}
		sp.CapacityScaler = &val
	}

	return nil
}

// maybeEnableBackendConfig sets the backendConfig for the service port if necessary
func (t *Translator) maybeEnableBackendConfig(sp *utils.ServicePort, svc *api_v1.Service, port *api_v1.ServicePort) error {
	var beConfig *backendconfigv1.BackendConfig
	beConfig, err := backendconfig.GetBackendConfigForServicePort(t.ctx.BackendConfigInformer.GetIndexer(), svc, port)
	if err != nil {
		// If we could not find a backend config name for the current
		// service port, then do not return an error. Removing a reference
		// to a backend config from the service annotation is a valid
		// step that a user could take.
		if err != backendconfig.ErrNoBackendConfigForPort {
			return errors.ErrSvcBackendConfig{ServicePortID: sp.ID, Err: err}
		}
	}
	// Object in cache could be changed in-flight. Deepcopy to
	// reduce race conditions.
	beConfig = beConfig.DeepCopy()
	if err = backendconfig.Validate(t.ctx.KubeClient, beConfig); err != nil {
		return errors.ErrBackendConfigValidation{BackendConfig: *beConfig, Err: err}
	}

	sp.BackendConfig = beConfig
	return nil
}

// getServicePort looks in the svc store for a matching service:port,
// and returns the nodeport.
func (t *Translator) getServicePort(id utils.ServicePortID, params *getServicePortParams, namer namer_util.BackendNamer) (*utils.ServicePort, error) {
	svc, err := t.getCachedService(id)
	if err != nil {
		return nil, err
	}

	port := ServicePort(*svc, id.Port)
	if port == nil {
		// This is a fatal error.
		return nil, errors.ErrSvcPortNotFound{ServicePortID: id}
	}

	// We periodically add information to the ServicePort to ensure that we
	// always return as much as possible, rather than nil, if there was a non-fatal error.
	svcPort := &utils.ServicePort{
		ID:           id,
		NodePort:     int64(port.NodePort),
		Port:         port.Port,
		PortName:     port.Name,
		TargetPort:   port.TargetPort,
		L7ILBEnabled: params.isL7ILB,
		BackendNamer: namer,
	}

	if err := maybeEnableNEG(svcPort, svc); err != nil {
		return nil, err
	}

	if err := setAppProtocol(svcPort, svc, port); err != nil {
		return svcPort, err
	}

	if flags.F.EnableTrafficScaling {
		if err := setTrafficScaling(svcPort, svc); err != nil {
			return nil, err
		}
	}

	if err := t.maybeEnableBackendConfig(svcPort, svc, port); err != nil {
		return svcPort, err
	}

	return svcPort, nil
}

// TranslateIngress converts an Ingress into our internal UrlMap representation.
func (t *Translator) TranslateIngress(ing *v1.Ingress, systemDefaultBackend utils.ServicePortID, namer namer_util.BackendNamer) (*utils.GCEURLMap, []error) {
	var errs []error
	urlMap := utils.NewGCEURLMap()

	params := &getServicePortParams{}
	params.isL7ILB = utils.IsGCEL7ILBIngress(ing)

	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}

		pathRules := []utils.PathRule{}
		for _, p := range rule.HTTP.Paths {
			svcPortID, err := utils.BackendToServicePortID(p.Backend, ing.Namespace)
			if err != nil {
				// Only error possible is Backend is not a Service Backend, so move to next path
				errs = append(errs, err)
				continue
			}
			svcPort, err := t.getServicePort(svcPortID, params, namer)
			if err != nil {
				errs = append(errs, err)
			}
			if svcPort != nil {
				// The Ingress spec defines empty path as catch-all, so if a user
				// asks for a single host and multiple empty paths, all traffic is
				// sent to one of the last backend in the rules list.

				paths, err := validateAndGetPaths(p)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				for _, path := range paths {
					if path == "" {
						path = DefaultPath
					}
					pathRules = append(pathRules, utils.PathRule{Path: path, Backend: *svcPort})
				}
			}
		}

		host := rule.Host
		if host == "" {
			host = DefaultHost
		}
		urlMap.PutPathRulesForHost(host, pathRules)
	}

	if ing.Spec.DefaultBackend != nil {
		svcPortID, err := utils.BackendToServicePortID(*ing.Spec.DefaultBackend, ing.Namespace)
		if err != nil {
			errs = append(errs, err)
			return urlMap, errs
		}
		svcPort, err := t.getServicePort(svcPortID, params, namer)
		if err == nil {
			urlMap.DefaultBackend = svcPort
			return urlMap, errs
		}

		errs = append(errs, err)
		return urlMap, errs
	}

	svcPort, err := t.getServicePort(systemDefaultBackend, params, namer)
	if err == nil {
		urlMap.DefaultBackend = svcPort
		return urlMap, errs
	}

	errs = append(errs, fmt.Errorf("failed to retrieve the system default backend service %q with port %q: %v", systemDefaultBackend.Service.String(), systemDefaultBackend.Port.String(), err))
	return urlMap, errs
}

// validateAndGetPaths will validate the path based on the specifed path type and will return the
// the path rules that should be used. If no path type is provided, the path type will be assumed
// to be ImplementationSpecific. If a non existent path type is provided, an error will be returned.
func validateAndGetPaths(path v1.HTTPIngressPath) ([]string, error) {
	pathType := v1.PathTypeImplementationSpecific

	if path.PathType != nil {
		pathType = *path.PathType
	}

	switch pathType {
	case v1.PathTypeImplementationSpecific:
		// ImplementationSpecific will have no validation to continue backwards compatibility
		return []string{path.Path}, nil
	case v1.PathTypeExact:
		return validateExactPathType(path)
	case v1.PathTypePrefix:
		return validateAndModifyPrefixPathType(path)
	default:
		return nil, fmt.Errorf("unsupported path type: %s", pathType)
	}
}

// validateExactPathType will validate the path provided does not have any wildcards and will
// return the path unmodified. If the path is in valid, an empty list and error is returned.
func validateExactPathType(path v1.HTTPIngressPath) ([]string, error) {
	if path.Path == "" {
		return nil, fmt.Errorf("failed to validate exact path type due to empty path")
	}

	if strings.Contains(path.Path, "*") {
		return nil, fmt.Errorf("failed to validate exact path %s due to invalid wildcard", path.Path)
	}
	return []string{path.Path}, nil
}

// validateAndModifyPrefixPathType will validate the path provided does not have any wildcards
// and will return the path unmodified. If the path is in valid, an empty list and error is
// returned.
func validateAndModifyPrefixPathType(path v1.HTTPIngressPath) ([]string, error) {
	if path.Path == "" {
		return nil, fmt.Errorf("failed to validate prefix path type due to empty path")
	}

	// The Ingress spec defines Prefx path "/" as matching all paths
	if path.Path == "/" {
		return []string{"/*"}, nil
	}

	if strings.Contains(path.Path, "*") {
		return nil, fmt.Errorf("failed to validate prefix path %s due to invalid wildcard", path.Path)
	}

	// Prefix path `/foo` or `/foo/` should support requests for `/foo`, `/foo/` and `/foo/bar`. URLMap requires two
	// path rules 1) `/foo` & 2) `/foo/*` to support all three requests.
	// Therefore each prefix path should result in two paths for the URLMap, one without a
	// trailing '/' and one that ends with '/*'
	if path.Path[len(path.Path)-1] == '/' {
		return []string{path.Path[0 : len(path.Path)-1], path.Path + "*"}, nil
	}
	return []string{path.Path, path.Path + "/*"}, nil
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

// ListZones returns a list of zones containing nodes that satisfy the given predicate.
func (t *Translator) ListZones(predicate utils.NodeConditionPredicate) ([]string, error) {
	nodeLister := t.ctx.NodeInformer.GetIndexer()
	return t.listZones(listers.NewNodeLister(nodeLister), predicate)
}

func (t *Translator) listZones(lister listers.NodeLister, predicate utils.NodeConditionPredicate) ([]string, error) {
	zones := sets.String{}
	nodes, err := utils.ListWithPredicate(lister, predicate)
	if err != nil {
		return zones.List(), err
	}
	for _, n := range nodes {
		zones.Insert(getZone(n))
	}
	return zones.List(), nil
}

// geHTTPProbe returns the http readiness probe from the first container
// that matches targetPort, from the set of pods matching the given labels.
func (t *Translator) getHTTPProbe(svc api_v1.Service, targetPort intstr.IntOrString, protocol annotations.AppProtocol) (*api_v1.Probe, error) {
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
			if !isSimpleHTTPProbe(c.ReadinessProbe) || getProbeScheme(protocol) != c.ReadinessProbe.HTTPGet.Scheme {
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

					klog.Infof("%v: found matching targetPort on container %v, but not on readinessProbe (%+v)",
						logStr, c.Name, c.ReadinessProbe.Handler.HTTPGet.Port)
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
			var endpointPorts []int
			// if targetPort is integer, no need to look for ports from endpoints
			if p.TargetPort.Type == intstr.Int {
				endpointPorts = []int{p.TargetPort.IntValue()}
			} else {
				if t.ctx.UseEndpointSlices {
					endpointPorts = listEndpointTargetPortsFromEndpointSlices(t.ctx.EndpointSliceInformer.GetIndexer(), p.ID.Service.Namespace, p.ID.Service.Name, p.PortName)
				} else {
					endpointPorts = listEndpointTargetPortsFromEndpoints(t.ctx.EndpointInformer.GetIndexer(), p.ID.Service.Namespace, p.ID.Service.Name, p.PortName)
				}
			}
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
func (t *Translator) GetProbe(port utils.ServicePort) (*api_v1.Probe, error) {
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

// finds the actual target port behind named target port, the name of the target port is the same as service port name
func listEndpointTargetPortsFromEndpointSlices(indexer cache.Indexer, namespace, name, svcPortName string) []int {
	slices, err := indexer.ByIndex(endpointslices.EndpointSlicesByServiceIndex, endpointslices.FormatEndpointSlicesServiceKey(namespace, name))
	if len(slices) == 0 {
		klog.Errorf("No Endpoint Slices found for service %s/%s.", namespace, name)
		return []int{}
	}
	if err != nil {
		klog.Errorf("Failed to retrieve endpoint slices for service %s/%s: %v", namespace, name, err)
		return []int{}
	}

	ret := []int{}
	for _, sliceObj := range slices {
		slice := sliceObj.(*discoveryapi.EndpointSlice)
		for _, port := range slice.Ports {
			if port.Protocol != nil && *port.Protocol == api_v1.ProtocolTCP && port.Name != nil && *port.Name == svcPortName && port.Port != nil {
				ret = append(ret, int(*port.Port))
			}
		}
	}
	return ret
}

// finds the actual target port behind named target port, the name of the target port is the same as service port name
func listEndpointTargetPortsFromEndpoints(indexer cache.Indexer, namespace, name, svcPortName string) []int {
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
			if port.Protocol == api_v1.ProtocolTCP && port.Name == svcPortName {
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
