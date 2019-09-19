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

package namer

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"k8s.io/klog"
)

const (
	defaultPrefix = "k8s"

	// A single target proxy/urlmap/forwarding rule is created per loadbalancer.
	// Tagged with the namespace/name of the Ingress.
	targetHTTPProxyPrefix  = "tp"
	targetHTTPSProxyPrefix = "tps"
	// This prefix is used along with namespace/name of ingress in legacy cert names. New names use this prefix along
	// with hash of the ingress/namespace name and cert contents.
	sslCertPrefix = "ssl"
	// TODO: this should really be "fr" and "frs".
	forwardingRulePrefix      = "fw"
	httpsForwardingRulePrefix = "fws"
	urlMapPrefix              = "um"

	// This allows sharing of backends across loadbalancers.
	backendPrefix = "be"
	backendRegex  = "be-([0-9]+).*"

	// Prefix used for instance groups involved in L7 balancing.
	igPrefix = "ig"

	// Suffix used in the l7 firewall rule. There is currently only one.
	// Note that this name is used by the cloudprovider lib that inserts
	// its own k8s-fw prefix.
	globalFirewallSuffix = "l7"

	// A delimiter used for clarity in naming GCE resources.
	clusterNameDelimiter = "--"

	// Arbitrarily chosen alphanumeric character to use in constructing
	// resource names, eg: to avoid cases where we end up with a name
	// ending in '-'.
	alphaNumericChar = "0"

	// Names longer than this are truncated, because of GCE
	// restrictions. Note that this is less than 63 as one character is
	// reserved at the end for adding alphaNumericChar to the end to
	// avoid terminating on an invalid character ('-').
	nameLenLimit = 62

	// clusterNameEvalThreshold is the minimum required length of the clusterName section
	// in the suffix of a GCE resource in order to qualify for evaluating if a given name
	// belong to the current cluster. This is for minimizing chances of cluster name collision due
	// matching uid prefix.
	clusterNameEvalThreshold = 4

	// maxNEGDescriptiveLabel is the max length for namespace, name and
	// port for neg name.  63 - 5 (k8s and naming schema version prefix)
	// - 8 (truncated cluster id) - 8 (suffix hash) - 4 (hyphen connector) = 38
	maxNEGDescriptiveLabel = 38

	// schemaVersionV1 is the version 1 naming scheme for NEG
	schemaVersionV1 = "1"
)

// NamerProtocol is an enum for the different protocols given as
// parameters to Namer.
type NamerProtocol string

const (
	HTTPProtocol  NamerProtocol = "HTTP"
	HTTPSProtocol NamerProtocol = "HTTPS"
)

// Namer is the centralized naming policy for Ingress-related GCP
// resources.
type Namer struct {
	// Prefix for all Namer generated names. By default, this is the
	// DefaultPrefix.
	prefix string

	nameLock     sync.Mutex
	clusterName  string
	firewallName string
}

// NewNamer creates a new namer with a Cluster and Firewall name.
func NewNamer(clusterName, firewallName string) *Namer {
	return NewNamerWithPrefix(defaultPrefix, clusterName, firewallName)
}

// NewNamerWithPrefix creates a new namer with a custom prefix.
func NewNamerWithPrefix(prefix, clusterName, firewallName string) *Namer {
	namer := &Namer{prefix: prefix}
	namer.SetUID(clusterName)
	namer.SetFirewall(firewallName)

	return namer
}

// NameComponents is a struct representing the components of a a GCE
// resource name constructed by the namer. The format of such a name
// is: k8s-resource-<metadata, eg port>--uid
// Note that the LbName field is empty if the resource is a BackendService.
type NameComponents struct {
	ClusterName, Resource, Metadata, LbName string
}

// SetUID sets the UID/name of this cluster.
func (n *Namer) SetUID(name string) {
	n.nameLock.Lock()
	defer n.nameLock.Unlock()

	if strings.Contains(name, clusterNameDelimiter) {
		tokens := strings.Split(name, clusterNameDelimiter)
		klog.Warningf("Name %q contains %q, taking last token in: %+v", name, clusterNameDelimiter, tokens)
		name = tokens[len(tokens)-1]
	}

	if n.clusterName == name {
		klog.V(4).Infof("Cluster name is unchanged (%q)", name)
		return
	}

	klog.Infof("Changing cluster name from %q to %q", n.clusterName, name)
	n.clusterName = name
}

