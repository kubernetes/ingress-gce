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

package composite

import (
	"encoding/json"
	"fmt"

	"k8s.io/klog"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
)

// TODO(rramkumar): All code in this file should ideally be generated.

func CreateBackendService(be *BackendService, cloud *gce.Cloud) error {
	switch be.Version {
	case meta.VersionAlpha:
		alpha, err := be.toAlpha()
		if err != nil {
			return err
		}
		klog.V(3).Infof("Creating alpha backend service %v", alpha.Name)
		return cloud.CreateAlphaGlobalBackendService(alpha)
	case meta.VersionBeta:
		beta, err := be.toBeta()
		if err != nil {
			return err
		}
		klog.V(3).Infof("Creating beta backend service %v", beta.Name)
		return cloud.CreateBetaGlobalBackendService(beta)
	default:
		ga, err := be.toGA()
		if err != nil {
			return err
		}
		klog.V(3).Infof("Creating ga backend service %v", ga.Name)
		return cloud.CreateGlobalBackendService(ga)
	}
}

func UpdateBackendService(be *BackendService, cloud *gce.Cloud) error {
	switch be.Version {
	case meta.VersionAlpha:
		alpha, err := be.toAlpha()
		if err != nil {
			return err
		}
		klog.V(3).Infof("Updating alpha backend service %v", alpha.Name)
		return cloud.UpdateAlphaGlobalBackendService(alpha)
	case meta.VersionBeta:
		beta, err := be.toBeta()
		if err != nil {
			return err
		}
		klog.V(3).Infof("Updating beta backend service %v", beta.Name)
		return cloud.UpdateBetaGlobalBackendService(beta)
	default:
		ga, err := be.toGA()
		if err != nil {
			return err
		}
		klog.V(3).Infof("Updating ga backend service %v", ga.Name)
		return cloud.UpdateGlobalBackendService(ga)
	}
}

func GetBackendService(name string, version meta.Version, cloud *gce.Cloud) (*BackendService, error) {
	var gceObj interface{}
	var err error
	switch version {
	case meta.VersionAlpha:
		gceObj, err = cloud.GetAlphaGlobalBackendService(name)
	case meta.VersionBeta:
		gceObj, err = cloud.GetBetaGlobalBackendService(name)
	default:
		gceObj, err = cloud.GetGlobalBackendService(name)
	}
	if err != nil {
		return nil, err
	}
	return toBackendService(gceObj)
}

// toBackendService converts a compute alpha, beta or GA
// BackendService into our composite type.
func toBackendService(obj interface{}) (*BackendService, error) {
	be := &BackendService{}
	bytes, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("could not marshal object %+v to JSON: %v", obj, err)
	}
	err = json.Unmarshal(bytes, be)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling to BackendService: %v", err)
	}
	return be, nil
}

// BackendService is a composite type which embeds the
// structure of all the compute alpha, beta and GA Backend Service.
type BackendService struct {
	// Version keeps track of the intended compute version for this BackendService.
	// Note that the compute API's do not contain this field. It is for our
	// own bookkeeping purposes.
	Version meta.Version `json:"-"`

	AffinityCookieTtlSec     int64                               `json:"affinityCookieTtlSec,omitempty"`
	AppEngineBackend         *BackendServiceAppEngineBackend     `json:"appEngineBackend,omitempty"`
	Backends                 []*Backend                          `json:"backends,omitempty"`
	CdnPolicy                *BackendServiceCdnPolicy            `json:"cdnPolicy,omitempty"`
	CloudFunctionBackend     *BackendServiceCloudFunctionBackend `json:"cloudFunctionBackend,omitempty"`
	ConnectionDraining       *ConnectionDraining                 `json:"connectionDraining,omitempty"`
	CreationTimestamp        string                              `json:"creationTimestamp,omitempty"`
	CustomRequestHeaders     []string                            `json:"customRequestHeaders,omitempty"`
	Description              string                              `json:"description,omitempty"`
	EnableCDN                bool                                `json:"enableCDN,omitempty"`
	FailoverPolicy           *BackendServiceFailoverPolicy       `json:"failoverPolicy,omitempty"`
	Fingerprint              string                              `json:"fingerprint,omitempty"`
	HealthChecks             []string                            `json:"healthChecks,omitempty"`
	Iap                      *BackendServiceIAP                  `json:"iap,omitempty"`
	Id                       uint64                              `json:"id,omitempty,string"`
	Kind                     string                              `json:"kind,omitempty"`
	LoadBalancingScheme      string                              `json:"loadBalancingScheme,omitempty"`
	Name                     string                              `json:"name,omitempty"`
	Port                     int64                               `json:"port,omitempty"`
	PortName                 string                              `json:"portName,omitempty"`
	Protocol                 string                              `json:"protocol,omitempty"`
	Region                   string                              `json:"region,omitempty"`
	SecurityPolicy           string                              `json:"securityPolicy,omitempty"`
	SelfLink                 string                              `json:"selfLink,omitempty"`
	SessionAffinity          string                              `json:"sessionAffinity,omitempty"`
	TimeoutSec               int64                               `json:"timeoutSec,omitempty"`
	googleapi.ServerResponse `json:"-"`
	ForceSendFields          []string `json:"-"`
	NullFields               []string `json:"-"`
}

