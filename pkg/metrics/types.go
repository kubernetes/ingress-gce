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

// NegServiceState contains all the neg usage associated with one service
type NegServiceState struct {
	// standaloneNeg is the count of standalone NEG
	StandaloneNeg int
	// ingressNeg is the count of NEGs created for ingress
	IngressNeg int
	// asmNeg is the count of NEGs created for ASM
	AsmNeg int
	// VmIpNeg specifies if a service uses GCE_VM_IP NEG.
	VmIpNeg *VmIpNegType
	// CustomNamedNeg is the count of standalone negs with custom names
	CustomNamedNeg int
	// SuccessfulNeg is the count of successful NEG syncer creations
	SuccessfulNeg int
	// SuccessfulNeg is the count of errors in NEG syncer creations
	ErrorNeg int
}

// VmIpNegType contains whether a GCE_VM_IP NEG is requesting for
// local traffic (or service external policy set to local).
type VmIpNegType struct {
	trafficPolicyLocal bool
}

// NewVmIpNegType returns a new VmIpNegType.
func NewVmIpNegType(trafficPolicyLocal bool) *VmIpNegType {
	return &VmIpNegType{trafficPolicyLocal: trafficPolicyLocal}
}

// L4ILBServiceState defines if global access and subnet features are enabled
// for an L4 ILB service.
type L4ILBServiceState struct {
	// EnabledGlobalAccess specifies if Global Access is enabled.
	EnabledGlobalAccess bool
	// EnabledCustomSubNet specifies if Custom Subnet is enabled.
	EnabledCustomSubnet bool
	// InSuccess specifies if the ILB service VIP is configured.
	InSuccess bool
}

// IngressMetricsCollector is an interface to update/delete ingress states in the cache
// that is used for computing ingress usage metrics.
type IngressMetricsCollector interface {
	// SetIngress adds/updates ingress state for given ingress key.
	SetIngress(ingKey string, ing IngressState)
	// DeleteIngress removes the given ingress key.
	DeleteIngress(ingKey string)
}

// NegMetricsCollector is an interface to update/delete Neg states in the cache
// that is used for computing neg usage metrics.
type NegMetricsCollector interface {
	// SetNegService adds/updates neg state for given service key.
	SetNegService(svcKey string, negState NegServiceState)
	// DeleteNegService removes the given service key.
	DeleteNegService(svcKey string)
}

// L4ILBMetricsCollector is an interface to update/delete L4 ILb service states
// in the cache that is used for computing L4 ILB usage metrics.
type L4ILBMetricsCollector interface {
	// SetL4ILBService adds/updates L4 ILB service state for given service key.
	SetL4ILBService(svcKey string, state L4ILBServiceState)
	// DeleteL4ILBService removes the given L4 ILB service key.
	DeleteL4ILBService(svcKey string)
}
