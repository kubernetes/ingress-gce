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
	"sync"

	"k8s.io/klog"

	"sort"
	"testing"

	v1 "k8s.io/api/core/v1"
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
	if _, err := s.f.Clientset.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{}); err != nil {
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

// IstioEnabled returns true if Istio is enabled for target cluster.
func (s *Sandbox) IstioEnabled() bool {
	return s.f.DestinationRuleClient != nil
}

// Destroy the sandbox and all resources associated with the sandbox.
func (s *Sandbox) Destroy() {
	klog.V(2).Infof("Destroying test sandbox %q", s.Namespace)

	s.lock.Lock()
	defer s.lock.Unlock()

	if s.destroyed {
		return
	}

	if err := s.f.Clientset.CoreV1().Namespaces().Delete(context.TODO(), s.Namespace, metav1.DeleteOptions{}); err != nil {
		klog.Errorf("Error deleting namespace %q: %v", s.Namespace, err)
	}
	s.destroyed = true
}

// PutStatus into the status manager.
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

// DumpSandboxInfo dumps information about the sandbox into logs
func (s *Sandbox) DumpSandboxInfo(t *testing.T) {
	s.dumpSandboxEvents(t)
}

// dumpSandboxEvents dumps the events happened in the sandbox namespace into logs
func (s *Sandbox) dumpSandboxEvents(t *testing.T) {
	t.Logf("Collecting events from namespace %q.", s.Namespace)
	events, err := s.f.Clientset.CoreV1().Events(s.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Logf("Failed to list events in namespace %q", s.Namespace)
		return
	}
	t.Logf("Found %d events.", len(events.Items))
	// Sort events by their first timestamp
	sortedEvents := events.Items
	if len(sortedEvents) > 1 {
		sort.Sort(byFirstTimestamp(sortedEvents))
	}
	for _, e := range sortedEvents {
		t.Logf("At %v - event for %v/%v: %v %v: %v", e.FirstTimestamp, e.Namespace, e.InvolvedObject.Name, e.Source, e.Reason, e.Message)
	}
}

// byFirstTimestamp sorts a slice of events by first timestamp, using their involvedObject's name as a tie breaker.
type byFirstTimestamp []v1.Event

func (o byFirstTimestamp) Len() int      { return len(o) }
func (o byFirstTimestamp) Swap(i, j int) { o[i], o[j] = o[j], o[i] }

func (o byFirstTimestamp) Less(i, j int) bool {
	if o[i].FirstTimestamp.Equal(&o[j].FirstTimestamp) {
		return o[i].InvolvedObject.Name < o[j].InvolvedObject.Name
	}
	return o[i].FirstTimestamp.Before(&o[j].FirstTimestamp)
}
