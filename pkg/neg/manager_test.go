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
	context2 "context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/googleapi"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/legacy-cloud-providers/gce"

	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/neg/types"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils/common"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
)

const (
	ClusterID     = "clusterid"
	KubeSystemUID = "kube-system-uid"

	namespace1 = "ns1"
	namespace2 = "ns2"
	name1      = "svc1"
	name2      = "svc2"
	name3      = "svc3"

	portName1   = "foo"
	portName2   = "bar"
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
	negName1    = "neg1"
)

func NewTestSyncerManager(kubeClient kubernetes.Interface) (*syncerManager, *gce.Cloud) {
	testContext := negtypes.NewTestContextWithKubeClient(kubeClient)
	manager := newSyncerManager(
		testContext.NegNamer,
		record.NewFakeRecorder(100),
		negtypes.NewAdapter(testContext.Cloud),
		negtypes.NewFakeZoneGetter(),
		testContext.SvcNegClient,
		testContext.KubeSystemUID,
		testContext.PodInformer.GetIndexer(),
		testContext.ServiceInformer.GetIndexer(),
		testContext.EndpointInformer.GetIndexer(),
		testContext.EndpointSliceInformer.GetIndexer(),
		testContext.NodeInformer.GetIndexer(),
		testContext.SvcNegInformer.GetIndexer(),
		false, //enableNonGcpMode
		false, //enableEndpointSlices
	)
	return manager, testContext.Cloud
}

