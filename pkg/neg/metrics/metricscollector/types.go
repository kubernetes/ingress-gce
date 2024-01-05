/*
Copyright 2024 The Kubernetes Authors.

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

package metricscollector

import (
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
)

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

type negLocTypeKey struct {
	location     string
	endpointType string
}

type syncerState struct {
	lastSyncResult negtypes.Reason
	inErrorState   bool
}

type syncerStateCount map[syncerState]int

// LabelPropagationStat contains stats related to label propagation.
type LabelPropagationStats struct {
	EndpointsWithAnnotation int
	NumberOfEndpoints       int
}

// LabelPropagationMetrics contains aggregated label propagation related metrics.
type LabelPropagationMetrics struct {
	EndpointsWithAnnotation int
	NumberOfEndpoints       int
}

// NewVmIpNegType returns a new VmIpNegType.
func NewVmIpNegType(trafficPolicyLocal bool) *VmIpNegType {
	return &VmIpNegType{trafficPolicyLocal: trafficPolicyLocal}
}
