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

package utils

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	api_v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/cloud-provider"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/slice"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	// Add used to record additions in a sync pool.
	Add = iota
	// Remove used to record removals from a sync pool.
	Remove
	// Sync used to record syncs of a sync pool.
	Sync
	// Get used to record Get from a sync pool.
	Get
	// Create used to record creations in a sync pool.
	Create
	// Update used to record updates in a sync pool.
	Update
	// Delete used to record deltions from a sync pool.
	Delete
	// AddInstances used to record a call to AddInstances.
	AddInstances
	// RemoveInstances used to record a call to RemoveInstances.
	RemoveInstances
	// LabelNodeRoleMaster specifies that a node is a master
	// This is a duplicate definition of the constant in:
	// kubernetes/kubernetes/pkg/controller/service/service_controller.go
	LabelNodeRoleMaster = "node-role.kubernetes.io/master"
	// LabelNodeRoleExcludeBalancer specifies that a node should be excluded from load-balancing
	// This is a duplicate definition of the constant in kubernetes core:
	//  https://github.com/kubernetes/kubernetes/blob/ea0764452222146c47ec826977f49d7001b0ea8c/staging/src/k8s.io/api/core/v1/well_known_labels.go#L67
	LabelNodeRoleExcludeBalancer = "node.kubernetes.io/exclude-from-external-load-balancers"
	// ToBeDeletedTaint is the taint that the autoscaler adds when a node is scheduled to be deleted
	// https://github.com/kubernetes/autoscaler/blob/cluster-autoscaler-0.5.2/cluster-autoscaler/utils/deletetaint/delete.go#L33
	ToBeDeletedTaint         = "ToBeDeletedByClusterAutoscaler"
	L4ILBServiceDescKey      = "networking.gke.io/service-name"
	L4ILBSharedResourcesDesc = "This resource is shared by all L4 ILB Services using ExternalTrafficPolicy: Cluster."

	// LabelAlphaNodeRoleExcludeBalancer specifies that the node should be
	// exclude from load balancers created by a cloud provider. This label is deprecated and will
	// be removed in 1.18.
	LabelAlphaNodeRoleExcludeBalancer = "alpha.service-controller.kubernetes.io/exclude-balancer"
	GKEUpgradeOperation               = "operation_type: UPGRADE_NODES"
	GKECurrentOperationAnnotation     = "gke-current-operation"
)

// FrontendGCAlgorithm species GC algorithm used for ingress frontend resources.
type FrontendGCAlgorithm int

const (
	// NoCleanUpNeeded specifies that frontend resources need not be deleted.
	NoCleanUpNeeded FrontendGCAlgorithm = iota
	// CleanupV1FrontendResources specifies that frontend resources for ingresses
	// that use v1 naming scheme need to be deleted.
	CleanupV1FrontendResources
	// CleanupV2FrontendResources specifies that frontend resources for ingresses
	// that use v2 naming scheme need to be deleted.
	CleanupV2FrontendResources
	// CleanupV2FrontendResourcesScopeChange specifies that frontend resources for ingresses
	// that use v2 naming scheme and have changed their LB scope (e.g. ILB -> ELB or vice versa)
	// need to be deleted
	CleanupV2FrontendResourcesScopeChange
	// AffinityTypeNone - no session affinity.
	gceAffinityTypeNone = "NONE"
	// AffinityTypeClientIP - affinity based on Client IP.
	gceAffinityTypeClientIP = "CLIENT_IP"
)

// FakeGoogleAPIForbiddenErr creates a Forbidden error with type googleapi.Error
func FakeGoogleAPIForbiddenErr() *googleapi.Error {
	return &googleapi.Error{Code: http.StatusForbidden}
}

// FakeGoogleAPINotFoundErr creates a NotFound error with type googleapi.Error
func FakeGoogleAPINotFoundErr() *googleapi.Error {
	return &googleapi.Error{Code: http.StatusNotFound}
}

// IsHTTPErrorCode checks if the given error matches the given HTTP Error code.
// For this to work the error must be a googleapi Error.
func IsHTTPErrorCode(err error, code int) bool {
	if err == nil {
		return false
	}
	apiErr, ok := err.(*googleapi.Error)
	return ok && apiErr.Code == code
}

// ToNamespacedName returns a types.NamespacedName struct parsed from namespace/name.
func ToNamespacedName(s string) (r types.NamespacedName, err error) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return r, fmt.Errorf("service should take the form 'namespace/name': %q", s)
	}
	return types.NamespacedName{
		Namespace: parts[0],
		Name:      parts[1],
	}, nil
}

