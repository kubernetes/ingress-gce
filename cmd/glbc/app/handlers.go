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
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/ingress-gce/pkg/controller"
)

func RunHTTPServer(lbc *controller.LoadBalancerController) {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := lbc.CloudClusterManager.IsHealthy(); err != nil {
			w.WriteHeader(500)
			w.Write([]byte(fmt.Sprintf("Cluster unhealthy: %v", err)))
			return
		}
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/delete-all-and-quit", func(w http.ResponseWriter, r *http.Request) {
		// TODO: Retry failures during shutdown.
		lbc.Stop(true)
	})

	glog.Fatal(http.ListenAndServe(fmt.Sprintf(":%v", Flags.HealthzPort), nil))
}

func RunSIGTERMHandler(lbc *controller.LoadBalancerController, deleteAll bool) {
	// Multiple SIGTERMs will get dropped
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	<-signalChan
	glog.Infof("Received SIGTERM, shutting down")

	// TODO: Better retries than relying on restartPolicy.
	exitCode := 0
	if err := lbc.Stop(deleteAll); err != nil {
		glog.Infof("Error during shutdown %v", err)
		exitCode = 1
	}
	glog.Infof("Exiting with %v", exitCode)
	os.Exit(exitCode)
}