func TestEnsureAndStopSyncer(t *testing.T) {
	t.Parallel()

	manager, _ := NewTestSyncerManager(fake.NewSimpleClientset())
	namer := manager.namer

	svcName := "n1"
	svcNamespace1 := "ns1"
	svcNamespace2 := "ns2"
	portName0 := ""
	portName1 := "http"
	portName2 := "bar"
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
			portInfoMap: negtypes.NewPortInfoMap(svcNamespace1, svcName, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 1000, TargetPort: "80"}, negtypes.SvcPortTuple{Port: 2000, TargetPort: "443"}), namer, false, nil),
			stop:        false,
			expectInternals: map[negtypes.NegSyncerKey]bool{
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 1000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 1000, TargetPort: "80"}, NegName: namer.NEG(svcNamespace1, svcName, 1000)}):  false,
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 2000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 2000, TargetPort: "443"}, NegName: namer.NEG(svcNamespace1, svcName, 2000)}): false,
			},
		},
		{
			desc:        "modify 1 port to enable readinessGate",
			namespace:   svcNamespace1,
			name:        svcName,
			portInfoMap: portInfoUnion(negtypes.NewPortInfoMap(svcNamespace1, svcName, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 1000, TargetPort: "80"}), namer, false, nil), negtypes.NewPortInfoMap(svcNamespace1, svcName, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 2000, TargetPort: "443"}), namer, true, nil)),
			stop:        false,
			expectInternals: map[negtypes.NegSyncerKey]bool{
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 1000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 1000, TargetPort: "80"}, NegName: namer.NEG(svcNamespace1, svcName, 1000)}):  false,
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 2000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 2000, TargetPort: "443"}, NegName: namer.NEG(svcNamespace1, svcName, 2000)}): true,
			},
			expectEnsureError: true,
		},
		{
			desc:        "add 2 new ports, remove 2 existing ports",
			namespace:   svcNamespace1,
			name:        svcName,
			portInfoMap: negtypes.NewPortInfoMap(svcNamespace1, svcName, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 3000, TargetPort: "80"}, negtypes.SvcPortTuple{Port: 4000, TargetPort: "namedport"}), namer, false, nil),
			stop:        false,
			expectInternals: map[negtypes.NegSyncerKey]bool{
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 3000, TargetPort: "80"}, NegName: namer.NEG(svcNamespace1, svcName, 3000)}):        false,
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 4000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 4000, TargetPort: "namedport"}, NegName: namer.NEG(svcNamespace1, svcName, 4000)}): false,
			},
		},
		{
			desc:        "modify 2 existing ports to enable readinessGate",
			namespace:   svcNamespace1,
			name:        svcName,
			portInfoMap: negtypes.NewPortInfoMap(svcNamespace1, svcName, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 3000, TargetPort: "80"}, negtypes.SvcPortTuple{Port: 4000, TargetPort: "namedport"}), namer, true, nil),
			stop:        false,
			expectInternals: map[negtypes.NegSyncerKey]bool{
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 3000, TargetPort: "80"}, NegName: namer.NEG(svcNamespace1, svcName, 3000)}):        true,
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 4000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 4000, TargetPort: "namedport"}, NegName: namer.NEG(svcNamespace1, svcName, 4000)}): true,
			},
		},
		{
			desc:        "add 1 new port for a different service",
			namespace:   svcNamespace2,
			name:        svcName,
			portInfoMap: negtypes.NewPortInfoMap(svcNamespace2, svcName, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 3000, TargetPort: "80"}), namer, false, nil),
			stop:        false,
			expectInternals: map[negtypes.NegSyncerKey]bool{
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 3000, TargetPort: "80"}, NegName: namer.NEG(svcNamespace1, svcName, 3000)}):        true,
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 4000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 4000, TargetPort: "namedport"}, NegName: namer.NEG(svcNamespace1, svcName, 4000)}): true,
				manager.getSyncerKey(svcNamespace2, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 3000, TargetPort: "80"}, NegName: namer.NEG(svcNamespace2, svcName, 3000)}):        false,
			},
		},
		{
			desc:        "change target port of 1 existing port",
			namespace:   svcNamespace1,
			name:        svcName,
			portInfoMap: negtypes.NewPortInfoMap(svcNamespace1, svcName, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: 3000, TargetPort: "80"}, negtypes.SvcPortTuple{Port: 4000, TargetPort: "443"}), namer, true, nil),
			stop:        false,
			expectInternals: map[negtypes.NegSyncerKey]bool{
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 3000, TargetPort: "80"}, NegName: namer.NEG(svcNamespace1, svcName, 3000)}):  true,
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 4000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 4000, TargetPort: "443"}, NegName: namer.NEG(svcNamespace1, svcName, 4000)}): true,
				manager.getSyncerKey(svcNamespace2, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 3000, TargetPort: "80"}, NegName: namer.NEG(svcNamespace2, svcName, 3000)}):  false,
			},
			expectEnsureError: true,
		},
		{
			desc:      "remove all ports for ns1/n1 service",
			namespace: svcNamespace1,
			name:      svcName,
			stop:      true,
			expectInternals: map[negtypes.NegSyncerKey]bool{
				manager.getSyncerKey(svcNamespace2, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 3000, TargetPort: "80"}, NegName: namer.NEG(svcNamespace2, svcName, 3000)}): false,
			},
		},
		{
			desc:        "add named ports for ns2/n1 service",
			namespace:   svcNamespace1,
			name:        svcName,
			stop:        false,
			portInfoMap: negtypes.NewPortInfoMap(svcNamespace1, svcName, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Name: portName1, Port: 3000, TargetPort: "80"}, negtypes.SvcPortTuple{Name: portName2, Port: 4000, TargetPort: "bar"}), namer, true, nil),
			expectInternals: map[negtypes.NegSyncerKey]bool{
				manager.getSyncerKey(svcNamespace2, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 3000, TargetPort: "80"}, NegName: namer.NEG(svcNamespace2, svcName, 3000)}):                   false,
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Name: portName1, Port: 3000, TargetPort: "80"}, NegName: namer.NEG(svcNamespace1, svcName, 3000)}):  true,
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 4000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Name: portName2, Port: 4000, TargetPort: "bar"}, NegName: namer.NEG(svcNamespace1, svcName, 4000)}): true,
			},
			expectEnsureError: false,
		},
		{
			desc:        "change named ports for ns2/n1 service",
			namespace:   svcNamespace1,
			name:        svcName,
			stop:        false,
			portInfoMap: negtypes.NewPortInfoMap(svcNamespace1, svcName, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Name: portName2, Port: 3000, TargetPort: "80"}, negtypes.SvcPortTuple{Name: portName0, Port: 4000, TargetPort: "bar"}), namer, true, nil),
			expectInternals: map[negtypes.NegSyncerKey]bool{
				manager.getSyncerKey(svcNamespace2, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 3000, TargetPort: "80"}, NegName: namer.NEG(svcNamespace2, svcName, 3000)}):                   false,
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Name: portName2, Port: 3000, TargetPort: "80"}, NegName: namer.NEG(svcNamespace1, svcName, 3000)}):  true,
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 4000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Name: portName0, Port: 4000, TargetPort: "bar"}, NegName: namer.NEG(svcNamespace1, svcName, 4000)}): true,
			},
			expectEnsureError: false,
		},
		{
			desc:        "add a new l4 ilb port for ns2/n1 service",
			namespace:   svcNamespace1,
			name:        svcName,
			stop:        false,
			portInfoMap: negtypes.NewPortInfoMap(svcNamespace1, svcName, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Name: portName2, Port: 3000, TargetPort: "80"}, negtypes.SvcPortTuple{Name: portName0, Port: 4000, TargetPort: "bar"}, negtypes.SvcPortTuple{Name: string(negtypes.VmIpEndpointType), Port: 0}), namer, true, nil),
			expectInternals: map[negtypes.NegSyncerKey]bool{
				manager.getSyncerKey(svcNamespace2, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: 3000, TargetPort: "80"}, NegName: namer.NEG(svcNamespace2, svcName, 3000)}):                   false,
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 3000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Name: portName2, Port: 3000, TargetPort: "80"}, NegName: namer.NEG(svcNamespace1, svcName, 3000)}):  true,
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 4000, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Name: portName0, Port: 4000, TargetPort: "bar"}, NegName: namer.NEG(svcNamespace1, svcName, 4000)}): true,
				manager.getSyncerKey(svcNamespace1, svcName, negtypes.PortInfoMapKey{ServicePort: 0, Subset: ""}, negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Name: string(negtypes.VmIpEndpointType), Port: 0}, NegName: namer.NEG(svcNamespace1, svcName, 0)}):     true,
			},
			expectEnsureError: false,
		},
	}

	for _, tc := range testCases {
		manager.serviceLister.Add(&v1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: tc.namespace, Name: tc.name}})
		if tc.stop {
			manager.StopSyncer(tc.namespace, tc.name)
		} else {
			if tc.expectEnsureError {
				if err := wait.Poll(time.Second, 10*time.Second, func() (bool, error) {
					if _, _, err := manager.EnsureSyncers(tc.namespace, tc.name, tc.portInfoMap); err != nil {
						return false, nil
					}
					return true, nil
				}); err != nil {
					t.Errorf("For case %q, timeout waiting for ensure syncer %s/%s-%v: %v", tc.desc, tc.namespace, tc.name, tc.portInfoMap, err)
				}
			} else {
				// Expect EnsureSyncers returns successfully immediately
				if _, _, err := manager.EnsureSyncers(tc.namespace, tc.name, tc.portInfoMap); err != nil {
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
				if info, portFound := portInfoMap[negtypes.PortInfoMapKey{ServicePort: key.PortTuple.Port, Subset: ""}]; portFound {
					if info.ReadinessGate != readinessGate {
						t.Errorf("For case %q, expect readinessGate of key %q to be %v, but got %v", tc.desc, key.String(), readinessGate, info.ReadinessGate)
					}

					expectNegName := namer.NEG(key.Namespace, key.Name, key.PortTuple.Port)
					if info.NegName != expectNegName {
						t.Errorf("For case %q, expect NEG name %q, but got %q", tc.desc, expectNegName, info.NegName)
					}

				} else {
					t.Errorf("For case %q, expect port %d of service %q to be registered", tc.desc, key.PortTuple.Port, svcKey.Key())
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

	manager, _ := NewTestSyncerManager(fake.NewSimpleClientset())
	namer := manager.namer
	namespace := testServiceNamespace
	name := testServiceName
	portName1 := ""
	portName2 := "foo"
	svcPort1 := int32(3000)
	svcPort2 := int32(4000)
	targetPort1 := "80"
	targetPort2 := "namedport"
	negName1 := namer.NEG(namespace, name, svcPort1)
	negName2 := namer.NEG(namespace, name, svcPort2)
	portMap := make(types.PortInfoMap)
	portInfo1 := types.PortInfo{PortTuple: negtypes.SvcPortTuple{Name: portName1, Port: svcPort1, TargetPort: targetPort1}, NegName: negName1}
	portInfo2 := types.PortInfo{PortTuple: negtypes.SvcPortTuple{Name: portName2, Port: svcPort2, TargetPort: targetPort2}, NegName: negName2}

	portMap[negtypes.PortInfoMapKey{ServicePort: svcPort1, Subset: ""}] = portInfo1
	portMap[negtypes.PortInfoMapKey{ServicePort: svcPort2, Subset: ""}] = portInfo2

	manager.serviceLister.Add(&v1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}})

	if _, _, err := manager.EnsureSyncers(namespace, name, portMap); err != nil {
		t.Fatalf("Failed to ensure syncer: %v", err)
	}
	manager.StopSyncer(namespace, name)

	syncer1 := manager.syncerMap[manager.getSyncerKey(namespace, name, negtypes.PortInfoMapKey{ServicePort: svcPort1, Subset: ""}, portInfo1)]
	syncer2 := manager.syncerMap[manager.getSyncerKey(namespace, name, negtypes.PortInfoMapKey{ServicePort: svcPort2, Subset: ""}, portInfo2)]

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
	if _, err := kubeClient.CoreV1().Endpoints(testServiceNamespace).Create(context2.TODO(), getDefaultEndpoint(), metav1.CreateOptions{}); err != nil {
		t.Fatalf("Failed to create endpoint: %v", err)
	}
	manager, _ := NewTestSyncerManager(kubeClient)
	svcPort := int32(80)
	ports := make(types.PortInfoMap)
	manager.serviceLister.Add(&v1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: testServiceNamespace, Name: testServiceName}})
	ports[negtypes.PortInfoMapKey{ServicePort: svcPort, Subset: ""}] = types.PortInfo{PortTuple: negtypes.SvcPortTuple{TargetPort: "namedport"}, NegName: manager.namer.NEG(testServiceNamespace, testServiceName, svcPort)}
	if _, _, err := manager.EnsureSyncers(testServiceNamespace, testServiceName, ports); err != nil {
		t.Fatalf("Failed to ensure syncer: %v", err)
	}

	version := meta.VersionGA
	for _, networkEndpointType := range []negtypes.NetworkEndpointType{negtypes.VmIpPortEndpointType, negtypes.NonGCPPrivateEndpointType, negtypes.VmIpEndpointType} {

		if networkEndpointType == negtypes.VmIpEndpointType {
			version = meta.VersionAlpha
		}
		negName := manager.namer.NEG("test", "test", 80)
		manager.cloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{
			Version:             version,
			Name:                negName,
			NetworkEndpointType: string(networkEndpointType),
		}, negtypes.TestZone1)
		manager.cloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{
			Version:             version,
			Name:                negName,
			NetworkEndpointType: string(networkEndpointType),
		}, negtypes.TestZone2)

		svcNeg := &negv1beta1.ServiceNetworkEndpointGroup{
			ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: negName},
			Spec:       negv1beta1.ServiceNetworkEndpointGroupSpec{},
			Status:     negv1beta1.ServiceNetworkEndpointGroupStatus{},
		}
		manager.svcNegLister.Add(svcNeg)
		manager.svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups("test").Create(context2.Background(), svcNeg, metav1.CreateOptions{})

		if err := manager.GC(); err != nil {
			t.Fatalf("Failed to GC: %v", err)
		}
		for _, zone := range []string{negtypes.TestZone1, negtypes.TestZone2} {
			negs, _ := manager.cloud.ListNetworkEndpointGroup(zone, version)
			for _, neg := range negs {
				if neg.Name == negName {
					t.Errorf("Expect NEG %q in zone %q to be GCed.", negName, zone)
				}
			}
		}
	}

	// make sure there is no leaking go routine
	manager.StopSyncer(testServiceNamespace, testServiceName)
}

