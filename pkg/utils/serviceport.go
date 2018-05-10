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

package utils

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/annotations"
)

// ServicePort maintains configuration for a single backend.
type ServicePort struct {
	SvcName       types.NamespacedName
	SvcPort       intstr.IntOrString
	NodePort      int64
	Protocol      annotations.AppProtocol
	SvcTargetPort string
	NEGEnabled    bool
}

// Description returns a string describing the ServicePort.
func (sp ServicePort) Description() string {
	if sp.SvcName.String() == "" || sp.SvcPort.String() == "" {
		return ""
	}
	return fmt.Sprintf(`{"kubernetes.io/service-name":"%s","kubernetes.io/service-port":"%s"}`, sp.SvcName.String(), sp.SvcPort.String())
}

// IsAlpha returns true if the ServicePort is using ProtocolHTTP2 - which means
// we need to use the Alpha API.
func (sp ServicePort) IsAlpha() bool {
	return sp.Protocol == annotations.ProtocolHTTP2
}

// BackendName returns the name of the backend which would be used for this ServicePort.
func (sp ServicePort) BackendName(namer *Namer) string {
	if !sp.NEGEnabled {
		return namer.Backend(sp.NodePort)
	}

	return namer.BackendNEG(sp.SvcName.Namespace, sp.SvcName.Name, sp.SvcTargetPort)
}
