package address

import (
	"errors"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
	"k8s.io/klog/v2"
	"k8s.io/utils/net"

	"k8s.io/cloud-provider-gcp/providers/gce"
)

// IPv4ToUse determines which IPv4 address needs to be used in the ForwardingRule,
// address evaluated in the following order:
//
//  1. Use static addresses annotation "networking.gke.io/load-balancer-ip-addresses".
//  2. Use .Spec.LoadBalancerIP (old field, was deprecated).
//  3. Use existing forwarding rule IP. If subnetwork was changed (or no existing IP),
//     reset the IP (by returning empty string).
func IPv4ToUse(cloud *gce.Cloud, recorder record.EventRecorder, svc *v1.Service, fwdRule *composite.ForwardingRule, requestedSubnet string) (ipAddress, ipName string, err error) {
	// Get value from new annotation which support both IPv4 and IPv6
	ipv4FromAnnotation, ipNameFromAnnotation, err := annotations.FromService(svc).IPv4AddressAnnotation(cloud)
	if err != nil {
		if errors.Is(err, annotations.ErrInvalidStaticIPName) {
			return "", "", l4utils.NewUserError(err)
		}
		return "", "", err
	}
	if ipv4FromAnnotation != "" {
		if svc.Spec.LoadBalancerIP != "" {
			recorder.Event(svc, v1.EventTypeNormal, "MixedStaticIP", "Found both .Spec.LoadBalancerIP and \"networking.gke.io/load-balancer-ip-addresses\" annotation. Consider using annotation only.")
		}
		return ipv4FromAnnotation, ipNameFromAnnotation, nil
		// if no value from annotation (for example, annotation has only IPv6 addresses) -- continue
	}
	if svc.Spec.LoadBalancerIP != "" {
		if net.IsIPv6String(svc.Spec.LoadBalancerIP) {
			return "", "", ipv6LoadBalancerIPError(svc.Spec.LoadBalancerIP)
		}
		return svc.Spec.LoadBalancerIP, "", nil
	}
	if fwdRule == nil {
		return "", "", nil
	}
	if requestedSubnet != fwdRule.Subnetwork {
		// reset ip address since subnet is being changed.
		return "", "", nil
	}
	return fwdRule.IPAddress, "", nil
}

// IPv6ToUse determines which IPv6 address needs to be used in the ForwardingRule,
// address evaluated in the following order:
//
//  1. Use static addresses annotation "networking.gke.io/load-balancer-ip-addresses".
//  2. Use existing forwarding rule IP. If subnetwork was changed (or no existing IP),
//     reset the IP (by returning empty string).
func IPv6ToUse(cloud *gce.Cloud, svc *v1.Service, ipv6FwdRule *composite.ForwardingRule, requestedSubnet string, logger klog.Logger) (ipAddress, ipName string, err error) {
	// Get value from new annotation which support both IPv4 and IPv6
	ipv6AddressFromAnnotation, ipNameFromAnnotation, err := annotations.FromService(svc).IPv6AddressAnnotation(cloud)
	if err != nil {
		if errors.Is(err, annotations.ErrInvalidStaticIPName) {
			return "", "", l4utils.NewUserError(err)
		}
		return "", "", err
	}
	if ipv6AddressFromAnnotation != "" {
		// Google Cloud stores ipv6 addresses in CIDR form,
		// but to create static address you need to specify address without range
		addr := ipv6AddressWithoutRange(ipv6AddressFromAnnotation)
		logger.V(2).Info("ipv6AddressToUse: using IPv6 Address from annotation", "address", addr)
		return addr, ipNameFromAnnotation, nil
	}
	if lbIP := svc.Spec.LoadBalancerIP; lbIP != "" {
		if net.IsIPv6String(lbIP) {
			return "", "", ipv6LoadBalancerIPError(lbIP)
		}
		if !l4utils.NeedsIPv4(svc) {
			return "", "", loadBalancerIPOnIPv6OnlyServiceError(lbIP)
		}
		// IPv4 address on a dual-stack Service: it is used by the IPv4 forwarding rule only.
	}
	if ipv6FwdRule == nil {
		logger.V(2).Info("ipv6AddressToUse: use any IPv6 Address")
		return "", "", nil
	}
	// Forwarding rules with an IP collection (BYOIP) take the IP from the collection,
	// not from the subnet, so a subnet change is irrelevant for them. IP collection
	// changes are handled by the caller.
	if ipv6FwdRule.IpCollection == "" && requestedSubnet != ipv6FwdRule.Subnetwork {
		logger.V(2).Info("ipv6AddressToUse: reset IPv6 Address due to changed subnet")
		return "", "", nil
	}

	// Google Cloud creates ipv6 forwarding rules with IPAddress in CIDR form,
	// but to create static address you need to specify address without range
	addr := ipv6AddressWithoutRange(ipv6FwdRule.IPAddress)
	logger.V(2).Info("ipv6AddressToUse: using IPv6 Address from existing forwarding rule", "forwardingRuleName", ipv6FwdRule.Name, "address", addr)
	return addr, "", nil
}

// ipv6LoadBalancerIPError is returned when spec.loadBalancerIP contains an IPv6 address.
// spec.loadBalancerIP supports only IPv4 addresses.
func ipv6LoadBalancerIPError(lbIP string) error {
	return l4utils.NewIPConfigurationError(lbIP, fmt.Sprintf("spec.loadBalancerIP does not support IPv6 addresses; reserve a static IPv6 address and set its name in the %q annotation instead", annotations.StaticL4AddressesAnnotationKey))
}

// loadBalancerIPOnIPv6OnlyServiceError is returned when spec.loadBalancerIP is set on an
// IPv6-only Service (spec.ipFamilies: [IPv6], the default on IPv6-only clusters).
// spec.loadBalancerIP can only specify an IPv4 address, which an IPv6-only Service doesn't have.
func loadBalancerIPOnIPv6OnlyServiceError(lbIP string) error {
	return l4utils.NewIPConfigurationError(lbIP, fmt.Sprintf("spec.loadBalancerIP is not supported on IPv6-only Services (spec.ipFamilies: [IPv6]) because it can only specify an IPv4 address; remove spec.loadBalancerIP, or reserve a static IPv6 address and set its name in the %q annotation", annotations.StaticL4AddressesAnnotationKey))
}

func ipv6AddressWithoutRange(ipv6Address string) string {
	return strings.Split(ipv6Address, "/")[0]
}

// IsSameIPv6 reports whether ipv6, as returned by IPv6ToUse, is the address used by
// a forwarding rule. Google Cloud stores the address of IPv6 forwarding rules in CIDR
// form (with a /96 prefix), so the prefix is ignored on both sides of the comparison.
func IsSameIPv6(ipv6, forwardingRuleIP string) bool {
	if ipv6 == "" || forwardingRuleIP == "" {
		return false
	}
	return IsSameIP(ipv6AddressWithoutRange(ipv6), ipv6AddressWithoutRange(forwardingRuleIP))
}
