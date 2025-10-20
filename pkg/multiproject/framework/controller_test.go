package framework

import (
	"context"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	providerconfigv1 "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/multiproject/common/finalizer"
	fakeproviderconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned/fake"
	providerconfiginformers "k8s.io/ingress-gce/pkg/providerconfig/client/informers/externalversions"
	"k8s.io/klog/v2"
)

func init() {
	// Register the ProviderConfig types with the scheme
	providerconfigv1.AddToScheme(scheme.Scheme)
}

// fakePCManager implements controllerManager
// and lets us track calls to StartControllersForProviderConfig/StopControllersForProviderConfig.
type fakePCManager struct {
	mu             sync.Mutex
	startedConfigs map[string]*providerconfigv1.ProviderConfig
	stoppedConfigs map[string]*providerconfigv1.ProviderConfig

	startErr error // optional injected error
	stopErr  error // optional injected error

	pcClient      *fakeproviderconfigclient.Clientset
	finalizerName string
	logger        klog.Logger
}

func newFakeProviderConfigControllersManager(pcClient *fakeproviderconfigclient.Clientset, finalizerName string, logger klog.Logger) *fakePCManager {
	return &fakePCManager{
		startedConfigs: make(map[string]*providerconfigv1.ProviderConfig),
		stoppedConfigs: make(map[string]*providerconfigv1.ProviderConfig),
		pcClient:       pcClient,
		finalizerName:  finalizerName,
		logger:         logger,
	}
}

func (f *fakePCManager) StartControllersForProviderConfig(pc *providerconfigv1.ProviderConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return f.startErr
	}
	// Add finalizer before recording start
	err := finalizer.EnsureProviderConfigFinalizer(pc, f.finalizerName, f.pcClient, f.logger)
	if err != nil {
		return err
	}
	f.startedConfigs[pc.Name] = pc
	return nil
}

