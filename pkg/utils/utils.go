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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/googleapi"
	api_v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/slice"
	"k8s.io/klog/v2"
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
	// Delete used to record deletions from a sync pool.
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
	ToBeDeletedTaint = "ToBeDeletedByClusterAutoscaler"
	// GKECurrentOperationLabel is added by the GKE control plane, to nodes that are about to be drained as a result of an upgrade/resize operation.
	// The operation value is "drain".
	GKECurrentOperationLabel = "operation.gke.io/type"
	// NodeDrain is the string used to indicate the Node Draining operation.
	NodeDrain               = "drain"
	L4ILBServiceDescKey     = "networking.gke.io/service-name"
	L4LBSharedResourcesDesc = "This resource is shared by all L4 %s Services using ExternalTrafficPolicy: Cluster."

	// LabelAlphaNodeRoleExcludeBalancer specifies that the node should be
	// exclude from load balancers created by a cloud provider. This label is deprecated and will
	// be removed in 1.18.
	LabelAlphaNodeRoleExcludeBalancer = "alpha.service-controller.kubernetes.io/exclude-balancer"
	DualStackSubnetStackType          = "IPV4_IPV6"

	// LabelNodeSubnet specifies the subnet name of this node.
	LabelNodeSubnet = "cloud.google.com/gke-node-pool-subnet"
)

var networkTierErrorRegexp = regexp.MustCompile(`The network tier of external IP is STANDARD|PREMIUM, that of Address must be the same.`)

// NetworkTierError is a struct to define error caused by User misconfiguration of Network Tier.
type NetworkTierError struct {
	resource   string
	desiredNT  string
	receivedNT string
}

// Error function prints out Network Tier error.
func (e *NetworkTierError) Error() string {
	return fmt.Sprintf("Network tier mismatch for resource %s, desired: %s, received: %s", e.resource, e.desiredNT, e.receivedNT)
}

func NewNetworkTierErr(resourceInErr, desired, received string) *NetworkTierError {
	return &NetworkTierError{resource: resourceInErr, desiredNT: desired, receivedNT: received}
}

type UnsupportedNetworkTierError struct {
	resource      string
	unsupportedNT string
}

// Error function prints out Network Tier error.
func (e *UnsupportedNetworkTierError) Error() string {
	return fmt.Sprintf("Network tier %s is not supported for %s", e.unsupportedNT, e.resource)
}

func NewUnsupportedNetworkTierErr(resourceInErr, networkTier string) *UnsupportedNetworkTierError {
	return &UnsupportedNetworkTierError{resource: resourceInErr, unsupportedNT: networkTier}
}

// IPConfigurationError is a struct to define error caused by User misconfiguration the Load Balancer IP.
type IPConfigurationError struct {
	ip     string
	reason string
}

func (e *IPConfigurationError) Error() string {
	return fmt.Sprintf("IP configuration error: \"%s\" %s", e.ip, e.reason)
}

func NewIPConfigurationError(ip, reason string) *IPConfigurationError {
	return &IPConfigurationError{ip: ip, reason: reason}
}

// InvalidSubnetConfigurationError is a struct to define error caused by User misconfiguration of Load Balancer's subnet.
type InvalidSubnetConfigurationError struct {
	projectName string
	subnetName  string
}

func (e *InvalidSubnetConfigurationError) Error() string {
	return fmt.Sprintf("Subnetwork \"%s\" can't be found for project %s", e.subnetName, e.projectName)
}

func NewInvalidSubnetConfigurationError(projectName, subnetName string) *InvalidSubnetConfigurationError {
	return &InvalidSubnetConfigurationError{projectName: projectName, subnetName: subnetName}
}

// L4LBType indicates if L4 LoadBalancer is Internal or External
type L4LBType int

const (
	ILB L4LBType = iota
	XLB
)

func (lbType L4LBType) ToString() string {
	if lbType == ILB {
		return "ILB"
	}
	return "XLB"
}

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
	gceRateLimitExceeded    = "rateLimitExceeded"
)

