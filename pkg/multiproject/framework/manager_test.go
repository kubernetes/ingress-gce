package framework

import (
	"context"
	"fmt"
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/multiproject/common/finalizer"
	providerconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned/fake"
	"k8s.io/klog/v2/ktesting"
)

// mockControllerStarter is a mock implementation of ControllerStarter for testing.
type mockControllerStarter struct {
	mu                 sync.Mutex
	startCalls         int
	shouldFailStart    bool
	startedControllers map[string]chan<- struct{}
	startCounts        map[string]int
}

func newMockControllerStarter() *mockControllerStarter {
	return &mockControllerStarter{
		startedControllers: make(map[string]chan<- struct{}),
		startCounts:        make(map[string]int),
	}
}

func (m *mockControllerStarter) StartController(pc *providerconfig.ProviderConfig) (chan<- struct{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.startCalls++
	m.startCounts[pc.Name]++

	if m.shouldFailStart {
		return nil, fmt.Errorf("mock start failure")
	}

	stopCh := make(chan struct{})
	m.startedControllers[pc.Name] = stopCh
	return stopCh, nil
}

func (m *mockControllerStarter) getStartCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startCalls
}

func createTestProviderConfig(name string) *providerconfig.ProviderConfig {
	return &providerconfig.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: providerconfig.ProviderConfigSpec{
			ProjectID: "test-project",
		},
	}
}

// TestManagerStartIdempotent verifies that starting the same controller multiple times is idempotent.
func TestManagerStartIdempotent(t *testing.T) {
	ctx := context.TODO()
	logger, _ := ktesting.NewTestContext(t)
	client := providerconfigclient.NewSimpleClientset()
	mockStarter := newMockControllerStarter()

	manager := newManager(
		client,
		"test-finalizer",
		mockStarter,
		logger,
	)

	pc := createTestProviderConfig("test-pc")

	// Create the ProviderConfig in the fake client
	_, err := client.CloudV1().ProviderConfigs().Create(ctx, pc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test ProviderConfig: %v", err)
	}

	// First start should succeed
	err = manager.StartControllersForProviderConfig(pc)
	if err != nil {
		t.Fatalf("First start failed: %v", err)
	}

	// Second start should be idempotent (no error, no additional controller started)
	err = manager.StartControllersForProviderConfig(pc)
	if err != nil {
		t.Fatalf("Second start failed: %v", err)
	}

	// Should only have started once
	if mockStarter.getStartCallCount() != 1 {
		t.Errorf("Expected 1 start call, got %d", mockStarter.getStartCallCount())
	}
}

// TestManagerStartAddsFinalizerBeforeControllerStarts verifies that the finalizer
// is added before the controller starts.
func TestManagerStartAddsFinalizerBeforeControllerStarts(t *testing.T) {
	ctx := context.TODO()
	logger, _ := ktesting.NewTestContext(t)
	client := providerconfigclient.NewSimpleClientset()
	mockStarter := newMockControllerStarter()

	finalizerName := "test-finalizer"
	manager := newManager(
		client,
		finalizerName,
		mockStarter,
		logger,
	)

	pc := createTestProviderConfig("test-pc")

	// Create the ProviderConfig in the fake client
	_, err := client.CloudV1().ProviderConfigs().Create(ctx, pc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test ProviderConfig: %v", err)
	}

	err = manager.StartControllersForProviderConfig(pc)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify finalizer was added
	updatedPC, err := client.CloudV1().ProviderConfigs().Get(ctx, pc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get updated ProviderConfig: %v", err)
	}

	if !finalizer.HasGivenFinalizer(updatedPC.ObjectMeta, finalizerName) {
		t.Errorf("Finalizer %s was not added to ProviderConfig", finalizerName)
	}
}

// TestManagerStartFailureRollsBackFinalizer verifies that if controller startup fails,
// the finalizer is rolled back.
func TestManagerStartFailureRollsBackFinalizer(t *testing.T) {
	ctx := context.TODO()
	logger, _ := ktesting.NewTestContext(t)
	client := providerconfigclient.NewSimpleClientset()
	mockStarter := newMockControllerStarter()
	mockStarter.shouldFailStart = true

	finalizerName := "test-finalizer"
	manager := newManager(
		client,
		finalizerName,
		mockStarter,
		logger,
	)

	pc := createTestProviderConfig("test-pc")

	// Create the ProviderConfig in the fake client
	_, err := client.CloudV1().ProviderConfigs().Create(ctx, pc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test ProviderConfig: %v", err)
	}

	// Start should fail
	err = manager.StartControllersForProviderConfig(pc)
	if err == nil {
		t.Fatal("Expected start to fail, but it succeeded")
	}

	// Verify finalizer was rolled back
	updatedPC, err := client.CloudV1().ProviderConfigs().Get(ctx, pc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get updated ProviderConfig: %v", err)
	}

	if finalizer.HasGivenFinalizer(updatedPC.ObjectMeta, finalizerName) {
		t.Errorf("Finalizer %s was not rolled back after start failure", finalizerName)
	}
}

