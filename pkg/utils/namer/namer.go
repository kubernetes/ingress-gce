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

	"k8s.io/klog/v2"
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
	redirectMapPrefix         = "rm"

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
	principalEntityNameDelimiter = "--"

	// Arbitrarily chosen alphanumeric character to use in constructing
	// resource names, eg: to avoid cases where we end up with a name
	// ending in '-'.
	alphaNumericChar = "0"

	// Names longer than this are truncated, because of GCE
	// restrictions. Note that this is less than 63 as one character is
	// reserved at the end for adding alphaNumericChar to the end to
	// avoid terminating on an invalid character ('-').
	nameLenLimit = 62

	// principalEntityNameEvalThreshold is the minimum required length of the principalEntityName section
	// in the suffix of a GCE resource in order to qualify for evaluating if a given name
	// belong to the current principal entity. This is for minimizing chances of principal entity name collision due
	// matching uid prefix.
	principalEntityNameEvalThreshold = 4

	// maxNEGDescriptiveLabel is the max length for namespace, name and
	// port for neg name.  63 - 5 (k8s and naming schema version prefix)
	// - 8 (truncated cluster id) - 8 (suffix hash) - 4 (hyphen connector) = 38
	maxNEGDescriptiveLabel = 38


	// schemaVersionV1 is the version 1 naming scheme for NEG
	schemaVersionV1 = "1"

	// Length of the subnet hash for non default subnet NEGs.
	subnetHashLength = 6

	MaxDefaultSubnetNegNameLength = 56
)

var ErrCustomNEGNameTooLong = fmt.Errorf("custom NEG name exceeds %v characters limit", MaxDefaultSubnetNegNameLength)

// NamerProtocol is an enum for the different protocols given as
// parameters to Namer.
type NamerProtocol string

// LoadBalancerName is the name of a GCE load-balancer for an ingress.
type LoadBalancerName string

// String typecasts LoadBalancerName to string type.
func (lbName LoadBalancerName) String() string {
	return string(lbName)
}

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
	// principalEntityName could be cluster UID, or tenant UID in multi-project scenario
	principalEntityName  string
	firewallName string
	negSchemaVersion    string
	maxNEGLabelLength      int

	logger klog.Logger
}

// NewNamer creates a new namer with an ID and Firewall name.
func NewNamer(principalEntityName, firewallName string, logger klog.Logger) *Namer {
	return NewNamerWithPrefix(defaultPrefix, principalEntityName, firewallName, logger)
}

// NewNamerWithPrefix creates a new namer with a custom prefix.
func NewNamerWithPrefix(prefix, principalEntityName, firewallName string, logger klog.Logger) *Namer {
	namer := &Namer{
		prefix: prefix, 
		logger: logger.WithName("Namer"),
		negSchemaVersion: schemaVersionV1, 
        maxNEGLabelLength:   maxNEGDescriptiveLabel,
	}
	namer.SetUID(principalEntityName)
	namer.SetFirewall(firewallName)

	return namer
}

// NameComponents is a struct representing the components of a a GCE
// resource name constructed by the namer. The format of such a name
// is: k8s-resource-<metadata, eg port>--uid
// Note that the LbNamePrefix field is empty if the resource is a BackendService.
type NameComponents struct {
	PrincipalEntityName, Resource, Metadata, LbNamePrefix string
}

// SetUID sets the UID/name of this principal entity.
func (n *Namer) SetUID(name string) {
	n.nameLock.Lock()
	defer n.nameLock.Unlock()

	if strings.Contains(name, principalEntityNameDelimiter) {
		tokens := strings.Split(name, principalEntityNameDelimiter)
		n.logger.Info(fmt.Sprintf("Name %q contains %q, taking last token in: %+v", name, principalEntityNameDelimiter, tokens))
		name = tokens[len(tokens)-1]
	}

	if n.principalEntityName == name {
		n.logger.V(4).Info("Principal name is unchanged", "principalEntityName", name)
		return
	}

	n.logger.Info("Changing Principal name", "oldprincipalEntityName", n.principalEntityName, "newprincipalEntityName", name)
	n.principalEntityName = name
}

// SetFirewall sets the firewall name of this cluster.
func (n *Namer) SetFirewall(name string) {
	n.nameLock.Lock()
	defer n.nameLock.Unlock()

	if n.firewallName != name {
		n.logger.Info("Changing firewall name", "oldFirewallName", n.firewallName, "newFirewallName", name)
		n.firewallName = name
	}
}

