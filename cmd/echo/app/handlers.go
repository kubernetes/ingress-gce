/*
Copyright 2018 The Kubernetes Authors.

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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/golang/glog"
	"k8s.io/ingress-gce/pkg/version"
)

const (
	serverIdleTimeout   = 620 * time.Second
	queryStringCacheKey = "cache"
)

// RunHTTPServer runs HTTP and HTTPS goroutines and blocks.
func RunHTTPServer(ctx context.Context) {
	http.HandleFunc("/healthcheck", healthCheck)
	http.HandleFunc("/", echo)

	go func() {
		if F.HTTPSPort == 0 {
			return
		}

		server := &http.Server{Addr: fmt.Sprintf(":%d", F.HTTPSPort), IdleTimeout: serverIdleTimeout}
		cert, key := createCert()
		err := server.ListenAndServeTLS(cert, key)
		if err != nil {
			glog.Fatal(err)
		}

		<-ctx.Done()
		server.Shutdown(ctx)
	}()

	go func() {
		if F.HTTPPort == 0 {
			return
		}

		server := &http.Server{Addr: fmt.Sprintf(":%d", F.HTTPPort), IdleTimeout: serverIdleTimeout}
		err := server.ListenAndServe()
		if err != nil {
			glog.Fatal(err)
		}

		<-ctx.Done()
		server.Shutdown(ctx)
	}()

	<-ctx.Done()
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("health: OK"))
	glog.V(3).Infof("healthcheck: %v, %v, %v", time.Now(), r.UserAgent(), r.RemoteAddr)
}

func echo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	setHeadersFromQueryString(w, r)

	var dump = struct {
		Method        string              `json:"method"`
		URI           string              `json:"uri"`
		HTTPVersion   string              `json:"httpVersion"`
		Time          time.Time           `json:"time"`
		K8sEnv        Env                 `json:"k8sEnv"`
		RemoteAddr    string              `json:"remoteAddr"`
		TLS           bool                `json:"tls"`
		Header        map[string][]string `json:"header"`
		ServerVersion string              `json:"serverVersion"`
	}{
		Method:        r.Method,
		URI:           r.RequestURI,
		HTTPVersion:   fmt.Sprintf("%d.%d", r.ProtoMajor, r.ProtoMinor),
		Time:          time.Now(),
		K8sEnv:        E,
		RemoteAddr:    r.RemoteAddr,
		Header:        r.Header,
		TLS:           r.TLS != nil,
		ServerVersion: version.Version,
	}

	dumpData, err := json.MarshalIndent(dump, "", "\t")
	if err != nil {
		glog.Errorf("failed to marshal dump: %v", err)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(dumpData)
	glog.V(3).Infof("echo: %v, %v, %v", time.Now(), r.UserAgent(), r.RemoteAddr)
}

// setHeadersFromQueryString looks for certain keys in the request query string
// and sets response headers based on the values for those keys.
func setHeadersFromQueryString(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get(queryStringCacheKey) == "true" {
		w.Header().Set("Cache-Control", "max-age=86400,public")
	}
}
