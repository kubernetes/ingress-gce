package instancegroups

import (
	"testing"
	"time"

	api_v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	informerv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
	"k8s.io/utils/strings/slices"
)

func TestGetReadyNodesForSharedInstanceGroup(t *testing.T) {
	client := fake.NewSimpleClientset()
	resyncPeriod := 1 * time.Second
	informer := informerv1.NewNodeInformer(client, resyncPeriod, utils.NewNamespaceIndexer())
	nodeIndexer := informer.GetIndexer()
	subnetURL := "https://www.googleapis.com/compute/v1/projects/proj/regions/us-central1/subnetworks/defaultSubnet"
	anotherSubnetURL := "https://www.googleapis.com/compute/v1/projects/proj/regions/us-central1/subnetworks/anotherSubnet"
	nodeWithLabelForDefaultSubnetwork := api_v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node1",
			Labels: map[string]string{
				utils.LabelGKESubnetworkName: subnetURL,
			},
		},
		Status: api_v1.NodeStatus{
			Conditions: []api_v1.NodeCondition{
				{Type: api_v1.NodeReady, Status: api_v1.ConditionTrue},
			},
		},
	}
	nodeWithLabelForAnotherSubnetwork := api_v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node2",
			Labels: map[string]string{
				utils.LabelGKESubnetworkName: anotherSubnetURL,
			},
		},
		Status: api_v1.NodeStatus{
			Conditions: []api_v1.NodeCondition{
				{Type: api_v1.NodeReady, Status: api_v1.ConditionTrue},
			},
		},
	}
	nodeIndexer.Add(&nodeWithLabelForDefaultSubnetwork)
	nodeIndexer.Add(&nodeWithLabelForAnotherSubnetwork)

	nodeGetterWithoutMultiSubnet := NewNodeGetter(nodeIndexer, false, &fakeSubnetworkProvider{subnetworkURL: subnetURL})
	nodeGetterWithMultiSubnet := NewNodeGetter(nodeIndexer, true, &fakeSubnetworkProvider{subnetworkURL: subnetURL})

	nodes, err := nodeGetterWithoutMultiSubnet.GetReadyNodesForSharedInstanceGroup(klog.TODO())
	if err != nil {
		t.Errorf("failed to get nodes %v", err)
	}
	if !slices.Contains(nodes, nodeWithLabelForDefaultSubnetwork.Name) || !slices.Contains(nodes, nodeWithLabelForAnotherSubnetwork.Name) {
		t.Errorf("exopcted the getter without multi-subnet to return all nodes but it returned %+v", nodes)
	}
	nodes, err = nodeGetterWithMultiSubnet.GetReadyNodesForSharedInstanceGroup(klog.TODO())
	if err != nil {
		t.Errorf("failed to get nodes %v", err)
	}
	if !slices.Contains(nodes, nodeWithLabelForDefaultSubnetwork.Name) || slices.Contains(nodes, nodeWithLabelForAnotherSubnetwork.Name) {
		t.Errorf("exopcted the getter with multi-subnet to return only the first node but it returned %+v", nodes)
	}
}

type fakeSubnetworkProvider struct {
	subnetworkURL string
}

func (p *fakeSubnetworkProvider) SubnetworkURL() string {
	return p.subnetworkURL
}
