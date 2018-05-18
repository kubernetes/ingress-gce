/*
Copyright 2017 The Kubernetes Authors.

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

package errors

import (
	"fmt"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils"
)

// ErrSvcNotNodePort is returned when the service is not a nodeport.
type ErrSvcNotNodePort struct {
	Service types.NamespacedName
}

func (e ErrSvcNotNodePort) Error() string {
	return fmt.Sprintf("service %q is not type 'NodePort'", e.Service)
}

// ErrSvcNotFound is returned when a service is not found.
type ErrSvcNotFound struct {
	Service types.NamespacedName
}

func (e ErrSvcNotFound) Error() string {
	return fmt.Sprintf("could not find service %q", e.Service)
}

// ErrSvcPortNotFound is returned when a service's port is not found.
type ErrSvcPortNotFound struct {
	utils.ServicePortID
}

func (e ErrSvcPortNotFound) Error() string {
	return fmt.Sprintf("could not find port %q in service %q", e.ServicePortID.Port, e.ServicePortID.Service)
}

// ErrSvcAppProtosParsing is returned when the service is malformed.
type ErrSvcAppProtosParsing struct {
	Svc *v1.Service
	Err error
}

func (e ErrSvcAppProtosParsing) Error() string {
	return fmt.Sprintf("could not parse %v annotation on Service %v/%v, err: %v", annotations.ServiceApplicationProtocolKey, e.Svc.Namespace, e.Svc.Name, e.Err)
}
