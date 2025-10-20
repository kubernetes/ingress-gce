package framework

import "sync"

// controllerSet holds controller-specific resources for a ProviderConfig.
// It contains the stop channel used to signal controller shutdown.
type controllerSet struct {
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

// Delete removes the controllerSet for the given key.
func (cm *ControllerMap) Delete(key string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.data, key)
}
