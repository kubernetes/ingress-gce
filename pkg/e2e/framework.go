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
	"strings"
	"sync"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"golang.org/x/oauth2/google"
	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	compute "google.golang.org/api/compute/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	frontendconfigclient "k8s.io/ingress-gce/pkg/frontendconfig/client/clientset/versioned"
	serviceattachment "k8s.io/ingress-gce/pkg/serviceattachment/client/clientset/versioned"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/klog"
)

// Options for the test framework.
type Options struct {
	Project             string
	Region              string
	Network             string
	Seed                int64
	DestroySandboxes    bool
	GceEndpointOverride string
	CreateILBSubnet     bool
}

const (
	destinationRuleGroup      = "networking.istio.io"
	destinationRuleAPIVersion = "v1alpha3"
	destinationRulePlural     = "destinationrules"
	// This must match the spec fields below, and be in the form: <plural>.<group>
	destinationRuleCRDName = "destinationrules.networking.istio.io"
)

// NewFramework returns a new test framework to run.
func NewFramework(config *rest.Config, options Options) *Framework {
	theCloud, err := NewCloud(options.Project, options.GceEndpointOverride)
	if err != nil {
		panic(err)
	}
	backendConfigClient, err := backendconfigclient.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create BackendConfig client: %v", err)
	}

	frontendConfigClient, err := frontendconfigclient.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create FrontendConfig client: %v", err)
	}

	svcNegClient, err := svcnegclient.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create SvcNeg client: %v", err)
	}

	saClient, err := serviceattachment.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create Service Attachment client: %v", err)
	}

	f := &Framework{
		RestConfig:           config,
		Clientset:            kubernetes.NewForConfigOrDie(config),
		crdClient:            apiextensionsclient.NewForConfigOrDie(config),
		FrontendConfigClient: frontendConfigClient,
		BackendConfigClient:  backendConfigClient,
		SvcNegClient:         svcNegClient,
		SAClient:             saClient,
		Project:              options.Project,
		Region:               options.Region,
		Network:              options.Network,
		Cloud:                theCloud,
		Rand:                 rand.New(rand.NewSource(options.Seed)),
		destroySandboxes:     options.DestroySandboxes,
		CreateILBSubnet:      options.CreateILBSubnet,
	}
	f.statusManager = NewStatusManager(f)

	// Preparing dynamic client if Istio:DestinationRule CRD exisits and matches the required version.
	// The client is used by the ASM e2e tests.
	destinationRuleCRD, err := f.crdClient.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), destinationRuleCRDName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("Cannot load DestinationRule CRD, Istio is disabled on this cluster.")
		} else {
			klog.Fatalf("Failed to load DestinationRule CRD, error: %s", err)
		}
	} else {
		if destinationRuleCRD.Spec.Versions[0].Name != destinationRuleAPIVersion {
			klog.Fatalf("The cluster Istio version not meet the testing requirement, want: %s, got: %s.", destinationRuleAPIVersion, destinationRuleCRD.Spec.Versions[0].Name)
		} else {
			dynamicClient, err := dynamic.NewForConfig(config)
			if err != nil {
				klog.Fatalf("Failed to create Dynamic client: %v", err)
			}
			destrinationGVR := schema.GroupVersionResource{Group: destinationRuleGroup, Version: destinationRuleAPIVersion, Resource: destinationRulePlural}
			f.DestinationRuleClient = dynamicClient.Resource(destrinationGVR)
		}
	}
	return f
}

// Framework is the end-to-end test framework.
type Framework struct {
	RestConfig            *rest.Config
	Clientset             *kubernetes.Clientset
	DestinationRuleClient dynamic.NamespaceableResourceInterface
	crdClient             *apiextensionsclient.Clientset
	BackendConfigClient   *backendconfigclient.Clientset
	FrontendConfigClient  *frontendconfigclient.Clientset
	SvcNegClient          *svcnegclient.Clientset
	SAClient              *serviceattachment.Clientset
	Project               string
	Region                string
	Network               string
	Cloud                 cloud.Cloud
	Rand                  *rand.Rand
	statusManager         *StatusManager

	destroySandboxes bool
	CreateILBSubnet  bool

	lock      sync.Mutex
	sandboxes []*Sandbox
}

