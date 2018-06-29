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

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/kubernetes/pkg/apis/componentconfig"
	"k8s.io/kubernetes/pkg/client/leaderelectionconfig"
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

	// DefaultResourceLock is the type of resource used for the lock object.
	DefaultResourceLock = resourcelock.ConfigMapsResourceLock

	// DefaultLockObjectNamespace is the namespace which owns the lock object.
	DefaultLockObjectNamespace string = "kube-system"

	// DefaultLockObjectName is the object name of the lock object.
	DefaultLockObjectName = "ingress-gce-lock"
)

var (
	// F are global flags for the controller.
	F = struct {
		APIServerHost             string
		ClusterName               string
		ConfigFilePath            string
		DefaultSvcHealthCheckPath string
		DefaultSvc                string
		DefaultSvcPortName        string
		DeleteAllOnQuit           bool
		GCERateLimit              RateLimitSpecs
		HealthCheckPath           string
		HealthzPort               int
		Features                  *Features
		InCluster                 bool
		IngressClass              string
		KubeConfigFile            string
		ResyncPeriod              time.Duration
		Verbose                   bool
		Version                   bool
		WatchNamespace            string
		NodePortRanges            PortRanges
		EnableBackendConfig       bool
		EnableSyncTimestamp       bool

		LeaderElection LeaderElectionConfiguration
	}{}
)

type LeaderElectionConfiguration struct {
	componentconfig.LeaderElectionConfiguration

	// LockObjectNamespace defines the namespace of the lock object
	LockObjectNamespace string
	// LockObjectName defines the lock object name
	LockObjectName string
}

func defaultLeaderElectionConfiguration() LeaderElectionConfiguration {
	return LeaderElectionConfiguration{
		LeaderElectionConfiguration: componentconfig.LeaderElectionConfiguration{
			LeaderElect:   true,
			LeaseDuration: metav1.Duration{Duration: DefaultLeaseDuration},
			RenewDeadline: metav1.Duration{Duration: DefaultRenewDeadline},
			RetryPeriod:   metav1.Duration{Duration: DefaultRetryPeriod},
			ResourceLock:  DefaultResourceLock,
		},

		LockObjectNamespace: DefaultLockObjectNamespace,
		LockObjectName:      DefaultLockObjectName,
	}
}

func init() {
	F.NodePortRanges.ports = []string{DefaultNodePortRange}
	F.GCERateLimit.specs = []string{"alpha.Operations.Get,qps,10,100", "beta.Operations.Get,qps,10,100", "ga.Operations.Get,qps,10,100"}
	F.Features = EnabledFeatures()
	F.LeaderElection = defaultLeaderElectionConfiguration()
}

// Features is a collection of feature flags for easily enabling and disabling
// new Ingress features.
type Features struct {
	// Http2 enables ProtocolHTTP2 as a valid annotation key on Service, which
	// allows the creation HTTP2 BackendServices and HealthChecks. Alpha-only.
	Http2 bool
	// NEG enables using NetworkEndpointGroups instead of IGs as backends
	NEG bool
	// NEGExposed enables using standalone (exposed) NEGs
	NEGExposed bool
}

var DefaultFeatures = &Features{
	Http2:      true,
	NEG:        true,
	NEGExposed: true,
}

func EnabledFeatures() *Features {
	return DefaultFeatures
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
	flag.BoolVar(&F.DeleteAllOnQuit, "delete-all-on-quit", false,
		`If true, the controller will delete all Ingress and the associated
external cloud resources as it's shutting down. Mostly used for testing. In
normal environments the controller should only delete a loadbalancer if the
associated Ingress is deleted.`)
	flag.Var(&F.GCERateLimit, "gce-ratelimit",
		`Optional, can be used to rate limit certain GCE API calls. Example usage:
--gce-ratelimit=ga.Addresses.Get,qps,1.5,5
(limit ga.Addresses.Get to maximum of 1.5 qps with a burst of 5).
Use the flag more than once to rate limit more than one call. If you do not
specify this flag, the default is to rate limit Operations.Get for all versions.
If you do specify this flag one or more times, this default will be overwritten.
If you want to still use the default, simply specify it along with your other
values.`)
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
	flag.DurationVar(&F.ResyncPeriod, "sync-period", 30*time.Second,
		`Relist and confirm cloud resources this often.`)
	flag.StringVar(&F.WatchNamespace, "watch-namespace", v1.NamespaceAll,
		`Namespace to watch for Ingress/Services/Endpoints.`)
	flag.BoolVar(&F.Version, "version", false,
		`Print the version of the controller and exit`)
	flag.StringVar(&F.IngressClass, "ingress-class", "",
		`If set, overrides what ingress classes are managed by the controller.`)
	flag.Var(&F.NodePortRanges, "node-port-ranges", `Node port/port-ranges whitelisted for the
L7 load balancing. CSV values accepted. Example: -node-port-ranges=80,8080,400-500`)
	flag.BoolVar(&F.EnableBackendConfig, "enable-backend-config", false,
		`Optional, whether or not to enable BackendConfig.`)
	flag.BoolVar(&F.EnableSyncTimestamp, "enable-sync-timestamp", false,
		`Optional, whether or not to set timestamp annotation upon ingress sync.`)

	leaderelectionconfig.BindFlags(&F.LeaderElection.LeaderElectionConfiguration, flag.CommandLine)
	flag.StringVar(&F.LeaderElection.LockObjectNamespace, "lock-object-namespace", F.LeaderElection.LockObjectNamespace, "Define the namespace of the lock object.")
	flag.StringVar(&F.LeaderElection.LockObjectName, "lock-object-name", F.LeaderElection.LockObjectName, "Define the name of the lock object.")

	// Deprecated F.
	flag.BoolVar(&F.Verbose, "verbose", false,
		`This flag is deprecated. Use -v to control verbosity.`)
	flag.Bool("use-real-cloud", false,
		`This flag has been deprecated and no longer has any effect.`)
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
