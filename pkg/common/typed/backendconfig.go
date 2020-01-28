package typed

import (
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"

	"k8s.io/client-go/tools/cache"
)

// WrapBackendConfigStore wraps a generic store so the API is type-safe
func WrapBackendConfigStore(store cache.Store) *BackendConfigStore {
	return &BackendConfigStore{store: store}
}

// BackendConfigStore is a typed version of Store.
type BackendConfigStore struct {
	store cache.Store
}

// Add implements Store.
func (s *BackendConfigStore) Add(b *backendconfigv1.BackendConfig) error { return s.store.Add(b) }

// Update implements Store.
func (s *BackendConfigStore) Update(b *backendconfigv1.BackendConfig) error {
	return s.store.Update(b)
}

// Delete implements Store.
func (s *BackendConfigStore) Delete(b *backendconfigv1.BackendConfig) error {
	return s.store.Delete(b)
}

// List implements Store.
func (s *BackendConfigStore) List() []*backendconfigv1.BackendConfig {
	var ret []*backendconfigv1.BackendConfig
	for _, obj := range s.store.List() {
		ret = append(ret, obj.(*backendconfigv1.BackendConfig))
	}
	return ret
}

// ListKeys implements Store.
func (s *BackendConfigStore) ListKeys() []string { return s.store.ListKeys() }

// Get implements Store.
func (s *BackendConfigStore) Get(b *backendconfigv1.BackendConfig) (*backendconfigv1.BackendConfig, bool, error) {
	item, exists, err := s.store.Get(b)
	if item == nil {
		return nil, exists, err
	}
	return item.(*backendconfigv1.BackendConfig), exists, err
}

// GetByKey implements Store.
func (s *BackendConfigStore) GetByKey(key string) (*backendconfigv1.BackendConfig, bool, error) {
	item, exists, err := s.store.GetByKey(key)
	if item == nil {
		return nil, exists, err
	}
	return item.(*backendconfigv1.BackendConfig), exists, err
}

// Resync implements Store.
func (s *BackendConfigStore) Resync() error { return s.store.Resync() }

// This function is mostly likely not useful for ordinary consumers.
// func (s *BackendConfigStore) Replace(items []*backendconfigv1.BackendConfig, string) error {}
