package address

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
)

// IPv4ToUse determines which IPv4 address needs to be used in the ForwardingRule,
// address evaluated in the following order:
//
//  1. Use static addresses annotation "networking.gke.io/load-balancer-ip-addresses".
//  2. Use .Spec.LoadBalancerIP (old field, was deprecated).
//  3. Use existing forwarding rule IP. If subnetwork was changed (or no existing IP),
//     reset the IP (by returning empty string).
func IPv4ToUse(cloud *gce.Cloud, recorder record.EventRecorder, svc *v1.Service, fwdRule *composite.ForwardingRule, requestedSubnet string) (string, error) {
	// Get value from new annotation which support both IPv4 and IPv6
	ipv4FromAnnotation, err := annotations.FromService(svc).IPv4AddressAnnotation(cloud)
	if err != nil {
		return "", err
	}
	if ipv4FromAnnotation != "" {
		if svc.Spec.LoadBalancerIP != "" {
			recorder.Event(svc, v1.EventTypeNormal, "MixedStaticIP", "Found both .Spec.LoadBalancerIP and \"networking.gke.io/load-balancer-ip-addresses\" annotation. Consider using annotation only.")
		}
		return ipv4FromAnnotation, nil
		// if no value from annotation (for example, annotation has only IPv6 addresses) -- continue
	}
	if svc.Spec.LoadBalancerIP != "" {
		return svc.Spec.LoadBalancerIP, nil
	}
	if fwdRule == nil {
		return "", nil
	}
	if requestedSubnet != fwdRule.Subnetwork {
		// reset ip address since subnet is being changed.
		return "", nil
	}
	return fwdRule.IPAddress, nil
}
