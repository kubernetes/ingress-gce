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

package app

import (
	"flag"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/controller"
)

var (
	// Flags are the command line flags.
	Flags = struct {
		APIServerHost   string
		ClusterName     string
		ConfigFilePath  string
		DefaultSvc      string
		DeleteAllOnQuit bool
		HealthCheckPath string
		HealthzPort     int
		InCluster       bool
		KubeConfigFile  string
		ResyncPeriod    time.Duration
		UseRealCloud    bool
		Verbose         bool
		WatchNamespace  string
	}{}
)

func init() {
	flag.StringVar(&Flags.APIServerHost, "apiserver-host", "",
		`The address of the Kubernetes Apiserver to connect to in the format of
protocol://address:port, e.g., http://localhost:8080. If not specified, the
assumption is that the binary runs inside a Kubernetes cluster and local
discovery is attempted.`)
	flag.StringVar(&Flags.ClusterName, "cluster-uid", controller.DefaultClusterUID,
		`Optional, used to tag cluster wide, shared loadbalancer resources such
as instance groups. Use this flag if you'd like to continue using the same
resources across a pod restart. Note that this does not need to  match the name
of you Kubernetes cluster, it's just an arbitrary name used to tag/lookup cloud
resources.`)
	flag.StringVar(&Flags.ConfigFilePath, "config-file-path", "",
		`Path to a file containing the gce config. If left unspecified this
controller only works with default zones.`)
	flag.StringVar(&Flags.DefaultSvc, "default-backend-service", "kube-system/default-http-backend",
		`Service used to serve a 404 page for the default backend. Takes the
form namespace/name. The controller uses the first node port of this Service for
the default backend.`)
	flag.BoolVar(&Flags.DeleteAllOnQuit, "delete-all-on-quit", false,
		`If true, the controller will delete all Ingress and the associated
external cloud resources as it's shutting down. Mostly used for testing. In
normal environments the controller should only delete a loadbalancer if the
associated Ingress is deleted.`)
	flag.StringVar(&Flags.HealthCheckPath, "health-check-path", "/",
		`Path used to health-check a backend service. All Services must serve a
200 page on this path. Currently this is only configurable globally.`)
	flag.IntVar(&Flags.HealthzPort, "healthz-port", 8081,
		`Port to run healthz server. Must match the health check port in yaml.`)
	flag.BoolVar(&Flags.InCluster, "running-in-cluster", true,
		`Optional, if this controller is running in a kubernetes cluster, use
the pod secrets for creating a Kubernetes client.`)
	flag.StringVar(&Flags.KubeConfigFile, "kubeconfig", "",
		`Path to kubeconfig file with authorization and master location information.`)
	flag.DurationVar(&Flags.ResyncPeriod, "sync-period", 30*time.Second,
		`Relist and confirm cloud resources this often.`)
	// TODO: Consolidate this flag and running-in-cluster. People already use
	// the first one to mean "running in dev", unfortunately.
	flag.BoolVar(&Flags.UseRealCloud, "use-real-cloud", false,
		`Optional, if set a real cloud client is created. Only matters with
--running-in-cluster=false, i.e a real cloud is always used when this controller
is running on a Kubernetes node.`)
	flag.BoolVar(&Flags.Verbose, "verbose", false,
		`If true, logs are displayed at V(4), otherwise V(2).`)
	flag.StringVar(&Flags.WatchNamespace, "watch-namespace", v1.NamespaceAll,
		`Namespace to watch for Ingress/Services/Endpoints.`)
}
