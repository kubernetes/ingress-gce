/*
Copyright 2018 The Kubernetes Authors.

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

package controller

import (
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/utils"

	extensions "k8s.io/api/extensions/v1beta1"
)

// gcState is used by the controller to maintain state for garbage collection routines.
type gcState struct {
	ingresses []*extensions.Ingress
	lbNames   []string
	svcPorts  []utils.ServicePort
}

// syncState is used by the controller to maintain state for routines that sync GCP resources of an Ingress.
type syncState struct {
	urlMap *utils.GCEURLMap
	ing    *extensions.Ingress
	l7     *loadbalancers.L7
}