func TestReadinessGateEnabledNegs(t *testing.T) {
	t.Parallel()

	kubeClient := fake.NewSimpleClientset()
	manager, _ := NewTestSyncerManager(kubeClient)
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
	manager, _ := NewTestSyncerManager(kubeClient)
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
				Namespace: namespace1,
				Name:      name1,
				PortTuple: negtypes.SvcPortTuple{
					Port:       port1,
					TargetPort: targetPort2,
				},
			},
			expect: false,
		},
		{
			desc: "non exists key 2",
			key: negtypes.NegSyncerKey{
				Namespace: namespace1,
				Name:      name2,
				PortTuple: negtypes.SvcPortTuple{
					Port:       port1,
					TargetPort: targetPort1,
				},
			},
			expect: false,
		},
		{
			desc: "key exists but not enabled",
			key: negtypes.NegSyncerKey{
				Namespace: namespace1,
				Name:      name1,
				PortTuple: negtypes.SvcPortTuple{
					Port:       port1,
					TargetPort: targetPort1,
				},
			},
			expect: false,
		},
		{
			desc: "key exists but not enabled 2",
			key: negtypes.NegSyncerKey{
				Namespace: namespace2,
				Name:      name3,
				PortTuple: negtypes.SvcPortTuple{
					Port:       port2,
					TargetPort: targetPort2,
				},
			},
			expect: false,
		},
		{
			desc: "key exists and enabled 1",
			key: negtypes.NegSyncerKey{
				Namespace: namespace2,
				Name:      name2,
				PortTuple: negtypes.SvcPortTuple{
					Port:       port1,
					TargetPort: targetPort1,
				},
			},
			expect: true,
		},
		{
			desc: "key exists and enabled 2",
			key: negtypes.NegSyncerKey{
				Namespace: namespace2,
				Name:      name2,
				PortTuple: negtypes.SvcPortTuple{
					Port:       port2,
					TargetPort: targetPort2,
				},
			},
			expect: true,
		},
		{
			desc: "key exists and enabled 3",
			key: negtypes.NegSyncerKey{
				Namespace: namespace2,
				Name:      name2,
				PortTuple: negtypes.SvcPortTuple{
					Port:       port3,
					TargetPort: targetPort3,
				},
			},
			expect: true,
		},
		{
			desc: "key exists and enabled 4",
			key: negtypes.NegSyncerKey{
				Namespace: namespace1,
				Name:      name1,
				PortTuple: negtypes.SvcPortTuple{
					Port:       port3,
					TargetPort: targetPort3,
				},
			},
			expect: true,
		},
		{
			desc: "key exists and enabled 5",
			key: negtypes.NegSyncerKey{
				Namespace: namespace1,
				Name:      name1,
				PortTuple: negtypes.SvcPortTuple{
					Port:       port4,
					TargetPort: targetPort4,
				},
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
	kubeClient := fake.NewSimpleClientset()
	manager, _ := NewTestSyncerManager(kubeClient)

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
			p1:       negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}, negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}), namer, false, nil),
			p2:       negtypes.PortInfoMap{},
			expectP1: negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}, negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}), namer, false, nil),
			expectP2: negtypes.PortInfoMap{},
		},
		{
			desc:     "empty input 3",
			p1:       negtypes.PortInfoMap{},
			p2:       negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}, negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}), namer, true, nil),
			expectP1: negtypes.PortInfoMap{},
			expectP2: negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}, negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}), namer, true, nil),
		},
		{
			desc:     "difference in readiness gate",
			p1:       negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}, negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}), namer, false, nil),
			p2:       negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}, negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}), namer, true, nil),
			expectP1: negtypes.PortInfoMap{},
			expectP2: negtypes.PortInfoMap{},
		},
		{
			desc:     "difference in port name and readiness gate",
			p1:       negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Name: portName1, Port: port1, TargetPort: targetPort1}, negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}), namer, false, nil),
			p2:       negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}, negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}), namer, true, nil),
			expectP1: negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Name: portName1, Port: port1, TargetPort: targetPort1}), namer, false, nil),
			expectP2: negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}), namer, true, nil),
		},
		{
			desc:     "difference in neg name",
			p1:       negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}, negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}), namer, false, map[negtypes.SvcPortTuple]string{{Port: port1, TargetPort: targetPort1}: negName1}),
			p2:       negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}, negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}), namer, true, nil),
			expectP1: negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}), namer, false, map[negtypes.SvcPortTuple]string{{Port: port1, TargetPort: targetPort1}: negName1}),
			expectP2: negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}), namer, true, nil),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			manager.removeCommonPorts(tc.p1, tc.p2)

			if !reflect.DeepEqual(tc.p1, tc.expectP1) {
				t.Errorf("Expect p1 to be %v, but got %v", tc.expectP1, tc.p1)
			}

			if !reflect.DeepEqual(tc.p2, tc.expectP2) {
				t.Errorf("Expect p2 to be %v, but got %v", tc.expectP2, tc.p2)
			}
		})

	}
}

