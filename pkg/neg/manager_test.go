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

	compute "google.golang.org/api/compute/v0.beta"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/neg/syncers"
	"k8s.io/ingress-gce/pkg/neg/types"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	ClusterID = "clusterid"
)

func NewTestSyncerManager(kubeClient kubernetes.Interface) *syncerManager {
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	namer := utils.NewNamer(ClusterID, "")
	ctxConfig := context.ControllerContextConfig{
		Namespace:               apiv1.NamespaceAll,
		ResyncPeriod:            1 * time.Second,
		DefaultBackendSvcPortID: defaultBackend,
	}
	context := context.NewControllerContext(kubeClient, backendConfigClient, nil, namer, ctxConfig)
	manager := newSyncerManager(
		namer,
		record.NewFakeRecorder(100),
		negtypes.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network"),
		negtypes.NewFakeZoneGetter(),
		context.ServiceInformer.GetIndexer(),
		context.EndpointInformer.GetIndexer(),
		transactionSyncer,
	)
	return manager
}

func TestEnsureAndStopSyncer(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		namespace string
		name      string
		ports     types.SvcPortMap
		stop      bool
		expect    []syncers.NegSyncerKey // keys of running syncers
	}{
		{
			"ns1",
			"n1",
			types.SvcPortMap{1000: "80", 2000: "443"},
			false,
			[]syncers.NegSyncerKey{
				getSyncerKey("ns1", "n1", 1000, "80"),
				getSyncerKey("ns1", "n1", 2000, "443"),
			},
		},
		{
			"ns1",
			"n1",
			types.SvcPortMap{3000: "80", 4000: "namedport"},
			false,
			[]syncers.NegSyncerKey{
				getSyncerKey("ns1", "n1", 3000, "80"),
				getSyncerKey("ns1", "n1", 4000, "namedport"),
			},
		},
		{
			"ns2",
			"n1",
			types.SvcPortMap{3000: "80"},
			false,
			[]syncers.NegSyncerKey{
				getSyncerKey("ns1", "n1", 3000, "80"),
				getSyncerKey("ns1", "n1", 4000, "namedport"),
				getSyncerKey("ns2", "n1", 3000, "80"),
			},
		},
		{
			"ns1",
			"n1",
			types.SvcPortMap{},
			true,
			[]syncers.NegSyncerKey{
				getSyncerKey("ns2", "n1", 3000, "80"),
			},
		},
	}
	manager := NewTestSyncerManager(fake.NewSimpleClientset())
	namer := manager.namer
	for _, tc := range testCases {
		if tc.stop {
			manager.StopSyncer(tc.namespace, tc.name)
		} else {
			portInfoMap := negtypes.NewPortInfoMap(tc.namespace, tc.name, tc.ports, namer)
			if err := manager.EnsureSyncers(tc.namespace, tc.name, portInfoMap); err != nil {
				t.Errorf("Failed to ensure syncer %s/%s-%v: %v", tc.namespace, tc.name, tc.ports, err)
			}
		}

		for _, key := range tc.expect {
			syncer, ok := manager.syncerMap[key]
			if !ok {
				t.Errorf("Expect syncer key %+v to be present.", key)
				continue
			}
			if syncer.IsStopped() || syncer.IsShuttingDown() {
				t.Errorf("Expect syncer %+v to be running.", key)
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
				t.Errorf("Expect syncer %+v to be stopped.", key)
			}
		}
	}

	// make sure there is no leaking go routine
	manager.StopSyncer("ns1", "n1")
	manager.StopSyncer("ns2", "n1")
}

