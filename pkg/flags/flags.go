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

package flags

import (
	goflag "flag"
	"fmt"
	"sort"
	"strings"
	"time"

	flag "github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/component-base/config"
	leaderelectionconfig "k8s.io/component-base/config/options"
	"k8s.io/klog/v2"
)

const (
	// DefaultClusterUID is the uid to use for clusters resources created by an
	// L7 controller created without specifying the --cluster-uid flag.
	DefaultClusterUID = ""
	// DefaultNodePortRange is the list of ports or port ranges used by kubernetes for
	// allocating NodePort services.
	DefaultNodePortRange = "30000-32767"

	// DefaultLeaseDuration is a 15 second lease duration.
	DefaultLeaseDuration = 15 * time.Second
	// DefaultRenewDeadline is a 10 second renewal deadline.
	DefaultRenewDeadline = 10 * time.Second
	// DefaultRetryPeriod is a 2 second retry period.
	DefaultRetryPeriod = 2 * time.Second

	// DefaultLockObjectNamespace is the namespace which owns the lock object.
	DefaultLockObjectNamespace string = "kube-system"

	// DefaultLockObjectName is the object name of the lock object.
	DefaultLockObjectName = "ingress-gce-lock"
)

// F are global flags for the controller.
var F = struct {
	APIServerHost             string
	ClusterName               string
	ConfigFilePath            string
	DefaultSvc                string
	DefaultSvcHealthCheckPath string
	DefaultSvcPortName        string
	GCEOperationPollInterval  time.Duration
	GCERateLimit              RateLimitSpecs
	GCERateLimitScale         float64
	GKEClusterName            string
	GKEClusterHash            string
	GKEClusterType            string
	HealthCheckPath           string
	HealthzPort               int
	THCPort                   int
	InCluster                 bool
	IngressClass              string
	KubeConfigFile            string
	NegGCPeriod               time.Duration
	NumNegGCWorkers           int
	NodePortRanges            PortRanges
	ResyncPeriod              time.Duration
	L4NetLBProvisionDeadline  time.Duration
	NumL4Workers              int
	NumL4NetLBWorkers         int
	NumIngressWorkers         int
	RunIngressController      bool
	RunL4Controller           bool
	RunL4NetLBController      bool
	EnableIGController        bool
	Version                   bool
	WatchNamespace            string
	LeaderElection            LeaderElectionConfiguration
	MetricsExportInterval     time.Duration
	NegMetricsExportInterval  time.Duration
	KubeClientQPS             float32
	KubeClientBurst           int

	// Feature flags should be named Enablexxx.
	EnableFrontendConfig                      bool
	EnableNonGCPMode                          bool
	EnableReadinessReflector                  bool
	EnableV2FrontendNamer                     bool
	FinalizerAdd                              bool // Should have been named Enablexxx.
	FinalizerRemove                           bool // Should have been named Enablexxx.
	EnablePSC                                 bool
	EnableTrafficScaling                      bool
	EnableRecalculateUHCOnBCRemoval           bool
	EnableTransparentHealthChecks             bool
	EnableUpdateCustomHealthCheckDescription  bool
	EnablePinhole                             bool
	EnableL4ILBDualStack                      bool
	EnableL4NetLBDualStack                    bool
	EnableNEGController                       bool
	EnableL4NEG                               bool
	EnableL4NetLBNEG                          bool
	EnableL4NetLBNEGDefault                   bool
	GateNEGByLock                             bool
	GateL4ByLock                              bool
	EnableMultipleIGs                         bool
	EnableL4StrongSessionAffinity             bool
	EnableNEGLabelPropagation                 bool
	EnableMultiNetworking                     bool
	MaxIGSize                                 int
	EnableDegradedMode                        bool
	EnableDegradedModeMetrics                 bool
	EnableDualStackNEG                        bool
	EnableFirewallCR                          bool
	DisableFWEnforcement                      bool
	DisableL4LBFirewall                       bool
	EnableIngressRegionalExternal             bool
	EnableIngressGlobalExternal               bool
	OverrideComputeAPIEndpoint                string
	EnableIGMultiSubnetCluster                bool
	EnableMultiSubnetCluster                  bool
	EnableMultiSubnetClusterPhase1            bool
	NodeTopologyCRName                        string
	EnableWeightedL4ILB                       bool
	EnableWeightedL4NetLB                     bool
	EnableDiscretePortForwarding              bool
	EnableMultiProjectMode                    bool
	EnableL4ILBZonalAffinity                  bool
	ProviderConfigNameLabelKey                string
	EnableL4ILBMixedProtocol                  bool
	EnableL4NetLBMixedProtocol                bool
	EnableL4NetLBForwardingRulesOptimizations bool
	EnableIPV6OnlyNEG                         bool
	MultiProjectOwnerLabelKey                 string

	// ===============================
	// DEPRECATED FLAGS
	// ===============================
	// DEPRECATED: ASM ConfigMap based config is no longer used and will be removed in a future release.
	EnableASMConfigMapBasedConfig    bool
	ASMConfigMapBasedConfigCMName    string
	ASMConfigMapBasedConfigNamespace string
	// DEPRECATED: EnableDeleteUnusedFrontends is on by default and will be removed in a future release.
	EnableDeleteUnusedFrontends bool
}{
	GCERateLimitScale: 1.0,
}