// UID returns the UID/name of this cluster or tenant.
// WARNING: Use KubeSystemUID instead
func (n *Namer) UID() string {
	n.nameLock.Lock()
	defer n.nameLock.Unlock()

	return n.principalEntityName
}

func (n *Namer) shortUID() string {
	uid := n.UID()
	if len(uid) <= 8 {
		return uid
	}
	return uid[:8]
}

// Firewall returns the firewall name of this cluster.
func (n *Namer) Firewall() string {
	n.nameLock.Lock()
	defer n.nameLock.Unlock()

	// Retain backwards compatible behavior where firewallName == cluster name.
	if n.firewallName == "" {
		return n.principalEntityName
	}
	return n.firewallName
}

// truncate truncates the given key to a GCE length limit.
func truncate(key string) string {
	if len(key) > nameLenLimit {
		// GCE requires names to end with an alphanumeric, but allows
		// characters like '-', so make sure the truncated name ends
		// legally.
		return fmt.Sprintf("%v%v", key[:nameLenLimit], alphaNumericChar)
	}
	return key
}

func (n *Namer) decorateName(name string) string {
	principalEntityID := n.UID()
	// idName might be empty for legacy clusters
	if principalEntityID == "" {
		return name
	}
	return truncate(fmt.Sprintf("%v%v%v", name, principalEntityNameDelimiter, principalEntityID))
}

// ParseName parses the name of a resource generated by the namer.
// This is only valid for the following resources:
//
// Backend, InstanceGroup, UrlMap.
func (n *Namer) ParseName(name string) *NameComponents {
	l := strings.Split(name, principalEntityNameDelimiter)
	var uid, resource, lbNamePrefix string
	if len(l) >= 2 {
		uid = l[len(l)-1]
	}

	// We want to split the remainder of the name, minus the principal entity-delimited
	// portion. This should resemble:
	// prefix-resource-loadbalancernameprefix
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
		lbNamePrefix = strings.Join(c[2:], "-")
	}

	return &NameComponents{
		PrincipalEntityName:       uid,
		Resource:     resource,
		LbNamePrefix: lbNamePrefix,
	}
}

// NameBelongsToEntity checks if a given name is tagged with this
// principal entity's UID.
func (n *Namer) NameBelongsToEntity(name string) bool {

	// Name follows the NEG naming scheme
	if n.IsNEG(name) {
		return true
	}

	// Name follows the naming scheme where principalEntityName is the suffix.
	if !strings.HasPrefix(name, n.prefix+"-") {
		return false
	}
	fullPrincipalEntityName := n.UID()
	components := n.ParseName(name)
	componentPrincipalEntityName := components.PrincipalEntityName

	// if exact match is found, then return true immediately
	// otherwise, do best effort matching a`s follows
	if componentPrincipalEntityName == fullPrincipalEntityName {
		return true
	}

	// if the name is longer or equal to 63 characters and the last character of the resource matches alphaNumericChar,
	// it is likely that the name was truncated.
	if len(name) > nameLenLimit && len(componentPrincipalEntityName) > 0 && componentPrincipalEntityName[len(componentPrincipalEntityName)-1:] == alphaNumericChar {
		componentPrincipalEntityName = componentPrincipalEntityName[0 : len(componentPrincipalEntityName)-1]
	}

	// If the name is longer or equal to 63 characters and the length of the
	// principal entity name parsed from the resource name is too short, ignore the resource and do
	// not consider the resource managed by this principal entity. This is to prevent principal entity A
	// accidentally GC resources from principal entity B due to both entities share the same prefix
	// uid.
	// For example:
	// Case 1: k8s-fws-test-sandbox-50a6f22a4cd34e91-ingress-1--16a1467191ad30
	// The principal entity name is 16a1467191ad30 which is longer than principalEntityNameEvalThreshold.
	// Case 2: k8s-fws-test-sandbox-50a6f22a4cd34e91-ingress-1111111111111--10
	// The principal entity name is 10 which is shorter than principalEntityNameEvalThreshold.
	return len(componentPrincipalEntityName) > principalEntityNameEvalThreshold && strings.HasPrefix(fullPrincipalEntityName, componentPrincipalEntityName)

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
	return truncate(fmt.Sprintf("%v%v%v", globalFirewallSuffix, principalEntityNameDelimiter, firewallName))
}

