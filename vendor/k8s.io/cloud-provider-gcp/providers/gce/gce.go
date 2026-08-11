//go:build !providerless
// +build !providerless

/*
Copyright 2014 The Kubernetes Authors.

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

package gce

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	gcfg "gopkg.in/gcfg.v1"

	"cloud.google.com/go/compute/metadata"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	compute "google.golang.org/api/compute/v1"
	container "google.golang.org/api/container/v1"
	"google.golang.org/api/option"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/pkg/version"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/flowcontrol"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
)

const (
	// ProviderName is the official const representation of the Google Cloud Provider
	ProviderName = "gce"

	k8sNodeRouteTag = "k8s-node-route"

	// AffinityTypeNone - no session affinity.
	gceAffinityTypeNone = "NONE"
	// AffinityTypeClientIP - affinity based on Client IP.
	gceAffinityTypeClientIP = "CLIENT_IP"

	operationPollInterval           = time.Second
	maxTargetPoolCreateInstances    = 200
	maxInstancesPerTargetPoolUpdate = 1000

	// HTTP Load Balancer parameters
	// Configure 8 second period for external health checks.
	gceHcCheckIntervalSeconds = int64(8)
	gceHcTimeoutSeconds       = int64(1)
	// Start sending requests as soon as a pod is found on the node.
	gceHcHealthyThreshold = int64(1)
	// Defaults to 3 * 8 = 24 seconds before the LB will steer traffic away.
	gceHcUnhealthyThreshold = int64(3)

	gceComputeAPIEndpoint     = "https://www.googleapis.com/compute/v1/"
	gceComputeAPIEndpointBeta = "https://www.googleapis.com/compute/beta/"
)

var (
	_ cloudprovider.Interface    = (*Cloud)(nil)
	_ cloudprovider.Instances    = (*Cloud)(nil)
	_ cloudprovider.LoadBalancer = (*Cloud)(nil)
	_ cloudprovider.Routes       = (*Cloud)(nil)
	_ cloudprovider.Zones        = (*Cloud)(nil)
	_ cloudprovider.PVLabeler    = (*Cloud)(nil)
	_ cloudprovider.Clusters     = (*Cloud)(nil)
)

type StackType string

// NetworkStackDualStack is deprecated, use `clusterStackDualStack` instead.
const NetworkStackDualStack StackType = "IPV4_IPV6"

// NetworkStackIPV4 is deprecated, use `clusterStackIPV4` instead.
const NetworkStackIPV4 StackType = "IPV4"

// NetworkStackIPV6 is deprecated, use `clusterStackIPV6` instead.
const NetworkStackIPV6 StackType = "IPV6"

// clusterStackDualStack represents a dual stack cluster, i. e. Pods and Services are addressable with both
// IPv4 and IPv6 addresses. The underlying VPC's stack type must be dual as well.
const clusterStackDualStack StackType = "IPV4_IPV6"

// clusterStackIPV4 represents a cluster in which Pods and Services are addressable with IPv4 addresses.
// The underlying VPC's stack type could be either IPV4 or dual stack IPV4_IPV6.
const clusterStackIPV4 StackType = "IPV4"

// clusterStackIPV6 represents a cluster in which Pods and Services are addressable with IPv6 addresses.
// The underlying VPC's stack type could be either IPV6 or dual stack IPV4_IPV6.
const clusterStackIPV6 StackType = "IPV6"

// Cloud is an implementation of Interface, LoadBalancer and Instances for Google Compute Engine.
type Cloud struct {
	// ClusterID contains functionality for getting (and initializing) the ingress-uid. Call Cloud.Initialize()
	// for the cloudprovider to start watching the configmap.
	ClusterID ClusterID

	// initializer is used for lazy initialization of subnetworkURL
	// and isLegacyNetwork fields if they are not passed via the config.
	// The reason is to avoid GCE API calls to initialize them if they
	// will never be used. This is especially important when
	// it is run from Kubelets, as there can be thousands  of them.
	subnetworkURLAndIsLegacyNetworkInitializer sync.Once

	service          *compute.Service
	serviceBeta      *computebeta.Service
	serviceAlpha     *computealpha.Service
	containerService *container.Service
	tpuService       *tpuService
	client           clientset.Interface
	clientBuilder    cloudprovider.ControllerClientBuilder
	eventBroadcaster record.EventBroadcaster
	eventRecorder    record.EventRecorder
	projectID        string
	region           string
	regional         bool
	dynamicZones     bool
	localZone        string // The zone in which we are running
	// Lock for access to managedZones
	managedZonesLock sync.RWMutex
	// managedZones represents GCE zones CCM can manage (dynamically
	// refreshed). Used to scan GCE for project resources to avoid
	// bootstrap (finding first node) and scale-to-zero (cleaning
	// volumes in empty zones) issues. For scheduling/placement,
	// use GetAllCurrentZones() (active nodes) instead.
	managedZones []string
	networkURL   string
	// unsafeIsLegacyNetwork should be used only via IsLegacyNetwork() accessor,
	// to ensure it was properly initialized.
	unsafeIsLegacyNetwork bool
	// unsafeSubnetworkURL should be used only via SubnetworkURL() accessor,
	// to ensure it was properly initialized.
	unsafeSubnetworkURL string
	// DEPRECATED: Do not rely on this value as it may be incorrect.
	secondaryRangeName       string
	networkProjectID         string
	onXPN                    bool
	nodeTags                 []string    // List of tags to use on firewall rules for load balancers
	lastComputedNodeTags     []string    // List of node tags calculated in GetHostTags()
	lastKnownNodeNames       sets.String // List of hostnames used to calculate lastComputedHostTags in GetHostTags(names)
	computeNodeTagLock       sync.Mutex  // Lock for computing and setting node tags
	nodeInstancePrefix       string      // If non-"", an advisory prefix for all nodes in the cluster
	useMetadataServer        bool
	operationPollRateLimiter flowcontrol.RateLimiter
	manager                  diskServiceManager
	// Lock for access to nodeZones
	nodeZonesLock sync.Mutex
	// nodeZones maps GCE zones to active K8s Node names, dynamically
	// updated by nodeInformer. Used via GetAllCurrentZones() for
	// scheduling and topology-aware volume placement to ensure
	// resources are only created in zones with active node capacity.
	nodeZones          map[string]sets.String
	nodeInformerSynced cache.InformerSynced

	// sharedResourceLock is used to serialize GCE operations that may mutate shared state to
	// prevent inconsistencies. For example, load balancers manipulation methods will take the
	// lock to prevent shared resources from being prematurely deleted while the operation is
	// in progress.
	sharedResourceLock sync.Mutex
	// sharedResourceLocks is a concurrent map used for resource-specific fine-grained locking of shared resources (e.g. InstanceGroups, shared HealthChecks).
	sharedResourceLocks sync.Map // map[string]*sync.Mutex
	// AlphaFeatureGate gates gce alpha features in Cloud instance.
	// Related wrapper functions that interacts with gce alpha api should examine whether
	// the corresponding api is enabled.
	// If not enabled, it should return error.
	AlphaFeatureGate *AlphaFeatureGate

	// New code generated interface to the GCE compute library.
	c cloud.Cloud

	// Keep a reference of this around so we can inject a new cloud.RateLimiter implementation.
	s *cloud.Service

	metricsCollector loadbalancerMetricsCollector

	// the compute API endpoint with the `projects/` element.
	projectsBasePath string
	// stackType indicates whether the cluster is a single stack IPv4, single
	// stack IPv6 or a dual stack cluster
	stackType StackType

	// projectFromNodeProviderID determines whether the project derived through
	// the Node's .spec.providerID can be used to change the project used when
	// making GCE API calls.
	//
	// Enable this ony when the Node's .spec.providerID can be fully trusted.
	projectFromNodeProviderID bool

	// enableRBSDefaultForL4NetLB disable Service controller from picking up services by default
	enableRBSDefaultForL4NetLB bool

	// enableL4LBAnnotations enable annotations related to provisioned resources in GCE
	enableL4LBAnnotations bool

	// enableL4DenyFirewallRule creates an additional deny firewall rule at priority 1000
	// and moves the allow rule to priority 999 to improve security posture.
	enableL4DenyFirewallRule bool

	// enableL4DenyFirewallRollbackCleanup
	enableL4DenyFirewallRollbackCleanup bool

	// enableL4ILBFineGrainedLocks enables fine-grained resource-specific locking
	enableL4ILBFineGrainedLocks bool
}

type SharedResourceType string

const (
	ResourceTypeHealthCheck   SharedResourceType = "hc"
	ResourceTypeInstanceGroup SharedResourceType = "ig"
	ResourceTypeFirewall      SharedResourceType = "fw"
)

func (g *Cloud) getLockForResource(resType SharedResourceType, name string) *sync.Mutex {
	key := string(resType) + ":" + name
	if v, ok := g.sharedResourceLocks.Load(key); ok {
		return v.(*sync.Mutex)
	}
	v, _ := g.sharedResourceLocks.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// lockSharedResourcesIfCoarse acquires the global sharedResourceLock when fine-grained
// locking is disabled, preserving the legacy coarse locking behavior.
// It returns a function to defer for unlocking.
func (g *Cloud) lockSharedResourcesIfCoarse() func() {
	if g.enableL4ILBFineGrainedLocks {
		return func() { /* no-op */ }
	}
	g.sharedResourceLock.Lock()
	return g.sharedResourceLock.Unlock
}