// IsHTTPErrorCode checks if the given error matches the given HTTP Error code.
// For this to work the error must be a googleapi Error.
func IsHTTPErrorCode(err error, code int) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == code
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

func IsMemberAlreadyExistsError(err error) bool {
	apiErr, ok := err.(*googleapi.Error)
	if !ok || apiErr.Code != http.StatusBadRequest {
		return false
	}
	return strings.Contains(apiErr.Error(), "memberAlreadyExists")
}

// IsNetworkTierMismatchGCEError checks if error is a GCE network tier mismatch for external IP
func IsNetworkTierMismatchGCEError(err error) bool {
	return networkTierErrorRegexp.MatchString(err.Error())
}

// IsNetworkTierError checks if wrapped error is a Network Tier Mismatch error
func IsNetworkTierError(err error) bool {
	var netTierError *NetworkTierError
	return errors.As(err, &netTierError)
}

// IsUnsupportedNetworkTierError checks if wrapped error is an Unsupported Network Tier error
func IsUnsupportedNetworkTierError(err error) bool {
	var netTierError *UnsupportedNetworkTierError
	return errors.As(err, &netTierError)
}

// IsIPConfigurationError checks if wrapped error is an IP configuration error.
func IsIPConfigurationError(err error) bool {
	var ipConfigError *IPConfigurationError
	return errors.As(err, &ipConfigError)
}

// IsInvalidSubnetConfigurationError checks if wrapped error is an Invalid Subnet Configuration error.
func IsInvalidSubnetConfigurationError(err error) bool {
	var invalidSubnetConfigError *InvalidSubnetConfigurationError
	return errors.As(err, &invalidSubnetConfigError)
}

// IsInvalidLoadBalancerSourceRangesSpecError checks if wrapped error is an InvalidLoadBalancerSourceRangesSpecError error.
func IsInvalidLoadBalancerSourceRangesSpecError(err error) bool {
	var invalidLoadBalancerSourceRangesSpecError *InvalidLoadBalancerSourceRangesSpecError
	return errors.As(err, &invalidLoadBalancerSourceRangesSpecError)
}

// IsInvalidLoadBalancerSourceRangesAnnotationError checks if wrapped error is an InvalidLoadBalancerSourceRangesAnnotationError error.
func IsInvalidLoadBalancerSourceRangesAnnotationError(err error) bool {
	var invalidLoadBalancerSourceRangesAnnotationError *InvalidLoadBalancerSourceRangesAnnotationError
	return errors.As(err, &invalidLoadBalancerSourceRangesAnnotationError)
}

// UserError is a struct to define error caused by User misconfiguration.
type UserError struct {
	err error
}

func (e *UserError) Error() string {
	return e.err.Error()
}

func NewUserError(err error) *UserError {
	return &UserError{err}
}

// IsNotFoundError returns true if the resource does not exist
func IsNotFoundError(err error) bool {
	return IsHTTPErrorCode(err, http.StatusNotFound)
}

// IsForbiddenError returns true if the operation was forbidden
func IsForbiddenError(err error) bool {
	return IsHTTPErrorCode(err, http.StatusForbidden)
}

// IsQuotaExceededError returns true if the quota was exceeded
func IsQuotaExceededError(err error) bool {
	return IsHTTPErrorCode(err, http.StatusTooManyRequests) || isGCEError(err, gceRateLimitExceeded)
}

