/*
Copyright 2026 The Kubernetes Authors.

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

package standaloneneg

import (
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/klog/v2"
)

// serviceServesTCPPort checks if the service exposes the given port over TCP.
// HTTP health check traffic to such port can't be redirected, as it can't be
// distinguished from the service traffic.
func serviceServesTCPPort(svc *v1.Service, port int64) bool {
	for _, sp := range svc.Spec.Ports {
		protocol := sp.Protocol
		if protocol == "" {
			protocol = v1.ProtocolTCP
		}
		if protocol == v1.ProtocolTCP && int64(sp.Port) == port {
			return true
		}
	}
	return false
}

// updateExternalHealthCheckAnnotation sets the external health check annotation of the service
// to the given addresses, or removes the annotation if there are none.
// It returns true if the annotation was changed.
func (c *Controller) updateExternalHealthCheckAnnotation(svc *v1.Service, addrs []string, svcLogger klog.Logger) (bool, error) {
	newObjectMeta := svc.ObjectMeta.DeepCopy()
	oldValue, exists := newObjectMeta.Annotations[annotations.ExternalHealthCheckKey]
	if len(addrs) == 0 {
		if !exists {
			return false, nil
		}
		delete(newObjectMeta.Annotations, annotations.ExternalHealthCheckKey)
	} else {
		newValue := strings.Join(addrs, ",")
		if exists && oldValue == newValue {
			return false, nil
		}
		if newObjectMeta.Annotations == nil {
			newObjectMeta.Annotations = make(map[string]string)
		}
		newObjectMeta.Annotations[annotations.ExternalHealthCheckKey] = newValue
	}
	svcLogger.V(3).Info("Updating external health check annotation of service", "annotationKey", annotations.ExternalHealthCheckKey, "addresses", addrs)
	if err := patch.PatchServiceObjectMetadata(c.ctx.KubeClient.CoreV1(), svc, *newObjectMeta); err != nil {
		return false, err
	}
	return true, nil
}