// IgnoreHTTPNotFound returns the passed err if it's not a GoogleAPI error
// with a NotFound status code.
func IgnoreHTTPNotFound(err error) error {
	if err != nil && IsHTTPErrorCode(err, http.StatusNotFound) {
		return nil
	}
	return err
}

// IsInUsedByError returns true if the resource is being used by another GCP resource
func IsInUsedByError(err error) bool {
	apiErr, ok := err.(*googleapi.Error)
	if !ok || apiErr.Code != http.StatusBadRequest {
		return false
	}
	return strings.Contains(apiErr.Message, "being used by")
}

// IsNotFoundError returns true if the resource does not exist
func IsNotFoundError(err error) bool {
	return IsHTTPErrorCode(err, http.StatusNotFound)
}

// IsForbiddenError returns true if the operation was forbidden
func IsForbiddenError(err error) bool {
	return IsHTTPErrorCode(err, http.StatusForbidden)
}

func GetErrorType(err error) string {
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		return http.StatusText(gerr.Code)
	}
	var k8serr *k8serrors.StatusError
	if errors.As(err, &k8serr) {
		return "k8s " + string(k8serrors.ReasonForError(k8serr))
	}
	return ""
}

// PrettyJson marshals an object in a human-friendly format.
func PrettyJson(data interface{}) (string, error) {
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetIndent("", "\t")

	err := encoder.Encode(data)
	if err != nil {
		return "", err
	}
	return buffer.String(), nil
}

// KeyName returns the name portion from a full or partial GCP resource URL.
// Example:
//  Input:  https://googleapis.com/v1/compute/projects/my-project/global/backendServices/my-backend
//  Output: my-backend
func KeyName(url string) (string, error) {
	id, err := cloud.ParseResourceURL(url)
	if err != nil {
		return "", err
	}

	if id.Key == nil {
		// Resource is projects
		return id.ProjectID, nil
	}

	return id.Key.Name, nil
}

// RelativeResourceName returns the project, location, resource, and name from a full/partial GCP
// resource URL. This removes the endpoint prefix and version.
// Example:
//  Input:  https://googleapis.com/v1/compute/projects/my-project/global/backendServices/my-backend
//  Output: projects/my-project/global/backendServices/my-backend
func RelativeResourceName(url string) (string, error) {
	resID, err := cloud.ParseResourceURL(url)
	if err != nil {
		return "", err
	}
	return resID.RelativeResourceName(), nil
}

// ResourcePath returns the location, resource and name portion from a
// full or partial GCP resource URL. This removes the endpoint prefix, version, and project.
// Example:
//  Input:  https://googleapis.com/v1/compute/projects/my-project/global/backendServices/my-backend
//  Output: global/backendServices/my-backend
func ResourcePath(url string) (string, error) {
	resID, err := cloud.ParseResourceURL(url)
	if err != nil {
		return "", err
	}
	return resID.ResourcePath(), nil
}

// EqualResourcePaths returns true if a and b have equal ResourcePaths. Resource paths
// entail the location, resource type, and resource name.
func EqualResourcePaths(a, b string) bool {
	aPath, err := ResourcePath(a)
	if err != nil {
		return false
	}

	bPath, err := ResourcePath(b)
	if err != nil {
		return false
	}

	return aPath == bPath
}

// EqualResourceIDs returns true if a and b have equal ResourceIDs which entail the project,
// location, resource type, and resource name.
func EqualResourceIDs(a, b string) bool {
	aId, err := cloud.ParseResourceURL(a)
	if err != nil {
		return false
	}

	bId, err := cloud.ParseResourceURL(b)
	if err != nil {
		return false
	}

	return aId.Equal(bId)
}

// IGLinks returns a list of links extracted from the passed in list of
// compute.InstanceGroup's.
func IGLinks(igs []*compute.InstanceGroup) (igLinks []string) {
	for _, ig := range igs {
		igLinks = append(igLinks, ig.SelfLink)
	}
	return
}

// IsGCEIngress returns true if the Ingress matches the class managed by this
// controller.
func IsGCEIngress(ing *networkingv1.Ingress) bool {
	class := annotations.FromIngress(ing).IngressClass()
	if flags.F.IngressClass != "" && class == flags.F.IngressClass {
		return true
	}

	switch class {
	case "":
		// Ingress controller does not have any ingress classes that can be
		// specified by spec.IngressClassName. If spec.IngressClassName
		// is nil, then consider GCEIngress.
		return ing.Spec.IngressClassName == nil
	case annotations.GceIngressClass:
		return true
	case annotations.GceL7ILBIngressClass:
		return true
	default:
		return false
	}
}

