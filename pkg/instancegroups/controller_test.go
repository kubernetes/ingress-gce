package instancegroups

import (
	"context"
	"sync"
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
	"k8s.io/klog/v2"
)

func TestNodeStatusChanged(t *testing.T) {
	testCases := []struct {
		desc        string
		initialNode *api_v1.Node
		mutate      func(node *api_v1.Node)
		expect      bool
	}{
		{
			desc:   "no change",
			mutate: func(node *api_v1.Node) {},
			expect: false,
		},
		{
			desc: "become_unSchedulable",
			mutate: func(node *api_v1.Node) {
				node.Spec.Unschedulable = true
			},
			expect: true,
		},
		{
			desc:        "become_schedulable",
			initialNode: testNode(withUnschedulable(true)),
			mutate:      withUnschedulable(false),
			expect:      true,
		},
		{
			desc: "readiness changes",
			mutate: func(node *api_v1.Node) {
				node.Status.Conditions[0].Status = api_v1.ConditionFalse
				node.Status.Conditions[0].LastTransitionTime = meta_v1.NewTime(time.Now())
			},
			expect: true,
		},
		{
			desc: "new heartbeat",
			mutate: func(node *api_v1.Node) {
				node.Status.Conditions[0].LastHeartbeatTime = meta_v1.NewTime(time.Now())
			},
			expect: false,
		},
		{
			desc: "PodCIDR_added",
			mutate: func(node *api_v1.Node) {
				node.Spec.PodCIDR = "10.0.0.0/16"
			},
			expect: true,
		},
		{
			desc:        "PodCIDR_changed",
			initialNode: testNode(withPodCIDR("192.168.0.0/24")),
			mutate:      withPodCIDR("10.0.0.0/24"),
			expect:      true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			initialNode := tc.initialNode
			if initialNode == nil {
				initialNode = testNode()
			}
			currentNode := initialNode.DeepCopy()
			tc.mutate(currentNode)
			res := nodeStatusChanged(initialNode, currentNode)
			if res != tc.expect {
				t.Fatalf("Test case %q got: %v, expected: %v", tc.desc, res, tc.expect)
			}
		})
	}
}

