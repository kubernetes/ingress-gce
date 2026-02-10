package namer

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/ingress-gce/pkg/utils/common"
)

// maximumL4CombinedLength is the maximum combined length of namespace and
// name portions in the L4 LB resource name. The name format is:
// k8s2-{uid}-{ns}-{name}-{suffix}
// This is computed by subtracting: k8s2 - 4, dashes - 4,
// kubesystemUID - 8,  suffix - 8, from the total max len of gce resource = 63
// value is 63 - 24 = 39
// for forwarding rule, 4 more chars need to be subtracted.
const (
	maximumL4CombinedLength     = 39
	sharedHcSuffix              = "l4-shared-hc"
	firewallHcSuffix            = "-fw"
	ipv6Suffix                  = "ipv6"
	sharedFirewallHcSuffix      = sharedHcSuffix + firewallHcSuffix
	maxResourceNameLength       = 63
	l3ProtocolWithoutUnderscore = "l3"
	// base36 is used to encode numbers using smaller footprint than decimal
	// GCP resources can use 0-9a-z which is base36: 10 nums and 26 letters
	// See: https://cloud.google.com/compute/docs/naming-resources#resource-name-format
	base36 = 36
)

// L4Namer implements naming scheme for L4 LoadBalancer resources.
// This uses the V2 Naming scheme
// Example:
// For Service - namespace/svc, clusterUID/clusterName - uid01234, prefix - k8s2, protocol TCP
// Assume that hash("uid01234;svc;namespace") = cysix1wq
// The resource names are -
// TCP Forwarding Rule : k8s2-tcp-uid01234-namespace-svc-cysix1wq
// UDP Forwarding Rule : k8s2-udp-uid01234-namespace-svc-cysix1wq
// All other resources : k8s2-uid01234-namespace-svc-cysix1wq
// The "namespace-svc" part of the string will be trimmed as needed.
type L4Namer struct {
	// Namer is needed to implement all methods required by BackendNamer interface.
	*Namer
	// v2Prefix is the string 'k8s2'
	v2Prefix string
	// v2ClusterUID is the kube-system UID.
	v2ClusterUID string
}

func NewL4Namer(kubeSystemUID string, namer *Namer) *L4Namer {
	clusterUID := common.ContentHash(kubeSystemUID, clusterUIDLength)
	return &L4Namer{v2Prefix: defaultPrefix + schemaVersionV2, v2ClusterUID: clusterUID, Namer: namer}
}

// L4Backend returns the gce L4 Backend name based on the service namespace and name
// Naming convention:
//
//	k8s2-{uid}-{ns}-{name}-{nsNameHash}
//
// Output name is at most 63 characters.
func (namer *L4Namer) L4Backend(namespace, name string) string {
	return strings.Join([]string{
		namer.v2Prefix,
		namer.v2ClusterUID,
		getTrimmedNamespacedName(namespace, name, maximumL4CombinedLength),
		namer.getServiceHash(namespace, name),
	}, "-")
}

// NonDefaultSubnetNEG returns the gce NEG name for L4 NEGs in non default
// subnet based on the service namespace, name, and subnet name.
// Naming convention:
//
//	k8s2-{uid}-{ns}-{name}-{subnetHash}-{nsNameHash}
//
// subnetHash length = 6, nsNameHash length = 8, and the remainings are trimmed evenly.
// Output name is at most 63 characters.
func (namer *L4Namer) NonDefaultSubnetNEG(namespace, name, subnetName string, _ int32) string {
	return strings.Join([]string{
		namer.v2Prefix,
		namer.v2ClusterUID,
		getTrimmedNamespacedName(namespace, name, maximumL4CombinedLength-subnetHashLength-1),
		subnetHash(subnetName),
		namer.getServiceHash(namespace, name),
	}, "-")
}

// NonDefaultSubnetCustomNEG returns the gce neg name in the non-default subnet
// when the NEG name is a custom one.
// Custom Name NEG for L4 NEG is not allowed.
func (n *L4Namer) NonDefaultSubnetCustomNEG(customNEGName, subnetName string) (string, error) {
	return "", fmt.Errorf("Custom NEG is not allowed for L4")
}

