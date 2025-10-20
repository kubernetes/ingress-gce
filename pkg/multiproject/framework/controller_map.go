package framework

import "sync"

// controllerSet holds controller-specific resources for a ProviderConfig.
// It contains the stop channel used to signal controller shutdown and a mutex
// that serializes lifecycle transitions for the underlying controllers.
type controllerSet struct {
	mu     sync.Mutex
	stopCh chan<- struct{}
}

// ControllerMap is a thread-safe map for storing controllerSet instances.
// It uses read-write locking to allow concurrent read operations.
type ControllerMap struct {
	mu   sync.RWMutex
	data map[string]*controllerSet
}

// NewControllerMap creates a new thread-safe ControllerMap.
func NewControllerMap() *ControllerMap {
	return &ControllerMap{
		data: make(map[string]*controllerSet),
	}
}

// Get retrieves a controllerSet for the given key.
// Returns the controllerSet and a boolean indicating whether it exists.
func (cm *ControllerMap) Get(key string) (*controllerSet, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	cs, exists := cm.data[key]
	return cs, exists
}

// GetOrCreate retrieves the controllerSet for the given key, creating a new entry when absent.
// The second return value indicates whether the controllerSet already existed.
func (cm *ControllerMap) GetOrCreate(key string) (*controllerSet, bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cs, exists := cm.data[key]; exists {
		return cs, true
	}
	cs := &controllerSet{}
	cm.data[key] = cs
	return cs, false
}

// Set stores a controllerSet for the given key.
func (cm *ControllerMap) Set(key string, cs *controllerSet) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.data[key] = cs
}

// Delete removes and returns a controllerSet for the given key.
// Returns the controllerSet and a boolean indicating whether it existed.
func (cm *ControllerMap) Delete(key string) (*controllerSet, bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cs, exists := cm.data[key]
	if exists {
		delete(cm.data, key)
	}
	return cs, exists
}

// DeleteIfSame removes the controllerSet for key only if it currently equals expected.
// Returns true if a deletion happened.
func (cm *ControllerMap) DeleteIfSame(key string, expected *controllerSet) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cur, ok := cm.data[key]
	if !ok || cur != expected {
		return false
	}
	delete(cm.data, key)
	return true
}
