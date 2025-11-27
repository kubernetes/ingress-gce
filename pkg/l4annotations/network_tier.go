package l4annotations

import (
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	api_v1 "k8s.io/api/core/v1"
)

// NetworkTier returns Network Tier from service and stays if this is a service annotation.
// If the annotation is not present then default Network Tier is returned.
func NetworkTier(service *api_v1.Service) (cloud.NetworkTier, bool) {
	l, ok := service.Annotations[NetworkTierAnnotationKey]
	if !ok {
		return cloud.NetworkTierDefault, false
	}

	v := cloud.NetworkTier(l)
	switch v {
	case cloud.NetworkTierStandard:
		return v, true
	case cloud.NetworkTierPremium:
		return v, true
	default:
		return cloud.NetworkTierDefault, false
	}
}
