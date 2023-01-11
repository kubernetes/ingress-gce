/*
Copyright 2019 The Kubernetes Authors.

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

package readiness

import (
	"context"
	"fmt"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/neg/types/shared"
	"net"
	"reflect"
	"testing"
)

const (
	testNamespace = "ns"
	instance1     = "instance1"
	instance2     = "instance2"
	instance3     = "instance3"
	instance4     = "instance4"
)

func TestNegReadinessConditionStatus(t *testing.T) {
	namespace := "ns"
	name := "n"
	for _, tc := range []struct {
		desc            string
		pod             *v1.Pod
		expectCondition v1.PodCondition
		expectFound     bool
	}{
		{
			desc: "nil input",
			pod:  nil,
		},
		{
			desc: "pod does not have neg condition",
			pod:  generatePod(namespace, name, false, false, false),
		},
		{
			desc: "pod has neg readiness gate",
			pod:  generatePod(namespace, name, true, false, false),
		},
		{
			desc: "pod has empty neg readiness condition",
			pod:  generatePod(namespace, name, true, true, false),
			expectCondition: v1.PodCondition{
				Type: shared.NegReadinessGate,
			},
			expectFound: true,
		},
		{
			desc: "pod has false neg readiness condition",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:   shared.NegReadinessGate,
							Status: v1.ConditionFalse,
							Reason: "foo",
						},
					},
				},
			},
			expectCondition: v1.PodCondition{
				Type:   shared.NegReadinessGate,
				Status: v1.ConditionFalse,
				Reason: "foo",
			},
			expectFound: true,
		},
		{
			desc: "pod has true neg readiness condition",
			pod:  generatePod(namespace, name, true, true, true),
			expectCondition: v1.PodCondition{
				Type:   shared.NegReadinessGate,
				Status: v1.ConditionTrue,
				Reason: negReadyReason,
			},
			expectFound: true,
		},
	} {
		condition, ok := NegReadinessConditionStatus(tc.pod)
		if tc.expectCondition != condition {
			t.Errorf("For test case %q, expect condition %v, but got %v", tc.desc, tc.expectCondition, condition)
		}

		if tc.expectFound != ok {
			t.Errorf("For test case %q, expect status %v, but got %v", tc.desc, tc.expectFound, ok)
		}
	}
}

func TestEvalNegReadinessGate(t *testing.T) {
	namespace := "ns"
	name := "name"
	for _, tc := range []struct {
		desc                   string
		pod                    *v1.Pod
		expectNegReady         bool
		expectNegReadinessGate bool
	}{
		{
			desc: "nil input",
			pod:  nil,
		},
		{
			desc: "pod does not have neg condition",
			pod:  generatePod(namespace, name, false, false, false),
		},
		{
			desc:                   "pod has neg readiness gate",
			pod:                    generatePod(namespace, name, true, false, false),
			expectNegReady:         false,
			expectNegReadinessGate: true,
		},
		{
			desc:                   "pod has empty neg readiness condition",
			pod:                    generatePod(namespace, name, false, true, false),
			expectNegReady:         false,
			expectNegReadinessGate: false,
		},
		{
			desc: "pod has false neg readiness condition",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:   shared.NegReadinessGate,
							Status: v1.ConditionFalse,
							Reason: "foo",
						},
					},
				},
			},
			expectNegReady:         false,
			expectNegReadinessGate: false,
		},
		{
			desc:                   "pod has true neg readiness condition",
			pod:                    generatePod(namespace, name, false, true, true),
			expectNegReady:         true,
			expectNegReadinessGate: false,
		},
		{
			desc: "pod has neg readiness gate and false readiness condition",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					ReadinessGates: []v1.PodReadinessGate{
						{
							ConditionType: shared.NegReadinessGate,
						},
					},
				},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:   shared.NegReadinessGate,
							Status: v1.ConditionFalse,
							Reason: "foo",
						},
					},
				},
			},
			expectNegReady:         false,
			expectNegReadinessGate: true,
		},
		{
			desc:                   "pod has neg readiness gate and no readiness condition value",
			pod:                    generatePod(namespace, name, true, true, false),
			expectNegReady:         false,
			expectNegReadinessGate: true,
		},
		{
			desc:                   "pod has neg readiness gate and true readiness condition",
			pod:                    generatePod(namespace, name, true, true, true),
			expectNegReady:         true,
			expectNegReadinessGate: true,
		},
	} {
		negReady, hasReadinessGate := evalNegReadinessGate(tc.pod)

		if tc.expectNegReady != negReady {
			t.Errorf("For test case %q, expect negReady to be %v, but got %v", tc.desc, tc.expectNegReady, negReady)
		}

		if tc.expectNegReadinessGate != hasReadinessGate {
			t.Errorf("For test case %q, expect hasReadinessGate to be %v, but got %v", tc.desc, tc.expectNegReadinessGate, hasReadinessGate)
		}
	}
}

func TestSetNegReadinessConditionStatus(t *testing.T) {
	pod := &v1.Pod{}
	for _, tc := range []struct {
		desc            string
		condition       v1.PodCondition
		conditionExists bool
	}{
		{
			desc: "empty input",
		},
		{
			desc: "set to false",
			condition: v1.PodCondition{
				Type:   shared.NegReadinessGate,
				Status: v1.ConditionFalse,
			},
			conditionExists: true,
		},
		{
			desc: "set to true",
			condition: v1.PodCondition{
				Type:   shared.NegReadinessGate,
				Status: v1.ConditionTrue,
				Reason: negReadyReason,
			},
			conditionExists: true,
		},
	} {
		SetNegReadinessConditionStatus(pod, tc.condition)
		if !tc.conditionExists {
			_, ok := NegReadinessConditionStatus(pod)
			if ok {
				t.Errorf("For case %q, expect ok to be false, but got %v", tc.desc, ok)
			}
		} else {
			condition, _ := NegReadinessConditionStatus(pod)
			if !reflect.DeepEqual(condition, tc.condition) {
				t.Errorf("For case %q, expect condition %v, but got %v", tc.desc, tc.condition, condition)
			}
		}

	}

}

func TestPatchPodStatus(t *testing.T) {
	ns := "ns"
	name := "name"
	client := &fake.Clientset{}
	client.CoreV1().Pods(ns).Create(context.TODO(), &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
	}, metav1.CreateOptions{})

	testCases := []struct {
		description        string
		mutate             func(input v1.PodStatus) v1.PodStatus
		expectedPatchBytes []byte
	}{
		{
			"no change",
			func(input v1.PodStatus) v1.PodStatus { return input },
			[]byte(fmt.Sprintf(`{}`)),
		},
		{
			"message change",
			func(input v1.PodStatus) v1.PodStatus {
				input.Message = "random message"
				return input
			},
			[]byte(fmt.Sprintf(`{"status":{"message":"random message"}}`)),
		},
		{
			"pod condition change",
			func(input v1.PodStatus) v1.PodStatus {
				input.Conditions[0].Status = v1.ConditionFalse
				return input
			},
			[]byte(fmt.Sprintf(`{"status":{"$setElementOrder/conditions":[{"type":"Ready"},{"type":"PodScheduled"}],"conditions":[{"status":"False","type":"Ready"}]}}`)),
		},
		{
			"additional init container condition",
			func(input v1.PodStatus) v1.PodStatus {
				input.InitContainerStatuses = []v1.ContainerStatus{
					{
						Name:  "init-container",
						Ready: true,
					},
				}
				return input
			},
			[]byte(fmt.Sprintf(`{"status":{"initContainerStatuses":[{"image":"","imageID":"","lastState":{},"name":"init-container","ready":true,"restartCount":0,"state":{}}]}}`)),
		},
	}
	for _, tc := range testCases {
		bytes, err := preparePatchBytesforPodStatus(getPodStatus(), tc.mutate(getPodStatus()))
		if err != nil {
			t.Errorf("For test case %q, unexpect error from preparePatchBytesforPodStatus: %v", tc.description, err)
		}
		if !reflect.DeepEqual(bytes, tc.expectedPatchBytes) {
			t.Errorf("For test case %q, expect bytes: %q, got: %q\n", tc.description, tc.expectedPatchBytes, bytes)
		}
		_, patchBytes, err := patchPodStatus(client, ns, name, bytes)
		if err != nil {
			t.Errorf("For test case %q, unexpect error from patchPodStatus: %v", tc.description, err)
		}
		if !reflect.DeepEqual(bytes, tc.expectedPatchBytes) {
			t.Errorf("For test case %q, expect patchBytes: %q, got: %q\n", tc.description, tc.expectedPatchBytes, patchBytes)
		}
	}
}

func TestNeedToProcess(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		pod    *v1.Pod
		expect bool
	}{
		{
			desc: "empty input",
		},
		{
			desc: "has neg readiness gate and no neg condition",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					ReadinessGates: []v1.PodReadinessGate{
						{
							ConditionType: shared.NegReadinessGate,
						},
					},
				},
			},
			expect: true,
		},
		{
			desc: "has neg readiness gate and empty neg condition status",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					ReadinessGates: []v1.PodReadinessGate{
						{
							ConditionType: shared.NegReadinessGate,
						},
					},
				},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type: shared.NegReadinessGate,
						},
					},
				},
			},
			expect: true,
		},
		{
			desc: "has neg readiness gate and false neg condition status",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					ReadinessGates: []v1.PodReadinessGate{
						{
							ConditionType: shared.NegReadinessGate,
						},
					},
				},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:   shared.NegReadinessGate,
							Status: v1.ConditionFalse,
						},
					},
				},
			},
			expect: true,
		},
		{
			desc: "has neg readiness gate and true neg condition status",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					ReadinessGates: []v1.PodReadinessGate{
						{
							ConditionType: shared.NegReadinessGate,
						},
					},
				},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:   shared.NegReadinessGate,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			expect: false,
		},
		{
			desc: "has no neg readiness gate and true neg condition status",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:   shared.NegReadinessGate,
							Status: v1.ConditionTrue,
						},
					},
				},
			},
			expect: false,
		},
		{
			desc: "has no neg readiness gate and false neg condition status",
			pod: &v1.Pod{
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:   shared.NegReadinessGate,
							Status: v1.ConditionFalse,
						},
					},
				},
			},
			expect: false,
		},
	} {
		if needToProcess(tc.pod) != tc.expect {
			t.Errorf("For test case %q, expect %v, got: %v", tc.desc, tc.expect, needToProcess(tc.pod))
		}
	}
}

func TestNeedToPoll(t *testing.T) {
	poller := newFakePoller()
	fakeLookUp := poller.lookup.(*fakeLookUp)
	podLister := poller.podLister
	namespace := "ns"
	name := "svc"
	port := int32(80)
	targetPort := "8080"
	key := negtypes.NegSyncerKey{
		Namespace: namespace,
		Name:      name,
		PortTuple: negtypes.SvcPortTuple{
			Port:       port,
			TargetPort: targetPort,
		},
	}

	for _, tc := range []struct {
		desc            string
		inputMap        negtypes.EndpointPodMap
		mutateState     func()
		expectOutputMap negtypes.EndpointPodMap
	}{
		{
			desc:            "empty input",
			inputMap:        negtypes.EndpointPodMap{},
			mutateState:     func() {},
			expectOutputMap: negtypes.EndpointPodMap{},
		},
		{
			desc:     "empty input map and readiness gate enabled",
			inputMap: negtypes.EndpointPodMap{},
			mutateState: func() {
				fakeLookUp.readinessGateEnabled = true
			},
			expectOutputMap: negtypes.EndpointPodMap{},
		},
		{
			desc:     "readiness gate disabled",
			inputMap: generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080"),
			mutateState: func() {
				fakeLookUp.readinessGateEnabled = false
				endpointMap := generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080")
				for _, v := range endpointMap {
					podLister.Add(generatePod(testNamespace, v.Name, true, false, false))
				}

			},
			expectOutputMap: negtypes.EndpointPodMap{},
		},
		{
			desc:     "all pods included",
			inputMap: generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080"),
			mutateState: func() {
				fakeLookUp.readinessGateEnabled = true
				endpointMap := generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080")
				for _, v := range endpointMap {
					podLister.Add(generatePod(testNamespace, v.Name, true, false, false))
				}

			},
			expectOutputMap: generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080"),
		},
		{
			desc:     "none of the pods needed due to neg condition is true",
			inputMap: generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080"),
			mutateState: func() {
				fakeLookUp.readinessGateEnabled = true
				endpointMap := generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080")
				for _, v := range endpointMap {
					podLister.Add(generatePod(testNamespace, v.Name, true, true, true))
				}

			},
			expectOutputMap: negtypes.EndpointPodMap{},
		},
		{
			desc:     "none of the pods needed due to neg readiness gate does not exists",
			inputMap: generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080"),
			mutateState: func() {
				fakeLookUp.readinessGateEnabled = true
				endpointMap := generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080")
				for _, v := range endpointMap {
					podLister.Add(generatePod(testNamespace, v.Name, false, true, false))
				}

			},
			expectOutputMap: negtypes.EndpointPodMap{},
		},
		{
			desc:     "none of the pods needed due to pods does not exists",
			inputMap: generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080"),
			mutateState: func() {
				fakeLookUp.readinessGateEnabled = true
				endpointMap := generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080")
				for _, v := range endpointMap {
					podLister.Delete(generatePod(testNamespace, v.Name, false, true, false))
				}
			},
			expectOutputMap: negtypes.EndpointPodMap{},
		},
		{
			desc: "half of pods included ",
			inputMap: unionEndpointMap(
				generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080"),
				generateEndpointMap(net.ParseIP("1.1.1.2"), 10, instance2, "8080")),
			mutateState: func() {
				fakeLookUp.readinessGateEnabled = true

				endpointMap := generateEndpointMap(net.ParseIP("1.1.1.1"), 10, instance1, "8080")
				for _, v := range endpointMap {
					podLister.Add(generatePod(testNamespace, v.Name, true, true, true))
				}

				endpointMap = generateEndpointMap(net.ParseIP("1.1.1.2"), 10, instance2, "8080")
				for _, v := range endpointMap {
					podLister.Add(generatePod(testNamespace, v.Name, true, false, false))
				}

			},
			expectOutputMap: generateEndpointMap(net.ParseIP("1.1.1.2"), 10, instance2, "8080"),
		},
	} {
		tc.mutateState()
		ret := needToPoll(key, tc.inputMap, fakeLookUp, podLister)
		if !reflect.DeepEqual(ret, tc.expectOutputMap) {
			t.Errorf("For test case %q, expect %v, got: %v", tc.desc, tc.expectOutputMap, ret)
		}
	}
}

func generatePod(namespace, name string, hasNegReadinessGate, hasNegCondition, negConditionIsTrue bool) *v1.Pod {
	ret := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
	if hasNegReadinessGate {
		ret.Spec.ReadinessGates = []v1.PodReadinessGate{{ConditionType: shared.NegReadinessGate}}
	}

	if hasNegCondition {
		SetNegReadinessConditionStatus(ret, v1.PodCondition{
			Type: shared.NegReadinessGate,
		})
	}

	if negConditionIsTrue {
		SetNegReadinessConditionStatus(ret, v1.PodCondition{
			Type:   shared.NegReadinessGate,
			Status: v1.ConditionTrue,
			Reason: negReadyReason,
		})
	}
	return ret
}

func getPodStatus() v1.PodStatus {
	return v1.PodStatus{
		Phase: v1.PodRunning,
		Conditions: []v1.PodCondition{
			{
				Type:   v1.PodReady,
				Status: v1.ConditionTrue,
			},
			{
				Type:   v1.PodScheduled,
				Status: v1.ConditionTrue,
			},
		},
		ContainerStatuses: []v1.ContainerStatus{
			{
				Name:  "container1",
				Ready: true,
			},
			{
				Name:  "container2",
				Ready: true,
			},
		},
		Message: "Message",
	}
}

func generateEndpointMap(initialIp net.IP, num int, instance string, targetPort string) negtypes.EndpointPodMap {
	retMap := negtypes.EndpointPodMap{}
	ip := initialIp.To4()
	for i := 1; i <= num; i++ {
		if i%256 == 0 {
			ip[2]++
		}
		ip[3]++

		endpoint := negtypes.NetworkEndpoint{IP: ip.String(), Node: instance, Port: targetPort}
		retMap[endpoint] = types.NamespacedName{Namespace: testNamespace, Name: fmt.Sprintf("pod-%s-%d", instance, i)}
	}
	return retMap
}

func unionEndpointMap(m1, m2 negtypes.EndpointPodMap) negtypes.EndpointPodMap {
	for k, v := range m2 {
		m1[k] = v
	}
	return m1
}