// TestManagerStartFailureWithExistingFinalizerPreservesFinalizer verifies that a start failure
// does not remove a pre-existing finalizer that was not added by this manager instance.
func TestManagerStartFailureWithExistingFinalizerPreservesFinalizer(t *testing.T) {
	ctx := context.TODO()
	logger, _ := ktesting.NewTestContext(t)
	client := providerconfigclient.NewSimpleClientset()
	mockStarter := newMockControllerStarter()
	mockStarter.shouldFailStart = true

	finalizerName := "test-finalizer"
	manager := newManager(
		client,
		finalizerName,
		mockStarter,
		logger,
	)

	pc := createTestProviderConfig("test-pc-existing-finalizer")
	pc.Finalizers = []string{finalizerName}

	_, err := client.CloudV1().ProviderConfigs().Create(ctx, pc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test ProviderConfig: %v", err)
	}

	err = manager.StartControllersForProviderConfig(pc)
	if err == nil {
		t.Fatal("Expected start to fail when controller start returns error")
	}

	updatedPC, err := client.CloudV1().ProviderConfigs().Get(ctx, pc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get updated ProviderConfig: %v", err)
	}

	if !finalizer.HasGivenFinalizer(updatedPC.ObjectMeta, finalizerName) {
		t.Fatalf("Expected pre-existing finalizer to be preserved on failure, got %v", updatedPC.Finalizers)
	}
}

// TestManagerStopRemovesFinalizer verifies that stopping a controller removes the finalizer.
func TestManagerStopRemovesFinalizer(t *testing.T) {
	ctx := context.TODO()
	logger, _ := ktesting.NewTestContext(t)
	client := providerconfigclient.NewSimpleClientset()
	mockStarter := newMockControllerStarter()

	finalizerName := "test-finalizer"
	manager := newManager(
		client,
		finalizerName,
		mockStarter,
		logger,
	)

	pc := createTestProviderConfig("test-pc")

	// Create the ProviderConfig in the fake client
	_, err := client.CloudV1().ProviderConfigs().Create(ctx, pc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test ProviderConfig: %v", err)
	}

	// Start the controller
	err = manager.StartControllersForProviderConfig(pc)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify finalizer exists
	updatedPC, err := client.CloudV1().ProviderConfigs().Get(ctx, pc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get updated ProviderConfig: %v", err)
	}
	if !finalizer.HasGivenFinalizer(updatedPC.ObjectMeta, finalizerName) {
		t.Fatal("Finalizer was not added")
	}

	// Stop the controller
	err = manager.StopControllersForProviderConfig(updatedPC)
	if err != nil {
		t.Fatalf("StopControllersForProviderConfig(%s) failed: %v", updatedPC.Name, err)
	}

	// Verify finalizer was removed
	finalPC, err := client.CloudV1().ProviderConfigs().Get(ctx, pc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get final ProviderConfig: %v", err)
	}

	if finalizer.HasGivenFinalizer(finalPC.ObjectMeta, finalizerName) {
		t.Errorf("Finalizer %s was not removed after stop", finalizerName)
	}
}

// TestManagerStopIdempotent verifies that stopping a non-existent controller is safe.
func TestManagerStopIdempotent(t *testing.T) {
	ctx := context.TODO()
	logger, _ := ktesting.NewTestContext(t)
	client := providerconfigclient.NewSimpleClientset()
	mockStarter := newMockControllerStarter()

	manager := newManager(
		client,
		"test-finalizer",
		mockStarter,
		logger,
	)

	pc := createTestProviderConfig("test-pc")

	// Create the ProviderConfig in the fake client
	_, err := client.CloudV1().ProviderConfigs().Create(ctx, pc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test ProviderConfig: %v", err)
	}

	// Stop without start should not panic
	err = manager.StopControllersForProviderConfig(pc)
	if err != nil {
		t.Fatalf("StopControllersForProviderConfig(%s) failed: %v", pc.Name, err)
	}

	// Double stop should also be safe
	err = manager.StopControllersForProviderConfig(pc)
	if err != nil {
		t.Fatalf("StopControllersForProviderConfig(%s) failed: %v", pc.Name, err)
	}
}