// SetFirewall sets the firewall name of this cluster.
func (n *Namer) SetFirewall(name string) {
	n.nameLock.Lock()
	defer n.nameLock.Unlock()

	if n.firewallName != name {
		klog.Infof("Changing firewall name from %q to %q", n.firewallName, name)
		n.firewallName = name
	}
}

// UID returns the UID/name of this cluster.
func (n *Namer) UID() string {
	n.nameLock.Lock()
	defer n.nameLock.Unlock()

	return n.clusterName
}

func (n *Namer) shortUID() string {
	uid := n.UID()
	if len(uid) <= 8 {
		return uid
	}
	return uid[:8]
}

// GetFirewallName returns the firewall name of this cluster.
func (n *Namer) Firewall() string {
	n.nameLock.Lock()
	defer n.nameLock.Unlock()

	// Retain backwards compatible behavior where firewallName == clusterName.
	if n.firewallName == "" {
		return n.clusterName
	}
	return n.firewallName
}

// truncate truncates the given key to a GCE length limit.
func truncate(key string) string {
	if len(key) > nameLenLimit {
		// GCE requires names to end with an alphanumeric, but allows
		// characters like '-', so make sure the trucated name ends
		// legally.
		return fmt.Sprintf("%v%v", key[:nameLenLimit], alphaNumericChar)
	}
	return key
}

func (n *Namer) decorateName(name string) string {
	clusterName := n.UID()
	// clusterName might be empty for legacy clusters
	if clusterName == "" {
		return name
	}
	return truncate(fmt.Sprintf("%v%v%v", name, clusterNameDelimiter, clusterName))
}

// ParseName parses the name of a resource generated by the namer.
// This is only valid for the following resources:
//
// Backend, InstanceGroup, UrlMap.
func (n *Namer) ParseName(name string) *NameComponents {
	l := strings.Split(name, clusterNameDelimiter)
	var uid, resource, lbName string
	if len(l) >= 2 {
		uid = l[len(l)-1]
	}

	// We want to split the remainder of the name, minus the cluster-delimited
	// portion. This should resemble:
	// UID-resource-loadbalancername
	c := strings.Split(l[0], "-")
	if len(c) >= 2 {
		resource = c[1]
	}
	if resource == sslCertPrefix {
		// For ssl certs, the cluster uid is followed by a hyphen and the cert hash, so one more string split needed.
		uid = strings.Split(uid, "-")[0]
	}

	if resource == urlMapPrefix {
		// It is possible for the loadbalancer name to have dashes in it - so we
		// join the remaining name parts.
		lbName = strings.Join(c[2:], "-")
	}

	return &NameComponents{
		ClusterName: uid,
		Resource:    resource,
		LbName:      lbName,
	}
}

// NameBelongsToCluster checks if a given name is tagged with this
// cluster's UID.
func (n *Namer) NameBelongsToCluster(name string) bool {
	// Name follows the NEG naming scheme
	if n.IsNEG(name) {
		return true
	}

	// Name follows the naming scheme where clusterid is the suffix.
	if !strings.HasPrefix(name, n.prefix+"-") {
		return false
	}
	fullClusterName := n.UID()
	components := n.ParseName(name)
	componentClusterName := components.ClusterName

	// if exact match is found, then return true immediately
	// otherwise, do best effort matching as follows
	if componentClusterName == fullClusterName {
		return true
	}

	// if the name is longer or equal to 63 charactors and the last character of the resource matches alphaNumericChar,
	// it is likely that the name was truncated.
	if len(name) > nameLenLimit && len(componentClusterName) > 0 && componentClusterName[len(componentClusterName)-1:] == alphaNumericChar {
		componentClusterName = componentClusterName[0 : len(componentClusterName)-1]
	}

	// If the name is longer or equal to 63 characters and the length of the
	// cluster name parsed from the resource name is too short, ignore the resource and do
	// not consider the resource managed by this cluster. This is to prevent cluster A
	// accidentally GC resources from cluster B due to both clusters share the same prefix
	// uid.
	// For example:
	// Case 1: k8s-fws-test-sandbox-50a6f22a4cd34e91-ingress-1--16a1467191ad30
	// The cluster name is 16a1467191ad30 which is longer than clusterNameEvalThreshold.
	// Case 2: k8s-fws-test-sandbox-50a6f22a4cd34e91-ingress-1111111111111--10
	// The cluster name is 10 which is shorter than clusterNameEvalThreshold.
	return len(componentClusterName) > clusterNameEvalThreshold && strings.HasPrefix(fullClusterName, componentClusterName)

}

// IGBackend constructs the name for a backend service targeting instance groups.
func (n *Namer) IGBackend(port int64) string {
	return n.decorateName(fmt.Sprintf("%v-%v-%d", n.prefix, backendPrefix, port))
}

