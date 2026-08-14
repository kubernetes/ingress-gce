/*
Copyright 2023 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
you may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metricscollector

const (
	negControllerSubsystem = "neg_controller"

	// Label Values for Syncer Sync Result Metrics
	EPCountsDiffer           = "EndpointCountsDiffer"
	EPNodeMissing            = "EndpointNodeMissing"
	EPNodeNotFound           = "EndpointNodeNotFound"
	EPPodMissing             = "EndpointPodMissing"
	EPPodNotFound            = "EndpointPodNotFound"
	EPPodTypeAssertionFailed = "EndpointPodTypeAssertionFailed"
	EPZoneMissing            = "EndpointZoneMissing"
	EPSEndpointCountZero     = "EndpointSliceEndpointCountZero"
	EPCalculationCountZero   = "EndpointCalculationCountZero"
	InvalidAPIResponse       = "InvalidAPIResponse"
	InvalidEPAttach          = "InvalidEndpointAttach"
	InvalidEPDetach          = "InvalidEndpointDetach"
	NegNotFound              = "NetworkEndpointGroupNotFound"
	CurrentNegEPNotFound     = "CurrentNEGEndpointNotFound"
	EPSNotFound              = "EndpointSliceNotFound"
	OtherError               = "OtherError"
	Success                  = "Success"

	// Label values for Label Propagation Metrics
	epWithAnnotation = "with_annotation"
	totalEndpoints   = "total"

	// Classification of endpoints within a NEG.
	ipv4EndpointType      = "IPv4"
	ipv6EndpointType      = "IPv6"
	dualStackEndpointType = "DualStack"
	migrationEndpointType = "Migration"
)