// lockResourceIfShared is a helper function for acquiring locks on shared resources.
// If fine-grained locking is disabled or the resource is not shared, it does nothing.
func (g *Cloud) lockResourceIfShared(shared bool, resType SharedResourceType, name string) func() {
	if !g.enableL4ILBFineGrainedLocks || !shared {
		return func() { /* no-op */ }
	}
	lock := g.getLockForResource(resType, name)
	lock.Lock()
	return lock.Unlock
}

// lockInstanceGroup locks the shared unmanaged instance group in the specified zone.
// Since instance groups are always shared across the cluster, this locks unconditionally.
// It returns a function to defer for unlocking.
func (g *Cloud) lockInstanceGroup(igName, zone string) func() {
	return g.lockResourceIfShared(true, ResourceTypeInstanceGroup, igName+"-"+zone)
}

// lockHealthCheck locks a health check resource by name.
// It returns a function to defer for unlocking.
func (g *Cloud) lockHealthCheck(hcName string, shared bool) func() {
	return g.lockResourceIfShared(shared, ResourceTypeHealthCheck, hcName)
}

// lockFirewall locks a firewall resource by name.
// It returns a function to defer for unlocking.
func (g *Cloud) lockFirewall(fwName string, shared bool) func() {
	return g.lockResourceIfShared(shared, ResourceTypeFirewall, fwName)
}

