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
	"strings"

	"k8s.io/klog"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	api_v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/flags"
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
	// This is a duplicate definition of the constant in:
	// kubernetes/kubernetes/pkg/controller/service/service_controller.go
	// This label is feature-gated in kubernetes/kubernetes but we do not have feature gates
	// This will need to be updated after the end of the alpha
	LabelNodeRoleExcludeBalancer = "alpha.service-controller.kubernetes.io/exclude-balancer"
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

// TrimFieldsEvenly trims the fields evenly and keeps the total length
// <= max. Truncation is spread in ratio with their original length,
// meaning smaller fields will be truncated less than longer ones.
func TrimFieldsEvenly(max int, fields ...string) []string {
	if max <= 0 {
		return fields
	}
	total := 0
	for _, s := range fields {
		total += len(s)
	}
	if total <= max {
		return fields
	}
	// Distribute truncation evenly among the fields.
	excess := total - max
	remaining := max
	var lengths []int
	for _, s := range fields {
		// Scale truncation to shorten longer fields more than ones that are already short.
		l := len(s) - len(s)*excess/total - 1
		lengths = append(lengths, l)
		remaining -= l
	}
	// Add fractional space that was rounded down.
	for i := 0; i < remaining; i++ {
		lengths[i]++
	}

	var ret []string
	for i, l := range lengths {
		ret = append(ret, fields[i][:l])
	}

	return ret
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
func IsGCEIngress(ing *extensions.Ingress) bool {
	class := annotations.FromIngress(ing).IngressClass()
	if flags.F.IngressClass == "" {
		return class == "" || class == annotations.GceIngressClass
	}
	return class == flags.F.IngressClass
}

// IsGCEMultiClusterIngress returns true if the given Ingress has
// ingress.class annotation set to "gce-multi-cluster".
func IsGCEMultiClusterIngress(ing *extensions.Ingress) bool {
	class := annotations.FromIngress(ing).IngressClass()
	return class == annotations.GceMultiIngressClass
}

// IsGLBCIngress returns true if the given Ingress should be processed by GLBC
func IsGLBCIngress(ing *extensions.Ingress) bool {
	return IsGCEIngress(ing) || IsGCEMultiClusterIngress(ing)
}

// GetReadyNodeNames returns names of schedulable, ready nodes from the node lister
// It also filters out masters and nodes excluded from load-balancing
// TODO(rramkumar): Add a test for this.
func GetReadyNodeNames(lister listers.NodeLister) ([]string, error) {
	var nodeNames []string
	nodes, err := lister.ListWithPredicate(GetNodeConditionPredicate())
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

// This is a duplicate definition of the function in:
// kubernetes/kubernetes/pkg/controller/service/service_controller.go
func GetNodeConditionPredicate() listers.NodeConditionPredicate {
	return func(node *api_v1.Node) bool {
		// We add the master to the node list, but its unschedulable.  So we use this to filter
		// the master.
		if node.Spec.Unschedulable {
			return false
		}

		// As of 1.6, we will taint the master, but not necessarily mark it unschedulable.
		// Recognize nodes labeled as master, and filter them also, as we were doing previously.
		if _, hasMasterRoleLabel := node.Labels[LabelNodeRoleMaster]; hasMasterRoleLabel {
			return false
		}

		if _, hasExcludeBalancerLabel := node.Labels[LabelNodeRoleExcludeBalancer]; hasExcludeBalancerLabel {
			return false
		}

		// If we have no info, don't accept
		if len(node.Status.Conditions) == 0 {
			return false
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

func IngressKeyFunc(ing *extensions.Ingress) string {
	if ing == nil {
		return ""
	}
	return types.NamespacedName{Namespace: ing.Namespace, Name: ing.Name}.String()
}

// TraverseIngressBackends traverse thru all backends specified in the input ingress and call process
// If process return true, then return and stop traversing the backends
func TraverseIngressBackends(ing *extensions.Ingress, process func(id ServicePortID) bool) {
	if ing == nil {
		return
	}
	// Check service of default backend
	if ing.Spec.Backend != nil {
		if process(ServicePortID{Service: types.NamespacedName{Namespace: ing.Namespace, Name: ing.Spec.Backend.ServiceName}, Port: ing.Spec.Backend.ServicePort}) {
			return
		}
	}

	// Check the target service for each path rule
	for _, rule := range ing.Spec.Rules {
		if rule.IngressRuleValue.HTTP == nil {
			continue
		}
		for _, p := range rule.IngressRuleValue.HTTP.Paths {
			if process(ServicePortID{Service: types.NamespacedName{Namespace: ing.Namespace, Name: p.Backend.ServiceName}, Port: p.Backend.ServicePort}) {
				return
			}
		}
	}
	return
}

func ServiceKeyFunc(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}
