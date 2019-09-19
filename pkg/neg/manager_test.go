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
	"reflect"
	"testing"
	"time"

	"google.golang.org/api/compute/v1"
	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/neg/readiness"
	"k8s.io/ingress-gce/pkg/neg/types"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	ClusterID = "clusterid"

	namespace1 = "ns1"
	namespace2 = "ns2"
	name1      = "svc1"
	name2      = "svc2"
	name3      = "svc3"

	port1       = int32(1000)
	port2       = int32(2000)
	port3       = int32(3000)
	port4       = int32(4000)
	targetPort1 = "80"
	targetPort2 = "443"
	targetPort3 = "namedport"
	targetPort4 = "4000"
	labelKey1   = "l1"
	labelKey2   = "l2"
	labelValue1 = "v1"
	labelValue2 = "v2"
)

func NewTestSyncerManager(kubeClient kubernetes.Interface) *syncerManager {
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	namer := namer_util.NewNamer(ClusterID, "")
	ctxConfig := context.ControllerContextConfig{
		Namespace:             apiv1.NamespaceAll,
		ResyncPeriod:          1 * time.Second,
		DefaultBackendSvcPort: defaultBackend,
	}
	context := context.NewControllerContext(kubeClient, nil, backendConfigClient, nil, gce.NewFakeGCECloud(gce.DefaultTestClusterValues()), namer, ctxConfig)

	manager := newSyncerManager(
		namer,
		record.NewFakeRecorder(100),
		negtypes.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-network"),
		negtypes.NewFakeZoneGetter(),
		context.PodInformer.GetIndexer(),
		context.ServiceInformer.GetIndexer(),
		context.EndpointInformer.GetIndexer(),
		transactionSyncer,
	)
	manager.reflector = readiness.NewReadinessReflector(context, manager)
	return manager
}

