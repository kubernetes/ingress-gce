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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/ingress-gce/cmd/glbc/app"
	backendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	bcclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
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
	// feNamerFactory is frontend namer factory that creates frontend naming policy
	// for given ingress/ load-balancer.
	feNamerFactory namer.IngressFrontendNamerFactory
}

// NewDefaultValidatorEnv returns a new ValidatorEnv.
func NewDefaultValidatorEnv(config *rest.Config, ns string, gce cloud.Cloud) (ValidatorEnv, error) {
	ret := &DefaultValidatorEnv{ns: ns, gce: gce}
	var err error
	if ret.k8s, err = kubernetes.NewForConfig(config); err != nil {
		return nil, err
	}
	if ret.bc, err = bcclient.NewForConfig(config); err != nil {
		return nil, err
	}
	if ret.namer, err = app.NewStaticNamer(ret.k8s, "", ""); err != nil {
		return nil, err
	}
	// Get kube-system uid.
	kubeSystemNS, err := ret.k8s.CoreV1().Namespaces().Get(context.TODO(), "kube-system", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	ret.feNamerFactory = namer.NewFrontendNamerFactory(ret.namer, kubeSystemNS.GetUID())
	return ret, nil
}

// BackendConfigs implements ValidatorEnv.
func (e *DefaultValidatorEnv) BackendConfigs() (map[string]*backendconfig.BackendConfig, error) {
	bcCRUD := adapter.BackendConfigCRUD{C: e.bc}
	bcl, err := bcCRUD.List(e.ns)
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
	sl, err := e.k8s.CoreV1().Services(e.ns).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	ret := map[string]*v1.Service{}
	for _, s := range sl.Items {
		ret[s.Name] = s.DeepCopy()
	}
	return ret, nil
}

// Cloud implements ValidatorEnv.
func (e *DefaultValidatorEnv) Cloud() cloud.Cloud {
	return e.gce
}

// Namer implements ValidatorEnv.
func (e *DefaultValidatorEnv) BackendNamer() namer.BackendNamer {
	return e.namer
}

// DefaultValidatorEnv implements ValidatorEnv.
func (e *DefaultValidatorEnv) FrontendNamerFactory() namer.IngressFrontendNamerFactory {
	return e.feNamerFactory
}
