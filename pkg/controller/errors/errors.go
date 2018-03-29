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
	"k8s.io/api/extensions/v1beta1"

	"k8s.io/ingress-gce/pkg/annotations"
)

// ErrNodePortNotFound is returned when a port was not found.
type ErrNodePortNotFound struct {
	Backend v1beta1.IngressBackend
	Err     error
}

func (e ErrNodePortNotFound) Error() string {
	return fmt.Sprintf("Could not find nodeport for backend %+v: %v", e.Backend, e.Err)
}

// ErrSvcAppProtosParsing is returned when the service is malformed.
type ErrSvcAppProtosParsing struct {
	Svc *v1.Service
	Err error
}

func (e ErrSvcAppProtosParsing) Error() string {
	return fmt.Sprintf("could not parse %v annotation on Service %v/%v, err: %v", annotations.ServiceApplicationProtocolKey, e.Svc.Namespace, e.Svc.Name, e.Err)
}

// ErrServiceExtension is returned when fail to retrieve serviceExtension for service.
type ErrServiceExtension struct {
	Svc *v1.Service
	Err error
}

func (e ErrServiceExtension) Error() string {
	return fmt.Sprintf("failed to retrieve ServiceExtension referenced by Service %s/%s, err: %v", e.Svc.Namespace, e.Svc.Name, e.Err)
}