// ConfigGlobal is the in memory representation of the gce.conf config data
// TODO: replace gcfg with json
type ConfigGlobal struct {
	TokenURL  string `gcfg:"token-url"`
	TokenBody string `gcfg:"token-body" datapolicy:"token"`
	// ProjectID and NetworkProjectID can either be the numeric or string-based
	// unique identifier that starts with [a-z].
	ProjectID string `gcfg:"project-id"`
	// NetworkProjectID refers to the project which owns the network being used.
	NetworkProjectID string `gcfg:"network-project-id"`
	NetworkName      string `gcfg:"network-name"`
	SubnetworkName   string `gcfg:"subnetwork-name"`
	StackType        string `gcfg:"stack-type"`
	// DEPRECATED: Do not rely on this value as it may be incorrect.
	// SecondaryRangeName is the name of the secondary range to allocate IP
	// aliases. The secondary range must be present on the subnetwork the
	// cluster is attached to.
	SecondaryRangeName string   `gcfg:"secondary-range-name"`
	NodeTags           []string `gcfg:"node-tags"`
	NodeInstancePrefix string   `gcfg:"node-instance-prefix"`
	Regional           bool     `gcfg:"regional"`
	Multizone          bool     `gcfg:"multizone"`
	// APIEndpoint is the GCE compute API endpoint to use. If this is blank,
	// then the default endpoint is used.
	APIEndpoint string `gcfg:"api-endpoint"`
	// ContainerAPIEndpoint is the GCE container API endpoint to use. If this is blank,
	// then the default endpoint is used.
	ContainerAPIEndpoint string `gcfg:"container-api-endpoint"`
	// LocalZone specifies the GCE zone that gce cloud client instance is
	// located in (i.e. where the controller will be running). If this is
	// blank, then the local zone will be discovered via the metadata server.
	LocalZone string `gcfg:"local-zone"`
	// Default to none.
	// For example: MyFeatureFlag
	AlphaFeatures []string `gcfg:"alpha-features"`
}

// ConfigFile is the struct used to parse the /etc/gce.conf configuration file.
// NOTE: Cloud config files should follow the same Kubernetes deprecation policy as
// flags or CLIs. Config fields should not change behavior in incompatible ways and
// should be deprecated for at least 2 release prior to removing.
// See https://kubernetes.io/docs/reference/using-api/deprecation-policy/#deprecating-a-flag-or-cli
// for more details.
type ConfigFile struct {
	Global ConfigGlobal `gcfg:"global"`
}

// CloudConfig includes all the necessary configuration for creating Cloud
type CloudConfig struct {
	APIEndpoint          string
	ContainerAPIEndpoint string
	ProjectID            string
	NetworkProjectID     string
	Region               string
	Regional             bool
	Zone                 string
	ManagedZones         []string
	NetworkName          string
	NetworkURL           string
	SubnetworkName       string
	SubnetworkURL        string
	// DEPRECATED: Do not rely on this value as it may be incorrect.
	SecondaryRangeName string
	NodeTags           []string
	NodeInstancePrefix string
	TokenSource        oauth2.TokenSource
	UseMetadataServer  bool
	AlphaFeatureGate   *AlphaFeatureGate
	StackType          string
}

func init() {
	cloudprovider.RegisterCloudProvider(
		ProviderName,
		func(config io.Reader) (cloudprovider.Interface, error) {
			return newGCECloud(config)
		})
}

// Services is the set of all versions of the compute service.
type Services struct {
	// GA, Alpha, Beta versions of the compute API.
	GA    *compute.Service
	Alpha *computealpha.Service
	Beta  *computebeta.Service
}

// ComputeServices returns access to the internal compute services.
func (g *Cloud) ComputeServices() *Services {
	return &Services{g.service, g.serviceAlpha, g.serviceBeta}
}

// Compute returns the generated stubs for the compute API.
func (g *Cloud) Compute() cloud.Cloud {
	return g.c
}

// ContainerService returns the container service.
func (g *Cloud) ContainerService() *container.Service {
	return g.containerService
}

// newGCECloud creates a new instance of Cloud.
func newGCECloud(config io.Reader) (gceCloud *Cloud, err error) {
	var cloudConfig *CloudConfig
	var configFile *ConfigFile

	if config != nil {
		configFile, err = readConfig(config)
		if err != nil {
			return nil, err
		}
		klog.Infof("Using GCE provider config %+v", configFile)
	}

	cloudConfig, err = GenerateCloudConfig(configFile)
	if err != nil {
		return nil, err
	}

	return CreateGCECloud(cloudConfig)
}

func readConfig(reader io.Reader) (*ConfigFile, error) {
	cfg := &ConfigFile{}
	if err := gcfg.FatalOnly(gcfg.ReadInto(cfg, reader)); err != nil {
		klog.Errorf("Couldn't read config: %v", err)
		return nil, err
	}
	return cfg, nil
}