// TestManagerStopRemovesFinalizerWhenNoControllerExists verifies that
// StopControllersForProviderConfig removes the finalizer even when no controller
// mapping exists (e.g., after process restart or if controller was never started).
// This ensures ProviderConfig deletion can proceed instead of stalling indefinitely.
func TestManagerStopRemovesFinalizerWhenNoControllerExists(t *testing.T) {
	ctx := context.TODO()
	logger, _ := ktesting.NewTestContext(t)
	client := providerconfigclient.NewSimpleClientset()
	mockStarter := newMockControllerStarter()

	finalizerName := "test-finalizer"
	manager := newManager(
		client,
		finalizerName,
		mockStarter,
		logger,
	)

	// Create a ProviderConfig with the finalizer already present.
	// This simulates a scenario where:
	// 1. The controller previously started and added the finalizer
	// 2. The process restarted, losing the in-memory controller mapping
	// 3. The ProviderConfig is now being deleted
	pc := createTestProviderConfig("test-pc")
	pc.Finalizers = []string{finalizerName}

	_, err := client.CloudV1().ProviderConfigs().Create(ctx, pc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test ProviderConfig: %v", err)
	}

	// Verify finalizer exists
	pcBefore, err := client.CloudV1().ProviderConfigs().Get(ctx, pc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get ProviderConfig: %v", err)
	}
	if !finalizer.HasGivenFinalizer(pcBefore.ObjectMeta, finalizerName) {
		t.Fatal("Finalizer was not present on initial ProviderConfig")
	}

	// Call Stop WITHOUT ever calling Start.
	// This means no controller mapping exists in the manager.
	// The manager should still remove the finalizer regardless.
	err = manager.StopControllersForProviderConfig(pcBefore)
	if err != nil {
		t.Fatalf("StopControllersForProviderConfig(%s) failed: %v", pcBefore.Name, err)
	}

	// Verify finalizer was removed even though no controller existed
	pcAfter, err := client.CloudV1().ProviderConfigs().Get(ctx, pc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get ProviderConfig after stop: %v", err)
	}

	if finalizer.HasGivenFinalizer(pcAfter.ObjectMeta, finalizerName) {
		t.Errorf("Finalizer %s was NOT removed when no controller existed - THIS IS THE BUG", finalizerName)
	}
}

// TestManagerMultipleProviderConfigs verifies that multiple ProviderConfigs can be managed independently.
func TestManagerMultipleProviderConfigs(t *testing.T) {
	ctx := context.TODO()
	logger, _ := ktesting.NewTestContext(t)
	client := providerconfigclient.NewSimpleClientset()
	mockStarter := newMockControllerStarter()

	manager := newManager(
		client,
		"test-finalizer",
		mockStarter,
		logger,
	)

	pc1 := createTestProviderConfig("pc-1")
	pc2 := createTestProviderConfig("pc-2")
	pc3 := createTestProviderConfig("pc-3")

	// Create all ProviderConfigs
	for _, pc := range []*providerconfig.ProviderConfig{pc1, pc2, pc3} {
		_, err := client.CloudV1().ProviderConfigs().Create(ctx, pc, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create ProviderConfig %s: %v", pc.Name, err)
		}
	}

	// Start all controllers
	testCases := []struct {
		pc *providerconfig.ProviderConfig
	}{
		{pc: pc1},
		{pc: pc2},
		{pc: pc3},
	}

	for _, tc := range testCases {
		err := manager.StartControllersForProviderConfig(tc.pc)
		if err != nil {
			t.Fatalf("Failed to start controller for %s: %v", tc.pc.Name, err)
		}
	}

	// Verify all controllers started
	if mockStarter.getStartCallCount() != 3 {
		t.Errorf("Expected 3 start calls, got %d", mockStarter.getStartCallCount())
	}

	// Stop one controller
	var err error
	err = manager.StopControllersForProviderConfig(pc2)
	if err != nil {
		t.Fatalf("StopControllersForProviderConfig(%s) failed: %v", pc2.Name, err)
	}

	// Start pc2 again
	err = manager.StartControllersForProviderConfig(pc2)
	if err != nil {
		t.Fatalf("Failed to restart controller for pc-2: %v", err)
	}

	// Should have 4 total starts now
	if mockStarter.getStartCallCount() != 4 {
		t.Errorf("Expected 4 start calls after restart, got %d", mockStarter.getStartCallCount())
	}
}