func TestNegCRCreations(t *testing.T) {
	t.Parallel()
	manager, _ := NewTestSyncerManager(fake.NewSimpleClientset())
	svcNegClient := manager.svcNegClient
	namer := manager.namer

	svcName := "n1"
	svcNamespace := "ns1"
	customNegName := "neg-name"
	var svcUID apitypes.UID = "svc-uid"
	svc := &v1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: svcNamespace,
			Name:      svcName,
		},
	}
	svc.SetUID(svcUID)
	if err := manager.serviceLister.Add(svc); err != nil {
		t.Errorf("failed to add sample service to service store: %s", err)
	}

	expectedPortInfoMap := negtypes.NewPortInfoMap(
		svcNamespace,
		svcName,
		types.NewSvcPortTupleSet(
			negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1},
			negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}),
		namer, false,
		map[negtypes.SvcPortTuple]string{{Port: port1, TargetPort: targetPort1}: customNegName})

	if _, _, err := manager.EnsureSyncers(svcNamespace, svcName, expectedPortInfoMap); err != nil {
		t.Errorf("failed to ensure syncer %s/%s-%v: %v", svcNamespace, svcName, expectedPortInfoMap, err)
	}

	// validate portInfo
	if len(manager.svcPortMap) != 1 {
		t.Errorf("manager should have one service has %d", len(manager.svcPortMap))
	}

	svcKey := serviceKey{namespace: svcNamespace, name: svcName}
	if portInfoMap, svcFound := manager.svcPortMap[svcKey]; svcFound {
		for key, expectedInfo := range expectedPortInfoMap {
			if info, portFound := portInfoMap[key]; portFound {
				if info.NegName != expectedInfo.NegName {
					t.Errorf("expected NEG name %q, but got %q", expectedInfo.NegName, info.NegName)
				}

				if info.PortTuple.Port == port1 && info.NegName != customNegName {
					t.Errorf("expected Neg name for port %d, to be %s, but was %s", port1, customNegName, info.NegName)
				}
			} else {
				t.Errorf("expected port %d of service %q to be registered", key.ServicePort, svcKey.Key())
			}

			neg, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svcKey.namespace).Get(context2.TODO(), expectedInfo.NegName, metav1.GetOptions{})
			if err != nil {
				t.Errorf("error getting neg from neg client: %s", err)
			}
			checkNegCR(t, neg, svcKey, svcUID, expectedInfo)
		}

	} else {
		t.Errorf("expect service key %q to be registered", svcKey.Key())
	}

	// Second call of EnsureSyncers shouldn't cause any changes or errors
	if _, _, err := manager.EnsureSyncers(svcNamespace, svcName, expectedPortInfoMap); err != nil {
		t.Errorf("failed to ensure syncer after creating %s/%s-%v: %v", svcNamespace, svcName, expectedPortInfoMap, err)
	}

	for _, expectedInfo := range expectedPortInfoMap {
		neg, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svcKey.namespace).Get(context2.TODO(), expectedInfo.NegName, metav1.GetOptions{})
		if err != nil {
			t.Errorf("error getting neg from neg client: %s", err)
			continue
		}
		checkNegCR(t, neg, svcKey, svcUID, expectedInfo)
	}

	// Delete Neg CRs
	for _, expectedInfo := range expectedPortInfoMap {
		err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(namespace1).Delete(context2.TODO(), expectedInfo.NegName, metav1.DeleteOptions{})
		if err != nil {
			t.Errorf("error deleting neg from neg client: %s", err)
			continue
		}
	}

	// EnsureSyncers should recreate the deleted CRs
	if _, _, err := manager.EnsureSyncers(svcNamespace, svcName, expectedPortInfoMap); err != nil {
		t.Errorf("failed to ensure syncer after creating %s/%s-%v: %v", svcNamespace, svcName, expectedPortInfoMap, err)
	}

	for _, expectedInfo := range expectedPortInfoMap {
		neg, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svcKey.namespace).Get(context2.TODO(), expectedInfo.NegName, metav1.GetOptions{})
		if err != nil {
			t.Errorf("error getting neg from neg client: %s", err)
			continue
		}
		checkNegCR(t, neg, svcKey, svcUID, expectedInfo)
	}

}