func GenerateCloudConfig(configFile *ConfigFile) (cloudConfig *CloudConfig, err error) {
	cloudConfig = &CloudConfig{}
	// By default, fetch token from GCE metadata server
	cloudConfig.TokenSource = google.ComputeTokenSource("")
	cloudConfig.UseMetadataServer = true
	cloudConfig.AlphaFeatureGate = NewAlphaFeatureGate([]string{})
	if configFile != nil {
		if configFile.Global.APIEndpoint != "" {
			cloudConfig.APIEndpoint = configFile.Global.APIEndpoint
		}

		if configFile.Global.ContainerAPIEndpoint != "" {
			cloudConfig.ContainerAPIEndpoint = configFile.Global.ContainerAPIEndpoint
		}

		if configFile.Global.TokenURL != "" {
			// if tokenURL is nil, set tokenSource to nil. This will force the OAuth client to fall
			// back to use DefaultTokenSource. This allows running gceCloud remotely.
			if configFile.Global.TokenURL == "nil" {
				cloudConfig.TokenSource = nil
			} else {
				cloudConfig.TokenSource = NewAltTokenSource(configFile.Global.TokenURL, configFile.Global.TokenBody)
			}
		}

		cloudConfig.NodeTags = configFile.Global.NodeTags
		cloudConfig.NodeInstancePrefix = configFile.Global.NodeInstancePrefix
		cloudConfig.AlphaFeatureGate = NewAlphaFeatureGate(configFile.Global.AlphaFeatures)
	}

	// retrieve projectID and zone
	if configFile == nil || configFile.Global.ProjectID == "" || configFile.Global.LocalZone == "" {
		cloudConfig.ProjectID, cloudConfig.Zone, err = getProjectAndZone()
		if err != nil {
			return nil, err
		}
	}

	if configFile != nil {
		if configFile.Global.ProjectID != "" {
			cloudConfig.ProjectID = configFile.Global.ProjectID
		}
		if configFile.Global.LocalZone != "" {
			cloudConfig.Zone = configFile.Global.LocalZone
		}
		if configFile.Global.NetworkProjectID != "" {
			cloudConfig.NetworkProjectID = configFile.Global.NetworkProjectID
		}
	}

	// retrieve region
	cloudConfig.Region, err = GetGCERegion(cloudConfig.Zone)
	if err != nil {
		return nil, err
	}

	// Determine if its a regional cluster
	if configFile != nil && configFile.Global.Regional {
		cloudConfig.Regional = true
	}

	// generate managedZones
	cloudConfig.ManagedZones = []string{cloudConfig.Zone}
	if configFile != nil && (configFile.Global.Multizone || configFile.Global.Regional) {
		cloudConfig.ManagedZones = nil // Use all zones in region
	}

	// Determine if network parameter is URL or Name
	if configFile != nil && configFile.Global.NetworkName != "" {
		if strings.Contains(configFile.Global.NetworkName, "/") {
			cloudConfig.NetworkURL = configFile.Global.NetworkName
		} else {
			cloudConfig.NetworkName = configFile.Global.NetworkName
		}
	} else {
		cloudConfig.NetworkName, err = getNetworkNameViaMetadata()
		if err != nil {
			return nil, err
		}
	}

	// Determine if subnetwork parameter is URL or Name
	// If cluster is on a GCP network of mode=custom, then `SubnetName` must be specified in config file.
	if configFile != nil && configFile.Global.SubnetworkName != "" {
		if strings.Contains(configFile.Global.SubnetworkName, "/") {
			cloudConfig.SubnetworkURL = configFile.Global.SubnetworkName
		} else {
			cloudConfig.SubnetworkName = configFile.Global.SubnetworkName
		}
	}

	if configFile != nil {
		cloudConfig.SecondaryRangeName = configFile.Global.SecondaryRangeName
	}

	if configFile != nil {
		cloudConfig.StackType = configFile.Global.StackType
	}

	return cloudConfig, err
}

// clientOptions returns GCP API client options for authentication.
// A custom TokenSource set in the config, is used directly.
// Otherwise, FindDefaultCredentials discovers credentials.
// WithCredentialsJSON is preferred when available as it uses
// self-signed JWTs, which may be necessary for custom universe domains.
func clientOptions(ts oauth2.TokenSource) ([]option.ClientOption, error) {
	if ts != nil {
		return []option.ClientOption{option.WithTokenSource(ts)}, nil
	}

	creds, err := google.FindDefaultCredentials(context.Background(), compute.CloudPlatformScope)
	if err != nil {
		return nil, fmt.Errorf("failed to find default credentials: %w", err)
	}

	var opts []option.ClientOption
	if len(creds.JSON) > 0 {
		var f struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(creds.JSON, &f); err != nil {
			return nil, fmt.Errorf("failed to parse credentials JSON: %w", err)
		}
		opts = []option.ClientOption{option.WithAuthCredentialsJSON(option.CredentialsType(f.Type), creds.JSON)}
	} else {
		opts = []option.ClientOption{option.WithCredentials(creds)}
	}

	if ud, err := creds.GetUniverseDomain(); err == nil {
		opts = append(opts, option.WithUniverseDomain(ud))
	} else {
		klog.Warningf("Failed to get universe domain from credentials: %v", err)
	}

	return opts, nil
}

