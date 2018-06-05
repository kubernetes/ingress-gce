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
	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// NewFramework returns a new test framework to run.
func NewFramework(config *rest.Config) *Framework {
	return &Framework{
		Clientset: kubernetes.NewForConfigOrDie(config),
	}
}

// Framework is the ent-to-end test framework.
type Framework struct {
	Clientset *kubernetes.Clientset
}

// SanityCheck the test environment before proceeding.
func (f *Framework) SanityCheck() error {
	if _, err := f.Clientset.Core().Pods("default").List(metav1.ListOptions{}); err != nil {
		glog.Errorf("Error talking to the Kubernetes master: %v", err)
		return err
	}
	// TODO: check connectivity etc.
	return nil
}
