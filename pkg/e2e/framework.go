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

package e2e

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"testing"

	"github.com/golang/glog"
	"golang.org/x/oauth2/google"
	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	compute "google.golang.org/api/compute/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud"
)

// Options for the test framework.
type Options struct {
	Project          string
	Seed             int64
	DestroySandboxes bool
}

// NewFramework returns a new test framework to run.
func NewFramework(config *rest.Config, options Options) *Framework {
	theCloud, err := NewCloud(options.Project)
	if err != nil {
		panic(err)
	}
	backendConfigClient, err := backendconfigclient.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Failed to create BackendConfig client: %v", err)
	}
	f := &Framework{
		RestConfig:          config,
		Clientset:           kubernetes.NewForConfigOrDie(config),
		BackendConfigClient: backendConfigClient,
		Project:             options.Project,
		Cloud:               theCloud,
		Rand:                rand.New(rand.NewSource(options.Seed)),
		destroySandboxes:    options.DestroySandboxes,
	}
	f.statusManager = NewStatusManager(f)
	return f
}

// Framework is the end-to-end test framework.
type Framework struct {
	RestConfig          *rest.Config
	Clientset           *kubernetes.Clientset
	BackendConfigClient *backendconfigclient.Clientset
	Project             string
	Cloud               cloud.Cloud
	Rand                *rand.Rand
	statusManager       *StatusManager

	destroySandboxes bool

	lock      sync.Mutex
	sandboxes []*Sandbox
}

// SanityCheck the test environment before proceeding.
func (f *Framework) SanityCheck() error {
	glog.V(2).Info("Checking connectivity with Kubernetes API")
	if _, err := f.Clientset.Core().Pods("default").List(metav1.ListOptions{}); err != nil {
		glog.Errorf("Error accessing Kubernetes API: %v", err)
		return err
	}
	glog.V(2).Infof("Checking connectivity with Google Cloud API (get project %q)", f.Project)
	if _, err := f.Cloud.Projects().Get(context.Background(), f.Project); err != nil {
		glog.Errorf("Error accessing compute API: %v", err)
		return err
	}
	glog.V(2).Info("Checking external Internet connectivity")
	for _, url := range []string{
		"http://www.google.com",
		"http://www.amazon.com",
	} {
		if _, err := http.Get(url); err != nil {
			glog.Errorf("Error in HTTP GET to %q: %v", url, err)
			return err
		}
	}
	glog.V(2).Info("Checking status manager initialization")
	if err := f.statusManager.init(); err != nil {
		glog.Errorf("Error initalizing status manager: %v", err)
		return err
	}
	return nil
}

// CatchSIGINT and cleanup sandboxes when the test is interrupted.
func (f *Framework) CatchSIGINT() {
	glog.Infof("Catching SIGINT")

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for range c {
			f.sigintHandler()
		}
	}()
}
func (f *Framework) sigintHandler() {
	if !f.destroySandboxes {
		return
	}
	glog.Warningf("SIGINT received, shutting down (disable with -handleSIGINT=false)")
	f.shutdown(1)
}

func (f *Framework) shutdown(exitCode int) {
	f.lock.Lock()
	defer f.lock.Unlock()
	glog.V(2).Infof("Cleaning up sandboxes...")
	for _, s := range f.sandboxes {
		s.Destroy()
	}
	f.statusManager.shutdown()
	os.Exit(exitCode)
}

// WithSandbox runs the testFunc with the Sandbox, taking care of resource
// cleanup and isolation.
func (f *Framework) WithSandbox(testFunc func(*Sandbox) error) error {
	sandbox := &Sandbox{
		Namespace: fmt.Sprintf("sandbox-%x", f.Rand.Int63()),
		f:         f,
	}
	glog.V(2).Infof("Using namespace %q for test sandbox", sandbox.Namespace)
	if err := sandbox.Create(); err != nil {
		return err
	}

	f.lock.Lock()
	f.sandboxes = append(f.sandboxes, sandbox)
	f.lock.Unlock()

	if f.destroySandboxes {
		defer sandbox.Destroy()
	}

	return testFunc(sandbox)
}

// RunWithSandbox runs the testFunc with the Sandbox, taking care of resource
// cleanup and isolation. This indirectly calls testing.T.Run().
func (f *Framework) RunWithSandbox(name string, t *testing.T, testFunc func(*testing.T, *Sandbox)) {
	t.Run(name, func(t *testing.T) {
		sandbox := &Sandbox{
			Namespace: fmt.Sprintf("sandbox-%x", f.Rand.Int63()),
			f:         f,
		}
		glog.V(2).Infof("Using namespace %q for test sandbox", sandbox.Namespace)
		if err := sandbox.Create(); err != nil {
			t.Fatalf("error creating sandbox: %v", err)
		}

		f.lock.Lock()
		f.sandboxes = append(f.sandboxes, sandbox)
		f.lock.Unlock()

		if f.destroySandboxes {
			defer sandbox.Destroy()
		}

		testFunc(t, sandbox)
	})
}

// NewCloud creates a new cloud for the given project.
func NewCloud(project string) (cloud.Cloud, error) {
	const computeScope = "https://www.googleapis.com/auth/compute"
	client, err := google.DefaultClient(context.Background(), computeScope)
	if err != nil {
		return nil, err
	}

	service, err := compute.New(client)
	if err != nil {
		return nil, err
	}
	serviceAlpha, err := computealpha.New(client)
	if err != nil {
		return nil, err
	}
	serviceBeta, err := computebeta.New(client)
	if err != nil {
		return nil, err
	}

	cloudService := &cloud.Service{
		GA:            service,
		Alpha:         serviceAlpha,
		Beta:          serviceBeta,
		ProjectRouter: &cloud.SingleProjectRouter{ID: project},
		RateLimiter:   &cloud.NopRateLimiter{},
	}

	return cloud.NewGCE(cloudService), nil
}