// CreateGCECloud creates a Cloud object using the specified parameters.
// If no networkUrl is specified, loads networkName via rest call.
// If no tokenSource is specified, uses FindDefaultCredentials.
// If managedZones is nil / empty all zones in the region will be managed.
func CreateGCECloud(config *CloudConfig) (*Cloud, error) {
	// If ManagedZones was empty at startup, it means the cluster was configured
	// as a Regional or Multi-zone cluster and should dynamically refresh zones.
	dynamicZones := len(config.ManagedZones) == 0

	// Remove any pre-release version and build metadata from the semver,
	// leaving only the MAJOR.MINOR.PATCH portion. See http://semver.org/.
	version := strings.TrimLeft(strings.Split(strings.Split(version.Get().GitVersion, "-")[0], "+")[0], "v")

	// Create a user-agent header append string to supply to the Google API
	// clients, to identify Kubernetes as the origin of the GCP API calls.
	userAgent := fmt.Sprintf("Kubernetes/%s (%s %s)", version, runtime.GOOS, runtime.GOARCH) // e.g. "Kubernetes/v1.18.0 (linux amd64)"

	// Use ProjectID for NetworkProjectID, if it wasn't explicitly set.
	if config.NetworkProjectID == "" {
		config.NetworkProjectID = config.ProjectID
	}

	clientOpts, err := clientOptions(config.TokenSource)
	if err != nil {
		return nil, err
	}

	service, err := compute.NewService(context.Background(), clientOpts...)
	if err != nil {
		return nil, err
	}
	service.UserAgent = userAgent

	serviceBeta, err := computebeta.NewService(context.Background(), clientOpts...)
	if err != nil {
		return nil, err
	}
	serviceBeta.UserAgent = userAgent

	serviceAlpha, err := computealpha.NewService(context.Background(), clientOpts...)
	if err != nil {
		return nil, err
	}
	serviceAlpha.UserAgent = userAgent

	if config.APIEndpoint != "" {
		if strings.HasSuffix(service.BasePath, "/projects/") {
			service.BasePath = getProjectsBasePath(config.APIEndpoint)
			serviceBeta.BasePath = getProjectsBasePath(strings.Replace(config.APIEndpoint, "v1", "beta", -1))
			serviceAlpha.BasePath = getProjectsBasePath(strings.Replace(config.APIEndpoint, "v1", "alpha", -1))
		} else {
			service.BasePath = config.APIEndpoint
			serviceBeta.BasePath = strings.Replace(config.APIEndpoint, "v1", "beta", -1)
			serviceAlpha.BasePath = strings.Replace(config.APIEndpoint, "v1", "alpha", -1)
		}
	}

	containerService, err := container.NewService(context.Background(), clientOpts...)
	if err != nil {
		return nil, err
	}
	containerService.UserAgent = userAgent
	if config.ContainerAPIEndpoint != "" {
		containerService.BasePath = config.ContainerAPIEndpoint
	}

	tpuService, err := newTPUService()
	if err != nil {
		return nil, err
	}

	// ProjectID and.NetworkProjectID may be project number or name.
	projID, netProjID := tryConvertToProjectNames(config.ProjectID, config.NetworkProjectID, service)
	onXPN := projID != netProjID

	var networkURL string
	var subnetURL string
	var isLegacyNetwork bool

	if config.NetworkURL != "" {
		networkURL = config.NetworkURL
	} else if config.NetworkName != "" {
		networkURL = gceNetworkURL(config.APIEndpoint, netProjID, config.NetworkName)
	} else {
		// Other consumers may use the cloudprovider without utilizing the wrapped GCE API functions
		// or functions requiring network/subnetwork URLs (e.g. Kubelet).
		klog.Warningf("No network name or URL specified.")
	}

	if config.SubnetworkURL != "" {
		subnetURL = config.SubnetworkURL
	} else if config.SubnetworkName != "" {
		subnetURL = gceSubnetworkURL(config.APIEndpoint, netProjID, config.Region, config.SubnetworkName)
	}
	// If neither SubnetworkURL nor SubnetworkName are provided, defer to
	// lazy initialization. Determining subnetURL and isLegacyNetwork requires
	// GCE API call. Given that it's not used in many cases and the fact that
	// the provider is initialized also for Kubelets (and there can be thousands
	// of them) we defer to lazy initialization here.

	operationPollRateLimiter := flowcontrol.NewTokenBucketRateLimiter(5, 5) // 5 qps, 5 burst.

	gce := &Cloud{
		service:                  service,
		serviceAlpha:             serviceAlpha,
		serviceBeta:              serviceBeta,
		containerService:         containerService,
		tpuService:               tpuService,
		projectID:                projID,
		networkProjectID:         netProjID,
		onXPN:                    onXPN,
		region:                   config.Region,
		regional:                 config.Regional,
		localZone:                config.Zone,
		dynamicZones:             dynamicZones,
		networkURL:               networkURL,
		unsafeIsLegacyNetwork:    isLegacyNetwork,
		unsafeSubnetworkURL:      subnetURL,
		secondaryRangeName:       config.SecondaryRangeName,
		nodeTags:                 config.NodeTags,
		nodeInstancePrefix:       config.NodeInstancePrefix,
		useMetadataServer:        config.UseMetadataServer,
		operationPollRateLimiter: operationPollRateLimiter,
		AlphaFeatureGate:         config.AlphaFeatureGate,
		nodeZones:                map[string]sets.String{},
		metricsCollector:         newLoadBalancerMetrics(),
		projectsBasePath:         getProjectsBasePath(service.BasePath),
		stackType:                StackType(config.StackType),
	}

	gce.manager = &gceServiceManager{gce}
	gce.s = &cloud.Service{
		GA:            service,
		Alpha:         serviceAlpha,
		Beta:          serviceBeta,
		ProjectRouter: &gceProjectRouter{gce},
		RateLimiter:   &gceRateLimiter{gce},
	}
	gce.c = cloud.NewGCE(gce.s)

	if len(config.ManagedZones) == 0 {
		if err := gce.refreshManagedZones(); err != nil {
			return nil, err
		}
	} else {
		gce.managedZones = config.ManagedZones
	}

	return gce, nil
}

