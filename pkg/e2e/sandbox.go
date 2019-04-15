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
	"sync"

	"k8s.io/klog"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/fuzz"
)

// Sandbox represents a sandbox for running tests in a Kubernetes cluster.
type Sandbox struct {
	// Namespace to create resources in. Resources created in this namespace
	// will be deleted with Destroy().
	Namespace string
	// ValidatorEnv for use with the test.
	ValidatorEnv fuzz.ValidatorEnv

	lock      sync.Mutex
	f         *Framework
	destroyed bool
}

// Create the sandbox.
func (s *Sandbox) Create() error {
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: s.Namespace,
		},
	}
	if _, err := s.f.Clientset.CoreV1().Namespaces().Create(ns); err != nil {
		klog.Errorf("Error creating namespace %q: %v", s.Namespace, err)
		return err
	}

	var err error
	s.ValidatorEnv, err = fuzz.NewDefaultValidatorEnv(s.f.RestConfig, s.Namespace, s.f.Cloud)
	if err != nil {
		klog.Errorf("Error creating validator env for namespace %q: %v", s.Namespace, err)
		return err
	}

	return nil
}

// Destroy the sandbox and all resources associated with the sandbox.
func (s *Sandbox) Destroy() {
	klog.V(2).Infof("Destroying test sandbox %q", s.Namespace)

	s.lock.Lock()
	defer s.lock.Unlock()

	if s.destroyed {
		return
	}

	if err := s.f.Clientset.CoreV1().Namespaces().Delete(s.Namespace, &metav1.DeleteOptions{}); err != nil {
		klog.Errorf("Error deleting namespace %q: %v", s.Namespace, err)
	}
	s.destroyed = true
}

func (s *Sandbox) PutStatus(status IngressStability) {
	s.f.statusManager.putStatus(s.Namespace, status)
}

// MasterUpgraded checks the config map for whether or not the k8s master has
// successfully finished upgrading or not
func (s *Sandbox) MasterUpgraded() bool {
	return s.f.statusManager.masterUpgraded()
}

// MasterUpgrading checks the config map for whether or not the k8s master has
// successfully finished upgrading or not
func (s *Sandbox) MasterUpgrading() bool {
	return s.f.statusManager.masterUpgrading()
}
