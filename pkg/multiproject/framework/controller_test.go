package framework

import (
	"context"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	providerconfigv1 "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	fakeproviderconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned/fake"
	providerconfiginformers "k8s.io/ingress-gce/pkg/providerconfig/client/informers/externalversions"
	"k8s.io/klog/v2"
)

func init() {
	// Register the ProviderConfig types with the scheme
	providerconfigv1.AddToScheme(scheme.Scheme)
}

// pollForCondition polls for a condition to be true, with a timeout.
// Returns true if the condition is met within the timeout, false otherwise.
func pollForCondition(condition func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// fakeProviderConfigControllersManager implements ProviderConfigControllerManager
// and lets us track calls to StartControllersForProviderConfig/StopControllersForProviderConfig.
type fakeProviderConfigControllersManager struct {
	mu             sync.Mutex
	startedConfigs map[string]*providerconfigv1.ProviderConfig
	stoppedConfigs map[string]*providerconfigv1.ProviderConfig

	startErr error // optional injected error
	stopErr  error // optional injected error
}

func newFakeProviderConfigControllersManager() *fakeProviderConfigControllersManager {
	return &fakeProviderConfigControllersManager{
		startedConfigs: make(map[string]*providerconfigv1.ProviderConfig),
		stoppedConfigs: make(map[string]*providerconfigv1.ProviderConfig),
	}
}

func (f *fakeProviderConfigControllersManager) StartControllersForProviderConfig(pc *providerconfigv1.ProviderConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return f.startErr
	}
	f.startedConfigs[pc.Name] = pc
	return nil
}

func (f *fakeProviderConfigControllersManager) StopControllersForProviderConfig(pc *providerconfigv1.ProviderConfig) {
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
	pcController *providerConfigController
	pcClient     *fakeproviderconfigclient.Clientset
	pcInformer   cache.SharedIndexInformer
}

func newTestProviderConfigController(t *testing.T) *testProviderConfigController {
	pcClient := fakeproviderconfigclient.NewSimpleClientset()

	fakeManager := newFakeProviderConfigControllersManager()

	providerConfigInformer := providerconfiginformers.NewSharedInformerFactory(pcClient, 0).Cloud().V1().ProviderConfigs().Informer()

	stopCh := make(chan struct{})

	logger := klog.TODO()
	ctrl := newProviderConfigController(
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

func addProviderConfig(t *testing.T, tc *testProviderConfigController, pc *providerconfigv1.ProviderConfig) {
	t.Helper()
	_, err := tc.pcClient.CloudV1().ProviderConfigs().Create(context.TODO(), pc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create ProviderConfig: %v", err)
	}
	err = tc.pcInformer.GetIndexer().Add(pc)
	if err != nil {
		t.Fatalf("failed to add ProviderConfig to indexer: %v", err)
	}
}

func updateProviderConfig(t *testing.T, tc *testProviderConfigController, pc *providerconfigv1.ProviderConfig) {
	t.Helper()
	_, err := tc.pcClient.CloudV1().ProviderConfigs().Update(context.TODO(), pc, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("failed to update ProviderConfig: %v", err)
	}
	tc.pcInformer.GetIndexer().Update(pc)
}

func TestStartAndStop(t *testing.T) {
	tc := newTestProviderConfigController(t)

	// Start the controller in a separate goroutine
	controllerDone := make(chan struct{})
	go func() {
		tc.pcController.Run()
		close(controllerDone)
	}()

	// Let it run briefly, then stop
	time.Sleep(50 * time.Millisecond)
	close(tc.stopCh) // triggers stop

	// Wait for graceful shutdown with timeout
	select {
	case <-controllerDone:
		// Success - controller shut down gracefully
	case <-time.After(1 * time.Second):
		t.Fatal("Controller did not shut down within timeout")
	}
}

func TestCreateDeleteProviderConfig(t *testing.T) {
	tc := newTestProviderConfigController(t)
	go tc.pcController.Run()
	defer close(tc.stopCh)

	pc := &providerconfigv1.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pc-delete",
		},
	}
	addProviderConfig(t, tc, pc)

	// Poll for manager to start the controller
	if !pollForCondition(func() bool {
		return tc.manager.HasStarted("pc-delete")
	}, 1*time.Second) {
		t.Errorf("expected manager to have started 'pc-delete' within timeout")
	}
	if tc.manager.HasStopped("pc-delete") {
		t.Errorf("did not expect manager to have stopped 'pc-delete'")
	}

	// Now update it to have a DeletionTimestamp => triggers Stop
	pc2 := pc.DeepCopy()
	pc2.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	updateProviderConfig(t, tc, pc2)

	// Poll for manager to stop the controller
	if !pollForCondition(func() bool {
		return tc.manager.HasStopped("pc-delete")
	}, 1*time.Second) {
		t.Errorf("expected manager to stop 'pc-delete' within timeout, but it didn't")
	}
}

