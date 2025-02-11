package controller

import (
	"context"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	providerconfigv1 "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/multiproject/manager"
	fakeproviderconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned/fake"
	providerconfiginformers "k8s.io/ingress-gce/pkg/providerconfig/client/informers/externalversions"
	"k8s.io/klog/v2"
)

func init() {
	// Register the ProviderConfig types with the scheme
	providerconfigv1.AddToScheme(scheme.Scheme)
}

// fakeProviderConfigControllersManager implements manager.ProviderConfigControllersManager
// and lets us track calls to StartControllersForProviderConfig/StopControllersForProviderConfig.
type fakeProviderConfigControllersManager struct {
	manager.ProviderConfigControllersManager
	mu             sync.Mutex
	startedConfigs map[string]*providerconfig.ProviderConfig
	stoppedConfigs map[string]*providerconfig.ProviderConfig

	startErr error // optional injected error
	stopErr  error // optional injected error
}

func newFakeProviderConfigControllersManager() *fakeProviderConfigControllersManager {
	return &fakeProviderConfigControllersManager{
		ProviderConfigControllersManager: manager.ProviderConfigControllersManager{},
		startedConfigs:                   make(map[string]*providerconfig.ProviderConfig),
		stoppedConfigs:                   make(map[string]*providerconfig.ProviderConfig),
	}
}

func (f *fakeProviderConfigControllersManager) StartControllersForProviderConfig(pc *providerconfig.ProviderConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return f.startErr
	}
	f.startedConfigs[pc.Name] = pc
	return nil
}

func (f *fakeProviderConfigControllersManager) StopControllersForProviderConfig(pc *providerconfig.ProviderConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopErr != nil {
		klog.Errorf("fake error stopping controllers: %v", f.stopErr)
	}
	f.stoppedConfigs[pc.Name] = pc
}

func (f *fakeProviderConfigControllersManager) HasStarted(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.startedConfigs[name]
	return ok
}

func (f *fakeProviderConfigControllersManager) HasStopped(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.stoppedConfigs[name]
	return ok
}

// wrapper that holds references to the controller under test plus some fakes
type testProviderConfigController struct {
	t      *testing.T
	stopCh chan struct{}

	manager      *fakeProviderConfigControllersManager
	pcController *ProviderConfigController
	pcClient     *fakeproviderconfigclient.Clientset
	pcInformer   cache.SharedIndexInformer
}

func newTestProviderConfigController(t *testing.T) *testProviderConfigController {
	pcClient := fakeproviderconfigclient.NewSimpleClientset()

	fakeManager := newFakeProviderConfigControllersManager()

	providerConfigInformer := providerconfiginformers.NewSharedInformerFactory(pcClient, 0).Cloud().V1().ProviderConfigs().Informer()

	stopCh := make(chan struct{})

	logger := klog.TODO()
	ctrl := NewProviderConfigController(
		fakeManager,
		providerConfigInformer,
		stopCh,
		logger,
	)

	go providerConfigInformer.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, providerConfigInformer.HasSynced) {
		t.Fatalf("Failed to sync caches")
	}

	return &testProviderConfigController{
		t:            t,
		stopCh:       stopCh,
		pcController: ctrl,
		manager:      fakeManager,
		pcClient:     pcClient,
		pcInformer:   providerConfigInformer,
	}
}

