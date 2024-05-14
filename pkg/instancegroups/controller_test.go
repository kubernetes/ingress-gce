package instancegroups

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	compute "google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	informerv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
)

func TestNodeStatusChanged(t *testing.T) {
	testCases := []struct {
		desc   string
		mutate func(node *api_v1.Node)
		expect bool
	}{
		{
			"no change",
			func(node *api_v1.Node) {},
			false,
		},
		{
			"unSchedulable changes",
			func(node *api_v1.Node) {
				node.Spec.Unschedulable = true
			},
			true,
		},
		{
			"readiness changes",
			func(node *api_v1.Node) {
				node.Status.Conditions[0].Status = api_v1.ConditionFalse
				node.Status.Conditions[0].LastTransitionTime = meta_v1.NewTime(time.Now())
			},
			true,
		},
		{
			"new heartbeat",
			func(node *api_v1.Node) {
				node.Status.Conditions[0].LastHeartbeatTime = meta_v1.NewTime(time.Now())
			},
			false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			node := testNode()
			tc.mutate(node)
			res := nodeStatusChanged(testNode(), node)
			if res != tc.expect {
				t.Fatalf("Test case %q got: %v, expected: %v", tc.desc, res, tc.expect)
			}
		})
	}
}

func testNode() *api_v1.Node {
	return &api_v1.Node{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: "node",
			Annotations: map[string]string{
				"key1": "value1",
			},
		},
		Spec: api_v1.NodeSpec{
			Unschedulable: false,
		},
		Status: api_v1.NodeStatus{
			Conditions: []api_v1.NodeCondition{
				{
					Type:               api_v1.NodeReady,
					Status:             api_v1.ConditionTrue,
					LastHeartbeatTime:  meta_v1.NewTime(time.Date(2000, 01, 1, 1, 0, 0, 0, time.UTC)),
					LastTransitionTime: meta_v1.NewTime(time.Date(2000, 01, 1, 1, 0, 0, 0, time.UTC)),
				},
			},
		},
	}
}

func TestSync(t *testing.T) {
	config := &ControllerConfig{}
	resyncPeriod := 1 * time.Second
	fakeKubeClient := fake.NewSimpleClientset()
	informer := informerv1.NewNodeInformer(fakeKubeClient, resyncPeriod, utils.NewNamespaceIndexer())
	config.NodeInformer = informer
	fakeManager := &IGManagerFake{}
	config.IGManager = fakeManager
	config.HasSynced = func() bool {
		return true
	}
	config.ZoneGetter = zonegetter.NewFakeZoneGetter(informer)

	controller := NewController(config, logr.Logger{})

	channel := make(chan struct{})
	go informer.Run(channel)
	go controller.Run()

	var expectedSyncedNodesCounter = 0
	firstNode := testNode()
	secondNode := testNode()
	secondNode.Name = "secondNode"

	// Add two nodes
	fakeKubeClient.CoreV1().Nodes().Create(context.TODO(), firstNode, meta_v1.CreateOptions{})
	// wait time > resync period
	time.Sleep(2 * time.Second)
	fakeKubeClient.CoreV1().Nodes().Create(context.TODO(), secondNode, meta_v1.CreateOptions{})
	// The counter = 1 because it synced only once (for the first Create() call)
	expectedSyncedNodesCounter += 1
	verifyExpectedSyncerCount(t, fakeManager.syncedNodes, expectedSyncedNodesCounter)

	// Update both nodes
	firstNode.Annotations["key"] = "true"
	firstNode.Spec.Unschedulable = false
	secondNode.Annotations["key"] = "true"
	fakeKubeClient.CoreV1().Nodes().Update(context.TODO(), firstNode, meta_v1.UpdateOptions{})
	fakeKubeClient.CoreV1().Nodes().Update(context.TODO(), secondNode, meta_v1.UpdateOptions{})
	time.Sleep(2 * time.Second)
	// nodes were updated
	expectedSyncedNodesCounter += 1
	verifyExpectedSyncerCount(t, fakeManager.syncedNodes, expectedSyncedNodesCounter)

	// no real update
	fakeKubeClient.CoreV1().Nodes().Update(context.TODO(), firstNode, meta_v1.UpdateOptions{})
	// Nothing should change
	time.Sleep(2 * time.Second)
	verifyExpectedSyncerCount(t, fakeManager.syncedNodes, expectedSyncedNodesCounter)
}

func verifyExpectedSyncerCount(t *testing.T, syncedNodes [][]string, expectedCount int) {
	if len(syncedNodes) != expectedCount {
		t.Errorf("verifyExpectedSyncerCount(): synced unexpected amount of times (gotCount, expectedCount), (%d, %d)", len(syncedNodes), expectedCount)
	}
}

type IGManagerFake struct {
	syncedNodes [][]string
}

func (igmf *IGManagerFake) Sync(nodeNames []string) error {
	igmf.syncedNodes = append(igmf.syncedNodes, nodeNames)
	return nil
}

func (igmf *IGManagerFake) EnsureInstanceGroupsAndPorts(name string, ports []int64) ([]*compute.InstanceGroup, error) {
	igmf.syncedNodes = append(igmf.syncedNodes, []string{name})
	return []*compute.InstanceGroup{}, nil
}

func (igmf *IGManagerFake) DeleteInstanceGroup(name string) error {
	igmf.syncedNodes = append(igmf.syncedNodes, []string{name})
	return nil
}

func (igmf *IGManagerFake) Get(name, zone string) (*compute.InstanceGroup, error) {
	ig := compute.InstanceGroup{Name: name, Zone: zone}
	return &ig, nil
}

func (igmf *IGManagerFake) List() ([]string, error) {
	return []string{}, nil
}