type LeaderElectionConfiguration struct {
	config.LeaderElectionConfiguration

	// LockObjectNamespace defines the namespace of the lock object
	LockObjectNamespace string
	// LockObjectName defines the lock object name
	LockObjectName string
}

func defaultLeaderElectionConfiguration() LeaderElectionConfiguration {
	return LeaderElectionConfiguration{
		LeaderElectionConfiguration: config.LeaderElectionConfiguration{
			LeaderElect:   true,
			LeaseDuration: metav1.Duration{Duration: DefaultLeaseDuration},
			RenewDeadline: metav1.Duration{Duration: DefaultRenewDeadline},
			RetryPeriod:   metav1.Duration{Duration: DefaultRetryPeriod},
		},

		LockObjectNamespace: DefaultLockObjectNamespace,
		LockObjectName:      DefaultLockObjectName,
	}
}

func init() {
	F.NodePortRanges.ports = []string{DefaultNodePortRange}
	F.GCERateLimit.specs = []string{"alpha.Operations.Get,qps,10,10", "beta.Operations.Get,qps,10,10", "ga.Operations.Get,qps,10,10"}
	F.LeaderElection = defaultLeaderElectionConfiguration()
}

// Register flags with the command line parser.
func Register() {
	flag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	flag.StringVar(&F.APIServerHost, "apiserver-host", "",
		`The address of the Kubernetes Apiserver to connect to in the format of
protocol://address:port, e.g., http://localhost:8080. If not specified, the
assumption is that the binary runs inside a Kubernetes cluster and local
discovery is attempted.`)
	flag.StringVar(&F.ClusterName, "cluster-uid", DefaultClusterUID,
		`Optional, used to tag cluster wide, shared loadbalancer resources such
as instance groups. Use this flag if you'd like to continue using the same
resources across a pod restart. Note that this does not need to  match the name
of you Kubernetes cluster, it's just an arbitrary name used to tag/lookup cloud
resources.`)
	flag.StringVar(&F.ConfigFilePath, "config-file-path", "",
		`Path to a file containing the gce config. If left unspecified this
controller only works with default zones.`)
	flag.StringVar(&F.DefaultSvcHealthCheckPath, "default-backend-health-check-path", "/healthz",
		`Path used to health-check the default backend service. This path must serve a 200 page.
Flags default-backend-service and default-backend-service-port should never be empty - default
values will be used if not specified.`)
	flag.StringVar(&F.DefaultSvc, "default-backend-service", "kube-system/default-http-backend",
		`Service used to serve a 404 page for the default backend. Takes the
form namespace/name.`)
	flag.StringVar(&F.DefaultSvcPortName, "default-backend-service-port", "http",
		`Specify the default service's port used to serve a 404 page for the default backend. Takes
only the port's name - not its number.`)
	flag.BoolVar(&F.EnableFrontendConfig, "enable-frontend-config", false,
		`Optional, whether or not to enable FrontendConfig.`)
	flag.Var(&F.GCERateLimit, "gce-ratelimit",
		`Optional, can be used to rate limit certain GCE API calls. Example usage:
--gce-ratelimit=ga.Addresses.Get,qps,1.5,5
(limit ga.Addresses.Get to maximum of 1.5 qps with a burst of 5).
Use the flag more than once to rate limit more than one call. If you do not
specify this flag, the default is to rate limit Operations.Get for all versions.
If you do specify this flag one or more times, this default will be overwritten.
If you want to still use the default, simply specify it along with your other
values.
Also dynamic throttling strategy is supported in this flag. Example usage:
--gce-ratelimit=ga.NetworkEndpointGroups.ListNetworkEndpoints,strategy,dynamic,10ms,5s,5,2,5,5s,30s
(use dynamic throttling strategy for ga.NetworkEndpointGroups.ListNetworkEndpoints
with the following parameters:
minimum delay = 10ms
maximum delay = 5s
number of quota errors before increasing the delay = 5
number of requests without quota errors before decreasing the delay = 2
number of requests without quota errors before resetting the delay = 5
the amount of time without any requests before decreasing the delay = 5s
the amount of time without any requests before resetting the delay = 30s
Dynamic throttling can be combined with QPS rate limiter for one API, in that
case dynamic throttling is used first, and then the QPS rate limiter introduces
additional delay if needed.`)
	flag.Float64Var(&F.GCERateLimitScale, "gce-ratelimit-scale", 1.0,
		`Optional, scales rate limit options by a constant factor.
1.0 is no multiplier. 5.0 means increase all rate and capacity by 5x.`)
	flag.DurationVar(&F.GCEOperationPollInterval, "gce-operation-poll-interval", time.Second,
		`Minimum time between polling requests to GCE for checking the status of an operation.`)
	flag.StringVar(&F.HealthCheckPath, "health-check-path", "/",
		`Path used to health-check a backend service. All Services must serve a
200 page on this path. Currently this is only configurable globally.`)
	flag.IntVar(&F.HealthzPort, "healthz-port", 8081,
		`Port to run healthz server. Must match the health check port in yaml.`)
	flag.BoolVar(&F.InCluster, "running-in-cluster", true,
		`Optional, if this controller is running in a kubernetes cluster, use
the pod secrets for creating a Kubernetes client.`)
	flag.StringVar(&F.KubeConfigFile, "kubeconfig", "",
		`Path to kubeconfig file with authorization and master location information.`)
	flag.DurationVar(&F.ResyncPeriod, "sync-period", 10*time.Minute,
		`Relist and confirm cloud resources this often.`)
	flag.DurationVar(&F.L4NetLBProvisionDeadline, "l4-netlb-provision-deadline", 20*time.Minute,
		`Deadline latency for L4 NetLB provisioning.`)
	flag.IntVar(&F.NumL4Workers, "num-l4-workers", 5,
		`Number of parallel L4 Internal Load Balancer Service worker goroutines.`)
	flag.IntVar(&F.NumL4NetLBWorkers, "num-l4-net-workers", 5,
		`Number of parallel L4 External Load Balancer Service worker goroutines.`)
	flag.IntVar(&F.NumIngressWorkers, "num-ingress-workers", 1,
		`Number of Ingress sync-queue worker goroutines.`)
	flag.StringVar(&F.WatchNamespace, "watch-namespace", v1.NamespaceAll,
		`Namespace to watch for Ingress/Services/Endpoints.`)
	flag.BoolVar(&F.Version, "version", false,
		`Print the version of the controller and exit`)
	flag.StringVar(&F.IngressClass, "ingress-class", "",
		`If set, overrides what ingress classes are managed by the controller.`)
	flag.Var(&F.NodePortRanges, "node-port-ranges", `Node port/port-ranges whitelisted for the
L7 load balancing. CSV values accepted. Example: -node-port-ranges=80,8080,400-500`)

	leaderelectionconfig.BindLeaderElectionFlags(&F.LeaderElection.LeaderElectionConfiguration, flag.CommandLine)
	flag.StringVar(&F.LeaderElection.LockObjectNamespace, "lock-object-namespace", F.LeaderElection.LockObjectNamespace, "Define the namespace of the lock object.")
	flag.StringVar(&F.LeaderElection.LockObjectName, "lock-object-name", F.LeaderElection.LockObjectName, "Define the name of the lock object.")
	flag.DurationVar(&F.NegGCPeriod, "neg-gc-period", 120*time.Second,
		`Relist and garbage collect NEGs this often.`)
	flag.IntVar(&F.NumNegGCWorkers, "num-neg-gc-workers", 10, "Number of goroutines created by NEG garbage collector. This value controls the maximum number of concurrent calls made to the GCE NEG Delete API.")
	flag.BoolVar(&F.EnableReadinessReflector, "enable-readiness-reflector", true, "Enable NEG Readiness Reflector")
	flag.BoolVar(&F.FinalizerAdd, "enable-finalizer-add",
		F.FinalizerAdd, "Enable adding Finalizer to Ingress.")
	flag.BoolVar(&F.FinalizerRemove, "enable-finalizer-remove",
		F.FinalizerRemove, "Enable removing Finalizer from Ingress.")
	flag.BoolVar(&F.EnableASMConfigMapBasedConfig, "enable-asm-config-map-config", false, "DEPRECATED: Enable ASMConfigMapBasedConfig")
	flag.StringVar(&F.ASMConfigMapBasedConfigNamespace, "asm-configmap-based-config-namespace", "kube-system", "DEPRECATED: ASM Configmap based config: configmap namespace")
	flag.StringVar(&F.ASMConfigMapBasedConfigCMName, "asm-configmap-based-config-cmname", "ingress-controller-asm-cm-config", "DEPRECATED: ASM Configmap based config: configmap name")
	flag.BoolVar(&F.EnableNonGCPMode, "enable-non-gcp-mode", false, "Set to true when running on a non-GCP cluster.")
	flag.BoolVar(&F.EnableDeleteUnusedFrontends, "enable-delete-unused-frontends", true, "Enable deleting unused gce frontend resources.")
	flag.BoolVar(&F.EnableV2FrontendNamer, "enable-v2-frontend-namer", false, "Enable v2 ingress frontend naming policy.")
	flag.BoolVar(&F.RunIngressController, "run-ingress-controller", true, `Optional, if enabled then the ingress controller will be run.`)
	flag.BoolVar(&F.RunL4Controller, "run-l4-controller", false, `Optional, whether or not to run L4 Service Controller as part of glbc. If set to true, services of Type:LoadBalancer with Internal annotation will be processed by this controller.`)
	flag.BoolVar(&F.RunL4NetLBController, "run-l4-netlb-controller", false, `Optional, if enabled then the L4NetLbController will be run.`)
	flag.BoolVar(&F.EnableNEGController, "enable-neg-controller", true, `Optional, if enabled then the NEG controller will be run.`)
	flag.BoolVar(&F.EnableL4NEG, "enable-l4-neg", false, `Optional, if enabled then the NEG controller will process L4 NEGs.`)
	flag.BoolVar(&F.EnableL4NetLBNEG, "enable-l4-netlb-neg", false, `Optional, if enabled then the NetLB controller can create L4 NetLB services with NEG backends.`)
	flag.BoolVar(&F.EnableL4NetLBNEGDefault, "enable-l4-netlb-neg-default", false, `Optional, if enabled then newly created L4 NetLB services will use NEG backends. Has effect only if '--enable-l4-netlb-neg' is set to true.`)
	flag.BoolVar(&F.GateNEGByLock, "gate-neg-by-lock", false, "If enabled then the NEG controller will be run via leader election with NEG resource lock")
	flag.BoolVar(&F.GateL4ByLock, "gate-l4-by-lock", false, "If enabled then the L4 controllers will be run via leader election with L4 resource lock")
	flag.BoolVar(&F.EnableIGController, "enable-ig-controller", true, `Optional, if enabled then the IG controller will be run.`)
	flag.BoolVar(&F.EnablePSC, "enable-psc", false, "Enable PSC controller")
	flag.StringVar(&F.GKEClusterName, "gke-cluster-name", "", "The name of the GKE cluster this Ingress Controller will be interacting with")
	flag.StringVar(&F.GKEClusterHash, "gke-cluster-hash", "", "The cluster hash of the GKE cluster this Ingress Controller will be interacting with")
	flag.StringVar(&F.GKEClusterType, "gke-cluster-type", "ZONAL", "The cluster type of the GKE cluster this Ingress Controller will be interacting with")
	flag.BoolVar(&F.EnableTrafficScaling, "enable-traffic-scaling", false, "Enable support for Service {max-rate-per-endpoint, capacity-scaler}")
	flag.BoolVar(&F.EnableTransparentHealthChecks, "enable-transparent-health-checks", false, "Enable Transparent Health Checks.")
	flag.BoolVar(&F.EnableUpdateCustomHealthCheckDescription, "enable-update-hc-description", false, "Update health check Description when it is customized with BackendConfig CRD.")
	flag.BoolVar(&F.EnableRecalculateUHCOnBCRemoval, "enable-recalculate-uhc-on-backendconfig-removal", false, "Recalculate health check parameters when BackendConfig is removed from service. This flag cannot be used without --enable-update-hc-description.")
	flag.IntVar(&F.THCPort, "transparent-health-checks-port", 7877, "The port for Transparent Health Checks. It must be aligned with Transparent Health Check controller server. This flag only works when --enable-transparent-health-checks is enabled.")
	flag.BoolVar(&F.EnablePinhole, "enable-pinhole", false, "Enable Pinhole firewall feature")
	flag.BoolVar(&F.EnableL4ILBDualStack, "enable-l4ilb-dual-stack", false, "Enable Dual-Stack handling for L4 Internal Load Balancers")
	flag.BoolVar(&F.EnableL4NetLBDualStack, "enable-l4netlb-dual-stack", false, "Enable Dual-Stack handling for L4 External Load Balancers")
	// StrongSessionAffinity is a restricted feature that is enabled on
	// allow-listed projects only. If you need access to this feature for your
	// External L4 Load Balancer, please contact Google Cloud support team.
	flag.BoolVar(&F.EnableL4StrongSessionAffinity, "enable-l4lb-strong-sa", false, "Enable Strong Session Affinity for L4 External Load Balancers. The feature is restricted for allow-listed clusters only.")
	flag.BoolVar(&F.EnableMultipleIGs, "enable-multiple-igs", false, "Enable using multiple unmanaged instance groups")
	flag.BoolVar(&F.EnableMultiNetworking, "enable-multi-networking", false, "Enable support for multi-networking L4 load balancers.")
	flag.IntVar(&F.MaxIGSize, "max-ig-size", 1000, "Max number of instances in Instance Group")
	flag.DurationVar(&F.MetricsExportInterval, "metrics-export-interval", 10*time.Minute, `Period for calculating and exporting metrics related to state of managed objects.`)
	flag.DurationVar(&F.NegMetricsExportInterval, "neg-metrics-export-interval", 5*time.Second, `Period for calculating and exporting internal neg controller metrics, not usage.`)
	flag.BoolVar(&F.EnableDegradedMode, "enable-degraded-mode", false, `Enable degraded mode endpoint calculation and use results when error state is triggered. enabledDegradedMode also enables degrade mode correctness metrics with or without enabledDegradedModeMetrics.`)
	flag.BoolVar(&F.EnableDegradedModeMetrics, "enable-degraded-mode-metrics", false, `Enable metrics collection for degraded mode, but uses normal mode calculation result when error state is triggered.`)
	flag.BoolVar(&F.EnableNEGLabelPropagation, "enable-label-propagation", false, "Enable NEG endpoint label propagation")
	flag.BoolVar(&F.EnableDualStackNEG, "enable-dual-stack-neg", false, `Enable support for Dual-Stack NEGs within the NEG Controller`)
	flag.BoolVar(&F.EnableFirewallCR, "enable-firewall-cr", false, "Enable generating firewall CR")
	flag.BoolVar(&F.DisableFWEnforcement, "disable-fw-enforcement", false, "Disable Ingress controller to enforce the firewall rules. If set to true, Ingress Controller stops creating GCE firewall rules. We can only enable this if enable-firewall-cr sets to true.")
	flag.BoolVar(&F.DisableL4LBFirewall, "disable-l4-lb-fw", false, "Disable enforcement of L4 ILB and L4 NetLB VPC firewall rules. Required for firewall policies.")
	flag.BoolVar(&F.EnableIngressRegionalExternal, "enable-ingress-regional-external", false, "Enable L7 Ingress Regional External.")
	flag.BoolVar(&F.EnableIngressGlobalExternal, "enable-ingress-global-external", true, "Enable L7 Ingress Global External. Should be disabled when Regional External is enabled.")
	flag.StringVar(&F.OverrideComputeAPIEndpoint, "override-compute-api-endpoint", "", "Override endpoint that is used to communicate to GCP compute APIs.")
	flag.BoolVar(&F.EnableIGMultiSubnetCluster, "enable-ig-multi-subnet-cluster", false, "Enable Multi Subnet support for the controllers that use Instance Group backends.")
	flag.BoolVar(&F.EnableMultiSubnetCluster, "enable-multi-subnet-cluster", false, "Enable Multi Subnet support for all controllers that are running.")
	flag.BoolVar(&F.EnableMultiSubnetClusterPhase1, "enable-multi-subnet-cluster-phase1", false, "Enable Phase 1 Multi Subnet support for all controllers that are running.")
	flag.StringVar(&F.NodeTopologyCRName, "node-topology-cr-name", "default", "The name of the Node Topology CR.")
	flag.BoolVar(&F.EnableWeightedL4ILB, "enable-weighted-l4-ilb", false, "Enable Weighted Load balancing for L4 ILB.")
	flag.BoolVar(&F.EnableWeightedL4NetLB, "enable-weighted-l4-netlb", false, "EnableWeighted Load balancing for  L4 NetLB .")
	flag.BoolVar(&F.EnableL4ILBZonalAffinity, "enable-l4ilb-zonal-affinity", false, "Enable Zonal Affinity for L4 ILB.")
	flag.Float32Var(&F.KubeClientQPS, "kube-client-qps", 0.0, "The QPS that the controllers' kube client should adhere to through client side throttling. If zero, client will be created with default settings.")
	flag.IntVar(&F.KubeClientBurst, "kube-client-burst", 0, "The burst QPS that the controllers' kube client should adhere to through client side throttling. If zero, client will be created with default settings.")
	flag.BoolVar(&F.EnableDiscretePortForwarding, "enable-discrete-port-forwarding", false, "Enable forwarding of individual ports instead of port ranges.")
	flag.BoolVar(&F.EnableMultiProjectMode, "enable-multi-project-mode", false, "Enable running in multi-project mode.")
	flag.BoolVar(&F.EnableL4ILBMixedProtocol, "enable-l4ilb-mixed-protocol", false, "Enable support for mixed protocol L4 internal load balancers.")
	flag.BoolVar(&F.EnableL4NetLBMixedProtocol, "enable-l4netlb-mixed-protocol", false, "Enable support for mixed protocol L4 external load balancers.")
	flag.StringVar(&F.ProviderConfigNameLabelKey, "provider-config-name-label-key", "cloud.gke.io/provider-config-name", "The label key for provider-config name, which is used to identify the provider-config of objects in multi-project mode.")
	flag.BoolVar(&F.EnableL4NetLBForwardingRulesOptimizations, "enable-l4netlb-forwarding-rules-optimizations", false, "Enable optimized processing of forwarding rules for L4 NetLB.")
	flag.BoolVar(&F.EnableIPV6OnlyNEG, "enable-ipv6-only-neg", false, "Enable support for IPV6 Only NEG's.")
	flag.StringVar(&F.MultiProjectOwnerLabelKey, "multi-project-owner-label-key", "multiproject.gke.io/owner", "The label key for multi-project owner, which is used to identify the owner of objects in multi-project mode.")
}

