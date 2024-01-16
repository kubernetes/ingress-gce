/*
Copyright 2023 The Kubernetes Authors.

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

package zonegetter

import (
	"fmt"
	"testing"
	"time"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	informerv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/utils"
)

func FakeNodeInformer() cache.SharedIndexInformer {
	return informerv1.NewNodeInformer(fake.NewSimpleClientset(), 1*time.Second, utils.NewNamespaceIndexer())
}

// DeleteFakeNodesInZone deletes all nodes in a zone.
func DeleteFakeNodesInZone(t *testing.T, zone string, zoneGetter *ZoneGetter) {
	nodeIndexer := zoneGetter.nodeInformer.GetIndexer()
	nodes, err := listers.NewNodeLister(nodeIndexer).List(labels.Everything())
	if err != nil {
		t.Errorf("Failed listing nodes in zone %q, err - %v", zone, err)
	}
	for _, node := range nodes {
		nodeZone, _ := getZone(node)
		if nodeZone == zone {
			if err := nodeIndexer.Delete(node); err != nil {
				t.Errorf("Failed to delete node %q in zone %q, err - %v", node.Name, zone, err)
			}
		}
	}
}

// AddFakeNodes adds fake nodes to the ZoneGetter in the provided zone.
func AddFakeNodes(zoneGetter *ZoneGetter, newZone string, instances ...string) error {
	for _, instance := range instances {
		if err := zoneGetter.nodeInformer.GetIndexer().Add(&apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: instance,
			},
			Spec: apiv1.NodeSpec{
				ProviderID: fmt.Sprintf("gce://foo-project/%s/instance1", newZone),
			},
			Status: apiv1.NodeStatus{
				Conditions: []apiv1.NodeCondition{
					{
						Type:   apiv1.NodeReady,
						Status: apiv1.ConditionTrue,
					},
				},
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

// AddFakeNode adds fake node to the ZoneGetter.
func AddFakeNode(zoneGetter *ZoneGetter, node *apiv1.Node) error {
	if err := zoneGetter.nodeInformer.GetIndexer().Add(node); err != nil {
		return err
	}
	return nil
}

// PopulateFakeNodeInformer populates a fake node informer with fake nodes.
func PopulateFakeNodeInformer(nodeInformer cache.SharedIndexInformer) {
	// Ready nodes.
	if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "instance1",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/zone1/instance1",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}); err != nil {
		fmt.Printf("Failed to add node instance1: %v\n", err)
	}

	if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "instance2",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/zone1/instance2",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}); err != nil {
		fmt.Printf("Failed to add node instance2: %v\n", err)
	}

	if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "instance3",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/zone2/instance3",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}); err != nil {
		fmt.Printf("Failed to add node instance3: %v\n", err)
	}

	if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "instance4",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/zone2/instance4",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}); err != nil {
		fmt.Printf("Failed to add node instance4: %v\n", err)
	}

	if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "instance5",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/zone2/instance5",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}); err != nil {
		fmt.Printf("Failed to add node instance5: %v\n", err)
	}

	if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "instance6",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/zone2/instance6",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}); err != nil {
		fmt.Printf("Failed to add node instance6")
	}

	// Unready nodes.
	if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "unready-instance1",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/zone3/unready-instance1",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionFalse,
				},
			},
		},
	}); err != nil {
		fmt.Printf("Failed to add node unready-instance1: %v\n", err)
	}

	if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "unready-instance2",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/zone3/unready-instance2",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionFalse,
				},
			},
		},
	}); err != nil {
		fmt.Printf("Failed to add node unready-instance2: %v\n", err)
	}

	// Upgrade nodes.
	if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "upgrade-instance1",
			Labels: map[string]string{
				"operation.gke.io/type": "drain",
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/zone4/upgrade-instance1",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}); err != nil {
		fmt.Printf("Failed to add node upgrade-instance1: %v\n", err)
	}

	if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "upgrade-instance2",
			Labels: map[string]string{
				"operation.gke.io/type": "drain",
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/zone4/upgrade-instance2",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}); err != nil {
		fmt.Printf("Failed to add node upgrade-instance2: %v\n", err)
	}

	// Node with no providerID.
	// This should not affect any tests since the zoneGetter will ignore this.
	if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "instance-empty-providerID",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}); err != nil {
		fmt.Printf("Failed to add node instance-empty-providerID: %v\n", err)
	}

	// Node with invalid providerID.
	// This should not affect any tests since the zoneGetter will ignore this.
	if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "instance-invalid-providerID",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/instance-invalid-providerID",
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}); err != nil {
		fmt.Printf("Failed to add node instance-empty-providerID: %v\n", err)
	}
}