func (f *fakePCManager) StopControllersForProviderConfig(pc *providerconfigv1.ProviderConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopErr != nil {
		return f.stopErr
	}
	f.stoppedConfigs[pc.Name] = pc
	// Fetch latest PC and remove finalizer
	latestPC, err := f.pcClient.CloudV1().ProviderConfigs().Get(context.Background(), pc.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	return finalizer.DeleteProviderConfigFinalizer(latestPC, f.finalizerName, f.pcClient, f.logger)
}

func (f *fakePCManager) HasStarted(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.startedConfigs[name]
	return ok
}

func (f *fakePCManager) HasStopped(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.stoppedConfigs[name]
	return ok
}

// wrapper that holds references to the controller under test plus some fakes
type testProviderConfigController struct {
	t      *testing.T
	stopCh chan struct{}

	manager      *fakePCManager
	pcController *Controller
	pcClient     *fakeproviderconfigclient.Clientset
	pcInformer   cache.SharedIndexInformer
}

func newTestProviderConfigController(t *testing.T) *testProviderConfigController {
	pcClient := fakeproviderconfigclient.NewSimpleClientset()

	logger := klog.TODO()
	fakeManager := newFakeProviderConfigControllersManager(pcClient, "test-finalizer", logger)

	providerConfigInformer := providerconfiginformers.NewSharedInformerFactory(pcClient, 0).Cloud().V1().ProviderConfigs().Informer()

	stopCh := make(chan struct{})

	ctrl := newController(
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

// TestStartAndStop verifies that the controller starts and stops gracefully when stopCh is closed.
func TestStartAndStop(t *testing.T) {
	tc := newTestProviderConfigController(t)

	// Start the controller in a separate goroutine
	controllerDone := make(chan struct{})
	go func() {
		tc.pcController.Run()
		close(controllerDone)
	}()

	// Signal stop immediately - we're testing graceful shutdown
	close(tc.stopCh)

	// Poll for graceful shutdown
	err := wait.PollImmediate(10*time.Millisecond, 1*time.Second, func() (bool, error) {
		select {
		case <-controllerDone:
			return true, nil
		default:
			return false, nil
		}
	})
	if err != nil {
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
	err := wait.PollImmediate(10*time.Millisecond, 1*time.Second, func() (bool, error) {
		return tc.manager.HasStarted("pc-delete"), nil
	})
	if err != nil {
		t.Errorf("expected manager to have started 'pc-delete' within timeout: %v", err)
	}
	if tc.manager.HasStopped("pc-delete") {
		t.Errorf("did not expect manager to have stopped 'pc-delete'")
	}

	// Now update it to have a DeletionTimestamp => triggers Stop
	pc2 := pc.DeepCopy()
	pc2.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	updateProviderConfig(t, tc, pc2)

	// Poll for manager to stop the controller
	err = wait.PollImmediate(10*time.Millisecond, 1*time.Second, func() (bool, error) {
		return tc.manager.HasStopped("pc-delete"), nil
	})
	if err != nil {
		t.Errorf("expected manager to stop 'pc-delete' within timeout: %v", err)
	}

	// Verify finalizer was removed
	updatedPC, err := tc.pcClient.CloudV1().ProviderConfigs().Get(context.TODO(), "pc-delete", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get ProviderConfig: %v", err)
	}
	if finalizer.HasGivenFinalizer(updatedPC.ObjectMeta, "test-finalizer") {
		t.Errorf("expected finalizer to be removed after stop")
	}
}

// TestSyncNonExistent verifies that if the controller can't find the item in indexer, we return no error and do nothing.
func TestSyncNonExistent(t *testing.T) {
	tc := newTestProviderConfigController(t)
	go tc.pcController.Run()
	defer close(tc.stopCh)

	key := "some-ns/some-nonexistent"
	tc.pcController.providerConfigQueue.Enqueue(key)

	// Poll for queue to be empty, indicating the item was processed
	err := wait.PollImmediate(10*time.Millisecond, 1*time.Second, func() (bool, error) {
		return tc.pcController.providerConfigQueue.Len() == 0, nil
	})
	if err != nil {
		t.Fatalf("queue did not become empty within timeout: %v", err)
	}

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

	// Poll for queue to be empty, indicating the item was processed
	err := wait.PollImmediate(10*time.Millisecond, 1*time.Second, func() (bool, error) {
		return tc.pcController.providerConfigQueue.Len() == 0, nil
	})
	if err != nil {
		t.Fatalf("queue did not become empty within timeout: %v", err)
	}

	if len(tc.manager.startedConfigs) != 0 {
		t.Errorf("did not expect manager starts with a non-ProviderConfig object")
	}
	if len(tc.manager.stoppedConfigs) != 0 {
		t.Errorf("did not expect manager stops with a non-ProviderConfig object")
	}
}

// fakePanickingManager implements controllerManager and panics on Start.
type fakePanickingManager struct {
	panicCount int
	mu         sync.Mutex
}

func (f *fakePanickingManager) StartControllersForProviderConfig(pc *providerconfigv1.ProviderConfig) error {
	f.mu.Lock()
	f.panicCount++
	f.mu.Unlock()
	panic("intentional panic for testing")
}

func (f *fakePanickingManager) StopControllersForProviderConfig(pc *providerconfigv1.ProviderConfig) error {
	return nil
}

func (f *fakePanickingManager) getPanicCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.panicCount
}

// TestPanicRecovery verifies that panics in sync are caught and don't crash the worker.
func TestPanicRecovery(t *testing.T) {
	pcClient := fakeproviderconfigclient.NewSimpleClientset()
	panicManager := &fakePanickingManager{}
	providerConfigInformer := providerconfiginformers.NewSharedInformerFactory(pcClient, 0).Cloud().V1().ProviderConfigs().Informer()
	stopCh := make(chan struct{})
	defer close(stopCh)

	logger := klog.TODO()
	ctrl := newController(
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
	err = wait.PollImmediate(10*time.Millisecond, 1*time.Second, func() (bool, error) {
		return panicManager.getPanicCount() >= 1, nil
	})
	if err != nil {
		t.Errorf("expected panic to occur within timeout: %v", err)
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

	// Poll for the second ProviderConfig to be processed (which will also panic).
	// If the controller crashed after the first panic, this won't be processed.
	err = wait.PollImmediate(10*time.Millisecond, 1*time.Second, func() (bool, error) {
		return panicManager.getPanicCount() >= 2, nil
	})
	if err != nil {
		t.Errorf("expected second panic to occur within timeout (controller may have crashed): %v", err)
	}

	t.Log("Controller survived panic and continued processing")
}
