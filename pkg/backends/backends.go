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

package backends

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang/glog"

	computealpha "google.golang.org/api/compute/v0.alpha"
	compute "google.golang.org/api/compute/v1"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud/meta"

	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/healthchecks"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/storage"
	"k8s.io/ingress-gce/pkg/utils"
)

// BalancingMode represents the loadbalancing configuration of an individual
// Backend in a BackendService. This is *effectively* a cluster wide setting
// since you can't mix modes across Backends pointing to the same IG, and you
// can't have a single node in more than 1 loadbalanced IG.
type BalancingMode string

const (
	// Rate balances incoming requests based on observed RPS.
	// As of this writing, it's the only balancing mode supported by GCE's
	// internal LB. This setting doesn't make sense for Kubernetes clusters
	// because requests can get proxied between instance groups in different
	// zones by kube-proxy without GCE even knowing it. Setting equal RPS on
	// all IGs should achieve roughly equal distribution of requests.
	Rate BalancingMode = "RATE"
	// Utilization balances incoming requests based on observed utilization.
	// This mode is only useful if you want to divert traffic away from IGs
	// running other compute intensive workloads. Utilization statistics are
	// aggregated per instances, not per container, and requests can get proxied
	// between instance groups in different zones by kube-proxy without GCE even
	// knowing about it.
	Utilization BalancingMode = "UTILIZATION"
	// Connections balances incoming requests based on a connection counter.
	// This setting currently doesn't make sense for Kubernetes clusters,
	// because we use NodePort Services as HTTP LB backends, so GCE's connection
	// counters don't accurately represent connections per container.
	Connections BalancingMode = "CONNECTION"
)

// maxRPS is the RPS setting for all Backends with BalancingMode RATE. The exact
// value doesn't matter, as long as it's the same for all Backends. Requests
// received by GCLB above this RPS are NOT dropped, GCLB continues to distribute
// them across IGs.
// TODO: Should this be math.MaxInt64?
const maxRPS = 1

// Backends implements BackendPool.
type Backends struct {
	cloud         *gce.GCECloud
	negGetter     NEGGetter
	nodePool      instances.NodePool
	healthChecker healthchecks.HealthChecker
	snapshotter   storage.Snapshotter
	prober        ProbeProvider
	namer         *utils.Namer
}

// Backends is a BackendPool.
var _ BackendPool = (*Backends)(nil)

// NewBackendPool returns a new backend pool.
// - cloud: implements BackendServices and syncs backends with a cloud provider
// - healthChecker: is capable of producing health checks for backends.
// - nodePool: implements NodePool, used to create/delete new instance groups.
// - namer: procudes names for backends.
// - ignorePorts: is a set of ports to avoid syncing/GCing.
// - resyncWithCloud: if true, periodically syncs with cloud resources.
func NewBackendPool(
	cloud *gce.GCECloud,
	negGetter NEGGetter,
	healthChecker healthchecks.HealthChecker,
	nodePool instances.NodePool,
	namer *utils.Namer,
	resyncWithCloud bool) *Backends {

	backendPool := &Backends{
		cloud:         cloud,
		negGetter:     negGetter,
		nodePool:      nodePool,
		healthChecker: healthChecker,
		namer:         namer,
	}
	if !resyncWithCloud {
		backendPool.snapshotter = storage.NewInMemoryPool()
		return backendPool
	}
	keyFunc := func(i interface{}) (string, error) {
		bs := i.(*compute.BackendService)
		if !namer.NameBelongsToCluster(bs.Name) {
			return "", fmt.Errorf("unrecognized name %v", bs.Name)
		}
		return bs.Name, nil
	}
	backendPool.snapshotter = storage.NewCloudListingPool("backends", keyFunc, backendPool, 30*time.Second)
	return backendPool
}

// getHealthCheckLink gets the Healthcheck link off the BackendService
func getHealthCheckLink(be *composite.BackendService) string {
	if len(be.HealthChecks) == 1 {
		return be.HealthChecks[0]
	}
	return "invalid-healthcheck-link"
}

