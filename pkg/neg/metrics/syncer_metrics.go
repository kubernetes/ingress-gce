/*
Copyright 2023 The Kubernetes Authors.

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
	"github.com/prometheus/client_golang/prometheus"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
)

const (
	syncResultLabel = "result"
	syncResultKey   = "sync_result"

	syncerStateLabel = "state"
	syncerStateKey   = "syncer_state"

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
)

var (
	// syncerSyncResult tracks the count for each sync result
	syncerSyncResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: negControllerSubsystem,
			Name:      syncResultKey,
			Help:      "Current count for each sync result",
		},
		[]string{syncResultLabel},
	)

	// syncerSyncerState tracks the count of syncer in different states
	syncerSyncerState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: negControllerSubsystem,
			Name:      syncerStateKey,
			Help:      "Current count of syncers in each state",
		},
		[]string{syncerStateLabel},
	)
)

type syncerStateCount struct {
	epCountsDiffer           int
	epNodeMissing            int
	epNodeNotFound           int
	epPodMissing             int
	epPodNotFound            int
	epPodTypeAssertionFailed int
	epZoneMissing            int
	epsEndpointCountZero     int
	epCalculationCountZero   int
	invalidAPIResponse       int
	invalidEPAttach          int
	invalidEPDetach          int
	negNotFound              int
	currentNegEPNotFound     int
	epsNotFound              int
	otherError               int
	success                  int
}

func (sc *syncerStateCount) inc(reason negtypes.Reason) {
	switch reason {
	case negtypes.ReasonEPCountsDiffer:
		sc.epCountsDiffer++
	case negtypes.ReasonEPNodeMissing:
		sc.epNodeMissing++
	case negtypes.ReasonEPNodeNotFound:
		sc.epNodeNotFound++
	case negtypes.ReasonEPPodMissing:
		sc.epPodMissing++
	case negtypes.ReasonEPPodNotFound:
		sc.epPodNotFound++
	case negtypes.ReasonEPPodTypeAssertionFailed:
		sc.epPodTypeAssertionFailed++
	case negtypes.ReasonEPZoneMissing:
		sc.epZoneMissing++
	case negtypes.ReasonEPSEndpointCountZero:
		sc.epsEndpointCountZero++
	case negtypes.ReasonInvalidAPIResponse:
		sc.invalidAPIResponse++
	case negtypes.ReasonInvalidEPAttach:
		sc.invalidEPAttach++
	case negtypes.ReasonInvalidEPDetach:
		sc.invalidEPDetach++
	case negtypes.ReasonNegNotFound:
		sc.negNotFound++
	case negtypes.ReasonCurrentNegEPNotFound:
		sc.currentNegEPNotFound++
	case negtypes.ReasonEPSNotFound:
		sc.epsNotFound++
	case negtypes.ReasonOtherError:
		sc.otherError++
	case negtypes.ReasonSuccess:
		sc.success++
	}
}

func PublishSyncerStateMetrics(stateCount *syncerStateCount) {
	syncerSyncerState.WithLabelValues(EPCountsDiffer).Set(float64(stateCount.epCountsDiffer))
	syncerSyncerState.WithLabelValues(EPNodeMissing).Set(float64(stateCount.epNodeMissing))
	syncerSyncerState.WithLabelValues(EPNodeNotFound).Set(float64(stateCount.epNodeNotFound))
	syncerSyncerState.WithLabelValues(EPPodMissing).Set(float64(stateCount.epPodMissing))
	syncerSyncerState.WithLabelValues(EPPodNotFound).Set(float64(stateCount.epPodNotFound))
	syncerSyncerState.WithLabelValues(EPPodTypeAssertionFailed).Set(float64(stateCount.epPodTypeAssertionFailed))
	syncerSyncerState.WithLabelValues(EPZoneMissing).Set(float64(stateCount.epZoneMissing))
	syncerSyncerState.WithLabelValues(EPSEndpointCountZero).Set(float64(stateCount.epsEndpointCountZero))
	syncerSyncerState.WithLabelValues(EPCalculationCountZero).Set(float64(stateCount.epCalculationCountZero))
	syncerSyncerState.WithLabelValues(InvalidAPIResponse).Set(float64(stateCount.invalidAPIResponse))
	syncerSyncerState.WithLabelValues(InvalidEPAttach).Set(float64(stateCount.invalidEPAttach))
	syncerSyncerState.WithLabelValues(InvalidEPDetach).Set(float64(stateCount.invalidEPDetach))
	syncerSyncerState.WithLabelValues(NegNotFound).Set(float64(stateCount.negNotFound))
	syncerSyncerState.WithLabelValues(CurrentNegEPNotFound).Set(float64(stateCount.currentNegEPNotFound))
	syncerSyncerState.WithLabelValues(EPSNotFound).Set(float64(stateCount.epsNotFound))
	syncerSyncerState.WithLabelValues(OtherError).Set(float64(stateCount.otherError))
	syncerSyncerState.WithLabelValues(Success).Set(float64(stateCount.success))
}