func testNode(modifiers ...func(node *api_v1.Node)) *api_v1.Node {
	node := &api_v1.Node{
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

	for _, modifier := range modifiers {
		modifier(node)
	}

	return node
}

func withPodCIDR(podCIDR string) func(node *api_v1.Node) {
	return func(node *api_v1.Node) {
		node.Spec.PodCIDR = podCIDR
	}
}

func withUnschedulable(value bool) func(node *api_v1.Node) {
	return func(node *api_v1.Node) {
		node.Spec.Unschedulable = value
	}
}

func TestSync(t *testing.T) {
	t.Parallel()

	config := &ControllerConfig{}
	resyncPeriod := 1 * time.Second
	fakeKubeClient := fake.NewSimpleClientset()
	informer := informerv1.NewNodeInformer(fakeKubeClient, resyncPeriod, utils.NewNamespaceIndexer())
	config.NodeInformer = informer
	fakeManager := &IGManagerFake{SyncLock: &sync.Mutex{}}
	config.IGManager = fakeManager
	config.HasSynced = func() bool {
		return true
	}
	var err error
	config.ZoneGetter, err = zonegetter.NewFakeZoneGetter(informer, zonegetter.FakeNodeTopologyInformer(), defaultTestSubnetURL, false)
	if err != nil {
		t.Fatalf("failed to initialize zone getter: %v", err)
	}

	controller := NewController(config, logr.Logger{})

	channel := make(chan struct{})
	go informer.Run(channel)
	go controller.Run()

	var expectedSyncedNodesCounter = 0
	firstNode := testNode()
	secondNode := testNode()
	thirdNode := testNode()
	secondNode.Name = "secondNode"
	thirdNode.Name = "thirdNode"

	// Freeze resyncs to ensure that second node sync won't start before third node triggers it as well
	fakeManager.SyncLock.Lock()
	fakeKubeClient.CoreV1().Nodes().Create(context.TODO(), firstNode, meta_v1.CreateOptions{})
	time.Sleep(time.Second) // ensure sync started

	// Add two nodes
	fakeKubeClient.CoreV1().Nodes().Create(context.TODO(), secondNode, meta_v1.CreateOptions{})
	fakeKubeClient.CoreV1().Nodes().Create(context.TODO(), thirdNode, meta_v1.CreateOptions{})
	time.Sleep(time.Second) // ensure third node will be able to trigger sync
	fakeManager.SyncLock.Unlock()
	time.Sleep(2 * time.Second) // wait until both syncs complete
	// Second and third nodes should trigger only a single sync (+1 for the first)
	expectedSyncedNodesCounter += 2
	verifyExpectedSyncerCount(t, fakeManager.syncedNodes, expectedSyncedNodesCounter)

	// Freeze resyncs to ensure that second node sync won't start before third node triggers it as well
	fakeManager.SyncLock.Lock()
	firstNode.Spec.Unschedulable = true
	fakeKubeClient.CoreV1().Nodes().Update(context.TODO(), firstNode, meta_v1.UpdateOptions{})
	time.Sleep(time.Second) // ensure sync started

	// Update two nodes
	secondNode.Spec.Unschedulable = true
	thirdNode.Spec.Unschedulable = true
	fakeKubeClient.CoreV1().Nodes().Update(context.TODO(), secondNode, meta_v1.UpdateOptions{})
	fakeKubeClient.CoreV1().Nodes().Update(context.TODO(), thirdNode, meta_v1.UpdateOptions{})
	time.Sleep(time.Second) // ensure third node will be able to trigger sync
	fakeManager.SyncLock.Unlock()
	time.Sleep(2 * time.Second) // wait until both syncs complete
	// Second and third nodes should trigger only a single sync (+1 for the first)
	expectedSyncedNodesCounter += 2
	verifyExpectedSyncerCount(t, fakeManager.syncedNodes, expectedSyncedNodesCounter)

	// no real update
	fakeKubeClient.CoreV1().Nodes().Update(context.TODO(), firstNode, meta_v1.UpdateOptions{})
	// Nothing should change
	time.Sleep(2 * time.Second)
	verifyExpectedSyncerCount(t, fakeManager.syncedNodes, expectedSyncedNodesCounter)

	// Freeze resyncs to ensure that second node sync won't start before third node triggers it as well
	fakeManager.SyncLock.Lock()
	fakeKubeClient.CoreV1().Nodes().Delete(context.TODO(), firstNode.Name, meta_v1.DeleteOptions{})
	time.Sleep(time.Second) // ensure sync started

	// Delete two nodes
	fakeKubeClient.CoreV1().Nodes().Delete(context.TODO(), secondNode.Name, meta_v1.DeleteOptions{})
	fakeKubeClient.CoreV1().Nodes().Delete(context.TODO(), thirdNode.Name, meta_v1.DeleteOptions{})
	time.Sleep(time.Second) // ensure third node will be able to trigger sync
	fakeManager.SyncLock.Unlock()
	time.Sleep(2 * time.Second) // wait until both syncs complete
	// Second and third nodes should trigger only a single sync (+1 for the first)
	expectedSyncedNodesCounter += 2
	verifyExpectedSyncerCount(t, fakeManager.syncedNodes, expectedSyncedNodesCounter)
}

func verifyExpectedSyncerCount(t *testing.T, syncedNodes [][]string, expectedCount int) {
	if len(syncedNodes) != expectedCount {
		t.Errorf("verifyExpectedSyncerCount(): synced unexpected amount of times (gotCount, expectedCount), (%d, %d)", len(syncedNodes), expectedCount)
	}
}

type IGManagerFake struct {
	syncedNodes [][]string
	SyncLock    sync.Locker
}

func (igmf *IGManagerFake) Sync(nodeNames []string, logger klog.Logger) error {
	igmf.SyncLock.Lock()
	defer igmf.SyncLock.Unlock()
	igmf.syncedNodes = append(igmf.syncedNodes, nodeNames)
	return nil
}

func (igmf *IGManagerFake) InstanceGroupsExist(name string, logger klog.Logger) (exist bool, err error) {
	return len(igmf.syncedNodes) > 0, nil
}

func (igmf *IGManagerFake) EnsureInstanceGroupsAndPorts(name string, ports []int64, logger klog.Logger) ([]*compute.InstanceGroup, error) {
	igmf.syncedNodes = append(igmf.syncedNodes, []string{name})
	return []*compute.InstanceGroup{}, nil
}

func (igmf *IGManagerFake) DeleteInstanceGroup(name string, logger klog.Logger) error {
	igmf.syncedNodes = append(igmf.syncedNodes, []string{name})
	return nil
}

func (igmf *IGManagerFake) Get(name, zone string) (*compute.InstanceGroup, error) {
	ig := compute.InstanceGroup{Name: name, Zone: zone}
	return &ig, nil
}

func (igmf *IGManagerFake) List(logger klog.Logger) ([]string, error) {
	return []string{}, nil
}