// ensureProtocol updates the BackendService Protocol with the expected value
func ensureProtocol(be *composite.BackendService, p utils.ServicePort) (needsUpdate bool) {
	if be.Protocol == string(p.Protocol) {
		return false
	}
	be.Protocol = string(p.Protocol)
	return true
}

// ensureDescription updates the BackendService Description with the expected value
func ensureDescription(be *composite.BackendService, description string) (needsUpdate bool) {
	if be.Description == description {
		return false
	}
	be.Description = description
	return true
}

// ensureHealthCheckLink updates the BackendService HealthCheck with the expected value
func ensureHealthCheckLink(be *composite.BackendService, hcLink string) (needsUpdate bool) {
	existingHCLink := getHealthCheckLink(be)

	// Compare health check name instead of health check link.
	// This is because health check link contains api version.
	// For NEG, the api version for health check will be alpha.
	// Hence, it will cause the health check links to be always different
	// TODO (mixia): compare health check link directly once NEG is GA
	existingHCName := retrieveObjectName(existingHCLink)
	expectedHCName := retrieveObjectName(hcLink)
	if existingHCName == expectedHCName {
		return false
	}

	be.HealthChecks = []string{hcLink}
	return true
}

// Init sets the probeProvider interface value
func (b *Backends) Init(pp ProbeProvider) {
	b.prober = pp
}

// Get returns a single backend.
func (b *Backends) Get(name string, version meta.Version) (*composite.BackendService, error) {
	be, err := composite.GetBackendService(name, version, b.cloud)
	if err != nil {
		return nil, err
	}
	b.snapshotter.Add(name, be)
	return be, nil
}

func (b *Backends) ensureHealthCheck(sp utils.ServicePort, hasLegacyHC bool) (string, error) {
	hc := b.healthChecker.New(sp)
	if hasLegacyHC {
		existingLegacyHC, err := b.healthChecker.GetLegacy(sp.NodePort)
		if err != nil && !utils.IsNotFoundError(err) {
			return "", err
		}
		if existingLegacyHC != nil {
			glog.V(4).Infof("Applying settings of existing health check to newer health check on port %+v", sp)
			applyLegacyHCToHC(existingLegacyHC, hc)
		}
	} else if b.prober != nil {
		probe, err := b.prober.GetProbe(sp)
		if err != nil {
			return "", err
		}
		if probe != nil {
			glog.V(4).Infof("Applying httpGet settings of readinessProbe to health check on port %+v", sp)
			applyProbeSettingsToHC(probe, hc)
		}
	}

	return b.healthChecker.Sync(hc)
}

func (b *Backends) create(hcLink string, sp utils.ServicePort, name string) (*composite.BackendService, error) {
	namedPort := &compute.NamedPort{
		Name: b.namer.NamedPort(sp.NodePort),
		Port: sp.NodePort,
	}
	be := &composite.BackendService{
		Version:      sp.Version(),
		Name:         name,
		Description:  sp.Description(),
		Protocol:     string(sp.Protocol),
		HealthChecks: []string{hcLink},
		Port:         namedPort.Port,
		PortName:     namedPort.Name,
	}
	if err := composite.CreateBackendService(be, b.cloud); err != nil {
		return nil, err
	}
	// Note: We need to perform a GCE call to re-fetch the object we just created
	// so that the "Fingerprint" field is filled in. This is needed to update the
	// object without error.
	return b.Get(name, sp.Version())
}

// Ensure will update or create Backends for the given ports.
func (b *Backends) Ensure(svcPorts []utils.ServicePort, igLinks []string) error {
	glog.V(3).Infof("Sync: backends %v", svcPorts)
	// create backends for new ports, perform an edge hop for existing ports
	for _, port := range svcPorts {
		if err := b.ensureBackendService(port, igLinks); err != nil {
			return err
		}
	}
	return nil
}

