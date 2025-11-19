package l4resources

import (
	"errors"

	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/utils"
)

// IsUserError checks if given error is caused by User.
func IsUserError(err error) bool {
	var userErr *utils.UserError
	var firewallErr *firewalls.FirewallXPNError

	return utils.IsNetworkTierError(err) ||
		utils.IsIPConfigurationError(err) ||
		utils.IsInvalidSubnetConfigurationError(err) ||
		utils.IsInvalidLoadBalancerSourceRangesSpecError(err) ||
		utils.IsInvalidLoadBalancerSourceRangesAnnotationError(err) ||
		utils.IsUnsupportedNetworkTierError(err) ||
		errors.As(err, &firewallErr) ||
		errors.As(err, &userErr)
}