// FirewallRule constructs the full firewall rule name, this is the name
// assigned by the cloudprovider lib + suffix from glbc, so we don't
// mix this rule with a rule created for L4 loadbalancing.
func (n *Namer) FirewallRule() string {
	return fmt.Sprintf("%s-fw-%s", n.prefix, n.firewallRuleSuffix())
}

// LoadBalancer constructs a loadbalancer name from the given key. The key
// is usually the namespace/name of a Kubernetes Ingress.
func (n *Namer) LoadBalancer(key string) LoadBalancerName {
	// TODO: Pipe the principalEntityName through, for now it saves code churn
	// to just grab it globally, especially since we haven't decided how
	// to handle namespace conflicts in the Kubernetes context.
	parts := strings.Split(key, principalEntityNameDelimiter)
	scrubbedName := strings.Replace(key, "/", "-", -1)
	principalEntityName := n.UID()
	if principalEntityName == "" || parts[len(parts)-1] == principalEntityName {
		return LoadBalancerName(scrubbedName)
	}
	return LoadBalancerName(truncate(fmt.Sprintf("%v%v%v", scrubbedName, principalEntityNameDelimiter, principalEntityName)))
}

// LoadBalancerForURLMap returns the loadbalancer name for given URL map.
func (n *Namer) LoadBalancerForURLMap(urlMap string) LoadBalancerName {
	return LoadBalancerName(truncate(fmt.Sprintf("%v%v%v", n.ParseName(urlMap).LbNamePrefix, principalEntityNameDelimiter, n.UID())))
}