// SanityCheck the test environment before proceeding.
func (f *Framework) SanityCheck() error {
	klog.V(2).Info("Checking connectivity with Kubernetes API")
	if _, err := f.Clientset.CoreV1().Pods("default").List(context.TODO(), metav1.ListOptions{}); err != nil {
		klog.Errorf("Error accessing Kubernetes API: %v", err)
		return err
	}
	klog.V(2).Infof("Checking connectivity with Google Cloud API (get project %q)", f.Project)
	if _, err := f.Cloud.Projects().Get(context.Background(), f.Project); err != nil {
		klog.Errorf("Error accessing compute API: %v", err)
		return err
	}
	klog.V(2).Info("Checking external Internet connectivity")
	for _, url := range []string{
		"http://www.google.com",
		"http://www.amazon.com",
	} {
		if _, err := http.Get(url); err != nil {
			klog.Errorf("Error in HTTP GET to %q: %v", url, err)
			return err
		}
	}
	klog.V(2).Info("Checking status manager initialization")
	if err := f.statusManager.init(); err != nil {
		klog.Errorf("Error initializing status manager: %v", err)
		return err
	}
	klog.V(2).Info("Waiting for BackendConfig CRD to be established")
	if err := waitForBackendConfigCRDEstablish(f.crdClient); err != nil {
		klog.Errorf("Error waiting for BackendConfig CRD to be established: %v", err)
		return err
	}
	return nil
}

// CatchSIGINT and cleanup sandboxes when the test is interrupted.
func (f *Framework) CatchSIGINT() {
	klog.Infof("Catching SIGINT")

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for range c {
			f.sigintHandler()
		}
	}()
}
func (f *Framework) sigintHandler() {
	klog.Warningf("SIGINT received, shutting down (disable with -handleSIGINT=false)")
	f.shutdown(1)
}

func (f *Framework) shutdown(exitCode int) {
	f.lock.Lock()
	defer f.lock.Unlock()

	if f.destroySandboxes {
		klog.V(2).Infof("Cleaning up sandboxes...")
		for _, s := range f.sandboxes {
			s.Destroy()
		}
	}

	f.statusManager.shutdown()
	os.Exit(exitCode)
}

// WithSandbox runs the testFunc with the Sandbox, taking care of resource
// cleanup and isolation.
func (f *Framework) WithSandbox(testFunc func(*Sandbox) error) error {
	f.lock.Lock()
	sandbox := &Sandbox{
		Namespace: fmt.Sprintf("test-sandbox-%x", f.Rand.Int63()),
		f:         f,
	}
	for _, s := range f.sandboxes {
		if s.Namespace == sandbox.Namespace {
			f.lock.Unlock()
			return fmt.Errorf("sandbox %s was created previously by the framework", s.Namespace)
		}
	}
	klog.V(2).Infof("Using namespace %q for test sandbox", sandbox.Namespace)
	if err := sandbox.Create(); err != nil {
		f.lock.Unlock()
		return err
	}

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
		f.lock.Lock()
		randInt := f.Rand.Int63()
		sandbox := &Sandbox{

			Namespace: fmt.Sprintf("test-sandbox-%x", randInt),
			f:         f,
			RandInt:   randInt,
		}
		for _, s := range f.sandboxes {
			if s.Namespace == sandbox.Namespace {
				f.lock.Unlock()
				t.Fatalf("Sandbox %s was created previously by the framework.", s.Namespace)
			}
		}
		klog.V(2).Infof("Using namespace %q for test sandbox", sandbox.Namespace)
		if err := sandbox.Create(); err != nil {
			f.lock.Unlock()
			t.Fatalf("error creating sandbox: %v", err)
		}

		f.sandboxes = append(f.sandboxes, sandbox)
		f.lock.Unlock()

		if f.destroySandboxes {
			defer sandbox.Destroy()
		}

		defer sandbox.DumpSandboxInfo(t)
		testFunc(t, sandbox)
	})
}

// NewCloud creates a new cloud for the given project.
func NewCloud(project, GceEndpointOverride string) (cloud.Cloud, error) {
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

	if GceEndpointOverride != "" {
		service.BasePath = fmt.Sprintf("%sprojects/", GceEndpointOverride)
		serviceBeta.BasePath = fmt.Sprintf("%sprojects/", strings.Replace(GceEndpointOverride, "v1", "beta", -1))
		serviceAlpha.BasePath = fmt.Sprintf("%sprojects/", strings.Replace(GceEndpointOverride, "v1", "alpha", -1))
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
