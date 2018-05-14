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
	cloud         BackendServices
	negGetter     NEGGetter
	nodePool      instances.NodePool
	healthChecker healthchecks.HealthChecker
	snapshotter   storage.Snapshotter
	prober        ProbeProvider
	namer         *utils.Namer
}

// BackendService embeds both the GA and alpha compute BackendService types
type BackendService struct {
	Alpha *computealpha.BackendService
	Ga    *compute.BackendService
}

// GetProtocol gets the Protocol off the correct BackendService
func (be *BackendService) GetProtocol() string {
	if be.Alpha != nil {
		return be.Alpha.Protocol
	}

	return be.Ga.Protocol
}

// GetDescription gets the Description off the correct BackendService
func (be *BackendService) GetDescription() string {
	if be.Alpha != nil {
		return be.Alpha.Description
	}

	return be.Ga.Description
}

// GetHealthCheckLink gets the Healthcheck link off the correct BackendService
func (be *BackendService) GetHealthCheckLink() string {
	if be.Alpha != nil && len(be.Alpha.HealthChecks) == 1 {
		return be.Alpha.HealthChecks[0]
	}

	if len(be.Ga.HealthChecks) == 1 {
		return be.Ga.HealthChecks[0]
	}

	return "invalid-healthcheck-link"
}

// ensureProtocol updates the BackendService Protocol with the expected value
func (be *BackendService) ensureProtocol(p utils.ServicePort) (needsUpdate bool) {
	existingProtocol := be.GetProtocol()
	if existingProtocol == string(p.Protocol) {
		return false
	}

	if be.Alpha != nil {
		be.Alpha.Protocol = string(p.Protocol)
	}

	be.Ga.Protocol = string(p.Protocol)
	return true
}

// ensureHealthCheckLink updates the BackendService HealthCheck with the expected value
func (be *BackendService) ensureHealthCheckLink(hcLink string) (needsUpdate bool) {
	existingHCLink := be.GetHealthCheckLink()

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

	if be.Alpha != nil {
		be.Alpha.HealthChecks = []string{hcLink}
	}

	be.Ga.HealthChecks = []string{hcLink}
	return true
}

