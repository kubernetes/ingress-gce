package typed

import (
	extensions "k8s.io/api/extensions/v1beta1"

	"k8s.io/client-go/tools/cache"
)

// WrapIngressStore wraps a generic store so the API is type-safe.
func WrapIngressStore(store cache.Store) *IngressStore {
	return &IngressStore{store: store}
}

// IngressStore is a typed version of Store.
type IngressStore struct {
	store cache.Store
}

// Add implements Store.
func (s *IngressStore) Add(i *extensions.Ingress) error { return s.store.Add(i) }

// Update implements Store.
func (s *IngressStore) Update(i *extensions.Ingress) error { return s.store.Update(i) }

// Delete implements Store.
func (s *IngressStore) Delete(i *extensions.Ingress) error { return s.store.Delete(i) }

// List implements Store.
func (s *IngressStore) List() []*extensions.Ingress {
	var ret []*extensions.Ingress
	for _, obj := range s.store.List() {
		ret = append(ret, obj.(*extensions.Ingress))
	}
	return ret
}

// ListKeys implements Store.
func (s *IngressStore) ListKeys() []string { return s.store.ListKeys() }

// Get implements Store.
func (s *IngressStore) Get(i *extensions.Ingress) (*extensions.Ingress, bool, error) {
	item, exists, err := s.store.Get(i)
	if item == nil {
		return nil, exists, err
	}
	return item.(*extensions.Ingress), exists, err
}

// GetByKey implements Store.
func (s *IngressStore) GetByKey(key string) (*extensions.Ingress, bool, error) {
	item, exists, err := s.store.GetByKey(key)
	if item == nil {
		return nil, exists, err
	}
	return item.(*extensions.Ingress), exists, err
}

// Resync implements Store.
func (s *IngressStore) Resync() error { return s.store.Resync() }

// This function is mostly likely not useful for ordinary consumers.
// func (s *IngressStore) Replace(items []*extensions.Ingress, string) error {}
