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
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"k8s.io/ingress-gce/pkg/version"
	"k8s.io/klog/v2"
)

const (
	serverIdleTimeout   = 620 * time.Second
	queryStringCacheKey = "cache"
)

// ResponseBody is the structure returned by the echo server.
type ResponseBody struct {
	Host          string              `json:"host"`
	Method        string              `json:"method"`
	URI           string              `json:"uri"`
	HTTPVersion   string              `json:"httpVersion"`
	Time          time.Time           `json:"time"`
	K8sEnv        Env                 `json:"k8sEnv"`
	RemoteAddr    string              `json:"remoteAddr"`
	TLS           bool                `json:"tls"`
	Header        map[string][]string `json:"header"`
	ServerVersion string              `json:"serverVersion"`
}

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
			klog.Fatal(err)
		}

		<-ctx.Done()
		err = server.Shutdown(ctx)
		if err != nil {
			klog.Infof("Error on server shutdown %v", err)
		}
	}()

	go func() {
		if F.HTTPPort == 0 {
			return
		}

		server := &http.Server{Addr: fmt.Sprintf(":%d", F.HTTPPort), IdleTimeout: serverIdleTimeout}
		err := server.ListenAndServe()
		if err != nil {
			klog.Fatal(err)
		}

		<-ctx.Done()

		err = server.Shutdown(ctx)
		if err != nil {
			klog.Errorf("Error on server shutdown %v", err)
		}
	}()

	<-ctx.Done()
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, err := w.Write([]byte("health: OK"))
	if err != nil {
		klog.Errorf("Error writing bytes: %v, UserAgent: %v, RemoteAddr: %v", err, r.UserAgent(), r.RemoteAddr)
		return
	}
	klog.V(3).Infof("healthcheck: %v, %v, %v", time.Now(), r.UserAgent(), r.RemoteAddr)
}

func echo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	setHeadersFromQueryString(w, r)

	dump := ResponseBody{
		Host:          r.Host,
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
		klog.Errorf("failed to marshal dump: %v", err)
		return
	}

	processedData, err := process(w, r, dumpData)
	if err != nil {
		klog.Errorf("error processing data dump: %v", err)
		return
	}

	_, err = w.Write(processedData)
	if err != nil {
		klog.Errorf("Error writing data: %v, UserAgent: %v, RemoteAddr: %v", err, r.UserAgent(), r.RemoteAddr)
		return
	}
	klog.V(3).Infof("echo: %v, %v, %v", time.Now(), r.UserAgent(), r.RemoteAddr)
}

// process does additional processing on response data if necessary.
func process(w http.ResponseWriter, r *http.Request, b []byte) ([]byte, error) {
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		var compressed bytes.Buffer
		zw := gzip.NewWriter(&compressed)
		_, err := zw.Write(b)
		if err != nil {
			return nil, fmt.Errorf("failed to compress data: %v", err)
		}
		if err := zw.Close(); err != nil {
			return nil, fmt.Errorf("failed to close gzip writer: %v", err)
		}
		w.Header().Set("Content-Encoding", "gzip")
		return compressed.Bytes(), nil
	}
	return b, nil
}

// setHeadersFromQueryString looks for certain keys in the request query string
// and sets response headers based on the values for those keys.
func setHeadersFromQueryString(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get(queryStringCacheKey) == "true" {
		w.Header().Set("Cache-Control", "max-age=86400,public")
	}
}