// L4Firewall returns the gce Firewall name based on the service namespace and name
// Naming convention:
//
//	k8s2-{uid}-{ns}-{name}-{nsNameHash}
//
// Output name is at most 63 characters.
// This name is identical to L4Backend.
func (namer *L4Namer) L4Firewall(namespace, name string) string {
	return namer.L4Backend(namespace, name)
}

// L4FirewallV21 returns the gce Firewall name based on the service namespace and name
// Naming convention:
//
//	k8s2-{uid}-{ns}-{name}-{nsNameHash}-{suffix}
//
// Where suffix is the hash based on the namespace and name.
// Output name is at most 63 characters.
func (namer *L4Namer) L4FirewallV21(namespace, name, suffix string) string {
	const maxGCEChars = 63

	prefix := namer.v2Prefix
	nameHash := namer.getServiceHash(namespace, name)

	lengthUsed := len(prefix) + 1 +
		len(namer.v2ClusterUID) + 1 +
		len(nameHash) + 1 +
		1 // for the dash between namespace and name
	if suffix != "" {
		lengthUsed += len(suffix) + 1
	}
	lengthForHumanReadableName := maxGCEChars - lengthUsed

	parts := []string{
		prefix,
		namer.v2ClusterUID,
		getTrimmedNamespacedName(namespace, name, lengthForHumanReadableName),
		nameHash,
	}
	if suffix != "" {
		parts = append(parts, suffix)
	}

	return strings.Join(parts, "-")
}

// L4FirewallDeny returns the gce Firewall name for the Deny rule
// Naming convention:
//
//	k8s2-{uid}-{ns}-{name}-{nsNameHash}-deny
//
// Where suffix is the hash based on the namespace and name.
// Output name is at most 63 characters, whole "-deny" will be always at the end.
func (namer *L4Namer) L4FirewallDeny(namespace, name string) string {
	return namer.L4FirewallV21(namespace, name, "deny")
}

// L4IPv6Firewall returns the gce IPv6 Firewall name based on the service namespace and name
// Naming convention:
//
//	k8s2-{uid}-{ns}-{name}-{nsNameHash}-ipv6
//
// Output name is at most 63 characters.
func (namer *L4Namer) L4IPv6Firewall(namespace, name string) string {
	return GetSuffixedName(namer.L4Backend(namespace, name), "-"+ipv6Suffix)
}

// L4IPv6FirewallDeny returns the gce IPv6 Firewall name for the Deny Rule
// Naming convention:
//
//	k8s2-{uid}-{ns}-{name}-{nsNameHash}-deny-ipv6
//
// Output name is at most 63 characters, the name will always have "-deny-ipv6" suffix.
func (namer *L4Namer) L4IPv6FirewallDeny(namespace, name string) string {
	return namer.L4FirewallV21(namespace, name, "deny-ipv6")
}

// L4ForwardingRule returns the name of the L4 forwarding rule name based on the service namespace, name and protocol.
// Naming convention:
//
//	k8s2-{protocol}-{uid}-{ns}-{name}-{nsNameHash}
//
// Output name is at most 63 characters.
func (namer *L4Namer) L4ForwardingRule(namespace, name, protocol string) string {
	protocol = strings.ToLower(protocol)
	if protocol == "l3_default" {
		protocol = l3ProtocolWithoutUnderscore
	}

	// add 1 for hyphen
	protoLen := len(protocol) + 1
	return strings.Join([]string{
		namer.v2Prefix,
		protocol,
		namer.v2ClusterUID,
		getTrimmedNamespacedName(namespace, name, maximumL4CombinedLength-protoLen),
		namer.getServiceHash(namespace, name),
	}, "-")
}

// L4NetLBForwardingRule returns the name of the L4NetLB forwarding rule based on the service's name, namespace and protocol.
// Since there can be multiple Forwarding Rules for a single service we use additional num.
// Naming convention:
//
//	k8s2-{protocol}-{uid}-{ns}-{name}-{nsNameHash}-{num}
//
// Output name is at most 63 characters.
func (namer *L4Namer) L4NetLBForwardingRule(namespace, name, protocol string, num uint) string {
	encodedNum := strconv.FormatUint(uint64(num), base36)

	return GetSuffixedName(
		namer.L4ForwardingRule(namespace, name, protocol),
		"-"+encodedNum,
	)
}

