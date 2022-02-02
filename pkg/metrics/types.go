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
	"fmt"

	v1 "k8s.io/api/networking/v1"
	frontendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

const (
	// L4 LB metrics key
	// ManagedStaticIP specifies if Static IP for L4 NetLB service is managed.
	ManagedStaticIPKey = "ManagedStaticIP"
	// PremiumNetworkTier defines if NetworkTier for L4 NetLB service is Premium
	PremiumNetworkTierKey = "PremiumNetworkTier"
	// EnabledGlobalAccess specifies if Global Access for L4 ILB is enabled.
	EnabledGlobalAccessKey = "EnabledGlobalAccess"
	// EnabledCustomSubNet specifies if Custom Subnet is enabled for L4 ILB.
	EnabledCustomSubnetKey = "EnabledCustomSubnet"
	// InSuccess specifies if the ILB service VIP is configured for L4 LB.
	InSuccessKey = "InSuccess"
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

// L4ServiceState stores metrics for L4 LB Service
type L4ServiceState struct {
	Metrics map[string]bool
}

// SetMetic set metric with a given key to true
func (m L4ServiceState) SetMetric(key string) {
	if m.Metrics == nil {
		klog.Errorf("No metric map set for service")
		return
	}
	if _, ok := m.Metrics[key]; !ok {
		klog.Errorf("Service does not support this metric key %s", key)
	}
	m.Metrics[key] = true
}

// validateILBMetrics checks if Service Status map contains all keys related to L4 ILB
func (m L4ServiceState) validateILBMetrics() error {
	if _, ok := m.Metrics[InSuccessKey]; !ok {
		return fmt.Errorf("ILB Service State should have metric key %s in map", InSuccessKey)
	}
	if _, ok := m.Metrics[EnabledGlobalAccessKey]; !ok {
		return fmt.Errorf("ILB Service State should have metric key %s in map", EnabledGlobalAccessKey)
	}
	if _, ok := m.Metrics[EnabledCustomSubnetKey]; !ok {
		return fmt.Errorf("ILB Service State should have metric key %s in map", EnabledCustomSubnetKey)
	}
	return nil
}

// validateNetLBMetrics checks if Service Status map contains all keys related to L4 NetLB
func (m L4ServiceState) validateNetLBMetrics() error {
	if _, ok := m.Metrics[InSuccessKey]; !ok {
		return fmt.Errorf("L4 Net Service State should have metric key %s in map", InSuccessKey)
	}
	if _, ok := m.Metrics[PremiumNetworkTierKey]; !ok {
		return fmt.Errorf("L4 Net Service State should have metric key %s in map", PremiumNetworkTierKey)
	}
	if _, ok := m.Metrics[ManagedStaticIPKey]; !ok {
		return fmt.Errorf("L4 Net Service State should have metric key %s in map", ManagedStaticIPKey)
	}
	return nil
}

// NewL4NetLBMetricsStates creates new Metrics map for L4 ILB
func NewL4ILBMetricsStates() L4ServiceState {
	return L4ServiceState{
		Metrics: map[string]bool{
			EnabledGlobalAccessKey: false,
			EnabledCustomSubnetKey: false,
			InSuccessKey:           false,
		},
	}
}

// NewL4NetLBMetricsStates creates new Metrics map for L4 NetLB
func NewL4NetLBMetricsStates() L4ServiceState {
	return L4ServiceState{
		Metrics: map[string]bool{
			PremiumNetworkTierKey: false,
			ManagedStaticIPKey:    false,
			InSuccessKey:          false,
		},
	}
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

// L4LBMetricsCollector is an interface to update/delete L4 Lb service states
// in the cache that is used for computing L4 LB usage metrics.
type L4LBMetricsCollector interface {
	// SetL4LBService adds/updates L4 LB service state for given service key.
	SetL4LBService(svcKey string, state L4ServiceState)
	// DeleteL4LBService removes the given L4 LB service key.
	DeleteL4LBService(svcKey string)
}
