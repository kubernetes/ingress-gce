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

	glog.V(0).Infof("Starting GLBC image: %q, cluster name %q", imageVersion, app.Flags.ClusterName)
	for i, a := range os.Args {
		glog.V(0).Infof("argv[%d]: %q", i, a)
	}

	glog.V(2).Infof("Flags = %+v", app.Flags)

	kubeClient, err := app.NewKubeClient()
	if err != nil {
		glog.Fatalf("Failed to create kubernetes client: %v", err)
	}

	namer, err := app.NewNamer(kubeClient, app.Flags.ClusterName, controller.DefaultFirewallName)
	if err != nil {
		glog.Fatalf("%v", err)
	}

	cloud := app.NewGCEClient()
	defaultBackendServicePort := app.DefaultBackendServicePort(kubeClient)
	clusterManager, err := controller.NewClusterManager(cloud, namer, *defaultBackendServicePort, app.Flags.HealthCheckPath)
	if err != nil {
		glog.Fatalf("Error creating cluster manager: %v", err)
	}

	enableNEG := cloud.AlphaFeatureGate.Enabled(gce.AlphaFeatureNetworkEndpointGroup)
	stopCh := make(chan struct{})
	ctx := context.NewControllerContext(kubeClient, app.Flags.WatchNamespace, app.Flags.ResyncPeriod, enableNEG)
	lbc, err := controller.NewLoadBalancerController(kubeClient, stopCh, ctx, clusterManager, enableNEG)
	if err != nil {
		glog.Fatalf("Error creating load balancer controller: %v", err)
	}

	if clusterManager.ClusterNamer.UID() != "" {
		glog.V(0).Infof("Cluster name is %+v", clusterManager.ClusterNamer.UID())
	}
	clusterManager.Init(lbc.Translator, lbc.Translator)
	glog.V(0).Infof("clusterManager initialized")

	if enableNEG {
		negController, _ := neg.NewController(kubeClient, cloud, ctx, lbc.Translator, namer, app.Flags.ResyncPeriod)
		go negController.Run(stopCh)
		glog.V(0).Infof("negController started")
	}

	go app.RunHTTPServer(lbc)
	go app.RunSIGTERMHandler(lbc, app.Flags.DeleteAllOnQuit)

	ctx.Start(stopCh)

	glog.V(0).Infof("Starting load balancer controller")
	lbc.Run()

	for {
		glog.Infof("Handled quit, awaiting pod deletion.")
		time.Sleep(30 * time.Second)
	}
}
