package framework

import (
	"context"
	"fmt"
	"slices"
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

func (m *mockControllerStarter) getStartCountForKey(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startCounts[key]
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
	manager.StopControllersForProviderConfig(updatedPC)

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
	manager.StopControllersForProviderConfig(pc)

	// Double stop should also be safe
	manager.StopControllersForProviderConfig(pc)
}

// TestManagerStopRemovesFinalizerWhenNoControllerExists verifies the critical bug fix:
// StopControllersForProviderConfig must remove the finalizer even when no controller
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

	// Critical test: Call Stop WITHOUT ever calling Start.
	// This means no controller mapping exists in the manager.
	// Before the fix, this would return early and never remove the finalizer.
	// After the fix, it should remove the finalizer regardless.
	manager.StopControllersForProviderConfig(pcBefore)

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
	manager.StopControllersForProviderConfig(pc2)

	// Start pc2 again
	err := manager.StartControllersForProviderConfig(pc2)
	if err != nil {
		t.Fatalf("Failed to restart controller for pc-2: %v", err)
	}

	// Should have 4 total starts now
	if mockStarter.getStartCallCount() != 4 {
		t.Errorf("Expected 4 start calls after restart, got %d", mockStarter.getStartCallCount())
	}
}

// TestManagerConcurrentStartStop verifies concurrent start/stop operations are safe.
func TestManagerConcurrentStartStop(t *testing.T) {
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

	const numGoroutines = 10
	const numProviderConfigs = 5

	// Create test ProviderConfigs
	providerConfigs := make([]*providerconfig.ProviderConfig, numProviderConfigs)
	for i := 0; i < numProviderConfigs; i++ {
		pc := createTestProviderConfig(fmt.Sprintf("pc-%d", i))
		providerConfigs[i] = pc
		_, err := client.CloudV1().ProviderConfigs().Create(ctx, pc, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create ProviderConfig: %v", err)
		}
	}

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrently start and stop controllers
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			pc := providerConfigs[idx%numProviderConfigs]

			// Try to start
			_ = manager.StartControllersForProviderConfig(pc)

			// Try to stop
			manager.StopControllersForProviderConfig(pc)
		}(i)
	}

	wg.Wait()

	// Should not panic and should handle concurrent operations gracefully
	t.Log("Concurrent operations completed without panic")
}

// TestManagerConcurrentStartSameProviderConfig verifies concurrent start attempts on the same ProviderConfig
// only result in a single controller start.
func TestManagerConcurrentStartSameProviderConfig(t *testing.T) {
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

	pc := createTestProviderConfig("shared-pc")

	_, err := client.CloudV1().ProviderConfigs().Create(ctx, pc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create ProviderConfig: %v", err)
	}

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			err := manager.StartControllersForProviderConfig(pc)
			if err != nil {
				t.Errorf("StartControllersForProviderConfig failed: %v", err)
			}
		}()
	}

	wg.Wait()

	startCount := mockStarter.getStartCountForKey(pc.Name)
	if startCount != 1 {
		t.Fatalf("Expected exactly one controller start, got %d", startCount)
	}

	updatedPC, err := client.CloudV1().ProviderConfigs().Get(ctx, pc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to retrieve ProviderConfig: %v", err)
	}
	manager.StopControllersForProviderConfig(updatedPC)
}

// TestLatestPCWithCleanupFinalizerGetFailure verifies that when API Get fails,
// latestPCWithCleanupFinalizer returns a copy with the finalizer without duplicates.
func TestLatestPCWithCleanupFinalizerGetFailure(t *testing.T) {
	logger, _ := ktesting.NewTestContext(t)
	// Use an empty clientset so Get will fail
	client := providerconfigclient.NewSimpleClientset()
	mockStarter := newMockControllerStarter()

	finalizerName := "test-finalizer"
	manager := newManager(
		client,
		finalizerName,
		mockStarter,
		logger,
	)

	testCases := []struct {
		name               string
		initialFinalizers  []string
		expectedFinalizers []string
	}{
		{
			name:               "no existing finalizers",
			initialFinalizers:  nil,
			expectedFinalizers: []string{finalizerName},
		},
		{
			name:               "has other finalizers",
			initialFinalizers:  []string{"other-finalizer"},
			expectedFinalizers: []string{"other-finalizer", finalizerName},
		},
		{
			name:               "already has cleanup finalizer",
			initialFinalizers:  []string{finalizerName},
			expectedFinalizers: []string{finalizerName},
		},
		{
			name:               "has both cleanup and other finalizers",
			initialFinalizers:  []string{"other-finalizer", finalizerName},
			expectedFinalizers: []string{"other-finalizer", finalizerName},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pc := &providerconfig.ProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-pc",
					Finalizers: tc.initialFinalizers,
				},
			}

			// Call latestPCWithCleanupFinalizer - Get will fail since PC doesn't exist
			result := manager.latestPCWithCleanupFinalizer(pc)

			// Verify result has the expected finalizers
			if len(result.Finalizers) != len(tc.expectedFinalizers) {
				t.Errorf("expected %d finalizers, got %d: %v", len(tc.expectedFinalizers), len(result.Finalizers), result.Finalizers)
			}

			for _, expectedFinalizer := range tc.expectedFinalizers {
				if !slices.Contains(result.Finalizers, expectedFinalizer) {
					t.Errorf("expected finalizer %s not found in result: %v", expectedFinalizer, result.Finalizers)
				}
			}

			// Verify no duplicate finalizers
			seen := make(map[string]bool)
			for _, f := range result.Finalizers {
				if seen[f] {
					t.Errorf("duplicate finalizer found: %s", f)
				}
				seen[f] = true
			}
		})
	}
}