func Validate() {
	if F.EnableRecalculateUHCOnBCRemoval && !F.EnableUpdateCustomHealthCheckDescription {
		klog.Fatalf("The flag --enable-recalculate-uhc-on-backendconfig-removal cannot be used without --enable-update-hc-description.")
	}

	// There is no information available whether --transparent-health-checks-port is set, but it is certainly set if F.THCPort stores a non-default value.
	if F.THCPort != 7877 && !F.EnableTransparentHealthChecks {
		klog.Fatalf("The flag --transparent-health-checks-port cannot be used without --enable-transparent-health-checks.")
	}
}

type RateLimitSpecs struct {
	specs []string
	isSet bool
}

// Part of the flag.Value interface.
func (r *RateLimitSpecs) String() string {
	return strings.Join(r.specs, ";")
}

// Set supports the flag being repeated multiple times. Part of the flag.Value interface.
func (r *RateLimitSpecs) Set(value string) error {
	// On first Set(), clear the original defaults
	// On subsequent Set()'s, append.
	if !r.isSet {
		r.specs = []string{}
		r.isSet = true
	}
	r.specs = append(r.specs, value)
	return nil
}

func (r *RateLimitSpecs) Values() []string {
	return r.specs
}

func (r *RateLimitSpecs) Type() string {
	return "rateLimitSpecs"
}

type PortRanges struct {
	ports []string
	isSet bool
}

// String is the method to format the flag's value, part of the flag.Value interface.
func (c *PortRanges) String() string {
	return strings.Join(c.ports, ",")
}

// Set supports a value of CSV or the flag repeated multiple times
func (c *PortRanges) Set(value string) error {
	// On first Set(), clear the original defaults
	if !c.isSet {
		c.isSet = true
	} else {
		return fmt.Errorf("NodePort Ranges have already been set")
	}

	c.ports = strings.Split(value, ",")
	sort.Strings(c.ports)
	return nil
}

// Set supports a value of CSV or the flag repeated multiple times
func (c *PortRanges) Values() []string {
	return c.ports
}

func (c *PortRanges) Type() string {
	return "portRanges"
}
