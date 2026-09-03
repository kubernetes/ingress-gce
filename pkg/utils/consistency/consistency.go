package consistency

import (
	"fmt"
	"strconv"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type LastSyncRVGetter interface {
	LastSyncResourceVersion() string
}

type ConsistencyError struct {
	NamespacedName types.NamespacedName
	Message        string
}

func (e *ConsistencyError) Error() string {
	return e.Message
}

type ConsistencyStore interface {
	EnsureReady(namespacedName types.NamespacedName) error
	WroteAt(namespacedName types.NamespacedName, uid types.UID, resource schema.GroupResource, resourceVersion string)
	Clear(namespacedName types.NamespacedName, uid types.UID)
}

type record struct {
	uid      types.UID
	rv       int64
	resource schema.GroupResource
}

type consistencyStore struct {
	mu      sync.RWMutex
	getters map[schema.GroupResource]LastSyncRVGetter
	records map[types.NamespacedName]record
}

func NewConsistencyStore(getters map[schema.GroupResource]LastSyncRVGetter) ConsistencyStore {
	return &consistencyStore{
		getters: getters,
		records: make(map[types.NamespacedName]record),
	}
}

func (c *consistencyStore) EnsureReady(namespacedName types.NamespacedName) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rec, ok := c.records[namespacedName]
	if !ok {
		return nil
	}

	getter, ok := c.getters[rec.resource]
	if !ok {
		return nil // if no getter is configured, we can't stall.
	}

	lastSyncRVStr := getter.LastSyncResourceVersion()
	if lastSyncRVStr == "" {
		return &ConsistencyError{
			NamespacedName: namespacedName,
			Message:        fmt.Sprintf("informer %v has empty LastSyncResourceVersion", rec.resource),
		}
	}

	lastSyncRV, err := strconv.ParseInt(lastSyncRVStr, 10, 64)
	if err != nil {
		// RV is not a number, can't track stall by number, maybe skip
		return nil
	}

	if lastSyncRV < rec.rv {
		return &ConsistencyError{
			NamespacedName: namespacedName,
			Message:        fmt.Sprintf("cache stale for %v: expected RV %v but cache is at %v", rec.resource, rec.rv, lastSyncRV),
		}
	}

	return nil
}

func (c *consistencyStore) WroteAt(namespacedName types.NamespacedName, uid types.UID, resource schema.GroupResource, resourceVersion string) {
	rv, err := strconv.ParseInt(resourceVersion, 10, 64)
	if err != nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// If uid changes, it's a re-creation. We overwrite.
	c.records[namespacedName] = record{
		uid:      uid,
		rv:       rv,
		resource: resource,
	}
}

func (c *consistencyStore) Clear(namespacedName types.NamespacedName, uid types.UID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if rec, ok := c.records[namespacedName]; ok {
		if string(uid) == "" || rec.uid == uid {
			delete(c.records, namespacedName)
		}
	}
}

type noopConsistencyStore struct{}

func (n *noopConsistencyStore) EnsureReady(namespacedName types.NamespacedName) error { return nil }
func (n *noopConsistencyStore) WroteAt(namespacedName types.NamespacedName, uid types.UID, resource schema.GroupResource, resourceVersion string) {
}
func (n *noopConsistencyStore) Clear(namespacedName types.NamespacedName, uid types.UID) {}
func NewNoopConsistencyStore() ConsistencyStore                                          { return &noopConsistencyStore{} }
