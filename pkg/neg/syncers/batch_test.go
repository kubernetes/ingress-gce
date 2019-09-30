package syncers

import (
	"reflect"
	"testing"
	"time"

	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/context"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
)

const (
	testNegName          = "test-neg-name"
	testServiceNamespace = "test-ns"
	testServiceName      = "test-name"
	testNamedPort        = "named-Port"
	clusterID            = "clusterid"
)

var (
	defaultBackend = utils.ServicePort{
		ID: utils.ServicePortID{
			Service: types.NamespacedName{
				Name:      "default-http-backend",
				Namespace: "kube-system",
			},
			Port: intstr.FromString("http"),
		},
		TargetPort: "9376",
	}
)

func NewTestSyncer() *batchSyncer {
	kubeClient := fake.NewSimpleClientset()
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	namer := namer_util.NewNamer(clusterID, "")
	ctxConfig := context.ControllerContextConfig{
		Namespace:             apiv1.NamespaceAll,
		ResyncPeriod:          1 * time.Second,
		DefaultBackendSvcPort: defaultBackend,
	}
	context := context.NewControllerContext(kubeClient, nil, backendConfigClient, nil, nil, namer, ctxConfig)
	svcPort := negtypes.NegSyncerKey{
		Namespace:  testServiceNamespace,
		Name:       testServiceName,
		Port:       80,
		TargetPort: "80",
	}

	return NewBatchSyncer(svcPort,
		testNegName,
		record.NewFakeRecorder(100),
		negtypes.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-newtork"),
		negtypes.NewFakeZoneGetter(),
		context.ServiceInformer.GetIndexer(),
		context.EndpointInformer.GetIndexer(),
		context.PodInformer.GetIndexer())
}

func TestStartAndStopSyncer(t *testing.T) {
	syncer := NewTestSyncer()
	if !syncer.IsStopped() {
		t.Fatalf("Syncer is not stopped after creation.")
	}
	if syncer.IsShuttingDown() {
		t.Fatalf("Syncer is shutting down after creation.")
	}

	if err := syncer.Start(); err != nil {
		t.Fatalf("Failed to start syncer: %v", err)
	}
	if syncer.IsStopped() {
		t.Fatalf("Syncer is stopped after Start.")
	}
	if syncer.IsShuttingDown() {
		t.Fatalf("Syncer is shutting down after Start.")
	}

	syncer.Stop()
	if !syncer.IsStopped() {
		t.Fatalf("Syncer is not stopped after Stop.")
	}

	if err := wait.PollImmediate(time.Second, 30*time.Second, func() (bool, error) {
		return !syncer.IsShuttingDown() && syncer.IsStopped(), nil
	}); err != nil {
		t.Fatalf("Syncer failed to shutdown: %v", err)
	}

	if err := syncer.Start(); err != nil {
		t.Fatalf("Failed to restart syncer: %v", err)
	}
	if syncer.IsStopped() {
		t.Fatalf("Syncer is stopped after restart.")
	}
	if syncer.IsShuttingDown() {
		t.Fatalf("Syncer is shutting down after restart.")
	}

	syncer.Stop()
	if !syncer.IsStopped() {
		t.Fatalf("Syncer is not stopped after Stop.")
	}
}

func TestEnsureNetworkEndpointGroups(t *testing.T) {
	syncer := NewTestSyncer()
	if err := syncer.ensureNetworkEndpointGroups(); err != nil {
		t.Errorf("Failed to ensure NEGs: %v", err)
	}

	ret, _ := syncer.cloud.AggregatedListNetworkEndpointGroup()
	expectZones := []string{negtypes.TestZone1, negtypes.TestZone2}
	for _, zone := range expectZones {
		negs, ok := ret[zone]
		if !ok {
			t.Errorf("Failed to find zone %q from ret %v", zone, ret)
			continue
		}

		if len(negs) != 1 {
			t.Errorf("Unexpected negs %v", negs)
		} else {
			if negs[0].Name != testNegName {
				t.Errorf("Unexpected neg %q", negs[0].Name)
			}
		}
	}
}

func TestToZoneNetworkEndpointMap(t *testing.T) {
	syncer := NewTestSyncer()
	testCases := []struct {
		targetPort string
		expect     map[string]sets.String
	}{
		{
			targetPort: "80",
			expect: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("10.100.1.1||instance1||80", "10.100.1.2||instance1||80", "10.100.2.1||instance2||80"),
				negtypes.TestZone2: sets.NewString("10.100.3.1||instance3||80"),
			},
		},
		{
			targetPort: testNamedPort,
			expect: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("10.100.2.2||instance2||81"),
				negtypes.TestZone2: sets.NewString("10.100.4.1||instance4||81", "10.100.3.2||instance3||8081", "10.100.4.2||instance4||8081"),
			},
		},
	}

	for _, tc := range testCases {
		syncer.TargetPort = tc.targetPort
		res, _ := syncer.toZoneNetworkEndpointMap(getDefaultEndpoint(), "")

		if !reflect.DeepEqual(res, tc.expect) {
			t.Errorf("Expect %v, but got %v.", tc.expect, res)
		}
	}
}