func TestNegCRDuplicateCreations(t *testing.T) {
	t.Parallel()

	svc1Name := "svc1"
	svc2Name := "svc2"
	namespace := "ns1"
	customNegName := "neg-name"

	var svc1UID apitypes.UID = "svc-1-uid"
	var svc2UID apitypes.UID = "svc-2-uid"
	svc1 := &v1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      svc1Name,
		},
	}
	svc1.SetUID(svc1UID)

	svc2 := svc1.DeepCopy()
	svc2.Name = svc2Name
	svc2.SetUID(svc2UID)

	svcTuple1 := negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}
	svcTuple2 := negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort1}

	testCases := []struct {
		desc              string
		svc               *v1.Service
		svcTuple          negtypes.SvcPortTuple
		markedForDeletion bool
		expectErr         bool
		extraLabel        bool
		incorrectLabel    bool
		incorrectSvcUID   bool
		missingLabel      bool
		crExists          bool
		expectUpdate      bool
	}{
		{desc: "no cr exists yet",
			crExists:     false,
			expectUpdate: true,
		},
		{desc: "same service, same port configuration, original cr is not marked for deletion",
			svc:               svc1,
			svcTuple:          svcTuple1,
			markedForDeletion: false,
			expectErr:         false,
			crExists:          true,
			expectUpdate:      false,
		},
		{desc: "same service, same port configuration, original cr and has extra labels",
			svc:               svc1,
			svcTuple:          svcTuple1,
			markedForDeletion: false,
			expectErr:         false,
			extraLabel:        true,
			crExists:          true,
			expectUpdate:      false,
		},
		{desc: "same service, same port configuration, original cr is marked for deletion",
			svc:               svc1,
			svcTuple:          svcTuple1,
			markedForDeletion: true,
			expectErr:         false,
			crExists:          true,
			expectUpdate:      false,
		},
		{desc: "same service, different port configuration, original cr",
			svc:               svc1,
			svcTuple:          svcTuple2,
			markedForDeletion: false,
			expectErr:         true,
			crExists:          true,
			expectUpdate:      false,
		},
		{desc: "different service name",
			svc:               svc2,
			svcTuple:          svcTuple1,
			markedForDeletion: false,
			expectErr:         true,
			crExists:          true,
			expectUpdate:      false,
		},
		{desc: "same service, same port config, incorrect svcUID",
			svc:               svc1,
			svcTuple:          svcTuple1,
			markedForDeletion: false,
			incorrectSvcUID:   true,
			expectErr:         false,
			crExists:          true,
			expectUpdate:      true,
		},
		{desc: "same service, same port config, incorrect label",
			svc:               svc1,
			svcTuple:          svcTuple1,
			markedForDeletion: false,
			incorrectLabel:    true,
			expectErr:         true,
			crExists:          true,
			expectUpdate:      false,
		},
		{desc: "same service, same port config, missing label",
			svc:               svc1,
			svcTuple:          svcTuple1,
			markedForDeletion: false,
			missingLabel:      true,
			expectErr:         false,
			crExists:          true,
			expectUpdate:      true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			manager, _ := NewTestSyncerManager(fake.NewSimpleClientset())
			svcNegClient := manager.svcNegClient

			namer := manager.namer

			var testNeg negv1beta1.ServiceNetworkEndpointGroup
			if tc.crExists {
				if err := manager.serviceLister.Add(tc.svc); err != nil {
					t.Errorf("failed to add original service to service store: %s", err)
				}
				svcKey := serviceKey{namespace: namespace, name: tc.svc.Name}
				portInfo := negtypes.PortInfo{PortTuple: tc.svcTuple, NegName: customNegName}
				testNeg = createNegCR(tc.svc, svcKey, portInfo)
				if tc.markedForDeletion {
					deletionTS := metav1.Now()
					testNeg.SetDeletionTimestamp(&deletionTS)
				}

				if tc.extraLabel {
					testNeg.Labels["extra-label"] = "extra-value"
				}

				if tc.incorrectLabel {
					testNeg.Labels[negtypes.NegCRManagedByKey] = "wrong-value"
				}

				if tc.missingLabel {
					delete(testNeg.Labels, negtypes.NegCRManagedByKey)
				}

				if tc.incorrectSvcUID {
					testNeg.OwnerReferences = append(testNeg.OwnerReferences, metav1.OwnerReference{UID: "extra-uid"})
				}

				_, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(namespace1).Create(context2.TODO(), &testNeg, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("error creating test neg")
				}
			}

			if err := manager.serviceLister.Add(svc1); err != nil {
				t.Errorf("failed to add sample service to service store: %s", err)
			}

			portInfoMap := negtypes.NewPortInfoMap(
				namespace,
				svc1.Name,
				types.NewSvcPortTupleSet(svcTuple1),
				namer, false,
				map[negtypes.SvcPortTuple]string{svcTuple1: customNegName},
			)

			_, _, err := manager.EnsureSyncers(namespace, svc1.Name, portInfoMap)
			if tc.expectErr && err == nil {
				t.Errorf("expected error when ensuring syncer %s/%s %+v", namespace, svc1.Name, portInfoMap)
			} else if !tc.expectErr && err != nil {
				t.Errorf("unexpected error when ensuring syncer %s/%s %+v: %s", namespace, svc1.Name, portInfoMap, err)
			}

			negs, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(namespace).List(context2.TODO(), metav1.ListOptions{})
			if len(negs.Items) != 1 {
				t.Errorf("expected to retrieve one negs, retrieved %d", len(negs.Items))
			}

			if !tc.expectUpdate {
				// If errored or if neg cr is already correct, no update should occur
				if !reflect.DeepEqual(testNeg, negs.Items[0]) {
					t.Errorf("test neg should not have been updated")
				}
			} else {
				svcKey := serviceKey{namespace: namespace, name: svc1Name}
				portInfo := portInfoMap[negtypes.PortInfoMapKey{ServicePort: svcTuple1.Port, Subset: ""}]
				checkNegCR(t, &negs.Items[0], svcKey, svc1.UID, portInfo)
				// If update was unnecessary, the resource version should not change.
				// If upate was necessary, the same CR should be used for an update so the resource
				// version should be unchanged. API server will change resource version.
				if negs.Items[0].ResourceVersion != testNeg.ResourceVersion {
					t.Errorf("neg resource version should not be updated")
				}
			}

			// make sure there is no leaking go routine
			manager.StopSyncer(namespace, svc1Name)
		})
	}
}

