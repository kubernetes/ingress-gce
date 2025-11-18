package mixedprotocolilbtest

import (
	api_v1 "k8s.io/api/core/v1"
)

// IPv4Ingress is an Ingress for already existing IPv4 load balancers
func IPv4Ingress() []api_v1.LoadBalancerIngress {
	mode := api_v1.LoadBalancerIPModeVIP
	return []api_v1.LoadBalancerIngress{
		{
			IP:     IPv4Address,
			IPMode: &mode,
		},
	}
}

// IPv6Ingress is an Ingress for already existing IPv6 load balancers
func IPv6Ingress() []api_v1.LoadBalancerIngress {
	mode := api_v1.LoadBalancerIPModeVIP
	return []api_v1.LoadBalancerIngress{
		{
			IP:     IPv6Address,
			IPMode: &mode,
		},
	}
}

// DualStackIngress is an Ingress for already existing Dual Stack load balancers
func DualStackIngress() []api_v1.LoadBalancerIngress {
	mode := api_v1.LoadBalancerIPModeVIP
	return []api_v1.LoadBalancerIngress{
		{
			IP:     IPv4Address,
			IPMode: &mode,
		},
		{
			IP:     IPv6Address,
			IPMode: &mode,
		},
	}
}
