/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metrics

import (
	"time"

	v1 "k8s.io/api/networking/v1"
	frontendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
)

// IngressState defines an ingress and its associated service ports.
type IngressState struct {
	ingress        *v1.Ingress
	frontendconfig *frontendconfigv1beta1.FrontendConfig
	servicePorts   []utils.ServicePort
}

// L4ILBServiceLegacyState defines if global access and subnet features are enabled
// for an L4 ILB service.
type L4ILBServiceLegacyState struct {
	// EnabledGlobalAccess specifies if Global Access is enabled.
	EnabledGlobalAccess bool
	// EnabledCustomSubNet specifies if Custom Subnet is enabled.
	EnabledCustomSubnet bool
	// InSuccess specifies if the ILB service VIP is configured.
	InSuccess bool
	// IsUserError specifies if the error was caused by User misconfiguration.
	IsUserError bool
}

type L4ServiceStatus string

const StatusSuccess = L4ServiceStatus("Success")
const StatusUserError = L4ServiceStatus("UserError")
const StatusError = L4ServiceStatus("Error")
const StatusPersistentError = L4ServiceStatus("PersistentError")

// L4DualStackServiceLabels defines ipFamilies, ipFamilyPolicy
// of L4 DualStack service
type L4DualStackServiceLabels struct {
	// IPFamilies stores spec.ipFamilies of Service
	IPFamilies string
	// IPFamilyPolicy specifies spec.IPFamilyPolicy of Service
	IPFamilyPolicy string
}

// L4FeaturesServiceLabels defines various properties we want to track for L4 LBs
type L4FeaturesServiceLabels struct {
	// Multinetwork specifies if the service is a multinetworked service
	Multinetwork bool
	// StrongSessionAffinity is true if String Session Affinity is enabled
	StrongSessionAffinity bool
}

// L4ServiceState tracks the state of an L4 service. It includes data needed to fill various L4 metrics plus the status of the service.
// FirstSyncErrorTime of an L4 service
type L4ServiceState struct {
	L4DualStackServiceLabels
	L4FeaturesServiceLabels
	// Status specifies status of an L4 Service
	Status L4ServiceStatus
	// FirstSyncErrorTime specifies the time timestamp when the service sync ended up with error for the first time.
	FirstSyncErrorTime *time.Time
}

// L4NetLBServiceLegacyState defines if network tier is premium and
// if static ip address is managed by controller
// for an L4 NetLB service.
type L4NetLBServiceLegacyState struct {
	// IsManagedIP specifies if Static IP is managed by controller.
	IsManagedIP bool
	// IsPremiumTier specifies if network tier for forwarding rule is premium.
	IsPremiumTier bool
	// InSuccess specifies if the NetLB service VIP is configured.
	InSuccess bool
	// IsUserError specifies if the error was caused by User misconfiguration.
	IsUserError bool
	// FirstSyncErrorTime specifies the time timestamp when the service sync ended up with error for the first time.
	FirstSyncErrorTime *time.Time
}

// InitL4NetLBServiceLegacyState created and inits the L4NetLBServiceLegacyState struct by setting FirstSyncErrorTime.
func InitL4NetLBServiceLegacyState(syncTime *time.Time) L4NetLBServiceLegacyState {
	return L4NetLBServiceLegacyState{FirstSyncErrorTime: syncTime}
}

// IngressMetricsCollector is an interface to update/delete ingress states in the cache
// that is used for computing ingress usage metrics.
type IngressMetricsCollector interface {
	// SetIngress adds/updates ingress state for given ingress key.
	SetIngress(ingKey string, ing IngressState)
	// DeleteIngress removes the given ingress key.
	DeleteIngress(ingKey string)
}

// L4ILBMetricsCollector is an interface to update/delete L4 ILb service states
// in the cache that is used for computing L4 ILB usage metrics.
type L4ILBMetricsCollector interface {
	// SetL4ILBServiceForLegacyMetric adds/updates L4 ILB service state for given service key.
	SetL4ILBServiceForLegacyMetric(svcKey string, state L4ILBServiceLegacyState)
	// DeleteL4ILBServiceForLegacyMetric removes the given L4 ILB service key.
	DeleteL4ILBServiceForLegacyMetric(svcKey string)
}
