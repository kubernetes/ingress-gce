package metrics

type feature string

func (f feature) String() string {
	return string(f)
}

const (
	l4ILBService      = feature("L4ILBService")
	l4ILBGlobalAccess = feature("L4ILBGlobalAccess")
	l4ILBCustomSubnet = feature("L4ILBCustomSubnet")
	// l4ILBInSuccess feature specifies that ILB VIP is configured.
	l4ILBInSuccess = feature("L4ILBInSuccess")
	// l4ILBInInError feature specifies that an error had occurred while creating/
	// updating GCE Load Balancer.
	l4ILBInError = feature("L4ILBInError")
	// l4ILBInUserError feature specifies that an error cause by User misconfiguration had occurred while creating/updating GCE Load Balancer.
	l4ILBInUserError = feature("L4ILBInUserError")
)

const (
	l4NetLBService = feature("L4NetLBService")
	// l4NetLBPremiumNetworkTier feature specifies that NetLB VIP is configured in Premium Network Tier.
	l4NetLBPremiumNetworkTier = feature("L4NetLBPremiumNetworkTier")
	// l4NetLBManagedStaticIP feature specifies that static IP Address is managed by controller.
	l4NetLBManagedStaticIP = feature("L4NetLBManagedStaticIP")
	// l4NetLBStaticIP feature specifies number of all static IP Address managed by controller and by user.
	l4NetLBStaticIP = feature("L4NetLBStaticIP")
	// l4NetLBInSuccess feature specifies that NetLB VIP is configured.
	l4NetLBInSuccess = feature("L4NetLBInSuccess")
	// l4NetLBInInError feature specifies that an error had occurred while creating/updating GCE Load Balancer.
	l4NetLBInError = feature("L4NetLBInError")
	// l4NetLBInInUserError feature specifies that an error cause by User misconfiguration had occurred while creating/updating GCE Load Balancer.
	l4NetLBInUserError = feature("L4NetLBInUserError")
)