// IGBackendPort retrieves the port from the given backend name.
func (n *Namer) IGBackendPort(beName string) (string, error) {
	r := regexp.MustCompile(n.prefix + "-" + backendRegex)
	match := r.FindStringSubmatch(beName)
	if len(match) < 2 {
		return "", fmt.Errorf("invalid backend name %q", beName)
	}
	_, err := strconv.Atoi(match[1])
	if err != nil {
		return "", fmt.Errorf("invalid backend name %q", beName)
	}
	return match[1], nil
}

// InstanceGroup constructs the name for an Instance Group.
func (n *Namer) InstanceGroup() string {
	return n.decorateName(n.prefix + "-" + igPrefix)
}

// firewallRuleSuffix constructs the glbc specific suffix for the FirewallRule.
func (n *Namer) firewallRuleSuffix() string {
	firewallName := n.Firewall()
	// The entire cluster only needs a single firewall rule.
	if firewallName == "" {
		return globalFirewallSuffix
	}
	return truncate(fmt.Sprintf("%v%v%v", globalFirewallSuffix, clusterNameDelimiter, firewallName))
}

// FirewallRule constructs the full firewall rule name, this is the name
// assigned by the cloudprovider lib + suffix from glbc, so we don't
// mix this rule with a rule created for L4 loadbalancing.
func (n *Namer) FirewallRule() string {
	return fmt.Sprintf("%s-fw-%s", n.prefix, n.firewallRuleSuffix())
}

// LoadBalancer constructs a loadbalancer name from the given key. The key
// is usually the namespace/name of a Kubernetes Ingress.
func (n *Namer) LoadBalancer(key string) string {
	// TODO: Pipe the clusterName through, for now it saves code churn
	// to just grab it globally, especially since we haven't decided how
	// to handle namespace conflicts in the Ubernetes context.
	parts := strings.Split(key, clusterNameDelimiter)
	scrubbedName := strings.Replace(key, "/", "-", -1)
	clusterName := n.UID()
	if clusterName == "" || parts[len(parts)-1] == clusterName {
		return scrubbedName
	}
	return truncate(fmt.Sprintf("%v%v%v", scrubbedName, clusterNameDelimiter, clusterName))
}

// LoadBalancerFromLbName reconstructs the full loadbalancer name, given the
// lbName portion from NameComponents
func (n *Namer) LoadBalancerFromLbName(lbName string) string {
	return truncate(fmt.Sprintf("%v%v%v", lbName, clusterNameDelimiter, n.UID()))
}

// TargetProxy returns the name for target proxy given the load
// balancer name and the protocol.
func (n *Namer) TargetProxy(lbName string, protocol NamerProtocol) string {
	switch protocol {
	case HTTPProtocol:
		return truncate(fmt.Sprintf("%v-%v-%v", n.prefix, targetHTTPProxyPrefix, lbName))
	case HTTPSProtocol:
		return truncate(fmt.Sprintf("%v-%v-%v", n.prefix, targetHTTPSProxyPrefix, lbName))
	}
	klog.Fatalf("Invalid TargetProxy protocol: %v", protocol)
	return "invalid"
}

// IsCertUsedForLB returns true if the resourceName belongs to this cluster's ingress.
// It checks that the hashed lbName exists and
func (n *Namer) IsCertUsedForLB(lbName, resourceName string) bool {
	lbNameHash := n.lbNameToHash(lbName)
	prefix := fmt.Sprintf("%s-%s-%s", n.prefix, sslCertPrefix, lbNameHash)
	return strings.HasPrefix(resourceName, prefix) && strings.HasSuffix(resourceName, n.UID())
}

func (n *Namer) lbNameToHash(lbName string) string {
	ingHash := fmt.Sprintf("%x", sha256.Sum256([]byte(lbName)))
	return ingHash[:16]
}

// IsLegacySSLCert returns true if certName is an Ingress managed name following the older naming convention. The check
// also ensures that the cert is managed by the specific ingress instance - lbName
func (n *Namer) IsLegacySSLCert(lbName string, resourceName string) bool {
	// old style name is of the form k8s-ssl-<lbname> or k8s-ssl-1-<lbName>.
	legacyPrefixPrimary := truncate(fmt.Sprintf("%s-%s-%s", n.prefix, sslCertPrefix, lbName))
	legacyPrefixSec := truncate(fmt.Sprintf("%s-%s-1-%s", n.prefix, sslCertPrefix, lbName))
	return strings.HasPrefix(resourceName, legacyPrefixPrimary) || strings.HasPrefix(resourceName, legacyPrefixSec)
}

