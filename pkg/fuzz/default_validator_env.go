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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/ingress-gce/cmd/glbc/app"
	backendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	bcclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils/namer"
)

// DefaultValidatorEnv is a ValidatorEnv that gets data from the Kubernetes
// clientset.
type DefaultValidatorEnv struct {
	ns    string
	k8s   *kubernetes.Clientset
	bc    *bcclient.Clientset
	gce   cloud.Cloud
	namer *namer.Namer
}

// NewDefaultValidatorEnv returns a new ValidatorEnv.
func NewDefaultValidatorEnv(config *rest.Config, ns string, gce cloud.Cloud) (ValidatorEnv, error) {
	ret := &DefaultValidatorEnv{ns: ns, gce: gce}
	var err error
	ret.k8s, err = kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	ret.bc, err = bcclient.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	ret.namer, err = app.NewStaticNamer(ret.k8s, "", "")
	return ret, err
}

// BackendConfigs implements ValidatorEnv.
func (e *DefaultValidatorEnv) BackendConfigs() (map[string]*backendconfig.BackendConfig, error) {
	bcl, err := e.bc.CloudV1beta1().BackendConfigs(e.ns).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	ret := map[string]*backendconfig.BackendConfig{}
	for _, bc := range bcl.Items {
		ret[bc.Name] = bc.DeepCopy()
	}
	return ret, nil
}

// Services implements ValidatorEnv.
func (e *DefaultValidatorEnv) Services() (map[string]*v1.Service, error) {
	sl, err := e.k8s.CoreV1().Services(e.ns).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	ret := map[string]*v1.Service{}
	for _, s := range sl.Items {
		ret[s.Name] = s.DeepCopy()
	}
	return ret, nil
}

// DefaultValidatorEnv implements ValidatorEnv.
func (e *DefaultValidatorEnv) Cloud() cloud.Cloud {
	return e.gce
}

// DefaultValidatorEnv implements ValidatorEnv.
func (e *DefaultValidatorEnv) Namer() *namer.Namer {
	return e.namer
}