// L4HealthCheck returns the name of the L4 LB Healthcheck
func (namer *L4Namer) L4HealthCheck(namespace, name string, shared bool) string {
	if shared {
		return strings.Join([]string{namer.v2Prefix, namer.v2ClusterUID, sharedHcSuffix}, "-")
	}
	l4Name := namer.L4Backend(namespace, name)
	return l4Name
}

// L4HealthCheckFirewall returns the name of the L4 LB Healthcheck Firewall
func (namer *L4Namer) L4HealthCheckFirewall(namespace, name string, shared bool) string {
	if shared {
		return strings.Join([]string{namer.v2Prefix, namer.v2ClusterUID, sharedFirewallHcSuffix}, "-")
	}
	l4Name := namer.L4Backend(namespace, name)
	return namer.hcFirewallName(l4Name, "")
}

// L4IPv6ForwardingRule returns the name of the L4 forwarding rule name based on the service namespace, name and protocol.
// Naming convention:
//
//	k8s2-{protocol}-{uid}-{ns}-{name}-{nsNameHash}-ipv6
//
// Output name is at most 63 characters.
func (namer *L4Namer) L4IPv6ForwardingRule(namespace, name, protocol string) string {
	return GetSuffixedName(namer.L4ForwardingRule(namespace, name, protocol), "-"+ipv6Suffix)
}

// L4NetLBIPv6ForwardingRule returns the name of the IPv6 L4NetLB forwarding rule based on the service's name, namespace and protocol.
// Since there can be multiple Forwarding Rules for a single service we use additional num.
// Naming convention:
//
//	k8s2-{protocol}-{uid}-{ns}-{name}-{nsNameHash}-{num}-ipv6
//
// Output name is at most 63 characters.
func (namer *L4Namer) L4NetLBIPv6ForwardingRule(namespace, name, protocol string, num uint) string {
	encodedNum := strconv.FormatUint(uint64(num), base36)
	// We cannot use L4IPv6ForwardingRule or L4IPv6ForwardingRule,
	// as they may end up causing name collisions for longer service names.
	return GetSuffixedName(
		namer.L4ForwardingRule(namespace, name, protocol),
		"-"+encodedNum+"-"+ipv6Suffix,
	)
}

// L4IPv6HealthCheckFirewall returns the name of the IPv6 L4 LB health check firewall rule.
func (namer *L4Namer) L4IPv6HealthCheckFirewall(namespace, name string, shared bool) string {
	if shared {
		return strings.Join([]string{namer.v2Prefix, namer.v2ClusterUID, sharedFirewallHcSuffix, ipv6Suffix}, "-")
	}
	l4Name := namer.L4Backend(namespace, name)
	return namer.hcFirewallName(l4Name, "-"+ipv6Suffix)
}

// IsNEG indicates if the given name is a NEG following the L4 naming convention.
func (namer *L4Namer) IsNEG(name string) bool {
	return strings.HasPrefix(name, namer.v2Prefix+"-"+namer.v2ClusterUID)
}

// getServiceHash returns hash string of length 8 of a concatenated string generated from
// kube-system uid, namespace and name. These fields in combination define an l4 load-balancer uniquely.
func (n *L4Namer) getServiceHash(namespace, name string) string {
	lbString := strings.Join([]string{n.v2ClusterUID, namespace, name}, ";")
	return common.ContentHash(lbString, 8)
}

// hcFirewallName generates the firewall name for the given healthcheck.
// It ensures that the name is at most 63 chars long.
func (n *L4Namer) hcFirewallName(hcName, suffix string) string {
	return GetSuffixedName(hcName, firewallHcSuffix+suffix)
}

func getTrimmedNamespacedName(namespace, name string, maxLength int) string {
	return strings.Join(TrimFieldsEvenly(maxLength, namespace, name), "-")
}

func GetSuffixedName(name string, suffix string) string {
	return ensureSpaceForSuffix(name, suffix) + suffix
}

func ensureSpaceForSuffix(name string, suffix string) string {
	maxRealNameLen := maxResourceNameLength - len(suffix)
	if len(name) > maxRealNameLen {
		name = name[:maxRealNameLen]
	}
	return name
}