func TestNegCRDeletions(t *testing.T) {
	t.Parallel()
	svcName := "n1"
	svcNamespace := "ns1"
	customNegName := "neg-name"
	svcKey := serviceKey{namespace: svcNamespace, name: svcName}
	var svcUID apitypes.UID = "svc-uid"
	svc := &v1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: svcNamespace,
			Name:      svcName,
		},
	}
	svc.SetUID(svcUID)

	testCases := []struct {
		desc          string
		negsExist     bool
		hasDeletionTS bool
	}{
		{
			desc:          "negs exist, no deletion timestamp, remove desired ports",
			negsExist:     true,
			hasDeletionTS: false,
		},
		{
			desc:          "negs already have deletion timestamp, remove desired ports",
			negsExist:     true,
			hasDeletionTS: true,
		},
		{
			desc:          "negs don't exist, remove desired ports",
			negsExist:     false,
			hasDeletionTS: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			manager, _ := NewTestSyncerManager(fake.NewSimpleClientset())
			svcNegClient := manager.svcNegClient

			if err := manager.serviceLister.Add(svc); err != nil {
				t.Errorf("failed to add sample service to service store: %s", err)
			}

			// set up manager current state before deleting
			namer := manager.namer
			expectedPortInfoMap := negtypes.NewPortInfoMap(
				svcNamespace,
				svcName,
				types.NewSvcPortTupleSet(
					negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1},
					negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}),
				namer, false,
				map[negtypes.SvcPortTuple]string{{Port: port1, TargetPort: targetPort1}: customNegName},
			)

			svcPortMap := map[serviceKey]negtypes.PortInfoMap{
				{namespace: svcNamespace, name: svcName}: expectedPortInfoMap,
			}
			manager.svcPortMap = svcPortMap

			var deletionTS metav1.Time
			if tc.hasDeletionTS {
				deletionTS = metav1.Now()
			}

			if tc.negsExist {
				for _, portInfo := range expectedPortInfoMap {
					neg := createNegCR(svc, svcKey, portInfo)
					if tc.hasDeletionTS {
						neg.SetDeletionTimestamp(&deletionTS)
					}
					manager.svcNegLister.Add(&neg)
					if _, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svcKey.namespace).Create(context2.TODO(), &neg, metav1.CreateOptions{}); err != nil {
						t.Errorf("failed adding neg %s to fake neg client: %s", portInfo.NegName, err)
					}
				}
			}

			if _, _, err := manager.EnsureSyncers(svcNamespace, svcName, nil); err != nil {

				t.Errorf("unexpected error when deleting negs: %s", err)
			}

			negs, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svcKey.namespace).List(context2.TODO(), metav1.ListOptions{})
			if err != nil {
				t.Errorf("error retrieving negs : %s", err)
			}

			if tc.hasDeletionTS {
				if len(negs.Items) != 2 {
					t.Errorf("expected to retrieve two neg, retrieved %d", len(negs.Items))
				}

				for _, neg := range negs.Items {
					if tc.hasDeletionTS && !neg.GetDeletionTimestamp().Equal(&deletionTS) {
						t.Errorf("Expected neg cr %s deletion timestamp to be unchanged", neg.Name)
					} else if !tc.hasDeletionTS && neg.GetDeletionTimestamp().IsZero() {
						t.Errorf("Expected neg cr %s deletion timestamp to be set", neg.Name)
					}
				}
				//clear all existing neg CRs
				for _, portInfo := range expectedPortInfoMap {
					if err = svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svcKey.namespace).Delete(context2.TODO(), portInfo.NegName, metav1.DeleteOptions{}); err != nil {
						t.Errorf("failed deleting neg %s in fake neg client: %s", portInfo.NegName, err)
					}
				}
			} else {
				if len(negs.Items) != 0 {
					t.Errorf("expected to retrieve zero negs, retrieved %d", len(negs.Items))
				}
			}
			//ensure all goroutines are stopped
			manager.StopSyncer(svcNamespace, svcName)
		})
	}
}

