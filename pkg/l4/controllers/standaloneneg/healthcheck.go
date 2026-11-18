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
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/klog/v2"
)

// healthCheckPort returns the port of the HTTP health check of the backend service.
// ok is false if the backend service has no HTTP health check with a fixed port.
func (c *Controller) healthCheckPort(bs *composite.BackendService, svc *v1.Service, svcLogger klog.Logger) (port int64, ok bool, err error) {
	hcs := bs.HealthChecks
	// Currently, at most one health check can be specified for each backend service.
	if len(hcs) == 0 {
		return 0, false, nil
	}
	hcID, err := cloud.ParseResourceURL(hcs[0])
	if err != nil || hcID.Key == nil || hcID.Resource != "healthChecks" {
		return 0, false, fmt.Errorf("invalid health check URL %q of backend service %s", hcs[0], bs.Name)
	}

	hc, err := composite.GetHealthCheck(c.ctx.Cloud, hcID.Key, meta.VersionBeta, svcLogger)
	switch {
	case utils.IsNotFoundError(err):
		c.recordNoHTTPHealthCheck(svc, bs, fmt.Sprintf("health check %s not found", hcs[0]), svcLogger)
		return 0, false, nil
	case err != nil:
		return 0, false, fmt.Errorf("failed to get health check %s: %w", hcs[0], err)
	case hc.Type != "HTTP" || hc.HttpHealthCheck == nil:
		c.recordNoHTTPHealthCheck(svc, bs, fmt.Sprintf("health check %s is of type %s, not HTTP", hcs[0], hc.Type), svcLogger)
		return 0, false, nil
	case hc.HttpHealthCheck.Port == 0:
		c.recordNoHTTPHealthCheck(svc, bs, fmt.Sprintf("HTTP health check %s has no fixed port (port specification %q)", hcs[0], hc.HttpHealthCheck.PortSpecification), svcLogger)
		return 0, false, nil
	}
	return hc.HttpHealthCheck.Port, true, nil
}

// recordNoHTTPHealthCheck reports that the backend service has no HTTP health check
// with a fixed port, so no health check redirection is configured for it.
func (c *Controller) recordNoHTTPHealthCheck(svc *v1.Service, bs *composite.BackendService, reason string, svcLogger klog.Logger) {
	c.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "NoHTTPHealthChecks", "Backend service %s has no usable HTTP health check: %s", bs.Name, reason)
	svcLogger.V(4).Info("Backend service has no usable HTTP health check", "backendService", bs.Name, "reason", reason)
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

// cachedHealthCheckPort returns the health check port of the backend service,
// retrieving each health check only once per sync.
func (c *Controller) cachedHealthCheckPort(resourceCache *lbResourceCache, bs *composite.BackendService, svc *v1.Service, svcLogger klog.Logger) (port int64, ok bool, err error) {
	if len(bs.HealthChecks) == 0 {
		return 0, false, nil
	}
	hcURL := bs.HealthChecks[0]
	if result, found := resourceCache.hcPorts[hcURL]; found {
		return result.port, result.ok, result.err
	}
	port, ok, err = c.healthCheckPort(bs, svc, svcLogger)
	resourceCache.hcPorts[hcURL] = hcPortResult{port: port, ok: ok, err: err}
	return port, ok, err
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
