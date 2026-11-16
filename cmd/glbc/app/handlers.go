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
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/GoogleCloudPlatform/gke-enterprise-mt/pkg/mtmetrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"

	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/systemhealth"
)

// RunHTTPServer starts an HTTP server. `healthChecker` returns a mapping of component/controller
// name to the result of its healthcheck.
func RunHTTPServer(healthChecker func() systemhealth.HealthCheckResults, logger klog.Logger) {
	http.HandleFunc("/healthz", healthCheckHandler(healthChecker, logger))
	http.Handle("/metrics", promhttp.Handler())
	if flags.F.EnableMultiProjectMode {
		registerMultitenancyMetricsHandler(http.DefaultServeMux, logger)
	}

	logger.V(0).Info("Running http server", "port", flags.F.HealthzPort)
	klog.Fatal(http.ListenAndServe(fmt.Sprintf(":%v", flags.F.HealthzPort), nil))
}

func registerMultitenancyMetricsHandler(mux *http.ServeMux, logger klog.Logger) {
	gatherer, ok := mtmetrics.DefaultMultiGatherer.(prometheus.Gatherer)
	if !ok {
		logger.Error(nil, "Failed to initialize multi-tenant metrics: mtmetrics.DefaultMultiGatherer does not implement prometheus.Gatherer")
		return
	}
	mux.HandleFunc("/metrics/multitenancy", loopbackOnly(promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{})))
}

func loopbackOnly(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, "Forbidden: accessed from outside localhost", http.StatusForbidden)
			return
		}
		ip := net.ParseIP(host)
		if ip != nil && ip.IsLoopback() {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "Forbidden: accessed from outside localhost", http.StatusForbidden)
	}
}

func RunSIGTERMHandler(closeStopCh func(), logger klog.Logger) {
	// Multiple SIGTERMs will get dropped
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	logger.V(0).Info("SIGTERM handler registered")
	<-signalChan
	logger.Info("Received SIGTERM, shutting down")

	closeStopCh()
}

func healthCheckHandler(checker func() systemhealth.HealthCheckResults, logger klog.Logger) http.HandlerFunc {
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
				logger.Error(err, "Error writing bytes")
			}
			return
		}

		_, err := w.Write([]byte(s.String()))
		if err != nil {
			logger.Error(err, "Error writing bytes")
		}
		return
	}
}
