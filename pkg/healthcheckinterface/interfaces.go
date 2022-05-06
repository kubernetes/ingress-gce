/*
Copyright 2015 The Kubernetes Authors.

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

package healthcheckinterface

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
)

// L4HealthChecks defines methods for creating and deleting health checks (and their firewall rules) for l4 services
type L4HealthChecks interface {
	// EnsureL4HealthCheck creates health check (and firewall rule) for l4 service
	EnsureL4HealthCheck(svc *v1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType, nodeNames []string) (string, string, string, string, error)
	// DeleteHealthCheck deletes health check (and firewall rule) for l4 service
	DeleteHealthCheck(svc *v1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType) (string, error)
}