func TestSyncNetworkEndpoints(t *testing.T) {
	syncer := NewTestSyncer()
	if err := syncer.ensureNetworkEndpointGroups(); err != nil {
		t.Fatalf("Failed to ensure NEG: %v", err)
	}

	testCases := []struct {
		expectSet map[string]sets.String
		addSet    map[string]sets.String
		removeSet map[string]sets.String
	}{
		{
			expectSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("10.100.1.1||instance1||80", "10.100.2.1||instance2||80"),
				negtypes.TestZone2: sets.NewString("10.100.3.1||instance3||80", "10.100.4.1||instance4||80"),
			},
			addSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("10.100.1.1||instance1||80", "10.100.2.1||instance2||80"),
				negtypes.TestZone2: sets.NewString("10.100.3.1||instance3||80", "10.100.4.1||instance4||80"),
			},
			removeSet: map[string]sets.String{},
		},
		{
			expectSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("10.100.1.2||instance1||80"),
				negtypes.TestZone2: sets.NewString(),
			},
			addSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("10.100.1.2||instance1||80"),
			},
			removeSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("10.100.1.1||instance1||80", "10.100.2.1||instance2||80"),
				negtypes.TestZone2: sets.NewString("10.100.3.1||instance3||80", "10.100.4.1||instance4||80"),
			},
		},
		{
			expectSet: map[string]sets.String{
				negtypes.TestZone1: sets.NewString("10.100.1.2||instance1||80"),
				negtypes.TestZone2: sets.NewString("10.100.3.2||instance3||80"),
			},
			addSet: map[string]sets.String{
				negtypes.TestZone2: sets.NewString("10.100.3.2||instance3||80"),
			},
			removeSet: map[string]sets.String{},
		},
	}

	for _, tc := range testCases {
		if err := syncer.syncNetworkEndpoints(tc.addSet, tc.removeSet); err != nil {
			t.Fatalf("Failed to sync network endpoints: %v", err)
		}
		examineNetworkEndpoints(tc.expectSet, syncer, t)
	}
}

func examineNetworkEndpoints(expectSet map[string]sets.String, syncer *batchSyncer, t *testing.T) {
	for zone, endpoints := range expectSet {
		expectEndpoints, err := syncer.toNetworkEndpointBatch(endpoints)
		if err != nil {
			t.Fatalf("Failed to convert endpoints to network endpoints: %v", err)
		}
		if cloudEndpoints, err := syncer.cloud.ListNetworkEndpoints(syncer.negName, zone, false); err == nil {
			if len(expectEndpoints) != len(cloudEndpoints) {
				t.Errorf("Expect number of endpoints to be %v, but got %v.", len(expectEndpoints), len(cloudEndpoints))
			}
			for _, expectEp := range expectEndpoints {
				found := false
				for _, cloudEp := range cloudEndpoints {
					if reflect.DeepEqual(*expectEp, *cloudEp.NetworkEndpoint) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Endpoint %v not found.", expectEp)
				}
			}
		} else {
			t.Errorf("Failed to list network endpoints in zone %q: %v.", zone, err)
		}
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
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod1",
						},
					},
					{
						IP:       "10.100.1.2",
						NodeName: &instance1,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod2",
						},
					},
					{
						IP:       "10.100.2.1",
						NodeName: &instance2,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod3",
						},
					},
					{
						IP:       "10.100.3.1",
						NodeName: &instance3,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod4",
						},
					},
				},
				NotReadyAddresses: []apiv1.EndpointAddress{
					{
						IP:       "10.100.1.3",
						NodeName: &instance1,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod5",
						},
					},
					{
						IP:       "10.100.1.4",
						NodeName: &instance1,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod6",
						},
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
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod7",
						},
					},
					{
						IP:       "10.100.4.1",
						NodeName: &instance4,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod8",
						},
					},
				},
				NotReadyAddresses: []apiv1.EndpointAddress{
					{
						IP:       "10.100.4.3",
						NodeName: &instance4,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod9",
						},
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
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod10",
						},
					},
					{
						IP:       "10.100.4.2",
						NodeName: &instance4,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod11",
						},
					},
				},
				NotReadyAddresses: []apiv1.EndpointAddress{
					{
						IP:       "10.100.4.4",
						NodeName: &instance4,
						TargetRef: &v1.ObjectReference{
							Namespace: testServiceNamespace,
							Name:      "pod12",
						},
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
