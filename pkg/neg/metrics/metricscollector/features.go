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

type feature string

func (f feature) String() string {
	return string(f)
}

const (
	neg            = feature("NEG")
	standaloneNeg  = feature("StandaloneNEG")
	ingressNeg     = feature("IngressNEG")
	asmNeg         = feature("AsmNEG")
	vmIpNeg        = feature("VmIpNEG")
	vmIpNegLocal   = feature("VmIpNegLocal")
	vmIpNegCluster = feature("VmIpNegCluster")
	customNamedNeg = feature("CustomNamedNEG")
	// negInSuccess feature specifies that syncers were created for the Neg
	negInSuccess = feature("NegInSuccess")
	// negInError feature specifies that an error occurring in ensuring Neg Syncer
	negInError = feature("NegInError")
)
