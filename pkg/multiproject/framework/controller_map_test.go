package framework

import (
	"testing"
)

// TestControllerMapBasicOperations verifies basic Get, GetOrCreate, and Delete operations.
func TestControllerMapBasicOperations(t *testing.T) {
	cm := NewControllerMap()

	// Test Get on non-existent key
	_, exists := cm.Get("test-key")
	if exists {
		t.Error("Expected controller to not exist")
	}

	// Test GetOrCreate creates new entry
	cs, existed := cm.GetOrCreate("test-key")
	if existed {
		t.Error("Expected controller to not exist before GetOrCreate")
	}
	if cs == nil {
		t.Error("Expected GetOrCreate to return a controllerSet")
	}

	// Test Get returns the same entry
	retrievedCS, exists := cm.Get("test-key")
	if !exists {
		t.Error("Expected controller to exist after GetOrCreate")
	}
	if retrievedCS != cs {
		t.Error("Retrieved controller does not match created controller")
	}

	// Test Delete
	cm.Delete("test-key")

	// Test Get after Delete
	_, exists = cm.Get("test-key")
	if exists {
		t.Error("Expected controller to not exist after Delete")
	}

	// Test Delete on non-existent key (should not panic)
	cm.Delete("non-existent")
}

// TestControllerMapGetOrCreate verifies GetOrCreate idempotency.
func TestControllerMapGetOrCreate(t *testing.T) {
	cm := NewControllerMap()

	first, existed := cm.GetOrCreate("alpha")
	if existed {
		t.Fatal("expected first GetOrCreate call to report non-existence")
	}
	if first == nil {
		t.Fatal("expected controllerSet instance on first GetOrCreate call")
	}

	second, existed := cm.GetOrCreate("alpha")
	if !existed {
		t.Fatal("expected second GetOrCreate call to report existence")
	}
	if first != second {
		t.Fatal("expected GetOrCreate to return the same controllerSet instance")
	}
}