func TestGarbageCollectionSyncer(t *testing.T) {
	t.Parallel()

	manager := NewTestSyncerManager(fake.NewSimpleClientset())
	namer := manager.namer
	namespace := testServiceNamespace
	name := testServiceName
	svcPort1 := int32(3000)
	svcPort2 := int32(4000)
	targetPort1 := "80"
	targetPort2 := "namedport"
	portMap := make(types.PortInfoMap)
	portMap[svcPort1] = types.PortInfo{TargetPort: targetPort1, NegName: namer.NEG(namespace, name, svcPort1)}
	portMap[svcPort2] = types.PortInfo{TargetPort: targetPort2, NegName: namer.NEG(namespace, name, svcPort2)}

	if err := manager.EnsureSyncers(namespace, name, portMap); err != nil {
		t.Fatalf("Failed to ensure syncer: %v", err)
	}
	manager.StopSyncer(namespace, name)

	syncer1 := manager.syncerMap[getSyncerKey(namespace, name, svcPort1, targetPort1)]
	syncer2 := manager.syncerMap[getSyncerKey(namespace, name, svcPort2, targetPort2)]

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
	t.Parallel()

	kubeClient := fake.NewSimpleClientset()
	if _, err := kubeClient.CoreV1().Endpoints(testServiceNamespace).Create(getDefaultEndpoint()); err != nil {
		t.Fatalf("Failed to create endpoint: %v", err)
	}
	manager := NewTestSyncerManager(kubeClient)
	svcPort := int32(80)
	ports := make(types.PortInfoMap)
	ports[svcPort] = types.PortInfo{TargetPort: "namedport", NegName: manager.namer.NEG(testServiceNamespace, testServiceName, svcPort)}
	if err := manager.EnsureSyncers(testServiceNamespace, testServiceName, ports); err != nil {
		t.Fatalf("Failed to ensure syncer: %v", err)
	}

	negName := manager.namer.NEG("test", "test", 80)
	manager.cloud.CreateNetworkEndpointGroup(&compute.NetworkEndpointGroup{
		Name: negName,
	}, negtypes.TestZone1)

	if err := manager.GC(); err != nil {
		t.Fatalf("Failed to GC: %v", err)
	}

	negs, _ := manager.cloud.ListNetworkEndpointGroup(negtypes.TestZone1)
	for _, neg := range negs {
		if neg.Name == negName {
			t.Errorf("Expect NEG %q to be GCed.", negName)
		}
	}

	// make sure there is no leaking go routine
	manager.StopSyncer(testServiceNamespace, testServiceName)
}

func getDefaultEndpoint() *apiv1.Endpoints {
	instance1 := negtypes.TestInstance1
	instance2 := negtypes.TestInstance2
	instance3 := negtypes.TestInstance3
	instance4 := negtypes.TestInstance4
	return &apiv1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testServiceName,
			Namespace: testServiceNamespace,
		},
		Subsets: []apiv1.EndpointSubset{
			{
				Addresses: []apiv1.EndpointAddress{
					{
						IP:       "10.100.1.1",
						NodeName: &instance1,
					},
					{
						IP:       "10.100.1.2",
						NodeName: &instance1,
					},
					{
						IP:       "10.100.2.1",
						NodeName: &instance2,
					},
					{
						IP:       "10.100.3.1",
						NodeName: &instance3,
					},
				},
				Ports: []apiv1.EndpointPort{
					{
						Name:     "",
						Port:     int32(80),
						Protocol: apiv1.ProtocolTCP,
					},
				},
			},
			{
				Addresses: []apiv1.EndpointAddress{
					{
						IP:       "10.100.2.2",
						NodeName: &instance2,
					},
					{
						IP:       "10.100.4.1",
						NodeName: &instance4,
					},
				},
				Ports: []apiv1.EndpointPort{
					{
						Name:     testNamedPort,
						Port:     int32(81),
						Protocol: apiv1.ProtocolTCP,
					},
				},
			},
			{
				Addresses: []apiv1.EndpointAddress{
					{
						IP:       "10.100.3.2",
						NodeName: &instance3,
					},
					{
						IP:       "10.100.4.2",
						NodeName: &instance4,
					},
				},
				Ports: []apiv1.EndpointPort{
					{
						Name:     testNamedPort,
						Port:     int32(8081),
						Protocol: apiv1.ProtocolTCP,
					},
				},
			},
		},
	}
}
