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
	"flag"
	"os"
	"time"

	"github.com/golang/glog"

	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"

	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller"
	neg "k8s.io/ingress-gce/pkg/networkendpointgroup"
	"k8s.io/ingress-gce/pkg/utils"

	"k8s.io/ingress-gce/cmd/glbc/app"
)

// Entrypoint of GLBC. Example invocation:
// 1. In a pod:
// glbc --delete-all-on-quit
// 2. Dry run (on localhost):
// $ kubectl proxy --api-prefix="/"
// $ glbc --proxy="http://localhost:proxyport"

const (
	// Current docker image version. Only used in debug logging.
	// TODO: this should be populated from the build.
	imageVersion = "glbc:0.9.7"
)

// main function for GLBC.
func main() {
	flag.Parse()
	if app.Flags.Verbose {
		flag.Set("v", "4")
	}

	glog.V(0).Infof("Starting GLBC image: %v, cluster name %v", imageVersion, app.Flags.ClusterName)

	kubeClient, err := app.NewKubeClient()
	if err != nil {
		glog.Fatalf("Failed to create client: %v.", err)
	}

	var namer *utils.Namer
	var cloud *gce.GCECloud
	var clusterManager *controller.ClusterManager

	if app.Flags.InCluster || app.Flags.UseRealCloud {
		namer, err = app.NewNamer(kubeClient, app.Flags.ClusterName, controller.DefaultFirewallName)
		if err != nil {
			glog.Fatalf("%v", err)
		}

		// TODO: Make this more resilient. Currently we create the cloud client
		// and pass it through to all the pools. This makes unit testing easier.
		// However if the cloud client suddenly fails, we should try to re-create it
		// and continue.
		if app.Flags.ConfigFilePath != "" {
			glog.Infof("Reading config from path %v", app.Flags.ConfigFilePath)
			config, err := os.Open(app.Flags.ConfigFilePath)
			if err != nil {
				glog.Fatalf("%v", err)
			}
			defer config.Close()
			cloud = app.NewGCEClient(config)
			glog.Infof("Successfully loaded cloudprovider using config %q", app.Flags.ConfigFilePath)
		} else {
			// TODO: refactor so this comment does not need to be here.
			// While you might be tempted to refactor so we simply assing nil to the
			// config and only invoke getGCEClient once, that will not do the right
			// thing because a nil check against an interface isn't true in golang.
			cloud = app.NewGCEClient(nil)
			glog.Infof("Created GCE client without a config file")
		}

		defaultBackendServicePort := app.DefaultBackendServicePort(kubeClient)
		clusterManager, err = controller.NewClusterManager(cloud, namer, *defaultBackendServicePort, app.Flags.HealthCheckPath)
		if err != nil {
			glog.Fatalf("%v", err)
		}
	} else {
		// Create fake cluster manager
		clusterManager = controller.NewFakeClusterManager(app.Flags.ClusterName, controller.DefaultFirewallName).ClusterManager
	}

	enableNEG := cloud.AlphaFeatureGate.Enabled(gce.AlphaFeatureNetworkEndpointGroup)
	stopCh := make(chan struct{})
	ctx := context.NewControllerContext(kubeClient, app.Flags.WatchNamespace, app.Flags.ResyncPeriod, enableNEG)
	// Start loadbalancer controller
	lbc, err := controller.NewLoadBalancerController(kubeClient, stopCh, ctx, clusterManager, enableNEG)
	if err != nil {
		glog.Fatalf("%v", err)
	}

	if clusterManager.ClusterNamer.UID() != "" {
		glog.V(0).Infof("Cluster name is %+v", clusterManager.ClusterNamer.UID())
	}
	clusterManager.Init(lbc.Translator, lbc.Translator)

	if enableNEG {
		negController, _ := neg.NewController(kubeClient, cloud, ctx, lbc.Translator, namer, app.Flags.ResyncPeriod)
		go negController.Run(stopCh)
	}

	go app.RunHTTPServer(lbc)
	go app.RunSIGTERMHandler(lbc, app.Flags.DeleteAllOnQuit)

	ctx.Start(stopCh)
	lbc.Run()

	for {
		glog.Infof("Handled quit, awaiting pod deletion.")
		time.Sleep(30 * time.Second)
	}
}