// ensureBackendService will update or create a Backend for the given port.
func (b *Backends) ensureBackendService(sp utils.ServicePort, igLinks []string) error {
	// We must track the ports even if creating the backends failed, because
	// we might've created health-check for them.
	be := &composite.BackendService{}
	beName := sp.BackendName(b.namer)

	defer func() {
		b.snapshotter.Add(beName, be)
	}()

	be, getErr := b.Get(beName, sp.Version())
	hasLegacyHC := false
	if be != nil {
		// If the backend already exists, find out if it is using a legacy health check.
		existingHCLink := getHealthCheckLink(be)
		if strings.Contains(existingHCLink, "/httpHealthChecks/") {
			hasLegacyHC = true
		}
	}

	// Ensure health check for backend service exists. Note that hasLegacyHC
	// will dictate whether we search for an existing legacy health check.
	hcLink, err := b.ensureHealthCheck(sp, hasLegacyHC)
	if err != nil {
		return err
	}

	// Verify existance of a backend service for the proper port, but do not specify any backends/igs
	if getErr != nil {
		if !utils.IsNotFoundError(getErr) {
			return getErr
		}
		// Only create the backend service if the error was 404.
		glog.V(2).Infof("Creating backend service for port %v named %v", sp.NodePort, beName)
		be, err = b.create(hcLink, sp, beName)
		if err != nil {
			return err
		}
	}
	be.Version = sp.Version()
	needUpdate := ensureProtocol(be, sp)
	needUpdate = ensureHealthCheckLink(be, hcLink) || needUpdate
	needUpdate = ensureDescription(be, sp.Description()) || needUpdate
	if needUpdate {
		if err = composite.UpdateBackendService(be, b.cloud); err != nil {
			return err
		}
	}

	// If previous health check was legacy type, we need to delete it.
	if hasLegacyHC {
		if err = b.healthChecker.DeleteLegacy(sp.NodePort); err != nil {
			glog.Warning("Failed to delete legacy HttpHealthCheck %v; Will not try again, err: %v", beName, err)
		}
	}

	// If there are instance pools(node pool is synced) and NEG is not enabled,
	// perform edgeHop to verify that BackendServices contains links to all
	// backends/instancegroups
	if len(igLinks) > 0 && !sp.NEGEnabled {
		return b.edgeHop(be, igLinks)
	}

	return nil
}

// edgeHop checks the links of the given backend by executing an edge hop.
// It fixes broken links and updates the Backend accordingly.
func (b *Backends) edgeHop(be *composite.BackendService, igLinks []string) error {
	addIGs := getInstanceGroupsToAdd(be, igLinks)
	if len(addIGs) == 0 {
		return nil
	}

	originalIGBackends := []*composite.Backend{}
	for _, backend := range be.Backends {
		// Backend service is not able to point to NEG and IG at the same time.
		// Filter IG backends here.
		if strings.Contains(backend.Group, "instanceGroups") {
			originalIGBackends = append(originalIGBackends, backend)
		}
	}

	// We first try to create the backend with balancingMode=RATE.  If this	+	return addIGs
	// fails, it's mostly likely because there are existing backends with
	// balancingMode=UTILIZATION. This failure mode throws a googleapi error
	// which wraps a HTTP 400 status code. We handle it in the loop below
	// and come around to retry with the right balancing mode. The goal is to
	// switch everyone to using RATE.
	var errs []string
	for _, bm := range []BalancingMode{Rate, Utilization} {
		// Generate backends with given instance groups with a specific mode
		newBackends := getBackendsForIGs(addIGs, bm)
		be.Backends = append(originalIGBackends, newBackends...)

		if err := composite.UpdateBackendService(be, b.cloud); err != nil {
			if utils.IsHTTPErrorCode(err, http.StatusBadRequest) {
				glog.V(2).Infof("Updating backend service backends with balancing mode %v failed, will try another mode. err:%v", bm, err)
				errs = append(errs, err.Error())
				// This is probably a failure because we tried to create the backend
				// with balancingMode=RATE when there are already backends with
				// balancingMode=UTILIZATION. Just ignore it and retry setting
				// balancingMode=UTILIZATION (b/35102911).
				continue
			}
			glog.V(2).Infof("Error updating backend service backends with balancing mode %v:%v", bm, err)
			return err
		}
		// Successfully updated Backends, no need to Update the BackendService again
		return nil
	}
	return fmt.Errorf("received errors when updating backend service: %v", strings.Join(errs, "\n"))
}