// TargetProxy returns the name for target proxy given the load
// balancer name and the protocol.
func (n *Namer) TargetProxy(lbName LoadBalancerName, protocol NamerProtocol) string {
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
func (n *Namer) IsCertUsedForLB(lbName LoadBalancerName, resourceName string) bool {
	lbNameHash := n.lbNameToHash(lbName)
	prefix := fmt.Sprintf("%s-%s-%s", n.prefix, sslCertPrefix, lbNameHash)
	return strings.HasPrefix(resourceName, prefix) && strings.HasSuffix(resourceName, n.UID())
}

func (n *Namer) lbNameToHash(lbName LoadBalancerName) string {
	ingHash := fmt.Sprintf("%x", sha256.Sum256([]byte(lbName)))
	return ingHash[:16]
}

// IsLegacySSLCert returns true if certName is an Ingress managed name following the older naming convention. The check
// also ensures that the cert is managed by the specific ingress instance - lbName
func (n *Namer) IsLegacySSLCert(lbName LoadBalancerName, resourceName string) bool {
	// old style name is of the form k8s-ssl-<lbname> or k8s-ssl-1-<lbName>.
	legacyPrefixPrimary := truncate(fmt.Sprintf("%s-%s-%s", n.prefix, sslCertPrefix, lbName))
	legacyPrefixSec := truncate(fmt.Sprintf("%s-%s-1-%s", n.prefix, sslCertPrefix, lbName))
	return strings.HasPrefix(resourceName, legacyPrefixPrimary) || strings.HasPrefix(resourceName, legacyPrefixSec)
}

// SSLCertName returns the name of the certificate.
func (n *Namer) SSLCertName(lbName LoadBalancerName, secretHash string) string {
	lbNameHash := n.lbNameToHash(lbName)
	// k8s-ssl-[lbNameHash]-[certhash]--[clusterUID]
	return n.decorateName(fmt.Sprintf("%s-%s-%s-%s", n.prefix, sslCertPrefix, lbNameHash, secretHash))
}

// ForwardingRule returns the name of the forwarding rule prefix.
func (n *Namer) ForwardingRule(lbName LoadBalancerName, protocol NamerProtocol) string {
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
func (n *Namer) UrlMap(lbName LoadBalancerName) string {
	return truncate(fmt.Sprintf("%v-%v-%v", n.prefix, urlMapPrefix, lbName))
}

// RedirectUrlMap returns the name for the UrlMap for a given load balancer.
func (n *Namer) RedirectUrlMap(lbName LoadBalancerName) string {
	return truncate(fmt.Sprintf("%v-%v-%v", n.prefix, redirectMapPrefix, lbName))
}

// NamedPort returns the name for a named port.
func (n *Namer) NamedPort(port int64) string {
	return fmt.Sprintf("port%v", port)
}

// NEG returns the gce neg name based on the service namespace, name
// and target port. NEG naming convention:
//
//	{prefix}{version}-{principalEntityName}-{namespace}-{name}-{service port}-{hash}
//
// Output name is at most 63 characters. NEG tries to keep as much
// information as possible.
//
// WARNING: Controllers depend on the naming pattern to get the list
// of all NEGs associated with the current principal entity. Any modifications
// must be backward compatible.
func (n *Namer) NEG(namespace, name string, port int32) string {
	portStr := fmt.Sprintf("%v", port)
	truncFields := TrimFieldsEvenly(n.maxNEGLabelLength, namespace, name, portStr)
	truncNamespace := truncFields[0]
	truncName := truncFields[1]
	truncPort := truncFields[2]
	return fmt.Sprintf("%s-%s-%s-%s-%s", n.negPrefix(), truncNamespace, truncName, truncPort, negSuffix(n.shortUID(), namespace, name, portStr, ""))
}

// NonDefaultSubnetNEG returns the gce neg name in non default subnet based on
// the service namespace, name, target port and subnet name. NEG naming convention:
//
//	{prefix}{version}-{principalEntityName}-{namespace}-{name}-{service port}-{subnetHash}-{combinedHash}
//
// subnetHash length = 6, combinedHash length = 8, and the remainings are trimmed evenly.
// Output name is at most 63 characters. NEG tries to keep as much
// information as possible, while avoiding potential conflicts with the NEGs in
// the default subnet, or conflicts due to the naming pattern for non default
// subnets(e.g.: us-central1-subnet, us-central2-subnet).
func (n *Namer) NonDefaultSubnetNEG(namespace, name, subnetName string, port int32) string {
	portStr := fmt.Sprintf("%v", port)
	hashedSubnet := subnetHash(subnetName)
	truncFields := TrimFieldsEvenly(n.maxNEGLabelLength-subnetHashLength-1, namespace, name, portStr)
	truncNamespace := truncFields[0]
	truncName := truncFields[1]
	truncPort := truncFields[2]
	return fmt.Sprintf("%s-%s-%s-%s-%s-%s", n.negPrefix(), truncNamespace, truncName, truncPort, hashedSubnet, negSuffix(n.shortUID(), namespace, name, portStr, ""))
}

// RXLBBackendName returns the gce Backend name based on the service namespace, name
// and target port. Naming convention:
//
//	{prefix}{version}-{principalEntityName}-e-{namespace}-{name}-{service port}-{hash}
//
// Output name is at most 63 characters.
func (n *Namer) RXLBBackendName(namespace, name string, port int32) string {
	portStr := fmt.Sprintf("%v", port)
	// minus 2, as we added "-e" to prefix
	truncFields := TrimFieldsEvenly(n.maxNEGLabelLength-2, namespace, name, portStr)
	truncNamespace := truncFields[0]
	truncName := truncFields[1]
	truncPort := truncFields[2]
	return fmt.Sprintf("%s-e-%s-%s-%s-%s", n.negPrefix(), truncNamespace, truncName, truncPort, negSuffix(n.shortUID(), namespace, name, portStr, ""))
}

// NonDefaultSubnetCustomNEG returns the gce neg name in the non-default subnet
// when the NEG name is a custom one.
func (n *Namer) NonDefaultSubnetCustomNEG(customNEGName, subnetName string) (string, error) {
	if len(customNEGName) > MaxDefaultSubnetNegNameLength {
		return "", ErrCustomNEGNameTooLong
	}
	return fmt.Sprintf("%s-%s", customNEGName, subnetHash(subnetName)), nil
}

// IsNEG returns true if the name is a NEG owned by this principal entity.
// It checks that the UID is present and a substring of the
// principal entity uid, since the NEG naming schema truncates it to 8 characters.
// This is only valid for NEGs, BackendServices and Healthchecks for NEG.
func (n *Namer) IsNEG(name string) bool {
	return strings.HasPrefix(name, n.negPrefix())
}

// L4Backend is only supported by L4Namer.
func (namer *Namer) L4Backend(namespace, name string) string {
	return ""
}

func (n *Namer) negPrefix() string {
	return fmt.Sprintf("%s%s-%s", n.prefix, n.negSchemaVersion, n.shortUID())
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

// subnetHash returns hash code with 6 characters
func subnetHash(subnetName string) string {
	subnetHash := fmt.Sprintf("%x", sha256.Sum256([]byte(subnetName)))
	return subnetHash[:6]
}
