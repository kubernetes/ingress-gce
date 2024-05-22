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

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	networkv1 "k8s.io/cloud-provider-gcp/crd/apis/network/v1"
	"k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

// TestLocalGetEndpointSet verifies the GetEndpointSet method implemented by the LocalL4ILBEndpointsCalculator.
// The L7 implementation is tested in TestToZoneNetworkEndpointMapUtil.
func TestLocalGetEndpointSet(t *testing.T) {
	t.Parallel()
	nodeInformer := zonegetter.FakeNodeInformer()
	zoneGetter := zonegetter.NewFakeZoneGetter(nodeInformer, defaultTestSubnetURL, false)
	zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
	defaultNetwork := network.NetworkInfo{IsDefault: true, K8sNetwork: "default"}

	testCases := []struct {
		desc                string
		endpointsData       []negtypes.EndpointsData
		wantEndpointSets    map[string]negtypes.NetworkEndpointSet
		networkEndpointType negtypes.NetworkEndpointType
		nodeLabelsMap       map[string]map[string]string
		nodeAnnotationsMap  map[string]map[string]string
		nodeReadyStatusMap  map[string]v1.ConditionStatus
		nodeNames           []string
		network             network.NetworkInfo
	}{
		{
			desc:          "default endpoints",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// only 4 out of 6 nodes are picked since there are > 4 endpoints, but they are found only on 4 nodes.
			wantEndpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			network:             defaultNetwork,
		},
		{
			desc:          "default endpoints, all nodes unready",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// only 4 out of 6 nodes are picked since there are > 4 endpoints, but they are found only on 4 nodes.
			wantEndpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			nodeReadyStatusMap: map[string]v1.ConditionStatus{
				testInstance1: v1.ConditionFalse, testInstance2: v1.ConditionFalse, testInstance3: v1.ConditionFalse, testInstance4: v1.ConditionFalse, testInstance5: v1.ConditionFalse, testInstance6: v1.ConditionFalse,
			},
			network: defaultNetwork,
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
			wantEndpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			network:             defaultNetwork,
		},
		{
			desc:          "no endpoints",
			endpointsData: []negtypes.EndpointsData{},
			// No nodes are picked as there are no service endpoints.
			wantEndpointSets:    nil,
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			network:             defaultNetwork,
		},
		{
			desc:          "multinetwork, endpoints only for nodes connected to a matching non-default network",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			network:       network.NetworkInfo{IsDefault: false, K8sNetwork: "other"},
			nodeAnnotationsMap: map[string]map[string]string{
				testInstance1: {networkv1.NorthInterfacesAnnotationKey: nodeInterfacesAnnotation(t, "other", "20.2.3.1")},
				testInstance2: {networkv1.NorthInterfacesAnnotationKey: nodeInterfacesAnnotation(t, "other", "20.2.3.2")},
				testInstance3: {networkv1.NorthInterfacesAnnotationKey: nodeInterfacesAnnotation(t, "other", "20.2.3.3")},
			},
			//networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames: []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			// only 3 out of 6 nodes are picked because only 3 have multi-nic annotation with a matching network name
			wantEndpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "20.2.3.1", Node: testInstance1},
					negtypes.NetworkEndpoint{IP: "20.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "20.2.3.3", Node: testInstance3}),
			},
		},
	}
	svcKey := fmt.Sprintf("%s/%s", testServiceName, testServiceNamespace)
	for _, tc := range testCases {
		ec := NewLocalL4ILBEndpointsCalculator(listers.NewNodeLister(nodeInformer.GetIndexer()), zoneGetter, svcKey, klog.TODO(), &tc.network)
		updateNodes(t, tc.nodeNames, tc.nodeLabelsMap, tc.nodeAnnotationsMap, tc.nodeReadyStatusMap, nodeInformer.GetIndexer())
		retSet, _, _, err := ec.CalculateEndpoints(tc.endpointsData, nil)
		if err != nil {
			t.Errorf("For case %q, expect nil error, but got %v.", tc.desc, err)
		}
		if diff := cmp.Diff(retSet, tc.wantEndpointSets); diff != "" {
			t.Errorf("For case %q, expecting endpoint set %v, but got %v.\nDiff: (-want +got):\n%s", tc.desc, tc.wantEndpointSets, retSet, diff)
		}
		degradedSet, _, err := ec.CalculateEndpointsDegradedMode(tc.endpointsData, nil)
		if err != nil {
			t.Errorf("For degraded mode case %q, expect nil error, but got %v.", tc.desc, err)
		}
		if diff := cmp.Diff(degradedSet, tc.wantEndpointSets); diff != "" {
			t.Errorf("For degraded mode case %q, expecting endpoint set %v, but got %v.\nDiff: (-want +got):\n%s", tc.desc, tc.wantEndpointSets, retSet, diff)
		}
	}
}

