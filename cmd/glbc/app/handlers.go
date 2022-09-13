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
	"strings"
	"syscall"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"

	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/version"
)

// RunHTTPServer starts an HTTP server. `healthChecker` returns a mapping of component/controller
// name to the result of its healthcheck.
func RunHTTPServer(healthChecker func() context.HealthCheckResults) {
	http.HandleFunc("/healthz", healthCheckHandler(healthChecker))
	http.HandleFunc("/flag", flagHandler)
	http.Handle("/metrics", promhttp.Handler())

	klog.V(0).Infof("Running http server on :%v", flags.F.HealthzPort)
	klog.Fatal(http.ListenAndServe(fmt.Sprintf(":%v", flags.F.HealthzPort), nil))
}

func RunSIGTERMHandler(lbc *controller.LoadBalancerController, deleteAll bool) {
	// Multiple SIGTERMs will get dropped
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	klog.V(0).Infof("SIGTERM handler registered")
	<-signalChan
	klog.Infof("Received SIGTERM, shutting down")

	// TODO: Better retries than relying on restartPolicy.
	exitCode := 0
	if err := lbc.Stop(deleteAll); err != nil {
		klog.Infof("Error during shutdown %v", err)
		exitCode = 1
	}
	klog.Infof("Exiting with %v", exitCode)
	os.Exit(exitCode)
}

func healthCheckHandler(checker func() context.HealthCheckResults) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var hasErr bool
		var s strings.Builder
		for component, result := range checker() {
			status := "OK"
			if result != nil {
				hasErr = true
				status = fmt.Sprintf("err: %v", result)
			}
			s.WriteString(fmt.Sprintf("%v: %v\n", component, status))
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if hasErr {
			w.WriteHeader(500)
		} else {
			w.WriteHeader(200)
		}

		if s.Len() == 0 {
			_, err := w.Write([]byte("OK - no running controllers"))
			if err != nil {
				klog.Errorf("Error writing bytes: %v", err)
			}
			return
		}

		_, err := w.Write([]byte(s.String()))
		if err != nil {
			klog.Errorf("Error writing bytes: %v", err)
		}
		return
	}
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
			klog.Warningln("No query string params provided")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		v := values[0]
		switch key {
		case "v":
			setVerbosity(v)
		default:
			klog.Warningf("Unrecognized key: %q", key)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

func setVerbosity(v string) {
	klog.V(0).Infof("Setting verbosity level to %q", v)
	err := flag.Lookup("v").Value.Set(v)
	if err != nil {
		klog.Errorf("flag.Lookup(\"v\").Value.Set(%v) returned error: %v", v, err)
	}
}

func getFlagPage(w http.ResponseWriter, r *http.Request) {
	s := struct {
		Version   string
		Verbosity string
	}{
		Version:   version.Version,
		Verbosity: flag.Lookup("v").Value.String(),
	}
	if err := flagPageTemplate.Execute(w, s); err != nil {
		klog.Errorf("Unable to apply flag page template: %v", err)
	}
}

var flagPageTemplate = template.Must(template.New("").Parse(`GCE Ingress Controller "GLBC"
Version: {{.Version}}

Verbosity ('v'): {{.Verbosity}}
`))
