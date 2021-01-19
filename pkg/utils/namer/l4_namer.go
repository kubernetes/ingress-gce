package namer

import (
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
	maximumL4CombinedLength = 39
	sharedHcSuffix          = "l4-shared-hc"
	firewallHcSuffix        = "-fw"
	sharedFirewallHcSuffix  = sharedHcSuffix + firewallHcSuffix
	maxResourceNameLength   = 63
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
	// v2Prefix is the string 'k8sv2'
	v2Prefix string
	// v2ClusterUID is the kube-system UID.
	v2ClusterUID string
}

func NewL4Namer(kubeSystemUID string, namer *Namer) *L4Namer {
	clusterUID := common.ContentHash(kubeSystemUID, clusterUIDLength)
	return &L4Namer{v2Prefix: defaultPrefix + schemaVersionV2, v2ClusterUID: clusterUID, Namer: namer}
}

// VMIPNEG returns the gce VM_IP_NEG name based on the service namespace and name
// NEG naming convention:
//   k8s2-{uid}-{ns}-{name}-{suffix}
// Output name is at most 63 characters.
func (namer *L4Namer) VMIPNEG(namespace, name string) (string, bool) {
	truncFields := TrimFieldsEvenly(maximumL4CombinedLength, namespace, name)
	truncNamespace := truncFields[0]
	truncName := truncFields[1]
	return strings.Join([]string{namer.v2Prefix, namer.v2ClusterUID, truncNamespace, truncName, namer.suffix(namespace, name)}, "-"), true

}

// L4ForwardingRule returns the name of the L4 forwarding rule name based on the service namespace, name and protocol.
// Naming convention:
//   k8s2-{protocol}-{uid}-{ns}-{name}-{suffix}
// Output name is at most 63 characters.
func (namer *L4Namer) L4ForwardingRule(namespace, name, protocol string) string {
	// add 1 for hyphen
	protoLen := len(protocol) + 1
	truncFields := TrimFieldsEvenly(maximumL4CombinedLength-protoLen, namespace, name)
	truncNamespace := truncFields[0]
	truncName := truncFields[1]
	return strings.Join([]string{namer.v2Prefix, protocol, namer.v2ClusterUID, truncNamespace, truncName, namer.suffix(namespace, name)}, "-")
}

// L4HealthCheck returns the name of the L4 ILB Healthcheck and the associated firewall rule.
func (namer *L4Namer) L4HealthCheck(namespace, name string, shared bool) (string, string) {
	if !shared {
		l4Name, _ := namer.VMIPNEG(namespace, name)
		return l4Name, namer.hcFirewallName(l4Name)
	}
	return strings.Join([]string{namer.v2Prefix, namer.v2ClusterUID, sharedHcSuffix}, "-"),
		strings.Join([]string{namer.v2Prefix, namer.v2ClusterUID, sharedFirewallHcSuffix}, "-")
}

// IsNEG indicates if the given name is a NEG following the L4 naming convention.
func (namer *L4Namer) IsNEG(name string) bool {
	return strings.HasPrefix(name, namer.v2Prefix+"-"+namer.v2ClusterUID)
}

// suffix returns hash string of length 8 of a concatenated string generated from
// kube-system uid, namespace and name. These fields in combination define an l4 load-balancer uniquely.
func (n *L4Namer) suffix(namespace, name string) string {
	lbString := strings.Join([]string{n.v2ClusterUID, namespace, name}, ";")
	return common.ContentHash(lbString, 8)
}

// hcFirewallName generates the firewall name for the given healthcheck.
// It ensures that the name is atmost 63 chars long.
func (n *L4Namer) hcFirewallName(hcName string) string {
	maxHcNameLen := maxResourceNameLength - len(firewallHcSuffix)
	if len(hcName) > maxHcNameLen {
		hcName = hcName[:maxHcNameLen]
	}
	return hcName + firewallHcSuffix
}
