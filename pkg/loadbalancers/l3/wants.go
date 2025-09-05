package l3

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/flags"
)

// ExperimentAnnotation is the key for enabling experimental L3 support for NetLB services.
// Note that the controller must have the flag for the experiment also enabled.
const ExperimentAnnotation = "networking.gke.io/l3-experiment"

// Wants determines if the service should use experimental L3 GCE resources.
// Controller must be run with experimental feature flag enabled for the annotation to take effect.
func Wants(svc *corev1.Service) bool {
	if !flags.F.EnableL3NetLBOptIn {
		return false
	}

	acceptableValues := map[string]struct{}{
		"true": {}, "enabled": {}, "enable": {},
		"on": {}, "yes": {}, "True": {},
	}

	val, ok := svc.Annotations[ExperimentAnnotation]
	if !ok {
		return false
	}
	_, ok = acceptableValues[val]
	return ok
}
