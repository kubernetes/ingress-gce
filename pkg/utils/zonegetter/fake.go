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

const (
	defaultTestSubnet    = "default"
	nonDefaultTestSubnet = "non-default"

	defaultTestSubnetURL = "https://www.googleapis.com/compute/v1/projects/proj/regions/us-central1/subnetworks/default"
)

func FakeNodeInformer() cache.SharedIndexInformer {
	return informerv1.NewNodeInformer(fake.NewSimpleClientset(), 1*time.Second, utils.NewNamespaceIndexer())
}

// DeleteFakeNodesInZone deletes all nodes in a zone.
func DeleteFakeNodesInZone(t *testing.T, zone string, zoneGetter *ZoneGetter) {
	nodes, err := listers.NewNodeLister(zoneGetter.nodeLister).List(labels.Everything())
	if err != nil {
		t.Errorf("Failed listing nodes in zone %q, err - %v", zone, err)
	}
	for _, node := range nodes {
		nodeZone, _ := getZone(node)
		if nodeZone == zone {
			if err := zoneGetter.nodeLister.Delete(node); err != nil {
				t.Errorf("Failed to delete node %q in zone %q, err - %v", node.Name, zone, err)
			}
		}
	}
}

// AddFakeNodes adds fake nodes to the ZoneGetter in the provided zone.
func AddFakeNodes(zoneGetter *ZoneGetter, newZone string, instances ...string) error {
	for i, instance := range instances {
		if err := zoneGetter.nodeLister.Add(&apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: instance,
				Labels: map[string]string{
					utils.LabelNodeSubnet: defaultTestSubnet,
				},
			},
			Spec: apiv1.NodeSpec{
				ProviderID: fmt.Sprintf("gce://foo-project/%s/instance1", newZone),
				PodCIDR:    fmt.Sprintf("10.100.%d.0/24", i),
				PodCIDRs:   []string{fmt.Sprintf("10.100.%d.0/24", i)},
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
	if err := zoneGetter.nodeLister.Add(node); err != nil {
		return err
	}
	return nil
}

// PopulateFakeNodeInformer populates a fake node informer with fake nodes.
func PopulateFakeNodeInformer(nodeInformer cache.SharedIndexInformer, addMSCNodes bool) {
	// Ready nodes.
	if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "instance1",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/zone1/instance1",
			PodCIDR:    "10.100.1.0/24",
			PodCIDRs:   []string{"a:b::/48", "10.100.1.0/24"},
		},
		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				{
					Type:    apiv1.NodeInternalIP,
					Address: fmt.Sprintf("1.2.3.1"),
				},
			},
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
			PodCIDR:    "10.100.2.0/24",
			PodCIDRs:   []string{"a:b::/48", "10.100.2.0/24"},
		},
		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				{
					Type:    apiv1.NodeInternalIP,
					Address: fmt.Sprintf("1.2.3.2"),
				},
			},
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
			PodCIDR:    "10.100.3.0/24",
			PodCIDRs:   []string{"a:b::/48", "10.100.3.0/24"},
		},
		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				{
					Type:    apiv1.NodeInternalIP,
					Address: fmt.Sprintf("1.2.3.3"),
				},
			},
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
			PodCIDR:    "10.100.4.0/24",
			PodCIDRs:   []string{"a:b::/48", "10.100.4.0/24"},
		},
		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				{
					Type:    apiv1.NodeInternalIP,
					Address: fmt.Sprintf("1.2.3.4"),
				},
			},
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
			PodCIDR:    "10.100.5.0/24",
			PodCIDRs:   []string{"a:b::/48", "10.100.5.0/24"},
		},
		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				{
					Type:    apiv1.NodeInternalIP,
					Address: fmt.Sprintf("1.2.3.5"),
				},
			},
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
			PodCIDR:    "10.100.6.0/24",
			PodCIDRs:   []string{"a:b::/48", "10.100.6.0/24"},
		},
		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				{
					Type:    apiv1.NodeInternalIP,
					Address: fmt.Sprintf("1.2.3.6"),
				},
			},
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
			PodCIDR:    "10.100.7.0/24",
			PodCIDRs:   []string{"a:b::/48", "10.100.7.0/24"},
		},
		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				{
					Type:    apiv1.NodeInternalIP,
					Address: fmt.Sprintf("1.2.3.7"),
				},
			},
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
			PodCIDR:    "10.100.8.0/24",
			PodCIDRs:   []string{"a:b::/48", "10.100.8.0/24"},
		},
		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				{
					Type:    apiv1.NodeInternalIP,
					Address: fmt.Sprintf("1.2.3.8"),
				},
			},
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
			PodCIDR:    "10.100.9.0/24",
			PodCIDRs:   []string{"a:b::/48", "10.100.9.0/24"},
		},
		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				{
					Type:    apiv1.NodeInternalIP,
					Address: fmt.Sprintf("1.2.3.9"),
				},
			},
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
			PodCIDR:    "10.100.10.0/24",
			PodCIDRs:   []string{"a:b::/48", "10.100.10.0/24"},
		},
		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				{
					Type:    apiv1.NodeInternalIP,
					Address: fmt.Sprintf("1.2.3.10"),
				},
			},
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
			Labels: map[string]string{
				"operation.gke.io/type": "drain",
			},
		},
		Spec: apiv1.NodeSpec{
			PodCIDR:  "10.100.11.0/24",
			PodCIDRs: []string{"a:b::/48", "10.100.11.0/24"},
		},
		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				{
					Type:    apiv1.NodeInternalIP,
					Address: fmt.Sprintf("1.2.3.11"),
				},
			},
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
			Labels: map[string]string{
				"operation.gke.io/type": "drain",
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project/instance-invalid-providerID",
			PodCIDR:    "10.100.12.0/24",
			PodCIDRs:   []string{"a:b::/48", "10.100.12.0/24"},
		},
		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				{
					Type:    apiv1.NodeInternalIP,
					Address: fmt.Sprintf("1.2.3.12"),
				},
			},
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}); err != nil {
		fmt.Printf("Failed to add node instance-invalid-providerID: %v\n", err)
	}

	// Node with empty zone in providerID.
	// This should not affect any tests since the zoneGetter will ignore this.
	if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "instance-empty-zone-providerID",
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "gce://foo-project//instance-empty-zone-providerID",
			PodCIDR:    "10.100.13.0/24",
			PodCIDRs:   []string{"a:b::/48", "10.100.13.0/24"},
		},
		Status: apiv1.NodeStatus{
			Addresses: []apiv1.NodeAddress{
				{
					Type:    apiv1.NodeInternalIP,
					Address: fmt.Sprintf("1.2.3.12"),
				},
			},
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}); err != nil {
		fmt.Printf("Failed to add node instance-empty-zone-providerID: %v\n", err)
	}

	if addMSCNodes {
		// Add a node does not have subnet label.
		if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default-subnet-label-instance",
				Labels: map[string]string{
					utils.LabelNodeSubnet: defaultTestSubnet,
				},
			},
			Spec: apiv1.NodeSpec{
				ProviderID: "gce://foo-project/zone5/default-subnet-label-instance",
				PodCIDR:    "10.101.1.0/24",
				PodCIDRs:   []string{"a:b::/48", "10.101.1.0/24"},
			},
			Status: apiv1.NodeStatus{
				Addresses: []apiv1.NodeAddress{
					{
						Type:    apiv1.NodeInternalIP,
						Address: fmt.Sprintf("1.3.3.1"),
					},
				},
				Conditions: []apiv1.NodeCondition{
					{
						Type:   apiv1.NodeReady,
						Status: apiv1.ConditionTrue,
					},
				},
			},
		}); err != nil {
			fmt.Printf("Failed to add node default-subnet-label-instance: %v\n", err)
		}
		// Add a node with empty subnet label.
		if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "empty-subnet-label-instance",
				Labels: map[string]string{
					utils.LabelNodeSubnet: "",
				},
			},
			Spec: apiv1.NodeSpec{
				ProviderID: "gce://foo-project/zone6/empty-subnet-label-instance",
				PodCIDR:    "10.101.2.0/24",
				PodCIDRs:   []string{"a:b::/48", "10.101.2.0/24"},
			},
			Status: apiv1.NodeStatus{
				Addresses: []apiv1.NodeAddress{
					{
						Type:    apiv1.NodeInternalIP,
						Address: fmt.Sprintf("1.3.3.2"),
					},
				},
				Conditions: []apiv1.NodeCondition{
					{
						Type:   apiv1.NodeReady,
						Status: apiv1.ConditionTrue,
					},
				},
			},
		}); err != nil {
			fmt.Printf("Failed to add node empty-subnet-label-instance: %v\n", err)
		}
		// Add a node with PodCIDR not set.
		if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "no-podcidr-instance",
			},
			Spec: apiv1.NodeSpec{
				ProviderID: "gce://foo-project/zone7/no-podcidr-instance",
			},
			Status: apiv1.NodeStatus{
				Addresses: []apiv1.NodeAddress{
					{
						Type:    apiv1.NodeInternalIP,
						Address: fmt.Sprintf("1.3.3.3"),
					},
				},
				Conditions: []apiv1.NodeCondition{
					{
						Type:   apiv1.NodeReady,
						Status: apiv1.ConditionTrue,
					},
				},
			},
		}); err != nil {
			fmt.Printf("Failed to add node no-podcidr-instance: %v\n", err)
		}
		// Add a node that is not in the default subnet and a pod corresponds to this node.
		if err := nodeInformer.GetIndexer().Add(&apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "non-default-subnet-instance",
				Labels: map[string]string{
					utils.LabelNodeSubnet: nonDefaultTestSubnet,
				},
			},
			Spec: apiv1.NodeSpec{
				ProviderID: "gce://foo-project/zone8/non-default-subnet-instance",
				PodCIDR:    "10.200.1.0/24",
				PodCIDRs:   []string{"a:b::/48", "10.200.1.0/24"},
			},
			Status: apiv1.NodeStatus{
				Addresses: []apiv1.NodeAddress{
					{
						Type:    apiv1.NodeInternalIP,
						Address: fmt.Sprintf("2.2.3.1"),
					},
				},
				Conditions: []apiv1.NodeCondition{
					{
						Type:   apiv1.NodeReady,
						Status: apiv1.ConditionTrue,
					},
				},
			},
		}); err != nil {
			fmt.Printf("Failed to add node non-default-subnet-instance: %v\n", err)
		}
	}
}
