/*
Copyright 2020 The Kubernetes Authors.

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

package syncers

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
	"k8s.io/legacy-cloud-providers/gce"
)

// TestLocalGetEndpointSet verifies the GetEndpointSet method implemented by the LocalL4ILBEndpointsCalculator.
// The L7 implementation is tested in TestToZoneNetworkEndpointMapUtil.
func TestLocalGetEndpointSet(t *testing.T) {
	t.Parallel()
	mode := negtypes.L4LocalMode
	_, transactionSyncer := newL4ILBTestTransactionSyncer(negtypes.NewAdapter(gce.NewFakeGCECloud(gce.DefaultTestClusterValues())), mode)
	zoneGetter := negtypes.NewFakeZoneGetter()
	nodeLister := listers.NewNodeLister(transactionSyncer.nodeLister)

	testCases := []struct {
		desc                string
		endpointsData       []negtypes.EndpointsData
		endpointSets        map[string]negtypes.NetworkEndpointSet
		networkEndpointType negtypes.NetworkEndpointType
		nodeLabelsMap       map[string]map[string]string
		nodeReadyStatusMap  map[string]v1.ConditionStatus
		nodeNames           []string
	}{
		{
			desc:          "default endpoints",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// only 4 out of 6 nodes are picked since there are > 4 endpoints, but they are found only on 4 nodes.
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
		},
		{
			desc:          "default endpoints, all nodes unready",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// only 4 out of 6 nodes are picked since there are > 4 endpoints, but they are found only on 4 nodes.
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			nodeReadyStatusMap: map[string]v1.ConditionStatus{
				testInstance1: v1.ConditionFalse, testInstance2: v1.ConditionFalse, testInstance3: v1.ConditionFalse, testInstance4: v1.ConditionFalse, testInstance5: v1.ConditionFalse, testInstance6: v1.ConditionFalse,
			},
		},
		{
			desc:          "default endpoints, some nodes marked as non-candidates",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			nodeLabelsMap: map[string]map[string]string{
				testInstance1: {utils.LabelNodeRoleExcludeBalancer: "true"},
				testInstance3: {utils.GKECurrentOperationLabel: utils.NodeDrain},
				// label for testInstance4 will not remove it from endpoints list, since operation value is "random".
				testInstance4: {utils.GKECurrentOperationLabel: "random"},
			},
			nodeNames: []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			// only 2 out of 6 nodes are picked since there are > 4 endpoints, but they are found only on 4 nodes. 2 out of those 4 are non-candidates.
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
		},
		{
			desc:          "no endpoints",
			endpointsData: []negtypes.EndpointsData{},
			// No nodes are picked as there are no service endpoints.
			endpointSets:        nil,
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
		},
	}
	svcKey := fmt.Sprintf("%s/%s", testServiceName, testServiceNamespace)
	ec := NewLocalL4ILBEndpointsCalculator(nodeLister, zoneGetter, svcKey, klog.TODO())
	for _, tc := range testCases {
		createNodes(t, tc.nodeNames, tc.nodeLabelsMap, tc.nodeReadyStatusMap, transactionSyncer.nodeLister)
		retSet, _, _, err := ec.CalculateEndpoints(tc.endpointsData, nil, false)
		if err != nil {
			t.Errorf("For case %q, expect nil error, but got %v.", tc.desc, err)
		}
		if !reflect.DeepEqual(retSet, tc.endpointSets) {
			t.Errorf("For case %q, expecting endpoint set %v, but got %v.", tc.desc, tc.endpointSets, retSet)
		}
		deleteNodes(t, tc.nodeNames, transactionSyncer.nodeLister)
	}
}

// TestClusterGetEndpointSet verifies the GetEndpointSet method implemented by the ClusterL4ILBEndpointsCalculator.
func TestClusterGetEndpointSet(t *testing.T) {
	t.Parallel()
	mode := negtypes.L4ClusterMode
	// The "enableEndpointSlices=false" case is enough since this test does not depend on endpoints at all.
	_, transactionSyncer := newL4ILBTestTransactionSyncer(negtypes.NewAdapter(gce.NewFakeGCECloud(gce.DefaultTestClusterValues())), mode)
	zoneGetter := negtypes.NewFakeZoneGetter()
	nodeLister := listers.NewNodeLister(transactionSyncer.nodeLister)
	testCases := []struct {
		desc                string
		endpointsData       []negtypes.EndpointsData
		endpointSets        map[string]negtypes.NetworkEndpointSet
		networkEndpointType negtypes.NetworkEndpointType
		nodeLabelsMap       map[string]map[string]string
		nodeAnnotationsMap  map[string]map[string]string
		nodeReadyStatusMap  map[string]v1.ConditionStatus
		nodeNames           []string
	}{
		{
			desc:          "default endpoints",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// all nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
		},
		{
			desc:          "default endpoints, all nodes unready",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// all nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			nodeReadyStatusMap: map[string]v1.ConditionStatus{
				testInstance1: v1.ConditionFalse, testInstance2: v1.ConditionFalse, testInstance3: v1.ConditionFalse, testInstance4: v1.ConditionFalse, testInstance5: v1.ConditionFalse, testInstance6: v1.ConditionFalse,
			},
		},
		{
			desc:          "default endpoints, some nodes marked as non-candidates",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// all valid candidate nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			nodeLabelsMap: map[string]map[string]string{
				testInstance1: {utils.LabelNodeRoleExcludeBalancer: "true"},
				testInstance3: {utils.GKECurrentOperationLabel: utils.NodeDrain},
				// label for testInstance4 will not remove it from endpoints list, since operation value is "random".
				testInstance4: {utils.GKECurrentOperationLabel: "random"},
			},
		},
		{
			desc: "no endpoints",
			// all nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			// Even when there are no service endpoints, nodes are selected at random.
			endpointsData: []negtypes.EndpointsData{},
			endpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
		},
	}
	svcKey := fmt.Sprintf("%s/%s", testServiceName, testServiceNamespace)
	ec := NewClusterL4ILBEndpointsCalculator(nodeLister, zoneGetter, svcKey, klog.TODO())
	for _, tc := range testCases {
		createNodes(t, tc.nodeNames, tc.nodeLabelsMap, tc.nodeReadyStatusMap, transactionSyncer.nodeLister)
		retSet, _, _, err := ec.CalculateEndpoints(tc.endpointsData, nil, false)
		if err != nil {
			t.Errorf("For case %q, expect nil error, but got %v.", tc.desc, err)
		}
		if !reflect.DeepEqual(retSet, tc.endpointSets) {
			t.Errorf("For case %q, expecting endpoint set %v, but got %v.", tc.desc, tc.endpointSets, retSet)
		}
		deleteNodes(t, tc.nodeNames, transactionSyncer.nodeLister)
	}
}

func TestValidateEndpoints(t *testing.T) {
	t.Parallel()

	instance1 := testInstance1
	instance2 := testInstance2
	instance4 := testInstance4
	testPortName := ""
	testServiceNamespace := "namespace"
	port80 := int32(80)
	port81 := int32(81)
	testIP1 := "10.100.1.1"
	testIP2 := "10.100.1.2"
	testIP3 := "10.100.2.2"
	testIP4 := "10.100.4.1"
	testPodName1 := "pod1"
	testPodName2 := "pod2"
	testPodName3 := "pod3"
	testPodName4 := "pod4"
	svcKey := fmt.Sprintf("%s/%s", testServiceName, testServiceNamespace)

	zoneGetter := negtypes.NewFakeZoneGetter()
	testContext := negtypes.NewTestContext()
	podLister := testContext.PodInformer.GetIndexer()
	nodeLister := testContext.NodeInformer.GetIndexer()
	serviceLister := testContext.ServiceInformer.GetIndexer()
	L7EndpointsCalculator := NewL7EndpointsCalculator(zoneGetter, podLister, nodeLister, serviceLister, testPortName, negtypes.VmIpPortEndpointType, false, klog.TODO(), negtypes.PodLabelPropagationConfig{})
	L4LocalEndpointCalculator := NewLocalL4ILBEndpointsCalculator(listers.NewNodeLister(nodeLister), zoneGetter, svcKey, klog.TODO())
	L4ClusterEndpointCalculator := NewClusterL4ILBEndpointsCalculator(listers.NewNodeLister(nodeLister), zoneGetter, svcKey, klog.TODO())

	testEndpointPodMap := map[negtypes.NetworkEndpoint]types.NamespacedName{
		{
			IP:   testIP1,
			Port: "80",
			Node: instance1,
		}: {
			Namespace: testServiceNamespace,
			Name:      testPodName1,
		},
		{
			IP:   testIP2,
			Port: "80",
			Node: instance1,
		}: {
			Namespace: testServiceNamespace,
			Name:      testPodName2,
		},
		{
			IP:   testIP3,
			Port: "81",
			Node: instance2,
		}: {
			Namespace: testServiceNamespace,
			Name:      testPodName3,
		},
		{
			IP:   testIP4,
			Port: "81",
			Node: instance4,
		}: {
			Namespace: testServiceNamespace,
			Name:      testPodName4,
		},
	}

	testCases := []struct {
		desc           string
		ec             negtypes.NetworkEndpointsCalculator
		endpointsData  []negtypes.EndpointsData
		endpointPodMap map[negtypes.NetworkEndpoint]types.NamespacedName
		dupCount       int
		expect         error
	}{
		{
			desc:           "ValidateEndpoints for L4 local endpoint calculator", // we are adding this test to make sure the test is updated when the functionality is added
			ec:             L4LocalEndpointCalculator,
			endpointsData:  nil, // for now it is a no-op
			endpointPodMap: nil, // L4 EndpointCalculators do not compute endpoints pod data
			dupCount:       0,
			expect:         nil,
		},
		{

			desc:           "ValidateEndpoints for L4 cluster endpoint calculator", // we are adding this test to make sure the test is updated when the functionality is added
			ec:             L4ClusterEndpointCalculator,
			endpointsData:  nil, // for now it is a no-op
			endpointPodMap: nil, // L4 EndpointCalculators do not compute endpoints pod data
			dupCount:       0,
			expect:         nil,
		},
		{
			desc: "ValidateEndpoints for L7 Endpoint Calculator. Endpoint counts equal, endpointData has no duplicated endpoints",
			ec:   L7EndpointsCalculator,
			endpointsData: []negtypes.EndpointsData{
				{
					Meta: &metav1.ObjectMeta{
						Name:      testServiceName + "-1",
						Namespace: testServiceNamespace,
					},
					Ports: []negtypes.PortData{
						{
							Name: testPortName,
							Port: port80,
						},
					},
					Addresses: []negtypes.AddressData{
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName1,
							},
							NodeName:  &instance1,
							Addresses: []string{testIP1},
							Ready:     true,
						},
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName2,
							},
							NodeName:  &instance1,
							Addresses: []string{testIP2},
							Ready:     true,
						},
					},
				},
				{
					Meta: &metav1.ObjectMeta{
						Name:      testServiceName + "-2",
						Namespace: testServiceNamespace,
					},
					Ports: []negtypes.PortData{
						{
							Name: testPortName,
							Port: port81,
						},
					},
					Addresses: []negtypes.AddressData{
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName3,
							},
							NodeName:  &instance2,
							Addresses: []string{testIP3},
							Ready:     true,
						},
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName4,
							},
							NodeName:  &instance4,
							Addresses: []string{testIP4},
							Ready:     true,
						},
					},
				},
			},
			endpointPodMap: testEndpointPodMap,
			dupCount:       0,
			expect:         nil,
		},
		{
			desc: "ValidateEndpoints for L7 Endpoint Calculator. Endpoint counts equal, endpointData has duplicated endpoints",
			ec:   L7EndpointsCalculator,
			endpointsData: []negtypes.EndpointsData{
				{
					Meta: &metav1.ObjectMeta{
						Name:      testServiceName + "-1",
						Namespace: testServiceNamespace,
					},
					Ports: []negtypes.PortData{
						{
							Name: testPortName,
							Port: port80,
						},
					},
					Addresses: []negtypes.AddressData{
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName1,
							},
							NodeName:  &instance1,
							Addresses: []string{testIP1},
							Ready:     true,
						},
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName2,
							},
							NodeName:  &instance1,
							Addresses: []string{testIP2},
							Ready:     true,
						},
					},
				},
				{
					Meta: &metav1.ObjectMeta{
						Name:      testServiceName + "-2",
						Namespace: testServiceNamespace,
					},
					Ports: []negtypes.PortData{
						{
							Name: testPortName,
							Port: port81,
						},
					},
					Addresses: []negtypes.AddressData{
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName2,
							},
							NodeName:  &instance1,
							Addresses: []string{testIP2},
							Ready:     true,
						}, // this is a duplicated endpoint
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName3,
							},
							NodeName:  &instance2,
							Addresses: []string{testIP3},
							Ready:     true,
						},
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName4,
							},
							NodeName:  &instance4,
							Addresses: []string{testIP4},
							Ready:     true,
						},
					},
				},
			},
			endpointPodMap: testEndpointPodMap,
			dupCount:       1,
			expect:         nil,
		},
		{
			desc: "ValidateEndpoints for L7 Endpoint Calculator. Endpoint counts not equal, endpointData has no duplicated endpoints",
			ec:   L7EndpointsCalculator,
			endpointsData: []negtypes.EndpointsData{
				{
					Meta: &metav1.ObjectMeta{
						Name:      testServiceName + "-1",
						Namespace: testServiceNamespace,
					},
					Ports: []negtypes.PortData{
						{
							Name: testPortName,
							Port: port80,
						},
					},
					Addresses: []negtypes.AddressData{
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName2,
							},
							NodeName:  &instance1,
							Addresses: []string{testIP2},
							Ready:     true,
						},
					},
				},
				{
					Meta: &metav1.ObjectMeta{
						Name:      testServiceName + "-2",
						Namespace: testServiceNamespace,
					},
					Ports: []negtypes.PortData{
						{
							Name: testPortName,
							Port: port81,
						},
					},
					Addresses: []negtypes.AddressData{
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName3,
							},
							NodeName:  &instance2,
							Addresses: []string{testIP3},
							Ready:     true,
						},
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName4,
							},
							NodeName:  &instance4,
							Addresses: []string{testIP4},
							Ready:     true,
						},
					},
				},
			},
			endpointPodMap: testEndpointPodMap,
			dupCount:       0,
			expect:         negtypes.ErrEPCountsDiffer,
		},
		{
			desc: "ValidateEndpoints for L7 Endpoint Calculator. Endpoint counts not equal, endpointData has duplicated endpoints",
			ec:   L7EndpointsCalculator,
			endpointsData: []negtypes.EndpointsData{
				{
					Meta: &metav1.ObjectMeta{
						Name:      testServiceName + "-1",
						Namespace: testServiceNamespace,
					},
					Ports: []negtypes.PortData{
						{
							Name: testPortName,
							Port: port80,
						},
					},
					Addresses: []negtypes.AddressData{
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName2,
							},
							NodeName:  &instance1,
							Addresses: []string{testIP2},
							Ready:     true,
						},
					},
				},
				{
					Meta: &metav1.ObjectMeta{
						Name:      testServiceName + "-2",
						Namespace: testServiceNamespace,
					},
					Ports: []negtypes.PortData{
						{
							Name: testPortName,
							Port: port81,
						},
					},
					Addresses: []negtypes.AddressData{
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName2,
							},
							NodeName:  &instance1,
							Addresses: []string{testIP2},
							Ready:     true,
						}, // this is a duplicated endpoint
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName3,
							},
							NodeName:  &instance2,
							Addresses: []string{testIP3},
							Ready:     true,
						},
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName4,
							},
							NodeName:  &instance4,
							Addresses: []string{testIP4},
							Ready:     true,
						},
					},
				},
			},
			endpointPodMap: testEndpointPodMap,
			dupCount:       1,
			expect:         negtypes.ErrEPCountsDiffer,
		},
		{
			desc: "ValidateEndpoints for L7 Endpoint Calculator. EndpointData has zero endpoint",
			ec:   L7EndpointsCalculator,
			endpointsData: []negtypes.EndpointsData{
				{
					Meta: &metav1.ObjectMeta{
						Name:      testServiceName + "-1",
						Namespace: testServiceNamespace,
					},
					Ports: []negtypes.PortData{
						{
							Name: testPortName,
							Port: port80,
						},
					},
					Addresses: []negtypes.AddressData{},
				},
				{
					Meta: &metav1.ObjectMeta{
						Name:      testServiceName + "-2",
						Namespace: testServiceNamespace,
					},
					Ports: []negtypes.PortData{
						{
							Name: testPortName,
							Port: port81,
						},
					},
					Addresses: []negtypes.AddressData{},
				},
			},
			endpointPodMap: testEndpointPodMap,
			dupCount:       0,
			expect:         negtypes.ErrEPSEndpointCountZero,
		},
		{
			desc: "ValidateEndpoints for L7 Endpoint Calculator. EndpointPodMap has zero endpoint",
			ec:   L7EndpointsCalculator,
			endpointsData: []negtypes.EndpointsData{
				{
					Meta: &metav1.ObjectMeta{
						Name:      testServiceName + "-1",
						Namespace: testServiceNamespace,
					},
					Ports: []negtypes.PortData{
						{
							Name: testPortName,
							Port: port80,
						},
					},
					Addresses: []negtypes.AddressData{
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName1,
							},
							NodeName:  &instance1,
							Addresses: []string{testIP1},
							Ready:     true,
						},
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName2,
							},
							NodeName:  &instance1,
							Addresses: []string{testIP2},
							Ready:     true,
						},
					},
				},
				{
					Meta: &metav1.ObjectMeta{
						Name:      testServiceName + "-2",
						Namespace: testServiceNamespace,
					},
					Ports: []negtypes.PortData{
						{
							Name: testPortName,
							Port: port81,
						},
					},
					Addresses: []negtypes.AddressData{
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName3,
							},
							NodeName:  &instance2,
							Addresses: []string{testIP3},
							Ready:     true,
						},
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName4,
							},
							NodeName:  &instance4,
							Addresses: []string{testIP4},
							Ready:     true,
						},
					},
				},
			},
			endpointPodMap: map[negtypes.NetworkEndpoint]types.NamespacedName{},
			dupCount:       0,
			expect:         negtypes.ErrEPCalculationCountZero,
		},
		{
			desc: "ValidateEndpoints for L7 Endpoint Calculator. EndpointData and endpointPodMap both have zero endpoint",
			ec:   L7EndpointsCalculator,
			endpointsData: []negtypes.EndpointsData{
				{
					Meta: &metav1.ObjectMeta{
						Name:      testServiceName + "-1",
						Namespace: testServiceNamespace,
					},
					Ports: []negtypes.PortData{
						{
							Name: testPortName,
							Port: port80,
						},
					},
					Addresses: []negtypes.AddressData{},
				},
				{
					Meta: &metav1.ObjectMeta{
						Name:      testServiceName + "-2",
						Namespace: testServiceNamespace,
					},
					Ports: []negtypes.PortData{
						{
							Name: testPortName,
							Port: port81,
						},
					},
					Addresses: []negtypes.AddressData{},
				},
			},
			endpointPodMap: map[negtypes.NetworkEndpoint]types.NamespacedName{},
			dupCount:       0,
			expect:         negtypes.ErrEPCalculationCountZero, // PodMap count is check and returned first,
		},
		{
			desc: "ValidateEndpoints for L7 Endpoint Calculator. EndpointData and endpointPodMap both have non-zero endpoints",
			ec:   L7EndpointsCalculator,
			endpointsData: []negtypes.EndpointsData{
				{
					Meta: &metav1.ObjectMeta{
						Name:      testServiceName + "-1",
						Namespace: testServiceNamespace,
					},
					Ports: []negtypes.PortData{
						{
							Name: testPortName,
							Port: port80,
						},
					},
					Addresses: []negtypes.AddressData{
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName1,
							},
							NodeName:  &instance1,
							Addresses: []string{testIP1},
							Ready:     true,
						},
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName2,
							},
							NodeName:  &instance1,
							Addresses: []string{testIP2},
							Ready:     true,
						},
					},
				},
				{
					Meta: &metav1.ObjectMeta{
						Name:      testServiceName + "-2",
						Namespace: testServiceNamespace,
					},
					Ports: []negtypes.PortData{
						{
							Name: testPortName,
							Port: port81,
						},
					},
					Addresses: []negtypes.AddressData{
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName3,
							},
							NodeName:  &instance2,
							Addresses: []string{testIP3},
							Ready:     true,
						},
						{
							TargetRef: &v1.ObjectReference{
								Namespace: testServiceNamespace,
								Name:      testPodName4,
							},
							NodeName:  &instance4,
							Addresses: []string{testIP4},
							Ready:     true,
						},
					},
				},
			},
			endpointPodMap: testEndpointPodMap,
			dupCount:       0,
			expect:         nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			if got := tc.ec.ValidateEndpoints(tc.endpointsData, tc.endpointPodMap, tc.dupCount); !errors.Is(got, tc.expect) {
				t.Errorf("ValidateEndpoints() = %t,  expected %t", got, tc.expect)
			}
		})
	}
}

// TestEnableDegradedMode verifies if DegradedMode has been correctly enabled for L7 endpoint calculator
func TestEnableDegradedMode(t *testing.T) {
	t.Parallel()

	testPortName := ""
	zoneGetter := negtypes.NewFakeZoneGetter()
	testContext := negtypes.NewTestContext()
	podLister := testContext.PodInformer.GetIndexer()
	nodeLister := testContext.NodeInformer.GetIndexer()
	serviceLister := testContext.ServiceInformer.GetIndexer()
	testEndpointSlices := getDefaultEndpointSlices()
	addPodsToLister(podLister, testEndpointSlices)

	for i := 1; i <= 4; i++ {
		nodeLister.Add(&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("instance%v", i),
			},
			Spec: v1.NodeSpec{
				PodCIDR:  fmt.Sprintf("10.100.%v.0/24", i),
				PodCIDRs: []string{fmt.Sprintf("200%v:db8::/48", i), fmt.Sprintf("10.100.%v.0/24", i)},
			},
		})
	}

	serviceLister.Add(&v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testServiceNamespace,
			Name:      testServiceName,
		},
		Spec: v1.ServiceSpec{
			Selector: map[string]string{
				"run": "foo",
			},
		},
	})

	validEndpointData := negtypes.EndpointsDataFromEndpointSlices(testEndpointSlices)
	invalidEndpointData := negtypes.EndpointsDataFromEndpointSlices(testEndpointSlices)
	invalidEndpointData[0].Addresses[0].NodeName = nil

	endpointSets := map[string]negtypes.NetworkEndpointSet{
		negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
			networkEndpointFromEncodedEndpoint("10.100.1.1||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.1.2||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.1.3||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.1.4||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.2.1||instance2||80")),
		negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
			networkEndpointFromEncodedEndpoint("10.100.3.1||instance3||80")),
	}
	endpointMap := negtypes.EndpointPodMap{
		networkEndpointFromEncodedEndpoint("10.100.1.1||instance1||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod1"},
		networkEndpointFromEncodedEndpoint("10.100.1.2||instance1||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod2"},
		networkEndpointFromEncodedEndpoint("10.100.2.1||instance2||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod3"},
		networkEndpointFromEncodedEndpoint("10.100.3.1||instance3||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod4"},
		networkEndpointFromEncodedEndpoint("10.100.1.3||instance1||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod5"},
		networkEndpointFromEncodedEndpoint("10.100.1.4||instance1||80"): types.NamespacedName{Namespace: testServiceNamespace, Name: "pod6"},
	}
	testCases := []struct {
		desc         string
		ec           negtypes.NetworkEndpointsCalculator
		endpointData []negtypes.EndpointsData
		inErrorState bool
		expectedSets map[string]negtypes.NetworkEndpointSet
		expectedMap  negtypes.EndpointPodMap
		expectErr    error
	}{
		{
			desc:         "disable degraded mode, not in error state, using valid endpoint",
			ec:           NewL7EndpointsCalculator(zoneGetter, podLister, nodeLister, serviceLister, testPortName, negtypes.VmIpPortEndpointType, false, klog.TODO(), negtypes.PodLabelPropagationConfig{}),
			endpointData: validEndpointData,
			inErrorState: true,
			expectedSets: endpointSets,
			expectedMap:  endpointMap,
			expectErr:    nil,
		},
		{
			desc:         "disable degraded mode, not in error state, using invalid endpoint",
			ec:           NewL7EndpointsCalculator(zoneGetter, podLister, nodeLister, serviceLister, testPortName, negtypes.VmIpPortEndpointType, false, klog.TODO(), negtypes.PodLabelPropagationConfig{}),
			endpointData: invalidEndpointData,
			inErrorState: false,
			expectedSets: nil,
			expectedMap:  nil,
			expectErr:    negtypes.ErrEPNodeMissing,
		},
		{
			desc:         "disable degraded mode, in error state, using invalid endpoint",
			ec:           NewL7EndpointsCalculator(zoneGetter, podLister, nodeLister, serviceLister, testPortName, negtypes.VmIpPortEndpointType, false, klog.TODO(), negtypes.PodLabelPropagationConfig{}),
			endpointData: invalidEndpointData,
			inErrorState: true,
			expectedSets: nil,
			expectedMap:  nil,
			expectErr:    negtypes.ErrEPNodeMissing,
		},
		{
			desc:         "enable degraded mode, not in error state, using valid endpoint",
			ec:           NewL7EndpointsCalculator(zoneGetter, podLister, nodeLister, serviceLister, testPortName, negtypes.VmIpPortEndpointType, true, klog.TODO(), negtypes.PodLabelPropagationConfig{}),
			endpointData: validEndpointData,
			inErrorState: false,
			expectedSets: endpointSets,
			expectedMap:  endpointMap,
			expectErr:    nil,
		},
		{
			desc:         "enable degraded mode, not in error state, using invalid endpoint",
			ec:           NewL7EndpointsCalculator(zoneGetter, podLister, nodeLister, serviceLister, testPortName, negtypes.VmIpPortEndpointType, true, klog.TODO(), negtypes.PodLabelPropagationConfig{}),
			endpointData: invalidEndpointData,
			inErrorState: false,
			expectedSets: nil,
			expectedMap:  nil,
			expectErr:    negtypes.ErrEPNodeMissing,
		},
		{
			desc:         "enable degraded mode, in error state, using invalid endpoint",
			ec:           NewL7EndpointsCalculator(zoneGetter, podLister, nodeLister, serviceLister, testPortName, negtypes.VmIpPortEndpointType, true, klog.TODO(), negtypes.PodLabelPropagationConfig{}),
			endpointData: invalidEndpointData,
			inErrorState: true,
			expectedSets: endpointSets,
			expectedMap:  endpointMap,
			expectErr:    nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			retSet, retMap, _, err := tc.ec.CalculateEndpoints(tc.endpointData, nil, tc.inErrorState)
			if !reflect.DeepEqual(retSet, tc.expectedSets) {
				t.Errorf("For case %q, expecting endpoint set %v, but got %v.", tc.desc, tc.expectedSets, retSet)
			}
			if !reflect.DeepEqual(retMap, tc.expectedMap) {
				t.Errorf("For case %q, expecting endpoint map %v, but got %v.", tc.desc, tc.expectedMap, retMap)
			}
			if !errors.Is(err, tc.expectErr) {
				t.Errorf("For case %q, expect error %v, but got %v.", tc.desc, tc.expectErr, err)
			}
		})
	}
}

func createNodes(t *testing.T, nodeNames []string, nodeLabels map[string]map[string]string, nodeReadyStatus map[string]v1.ConditionStatus, nodeIndexer cache.Indexer) {
	t.Helper()
	for i, nodeName := range nodeNames {
		var labels, annotations map[string]string
		readyStatus := v1.ConditionTrue
		if nodeLabels != nil {
			labels = nodeLabels[nodeName]
		}
		if nodeReadyStatus != nil {
			status, ok := nodeReadyStatus[nodeName]
			if ok {
				readyStatus = status
			}
		}

		err := nodeIndexer.Add(&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:        nodeName,
				Labels:      labels,
				Annotations: annotations,
			},
			Status: v1.NodeStatus{
				Addresses: []v1.NodeAddress{
					{
						Type:    v1.NodeInternalIP,
						Address: fmt.Sprintf("1.2.3.%d", i+1),
					},
				},
				Conditions: []v1.NodeCondition{
					{
						Type:   v1.NodeReady,
						Status: readyStatus,
					},
				},
			},
		})
		if err != nil {
			t.Errorf("Failed to add node %s to syncer's nodeLister, err %v", nodeNames[i], err)
		}
	}

}

func deleteNodes(t *testing.T, nodeNames []string, nodeIndexer cache.Indexer) {
	t.Helper()
	for _, nodeName := range nodeNames {
		node, exists, err := nodeIndexer.GetByKey(nodeName)
		if err != nil || !exists {
			t.Errorf("Could not lookup node %q, err - %v", nodeName, err)
			continue
		}
		if err := nodeIndexer.Delete(node); err != nil {
			t.Errorf("Failed to delete node %q, err - %v", nodeName, err)
		}
	}
}
