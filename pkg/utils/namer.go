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

package utils

import (
	"crypto/md5"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/golang/glog"
)

const (
	defaultPrefix = "k8s"

	// A single target proxy/urlmap/forwarding rule is created per loadbalancer.
	// Tagged with the namespace/name of the Ingress.
	targetHTTPProxyPrefix  = "tp"
	targetHTTPSProxyPrefix = "tps"
	sslCertPrefix          = "ssl"
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

	// DefaultBackendKey is the key used to transmit the defaultBackend
	// through a urlmap. It's not a valid subdomain, and it is a catch
	// all path.  TODO: Find a better way to transmit this, once we've
	// decided on default backend semantics (i.e do we want a default
	// per host, per lb etc).
	DefaultBackendKey = "DefaultBackend"

	// maxNEGDescriptiveLabel is the max length for namespace, name and
	// port for neg name.  63 - 5 (k8s and naming schema version prefix)
	// - 16 (cluster id) - 8 (suffix hash) - 4 (hyphen connector) = 30
	maxNEGDescriptiveLabel = 30

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
type NameComponents struct {
	ClusterName, Resource, Metadata string
}

// SetUID sets the UID/name of this cluster.
func (n *Namer) SetUID(name string) {
	n.nameLock.Lock()
	defer n.nameLock.Unlock()

	if strings.Contains(name, clusterNameDelimiter) {
		tokens := strings.Split(name, clusterNameDelimiter)
		glog.Warningf("Given name %v contains %v, taking last token in: %+v", name, clusterNameDelimiter, tokens)
		name = tokens[len(tokens)-1]
	}
	glog.Infof("Changing cluster name from %v to %v", n.clusterName, name)
	n.clusterName = name
}

// SetFirewall sets the firewall name of this cluster.
func (n *Namer) SetFirewall(name string) {
	n.nameLock.Lock()
	defer n.nameLock.Unlock()

	if n.firewallName != name {
		glog.Infof("Changing firewall name from %v to %v", n.firewallName, name)
		n.firewallName = name
	}
}

// UID returns the UID/name of this cluster.
func (n *Namer) UID() string {
	n.nameLock.Lock()
	defer n.nameLock.Unlock()

	return n.clusterName
}

// Firewall returns the firewall name of this cluster.
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
	if clusterName == "" {
		return name
	}
	return truncate(fmt.Sprintf("%v%v%v", name, clusterNameDelimiter, clusterName))
}

// ParseName parses the name of a resource generated by the namer.
// This is only valid for the following resources:
//
// Backend, InstanceGroup, TargetProxy, SSLCert, ForwardingRule,
// UrlMap.
func (n *Namer) ParseName(name string) *NameComponents {
	l := strings.Split(name, clusterNameDelimiter)
	var uid, resource string
	if len(l) >= 2 {
		uid = l[len(l)-1]
	}
	c := strings.Split(name, "-")
	if len(c) >= 2 {
		resource = c[1]
	}
	return &NameComponents{
		ClusterName: uid,
		Resource:    resource,
	}
}

// NameBelongsToCluster checks if a given name is tagged with this
// cluster's UID.
func (n *Namer) NameBelongsToCluster(name string) bool {
	if !strings.HasPrefix(name, n.prefix+"-") {
		return false
	}

	parts := strings.Split(name, clusterNameDelimiter)
	clusterName := n.UID()
	if len(parts) == 1 {
		return clusterName == ""
	}

	if len(parts) > 2 {
		return false
	}

	return parts[1] == clusterName
}

// Backend constructs the name for a backend.
func (n *Namer) Backend(port int64) string {
	return n.decorateName(fmt.Sprintf("%v-%v-%d", n.prefix, backendPrefix, port))
}

// BackendPort retrieves the port from the given backend name.
func (n *Namer) BackendPort(beName string) (string, error) {
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

// TargetProxy returns the name for target proxy given the load
// balancer name and the protocol.
func (n *Namer) TargetProxy(lbName string, protocol NamerProtocol) string {
	switch protocol {
	case HTTPProtocol:
		return truncate(fmt.Sprintf("%v-%v-%v", n.prefix, targetHTTPProxyPrefix, lbName))
	case HTTPSProtocol:
		return truncate(fmt.Sprintf("%v-%v-%v", n.prefix, targetHTTPSProxyPrefix, lbName))
	}
	glog.Fatalf("Invalid TargetProxy protocol: %v", protocol)
	return "invalid"
}

// IsSSLCert returns true if certName is an Ingress managed name.
func (n *Namer) IsSSLCert(name string) bool {
	return strings.HasPrefix(name, n.prefix+"-"+sslCertPrefix)
}

// SSLCert returns the name of the certificate. isPrimary denotes
// whether the name is for a primary certificate or secondary.
func (n *Namer) SSLCert(lbName string, isPrimary bool) string {
	if isPrimary {
		return truncate(fmt.Sprintf("%v-%v-%v", n.prefix, sslCertPrefix, lbName))
	}
	return truncate(fmt.Sprintf("%v-%v-%d-%v", n.prefix, sslCertPrefix, 1, lbName))
}

// ForwardingRule returns the name of the forwarding rule prefix.
func (n *Namer) ForwardingRule(lbName string, protocol NamerProtocol) string {
	switch protocol {
	case HTTPProtocol:
		return truncate(fmt.Sprintf("%v-%v-%v", n.prefix, forwardingRulePrefix, lbName))
	case HTTPSProtocol:
		return truncate(fmt.Sprintf("%v-%v-%v", n.prefix, httpsForwardingRulePrefix, lbName))
	}
	glog.Fatalf("invalid ForwardingRule protocol: %q", protocol)
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
//   {prefix}{version}-{clusterid}-{namespace}-{name}-{target port}-{hash}
//
// Output name is at most 63 characters. NEG tries to keep as much
// information as possible.
//
// WARNING: Controllers depend on the naming pattern to get the list
// of all NEGs associated with the current cluster. Any modifications
// must be backward compatible.
func (n *Namer) NEG(namespace, name, port string) string {
	trimmedFields := trimFieldsEvenly(maxNEGDescriptiveLabel, namespace, name, port)
	trimedNamespace := trimmedFields[0]
	trimedName := trimmedFields[1]
	trimedPort := trimmedFields[2]
	return fmt.Sprintf("%s-%s-%s-%s-%s", n.negPrefix(), trimedNamespace, trimedName, trimedPort, negSuffix(namespace, name, port))
}

// IsNEG returns true if the name is a NEG owned by this cluster.
func (n *Namer) IsNEG(name string) bool {
	return strings.HasPrefix(name, n.negPrefix())
}

func (n *Namer) negPrefix() string {
	return fmt.Sprintf("%s%s-%s", n.prefix, schemaVersionV1, n.UID())
}

// negSuffix returns hash code with 8 characters
func negSuffix(namespace, name, port string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(namespace+name+port)))[:8]
}