func TestEnsureAndStopSyncer(t *testing.T) {
	t.Parallel()

	manager := NewTestSyncerManager(fake.NewSimpleClientset())
	namer := manager.namer

	svcName := "n1"
	svcNamespace1 := "ns1"
	svcNamespace2 := "ns2"
	testCases := []struct {
		desc        string
		namespace   string
		name        string
		portInfoMap negtypes.PortInfoMap
		stop        bool
		// NegSyncerKey -> readinessGate
		expectInternals   map[negtypes.NegSyncerKey]bool
		expectEnsureError bool
	}{
		{
			desc:        "add 2 new ports",
			namespace:   svcNamespace1,
			name:        svcName,
			portInfoMap: negtypes.NewPortInfoMap(svcNamespace1, svcName, types.SvcPortMap{1000: "80", 2000: "443"}, namer, false),
			stop:        false,
			expectInternals: map[negtypes.NegSyncerKey]bool{
				getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 1000, Subset: ""}, negtypes.PortInfo{TargetPort: "80"}):  false,
				getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 2000, Subset: ""}, negtypes.PortInfo{TargetPort: "443"}): false,
			},
		},
		{
			desc:        "modify 1 port to enable readinessGate",
			namespace:   svcNamespace1,
			name:        svcName,
			portInfoMap: portInfoUnion(negtypes.NewPortInfoMap(svcNamespace1, svcName, types.SvcPortMap{1000: "80"}, namer, false), negtypes.NewPortInfoMap(svcNamespace1, svcName, types.SvcPortMap{2000: "443"}, namer, true)),
			stop:        false,
			expectInternals: map[negtypes.NegSyncerKey]bool{
				getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 1000, Subset: ""}, negtypes.PortInfo{TargetPort: "80"}):  false,
				getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 2000, Subset: ""}, negtypes.PortInfo{TargetPort: "443"}): true,
			},
			expectEnsureError: true,
		},
		{
			desc:        "add 2 new ports, remove 2 existing ports",
			namespace:   svcNamespace1,
			name:        svcName,
			portInfoMap: negtypes.NewPortInfoMap(svcNamespace1, svcName, types.SvcPortMap{3000: "80", 4000: "namedport"}, namer, false),
			stop:        false,
			expectInternals: map[negtypes.NegSyncerKey]bool{
				getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{TargetPort: "80"}):        false,
				getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 4000, Subset: ""}, negtypes.PortInfo{TargetPort: "namedport"}): false,
			},
		},
		{
			desc:        "modify 2 existing ports to enable readinessGate",
			namespace:   svcNamespace1,
			name:        svcName,
			portInfoMap: negtypes.NewPortInfoMap(svcNamespace1, svcName, types.SvcPortMap{3000: "80", 4000: "namedport"}, namer, true),
			stop:        false,
			expectInternals: map[negtypes.NegSyncerKey]bool{
				getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{TargetPort: "80"}):        true,
				getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 4000, Subset: ""}, negtypes.PortInfo{TargetPort: "namedport"}): true,
			},
		},
		{
			desc:        "add 1 new port for a different service",
			namespace:   svcNamespace2,
			name:        svcName,
			portInfoMap: negtypes.NewPortInfoMap(svcNamespace2, svcName, types.SvcPortMap{3000: "80"}, namer, false),
			stop:        false,
			expectInternals: map[negtypes.NegSyncerKey]bool{
				getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{TargetPort: "80"}):        true,
				getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 4000, Subset: ""}, negtypes.PortInfo{TargetPort: "namedport"}): true,
				getSyncerKey(svcNamespace2, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{TargetPort: "80"}):        false,
			},
		},
		{
			desc:        "change target port of 1 existing port",
			namespace:   svcNamespace1,
			name:        svcName,
			portInfoMap: negtypes.NewPortInfoMap(svcNamespace1, svcName, types.SvcPortMap{3000: "80", 4000: "443"}, namer, true),
			stop:        false,
			expectInternals: map[negtypes.NegSyncerKey]bool{
				getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{TargetPort: "80"}):  true,
				getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 4000, Subset: ""}, negtypes.PortInfo{TargetPort: "443"}): true,
				getSyncerKey(svcNamespace2, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{TargetPort: "80"}):  false,
			},
			expectEnsureError: true,
		},
		{
			desc:      "remove all ports for ns1/n1 service",
			namespace: svcNamespace1,
			name:      svcName,
			stop:      true,
			expectInternals: map[negtypes.NegSyncerKey]bool{
				getSyncerKey(svcNamespace2, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{TargetPort: "80"}): false,
			},
		},
	}

	for _, tc := range testCases {
		if tc.stop {
			manager.StopSyncer(tc.namespace, tc.name)
		} else {
			if tc.expectEnsureError {
				if err := wait.Poll(time.Second, 10*time.Second, func() (bool, error) {
					if err := manager.EnsureSyncers(tc.namespace, tc.name, tc.portInfoMap); err != nil {
						return false, nil
					}
					return true, nil
				}); err != nil {
					t.Errorf("For case %q, timeout waiting for ensure syncer %s/%s-%v: %v", tc.desc, tc.namespace, tc.name, tc.portInfoMap, err)
				}
			} else {
				// Expect EnsureSyncers returns successfully immediately
				if err := manager.EnsureSyncers(tc.namespace, tc.name, tc.portInfoMap); err != nil {
					t.Errorf("For case %q, failed to ensure syncer %s/%s-%v: %v", tc.desc, tc.namespace, tc.name, tc.portInfoMap, err)
				}
			}
		}

		// check if all expected syncers are present
		for key, readinessGate := range tc.expectInternals {
			syncer, ok := manager.syncerMap[key]
			if !ok {
				t.Errorf("For case %q, expect syncer key %+v to be present.", tc.desc, key)
				continue
			}
			if syncer.IsStopped() || syncer.IsShuttingDown() {
				t.Errorf("For case %q, expect syncer %+v to be running.", tc.desc, key)
			}

			// validate portInfo
			svcKey := serviceKey{namespace: key.Namespace, name: key.Name}
			if portInfoMap, svcFound := manager.svcPortMap[svcKey]; svcFound {
				if info, portFound := portInfoMap[negtypes.PortInfoMapKey{ServicePort: key.Port, Subset: ""}]; portFound {
					if info.ReadinessGate != readinessGate {
						t.Errorf("For case %q, expect readinessGate of key %q to be %v, but got %v", tc.desc, key.String(), readinessGate, info.ReadinessGate)
					}

					expectNegName := namer.NEG(key.Namespace, key.Name, key.Port)
					if info.NegName != expectNegName {
						t.Errorf("For case %q, expect NEG name %q, but got %q", tc.desc, expectNegName, info.NegName)
					}

				} else {
					t.Errorf("For case %q, expect port %d of service %q to be registered", tc.desc, key.Port, svcKey.Key())
				}
			} else {
				t.Errorf("For case %q, expect service key %q to be registered", tc.desc, svcKey.Key())
			}
		}

		// check if the there is unexpected syncer
		for key, syncer := range manager.syncerMap {
			found := false
			for k := range tc.expectInternals {
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
	manager.StopSyncer(svcNamespace1, svcName)
	manager.StopSyncer(svcNamespace2, svcName)
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
	portMap[negtypes.PortInfoMapKey{ServicePort: svcPort1, Subset: ""}] = types.PortInfo{TargetPort: targetPort1, NegName: namer.NEG(namespace, name, svcPort1)}
	portMap[negtypes.PortInfoMapKey{ServicePort: svcPort2, Subset: ""}] = types.PortInfo{TargetPort: targetPort2, NegName: namer.NEG(namespace, name, svcPort2)}

	if err := manager.EnsureSyncers(namespace, name, portMap); err != nil {
		t.Fatalf("Failed to ensure syncer: %v", err)
	}
	manager.StopSyncer(namespace, name)

	syncer1 := manager.syncerMap[getSyncerKey(namespace, name, negtypes.PortInfoMapKey{ServicePort: svcPort1, Subset: ""}, negtypes.PortInfo{TargetPort: targetPort1})]
	syncer2 := manager.syncerMap[getSyncerKey(namespace, name, negtypes.PortInfoMapKey{ServicePort: svcPort2, Subset: ""}, negtypes.PortInfo{TargetPort: targetPort2})]

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
	ports[negtypes.PortInfoMapKey{ServicePort: svcPort, Subset: ""}] = types.PortInfo{TargetPort: "namedport", NegName: manager.namer.NEG(testServiceNamespace, testServiceName, svcPort)}
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

func TestReadinessGateEnabledNegs(t *testing.T) {
	t.Parallel()

	kubeClient := fake.NewSimpleClientset()
	manager := NewTestSyncerManager(kubeClient)
	populateSyncerManager(manager, kubeClient)

	testCases := []struct {
		desc      string
		namespace string
		labels    map[string]string
		expect    sets.String
	}{
		{
			desc:   "empty input 1",
			expect: sets.NewString(),
		},
		{
			desc:      "empty input 2",
			namespace: namespace1,
			expect:    sets.NewString(),
		},
		{
			desc:      "does not match 1",
			namespace: namespace1,
			labels:    map[string]string{labelKey1: labelValue2},
			expect:    sets.NewString(),
		},
		{
			desc:      "does not match 2",
			namespace: namespace2,
			labels:    map[string]string{labelKey1: "not-matching"},
			expect:    sets.NewString(),
		},
		{
			desc:      "does not match 3",
			namespace: namespace2,
			labels:    map[string]string{labelKey2: labelValue1},
			expect:    sets.NewString(),
		},
		{
			desc:      "match service ns2/svc1 but not enabled",
			namespace: namespace2,
			labels:    map[string]string{labelKey1: labelValue1},
			expect:    sets.NewString(),
		},
		{
			desc:      "match service ns1/svc1 and enabled 1",
			namespace: namespace1,
			labels:    map[string]string{labelKey1: labelValue1},
			expect: sets.NewString(
				"k8s1-clusteri-ns1-svc1-3000-03eb18a3",
				"k8s1-clusteri-ns1-svc1-4000-2afaa36d"),
		},
		{
			desc:      "match 2 services ns2/svc1, ns2/svc2 and enabled 3 ports",
			namespace: namespace2,
			labels:    map[string]string{labelKey1: labelValue1, labelKey2: labelValue2},
			expect: sets.NewString(
				"k8s1-clusteri-ns2-svc2-1000-d1ff5450",
				"k8s1-clusteri-ns2-svc2-2000-c83ca053",
				"k8s1-clusteri-ns2-svc2-3000-ff1b34d8"),
		},
		{
			desc:      "match 2 services ns2/svc2, ns2/svc3 and enabled 4 ports",
			namespace: namespace2,
			labels:    map[string]string{labelKey1: labelValue2, labelKey2: labelValue2},
			expect: sets.NewString(
				"k8s1-clusteri-ns2-svc3-4000-bcf82183",
				"k8s1-clusteri-ns2-svc2-1000-d1ff5450",
				"k8s1-clusteri-ns2-svc2-2000-c83ca053",
				"k8s1-clusteri-ns2-svc2-3000-ff1b34d8"),
		},
	}

	for _, tc := range testCases {
		ret := manager.ReadinessGateEnabledNegs(tc.namespace, tc.labels)
		retSet := sets.NewString(ret...)
		if !retSet.Equal(tc.expect) {
			t.Errorf("For case %q, expect %v, but got %v", tc.desc, tc.expect, retSet)
		}
	}
}

func TestReadinessGateEnabled(t *testing.T) {
	t.Parallel()

	kubeClient := fake.NewSimpleClientset()
	manager := NewTestSyncerManager(kubeClient)
	populateSyncerManager(manager, kubeClient)

	testCases := []struct {
		desc   string
		key    negtypes.NegSyncerKey
		expect bool
	}{
		{
			desc:   "empty key",
			expect: false,
		},
		{
			desc: "non exists key 1",
			key: negtypes.NegSyncerKey{
				Namespace:  namespace1,
				Name:       name1,
				Port:       port1,
				TargetPort: targetPort2,
			},
			expect: false,
		},
		{
			desc: "non exists key 2",
			key: negtypes.NegSyncerKey{
				Namespace:  namespace1,
				Name:       name2,
				Port:       port1,
				TargetPort: targetPort1,
			},
			expect: false,
		},
		{
			desc: "key exists but not enabled",
			key: negtypes.NegSyncerKey{
				Namespace:  namespace1,
				Name:       name1,
				Port:       port1,
				TargetPort: targetPort1,
			},
			expect: false,
		},
		{
			desc: "key exists but not enabled 2",
			key: negtypes.NegSyncerKey{
				Namespace:  namespace2,
				Name:       name3,
				Port:       port2,
				TargetPort: targetPort2,
			},
			expect: false,
		},
		{
			desc: "key exists and enabled 1",
			key: negtypes.NegSyncerKey{
				Namespace:  namespace2,
				Name:       name2,
				Port:       port1,
				TargetPort: targetPort1,
			},
			expect: true,
		},
		{
			desc: "key exists and enabled 2",
			key: negtypes.NegSyncerKey{
				Namespace:  namespace2,
				Name:       name2,
				Port:       port2,
				TargetPort: targetPort2,
			},
			expect: true,
		},
		{
			desc: "key exists and enabled 3",
			key: negtypes.NegSyncerKey{
				Namespace:  namespace2,
				Name:       name2,
				Port:       port3,
				TargetPort: targetPort3,
			},
			expect: true,
		},
		{
			desc: "key exists and enabled 4",
			key: negtypes.NegSyncerKey{
				Namespace:  namespace1,
				Name:       name1,
				Port:       port3,
				TargetPort: targetPort3,
			},
			expect: true,
		},
		{
			desc: "key exists and enabled 5",
			key: negtypes.NegSyncerKey{
				Namespace:  namespace1,
				Name:       name1,
				Port:       port4,
				TargetPort: targetPort4,
			},
			expect: true,
		},
	}

	for _, tc := range testCases {
		if manager.ReadinessGateEnabled(tc.key) != tc.expect {
			t.Errorf("For case %q, expect %v, but got %v", tc.desc, tc.expect, manager.ReadinessGateEnabled(tc.key))
		}
	}
}

func TestFilterCommonPorts(t *testing.T) {
	t.Parallel()
	namer := namer_util.NewNamer(ClusterID, "")

	for _, tc := range []struct {
		desc     string
		p1       negtypes.PortInfoMap
		p2       negtypes.PortInfoMap
		expectP1 negtypes.PortInfoMap
		expectP2 negtypes.PortInfoMap
	}{
		{
			desc:     "empty input 1",
			p1:       negtypes.PortInfoMap{},
			p2:       negtypes.PortInfoMap{},
			expectP1: negtypes.PortInfoMap{},
			expectP2: negtypes.PortInfoMap{},
		},
		{
			desc:     "empty input 2",
			p1:       negtypes.NewPortInfoMap(namespace1, name1, types.SvcPortMap{port1: targetPort1, port2: targetPort2}, namer, false),
			p2:       negtypes.PortInfoMap{},
			expectP1: negtypes.NewPortInfoMap(namespace1, name1, types.SvcPortMap{port1: targetPort1, port2: targetPort2}, namer, false),
			expectP2: negtypes.PortInfoMap{},
		},
		{
			desc:     "empty input 3",
			p1:       negtypes.PortInfoMap{},
			p2:       negtypes.NewPortInfoMap(namespace1, name1, types.SvcPortMap{port1: targetPort1, port2: targetPort2}, namer, true),
			expectP1: negtypes.PortInfoMap{},
			expectP2: negtypes.NewPortInfoMap(namespace1, name1, types.SvcPortMap{port1: targetPort1, port2: targetPort2}, namer, true),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			removeCommonPorts(tc.p1, tc.p2)

			if !reflect.DeepEqual(tc.p1, tc.expectP1) {
				t.Errorf("Expect p1 to be %v, but got %v", tc.expectP1, tc.p1)
			}

			if !reflect.DeepEqual(tc.p2, tc.expectP2) {
				t.Errorf("Expect p2 to be %v, but got %v", tc.expectP2, tc.p2)
			}
		})

	}
}

// populateSyncerManager for testing
func populateSyncerManager(manager *syncerManager, kubeClient kubernetes.Interface) {
	namer := manager.namer

	inputs := []struct {
		namespace   string
		name        string
		portInfoMap negtypes.PortInfoMap
		selector    map[string]string
	}{
		{
			namespace: namespace1,
			name:      name1,
			portInfoMap: portInfoUnion(negtypes.NewPortInfoMap(namespace1, name1, types.SvcPortMap{port1: targetPort1, port2: targetPort2}, namer, false),
				negtypes.NewPortInfoMap(namespace1, name1, types.SvcPortMap{port3: targetPort3, port4: targetPort4}, namer, true)),
			selector: map[string]string{labelKey1: labelValue1},
		},
		{
			// nil selector
			namespace: namespace1,
			name:      name2,
			portInfoMap: portInfoUnion(negtypes.NewPortInfoMap(namespace1, name2, types.SvcPortMap{port1: targetPort1, port2: targetPort2}, namer, false),
				negtypes.NewPortInfoMap(namespace1, name2, types.SvcPortMap{port3: targetPort3, port4: targetPort4}, namer, true)),
			selector: nil,
		},
		{
			namespace:   namespace2,
			name:        name1,
			portInfoMap: negtypes.NewPortInfoMap(namespace1, name1, types.SvcPortMap{port1: targetPort1, port2: targetPort2}, namer, false),
			selector:    map[string]string{labelKey1: labelValue1},
		},
		{
			namespace:   namespace2,
			name:        name2,
			portInfoMap: negtypes.NewPortInfoMap(namespace2, name2, types.SvcPortMap{port1: targetPort1, port2: targetPort2, port3: targetPort3}, namer, true),
			selector:    map[string]string{labelKey2: labelValue2},
		},
		{
			namespace: namespace2,
			name:      name3,
			portInfoMap: portInfoUnion(negtypes.NewPortInfoMap(namespace2, name3, types.SvcPortMap{port1: targetPort1, port2: targetPort2, port3: targetPort3}, namer, false),
				negtypes.NewPortInfoMap(namespace2, name3, types.SvcPortMap{port4: targetPort4}, namer, true)),
			selector: map[string]string{labelKey1: labelValue2},
		},
	}

	for _, in := range inputs {
		manager.EnsureSyncers(in.namespace, in.name, in.portInfoMap)
		manager.serviceLister.Add(&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: in.namespace,
				Name:      in.name,
			},
			Spec: v1.ServiceSpec{
				Selector: in.selector,
			},
		})
	}
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

func portInfoUnion(p1, p2 negtypes.PortInfoMap) negtypes.PortInfoMap {
	p1.Merge(p2)
	return p1
}
