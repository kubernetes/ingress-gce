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

package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/golang/glog"
	flag "github.com/spf13/pflag"

	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"

	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller"
	neg "k8s.io/ingress-gce/pkg/neg"

	"k8s.io/ingress-gce/cmd/glbc/app"
	"k8s.io/ingress-gce/pkg/backendconfig"
	"k8s.io/ingress-gce/pkg/crd"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/version"
)

func main() {
	flags.Register()
	rand.Seed(time.Now().UTC().UnixNano())

	flag.Parse()
	if flags.F.Verbose {
		flag.Set("v", "3")
	}

	// TODO: remove this when we do a release so the -logtostderr can be
	// used as a proper argument.
	flag.Lookup("logtostderr").Value.Set("true")

	if flags.F.Version {
		fmt.Printf("Controller version: %s\n", version.Version)
		os.Exit(0)
	}

	glog.V(0).Infof("Starting GLBC image: %q, cluster name %q", version.Version, flags.F.ClusterName)
	glog.V(0).Infof("Latest commit hash: %q", version.GitCommit)
	for i, a := range os.Args {
		glog.V(0).Infof("argv[%d]: %q", i, a)
	}

	glog.V(2).Infof("Flags = %+v", flags.F)
	defer glog.Flush()
	kubeConfig, err := app.NewKubeConfig()
	if err != nil {
		glog.Fatalf("Failed to create kubernetes client config: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		glog.Fatalf("Failed to create kubernetes client: %v", err)
	}

	var backendConfigClient backendconfigclient.Interface
	if flags.F.EnableBackendConfig {
		crdClient, err := crdclient.NewForConfig(kubeConfig)
		if err != nil {
			glog.Fatalf("Failed to create kubernetes CRD client: %v", err)
		}
		// TODO(rramkumar): Reuse this CRD handler for other CRD's coming.
		crdHandler := crd.NewCRDHandler(crdClient)
		backendConfigCRDMeta := backendconfig.CRDMeta()
		if _, err := crdHandler.EnsureCRD(backendConfigCRDMeta); err != nil {
			glog.Fatalf("Failed to ensure BackendConfig CRD: %v", err)
		}

		backendConfigClient, err = backendconfigclient.NewForConfig(kubeConfig)
		if err != nil {
			glog.Fatalf("Failed to create BackendConfig client: %v", err)
		}
	}

	namer, err := app.NewNamer(kubeClient, flags.F.ClusterName, firewalls.DefaultFirewallName)
	if err != nil {
		glog.Fatalf("app.NewNamer(ctx.KubeClient, %q, %q) = %v", flags.F.ClusterName, firewalls.DefaultFirewallName, err)
	}
	if namer.UID() != "" {
		glog.V(0).Infof("Cluster name: %+v", namer.UID())
	}

	cloud := app.NewGCEClient()
	enableNEG := flags.F.Features.NEG
	defaultBackendServicePortID := app.DefaultBackendServicePortID(kubeClient)
	ctxConfig := context.ControllerContextConfig{
		NEGEnabled:                    enableNEG,
		BackendConfigEnabled:          flags.F.EnableBackendConfig,
		Namespace:                     flags.F.WatchNamespace,
		ResyncPeriod:                  flags.F.ResyncPeriod,
		DefaultBackendSvcPortID:       defaultBackendServicePortID,
		HealthCheckPath:               flags.F.HealthCheckPath,
		DefaultBackendHealthCheckPath: flags.F.DefaultSvcHealthCheckPath,
	}
	ctx := context.NewControllerContext(kubeClient, backendConfigClient, cloud, namer, ctxConfig)
	go app.RunHTTPServer(ctx.HealthCheck)

	if !flags.F.LeaderElection.LeaderElect {
		runControllers(ctx)
		return
	}

	electionConfig, err := makeLeaderElectionConfig(kubeClient, ctx.Recorder(flags.F.LeaderElection.LockObjectNamespace), func() {
		runControllers(ctx)
	})
	if err != nil {
		glog.Fatalf("%v", err)
	}
	leaderelection.RunOrDie(*electionConfig)
}

// makeLeaderElectionConfig builds a leader election configuration. It will
// create a new resource lock associated with the configuration.
func makeLeaderElectionConfig(client clientset.Interface, recorder record.EventRecorder, run func()) (*leaderelection.LeaderElectionConfig, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("unable to get hostname: %v", err)
	}
	// add a uniquifier so that two processes on the same host don't accidentally both become active
	id := fmt.Sprintf("%v_%x", hostname, rand.Intn(1e6))
	rl, err := resourcelock.New(resourcelock.ConfigMapsResourceLock,
		flags.F.LeaderElection.LockObjectNamespace,
		flags.F.LeaderElection.LockObjectName,
		client.CoreV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: recorder,
		})
	if err != nil {
		return nil, fmt.Errorf("couldn't create resource lock: %v", err)
	}

	return &leaderelection.LeaderElectionConfig{
		Lock:          rl,
		LeaseDuration: flags.F.LeaderElection.LeaseDuration.Duration,
		RenewDeadline: flags.F.LeaderElection.RenewDeadline.Duration,
		RetryPeriod:   flags.F.LeaderElection.RetryPeriod.Duration,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(_ <-chan struct{}) {
				// Since we are committing a suicide after losing
				// mastership, we can safely ignore the argument.
				run()
			},
			OnStoppedLeading: func() {
				glog.Fatalf("lost master")
			},
		},
	}, nil
}

func runControllers(ctx *context.ControllerContext) {
	stopCh := make(chan struct{})
	lbc := controller.NewLoadBalancerController(ctx, stopCh)

	fwc := firewalls.NewFirewallController(ctx, flags.F.NodePortRanges.Values())

	if ctx.NEGEnabled {
		// TODO: Refactor NEG to use cloud mocks so ctx.Cloud can be referenced within NewController.
		negController := neg.NewController(ctx.Cloud, ctx, lbc.Translator, ctx.ClusterNamer, flags.F.ResyncPeriod)
		go negController.Run(stopCh)
		glog.V(0).Infof("negController started")
	}

	go app.RunSIGTERMHandler(lbc, flags.F.DeleteAllOnQuit)

	go fwc.Run(stopCh)
	glog.V(0).Infof("firewall controller started")

	ctx.Start(stopCh)
	lbc.Init()
	lbc.Run()

	for {
		glog.Infof("Handled quit, awaiting pod deletion.")
		time.Sleep(30 * time.Second)
	}
}