// Delete deletes the Backend for the given port.
func (b *Backends) Delete(name string) (err error) {
	defer func() {
		if utils.IsHTTPErrorCode(err, http.StatusNotFound) {
			err = nil
		}
		if err == nil {
			b.snapshotter.Delete(name)
		}
	}()

	glog.V(2).Infof("Deleting backend service %v", name)

	// Try deleting health checks even if a backend is not found.
	if err = b.cloud.DeleteGlobalBackendService(name); err != nil && !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		return err
	}

	return b.healthChecker.Delete(name)
}

// List lists all backends.
func (b *Backends) List() ([]interface{}, error) {
	// TODO: for consistency with the rest of this sub-package this method
	// should return a list of backend ports.
	backends, err := b.cloud.ListGlobalBackendServices()
	if err != nil {
		return nil, err
	}
	var ret []interface{}
	for _, x := range backends {
		ret = append(ret, x)
	}
	return ret, nil
}

func getBackendsForIGs(igLinks []string, bm BalancingMode) []*composite.Backend {
	var backends []*composite.Backend
	for _, igLink := range igLinks {
		b := &composite.Backend{
			Group:         igLink,
			BalancingMode: string(bm),
		}
		switch bm {
		case Rate:
			b.MaxRatePerInstance = maxRPS
		default:
			// TODO: Set utilization and connection limits when we accept them
			// as valid fields.
		}

		backends = append(backends, b)
	}
	return backends
}

func getBackendsForNEGs(negs []*computealpha.NetworkEndpointGroup) []*computealpha.Backend {
	var backends []*computealpha.Backend
	for _, neg := range negs {
		b := &computealpha.Backend{
			Group:              neg.SelfLink,
			BalancingMode:      string(Rate),
			MaxRatePerEndpoint: maxRPS,
		}
		backends = append(backends, b)
	}
	return backends
}

func getInstanceGroupsToAdd(be *composite.BackendService, igLinks []string) []string {
	beName := be.Name
	beIGs := sets.String{}
	for _, existingBe := range be.Backends {
		beIGs.Insert(comparableGroupPath(existingBe.Group))
	}

	expectedIGs := sets.String{}
	for _, igLink := range igLinks {
		expectedIGs.Insert(comparableGroupPath(igLink))
	}

	if beIGs.IsSuperset(expectedIGs) {
		return nil
	}
	glog.V(2).Infof("Expected igs for backend service %v: %+v, current igs %+v",
		beName, expectedIGs.List(), beIGs.List())

	var addIGs []string
	for _, igLink := range igLinks {
		if !beIGs.Has(comparableGroupPath(igLink)) {
			addIGs = append(addIGs, igLink)
		}
	}

	return addIGs
}

// GC garbage collects services corresponding to ports in the given list.
func (b *Backends) GC(svcPorts []utils.ServicePort) error {
	knownPorts := sets.NewString()
	for _, sp := range svcPorts {
		name := sp.BackendName(b.namer)
		knownPorts.Insert(name)
	}
	pool := b.snapshotter.Snapshot()
	for port := range pool {
		if knownPorts.Has(port) {
			continue
		}

		glog.V(3).Infof("GCing backendService for port %s", port)
		if err := b.Delete(port); err != nil && !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
			return err
		}
	}
	return nil
}

// Shutdown deletes all backends and the default backend.
// This will fail if one of the backends is being used by another resource.
func (b *Backends) Shutdown() error {
	if err := b.GC([]utils.ServicePort{}); err != nil {
		return err
	}
	return nil
}

