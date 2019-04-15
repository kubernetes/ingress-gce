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

package fuzz

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	backendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

// pathForDefaultBackend is a unique string that will not match any path.
const pathForDefaultBackend = "/edeaaff3f1774ad2888673770c6d64097e391bc362d7d6fb34982ddf0efd18cb"

// ValidatorEnv captures non-Ingress spec related environment that affect the
// set of validations and Features.
type ValidatorEnv interface {
	BackendConfigs() (map[string]*backendconfig.BackendConfig, error)
	Services() (map[string]*v1.Service, error)
	Cloud() cloud.Cloud
	Namer() *utils.Namer
}

// MockValidatorEnv is an environment that is used for mock testing.
type MockValidatorEnv struct {
	BackendConfigsMap map[string]*backendconfig.BackendConfig
	ServicesMap       map[string]*v1.Service
	MockCloud         *cloud.MockGCE
	IngressNamer      *utils.Namer
}

// BackendConfigs implements ValidatorEnv.
func (e *MockValidatorEnv) BackendConfigs() (map[string]*backendconfig.BackendConfig, error) {
	return e.BackendConfigsMap, nil
}

// Services implements ValidatorEnv.
func (e *MockValidatorEnv) Services() (map[string]*v1.Service, error) {
	return e.ServicesMap, nil
}

// Cloud implements ValidatorEnv.
func (e *MockValidatorEnv) Cloud() cloud.Cloud {
	return e.MockCloud
}

// Cloud implements ValidatorEnv.
func (e *MockValidatorEnv) Namer() *utils.Namer {
	return e.IngressNamer
}

// IngressValidatorAttributes are derived attributes governing how the Ingress
// is validated. Features will use this structure to express changes to the
// standard checks by modifying this struct.
type IngressValidatorAttributes struct {
	CheckHTTP           bool
	CheckHTTPS          bool
	RejectInsecureCerts bool
	RequestTimeout      time.Duration
	// HTTPPort and HTTPSPort are used only for unit testing.
	HTTPPort  int
	HTTPSPort int
}

func (a *IngressValidatorAttributes) equal(b *IngressValidatorAttributes) bool {
	return *a == *b
}

func (a *IngressValidatorAttributes) clone() *IngressValidatorAttributes {
	var ret IngressValidatorAttributes
	ret = *a
	return &ret
}

func (a *IngressValidatorAttributes) schemes() []string {
	var ret []string
	if a.CheckHTTP {
		ret = append(ret, "http")
	}
	if a.CheckHTTPS {
		ret = append(ret, "https")
	}
	return ret
}

// baseAttributes apply settings for the vanilla Ingress spec.
func (a *IngressValidatorAttributes) baseAttributes(ing *v1beta1.Ingress) {
	a.CheckHTTP = true
	if len(ing.Spec.TLS) != 0 {
		a.CheckHTTPS = true
	}
}

// applyFeatures applies the settings for each of the additional features.
func (a *IngressValidatorAttributes) applyFeatures(env ValidatorEnv, ing *v1beta1.Ingress, features []FeatureValidator) error {
	for _, f := range features {
		klog.V(4).Infof("Applying feature %q", f.Name())
		if err := f.ConfigureAttributes(env, ing, a); err != nil {
			klog.Warningf("Feature %q could not be applied: %v", f.Name(), err)
			return err
		}
	}
	// Try to configure attributes again; no additional changes should occur.
	// If changes are detected, one of the features as written is not
	// commutative and should be fixed.
	copy := a.clone()
	for _, f := range features {
		if err := f.ConfigureAttributes(env, ing, copy); err != nil {
			return err
		}
		if !a.equal(copy) {
			klog.Errorf("Feature %q is unstable generating attributes, %+v becomes %+v", f.Name(), *a, *copy)
			return fmt.Errorf("feature %q is unstable generating attributes, %+v becomes %+v", f.Name(), *a, *copy)
		}
	}
	return nil
}

// IngressResult is the result of an Ingress validation.
type IngressResult struct {
	Err   error
	Paths []*PathResult
}

// PathResult is the result of validating a path.
type PathResult struct {
	Scheme string
	Host   string
	Path   string
	Err    error
}

// defaultAttributes are the base attributes for validation.
func defaultAttributes() *IngressValidatorAttributes {
	return &IngressValidatorAttributes{
		CheckHTTP:      true,
		CheckHTTPS:     false,
		HTTPPort:       80,
		HTTPSPort:      443,
		RequestTimeout: 1 * time.Second,
	}
}

// NewIngressValidator returns a new validator for checking the correctness of
// an Ingress spec against the behavior of the instantiated load balancer.
// If attribs is nil, then the default set of attributes will be used.
func NewIngressValidator(env ValidatorEnv, ing *v1beta1.Ingress, features []Feature, attribs *IngressValidatorAttributes) (*IngressValidator, error) {
	var fvs []FeatureValidator
	for _, f := range features {
		fvs = append(fvs, f.NewValidator())
	}

	if attribs == nil {
		attribs = defaultAttributes()
	}
	attribs.baseAttributes(ing)
	if err := attribs.applyFeatures(env, ing, fvs); err != nil {
		return nil, err
	}
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &IngressValidator{
		ing:      ing,
		features: fvs,
		attribs:  attribs,
		client:   client,
	}, nil
}

