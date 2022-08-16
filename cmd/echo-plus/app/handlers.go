/*
Copyright 2022 The Kubernetes Authors.

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
	"math/rand"
	"net/http"
	"os"
	"strconv"
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
	http.HandleFunc("/panic", triggerPanic)
	http.HandleFunc("/randomSlowRequest", randomSlowRequest)
	http.HandleFunc("/slowRequest/", slowRequest)
	http.HandleFunc("/testMethod", testMethods)
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
			klog.Infof("Error on server shutdown %v", err)
		}
	}()

	<-ctx.Done()
}

//triggerPanic crashes the app running on a pod
func triggerPanic(w http.ResponseWriter, r *http.Request) {
	klog.V(3).Infof("panic: %v, %v, %v, %v", time.Now(), r.UserAgent(), r.RemoteAddr, r.Method)
	klog.V(3).Infof("node: %v,  pod: %v, namespace: %v", E.Node, E.Pod, E.Namespace)
	os.Exit(1)
}

// randomSlowRequest does health check after a random latency
func randomSlowRequest(w http.ResponseWriter, r *http.Request) {
	rand.Seed(time.Now().UnixNano())
	sleepTime := rand.Intn(F.RequestLatency)
	time.Sleep(time.Duration(sleepTime) * time.Millisecond)

	klog.V(3).Infof("sleep for %v ms", sleepTime)

	w.WriteHeader(http.StatusOK)
	_, writeErr := w.Write([]byte(fmt.Sprintf("Slow request method: %v OK", r.Method)))
	if writeErr != nil {
		klog.Errorf("Error writing bytes: %v, UserAgent: %v, RemoteAddr: %v", writeErr, r.UserAgent(), r.RemoteAddr)
		return
	}
	klog.V(3).Infof("%v, %v, %v, %v", time.Now(), r.UserAgent(), r.RemoteAddr, r.Method)
}

// slowRequest does health check after a latency of n millisecond, n is in the path after /slowRequest/
func slowRequest(w http.ResponseWriter, r *http.Request) {
	sleepTime, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/slowRequest/"))
	if err != nil {
		klog.Errorf("failed to process slowRequest with error: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		_, writeErr := w.Write([]byte("Wrong input, should have latency length (in ms) after /slowRequest/"))
		if writeErr != nil {
			klog.Errorf("Error writing bytes: %v, UserAgent: %v, RemoteAddr: %v", writeErr, r.UserAgent(), r.RemoteAddr)
			return
		}
		return
	}
	time.Sleep(time.Duration(sleepTime) * time.Millisecond)

	klog.V(3).Infof("sleep for %v ms", sleepTime)
	w.WriteHeader(http.StatusOK)
	_, writeErr := w.Write([]byte(fmt.Sprintf("Slow request method: %v OK", r.Method)))
	if writeErr != nil {
		klog.Errorf("Error writing bytes: %v, UserAgent: %v, RemoteAddr: %v", writeErr, r.UserAgent(), r.RemoteAddr)
		return
	}
	klog.V(3).Infof("%v, %v, %v, %v", time.Now(), r.UserAgent(), r.RemoteAddr, r.Method)
}

// testMethods sends different http method calls to the workload
func testMethods(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, writeErr := w.Write([]byte(fmt.Sprintf("Test method: %v OK", r.Method)))
	if writeErr != nil {
		klog.Errorf("Error writing bytes: %v, UserAgent: %v, RemoteAddr: %v", writeErr, r.UserAgent(), r.RemoteAddr)
		return
	}
	klog.V(3).Infof("%v, %v, %v, %v", time.Now(), r.UserAgent(), r.RemoteAddr, r.Method)
}

// healthCheck checks whether the workload is healthy
func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, writeErr := w.Write([]byte("health: OK"))
	if writeErr != nil {
		klog.Errorf("Error writing bytes: %v, UserAgent: %v, RemoteAddr: %v", writeErr, r.UserAgent(), r.RemoteAddr)
		return
	}
	klog.V(3).Infof("healthcheck: %v, %v, %v", time.Now(), r.UserAgent(), r.RemoteAddr)
}

// echo retrieves a bunch of info about the workload and k8s environment
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

	_, writeErr := w.Write(processedData)
	if writeErr != nil {
		klog.Errorf("Error writing bytes: %v, UserAgent: %v, RemoteAddr: %v", writeErr, r.UserAgent(), r.RemoteAddr)
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
		zw.Close()
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