func nodeInterfacesAnnotation(t *testing.T, network, ip string) string {
	t.Helper()
	annotation, err := networkv1.MarshalNorthInterfacesAnnotation(networkv1.NorthInterfacesAnnotation{
		{
			Network:   network,
			IpAddress: ip,
		},
	})
	if err != nil {
		t.Errorf("could not create node annotations")
		return ""
	}
	return annotation
}

// TestClusterGetEndpointSet verifies the GetEndpointSet method implemented by the ClusterL4ILBEndpointsCalculator.
func TestClusterGetEndpointSet(t *testing.T) {
	t.Parallel()
	nodeInformer := zonegetter.FakeNodeInformer()
	zoneGetter := zonegetter.NewFakeZoneGetter(nodeInformer, defaultTestSubnetURL, false)
	zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
	defaultNetwork := network.NetworkInfo{IsDefault: true, K8sNetwork: "default"}
	testCases := []struct {
		desc                string
		endpointsData       []negtypes.EndpointsData
		wantEndpointSets    map[string]negtypes.NetworkEndpointSet
		networkEndpointType negtypes.NetworkEndpointType
		nodeLabelsMap       map[string]map[string]string
		nodeAnnotationsMap  map[string]map[string]string
		nodeReadyStatusMap  map[string]v1.ConditionStatus
		nodeNames           []string
		network             network.NetworkInfo
	}{
		{
			desc:          "default endpoints",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// all nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			wantEndpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
				negtypes.TestZone3: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.7", Node: testUnreadyInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.8", Node: testUnreadyInstance2}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			network:             defaultNetwork,
		},
		{
			desc:          "default endpoints, all nodes unready",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// all nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			wantEndpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
				negtypes.TestZone3: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.7", Node: testUnreadyInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.8", Node: testUnreadyInstance2}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			nodeReadyStatusMap: map[string]v1.ConditionStatus{
				testInstance1: v1.ConditionFalse, testInstance2: v1.ConditionFalse, testInstance3: v1.ConditionFalse, testInstance4: v1.ConditionFalse, testInstance5: v1.ConditionFalse, testInstance6: v1.ConditionFalse,
			},
			network: defaultNetwork,
		},
		{
			desc:          "default endpoints, some nodes marked as non-candidates",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			// all valid candidate nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			wantEndpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
				negtypes.TestZone3: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.7", Node: testUnreadyInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.8", Node: testUnreadyInstance2}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			nodeLabelsMap: map[string]map[string]string{
				testInstance1: {utils.LabelNodeRoleExcludeBalancer: "true"},
				testInstance3: {utils.GKECurrentOperationLabel: utils.NodeDrain},
				// label for testInstance4 will not remove it from endpoints list, since operation value is "random".
				testInstance4: {utils.GKECurrentOperationLabel: "random"},
			},
			network: defaultNetwork,
		},
		{
			desc: "no endpoints",
			// all nodes are picked since, in this mode, endpoints running do not need to run on the selected node.
			// Even when there are no service endpoints, nodes are selected at random.
			endpointsData: []negtypes.EndpointsData{},
			wantEndpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "1.2.3.4", Node: testInstance4},
					negtypes.NetworkEndpoint{IP: "1.2.3.5", Node: testInstance5}, negtypes.NetworkEndpoint{IP: "1.2.3.6", Node: testInstance6}),
				negtypes.TestZone3: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.2.3.7", Node: testUnreadyInstance1}, negtypes.NetworkEndpoint{IP: "1.2.3.8", Node: testUnreadyInstance2}),
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
			network:             defaultNetwork,
		},
		{
			desc:          "multinetwork endpoints, only for nodes connected to the specified network",
			endpointsData: negtypes.EndpointsDataFromEndpointSlices(getDefaultEndpointSlices()),
			wantEndpointSets: map[string]negtypes.NetworkEndpointSet{
				negtypes.TestZone1: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "20.2.3.1", Node: testInstance1}, negtypes.NetworkEndpoint{IP: "20.2.3.2", Node: testInstance2}),
				negtypes.TestZone2: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "20.2.3.3", Node: testInstance3}, negtypes.NetworkEndpoint{IP: "20.2.3.6", Node: testInstance6}),
			},
			network: network.NetworkInfo{IsDefault: false, K8sNetwork: "other"},
			nodeAnnotationsMap: map[string]map[string]string{
				testInstance1: {networkv1.NorthInterfacesAnnotationKey: nodeInterfacesAnnotation(t, "other", "20.2.3.1")},
				testInstance2: {networkv1.NorthInterfacesAnnotationKey: nodeInterfacesAnnotation(t, "other", "20.2.3.2")},
				testInstance3: {networkv1.NorthInterfacesAnnotationKey: nodeInterfacesAnnotation(t, "other", "20.2.3.3")},
				testInstance6: {networkv1.NorthInterfacesAnnotationKey: nodeInterfacesAnnotation(t, "other", "20.2.3.6")},
			},
			networkEndpointType: negtypes.VmIpEndpointType,
			nodeNames:           []string{testInstance1, testInstance2, testInstance3, testInstance4, testInstance5, testInstance6},
		},
	}
	svcKey := fmt.Sprintf("%s/%s", testServiceName, testServiceNamespace)
	for _, tc := range testCases {
		ec := NewClusterL4ILBEndpointsCalculator(listers.NewNodeLister(nodeInformer.GetIndexer()), zoneGetter, svcKey, klog.TODO(), &tc.network)
		updateNodes(t, tc.nodeNames, tc.nodeLabelsMap, tc.nodeAnnotationsMap, tc.nodeReadyStatusMap, nodeInformer.GetIndexer())
		retSet, _, _, err := ec.CalculateEndpoints(tc.endpointsData, nil)
		if err != nil {
			t.Errorf("For case %q, expect nil error, but got %v.", tc.desc, err)
		}
		if !reflect.DeepEqual(retSet, tc.wantEndpointSets) {
			t.Errorf("For case %q, expecting endpoint set %v, but got %v.", tc.desc, tc.wantEndpointSets, retSet)
		}
		degradedSet, _, err := ec.CalculateEndpointsDegradedMode(tc.endpointsData, nil)
		if err != nil {
			t.Errorf("For degraded mode case %q, expect nil error, but got %v.", tc.desc, err)
		}
		if !reflect.DeepEqual(degradedSet, tc.wantEndpointSets) {
			t.Errorf("For degraded mode case %q, expecting endpoint set %v, but got %v.", tc.desc, tc.wantEndpointSets, retSet)
		}
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
	svcPort := negtypes.NegSyncerKey{
		Namespace: testServiceNamespace,
		Name:      testServiceName,
		NegType:   negtypes.VmIpPortEndpointType,
		PortTuple: negtypes.SvcPortTuple{
			Port:       80,
			TargetPort: "8080",
			Name:       testPortName,
		},
		NegName: testNegName,
	}

	nodeInformer := zonegetter.FakeNodeInformer()
	zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
	zoneGetter := zonegetter.NewFakeZoneGetter(nodeInformer, defaultTestSubnetURL, false)
	testContext := negtypes.NewTestContext()
	podLister := testContext.PodInformer.GetIndexer()
	nodeLister := testContext.NodeInformer.GetIndexer()
	serviceLister := testContext.ServiceInformer.GetIndexer()
	L7EndpointsCalculator := NewL7EndpointsCalculator(zoneGetter, podLister, nodeLister, serviceLister, svcPort, klog.TODO(), testContext.EnableDualStackNEG, metricscollector.FakeSyncerMetrics())
	L4LocalEndpointCalculator := NewLocalL4ILBEndpointsCalculator(listers.NewNodeLister(nodeLister), zoneGetter, svcKey, klog.TODO(), &network.NetworkInfo{})
	L4ClusterEndpointCalculator := NewClusterL4ILBEndpointsCalculator(listers.NewNodeLister(nodeLister), zoneGetter, svcKey, klog.TODO(), &network.NetworkInfo{})

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

// updateNodes overwrites the label, annotation, or the readyStatus on a node if the node exists and the overwrite is provided.
func updateNodes(t *testing.T, nodeNames []string, nodeLabels map[string]map[string]string, nodeAnnotations map[string]map[string]string, nodeReadyStatus map[string]v1.ConditionStatus, nodeIndexer cache.Indexer) {
	t.Helper()
	for i, nodeName := range nodeNames {
		var labels, annotations map[string]string
		readyStatus := v1.ConditionTrue
		if nodeLabels != nil {
			labels = nodeLabels[nodeName]
		}
		if nodeAnnotations != nil {
			annotations = nodeAnnotations[nodeName]
		}
		if nodeReadyStatus != nil {
			status, ok := nodeReadyStatus[nodeName]
			if ok {
				readyStatus = status
			}
		}
		obj, exists, err := nodeIndexer.GetByKey(nodeName)
		if err != nil || !exists {
			t.Fatalf("Failed to get node %s in nodeLister, exists: %v, err: %v", nodeNames[i], exists, err)
		}
		node, ok := obj.(*v1.Node)
		if !ok {
			t.Fatalf("Failed to cast %v as type Node", obj)
		}
		node.ObjectMeta.Labels = labels
		node.ObjectMeta.Annotations = annotations
		node.Status.Conditions = []v1.NodeCondition{
			{
				Type:   v1.NodeReady,
				Status: readyStatus,
			},
		}
		err = nodeIndexer.Update(node)
		if err != nil {
			t.Fatalf("Failed to update node %q, err: %v", nodeName[i], err)
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