func TestGarbageCollectionNegCrdEnabled(t *testing.T) {
	t.Parallel()

	svc := &v1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      testServiceName,
		},
	}
	svc.SetUID("svc-uid")
	port80 := int32(80)
	zones := []string{negtypes.TestZone1, negtypes.TestZone2}

	matchingDesc := utils.NegDescription{
		ClusterUID:  KubeSystemUID,
		Namespace:   testServiceNamespace,
		ServiceName: testServiceName,
		Port:        fmt.Sprintf("%v", port80),
	}

	wrongDesc := utils.NegDescription{
		ClusterUID:  "another-cluster",
		Namespace:   "another-namespace",
		ServiceName: "another-svc",
		Port:        "another-port",
	}

	testCases := []struct {
		desc                 string
		negsExist            bool
		markedForDeletion    bool
		desiredConfig        bool
		emptyNegRefList      bool
		negDesc              string
		malformedNegSelflink bool
		expectNegGC          bool
		expectCrGC           bool
		expectErr            bool
		gcError              error

		// expectGenNamedNegGC indicates that the Neg GC only occurs if using a generated name
		// expectNegGC will take precedence over this value
		expectGenNamedNegGC bool
	}{
		{desc: "neg config not in svcPortMap, marked for deletion",
			negsExist:         true,
			markedForDeletion: true,
			expectNegGC:       true,
			expectCrGC:        true,
			negDesc:           matchingDesc.String(),
		},
		{desc: "neg config not in svcPortMap",
			negsExist:         true,
			markedForDeletion: false,
			expectNegGC:       true,
			expectCrGC:        true,
			negDesc:           matchingDesc.String(),
		},
		{desc: "neg config not in svcPortMap, marked for deletion, empty neg list",
			negsExist:         true,
			markedForDeletion: true,
			emptyNegRefList:   true,
			expectNegGC:       true,
			expectCrGC:        true,
			negDesc:           matchingDesc.String(),
		},
		{desc: "neg config not in svcPortMap, empty neg list",
			negsExist:         true,
			markedForDeletion: false,
			emptyNegRefList:   true,
			expectNegGC:       true,
			expectCrGC:        true,
			negDesc:           matchingDesc.String(),
		},
		{desc: "neg config not in svcPortMap, malformed neg selflink",
			negsExist:            true,
			markedForDeletion:    false,
			malformedNegSelflink: true,
			emptyNegRefList:      false,
			expectNegGC:          true,
			expectCrGC:           true,
			expectErr:            true,
			negDesc:              matchingDesc.String(),
		},
		{desc: "neg config not in svcPortMap, empty neg list, neg has matching description",
			negsExist:         true,
			markedForDeletion: false,
			emptyNegRefList:   true,
			expectNegGC:       true,
			expectCrGC:        true,
			negDesc:           matchingDesc.String(),
		},
		{desc: "neg config not in svcPortMap, empty neg list, neg does not have matching description",
			negsExist:         true,
			markedForDeletion: false,
			emptyNegRefList:   true,
			expectNegGC:       false,
			expectCrGC:        true,
			negDesc:           wrongDesc.String(),
		},
		{desc: "neg config not in svcPortMap, empty neg list, neg has empty description",
			negsExist:           true,
			markedForDeletion:   false,
			emptyNegRefList:     true,
			expectGenNamedNegGC: true,
			expectCrGC:          true,
			negDesc:             "",
		},
		{desc: "neg config in svcPortMap, marked for deletion",
			negsExist:         true,
			markedForDeletion: true,
			desiredConfig:     true,
			expectNegGC:       false,
			expectCrGC:        false,
		},
		{desc: "neg config in svcPortMap",
			negsExist:         true,
			markedForDeletion: false,
			desiredConfig:     true,
			expectNegGC:       false,
			expectCrGC:        false,
		},
		{desc: "negs don't exist, config not in svcPortMap",
			negsExist:         false,
			markedForDeletion: false,
			expectCrGC:        true,
		},
		{desc: "negs don't exist, config not in svcPortMap, marked for deletion",
			negsExist:         false,
			markedForDeletion: true,
			expectCrGC:        true,
		},
		{desc: "400 Bad Request error during neg gc, config not in svcPortMap",
			negsExist:         false,
			markedForDeletion: true,
			expectCrGC:        true,
			expectErr:         false,
			gcError:           &googleapi.Error{Code: http.StatusBadRequest},
			negDesc:           matchingDesc.String(),
		},
		{desc: "error during neg gc, config not in svcPortMap",
			negsExist:         true,
			markedForDeletion: true,
			expectCrGC:        true,
			expectErr:         true,
			gcError:           fmt.Errorf("gc-error"),
			negDesc:           matchingDesc.String(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			for _, customName := range []bool{true, false} {
				for _, networkEndpointType := range []negtypes.NetworkEndpointType{negtypes.VmIpPortEndpointType, negtypes.NonGCPPrivateEndpointType, negtypes.VmIpEndpointType} {

					kubeClient := fake.NewSimpleClientset()
					manager, testCloud := NewTestSyncerManager(kubeClient)
					svcNegClient := manager.svcNegClient

					manager.serviceLister.Add(svc)
					fakeNegCloud := manager.cloud

					version := meta.VersionGA
					if networkEndpointType == negtypes.VmIpEndpointType {
						version = meta.VersionAlpha
					}

					// Create NEG to be GC'ed
					negName := manager.namer.NEG("test", "test", port80)
					if customName {
						negName = "test"
					}

					for _, zone := range zones {
						fakeNegCloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{
							Version:             version,
							Name:                negName,
							NetworkEndpointType: string(networkEndpointType),
							Description:         tc.negDesc,
						}, zone)
					}

					gcPortInfo := negtypes.PortInfo{PortTuple: negtypes.SvcPortTuple{Port: port80}, NegName: negName}
					cr := createNegCR(svc, serviceKey{namespace: testServiceNamespace, name: testServiceName}, gcPortInfo)
					if tc.markedForDeletion {
						now := metav1.Now()
						cr.SetDeletionTimestamp(&now)
					}

					if !tc.emptyNegRefList {
						if !tc.malformedNegSelflink {
							cr.Status.NetworkEndpointGroups = getNegObjectRefs(t, fakeNegCloud, zones, negName, version)
						} else {
							cr.Status.NetworkEndpointGroups = []negv1beta1.NegObjectReference{{SelfLink: ""}}
						}
					}

					if _, err := manager.svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(cr.Namespace).Create(context2.TODO(), &cr, metav1.CreateOptions{}); err != nil {
						t.Fatalf("failed to create neg cr")
					}

					crs := getNegCRs(t, svcNegClient, testServiceNamespace)
					populateSvcNegCache(t, manager, svcNegClient, testServiceNamespace)

					if tc.desiredConfig {
						// Add config to svc map
						serviceKey := getServiceKey(testServiceNamespace, testServiceName)
						ports := make(negtypes.PortInfoMap)
						ports[negtypes.PortInfoMapKey{ServicePort: port80, Subset: ""}] = gcPortInfo

						manager.svcPortMap[serviceKey] = ports
					}

					if !tc.negsExist {
						for _, zone := range []string{negtypes.TestZone1, negtypes.TestZone2} {
							fakeNegCloud.DeleteNetworkEndpointGroup(negName, zone, version)
						}
					}

					if tc.gcError != nil {
						mockCloud := testCloud.Compute().(*cloud.MockGCE)
						mockNEG := mockCloud.NetworkEndpointGroups().(*cloud.MockNetworkEndpointGroups)

						for _, zone := range []string{negtypes.TestZone1, negtypes.TestZone2} {
							mockNEG.DeleteError[*meta.ZonalKey(negName, zone)] = tc.gcError
						}
					}

					err := manager.GC()
					if !tc.expectErr && err != nil {
						t.Fatalf("failed to GC: %v", err)
					} else if tc.expectErr && err == nil {
						t.Errorf("expected GC to error")
					}

					negs, err := fakeNegCloud.AggregatedListNetworkEndpointGroup(version)
					if err != nil {
						t.Errorf("failed getting negs from cloud: %s", err)
					}

					numExistingNegs, negsDeleted := checkForNegDeletions(negs, negName)

					expectNegGC := tc.expectNegGC || (tc.expectGenNamedNegGC && !customName)
					if tc.negsExist && expectNegGC && !negsDeleted {
						t.Errorf("expected negs to be GCed, but found %d", numExistingNegs)
					} else if tc.negsExist && !expectNegGC && numExistingNegs != 2 {
						t.Errorf("expected two negs in the cloud, but found %d", numExistingNegs)
					}

					crs = getNegCRs(t, svcNegClient, testServiceNamespace)
					crDeleted := checkForNegCRDeletion(crs, negName)

					if tc.expectCrGC && !crDeleted {
						t.Errorf("expected neg %s to be deleted", negName)
					} else if !tc.expectCrGC && crDeleted && !tc.markedForDeletion {
						t.Errorf("expected neg %s to not be deleted", negName)
					}
				}
			}
		})
	}
}

// getNegObjectRefs generates the NegObjectReference list of all negs with the specified negName in the specified zones
func getNegObjectRefs(t *testing.T, cloud negtypes.NetworkEndpointGroupCloud, zones []string, negName string, version meta.Version) []negv1beta1.NegObjectReference {
	var negRefs []negv1beta1.NegObjectReference
	for _, zone := range zones {
		neg, err := cloud.GetNetworkEndpointGroup(negName, zone, version)
		if err != nil {
			t.Errorf("failed to get neg %s, from zone %s", negName, zone)
			continue
		}
		negRefs = append(negRefs, negv1beta1.NegObjectReference{
			Id:                  fmt.Sprint(neg.Id),
			SelfLink:            neg.SelfLink,
			NetworkEndpointType: negv1beta1.NetworkEndpointType(neg.NetworkEndpointType),
		})
	}
	return negRefs
}

// checkForNegDeletions checks that negs does not have a neg with the provided negName. If none exists, returns true, otherwise returns false the number of negs found with the name
func checkForNegDeletions(negs map[*meta.Key]*composite.NetworkEndpointGroup, negName string) (int, bool) {
	foundNegs := 0
	for _, neg := range negs {
		if neg.Name == negName {
			foundNegs += 1
		}
	}

	return foundNegs, foundNegs == 0
}