// initializeNetworkConfig() is supposed to be called under sync.Once()
// for accessors to subnetworkURL and isLegacyNetwork fields.
func (g *Cloud) initializeSubnetworkURLAndIsLegacyNetwork() {
	if g.unsafeSubnetworkURL != "" {
		// This has already been initialized via the config.
		return
	}

	var subnetURL string
	var isLegacyNetwork bool

	// Determine the type of network and attempt to discover the correct subnet for AUTO mode.
	// Gracefully fail because kubelet calls CreateGCECloud without any config, and minions
	// lack the proper credentials for API calls.
	if networkName := lastComponent(g.NetworkURL()); networkName != "" {
		if n, err := getNetwork(g.service, g.NetworkProjectID(), networkName); err != nil {
			klog.Warningf("Could not retrieve network %q; err: %v", networkName, err)
		} else {
			switch typeOfNetwork(n) {
			case netTypeLegacy:
				klog.Infof("Network %q is type legacy - no subnetwork", networkName)
				isLegacyNetwork = true
			case netTypeCustom:
				klog.Warningf("Network %q is type custom - cannot auto select a subnetwork", networkName)
			case netTypeAuto:
				subnetURL, err = determineSubnetURL(g.service, g.NetworkProjectID(), networkName, g.Region())
				if err != nil {
					klog.Warningf("Could not determine subnetwork for network %q and region %v; err: %v", networkName, g.Region(), err)
				} else {
					klog.Infof("Auto selecting subnetwork %q", subnetURL)
				}
			}
		}
	}

	g.unsafeSubnetworkURL = subnetURL
	g.unsafeIsLegacyNetwork = isLegacyNetwork
}

// SetRateLimiter adds a custom cloud.RateLimiter implementation.
// WARNING: Calling this could have unexpected behavior if you have in-flight
// requests. It is best to use this immediately after creating a Cloud.
func (g *Cloud) SetRateLimiter(rl cloud.RateLimiter) {
	if rl != nil {
		g.s.RateLimiter = rl
	}
}

// determineSubnetURL queries for all subnetworks in a region for a given network and returns
// the URL of the subnetwork which exists in the auto-subnet range.
func determineSubnetURL(service *compute.Service, networkProjectID, networkName, region string) (string, error) {
	subnets, err := listSubnetworksOfNetwork(service, networkProjectID, networkName, region)
	if err != nil {
		return "", err
	}

	autoSubnets, err := subnetsInCIDR(subnets, autoSubnetIPRange)
	if err != nil {
		return "", err
	}

	if len(autoSubnets) == 0 {
		return "", fmt.Errorf("no subnet exists in auto CIDR")
	}

	if len(autoSubnets) > 1 {
		return "", fmt.Errorf("multiple subnetworks in the same region exist in auto CIDR")
	}

	return autoSubnets[0].SelfLink, nil
}

func tryConvertToProjectNames(configProject, configNetworkProject string, service *compute.Service) (projID, netProjID string) {
	projID = configProject
	if isProjectNumber(projID) {
		projName, err := getProjectID(service, projID)
		if err != nil {
			klog.Warningf("Failed to retrieve project %v while trying to retrieve its name. err %v", projID, err)
		} else {
			projID = projName
		}
	}

	netProjID = projID
	if configNetworkProject != configProject {
		netProjID = configNetworkProject
	}
	if isProjectNumber(netProjID) {
		netProjName, err := getProjectID(service, netProjID)
		if err != nil {
			klog.Warningf("Failed to retrieve network project %v while trying to retrieve its name. err %v", netProjID, err)
		} else {
			netProjID = netProjName
		}
	}

	return projID, netProjID
}

// Initialize takes in a clientBuilder and spawns a goroutine for watching the clusterid configmap.
// This must be called before utilizing the funcs of gce.ClusterID
func (g *Cloud) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {
	g.clientBuilder = clientBuilder
	g.client = clientBuilder.ClientOrDie("cloud-provider")

	g.eventBroadcaster = record.NewBroadcaster()
	g.eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: g.client.CoreV1().Events("")})
	g.eventRecorder = g.eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "g-cloudprovider"})

	go g.watchClusterID(stop)
	go g.metricsCollector.Run(stop)
}

// LoadBalancer returns an implementation of LoadBalancer for Google Compute Engine.
func (g *Cloud) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	return g, true
}

// Instances returns an implementation of Instances for Google Compute Engine.
func (g *Cloud) Instances() (cloudprovider.Instances, bool) {
	return g, true
}

// InstancesV2 returns an implementation of InstancesV2 for Google Compute Engine.
// Implement ONLY for external cloud provider
func (g *Cloud) InstancesV2() (cloudprovider.InstancesV2, bool) {
	return g, true
}

// Zones returns an implementation of Zones for Google Compute Engine.
func (g *Cloud) Zones() (cloudprovider.Zones, bool) {
	return g, true
}

// Clusters returns an implementation of Clusters for Google Compute Engine.
func (g *Cloud) Clusters() (cloudprovider.Clusters, bool) {
	return g, true
}

