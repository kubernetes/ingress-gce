package typed

import (
	api_v1 "k8s.io/api/core/v1"

	"k8s.io/client-go/tools/cache"
)

// WrapServiceStore wraps a generic store so the API is type-safe.
func WrapServiceStore(store cache.Store) *ServiceStore {
	return &ServiceStore{store: store}
}

// ServiceStore is a typed version of Store.
type ServiceStore struct {
	store cache.Store
}

// Add implements Store.
func (s *ServiceStore) Add(svc *api_v1.Service) error { return s.store.Add(svc) }

// Update implements Store.
func (s *ServiceStore) Update(svc *api_v1.Service) error { return s.store.Update(svc) }

// Delete implements Store.
func (s *ServiceStore) Delete(svc *api_v1.Service) error { return s.store.Delete(svc) }

// List implements Store.
func (s *ServiceStore) List() []*api_v1.Service {
	var ret []*api_v1.Service
	for _, obj := range s.store.List() {
		ret = append(ret, obj.(*api_v1.Service))
	}
	return ret
}

// ListKeys implements Store.
func (s *ServiceStore) ListKeys() []string { return s.store.ListKeys() }

// Get implements Store.
func (s *ServiceStore) Get(c *api_v1.Service) (*api_v1.Service, bool, error) {
	item, exists, err := s.store.Get(c)
	if item == nil {
		return nil, exists, err
	}
	return item.(*api_v1.Service), exists, err
}

// GetByKey implements Store.
func (s *ServiceStore) GetByKey(key string) (*api_v1.Service, bool, error) {
	item, exists, err := s.store.GetByKey(key)
	if item == nil {
		return nil, exists, err
	}
	return item.(*api_v1.Service), exists, err
}

// Resync implements Store.
func (s *ServiceStore) Resync() error { return s.store.Resync() }

// This function is mostly likely not useful for ordinary consumers.
// func (s *ServiceStore) Replace(items []*api_v1.Service, string) error {}
