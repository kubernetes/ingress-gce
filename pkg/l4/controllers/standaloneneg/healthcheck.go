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
	"fmt"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/klog/v2"
)

// externalHealthCheckAddrs returns the VIP:port entries of the external health
// check annotation: the HTTP health check traffic to these addresses is
// redirected to the health check node port of the service. It also returns
// the health check ports colliding with the TCP ports of the service, mapped
// to the forwarding rules using them: they are skipped, as the health check
// traffic can't be distinguished from the service traffic.
func externalHealthCheckAddrs(svc *v1.Service, usable []usableFR) (addrs []string, collisions map[int64][]string) {
	collisions = make(map[int64][]string)
	seenAddrs := sets.New[string]()
	for _, u := range usable {
		switch {
		case u.hcPort == 0:
			// No HTTP health check with a fixed port, nothing to redirect.
		case u.hcPort == int64(svc.Spec.HealthCheckNodePort):
			// The health check already targets the service health check node port, no redirection is needed.
		case serviceServesTCPPort(svc, u.hcPort):
			collisions[u.hcPort] = append(collisions[u.hcPort], u.ref.raw)
		default:
			for _, vip := range u.vips {
				addr := net.JoinHostPort(vip, strconv.FormatInt(u.hcPort, 10))
				if !seenAddrs.Has(addr) {
					seenAddrs.Insert(addr)
					addrs = append(addrs, addr)
				}
			}
		}
	}
	return addrs, collisions
}

// reportHealthCheckPortCollisions reports the health check ports colliding
// with the TCP ports of the service.
func (c *Controller) reportHealthCheckPortCollisions(svc *v1.Service, collisions map[int64][]string) {
	for _, port := range slices.Sorted(maps.Keys(collisions)) {
		c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "PortCollision",
			"Health check port %d collides with a TCP port of the service, skipping it in the %s annotation for forwarding rules: %s",
			port, annotations.ExternalHealthCheckKey, strings.Join(collisions[port], ", "))
	}
}

// patchExternalHealthCheckAnnotation updates the external health check
// annotation of the service and reports the result in an event.
func (c *Controller) patchExternalHealthCheckAnnotation(svc *v1.Service, addrs []string, svcLogger klog.Logger) error {
	changed, err := c.updateExternalHealthCheckAnnotation(svc, addrs, svcLogger)
	if err != nil {
		c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "UpdateExternalHealthCheckFailed",
			"Failed to update external health check annotation for load balancer, err: %v", err)
		return fmt.Errorf("failed to update the %s annotation: %w", annotations.ExternalHealthCheckKey, err)
	}
	if changed {
		c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeNormal, "UpdateExternalHealthCheckSuccessful",
			"Successfully updated external health check annotation for load balancer")
	}
	return nil
}

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