// ensureDescription updates the BackendService Description with the expected value
func (be *BackendService) ensureDescription(description string) (needsUpdate bool) {
	existingDescription := be.GetDescription()
	if existingDescription == description {
		return false
	}
	if be.Alpha != nil {
		be.Alpha.Description = description
	}
	be.Ga.Description = description
	return true
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
	cloud BackendServices,
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

// Init sets the probeProvider interface value
func (b *Backends) Init(pp ProbeProvider) {
	b.prober = pp
}

// Get returns a single backend.
func (b *Backends) Get(name string, isAlpha bool) (*BackendService, error) {
	beGa, err := b.cloud.GetGlobalBackendService(name)
	if err != nil {
		return nil, err
	}

	var beAlpha *computealpha.BackendService
	// If the Protocol is empty, this means this is a alpha BackendService and
	// Protocol is expected to be HTTP2
	// WARNING: If a user has created an alpha BackendService in the past but
	// is no longer alpha whitelisted, this will always return an isForbidden err
	// until the user can access the alpha APIs again.
	if beGa.Protocol == "" || isAlpha {
		beAlpha, err = b.cloud.GetAlphaGlobalBackendService(beGa.Name)
		if err != nil {
			return nil, err
		}
	}

	b.snapshotter.Add(name, beGa)
	return &BackendService{Ga: beGa, Alpha: beAlpha}, nil
}

func (b *Backends) ensureHealthCheck(sp utils.ServicePort) (string, error) {
	hc := b.healthChecker.New(sp)
	existingLegacyHC, err := b.healthChecker.GetLegacy(sp.NodePort)
	if err != nil && !utils.IsNotFoundError(err) {
		return "", err
	}

	if existingLegacyHC != nil {
		glog.V(4).Infof("Applying settings of existing health check to newer health check on port %+v", sp)
		applyLegacyHCToHC(existingLegacyHC, hc)
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

func (b *Backends) create(namedPort *compute.NamedPort, hcLink string, sp utils.ServicePort, name string) (*BackendService, error) {
	isAlpha := sp.IsAlpha()
	if isAlpha {
		bsAlpha := &computealpha.BackendService{
			Name:         name,
			Description:  sp.Description(),
			Protocol:     string(sp.Protocol),
			HealthChecks: []string{hcLink},
			Port:         namedPort.Port,
			PortName:     namedPort.Name,
		}

		if err := b.cloud.CreateAlphaGlobalBackendService(bsAlpha); err != nil {
			return nil, err
		}
	} else {
		bs := &compute.BackendService{
			Name:         name,
			Description:  sp.Description(),
			Protocol:     string(sp.Protocol),
			HealthChecks: []string{hcLink},
			Port:         namedPort.Port,
			PortName:     namedPort.Name,
		}

		if err := b.cloud.CreateGlobalBackendService(bs); err != nil {
			return nil, err
		}
	}

	return b.Get(name, isAlpha)
}

// Ensure will update or create Backends for the given ports.
// Uses the given instance groups if non-nil, else creates instance groups.
func (b *Backends) Ensure(svcPorts []utils.ServicePort, igs []*compute.InstanceGroup) error {
	glog.V(3).Infof("Sync: backends %v", svcPorts)
	// create backends for new ports, perform an edge hop for existing ports
	for _, port := range svcPorts {
		if err := b.ensureBackendService(port, igs); err != nil {
			return err
		}
	}
	return nil
}

// ensureBackendService will update or create a Backend for the given port.
// It assumes that the instance groups have been created and required named port has been added.
// If not, then Ensure should be called instead.
func (b *Backends) ensureBackendService(sp utils.ServicePort, igs []*compute.InstanceGroup) error {
	// We must track the ports even if creating the backends failed, because
	// we might've created health-check for them.
	be := &BackendService{}
	beName := sp.BackendName(b.namer)

	defer func() {
		b.snapshotter.Add(beName, be)
	}()

	// Ensure health check for backend service exists
	hcLink, err := b.ensureHealthCheck(sp)
	if err != nil {
		return err
	}

	// Verify existance of a backend service for the proper port, but do not specify any backends/igs
	be, _ = b.Get(beName, sp.IsAlpha())
	if be == nil {
		namedPort := &compute.NamedPort{
			Name: b.namer.NamedPort(sp.NodePort),
			Port: sp.NodePort,
		}

		glog.V(2).Infof("Creating backend service for port %v named %v", sp.NodePort, beName)
		be, err = b.create(namedPort, hcLink, sp, beName)
		if err != nil {
			return err
		}
	}

	needUpdate := be.ensureProtocol(sp)
	needUpdate = needUpdate || be.ensureHealthCheckLink(hcLink)
	needUpdate = needUpdate || be.ensureDescription(sp.Description())

	if needUpdate {
		if err = b.update(be); err != nil {
			return err
		}
	}

	existingHCLink := be.GetHealthCheckLink()

	// If previous health check was legacy type, we need to delete it.
	if existingHCLink != hcLink && strings.Contains(existingHCLink, "/httpHealthChecks/") {
		if err = b.healthChecker.DeleteLegacy(sp.NodePort); err != nil {
			glog.Warning("Failed to delete legacy HttpHealthCheck %v; Will not try again, err: %v", beName, err)
		}
	}

	// If there are instance pools(node pool is synced) and NEG is not enabled,
	// perform edgeHop to verify that BackendServices contains links to all
	// backends/instancegroups
	if len(igs) > 0 && !sp.NEGEnabled {
		return b.edgeHop(be, igs)
	}

	return nil
}

// edgeHop checks the links of the given backend by executing an edge hop.
// It fixes broken links and updates the Backend accordingly.
func (b *Backends) edgeHop(be *BackendService, igs []*compute.InstanceGroup) error {
	addIGs := getInstanceGroupsToAdd(be, igs)
	if len(addIGs) == 0 {
		return nil
	}

	originalAlphaIGBackends := []*computealpha.Backend{}
	originalGaIGBackends := []*compute.Backend{}
	if be.Alpha != nil {
		for _, backend := range be.Alpha.Backends {
			// Backend service is not able to point to NEG and IG at the same time.
			// Filter IG backends here.
			if strings.Contains(backend.Group, "instanceGroups") {
				originalAlphaIGBackends = append(originalAlphaIGBackends, backend)
			}
		}
	} else {
		for _, backend := range be.Ga.Backends {
			// Backend service is not able to point to NEG and IG at the same time.
			// Filter IG backends here.
			if strings.Contains(backend.Group, "instanceGroups") {
				originalGaIGBackends = append(originalGaIGBackends, backend)
			}
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
		if be.Alpha != nil {
			newBackends := getAlphaBackendsForIGs(addIGs, bm)
			be.Alpha.Backends = append(originalAlphaIGBackends, newBackends...)
		} else {
			newBackends := getBackendsForIGs(addIGs, bm)
			be.Ga.Backends = append(originalGaIGBackends, newBackends...)
		}

		if err := b.update(be); err != nil {
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

// update calls either the GA or Alpha update path depending on Protocol
func (b *Backends) update(be *BackendService) error {
	if be.Alpha != nil {
		glog.V(2).Infof("Updating %v backend service %v", "alpha", be.Alpha.Name)
		return b.cloud.UpdateAlphaGlobalBackendService(be.Alpha)
	}
	glog.V(2).Infof("Updating %v backend service %v", "ga", be.Ga.Name)
	return b.cloud.UpdateGlobalBackendService(be.Ga)
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

func getBackendsForIGs(igs []*compute.InstanceGroup, bm BalancingMode) []*compute.Backend {
	var backends []*compute.Backend
	for _, ig := range igs {
		b := &compute.Backend{
			Group:         ig.SelfLink,
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

func getAlphaBackendsForIGs(igs []*compute.InstanceGroup, bm BalancingMode) []*computealpha.Backend {
	var backends []*computealpha.Backend
	for _, ig := range igs {
		b := &computealpha.Backend{
			Group:         ig.SelfLink,
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

func getInstanceGroupsToAdd(be *BackendService, igs []*compute.InstanceGroup) []*compute.InstanceGroup {
	// A GA link can be used to reference an alpha object - so we only need to
	// check the GA InstanceGroups.
	beName := be.Ga.Name
	beIGs := sets.String{}
	for _, existingBe := range be.Ga.Backends {
		beIGs.Insert(comparableGroupPath(existingBe.Group))
	}

	expectedIGs := sets.String{}
	for _, ig := range igs {
		expectedIGs.Insert(comparableGroupPath(ig.SelfLink))
	}

	if beIGs.IsSuperset(expectedIGs) {
		return nil
	}
	glog.V(2).Infof("Expected igs for backend service %v: %+v, current igs %+v",
		beName, expectedIGs.List(), beIGs.List())

	var addIGs []*compute.InstanceGroup
	for _, ig := range igs {
		if !beIGs.Has(comparableGroupPath(ig.SelfLink)) {
			addIGs = append(addIGs, ig)
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
	negName := b.namer.NEG(sp.ID.Service.Namespace, sp.ID.Service.Name, sp.SvcTargetPort)
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
