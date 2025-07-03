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

package backends

import (
	"fmt"
	"net/http"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/backends/features"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

const (
	DefaultConnectionDrainingTimeoutSeconds = 30
	defaultTrackingMode                     = "PER_CONNECTION"
	PerSessionTrackingMode                  = "PER_SESSION" // the only one supported with strong session affinity
	ZonalAffinityEnabledSpillover           = "ZONAL_AFFINITY_SPILL_CROSS_ZONE"
	DefaultZonalAffinitySpilloverRatio      = 0
	ZonalAffinityDisabledSpillover          = "ZONAL_AFFINITY_DISABLED"
)

// LocalityLBPolicyType is the type of locality lb policy the backend service should use.
type LocalityLBPolicyType string

const (
	// LocalityLBPolicyDefault is the default locality lb policy for a backend service.
	LocalityLBPolicyDefault LocalityLBPolicyType = ""
	// LocalityLBPolicyWeightedMaglev is the locality lb policy for weighted load balancing by pods-per-node.
	LocalityLBPolicyWeightedMaglev LocalityLBPolicyType = "WEIGHTED_MAGLEV"
	// LocalityLBPolicyMaglev is the locality lb policy when weighted load balancing by pods-per-node is disabled.
	LocalityLBPolicyMaglev LocalityLBPolicyType = "MAGLEV"
)

// Pool handles CRUD operations on a pool of GCE Backend Services.
type Pool struct {
	cloud                       *gce.Cloud
	namer                       namer.BackendNamer
	useConnectionTrackingPolicy bool
}

// NewPool returns a new backend pool.
// - cloud: implements BackendServices
// - namer: produces names for backends.
func NewPool(cloud *gce.Cloud, namer namer.BackendNamer) *Pool {
	return &Pool{
		cloud: cloud,
		namer: namer,
	}
}

// NewPoolWithConnectionTrackingPolicy returns a new backend pool.
// It is similar to NewPool() but has a field for ConnectionTrackingPolicy flag
// - cloud: implements BackendServices
// - namer: produces names for backends.
// - useConnectionTrackingPolicy: specifies the need in Connection Tracking Policy configuration
func NewPoolWithConnectionTrackingPolicy(cloud *gce.Cloud, namer namer.BackendNamer, useConnectionTrackingPolicy bool) *Pool {
	return &Pool{
		cloud:                       cloud,
		namer:                       namer,
		useConnectionTrackingPolicy: useConnectionTrackingPolicy,
	}
}

// L4BackendServiceParams encapsulates parameters for ensuring an L4 BackendService.
type L4BackendServiceParams struct {
	Name                     string
	HealthCheckLink          string
	Protocol                 string
	SessionAffinity          string
	Scheme                   string
	NamespacedName           types.NamespacedName
	NetworkInfo              *network.NetworkInfo
	ConnectionTrackingPolicy *composite.BackendServiceConnectionTrackingPolicy
	LocalityLbPolicy         LocalityLBPolicyType
	EnableZonalAffinity      bool
}

var versionPrecedence = map[meta.Version]int{
	meta.VersionAlpha: 2,
	meta.VersionBeta:  1,
	meta.VersionGA:    0,
}

// selectApiVersionForUpdate selects the appropriate API version for updating a backend service.
// It compares the current API version (potentially retrieved from the service's description) with the expected API version.
// The function returns the API version with the higher feature set to ensure no functionality is lost during the update.
func selectApiVersionForUpdate(current, expected meta.Version) meta.Version {
	precedenceCurrent, okCurrent := versionPrecedence[current]
	precedenceExpected, okExpected := versionPrecedence[expected]

	if !okCurrent {
		precedenceCurrent = 0
		current = meta.VersionGA
	}

	if !okExpected {
		precedenceExpected = 0
		expected = meta.VersionGA
	}

	if precedenceCurrent > precedenceExpected {
		return current
	}
	return expected
}

// ensureDescription updates the BackendService Description with the expected value
func ensureDescription(be *composite.BackendService, sp *utils.ServicePort) (needsUpdate bool) {
	desc := sp.GetDescription()
	features.SetDescription(&desc, sp)
	descString := desc.String()
	if be.Description == descString {
		return false
	}
	be.Description = descString
	return true
}

// Create a composite BackendService and returns it.
func (p *Pool) Create(sp utils.ServicePort, hcLink string, beLogger klog.Logger) (*composite.BackendService, error) {
	name := sp.BackendName()
	namedPort := &compute.NamedPort{
		Name: p.namer.NamedPort(sp.NodePort),
		Port: sp.NodePort,
	}

	version := features.VersionFromServicePort(&sp)
	be := &composite.BackendService{
		Version:      version,
		Name:         name,
		Protocol:     string(sp.Protocol),
		Port:         namedPort.Port,
		PortName:     namedPort.Name,
		HealthChecks: []string{hcLink},
		// LogConfig is using GA API so this is not considered for computing API version.
		LogConfig: &composite.BackendServiceLogConfig{
			Enable: true,
			// Sampling rate needs to be specified explicitly.
			SampleRate: 1.0,
		},
	}

	if sp.L7ILBEnabled {
		// This enables l7-ILB and advanced traffic management features
		be.LoadBalancingScheme = "INTERNAL_MANAGED"
	} else if sp.L7XLBRegionalEnabled {
		be.LoadBalancingScheme = "EXTERNAL_MANAGED"
	}

	ensureDescription(be, &sp)
	scope := features.ScopeFromServicePort(&sp)
	key, err := composite.CreateKey(p.cloud, name, scope)
	if err != nil {
		return nil, err
	}

	if err := composite.CreateBackendService(p.cloud, key, be, beLogger); err != nil {
		return nil, err
	}
	// Note: We need to perform a GCE call to re-fetch the object we just created
	// so that the "Fingerprint" field is filled in. This is needed to update the
	// object without error.
	return p.Get(name, version, scope, beLogger)
}

// Update a BackendService given the composite type.
func (p *Pool) Update(be *composite.BackendService, beLogger klog.Logger) error {
	// Ensure the backend service has the proper version before updating.
	be.Version = features.VersionFromDescription(be.Description)
	scope, err := composite.ScopeFromSelfLink(be.SelfLink)
	if err != nil {
		return err
	}

	key, err := composite.CreateKey(p.cloud, be.Name, scope)
	if err != nil {
		return err
	}
	if err := composite.UpdateBackendService(p.cloud, key, be, beLogger); err != nil {
		return err
	}
	return nil
}

// Get a composite BackendService given a required version.
func (p *Pool) Get(name string, version meta.Version, scope meta.KeyType, beLogger klog.Logger) (*composite.BackendService, error) {
	key, err := composite.CreateKey(p.cloud, name, scope)
	if err != nil {
		return nil, err
	}
	be, err := composite.GetBackendService(p.cloud, key, version, beLogger)
	if err != nil {
		return nil, err
	}
	// Evaluate the existing features from description to see if a lower
	// API version is required so that we don't lose information from
	// the existing backend service.
	versionRequired := features.VersionFromDescription(be.Description)

	if features.IsLowerVersion(versionRequired, version) {
		be, err = composite.GetBackendService(p.cloud, key, versionRequired, beLogger)
		if err != nil {
			return nil, err
		}
	}
	return be, nil
}

// Delete a BackendService given its name.
func (p *Pool) Delete(name string, version meta.Version, scope meta.KeyType, beLogger klog.Logger) error {
	beLogger.Info("Deleting backend service")

	key, err := composite.CreateKey(p.cloud, name, scope)
	if err != nil {
		return err
	}
	beLogger = beLogger.WithValues("backendKey", key)
	err = composite.DeleteBackendService(p.cloud, key, version, beLogger)
	if err != nil {
		if utils.IsHTTPErrorCode(err, http.StatusNotFound) || utils.IsInUsedByError(err) {
			// key also contains region information.
			beLogger.Info("DeleteBackendService(): ignorable error", "err", err)
			return nil
		}
		beLogger.Error(err, "DeleteBackendService()")
		return err
	}
	beLogger.Info("DeleteBackendService() ok")
	return nil
}

// Health checks the health of a BackendService given its name.
// Returns ("HEALTHY", nil) if healthy, otherwise ("Unknown", err)
func (p *Pool) Health(name string, version meta.Version, scope meta.KeyType, beLogger klog.Logger) (string, error) {
	be, err := p.Get(name, version, scope, beLogger)
	if err != nil {
		return "Unknown", fmt.Errorf("error getting backend service %s: %w", name, err)
	}
	if len(be.Backends) == 0 {
		return "Unknown", fmt.Errorf("no backends found for backend service %q", name)
	}

	// TODO: Include port, ip in the status, since it's in the health info.
	// TODO (shance) convert to composite types
	ret := "Unknown"
	for _, backend := range be.Backends {
		var hs *compute.BackendServiceGroupHealth
		switch scope {
		case meta.Global:
			hs, err = p.cloud.GetGlobalBackendServiceHealth(name, backend.Group)
		case meta.Regional:
			hs, err = p.cloud.GetRegionalBackendServiceHealth(name, p.cloud.Region(), backend.Group)
		default:
			return "Unknown", fmt.Errorf("invalid scope for Health(): %s", scope)
		}

		if err != nil {
			return "Unknown", fmt.Errorf("error getting health for backend %q: %w", name, err)
		}
		if len(hs.HealthStatus) == 0 || hs.HealthStatus[0] == nil {
			beLogger.Info("backend service does not have health status", "healthStatus", hs.HealthStatus)
			continue
		}

		for _, instanceStatus := range hs.HealthStatus {
			ret = instanceStatus.HealthState
			// return immediately with the value if we found at least one healthy instance
			if ret == "HEALTHY" {
				return ret, nil
			}
		}
	}
	return ret, nil
}

// List BackendService names that are managed by this pool.
func (p *Pool) List(key *meta.Key, version meta.Version, beLogger klog.Logger) ([]*composite.BackendService, error) {
	// TODO: for consistency with the rest of this sub-package this method
	// should return a list of backend ports.
	var backends []*composite.BackendService
	var err error

	backends, err = composite.ListBackendServices(p.cloud, key, version, beLogger, filter.None)
	if err != nil {
		return nil, err
	}

	var clusterBackends []*composite.BackendService

	for _, bs := range backends {
		if p.namer.NameBelongsToCluster(bs.Name) {
			scope, err := composite.ScopeFromSelfLink(bs.SelfLink)
			if err != nil {
				return nil, err
			}
			bs.Scope = scope

			clusterBackends = append(clusterBackends, bs)
		}
	}
	return clusterBackends, nil
}

// AddSignedURLKey adds a SignedUrlKey to a BackendService
func (p *Pool) AddSignedURLKey(be *composite.BackendService, signedurlkey *composite.SignedUrlKey, urlKeyLogger klog.Logger) error {
	urlKeyLogger.Info("Adding SignedUrlKey")

	scope, err := composite.ScopeFromSelfLink(be.SelfLink)
	if err != nil {
		return err
	}

	key, err := composite.CreateKey(p.cloud, be.Name, scope)
	if err != nil {
		return err
	}
	if err := composite.AddSignedUrlKey(p.cloud, key, be, signedurlkey, urlKeyLogger); err != nil {
		return err
	}
	return nil
}

// DeleteSignedURLKey deletes a SignedUrlKey from BackendService
func (p *Pool) DeleteSignedURLKey(be *composite.BackendService, keyName string, urlKeyLogger klog.Logger) error {
	urlKeyLogger.Info("Deleting SignedUrlKey")

	scope, err := composite.ScopeFromSelfLink(be.SelfLink)
	if err != nil {
		return err
	}

	key, err := composite.CreateKey(p.cloud, be.Name, scope)
	if err != nil {
		return err
	}
	if err := composite.DeleteSignedUrlKey(p.cloud, key, be, keyName, urlKeyLogger); err != nil {
		return err
	}
	return nil
}

// apiVersionRequiredbyServiceFeatures to create a backend service with the given params
func apiVersionRequiredbyServiceFeatures(params L4BackendServiceParams) meta.Version {
	if params.EnableZonalAffinity {
		return meta.VersionBeta
	}
	return meta.VersionGA
}

// EnsureL4BackendService creates or updates the backend service with the given name.
func (p *Pool) EnsureL4BackendService(params L4BackendServiceParams, beLogger klog.Logger) (*composite.BackendService, utils.ResourceSyncStatus, error) {
	start := time.Now()
	beLogger = beLogger.WithValues("L4BackendServiceParams", params)
	beLogger.V(2).Info("EnsureL4BackendService started")
	defer func() {
		beLogger.V(2).Info("EnsureL4BackendService finished", "timeTaken", time.Since(start))
	}()

	beLogger.V(2).Info("EnsureL4BackendService: checking existing backend service")

	key, err := composite.CreateKey(p.cloud, params.Name, meta.Regional)
	if err != nil {
		return nil, utils.ResourceResync, err
	}
	expectedVersion := apiVersionRequiredbyServiceFeatures(params)

	currentBS, err := composite.GetBackendService(p.cloud, key, expectedVersion, beLogger)
	if err != nil && !utils.IsNotFoundError(err) {
		return nil, utils.ResourceResync, err
	}

	expectedDesc, err := utils.MakeL4LBServiceDescription(params.NamespacedName.String(), "", expectedVersion, false, utils.ILB)
	if err != nil {
		beLogger.Info("EnsureL4BackendService: Failed to generate description for BackendService", "err", err)
	}

	expectedBS := &composite.BackendService{
		Name:                params.Name,
		Protocol:            params.Protocol,
		Version:             expectedVersion,
		Description:         expectedDesc,
		HealthChecks:        []string{params.HealthCheckLink},
		SessionAffinity:     utils.TranslateAffinityType(params.SessionAffinity, beLogger),
		LoadBalancingScheme: params.Scheme,
		LocalityLbPolicy:    string(params.LocalityLbPolicy),
	}

	if params.EnableZonalAffinity {
		beLogger.V(2).Info("EnsureL4BackendService: using Zonal Affinity", "spillover", ZonalAffinityEnabledSpillover, "spilloverRatio", DefaultZonalAffinitySpilloverRatio)
		expectedBS.NetworkPassThroughLbTrafficPolicy = zonalAffinityEnabledTrafficPolicy()
	} else {
		beLogger.V(2).Info("EnsureL4BackendService: not using Zonal Affinity", "spillover", ZonalAffinityDisabledSpillover, "spilloverRatio", DefaultZonalAffinitySpilloverRatio)
		expectedBS.NetworkPassThroughLbTrafficPolicy = zonalAffinityDisabledTrafficPolicy()
	}

	// We need this configuration only for Strong Session Affinity feature
	if p.useConnectionTrackingPolicy {
		beLogger.V(2).Info(fmt.Sprintf("EnsureL4BackendService: using connection tracking policy: %+v", params.ConnectionTrackingPolicy))
		expectedBS.ConnectionTrackingPolicy = params.ConnectionTrackingPolicy
	}
	if params.NetworkInfo != nil && !params.NetworkInfo.IsDefault {
		beLogger.V(2).Info("EnsureL4BackendService: using non-default network")
		expectedBS.Network = params.NetworkInfo.NetworkURL
	}
	if params.Protocol == string(api_v1.ProtocolTCP) {
		expectedBS.ConnectionDraining = &composite.ConnectionDraining{DrainingTimeoutSec: DefaultConnectionDrainingTimeoutSeconds}
	} else {
		// This config is not supported in UDP mode, explicitly set to 0 to reset, if proto was TCP previously.
		expectedBS.ConnectionDraining = &composite.ConnectionDraining{DrainingTimeoutSec: 0}
	}

	// Create backend service if none was found
	if currentBS == nil {
		beLogger.V(2).Info("EnsureL4BackendService: creating backend service")
		err := composite.CreateBackendService(p.cloud, key, expectedBS, beLogger)
		if err != nil {
			return nil, utils.ResourceResync, err
		}
		beLogger.V(2).Info("EnsureL4BackendService: created backend service successfully")
		// We need to perform a GCE call to re-fetch the object we just created
		// so that the "Fingerprint" field is filled in. This is needed to update the
		// object without error. The lookup is also needed to populate the selfLink.
		createdBS, err := composite.GetBackendService(p.cloud, key, expectedBS.Version, beLogger)
		return createdBS, utils.ResourceUpdate, err
	} else {
		// TODO(FelipeYepez) remove this check once LocalityLBPolicyMaglev does not require allow lisiting
		// Use LocalityLBPolicyMaglev instead of LocalityLBPolicyDefault if ILB already uses MAGLEV or WEIGHTEDMAGLEV
		if expectedBS.LocalityLbPolicy == string(LocalityLBPolicyDefault) &&
			(currentBS.LocalityLbPolicy == string(LocalityLBPolicyWeightedMaglev) || currentBS.LocalityLbPolicy == string(LocalityLBPolicyMaglev)) {

			expectedBS.LocalityLbPolicy = string(LocalityLBPolicyMaglev)
		}

		// Determine the appropriate API version to use for updating the backend service
		currentVersion := meta.VersionGA
		var currentDesc utils.L4LBResourceDescription
		err = currentDesc.Unmarshal(currentBS.Description)
		if err != nil {
			beLogger.V(0).Error(err, "EnsureL4BackendService: error unmarshaling backend service description")
		} else {
			currentVersion = currentDesc.APIVersion
		}
		expectedBS.Version = selectApiVersionForUpdate(currentVersion, expectedBS.Version)
	}

	if backendSvcEqual(expectedBS, currentBS, p.useConnectionTrackingPolicy) {
		beLogger.V(2).Info("EnsureL4BackendService: backend service did not change, skipping update")
		return currentBS, utils.ResourceResync, nil
	}
	if currentBS.ConnectionDraining != nil && currentBS.ConnectionDraining.DrainingTimeoutSec > 0 && params.Protocol == string(api_v1.ProtocolTCP) {
		// only preserves user overridden timeout value when the protocol is TCP
		expectedBS.ConnectionDraining.DrainingTimeoutSec = currentBS.ConnectionDraining.DrainingTimeoutSec
	}
	beLogger.V(2).Info("EnsureL4BackendService: updating backend service")
	// Set fingerprint for optimistic locking
	expectedBS.Fingerprint = currentBS.Fingerprint
	// Copy backends to avoid detaching them during update. This could be replaced with a patch call in the future.
	expectedBS.Backends = currentBS.Backends
	if err := composite.UpdateBackendService(p.cloud, key, expectedBS, beLogger); err != nil {
		return nil, utils.ResourceUpdate, err
	}
	beLogger.V(2).Info("EnsureL4BackendService: updated backend service successfully")

	updatedBS, err := composite.GetBackendService(p.cloud, key, expectedBS.Version, beLogger)
	return updatedBS, utils.ResourceUpdate, err
}

// backendSvcEqual returns true if the 2 BackendService objects are equal.
// ConnectionDraining timeout is not checked for equality, if user changes
// this timeout and no other backendService parameters change, the backend
// service will not be updated. The list of backends is not checked either,
// since that is handled by the neg-linker.
// The list of backends is not checked, since that is handled by the neg-linker.
func backendSvcEqual(newBS, oldBS *composite.BackendService, compareConnectionTracking bool) bool {

	svcsEqual := newBS.Protocol == oldBS.Protocol &&
		newBS.Description == oldBS.Description &&
		newBS.SessionAffinity == oldBS.SessionAffinity &&
		newBS.LoadBalancingScheme == oldBS.LoadBalancingScheme &&
		newBS.Network == oldBS.Network

	if flags.F.EnableL4ILBZonalAffinity {
		// Compare healthChecks sets ignoring api version
		svcsEqual = svcsEqual && healthChecksEqual(newBS.HealthChecks, oldBS.HealthChecks)
	} else {
		svcsEqual = svcsEqual && utils.EqualStringSets(newBS.HealthChecks, oldBS.HealthChecks)
	}

	// Compare only for backendSvc that uses Strong Session Affinity feature
	if compareConnectionTracking {
		svcsEqual = svcsEqual && connectionTrackingPolicyEqual(newBS.ConnectionTrackingPolicy, oldBS.ConnectionTrackingPolicy)
	}

	// If the locality lb policy is not set for existing services, no need to update to MAGLEV since it is the default now.
	svcsEqual = svcsEqual &&
		(newBS.LocalityLbPolicy == oldBS.LocalityLbPolicy ||
			(newBS.LocalityLbPolicy == string(LocalityLBPolicyDefault) && oldBS.LocalityLbPolicy == string(LocalityLBPolicyMaglev)) ||
			(newBS.LocalityLbPolicy == string(LocalityLBPolicyMaglev) && oldBS.LocalityLbPolicy == string(LocalityLBPolicyDefault)))

	// If zonal affinity is set, needs to be equal
	svcsEqual = svcsEqual && zonalAffinityEqual(newBS, oldBS)
	return svcsEqual
}

// removeAPIVersionFromHealthChecks converts a slice of full health check URLs
// into a slice of their URL without the API version
func removeAPIVersionFromHealthChecks(hcLinks []string) []string {
	hcResourcePaths := make([]string, 0, len(hcLinks))
	for _, hcLink := range hcLinks {
		resourcePath := utils.FilterAPIversionFromResourcePath(hcLink)
		hcResourcePaths = append(hcResourcePaths, resourcePath)
	}
	return hcResourcePaths
}

func convertNetworkLbTrafficPolicyToZonalAffinity(trafficPolicy *composite.BackendServiceNetworkPassThroughLbTrafficPolicy) composite.BackendServiceNetworkPassThroughLbTrafficPolicyZonalAffinity {
	if trafficPolicy == nil || trafficPolicy.ZonalAffinity == nil {
		return *zonalAffinityDisabledTrafficPolicy().ZonalAffinity
	}

	return *trafficPolicy.ZonalAffinity
}

// healthCheckEqual compare healthcheck URL ignoring the API version used
func healthChecksEqual(hcLinksA, hcLinksB []string) bool {
	healthChecksA := removeAPIVersionFromHealthChecks(hcLinksA)
	healthChecksB := removeAPIVersionFromHealthChecks(hcLinksB)

	return utils.EqualStringSets(healthChecksA, healthChecksB)
}

func zonalAffinityEqual(a, b *composite.BackendService) bool {
	aZonalAffinity := convertNetworkLbTrafficPolicyToZonalAffinity(a.NetworkPassThroughLbTrafficPolicy)
	bZonalAffinity := convertNetworkLbTrafficPolicyToZonalAffinity(b.NetworkPassThroughLbTrafficPolicy)

	spilloverEqual := aZonalAffinity.Spillover == bZonalAffinity.Spillover
	spilloverRatioEqual := aZonalAffinity.SpilloverRatio == bZonalAffinity.SpilloverRatio

	return spilloverEqual && spilloverRatioEqual
}

// connectionTrackingPolicyEqual returns true if both elements are equal
// and return false if at least one parameter is different
func connectionTrackingPolicyEqual(a, b *composite.BackendServiceConnectionTrackingPolicy) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.TrackingMode == b.TrackingMode &&
		a.EnableStrongAffinity == b.EnableStrongAffinity &&
		a.IdleTimeoutSec == b.IdleTimeoutSec
}

func zonalAffinityEnabledTrafficPolicy() *composite.BackendServiceNetworkPassThroughLbTrafficPolicy {
	return &composite.BackendServiceNetworkPassThroughLbTrafficPolicy{
		ZonalAffinity: &composite.BackendServiceNetworkPassThroughLbTrafficPolicyZonalAffinity{
			Spillover:      ZonalAffinityEnabledSpillover,
			SpilloverRatio: DefaultZonalAffinitySpilloverRatio,
		},
	}
}

func zonalAffinityDisabledTrafficPolicy() *composite.BackendServiceNetworkPassThroughLbTrafficPolicy {
	return &composite.BackendServiceNetworkPassThroughLbTrafficPolicy{
		ZonalAffinity: &composite.BackendServiceNetworkPassThroughLbTrafficPolicyZonalAffinity{
			Spillover:      ZonalAffinityDisabledSpillover,
			SpilloverRatio: DefaultZonalAffinitySpilloverRatio,
		},
	}
}