type Backend struct {
	BalancingMode             string   `json:"balancingMode,omitempty"`
	CapacityScaler            float64  `json:"capacityScaler,omitempty"`
	Description               string   `json:"description,omitempty"`
	Failover                  bool     `json:"failover,omitempty"`
	Group                     string   `json:"group,omitempty"`
	MaxConnections            int64    `json:"maxConnections,omitempty"`
	MaxConnectionsPerEndpoint int64    `json:"maxConnectionsPerEndpoint,omitempty"`
	MaxConnectionsPerInstance int64    `json:"maxConnectionsPerInstance,omitempty"`
	MaxRate                   int64    `json:"maxRate,omitempty"`
	MaxRatePerEndpoint        float64  `json:"maxRatePerEndpoint,omitempty"`
	MaxRatePerInstance        float64  `json:"maxRatePerInstance,omitempty"`
	MaxUtilization            float64  `json:"maxUtilization,omitempty"`
	ForceSendFields           []string `json:"-"`
	NullFields                []string `json:"-"`
}

type BackendServiceIAP struct {
	Enabled                  bool                               `json:"enabled,omitempty"`
	Oauth2ClientId           string                             `json:"oauth2ClientId,omitempty"`
	Oauth2ClientSecret       string                             `json:"oauth2ClientSecret,omitempty"`
	Oauth2ClientSecretSha256 string                             `json:"oauth2ClientSecretSha256,omitempty"`
	ForceSendFields          []string                           `json:"-"`
	NullFields               []string                           `json:"-"`
	Oauth2ClientInfo         *BackendServiceIAPOAuth2ClientInfo `json:"oauth2ClientInfo,omitempty"`
}

type BackendServiceIAPOAuth2ClientInfo struct {
	ApplicationName       string   `json:"applicationName,omitempty"`
	ClientName            string   `json:"clientName,omitempty"`
	DeveloperEmailAddress string   `json:"developerEmailAddress,omitempty"`
	ForceSendFields       []string `json:"-"`
	NullFields            []string `json:"-"`
}

type BackendServiceCdnPolicy struct {
	CacheKeyPolicy          *CacheKeyPolicy `json:"cacheKeyPolicy,omitempty"`
	SignedUrlCacheMaxAgeSec int64           `json:"signedUrlCacheMaxAgeSec,omitempty,string"`
	SignedUrlKeyNames       []string        `json:"signedUrlKeyNames,omitempty"`
	ForceSendFields         []string        `json:"-"`
	NullFields              []string        `json:"-"`
}

type CacheKeyPolicy struct {
	IncludeHost          bool     `json:"includeHost,omitempty"`
	IncludeProtocol      bool     `json:"includeProtocol,omitempty"`
	IncludeQueryString   bool     `json:"includeQueryString,omitempty"`
	QueryStringBlacklist []string `json:"queryStringBlacklist,omitempty"`
	QueryStringWhitelist []string `json:"queryStringWhitelist,omitempty"`
	ForceSendFields      []string `json:"-"`
	NullFields           []string `json:"-"`
}

