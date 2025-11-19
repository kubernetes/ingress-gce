package address

import (
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4annotations"
	"k8s.io/klog/v2"
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
	ipv4FromAnnotation, ipNameFromAnnotation, err := l4annotations.FromService(svc).IPv4AddressAnnotation(cloud)
	if err != nil {
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
	ipv6AddressFromAnnotation, ipNameFromAnnotation, err := l4annotations.FromService(svc).IPv6AddressAnnotation(cloud)
	if err != nil {
		return "", "", err
	}
	if ipv6AddressFromAnnotation != "" {
		// Google Cloud stores ipv6 addresses in CIDR form,
		// but to create static address you need to specify address without range
		addr := ipv6AddressWithoutRange(ipv6AddressFromAnnotation)
		logger.V(2).Info("ipv6AddressToUse: using IPv6 Address from annotation", "address", addr)
		return addr, ipNameFromAnnotation, nil
	}
	if ipv6FwdRule == nil {
		logger.V(2).Info("ipv6AddressToUse: use any IPv6 Address")
		return "", "", nil
	}
	if requestedSubnet != ipv6FwdRule.Subnetwork {
		logger.V(2).Info("ipv6AddressToUse: reset IPv6 Address due to changed subnet")
		return "", "", nil
	}

	// Google Cloud creates ipv6 forwarding rules with IPAddress in CIDR form,
	// but to create static address you need to specify address without range
	addr := ipv6AddressWithoutRange(ipv6FwdRule.IPAddress)
	logger.V(2).Info("ipv6AddressToUse: using IPv6 Address from existing forwarding rule", "forwardingRuleName", ipv6FwdRule.Name, "address", addr)
	return addr, "", nil
}

func ipv6AddressWithoutRange(ipv6Address string) string {
	return strings.Split(ipv6Address, "/")[0]
}