// IsGCEMultiClusterIngress returns true if the given Ingress has
// ingress.class annotation set to "gce-multi-cluster".
func IsGCEMultiClusterIngress(ing *networkingv1.Ingress) bool {
	class := annotations.FromIngress(ing).IngressClass()
	return class == annotations.GceMultiIngressClass
}

// IsGCEL7ILBIngress returns true if the given Ingress has
// ingress.class annotation set to "gce-l7-ilb"
func IsGCEL7ILBIngress(ing *networkingv1.Ingress) bool {
	class := annotations.FromIngress(ing).IngressClass()
	return class == annotations.GceL7ILBIngressClass
}

// IsGLBCIngress returns true if the given Ingress should be processed by GLBC
func IsGLBCIngress(ing *networkingv1.Ingress) bool {
	return IsGCEIngress(ing) || IsGCEMultiClusterIngress(ing)
}

// GetReadyNodeNames returns names of schedulable, ready nodes from the node lister
// It also filters out masters and nodes excluded from load-balancing
// TODO(rramkumar): Add a test for this.
func GetReadyNodeNames(lister listers.NodeLister) ([]string, error) {
	var nodeNames []string
	nodes, err := ListWithPredicate(lister, GetNodeConditionPredicate())
	if err != nil {
		return nodeNames, err
	}
	for _, n := range nodes {
		nodeNames = append(nodeNames, n.Name)
	}
	return nodeNames, nil
}

// NodeIsReady returns true if a node contains at least one condition of type "Ready"
func NodeIsReady(node *api_v1.Node) bool {
	for i := range node.Status.Conditions {
		condition := &node.Status.Conditions[i]
		if condition.Type == api_v1.NodeReady {
			return condition.Status == api_v1.ConditionTrue
		}
	}
	return false
}

// NodeConditionPredicate is a function that indicates whether the given node's conditions meet
// some set of criteria defined by the function.
type NodeConditionPredicate func(node *api_v1.Node) bool

// This is a duplicate definition of the function in:
// kubernetes/kubernetes/pkg/controller/service/service_controller.go
func GetNodeConditionPredicate() NodeConditionPredicate {
	return func(node *api_v1.Node) bool {
		return nodePredicateInternal(node, false)
	}
}

// NodeConditionPredicateIncludeUnreadyNodes returns a predicate function that tolerates unready nodes.
func NodeConditionPredicateIncludeUnreadyNodes() NodeConditionPredicate {
	return func(node *api_v1.Node) bool {
		return nodePredicateInternal(node, true)
	}
}

func nodePredicateInternal(node *api_v1.Node, includeUnreadyNodes bool) bool {
	// Get all nodes that have a taint with NoSchedule effect
	for _, taint := range node.Spec.Taints {
		if taint.Key == ToBeDeletedTaint {
			return false
		}
	}

	// As of 1.6, we will taint the master, but not necessarily mark it unschedulable.
	// Recognize nodes labeled as master, and filter them also, as we were doing previously.
	if _, hasMasterRoleLabel := node.Labels[LabelNodeRoleMaster]; hasMasterRoleLabel {
		return false
	}

	// Will be removed in 1.18
	if _, hasExcludeBalancerLabel := node.Labels[LabelAlphaNodeRoleExcludeBalancer]; hasExcludeBalancerLabel {
		return false
	}

	if _, hasExcludeBalancerLabel := node.Labels[LabelNodeRoleExcludeBalancer]; hasExcludeBalancerLabel {
		return false
	}
	// This node is about to be upgraded.
	if opVal, _ := node.Annotations[GKECurrentOperationAnnotation]; strings.Contains(opVal, GKEUpgradeOperation) {
		return false
	}

	// If we have no info, don't accept
	if len(node.Status.Conditions) == 0 {
		return false
	}
	if includeUnreadyNodes {
		return true
	}
	for _, cond := range node.Status.Conditions {
		// We consider the node for load balancing only when its NodeReady condition status
		// is ConditionTrue
		if cond.Type == api_v1.NodeReady && cond.Status != api_v1.ConditionTrue {
			klog.V(4).Infof("Ignoring node %v with %v condition status %v", node.Name, cond.Type, cond.Status)
			return false
		}
	}
	return true

}

// ListWithPredicate gets nodes that matches predicate function.
func ListWithPredicate(nodeLister listers.NodeLister, predicate NodeConditionPredicate) ([]*api_v1.Node, error) {
	nodes, err := nodeLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	var filtered []*api_v1.Node
	for i := range nodes {
		if predicate(nodes[i]) {
			filtered = append(filtered, nodes[i])
		}
	}

	return filtered, nil
}