// IngressValidator encapsulates the logic required to validate a given configuration
// is behaving correctly.
type IngressValidator struct {
	ing      *v1beta1.Ingress
	features []FeatureValidator

	attribs *IngressValidatorAttributes
	client  *http.Client
}

// the right SSL certificate is presented
// each path, each host returns the right contents

// vip for the load balancer. This currently uses the first entry, returns nil
// if the VIP is not available.
func (v *IngressValidator) vip() *string {
	statuses := v.ing.Status.LoadBalancer.Ingress
	if len(statuses) == 0 {
		return nil
	}
	ret := statuses[0].IP
	return &ret
}

// Check runs all of the checks against the instantiated load balancer.
func (v *IngressValidator) Check(ctx context.Context) *IngressResult {
	klog.V(3).Infof("Check Ingress %s/%s attribs=%+v", v.ing.Namespace, v.ing.Name, v.attribs)
	ret := &IngressResult{}
	ret.Err = v.CheckPaths(ctx, ret)
	return ret
}

// CheckPaths checks the host, paths that have been configured. Checks are
// run in parallel.
func (v *IngressValidator) CheckPaths(ctx context.Context, vr *IngressResult) error {
	var (
		thunks []func()
		wg     sync.WaitGroup
	)
	for _, scheme := range v.attribs.schemes() {
		if v.ing.Spec.Backend != nil {
			klog.V(2).Infof("Checking default backend for Ingress %s/%s", v.ing.Namespace, v.ing.Name)
			// Capture variables for the thunk.
			result := &PathResult{Scheme: scheme}
			vr.Paths = append(vr.Paths, result)
			scheme := scheme
			ctx, cancelFunc := context.WithTimeout(ctx, v.attribs.RequestTimeout)
			defer cancelFunc()
			f := func() {
				result.Err = v.checkPath(ctx, scheme, "", pathForDefaultBackend)
				wg.Done()
			}
			thunks = append(thunks, f)
			wg.Add(1)
		}

		for _, rule := range v.ing.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}
			for _, path := range rule.HTTP.Paths {
				// Capture variables for the thunk.
				result := &PathResult{Scheme: scheme, Host: rule.Host, Path: path.Path}
				vr.Paths = append(vr.Paths, result)
				scheme, host, path := scheme, rule.Host, path.Path
				ctx, cancelFunc := context.WithTimeout(ctx, v.attribs.RequestTimeout)
				defer cancelFunc()
				f := func() {
					result.Err = v.checkPath(ctx, scheme, host, path)
					wg.Done()
				}
				thunks = append(thunks, f)
				wg.Add(1)
			}
		}
	}

	for _, f := range thunks {
		go f()
	}
	klog.V(2).Infof("Waiting for path checks for Ingress %s/%s to finish", v.ing.Namespace, v.ing.Name)
	wg.Wait()

	for _, r := range vr.Paths {
		if r.Err != nil {
			klog.V(2).Infof("Got an error checking paths for Ingress %s/%s: %v", v.ing.Namespace, v.ing.Name, r.Err)
			return r.Err
		}
	}

	return nil
}

// checkPath performs a check for scheme://host/path.
func (v *IngressValidator) checkPath(ctx context.Context, scheme, host, path string) error {
	if v.vip() == nil {
		return fmt.Errorf("ingress %s/%s does not have a VIP", v.ing.Namespace, v.ing.Name)
	}
	vip := *v.vip()

	url := fmt.Sprintf("%s://%s%s%s", scheme, vip, portStr(v.attribs, scheme), path)
	klog.V(3).Infof("Checking Ingress %s/%s url=%q", v.ing.Namespace, v.ing.Name, url)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	if host != "" {
		req.Host = host
	}
	req = req.WithContext(ctx)

	// Apply modifications for the features.
	for _, f := range v.features {
		f.ModifyRequest(host, path, req)
	}

	klog.V(3).Infof("Request is %+v", *req)

	resp, err := v.client.Do(req)
	if err != nil && err != http.ErrUseLastResponse {
		klog.Infof("Ingress %s/%s: %v", v.ing.Namespace, v.ing.Name, err)
		return err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		klog.Infof("Ingress %s/%s reading body: %v", v.ing.Namespace, v.ing.Name, err)
		return err
	}

	klog.V(2).Infof("Ingress %s/%s GET %q: %d (%d bytes)", v.ing.Namespace, v.ing.Name, url, resp.StatusCode, len(body))

	doStandardCheck := true
	// Perform the checks for each of the features.
	for _, f := range v.features {
		action, err := f.CheckResponse(host, path, resp, body)
		if err != nil {
			return err
		}
		switch action {
		case CheckResponseContinue:
		case CheckResponseSkip:
			doStandardCheck = false
		}
	}

	if doStandardCheck && resp.StatusCode != 200 {
		return fmt.Errorf("ingress %s/%s: GET %q: %d, want 200", v.ing.Namespace, v.ing.Name, url, resp.StatusCode)
	}

	return nil
}

// portStr returns the ":<port>" for the given scheme. If the port is default
// or scheme is unknown then "" will be returned.
func portStr(a *IngressValidatorAttributes, scheme string) string {
	switch scheme {
	case "http":
		if a.HTTPPort == 80 {
			return ""
		}
		return fmt.Sprintf(":%d", a.HTTPPort)
	case "https":
		if a.HTTPSPort == 443 {
			return ""
		}
		return fmt.Sprintf(":%d", a.HTTPSPort)
	}
	return ""
}