// Routes returns an implementation of Routes for Google Compute Engine.
func (g *Cloud) Routes() (cloudprovider.Routes, bool) {
	return g, true
}

// ProviderName returns the cloud provider ID.
func (g *Cloud) ProviderName() string {
	return ProviderName
}

// ProjectID returns the ProjectID corresponding to the project this cloud is in.
func (g *Cloud) ProjectID() string {
	return g.projectID
}

// NetworkProjectID returns the ProjectID corresponding to the project this cluster's network is in.
func (g *Cloud) NetworkProjectID() string {
	return g.networkProjectID
}

// Region returns the region
func (g *Cloud) Region() string {
	return g.region
}

// Regional returns true if the cluster is regional.
func (g *Cloud) Regional() bool {
	return g.regional
}

// LocalZone returns the localZone.
func (g *Cloud) LocalZone() string {
	return g.localZone
}

// OnXPN returns true if the cluster is running on a cross project network (XPN)
func (g *Cloud) OnXPN() bool {
	return g.onXPN
}

// NetworkURL returns the network url
func (g *Cloud) NetworkURL() string {
	return g.networkURL
}

// SubnetworkURL returns the subnetwork url
func (g *Cloud) SubnetworkURL() string {
	g.subnetworkURLAndIsLegacyNetworkInitializer.Do(g.initializeSubnetworkURLAndIsLegacyNetwork)
	return g.unsafeSubnetworkURL
}

// IsLegacyNetwork returns true if the cluster is still running a legacy network configuration.
func (g *Cloud) IsLegacyNetwork() bool {
	g.subnetworkURLAndIsLegacyNetworkInitializer.Do(g.initializeSubnetworkURLAndIsLegacyNetwork)
	return g.unsafeIsLegacyNetwork
}

// SetInformers sets up the zone handlers we need watching for node changes.
func (g *Cloud) SetInformers(informerFactory informers.SharedInformerFactory) {
	klog.Infof("Setting up informers for Cloud")
	nodeInformer := informerFactory.Core().V1().Nodes().Informer()
	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			node := obj.(*v1.Node)
			g.updateNodeZones(nil, node)
		},
		UpdateFunc: func(prev, obj interface{}) {
			prevNode := prev.(*v1.Node)
			newNode := obj.(*v1.Node)
			if getZone(newNode) == getZone(prevNode) {
				return
			}
			g.updateNodeZones(prevNode, newNode)
		},
		DeleteFunc: func(obj interface{}) {
			node, isNode := obj.(*v1.Node)
			// We can get DeletedFinalStateUnknown instead of *v1.Node here
			// and we need to handle that correctly.
			if !isNode {
				deletedState, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					klog.Errorf("Received unexpected object: %v", obj)
					return
				}
				node, ok = deletedState.Obj.(*v1.Node)
				if !ok {
					klog.Errorf("DeletedFinalStateUnknown contained non-Node object: %v", deletedState.Obj)
					return
				}
			}
			g.updateNodeZones(node, nil)
		},
	})
	g.nodeInformerSynced = nodeInformer.HasSynced
}

// updateNodeZonesMap updates the active node footprint in nodeZones under lock and returns the node's zone.
func (g *Cloud) updateNodeZonesMap(prevNode, newNode *v1.Node) string {
	g.nodeZonesLock.Lock()
	defer g.nodeZonesLock.Unlock()
	if prevNode != nil {
		prevZone := getZone(prevNode)
		if prevZone != emptyZone {
			g.nodeZones[prevZone].Delete(prevNode.ObjectMeta.Name)
			if g.nodeZones[prevZone].Len() == 0 {
				g.nodeZones[prevZone] = nil
			}
		}
	}
	var newZone string
	if newNode != nil {
		newZone = getZone(newNode)
		if newZone != emptyZone {
			if g.nodeZones[newZone] == nil {
				g.nodeZones[newZone] = sets.NewString()
			}
			g.nodeZones[newZone].Insert(newNode.ObjectMeta.Name)
		}
	}
	return newZone
}

func (g *Cloud) updateNodeZones(prevNode, newNode *v1.Node) {
	newZone := g.updateNodeZonesMap(prevNode, newNode)

	if !g.dynamicZones {
		return
	}

	if newNode == nil || newZone == emptyZone {
		return
	}

	if slices.Contains(g.getManagedZones(), newZone) {
		return
	}

	klog.Infof("Node %s in unmanaged zone %s; triggering GCE zone refresh.",
		newNode.ObjectMeta.Name, newZone)
	if err := g.refreshManagedZones(); err != nil {
		klog.Errorf("Failed to refresh GCE managed zones: %v", err)
		return
	}

	if !slices.Contains(g.getManagedZones(), newZone) {
		klog.Warningf("Node %s in unmanaged zone %s even after refresh. Managed: %v",
			newNode.ObjectMeta.Name, newZone, g.getManagedZones())
		return
	}

	klog.Infof("Successfully verified and added zone %s to CCM managed scope.", newZone)
}

// HasClusterID returns true if the cluster has a clusterID
func (g *Cloud) HasClusterID() bool {
	return true
}

// SetProjectFromNodeProviderID configures projectFromNodeProviderID option.
//
// Enable this ony when the Node's .spec.providerID can be fully trusted.
func (g *Cloud) SetProjectFromNodeProviderID(enabled bool) {
	g.projectFromNodeProviderID = enabled
}