// Status returns the status of the given backend by name.
func (b *Backends) Status(name string) string {
	backend, err := b.cloud.GetGlobalBackendService(name)
	if err != nil || len(backend.Backends) == 0 {
		return "Unknown"
	}

	// TODO: Look at more than one backend's status
	// TODO: Include port, ip in the status, since it's in the health info.
	hs, err := b.cloud.GetGlobalBackendServiceHealth(name, backend.Backends[0].Group)
	if err != nil || len(hs.HealthStatus) == 0 || hs.HealthStatus[0] == nil {
		return "Unknown"
	}
	// TODO: State transition are important, not just the latest.
	return hs.HealthStatus[0].HealthState
}

func (b *Backends) Link(sp utils.ServicePort, zones []string) error {
	if !sp.NEGEnabled {
		return nil
	}
	negName := sp.BackendName(b.namer)
	var negs []*computealpha.NetworkEndpointGroup
	var err error
	for _, zone := range zones {
		neg, err := b.negGetter.GetNetworkEndpointGroup(negName, zone)
		if err != nil {
			return err
		}
		negs = append(negs, neg)
	}

	beName := sp.BackendName(b.namer)
	backendService, err := b.cloud.GetAlphaGlobalBackendService(beName)
	if err != nil {
		return err
	}

	targetBackends := getBackendsForNEGs(negs)
	oldBackends := sets.NewString()
	newBackends := sets.NewString()

	// WARNING: the backend link includes api version.
	// API versions has to match, otherwise backend link will be always different.
	for _, be := range backendService.Backends {
		oldBackends.Insert(be.Group)
	}
	for _, be := range targetBackends {
		newBackends.Insert(be.Group)
	}

	if !oldBackends.Equal(newBackends) {
		backendService.Backends = targetBackends
		return b.cloud.UpdateAlphaGlobalBackendService(backendService)
	}
	return nil
}

func applyLegacyHCToHC(existing *compute.HttpHealthCheck, hc *healthchecks.HealthCheck) {
	hc.Description = existing.Description
	hc.CheckIntervalSec = existing.CheckIntervalSec
	hc.HealthyThreshold = existing.HealthyThreshold
	hc.Host = existing.Host
	hc.Port = existing.Port
	hc.RequestPath = existing.RequestPath
	hc.TimeoutSec = existing.TimeoutSec
	hc.UnhealthyThreshold = existing.UnhealthyThreshold
}

func applyProbeSettingsToHC(p *v1.Probe, hc *healthchecks.HealthCheck) {
	healthPath := p.Handler.HTTPGet.Path
	// GCE requires a leading "/" for health check urls.
	if !strings.HasPrefix(healthPath, "/") {
		healthPath = "/" + healthPath
	}
	// Extract host from HTTP headers
	host := p.Handler.HTTPGet.Host
	for _, header := range p.Handler.HTTPGet.HTTPHeaders {
		if header.Name == "Host" {
			host = header.Value
			break
		}
	}
	hc.RequestPath = healthPath
	hc.Host = host
	hc.Description = "Kubernetes L7 health check generated with readiness probe settings."
	hc.TimeoutSec = int64(p.TimeoutSeconds)
	if hc.ForNEG {
		// For NEG mode, we can support more aggressive healthcheck interval.
		hc.CheckIntervalSec = int64(p.PeriodSeconds)
	} else {
		// For IG mode, short healthcheck interval may health check flooding problem.
		hc.CheckIntervalSec = int64(p.PeriodSeconds) + int64(healthchecks.DefaultHealthCheckInterval.Seconds())
	}
}

//retrieveObjectName takes a GCE object link and return the last part of the url as object name
func retrieveObjectName(url string) string {
	splited := strings.Split(url, "/")
	return splited[len(splited)-1]
}

// comparableGroupPath trims project and compute version from the SelfLink
// /zones/[ZONE_NAME]/instanceGroups/[IG_NAME]
func comparableGroupPath(url string) string {
	path_parts := strings.Split(url, "/zones/")
	return fmt.Sprintf("/zones/%s", path_parts[1])
}