// checkForNegCRDeletion verifies that either no cr with name `negName` exists or a cr withe name `negName` has its deletion timestamp set
func checkForNegCRDeletion(negs []negv1beta1.ServiceNetworkEndpointGroup, negName string) bool {
	for _, neg := range negs {
		if neg.Name == negName {
			if neg.GetDeletionTimestamp().IsZero() {
				return false
			}
			return true
		}
	}

	return true
}

// Check that NEG CR Conditions exist and are in the expected condition
func checkNegCR(t *testing.T, neg *negv1beta1.ServiceNetworkEndpointGroup, svcKey serviceKey, svcUID apitypes.UID, expectedInfo negtypes.PortInfo) {

	if neg.GetNamespace() != svcKey.namespace {
		t.Errorf("neg namespace is %s, expected %s", neg.GetNamespace(), svcKey.namespace)
	}

	//check labels
	labels := neg.GetLabels()
	if len(labels) != 3 {
		t.Errorf("Expected 3 labels for neg %s, found %d", neg.Name, len(labels))
	} else {

		if val, ok := labels[negtypes.NegCRManagedByKey]; !ok || val != negtypes.NegCRControllerValue {
			t.Errorf("Expected neg to have label %s, with value %s found %s", negtypes.NegCRManagedByKey, negtypes.NegCRControllerValue, val)
		}

		if val, ok := labels[negtypes.NegCRServiceNameKey]; !ok || val != svcKey.name {
			t.Errorf("Expected neg to have label %s, with value %s found %s", negtypes.NegCRServiceNameKey, expectedInfo.NegName, val)
		}

		if val, ok := labels[negtypes.NegCRServicePortKey]; !ok || val != fmt.Sprint(expectedInfo.PortTuple.Port) {
			t.Errorf("Expected neg to have label %s, with value %d found %s", negtypes.NegCRServicePortKey, expectedInfo.PortTuple.Port, val)
		}
	}

	ownerRefs := neg.GetOwnerReferences()
	if len(ownerRefs) != 1 {
		t.Errorf("Expected neg to have one owner ref, has %d", len(ownerRefs))
	} else {

		if ownerRefs[0].Name != svcKey.name {
			t.Errorf("Expected neg owner ref to have name %s, instead has %s", svcKey.name, ownerRefs[0].Name)
		}

		if ownerRefs[0].UID != svcUID {
			t.Errorf("Expected neg owner ref to have UID %s, instead has %s", svcUID, ownerRefs[0].UID)
		}

		if *ownerRefs[0].BlockOwnerDeletion != false {
			t.Errorf("Expected neg owner ref not block owner deltion")
		}
	}

	finalizers := neg.GetFinalizers()
	if len(finalizers) != 1 {
		t.Errorf("Expected neg to have one finalizer, has %d", len(finalizers))
	} else {
		if finalizers[0] != common.NegFinalizerKey {
			t.Errorf("Expected neg to have finalizer %s, but found %s", common.NegFinalizerKey, finalizers[0])
		}
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
			portInfoMap: portInfoUnion(negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}, negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}), namer, false, nil),
				negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port3, TargetPort: targetPort3}, negtypes.SvcPortTuple{Port: port4, TargetPort: targetPort4}), namer, true, nil)),
			selector: map[string]string{labelKey1: labelValue1},
		},
		{
			// nil selector
			namespace: namespace1,
			name:      name2,
			portInfoMap: portInfoUnion(negtypes.NewPortInfoMap(namespace1, name2, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}, negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}), namer, false, nil),
				negtypes.NewPortInfoMap(namespace1, name2, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port3, TargetPort: targetPort3}, negtypes.SvcPortTuple{Port: port4, TargetPort: targetPort4}), namer, true, nil)),
			selector: nil,
		},
		{
			namespace:   namespace2,
			name:        name1,
			portInfoMap: negtypes.NewPortInfoMap(namespace1, name1, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}, negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}), namer, false, nil),
			selector:    map[string]string{labelKey1: labelValue1},
		},
		{
			namespace:   namespace2,
			name:        name2,
			portInfoMap: negtypes.NewPortInfoMap(namespace2, name2, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}, negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}, negtypes.SvcPortTuple{Port: port3, TargetPort: targetPort3}), namer, true, nil),
			selector:    map[string]string{labelKey2: labelValue2},
		},
		{
			namespace: namespace2,
			name:      name3,
			portInfoMap: portInfoUnion(negtypes.NewPortInfoMap(namespace2, name3, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port1, TargetPort: targetPort1}, negtypes.SvcPortTuple{Port: port2, TargetPort: targetPort2}, negtypes.SvcPortTuple{Port: port3, TargetPort: targetPort3}), namer, false, nil),
				negtypes.NewPortInfoMap(namespace2, name3, types.NewSvcPortTupleSet(negtypes.SvcPortTuple{Port: port4, TargetPort: targetPort4}), namer, true, nil)),
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

func createNegCR(service *v1.Service, svcKey serviceKey, portInfo negtypes.PortInfo) negv1beta1.ServiceNetworkEndpointGroup {
	ownerReference := metav1.NewControllerRef(service, service.GroupVersionKind())
	*ownerReference.BlockOwnerDeletion = false
	labels := map[string]string{
		negtypes.NegCRManagedByKey:   negtypes.NegCRControllerValue,
		negtypes.NegCRServiceNameKey: svcKey.name,
		negtypes.NegCRServicePortKey: fmt.Sprint(portInfo.PortTuple.Port),
	}

	return negv1beta1.ServiceNetworkEndpointGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:            portInfo.NegName,
			Namespace:       svcKey.namespace,
			OwnerReferences: []metav1.OwnerReference{*ownerReference},
			Labels:          labels,
			Finalizers:      []string{common.NegFinalizerKey},
			ResourceVersion: "rv",
		},
	}
}

// getNegCRs returns a list of NEG CRs under the specified namespace using the svcNegClient provided.
func getNegCRs(t *testing.T, svcNegClient svcnegclient.Interface, namespace string) []negv1beta1.ServiceNetworkEndpointGroup {
	crs, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(namespace).List(context2.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Errorf("failed to get neg crs")
	}
	return crs.Items
}

// populateSvcNegCache takes all the NEG CRs under the specified namespace using the provided the svcNegClient
// and use them to populate the manager's svcNeg cache.
func populateSvcNegCache(t *testing.T, manager *syncerManager, svcNegClient svcnegclient.Interface, namespace string) {
	crs, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(namespace).List(context2.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Errorf("failed to get neg crs")
	}
	for _, cr := range crs.Items {
		negCR := cr
		manager.svcNegLister.Add(&negCR)
	}
}
