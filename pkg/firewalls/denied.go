package firewalls

import "google.golang.org/api/compute/v1"

func DeniedAll() []*compute.FirewallDenied {
	return []*compute.FirewallDenied{
		{
			IPProtocol: "all",
		},
	}
}
