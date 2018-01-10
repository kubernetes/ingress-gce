/*
Copyright 2017 The Kubernetes Authors.

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

package neg

import (
	"testing"
	"time"

	compute "google.golang.org/api/compute/v0.alpha"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	CluseterID = "clusterid"
)

func NewTestSyncerManager(kubeClient kubernetes.Interface) *syncerManager {
	context := context.NewControllerContext(kubeClient, apiv1.NamespaceAll, 1*time.Second, true)
	manager := newSyncerManager(
		utils.NewNamer(CluseterID, ""),
		record.NewFakeRecorder(100),
		NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network"),
		NewFakeZoneGetter(),
		context.ServiceInformer.GetIndexer(),
		context.EndpointInformer.GetIndexer(),
	)
	return manager
}

func TestEnsureAndStopSyncer(t *testing.T) {
	testCases := []struct {
		namespace string
		name      string
		ports     sets.String
		stop      bool
		expect    []servicePort // keys of running syncers
	}{
		{
			"ns1",
			"n1",
			sets.NewString("80", "443"),
			false,
			[]servicePort{
				getSyncerKey("ns1", "n1", "80"),
				getSyncerKey("ns1", "n1", "443"),
			},
		},
		{
			"ns1",
			"n1",
			sets.NewString("80", "namedport"),
			false,
			[]servicePort{
				getSyncerKey("ns1", "n1", "80"),
				getSyncerKey("ns1", "n1", "namedport"),
			},
		},
		{
			"ns2",
			"n1",
			sets.NewString("80"),
			false,
			[]servicePort{
				getSyncerKey("ns1", "n1", "80"),
				getSyncerKey("ns1", "n1", "namedport"),
				getSyncerKey("ns2", "n1", "80"),
			},
		},
		{
			"ns1",
			"n1",
			sets.NewString(),
			true,
			[]servicePort{
				getSyncerKey("ns2", "n1", "80"),
			},
		},
	}

	manager := NewTestSyncerManager(fake.NewSimpleClientset())
	for _, tc := range testCases {
		if tc.stop {
			manager.StopSyncer(tc.namespace, tc.name)
		} else {
			if err := manager.EnsureSyncers(tc.namespace, tc.name, tc.ports); err != nil {
				t.Errorf("Failed to ensure syncer %s/%s-%v: %v", tc.namespace, tc.name, tc.ports, err)
			}
		}

		for _, key := range tc.expect {
			syncer, ok := manager.syncerMap[key]
			if !ok {
				t.Errorf("Expect syncer key %q to be present.", key)
				continue
			}
			if syncer.IsStopped() || syncer.IsShuttingDown() {
				t.Errorf("Expect syncer %q to be running.", key)
			}
		}
		for key, syncer := range manager.syncerMap {
			found := false
			for _, k := range tc.expect {
				if k == key {
					found = true
					break
				}
			}
			if found {
				continue
			}
			if !syncer.IsStopped() {
				t.Errorf("Expect syncer %q to be stopped.", key)
			}
		}
	}

	// make sure there is no leaking go routine
	manager.StopSyncer("ns1", "n1")
	manager.StopSyncer("ns2", "n1")
}

func TestGarbageCollectionSyncer(t *testing.T) {
	manager := NewTestSyncerManager(fake.NewSimpleClientset())
	if err := manager.EnsureSyncers("ns1", "n1", sets.NewString("80", "namedport")); err != nil {
		t.Fatalf("Failed to ensure syncer: %v", err)
	}
	manager.StopSyncer("ns1", "n1")

	syncer1 := manager.syncerMap[getSyncerKey("ns1", "n1", "80")]
	syncer2 := manager.syncerMap[getSyncerKey("ns1", "n1", "namedport")]

	if err := wait.PollImmediate(time.Second, 30*time.Second, func() (bool, error) {
		return !syncer1.IsShuttingDown() && syncer1.IsStopped() && !syncer2.IsShuttingDown() && syncer2.IsStopped(), nil
	}); err != nil {
		t.Fatalf("Syncer failed to shutdown: %v", err)
	}

	if err := manager.GC(); err != nil {
		t.Fatalf("Failed to GC: %v", err)
	}

	if len(manager.syncerMap) != 0 {
		t.Fatalf("Expect 0 syncers left, but got %v", len(manager.syncerMap))
	}
}

func TestGarbageCollectionNEG(t *testing.T) {
	kubeClient := fake.NewSimpleClientset()
	if _, err := kubeClient.Core().Endpoints(ServiceNamespace).Create(getDefaultEndpoint()); err != nil {
		t.Fatalf("Failed to create endpoint: %v", err)
	}
	manager := NewTestSyncerManager(kubeClient)
	if err := manager.EnsureSyncers(ServiceNamespace, ServiceName, sets.NewString("80")); err != nil {
		t.Fatalf("Failed to ensure syncer: %v", err)
	}

	negName := manager.namer.NEG("test", "test", "80")
	manager.cloud.CreateNetworkEndpointGroup(&compute.NetworkEndpointGroup{
		Name: negName,
	}, TestZone1)

	if err := manager.GC(); err != nil {
		t.Fatalf("Failed to GC: %v", err)
	}

	negs, _ := manager.cloud.ListNetworkEndpointGroup(TestZone1)
	for _, neg := range negs {
		if neg.Name == negName {
			t.Errorf("Expect NEG %q to be GCed.", negName)
		}
	}

	// make sure there is no leaking go routine
	manager.StopSyncer(ServiceNamespace, ServiceName)
}
