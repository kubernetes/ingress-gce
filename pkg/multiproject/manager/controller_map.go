package manager

import "sync"

// ControllerMap is a thread-safe map for storing ControllerSet instances.
// It uses fine-grained locking to allow concurrent operations on different keys.
type ControllerMap struct {
	mu   sync.Mutex
	data map[string]*ControllerSet
}

// NewControllerMap creates a new thread-safe ControllerMap.
func NewControllerMap() *ControllerMap {
	return &ControllerMap{
		data: make(map[string]*ControllerSet),
	}
}

// Get retrieves a ControllerSet for the given key.
// Returns the ControllerSet and a boolean indicating whether it exists.
func (cm *ControllerMap) Get(key string) (*ControllerSet, bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cs, exists := cm.data[key]
	return cs, exists
}

// Set stores a ControllerSet for the given key.
func (cm *ControllerMap) Set(key string, cs *ControllerSet) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.data[key] = cs
}

// Delete removes and returns a ControllerSet for the given key.
// Returns the ControllerSet and a boolean indicating whether it existed.
func (cm *ControllerMap) Delete(key string) (*ControllerSet, bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cs, exists := cm.data[key]
	if exists {
		delete(cm.data, key)
	}
	return cs, exists
}