func addProviderConfig(t *testing.T, tc *testProviderConfigController, pc *providerconfig.ProviderConfig) {
	t.Helper()
	_, err := tc.pcClient.CloudV1().ProviderConfigs(pc.Namespace).Create(context.TODO(), pc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create ProviderConfig: %v", err)
	}
	err = tc.pcInformer.GetIndexer().Add(pc)
	if err != nil {
		t.Fatalf("failed to add ProviderConfig to indexer: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
}

func updateProviderConfig(t *testing.T, tc *testProviderConfigController, pc *providerconfig.ProviderConfig) {
	t.Helper()
	_, err := tc.pcClient.CloudV1().ProviderConfigs(pc.Namespace).Update(context.TODO(), pc, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("failed to update ProviderConfig: %v", err)
	}
	tc.pcInformer.GetIndexer().Update(pc)
	time.Sleep(100 * time.Millisecond)
}

func TestStartAndStop(t *testing.T) {
	tc := newTestProviderConfigController(t)

	// Start the controller in a separate goroutine
	go tc.pcController.Run()

	// Let it run briefly, then stop
	time.Sleep(200 * time.Millisecond)
	close(tc.stopCh) // triggers stop

	// Wait some time for graceful shutdown
	time.Sleep(200 * time.Millisecond)

	// If no panic or deadlock => success
}

func TestCreateDeleteProviderConfig(t *testing.T) {
	tc := newTestProviderConfigController(t)
	go tc.pcController.Run()
	defer close(tc.stopCh)

	pc := &providerconfig.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pc-delete",
			Namespace: "test-namespace",
		},
	}
	addProviderConfig(t, tc, pc)

	// Manager should have started it
	if !tc.manager.HasStarted("pc-delete") {
		t.Errorf("expected manager to have started 'pc-delete'")
	}
	if tc.manager.HasStopped("pc-delete") {
		t.Errorf("did not expect manager to have stopped 'pc-delete'")
	}

	// Now update it to have a DeletionTimestamp => triggers Stop
	pc2 := pc.DeepCopy()
	pc2.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	updateProviderConfig(t, tc, pc2)

	if !tc.manager.HasStopped("pc-delete") {
		t.Errorf("expected manager to stop 'pc-delete', but it didn't")
	}
}

// TestProcessUpdateFunc ensures UpdateFunc enqueues the item, triggers re-sync.
func TestProcessUpdateFunc(t *testing.T) {
	tc := newTestProviderConfigController(t)
	go tc.pcController.Run()
	defer close(tc.stopCh)

	pcOld := &providerconfig.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pc-old",
		},
		Spec: providerconfig.ProviderConfigSpec{
			ProjectID: "old-project",
		},
	}

	pcNew := &providerconfig.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pc-old",
		},
		Spec: providerconfig.ProviderConfigSpec{
			ProjectID: "updated-project",
		},
	}

	// Add the old object => triggers Add
	addProviderConfig(t, tc, pcOld)
	if !tc.manager.HasStarted("pc-old") {
		t.Errorf("Expected manager to start controllers for pc-old (on Add), but it didn't")
	}

	// Now we "update" the object => triggers UpdateFunc => re-enqueue
	updateProviderConfig(t, tc, pcNew)

	// Ensure the manager's startedConfigs map is updated
	tc.manager.mu.Lock()
	defer tc.manager.mu.Unlock()
	projectVal, exists := tc.manager.startedConfigs["pc-old"]
	if !exists {
		t.Errorf("expected manager to have started config for 'pc-old', but it was not found")
	} else if projectVal.Spec.ProjectID != "updated-project" {
		t.Errorf("expected manager to have started config with updated project 'updated-project', got %q", projectVal.Spec.ProjectID)
	}
}

// TestSyncNonExistent verifies that if the controller can't find the item in indexer, we return no error and do nothing.
func TestSyncNonExistent(t *testing.T) {
	tc := newTestProviderConfigController(t)
	go tc.pcController.Run()
	defer close(tc.stopCh)

	key := "some-ns/some-nonexistent"
	tc.pcController.providerConfigQueue.Enqueue(key)

	time.Sleep(200 * time.Millisecond)

	// No starts or stops should have happened
	if len(tc.manager.startedConfigs) != 0 {
		t.Errorf("unexpected StartControllersForProviderConfig call: %v", tc.manager.startedConfigs)
	}
	if len(tc.manager.stoppedConfigs) != 0 {
		t.Errorf("unexpected StopControllersForProviderConfig call: %v", tc.manager.stoppedConfigs)
	}
}

// TestSyncBadObjectType ensures that if we get an unexpected type out of the indexer, we log an error but skip it.
func TestSyncBadObjectType(t *testing.T) {
	tc := newTestProviderConfigController(t)
	go tc.pcController.Run()
	defer close(tc.stopCh)

	// Insert something that is not *ProviderConfig
	tc.pcInformer.GetIndexer().Add(&struct{ Name string }{Name: "not-a-pc"})

	time.Sleep(200 * time.Millisecond)

	if len(tc.manager.startedConfigs) != 0 {
		t.Errorf("did not expect manager starts with a non-ProviderConfig object")
	}
	if len(tc.manager.stoppedConfigs) != 0 {
		t.Errorf("did not expect manager stops with a non-ProviderConfig object")
	}
}
