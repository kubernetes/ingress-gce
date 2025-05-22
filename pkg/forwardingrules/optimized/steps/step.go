package steps

import (
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/composite"
)

// ResourceName is a type alias for string, representing the name of a GCE resource.
type ResourceName string

// Step represents single transformation of Forwarding Rules.
// Multiple steps are chained to achieve desired transformation.
type Step func(ports []api_v1.ServicePort, frs map[ResourceName]*composite.ForwardingRule) error