type BackendServiceFailoverPolicy struct {
	DisableConnectionDrainOnFailover bool     `json:"disableConnectionDrainOnFailover,omitempty"`
	DropTrafficIfUnhealthy           bool     `json:"dropTrafficIfUnhealthy,omitempty"`
	FailoverRatio                    float64  `json:"failoverRatio,omitempty"`
	ForceSendFields                  []string `json:"-"`
	NullFields                       []string `json:"-"`
}

type BackendServiceCloudFunctionBackend struct {
	FunctionName    string   `json:"functionName,omitempty"`
	TargetProject   string   `json:"targetProject,omitempty"`
	ForceSendFields []string `json:"-"`
	NullFields      []string `json:"-"`
}

type ConnectionDraining struct {
	DrainingTimeoutSec int64    `json:"drainingTimeoutSec,omitempty"`
	ForceSendFields    []string `json:"-"`
	NullFields         []string `json:"-"`
}

type BackendServiceAppEngineBackend struct {
	AppEngineService string   `json:"appEngineService,omitempty"`
	TargetProject    string   `json:"targetProject,omitempty"`
	Version          string   `json:"version,omitempty"`
	ForceSendFields  []string `json:"-"`
	NullFields       []string `json:"-"`
}

// toAlpha converts our composite type into an alpha type.
// This alpha type can be used in GCE API calls.
func (be *BackendService) toAlpha() (*computealpha.BackendService, error) {
	bytes, err := json.Marshal(be)
	if err != nil {
		return nil, fmt.Errorf("error marshalling BackendService to JSON: %v", err)
	}
	alpha := &computealpha.BackendService{}
	err = json.Unmarshal(bytes, alpha)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling BackendService JSON to compute alpha type: %v", err)
	}
	// Set force send fields. This is a temporary hack.
	if alpha.CdnPolicy != nil && alpha.CdnPolicy.CacheKeyPolicy != nil {
		alpha.CdnPolicy.CacheKeyPolicy.ForceSendFields = []string{"IncludeHost", "IncludeProtocol", "IncludeQueryString", "QueryStringBlacklist", "QueryStringWhitelist"}
	}
	if alpha.Iap != nil {
		alpha.Iap.ForceSendFields = []string{"Enabled", "Oauth2ClientId", "Oauth2ClientSecret"}
	}
	return alpha, nil
}

// toBeta converts our composite type into an beta type.
// This beta type can be used in GCE API calls.
func (be *BackendService) toBeta() (*computebeta.BackendService, error) {
	bytes, err := json.Marshal(be)
	if err != nil {
		return nil, fmt.Errorf("error marshalling BackendService to JSON: %v", err)
	}
	beta := &computebeta.BackendService{}
	err = json.Unmarshal(bytes, beta)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling BackendService JSON to compute beta type: %v", err)
	}
	// Set force send fields. This is a temporary hack.
	if beta.CdnPolicy != nil && beta.CdnPolicy.CacheKeyPolicy != nil {
		beta.CdnPolicy.CacheKeyPolicy.ForceSendFields = []string{"IncludeHost", "IncludeProtocol", "IncludeQueryString", "QueryStringBlacklist", "QueryStringWhitelist"}
	}
	if beta.Iap != nil {
		beta.Iap.ForceSendFields = []string{"Enabled", "Oauth2ClientId", "Oauth2ClientSecret"}
	}
	return beta, nil
}

// toGA converts our composite type into a GA type.
// This GA type can be used in GCE API calls.
func (be *BackendService) toGA() (*compute.BackendService, error) {
	bytes, err := json.Marshal(be)
	if err != nil {
		return nil, fmt.Errorf("error marshalling BackendService to JSON: %v", err)
	}
	ga := &compute.BackendService{}
	err = json.Unmarshal(bytes, ga)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling BackendService JSON to compute GA type: %v", err)
	}
	// Set force send fields. This is a temporary hack.
	if ga.CdnPolicy != nil && ga.CdnPolicy.CacheKeyPolicy != nil {
		ga.CdnPolicy.CacheKeyPolicy.ForceSendFields = []string{"IncludeHost", "IncludeProtocol", "IncludeQueryString", "QueryStringBlacklist", "QueryStringWhitelist"}
	}
	if ga.Iap != nil {
		ga.Iap.ForceSendFields = []string{"Enabled", "Oauth2ClientId", "Oauth2ClientSecret"}
	}
	return ga, nil
}
