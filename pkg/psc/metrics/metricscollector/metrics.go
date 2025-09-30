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

package metricscollector

import "github.com/prometheus/client_golang/prometheus"

type feature string

func (f feature) String() string {
	return string(f)
}

const (
	// PSC Features
	sa          = feature("ServiceAttachments")
	saInSuccess = feature("ServiceAttachmentInSuccess")
	saInError   = feature("ServiceAttachmentInError")
	services    = feature("Services")
)

var (
	serviceAttachmentCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "number_of_service_attachments",
			Help: "Number of Service Attachments",
		},
		[]string{"feature"},
	)
	serviceCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "number_of_services",
			Help: "Number of Services",
		},
		[]string{"feature"},
	)
)