// SSLCertName returns the name of the certificate.
func (n *Namer) SSLCertName(lbName string, secretHash string) string {
	lbNameHash := n.lbNameToHash(lbName)
	// k8s-ssl-[lbNameHash]-[certhash]--[clusterUID]
	return n.decorateName(fmt.Sprintf("%s-%s-%s-%s", n.prefix, sslCertPrefix, lbNameHash, secretHash))
}

// ForwardingRule returns the name of the forwarding rule prefix.
func (n *Namer) ForwardingRule(lbName string, protocol NamerProtocol) string {
	switch protocol {
	case HTTPProtocol:
		return truncate(fmt.Sprintf("%v-%v-%v", n.prefix, forwardingRulePrefix, lbName))
	case HTTPSProtocol:
		return truncate(fmt.Sprintf("%v-%v-%v", n.prefix, httpsForwardingRulePrefix, lbName))
	}
	klog.Fatalf("invalid ForwardingRule protocol: %q", protocol)
	return "invalid"
}

// UrlMap returns the name for the UrlMap for a given load balancer.
func (n *Namer) UrlMap(lbName string) string {
	return truncate(fmt.Sprintf("%v-%v-%v", n.prefix, urlMapPrefix, lbName))
}

// NamedPort returns the name for a named port.
func (n *Namer) NamedPort(port int64) string {
	return fmt.Sprintf("port%v", port)
}

// NEG returns the gce neg name based on the service namespace, name
// and target port. NEG naming convention:
//
//   {prefix}{version}-{clusterid}-{namespace}-{name}-{service port}-{hash}
//
// Output name is at most 63 characters. NEG tries to keep as much
// information as possible.
//
// WARNING: Controllers depend on the naming pattern to get the list
// of all NEGs associated with the current cluster. Any modifications
// must be backward compatible.
func (n *Namer) NEG(namespace, name string, port int32) string {
	portStr := fmt.Sprintf("%v", port)
	truncFields := TrimFieldsEvenly(maxNEGDescriptiveLabel, namespace, name, portStr)
	truncNamespace := truncFields[0]
	truncName := truncFields[1]
	truncPort := truncFields[2]
	return fmt.Sprintf("%s-%s-%s-%s-%s", n.negPrefix(), truncNamespace, truncName, truncPort, negSuffix(n.shortUID(), namespace, name, portStr, ""))
}

// NEGWithSubset returns the gce neg name based on the service namespace, name
// target port and Istio:DestinationRule subset. NEG naming convention:
//
//   {prefix}{version}-{clusterid}-{namespace}-{name}-{service port}-{destination rule subset}-{hash}
//
// Output name is at most 63 characters. NEG tries to keep as much
// information as possible.
//
// WARNING: Controllers depend on the naming pattern to get the list
// of all NEGs associated with the current cluster. Any modifications
// must be backward compatible.
func (n *Namer) NEGWithSubset(namespace, name, subset string, port int32) string {
	portStr := fmt.Sprintf("%v", port)
	truncFields := TrimFieldsEvenly(maxNEGDescriptiveLabel, namespace, name, portStr, subset)
	truncNamespace := truncFields[0]
	truncName := truncFields[1]
	truncPort := truncFields[2]
	truncSubset := truncFields[3]

	return fmt.Sprintf("%s-%s-%s-%s-%s-%s", n.negPrefix(), truncNamespace, truncName, truncPort, truncSubset, negSuffix(n.shortUID(), namespace, name, portStr, subset))
}

// IsNEG returns true if the name is a NEG owned by this cluster.
// It checks that the UID is present and a substring of the
// cluster uid, since the NEG naming schema truncates it to 8 characters.
// This is only valid for NEGs, BackendServices and Healthchecks for NEG.
func (n *Namer) IsNEG(name string) bool {
	return strings.HasPrefix(name, n.negPrefix())
}

func (n *Namer) negPrefix() string {
	return fmt.Sprintf("%s%s-%s", n.prefix, schemaVersionV1, n.shortUID())
}

// negSuffix returns hash code with 8 characters
func negSuffix(uid, namespace, name, port, subset string) string {
	negString := strings.Join([]string{uid, namespace, name, port}, ";")
	if subset != "" {
		negString = strings.Join([]string{negString, subset}, ";")
	}
	negHash := fmt.Sprintf("%x", sha256.Sum256([]byte(negString)))
	return negHash[:8]
}