func isGCEError(err error, reason string) bool {
	var apiErr *googleapi.Error
	if !errors.As(err, &apiErr) {
		return false
	}

	for _, e := range apiErr.Errors {
		if e.Reason == reason {
			return true
		}
	}
	return false
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

// IsGCEServerError returns true if the error is GCE server error
func IsGCEServerError(err error) bool {
	if err == nil {
		return false
	}
	var gerr *googleapi.Error
	if !errors.As(err, &gerr) {
		return false
	}
	return gerr.Code >= http.StatusInternalServerError
}

// IsK8sServerError returns true if the error is K8s server error
func IsK8sServerError(err error) bool {
	if err == nil {
		return false
	}
	var k8serr *k8serrors.StatusError
	if !errors.As(err, &k8serr) {
		return false
	}
	return k8serr.ErrStatus.Code >= http.StatusInternalServerError
}

// KeyName returns the name portion from a full or partial GCP resource URL.
// Example:
//
//	Input:  https://googleapis.com/v1/compute/projects/my-project/global/backendServices/my-backend
//	Output: my-backend
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
//
//	Input:  https://googleapis.com/v1/compute/projects/my-project/global/backendServices/my-backend
//	Output: projects/my-project/global/backendServices/my-backend
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
//
//	Input:  https://googleapis.com/v1/compute/projects/my-project/global/backendServices/my-backend
//	Output: global/backendServices/my-backend
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

	return EqualCloudResourceIDs(aId, bId)
}

// EqualCloudResourceIDs returns true if a and b have equal ResourceIDs which entail the project,
// location, resource type, and resource name.
func EqualCloudResourceIDs(a, b *cloud.ResourceID) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	case a.ProjectID != b.ProjectID || a.Resource != b.Resource:
		return false
	case a.Key != nil && b.Key != nil:
		return *a.Key == *b.Key
	case a.Key == nil && b.Key == nil:
		return true
	default:
		return false
	}
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
		return flags.F.EnableIngressGlobalExternal
	case annotations.GceL7ILBIngressClass:
		return true
	case annotations.GceL7XLBRegionalIngressClass:
		return flags.F.EnableIngressRegionalExternal
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

// IsGCEL7XLBRegionalIngress returns true if the given Ingress has
// ingress.class annotation set to "gce-regional-external"
func IsGCEL7XLBRegionalIngress(ing *networkingv1.Ingress) bool {
	class := annotations.FromIngress(ing).IngressClass()
	return class == annotations.GceL7XLBRegionalIngressClass
}

// IsGLBCIngress returns true if the given Ingress should be processed by GLBC
func IsGLBCIngress(ing *networkingv1.Ingress) bool {
	return IsGCEIngress(ing) || IsGCEMultiClusterIngress(ing)
}