// failOnceMockStarter is a mock ControllerStarter that fails on the first attempt
// and succeeds on subsequent attempts for each ProviderConfig.
type failOnceMockStarter struct {
	mu                 sync.Mutex
	attemptCounts      map[string]int
	startedControllers map[string]chan<- struct{}
}

func newFailOnceMockStarter() *failOnceMockStarter {
	return &failOnceMockStarter{
		attemptCounts:      make(map[string]int),
		startedControllers: make(map[string]chan<- struct{}),
	}
}

func (m *failOnceMockStarter) StartController(pc *providerconfig.ProviderConfig) (chan<- struct{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.attemptCounts[pc.Name]++

	// Fail on first attempt
	if m.attemptCounts[pc.Name] == 1 {
		return nil, fmt.Errorf("mock start failure on first attempt")
	}

	// Succeed on subsequent attempts
	stopCh := make(chan struct{})
	m.startedControllers[pc.Name] = stopCh
	return stopCh, nil
}

func (m *failOnceMockStarter) isStarted(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, started := m.startedControllers[key]
	return started
}

// TestManagerConcurrentStartWithFailureDoesNotLeakControllers verifies that when
// concurrent Start calls race with a failing Start, we don't leak running controllers.
// This is a regression test for the race condition where unlock happened before delete,
// allowing another goroutine to start a controller that would then be orphaned.
func TestManagerConcurrentStartWithFailureDoesNotLeakControllers(t *testing.T) {
	ctx := context.TODO()
	logger, _ := ktesting.NewTestContext(t)
	client := providerconfigclient.NewSimpleClientset()
	mockStarter := newFailOnceMockStarter()

	manager := newManager(
		client,
		"test-finalizer",
		mockStarter,
		logger,
	)

	pc := createTestProviderConfig("race-test-pc")

	_, err := client.CloudV1().ProviderConfigs().Create(ctx, pc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create ProviderConfig: %v", err)
	}

	// Launch two concurrent Start attempts.
	// Due to the failOnceMockStarter, the first attempt will fail.
	// The key test is that we don't leak controllers when racing start/failure.
	var wg sync.WaitGroup
	wg.Add(2)

	errors := make([]error, 2)

	go func() {
		defer wg.Done()
		errors[0] = manager.StartControllersForProviderConfig(pc)
	}()

	go func() {
		defer wg.Done()
		errors[1] = manager.StartControllersForProviderConfig(pc)
	}()

	wg.Wait()

	// Due to the race and revalidation, both might fail/abort in this specific scenario.
	// What matters is that if a controller was started, it's properly tracked.
	// If both failed, we should be able to start successfully now.
	if errors[0] != nil && errors[1] != nil {
		// Both failed/aborted - this can happen due to revalidation race.
		// Try starting again, should succeed now.
		err = manager.StartControllersForProviderConfig(pc)
		if err != nil {
			t.Fatalf("Expected final start to succeed after concurrent failures, got: %v", err)
		}
	}

	// Critical assertion: verify controller state is consistent
	isStarted := mockStarter.isStarted(pc.Name)
	cs, exists := manager.controllers.Get(providerConfigKey(pc))

	// Both should agree: either controller is running and tracked, or neither
	if isStarted && !exists {
		t.Fatal("Controller was started but not tracked in manager - this indicates a leaked controller (THE BUG)")
	}
	if !isStarted && exists && cs.stopCh != nil {
		t.Fatal("Controller is tracked as running but was never started - inconsistent state")
	}
	if isStarted && exists && cs.stopCh == nil {
		t.Fatal("Controller was started and tracked but stopCh is nil - inconsistent state")
	}

	// If controller is running, verify it's properly tracked
	if isStarted {
		if !exists {
			t.Fatal("Started controller is not tracked")
		}
		if cs.stopCh == nil {
			t.Fatal("Started controller has nil stopCh")
		}
	}

	// Clean up if controller is running
	if isStarted {
		manager.StopControllersForProviderConfig(pc)

		// Verify proper cleanup
		_, exists = manager.controllers.Get(providerConfigKey(pc))
		if exists {
			t.Error("Controller entry still exists after stop")
		}
	}
}