// GetNodePrimaryIP returns a primary internal IP address of the node.
func GetNodePrimaryIP(inputNode *api_v1.Node) string {
	ip, err := getPreferredNodeAddress(inputNode, []api_v1.NodeAddressType{api_v1.NodeInternalIP})
	if err != nil {
		klog.Errorf("Failed to get IP address for node %s", inputNode.Name)
	}
	return ip
}

// getPreferredNodeAddress returns the address of the provided node, using the provided preference order.
// If none of the preferred address types are found, an error is returned.
func getPreferredNodeAddress(node *api_v1.Node, preferredAddressTypes []api_v1.NodeAddressType) (string, error) {
	for _, addressType := range preferredAddressTypes {
		for _, address := range node.Status.Addresses {
			if address.Type == addressType {
				return address.Address, nil
			}
		}
	}
	return "", fmt.Errorf("no matching node IP")
}

// NewNamespaceIndexer returns a new Indexer for use by SharedIndexInformers
func NewNamespaceIndexer() cache.Indexers {
	return cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}
}

// JoinErrs returns an aggregated error based on the passed in list of errors.
func JoinErrs(errs []error) error {
	var errStrs []string
	for _, e := range errs {
		errStrs = append(errStrs, e.Error())
	}
	return errors.New(strings.Join(errStrs, "; "))
}

// TraverseIngressBackends traverse thru all backends specified in the input ingress and call process
// If process return true, then return and stop traversing the backends
func TraverseIngressBackends(ing *networkingv1.Ingress, process func(id ServicePortID) bool) {
	if ing == nil {
		return
	}
	// Check service of default backend
	if ing.Spec.DefaultBackend != nil && ing.Spec.DefaultBackend.Service != nil {
		if process(ServicePortID{Service: types.NamespacedName{Namespace: ing.Namespace, Name: ing.Spec.DefaultBackend.Service.Name}, Port: ing.Spec.DefaultBackend.Service.Port}) {
			return
		}
	}

	// Check the target service for each path rule
	for _, rule := range ing.Spec.Rules {
		if rule.IngressRuleValue.HTTP == nil {
			continue
		}
		for _, p := range rule.IngressRuleValue.HTTP.Paths {
			if p.Backend.Service != nil {
				if process(ServicePortID{Service: types.NamespacedName{Namespace: ing.Namespace, Name: p.Backend.Service.Name}, Port: p.Backend.Service.Port}) {
					return
				}
			}
		}
	}
	return
}

