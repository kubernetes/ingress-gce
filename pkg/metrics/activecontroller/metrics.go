/*
Copyright 2025 The Kubernetes Authors.

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

package activecontrollermetrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// runningControllerName represents a name of a running controller.
type runningControllerName string

const (
	runningControllerValue = 1.0
	stoppedControllerValue = 0.0

	NEGControllerLabel     runningControllerName = "NEG"
	L4ILBControllerLabel   runningControllerName = "L4ILB"
	L4NetLBControllerLabel runningControllerName = "L4NetLB"
	IGControllerLabel      runningControllerName = "IG"
	PSCControllerLabel     runningControllerName = "PSC"
	IngressControllerLabel runningControllerName = "Ingress"
)

var (
	runningControllers = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "running_controllers",
			Help: "Metric fthat indicates which of the the ingress-gce controllers (Ingress, NEG, L4, IG, PSC) are running in the cluster",
		},
		[]string{"controller"},
	)
)

func init() {
	prometheus.MustRegister(runningControllers)
}

// RecordRunningController records in the metric state that the given controller is running.
func RecordRunningController(name runningControllerName) {
	runningControllers.WithLabelValues(string(name)).Set(runningControllerValue)
}

// RecordStoppedController records in the metric state that the given controller is not running.
func RecordStoppedController(name runningControllerName) {
	runningControllers.WithLabelValues(string(name)).Set(stoppedControllerValue)
}
