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
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"k8s.io/ingress-gce/pkg/controller"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/version"
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
	http.HandleFunc("/flag", flagHandler)
	http.HandleFunc("/delete-all-and-quit", func(w http.ResponseWriter, r *http.Request) {
		// TODO: Retry failures during shutdown.
		lbc.Stop(true)
	})

	glog.V(0).Infof("Running http server on :%v", flags.F.HealthzPort)
	glog.Fatal(http.ListenAndServe(fmt.Sprintf(":%v", flags.F.HealthzPort), nil))
}

func RunSIGTERMHandler(lbc *controller.LoadBalancerController, deleteAll bool) {
	// Multiple SIGTERMs will get dropped
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	glog.V(0).Infof("SIGTERM handler registered")
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

func flagHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		getFlagPage(w, r)
		return
	case "PUT":
		putFlag(w, r)
	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}
}

func putFlag(w http.ResponseWriter, r *http.Request) {
	for key, values := range r.URL.Query() {
		if len(values) != 1 {
			glog.Warningln("No query string params provided")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		v := values[0]
		switch key {
		case "v":
			setVerbosity(v)
		default:
			glog.Warningf("Unrecognized key: %q", key)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

func setVerbosity(v string) {
	flag.Lookup("v").Value.Set(v)
	glog.V(0).Infof("Setting verbosity level to %q", v)
}

func getFlagPage(w http.ResponseWriter, r *http.Request) {
	s := struct {
		Version   string
		Verbosity string
	}{
		Version:   version.Version,
		Verbosity: flag.Lookup("v").Value.String(),
	}
	flagPageTemplate.Execute(w, s)
}

var flagPageTemplate = template.Must(template.New("").Parse(`GCE Ingress Controller "GLBC"
Version: {{.Version}}

Verbosity ('v'): {{.Verbosity}}
`))
