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
	"context"
	"fmt"
	"math/rand"
	"os"
	"time"

	flag "github.com/spf13/pflag"
	"k8s.io/klog"

	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"

	ingctx "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller"
	neg "k8s.io/ingress-gce/pkg/neg"

	"k8s.io/ingress-gce/cmd/glbc/app"
	"k8s.io/ingress-gce/pkg/backendconfig"
	"k8s.io/ingress-gce/pkg/crd"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/flags"
	_ "k8s.io/ingress-gce/pkg/klog"
	"k8s.io/ingress-gce/pkg/version"
)

func main() {
	flags.Register()
	rand.Seed(time.Now().UTC().UnixNano())
	flag.Parse()

	if flags.F.Version {
		fmt.Printf("Controller version: %s\n", version.Version)
		os.Exit(0)
	}

	klog.V(0).Infof("Starting GLBC image: %q, cluster name %q", version.Version, flags.F.ClusterName)
	klog.V(0).Infof("Latest commit hash: %q", version.GitCommit)
	for i, a := range os.Args {
		klog.V(0).Infof("argv[%d]: %q", i, a)
	}

	klog.V(2).Infof("Flags = %+v", flags.F)
	defer klog.Flush()
	kubeConfig, err := app.NewKubeConfig()
	if err != nil {
		klog.Fatalf("Failed to create kubernetes client config: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		klog.Fatalf("Failed to create kubernetes client: %v", err)
	}

	// Due to scaling issues, leader election must be configured with a separate k8s client.
	leaderElectKubeClient, err := kubernetes.NewForConfig(restclient.AddUserAgent(kubeConfig, "leader-election"))
	if err != nil {
		klog.Fatalf("Failed to create kubernetes client for leader election: %v", err)
	}

	var backendConfigClient backendconfigclient.Interface
	crdClient, err := crdclient.NewForConfig(kubeConfig)
	if err != nil {
		klog.Fatalf("Failed to create kubernetes CRD client: %v", err)
	}
	// TODO(rramkumar): Reuse this CRD handler for other CRD's coming.
	crdHandler := crd.NewCRDHandler(crdClient)
	backendConfigCRDMeta := backendconfig.CRDMeta()
	if _, err := crdHandler.EnsureCRD(backendConfigCRDMeta); err != nil {
		klog.Fatalf("Failed to ensure BackendConfig CRD: %v", err)
	}

	backendConfigClient, err = backendconfigclient.NewForConfig(kubeConfig)
	if err != nil {
		klog.Fatalf("Failed to create BackendConfig client: %v", err)
	}

	namer, err := app.NewNamer(kubeClient, flags.F.ClusterName, firewalls.DefaultFirewallName)
	if err != nil {
		klog.Fatalf("app.NewNamer(ctx.KubeClient, %q, %q) = %v", flags.F.ClusterName, firewalls.DefaultFirewallName, err)
	}
	if namer.UID() != "" {
		klog.V(0).Infof("Cluster name: %+v", namer.UID())
	}

	cloud := app.NewGCEClient()
	defaultBackendServicePortID := app.DefaultBackendServicePortID(kubeClient)
	ctxConfig := ingctx.ControllerContextConfig{
		Namespace:                     flags.F.WatchNamespace,
		ResyncPeriod:                  flags.F.ResyncPeriod,
		DefaultBackendSvcPortID:       defaultBackendServicePortID,
		HealthCheckPath:               flags.F.HealthCheckPath,
		DefaultBackendHealthCheckPath: flags.F.DefaultSvcHealthCheckPath,
	}
	ctx := ingctx.NewControllerContext(kubeClient, backendConfigClient, cloud, namer, ctxConfig)
	go app.RunHTTPServer(ctx.HealthCheck)

	if !flags.F.LeaderElection.LeaderElect {
		runControllers(ctx)
		return
	}

	electionConfig, err := makeLeaderElectionConfig(leaderElectKubeClient, ctx.Recorder(flags.F.LeaderElection.LockObjectNamespace), func() {
		runControllers(ctx)
	})
	if err != nil {
		klog.Fatalf("%v", err)
	}
	leaderelection.RunOrDie(context.Background(), *electionConfig)
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
		client.CoordinationV1(),
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
			OnStartedLeading: func(context.Context) {
				// Since we are committing a suicide after losing
				// mastership, we can safely ignore the argument.
				run()
			},
			OnStoppedLeading: func() {
				klog.Fatalf("lost master")
			},
		},
	}, nil
}

func runControllers(ctx *ingctx.ControllerContext) {
	stopCh := make(chan struct{})
	lbc := controller.NewLoadBalancerController(ctx, stopCh)

	fwc := firewalls.NewFirewallController(ctx, flags.F.NodePortRanges.Values())

	// TODO: Refactor NEG to use cloud mocks so ctx.Cloud can be referenced within NewController.
	negController := neg.NewController(neg.NewAdapter(ctx.Cloud), ctx, lbc.Translator, ctx.ClusterNamer, flags.F.ResyncPeriod, flags.F.NegGCPeriod, neg.NegSyncerType(flags.F.NegSyncerType))
	go negController.Run(stopCh)
	klog.V(0).Infof("negController started")

	go app.RunSIGTERMHandler(lbc, flags.F.DeleteAllOnQuit)

	go fwc.Run()
	klog.V(0).Infof("firewall controller started")

	ctx.Start(stopCh)
	lbc.Init()
	lbc.Run()

	for {
		klog.Infof("Handled quit, awaiting pod deletion.")
		time.Sleep(30 * time.Second)
	}
}
