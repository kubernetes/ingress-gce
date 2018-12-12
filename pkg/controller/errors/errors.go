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
	v1beta1 "k8s.io/ingress-gce/pkg/apis/cloud/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
)

// ErrBadSvcType is returned when the service is not NodePort or LoadBalancer.
type ErrBadSvcType struct {
	Service     types.NamespacedName
	ServiceType v1.ServiceType
}

// Error returns the service name & type and what are acceptable types.
func (e ErrBadSvcType) Error() string {
	return fmt.Sprintf("service %q is type %q, expected \"NodePort\" or \"LoadBalancer\"", e.Service, e.ServiceType)
}

// ErrSvcNotFound is returned when a service is not found.
type ErrSvcNotFound struct {
	Service types.NamespacedName
}

// Error returns the name of the missing service.
func (e ErrSvcNotFound) Error() string {
	return fmt.Sprintf("could not find service %q", e.Service)
}

// ErrSvcPortNotFound is returned when a service's port is not found.
type ErrSvcPortNotFound struct {
	utils.ServicePortID
}

// Error returns the port name/number of the service which is not found.
func (e ErrSvcPortNotFound) Error() string {
	return fmt.Sprintf("could not find port %q in service %q", e.ServicePortID.Port.String(), e.ServicePortID.Service)
}

// ErrSvcAppProtosParsing is returned when the service is malformed.
type ErrSvcAppProtosParsing struct {
	Service types.NamespacedName
	Err     error
}

// Error returns the annotation key, service name, and the parsing error.
func (e ErrSvcAppProtosParsing) Error() string {
	return fmt.Sprintf("could not parse %q annotation on service %q, err: %v", annotations.ServiceApplicationProtocolKey, e.Service, e.Err)
}

// ErrSvcBackendConfig is returned when there was an error getting the
// BackendConfig for a service port.
type ErrSvcBackendConfig struct {
	utils.ServicePortID
	Err error
}

// Error returns the port name/number, service name, and the underlying error.
func (e ErrSvcBackendConfig) Error() string {
	return fmt.Sprintf("error getting BackendConfig for port %q on service %q, err: %v", e.ServicePortID.Port.String(), e.ServicePortID.Service.String(), e.Err)
}

// ErrBackendConfigValidation is returned when there was an error validating a BackendConfig.
type ErrBackendConfigValidation struct {
	v1beta1.BackendConfig
	Err error
}

// Error returns the BackendConfig's name and underlying error.
func (e ErrBackendConfigValidation) Error() string {
	return fmt.Sprintf("BackendConfig %v/%v is not valid: %v", e.BackendConfig.Namespace, e.BackendConfig.Name, e.Err)
}