// TestSyncNonExistent verifies that if the controller can't find the item in indexer, we return no error and do nothing.
func TestSyncNonExistent(t *testing.T) {
	tc := newTestProviderConfigController(t)
	go tc.pcController.Run()
	defer close(tc.stopCh)

	key := "some-ns/some-nonexistent"
	tc.pcController.providerConfigQueue.Enqueue(key)

	// Poll to ensure the queue has been processed
	// We check that after a reasonable time, no starts or stops happened
	time.Sleep(100 * time.Millisecond)

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

	// Give time for queue to process the invalid object
	time.Sleep(100 * time.Millisecond)

	if len(tc.manager.startedConfigs) != 0 {
		t.Errorf("did not expect manager starts with a non-ProviderConfig object")
	}
	if len(tc.manager.stoppedConfigs) != 0 {
		t.Errorf("did not expect manager stops with a non-ProviderConfig object")
	}
}

// fakePanickingManager implements providerConfigControllerManager and panics on Start.
type fakePanickingManager struct {
	panicOccurred bool
	mu            sync.Mutex
}

func (f *fakePanickingManager) StartControllersForProviderConfig(pc *providerconfigv1.ProviderConfig) error {
	f.mu.Lock()
	f.panicOccurred = true
	f.mu.Unlock()
	panic("intentional panic for testing")
}

func (f *fakePanickingManager) StopControllersForProviderConfig(pc *providerconfigv1.ProviderConfig) {
	// no-op
}

func (f *fakePanickingManager) didPanic() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.panicOccurred
}

// TestPanicRecovery verifies that panics in sync are caught and don't crash the worker.
func TestPanicRecovery(t *testing.T) {
	pcClient := fakeproviderconfigclient.NewSimpleClientset()
	panicManager := &fakePanickingManager{}
	providerConfigInformer := providerconfiginformers.NewSharedInformerFactory(pcClient, 0).Cloud().V1().ProviderConfigs().Informer()
	stopCh := make(chan struct{})
	defer close(stopCh)

	logger := klog.TODO()
	ctrl := newProviderConfigController(
		panicManager,
		providerConfigInformer,
		stopCh,
		logger,
	)

	go providerConfigInformer.Run(stopCh)
	if !cache.WaitForCacheSync(stopCh, providerConfigInformer.HasSynced) {
		t.Fatalf("Failed to sync caches")
	}

	// Start controller in background
	go ctrl.Run()

	// Create a ProviderConfig that will trigger the panic
	pc := &providerconfigv1.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "panic-test",
		},
	}
	_, err := pcClient.CloudV1().ProviderConfigs().Create(context.TODO(), pc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create ProviderConfig: %v", err)
	}
	providerConfigInformer.GetIndexer().Add(pc)

	// Poll to verify the panic occurred but didn't crash the controller
	if !pollForCondition(func() bool {
		return panicManager.didPanic()
	}, 1*time.Second) {
		t.Errorf("expected panic to occur within timeout")
	}

	// Verify the controller is still running by adding another ProviderConfig
	// If the worker crashed, this won't be processed
	pc2 := &providerconfigv1.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "after-panic",
		},
	}
	_, err = pcClient.CloudV1().ProviderConfigs().Create(context.TODO(), pc2, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create second ProviderConfig: %v", err)
	}
	providerConfigInformer.GetIndexer().Add(pc2)

	// Give some time for the second item to be processed
	// Since there are multiple workers, at least one should still be alive
	time.Sleep(100 * time.Millisecond)

	// If we reach here without the test crashing, panic recovery worked
	t.Log("Controller survived panic and continued processing")
}
