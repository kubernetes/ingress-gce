package typed

import (
	v1 "k8s.io/api/networking/v1"
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
func (s *IngressStore) Add(i *v1.Ingress) error { return s.store.Add(i) }

// Update implements Store.
func (s *IngressStore) Update(i *v1.Ingress) error { return s.store.Update(i) }

// Delete implements Store.
func (s *IngressStore) Delete(i *v1.Ingress) error { return s.store.Delete(i) }

// List implements Store.
func (s *IngressStore) List() []*v1.Ingress {
	var ret []*v1.Ingress
	for _, obj := range s.store.List() {
		ret = append(ret, obj.(*v1.Ingress))
	}
	return ret
}

// ListKeys implements Store.
func (s *IngressStore) ListKeys() []string { return s.store.ListKeys() }

// Get implements Store.
func (s *IngressStore) Get(i *v1.Ingress) (*v1.Ingress, bool, error) {
	item, exists, err := s.store.Get(i)
	if item == nil {
		return nil, exists, err
	}
	return item.(*v1.Ingress), exists, err
}

// GetByKey implements Store.
func (s *IngressStore) GetByKey(key string) (*v1.Ingress, bool, error) {
	item, exists, err := s.store.GetByKey(key)
	if item == nil {
		return nil, exists, err
	}
	return item.(*v1.Ingress), exists, err
}

// Resync implements Store.
func (s *IngressStore) Resync() error { return s.store.Resync() }

// This function is mostly likely not useful for ordinary consumers.
// func (s *IngressStore) Replace(items []*v1.Ingress, string) error {}
