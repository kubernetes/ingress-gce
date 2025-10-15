package firewalls

import (
	"google.golang.org/api/compute/v1"
	"k8s.io/utils/ptr"
)

var (
	DenyTrafficPriority = ptr.To(1000)
)

func DeniedAll() []*compute.FirewallDenied {
	return []*compute.FirewallDenied{
		{
			IPProtocol: "all",
		},
	}
}