func (g *Cloud) SetEnableRBSDefaultForL4NetLB(enabled bool) {
	g.enableRBSDefaultForL4NetLB = enabled
}

func (g *Cloud) SetEnableL4LBAnnotations(enabled bool) {
	g.enableL4LBAnnotations = enabled
}

func (g *Cloud) SetEnableL4DenyFirewallRule(firewallEnabled, rollbackEnabled bool) {
	g.enableL4DenyFirewallRule = firewallEnabled
	g.enableL4DenyFirewallRollbackCleanup = rollbackEnabled
}

func (g *Cloud) SetEnableL4ILBFineGrainedLocks(enabled bool) {
	g.enableL4ILBFineGrainedLocks = enabled
}

// getProjectsBasePath returns the compute API endpoint with the `projects/` element.
// The suffix must be added when generating compute resource urls.
func getProjectsBasePath(basePath string) string {
	if !strings.HasSuffix(basePath, "/") {
		basePath += "/"
	}
	if !strings.HasSuffix(basePath, "/projects/") {
		basePath += "projects/"
	}
	return basePath
}

// Project IDs cannot have a digit for the first characeter. If the id contains a digit,
// then it must be a project number.
func isProjectNumber(idOrNumber string) bool {
	_, err := strconv.ParseUint(idOrNumber, 10, 64)
	return err == nil
}

func gceNetworkURL(apiEndpoint, project, network string) string {
	if apiEndpoint == "" {
		apiEndpoint = gceComputeAPIEndpoint
	}
	return apiEndpoint + strings.Join([]string{"projects", project, "global", "networks", network}, "/")
}

func gceSubnetworkURL(apiEndpoint, project, region, subnetwork string) string {
	if apiEndpoint == "" {
		apiEndpoint = gceComputeAPIEndpoint
	}
	return apiEndpoint + strings.Join([]string{"projects", project, "regions", region, "subnetworks", subnetwork}, "/")
}

// getRegionInURL parses full resource URLS and shorter URLS
// https://www.googleapis.com/compute/v1/projects/myproject/regions/us-central1/subnetworks/a
// projects/myproject/regions/us-central1/subnetworks/a
// All return "us-central1"
func getRegionInURL(urlStr string) string {
	fields := strings.Split(urlStr, "/")
	for i, v := range fields {
		if v == "regions" && i < len(fields)-1 {
			return fields[i+1]
		}
	}
	return ""
}

func getNetworkNameViaMetadata() (string, error) {
	result, err := metadata.Get("instance/network-interfaces/0/network")
	if err != nil {
		return "", err
	}
	parts := strings.Split(result, "/")
	if len(parts) != 4 {
		return "", fmt.Errorf("unexpected response: %s", result)
	}
	return parts[3], nil
}

// getNetwork returns a GCP network
func getNetwork(svc *compute.Service, networkProjectID, networkID string) (*compute.Network, error) {
	return svc.Networks.Get(networkProjectID, networkID).Do()
}

// listSubnetworksOfNetwork returns a list of subnetworks for a particular region of a network.
func listSubnetworksOfNetwork(svc *compute.Service, networkProjectID, networkID, region string) ([]*compute.Subnetwork, error) {
	var subnets []*compute.Subnetwork
	err := svc.Subnetworks.List(networkProjectID, region).Filter(fmt.Sprintf("network eq .*/%v$", networkID)).Pages(context.Background(), func(res *compute.SubnetworkList) error {
		subnets = append(subnets, res.Items...)
		return nil
	})
	return subnets, err
}

// getProjectID returns the project's string ID given a project number or string
func getProjectID(svc *compute.Service, projectNumberOrID string) (string, error) {
	proj, err := svc.Projects.Get(projectNumberOrID).Do()
	if err != nil {
		return "", err
	}

	return proj.Name, nil
}

func findSubnetForRegion(subnetURLs []string, region string) string {
	for _, url := range subnetURLs {
		if thisRegion := getRegionInURL(url); thisRegion == region {
			return url
		}
	}
	return ""
}

func (manager *gceServiceManager) getProjectsAPIEndpoint() string {
	projectsAPIEndpoint := gceComputeAPIEndpoint + "projects/"
	if manager.gce.service != nil {
		projectsAPIEndpoint = manager.gce.projectsBasePath
	}

	return projectsAPIEndpoint
}

// getManagedZones returns a goroutine-safe copy of the GCE managed zones list.
func (g *Cloud) getManagedZones() []string {
	g.managedZonesLock.RLock()
	defer g.managedZonesLock.RUnlock()

	cp := make([]string, len(g.managedZones))
	copy(cp, g.managedZones)
	return cp
}

// refreshManagedZones queries the GCE API to update the managed zones list.
func (g *Cloud) refreshManagedZones() error {
	zones, err := g.ListZonesInRegion(g.region)
	if err != nil {
		return fmt.Errorf("failed to refresh managed zones: %v", err)
	}

	var zoneNames []string
	for _, zone := range zones {
		zoneNames = append(zoneNames, zone.Name)
	}

	g.managedZonesLock.Lock()
	defer g.managedZonesLock.Unlock()

	oldZones := sets.NewString(g.managedZones...)
	newZones := sets.NewString(zoneNames...)

	if !oldZones.Equal(newZones) {
		klog.Infof("Managed zones updated. Old: %v, New: %v", g.managedZones, zoneNames)
		g.managedZones = zoneNames
	}

	return nil
}