func ServiceKeyFunc(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

// NeedsCleanup returns true if the ingress needs to have its associated resources deleted.
func NeedsCleanup(ing *networkingv1.Ingress) bool {
	return common.IsDeletionCandidate(ing.ObjectMeta) || !IsGLBCIngress(ing)
}

// HasVIP returns true if given ingress has a vip.
func HasVIP(ing *networkingv1.Ingress) bool {
	if ing == nil {
		return false
	}
	if lbIPs := ing.Status.LoadBalancer.Ingress; len(lbIPs) == 0 || lbIPs[0].IP == "" {
		return false
	}
	return true
}

// NumEndpoints returns the count of endpoints in the given endpoints object.
func NumEndpoints(ep *api_v1.Endpoints) (result int) {
	for _, subset := range ep.Subsets {
		result = result + len(subset.Addresses)*len(subset.Ports)
	}
	return result
}

// EqualStringSets returns true if 2 given string slices contain the same elements, in any order.
func EqualStringSets(x, y []string) bool {
	if len(x) != len(y) {
		return false
	}
	xString := sets.NewString(x...)
	yString := sets.NewString(y...)
	return xString.Equal(yString)
}

// GetPortRanges returns a list of port ranges, given a list of ports.
func GetPortRanges(ports []int) (ranges []string) {
	if len(ports) < 1 {
		return ranges
	}
	sort.Ints(ports)

	start := ports[0]
	prev := ports[0]
	for ix, current := range ports {
		switch {
		case current == prev:
			// Loop over duplicates, except if the end of list is reached.
			if ix == len(ports)-1 {
				if start == current {
					ranges = append(ranges, fmt.Sprintf("%d", current))
				} else {
					ranges = append(ranges, fmt.Sprintf("%d-%d", start, current))
				}
			}
		case current == prev+1:
			// continue the streak, create the range if this is the last element in the list.
			if ix == len(ports)-1 {
				ranges = append(ranges, fmt.Sprintf("%d-%d", start, current))
			}
		default:
			// current is not prev + 1, streak is broken. Construct the range and handle last element case.
			if start == prev {
				ranges = append(ranges, fmt.Sprintf("%d", prev))
			} else {
				ranges = append(ranges, fmt.Sprintf("%d-%d", start, prev))
			}
			if ix == len(ports)-1 {
				ranges = append(ranges, fmt.Sprintf("%d", current))
			}
			// reset start element
			start = current
		}
		prev = current
	}
	return ranges
}

// GetPortsAndProtocol returns the list of ports, list of port ranges and the protocol given the list of k8s port info.
func GetPortsAndProtocol(svcPorts []api_v1.ServicePort) (ports []string, portRanges []string, protocol api_v1.Protocol) {
	if len(svcPorts) == 0 {
		return []string{}, []string{}, api_v1.ProtocolTCP
	}

	// GCP doesn't support multiple protocols for a single load balancer
	protocol = svcPorts[0].Protocol
	portInts := []int{}
	for _, p := range svcPorts {
		ports = append(ports, strconv.Itoa(int(p.Port)))
		portInts = append(portInts, int(p.Port))
	}

	return ports, GetPortRanges(portInts), protocol
}

// TranslateAffinityType converts the k8s affinity type to the GCE affinity type.
func TranslateAffinityType(affinityType string) string {
	switch affinityType {
	case string(api_v1.ServiceAffinityClientIP):
		return gceAffinityTypeClientIP
	case string(api_v1.ServiceAffinityNone):
		return gceAffinityTypeNone
	default:
		klog.Errorf("Unexpected affinity type: %v", affinityType)
		return gceAffinityTypeNone
	}
}

// IsLegacyL4ILBService returns true if the given LoadBalancer service is managed by service controller.
func IsLegacyL4ILBService(svc *api_v1.Service) bool {
	return slice.ContainsString(svc.ObjectMeta.Finalizers, common.LegacyILBFinalizer, nil)
}

// IsSubsettingL4ILBService returns true if the given LoadBalancer service is managed by NEG and L4 controller.
func IsSubsettingL4ILBService(svc *api_v1.Service) bool {
	return slice.ContainsString(svc.ObjectMeta.Finalizers, common.ILBFinalizerV2, nil)
}

func LegacyForwardingRuleName(svc *api_v1.Service) string {
	return cloudprovider.DefaultLoadBalancerName(svc)
}

// L4ILBResourceDescription stores the description fields for L4 ILB resources.
// This is useful to indetify which resources correspond to which L4 ILB service.
type L4ILBResourceDescription struct {
	// ServiceName indicates the name of the service the resource is for.
	ServiceName string `json:"networking.gke.io/service-name"`
	// APIVersion stores the version og the compute API used to create this resource.
	APIVersion          meta.Version `json:"networking.gke.io/api-version,omitempty"`
	ServiceIP           string       `json:"networking.gke.io/service-ip,omitempty"`
	ResourceDescription string       `json:"networking.gke.io/resource-description,omitempty"`
}

// Marshal returns the description as a JSON-encoded string.
func (d *L4ILBResourceDescription) Marshal() (string, error) {
	out, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	return string(out), err
}

// Unmarshal converts the JSON-encoded description string into the struct.
func (d *L4ILBResourceDescription) Unmarshal(desc string) error {
	return json.Unmarshal([]byte(desc), d)
}

func MakeL4ILBServiceDescription(svcName, ip string, version meta.Version, shared bool) (string, error) {
	if shared {
		return (&L4ILBResourceDescription{APIVersion: version, ResourceDescription: L4ILBSharedResourcesDesc}).Marshal()
	}
	return (&L4ILBResourceDescription{ServiceName: svcName, ServiceIP: ip, APIVersion: version}).Marshal()
}

// NewStringPointer returns a pointer to the provided string literal
func NewStringPointer(s string) *string {
	return &s
}

// NewInt64Pointer returns a pointer to the provided int64 literal
func NewInt64Pointer(i int64) *int64 {
	return &i
}

// GetBasePath returns the compute API endpoint with the `projects/<project-id>` element
// compute API v0.36 changed basepath and dropped the `projects/` suffix, therefore suffix
// must be added back when generating compute resource urls.
func GetBasePath(cloud *gce.Cloud) string {
	basePath := cloud.ComputeServices().GA.BasePath

	if basePath[len(basePath)-1] != '/' {
		basePath += "/"
	}
	// Trim  the trailing /, so that split will not consider the last element as empty
	elements := strings.Split(strings.TrimSuffix(basePath, "/"), "/")

	if elements[len(elements)-1] != "projects" {
		return fmt.Sprintf("%sprojects/%s/", basePath, cloud.ProjectID())
	}
	return fmt.Sprintf("%s%s/", basePath, cloud.ProjectID())
}
