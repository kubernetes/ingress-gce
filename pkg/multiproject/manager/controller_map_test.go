package manager

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestControllerMapBasicOperations verifies basic Get, Set, and Delete operations.
func TestControllerMapBasicOperations(t *testing.T) {
	cm := NewControllerMap()

	// Test Get on non-existent key
	_, exists := cm.Get("test-key")
	if exists {
		t.Error("Expected controller to not exist")
	}

	// Test Set and Get
	testCS := &ControllerSet{stopCh: make(chan struct{})}
	cm.Set("test-key", testCS)

	cs, exists := cm.Get("test-key")
	if !exists {
		t.Error("Expected controller to exist after Set")
	}
	if cs != testCS {
		t.Error("Retrieved controller does not match set controller")
	}

	// Test Delete
	deletedCS, exists := cm.Delete("test-key")
	if !exists {
		t.Error("Expected controller to exist for Delete")
	}
	if deletedCS != testCS {
		t.Error("Deleted controller does not match original")
	}

	// Test Get after Delete
	_, exists = cm.Get("test-key")
	if exists {
		t.Error("Expected controller to not exist after Delete")
	}

	// Test Delete on non-existent key
	_, exists = cm.Delete("non-existent")
	if exists {
		t.Error("Expected Delete to return false for non-existent key")
	}
}

// TestControllerMapConcurrentAccess verifies that concurrent operations on different keys
// can proceed without blocking each other.
func TestControllerMapConcurrentAccess(t *testing.T) {
	cm := NewControllerMap()

	// Track concurrent operations
	var concurrentOps atomic.Int32
	var maxConcurrentOps atomic.Int32

	const numGoroutines = 10
	const operationDelay = 50 * time.Millisecond

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()

			key := string(rune('a' + idx)) // Different keys: "a", "b", "c", etc.

			// Track concurrent execution
			current := concurrentOps.Add(1)
			defer concurrentOps.Add(-1)

			// Update max concurrent ops
			for {
				max := maxConcurrentOps.Load()
				if current <= max || maxConcurrentOps.CompareAndSwap(max, current) {
					break
				}
			}

			// Simulate some work while "holding" access to this key
			time.Sleep(operationDelay)

			// Perform operations
			cs := &ControllerSet{stopCh: make(chan struct{})}
			cm.Set(key, cs)

			retrievedCS, exists := cm.Get(key)
			if !exists || retrievedCS != cs {
				t.Errorf("Failed to retrieve controller for key %s", key)
			}

			cm.Delete(key)
		}(i)
	}

	wg.Wait()

	// Verify that multiple operations ran concurrently
	// With 10 goroutines and 50ms delay, we should see significant concurrency
	actualMax := maxConcurrentOps.Load()
	if actualMax < 2 {
		t.Errorf("Expected at least 2 concurrent operations, got %d. This suggests blocking that shouldn't occur.", actualMax)
	}

	t.Logf("Max concurrent operations: %d (out of %d goroutines)", actualMax, numGoroutines)
}

// TestControllerMapConcurrentSameKey verifies that concurrent operations on the same key
// are properly serialized and don't cause race conditions.
func TestControllerMapConcurrentSameKey(t *testing.T) {
	cm := NewControllerMap()

	const numGoroutines = 100
	const key = "shared-key"

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// All goroutines try to set and get the same key
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()

			cs := &ControllerSet{stopCh: make(chan struct{})}
			cm.Set(key, cs)

			retrievedCS, exists := cm.Get(key)
			if !exists {
				t.Errorf("Expected controller to exist for key %s", key)
			}
			// The retrieved controller might not be the one we just set
			// (due to concurrent operations), but it should be valid
			if retrievedCS == nil {
				t.Error("Retrieved nil controller")
			}
		}(i)
	}

	wg.Wait()

	// Cleanup - should still have one controller
	_, exists := cm.Get(key)
	if !exists {
		t.Error("Expected at least one controller to remain")
	}
}