// GetNodeNames extracts names from the given nodes
func GetNodeNames(nodes []*api_v1.Node) []string {
	var nodeNames []string
	for _, n := range nodes {
		nodeNames = append(nodeNames, n.Name)
	}
	return nodeNames
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

// GetNodePrimaryIP returns a primary internal IP address of the node.
func GetNodePrimaryIP(inputNode *api_v1.Node, logger klog.Logger) string {
	ip, err := getPreferredNodeAddress(inputNode, []api_v1.NodeAddressType{api_v1.NodeInternalIP})
	if err != nil {
		logger.Error(err, "Failed to get IP address for node", "nodeName", inputNode.Name)
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

func GetProtocol(svcPorts []api_v1.ServicePort) api_v1.Protocol {
	if len(svcPorts) == 0 {
		return api_v1.ProtocolTCP
	}

	return svcPorts[0].Protocol
}

func GetPorts(svcPorts []api_v1.ServicePort) []string {
	ports := []string{}
	for _, p := range svcPorts {
		ports = append(ports, strconv.Itoa(int(p.Port)))
	}

	return ports
}

func GetServicePortRanges(svcPorts []api_v1.ServicePort) []string {
	portInts := []int{}
	for _, p := range svcPorts {
		portInts = append(portInts, int(p.Port))
	}

	return GetPortRanges(portInts)
}

func minMaxPort[T api_v1.ServicePort | string](svcPorts []T) (int32, int32) {
	minPort := int32(65536)
	maxPort := int32(0)
	for _, svcPort := range svcPorts {
		port := func(value any) int32 {
			switch value.(type) {
			case api_v1.ServicePort:
				return value.(api_v1.ServicePort).Port
			case string:
				i, _ := strconv.ParseInt(value.(string), 10, 32)
				return int32(i)
			default:
				return 0
			}
		}(svcPort)
		if port < minPort {
			minPort = port
		}
		if port > maxPort {
			maxPort = port
		}
	}
	return minPort, maxPort
}

func MinMaxPortRange[T api_v1.ServicePort | string](svcPorts []T) string {
	if len(svcPorts) == 0 {
		return ""
	}

	minPort, maxPort := minMaxPort(svcPorts)
	return fmt.Sprintf("%d-%d", minPort, maxPort)
}

// TranslateAffinityType converts the k8s affinity type to the GCE affinity type.
func TranslateAffinityType(affinityType string, logger klog.Logger) string {
	switch affinityType {
	case string(api_v1.ServiceAffinityClientIP):
		return gceAffinityTypeClientIP
	case string(api_v1.ServiceAffinityNone):
		return gceAffinityTypeNone
	default:
		logger.Error(nil, "Unexpected affinity type", "affinityType", affinityType)
		return gceAffinityTypeNone
	}
}

// IsLegacyL4ILBService returns true if the given LoadBalancer service is managed by service controller.
func IsLegacyL4ILBService(svc *api_v1.Service) bool {
	if svc.Spec.LoadBalancerClass != nil {
		return annotations.HasLoadBalancerClass(svc, annotations.LegacyRegionalInternalLoadBalancerClass)
	}
	return slice.ContainsString(svc.ObjectMeta.Finalizers, common.LegacyILBFinalizer, nil)
}

// IsSubsettingL4ILBService returns true if the given LoadBalancer service is managed by NEG and L4 controller.
func IsSubsettingL4ILBService(svc *api_v1.Service) bool {
	if svc.Spec.LoadBalancerClass != nil {
		return annotations.HasLoadBalancerClass(svc, annotations.RegionalInternalLoadBalancerClass)
	}
	return HasL4ILBFinalizerV2(svc)
}

// HasL4ILBFinalizerV2 returns true if the given Service has ILBFinalizerV2
func HasL4ILBFinalizerV2(svc *api_v1.Service) bool {
	return slice.ContainsString(svc.ObjectMeta.Finalizers, common.ILBFinalizerV2, nil)
}

// HasL4NetLBFinalizerV2 returns true if the given Service has NetLBFinalizerV2
func HasL4NetLBFinalizerV2(svc *api_v1.Service) bool {
	return slice.ContainsString(svc.ObjectMeta.Finalizers, common.NetLBFinalizerV2, nil)
}

// HasL4NetLBFinalizerV3 returns true if the given Service has NetLBFinalizerV3
func HasL4NetLBFinalizerV3(svc *api_v1.Service) bool {
	return slice.ContainsString(svc.ObjectMeta.Finalizers, common.NetLBFinalizerV3, nil)
}

func LegacyForwardingRuleName(svc *api_v1.Service) string {
	return cloudprovider.DefaultLoadBalancerName(svc)
}

// L4LBResourceDescription stores the description fields for L4 ILB or NetLB resources.
// This is useful to identify which resources correspond to which L4 LB service.
type L4LBResourceDescription struct {
	// ServiceName indicates the name of the service the resource is for.
	ServiceName string `json:"networking.gke.io/service-name"`
	// APIVersion stores the version og the compute API used to create this resource.
	APIVersion          meta.Version `json:"networking.gke.io/api-version,omitempty"`
	ServiceIP           string       `json:"networking.gke.io/service-ip,omitempty"`
	ResourceDescription string       `json:"networking.gke.io/resource-description,omitempty"`
}

// Marshal returns the description as a JSON-encoded string.
func (d *L4LBResourceDescription) Marshal() (string, error) {
	out, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	return string(out), err
}

// Unmarshal converts the JSON-encoded description string into the struct.
func (d *L4LBResourceDescription) Unmarshal(desc string) error {
	return json.Unmarshal([]byte(desc), d)
}

func MakeL4LBFirewallDescription(svcName, ip string, version meta.Version, shared bool) (string, error) {
	if shared {
		return (&L4LBResourceDescription{APIVersion: version, ResourceDescription: fmt.Sprintf(L4LBSharedResourcesDesc, "")}).Marshal()
	}
	return (&L4LBResourceDescription{ServiceName: svcName, ServiceIP: ip, APIVersion: version}).Marshal()
}

func MakeL4LBServiceDescription(svcName, ip string, version meta.Version, shared bool, lbType L4LBType) (string, error) {
	if shared {
		return (&L4LBResourceDescription{APIVersion: version, ResourceDescription: fmt.Sprintf(L4LBSharedResourcesDesc, lbType.ToString())}).Marshal()
	}
	return (&L4LBResourceDescription{ServiceName: svcName, ServiceIP: ip, APIVersion: version}).Marshal()
}

func MakeL4IPv6ForwardingRuleDescription(service *api_v1.Service) (string, error) {
	return (&L4LBResourceDescription{ServiceName: ServiceKeyFunc(service.Namespace, service.Name)}).Marshal()
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

// IsLoadBalancerServiceType checks if kubernetes service is type of LoadBalancer.
func IsLoadBalancerServiceType(service *api_v1.Service) bool {
	if service == nil {
		return false
	}
	return service.Spec.Type == api_v1.ServiceTypeLoadBalancer
}

func AddIPToLBStatus(status *api_v1.LoadBalancerStatus, ips ...string) *api_v1.LoadBalancerStatus {
	if status == nil {
		status = &api_v1.LoadBalancerStatus{
			Ingress: []api_v1.LoadBalancerIngress{},
		}
	}

	for _, ip := range ips {
		status.Ingress = append(status.Ingress, api_v1.LoadBalancerIngress{IP: ip})
	}
	return status
}

func SubnetHasIPv6Range(cloud *gce.Cloud, subnetName, ipv6AccessType string) (bool, error) {
	subnet, err := cloud.GetSubnetwork(cloud.Region(), subnetName)
	if err != nil {
		return false, fmt.Errorf("failed getting subnet: %w", err)
	}
	return subnet.StackType == DualStackSubnetStackType && subnet.Ipv6AccessType == ipv6AccessType, nil
}

// IsUnsupportedFeatureError returns true if the error has 400 number,
// and has information that `featureName` isn't supported
//
//	ex: ```Error 400: Invalid value for field
//	resource.connectionTrackingPolicy.enableStrongAffinity': 'true'.
//	EnableStrongAffinity is not supported., invalid```
func IsUnsupportedFeatureError(err error, featureName string) bool {
	if err == nil {
		return false
	}
	isUnsupported := strings.Contains(err.Error(), "is not supported") && IsHTTPErrorCode(err, http.StatusBadRequest)
	if strings.Contains(err.Error(), featureName) && isUnsupported {
		return true
	}
	return false
}

// GetDomainFromGABasePath takes a GA base path of the form <path>/compute/v1 and returns the path.
func GetDomainFromGABasePath(basePath string) string {
	// Trim URL to remove the "/v1" part since we are using the GA path.
	// Start by trimming any trailing "/"
	domain := strings.TrimSuffix(basePath, "/")
	domain = strings.TrimSuffix(domain, "/compute/v1")
	return domain
}

// FilterAPIVersionFromResourcePath removes the /v1 /beta /alpha from the resource path
func FilterAPIVersionFromResourcePath(url string) string {
	computeIndex := strings.Index(url, "/compute/")
	if computeIndex == -1 {
		return url
	}

	pathStartIndex := computeIndex + len("/compute/")

	// if the URL ends with "/compute/" there is no version to remove
	if pathStartIndex >= len(url) {
		return url
	}

	baseUrlPart := url[:pathStartIndex]
	pathAfterCompute := url[pathStartIndex:]

	firstSlashIndex := strings.Index(pathAfterCompute, "/")
	if firstSlashIndex == -1 {
		// This case would mean the url is something like ".../compute/v1", without a resource path.
		// in this case with return the first part of the url ".../compute/"
		return baseUrlPart
	}

	// reconstruct the URL removing the version segment
	resourcePathPart := pathAfterCompute[firstSlashIndex+1:]

	return baseUrlPart + resourcePathPart
}
