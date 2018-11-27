package typed

import (
	frontendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"

	"k8s.io/client-go/tools/cache"
)

// WrapFrontendConfigStore wraps a generic store so the API is type-safe
func WrapFrontendConfigStore(store cache.Store) *FrontendConfigStore {
	return &FrontendConfigStore{store: store}
}

// FrontendConfigStore is a typed version of Store.
type FrontendConfigStore struct {
	store cache.Store
}

// Add implements Store.
func (s *FrontendConfigStore) Add(b *frontendconfigv1beta1.FrontendConfig) error {
	return s.store.Add(b)
}

// Update implements Store.
func (s *FrontendConfigStore) Update(b *frontendconfigv1beta1.FrontendConfig) error {
	return s.store.Update(b)
}

// Delete implements Store.
func (s *FrontendConfigStore) Delete(b *frontendconfigv1beta1.FrontendConfig) error {
	return s.store.Delete(b)
}

// List implements Store.
func (s *FrontendConfigStore) List() []*frontendconfigv1beta1.FrontendConfig {
	var ret []*frontendconfigv1beta1.FrontendConfig
	for _, obj := range s.store.List() {
		ret = append(ret, obj.(*frontendconfigv1beta1.FrontendConfig))
	}
	return ret
}

// ListKeys implements Store.
func (s *FrontendConfigStore) ListKeys() []string { return s.store.ListKeys() }

// Get implements Store.
func (s *FrontendConfigStore) Get(b *frontendconfigv1beta1.FrontendConfig) (*frontendconfigv1beta1.FrontendConfig, bool, error) {
	item, exists, err := s.store.Get(b)
	if item == nil {
		return nil, exists, err
	}
	return item.(*frontendconfigv1beta1.FrontendConfig), exists, err
}

// GetByKey implements Store.
func (s *FrontendConfigStore) GetByKey(key string) (*frontendconfigv1beta1.FrontendConfig, bool, error) {
	item, exists, err := s.store.GetByKey(key)
	if item == nil {
		return nil, exists, err
	}
	return item.(*frontendconfigv1beta1.FrontendConfig), exists, err
}

// Resync implements Store.
func (s *FrontendConfigStore) Resync() error { return s.store.Resync() }

// This function is mostly likely not useful for ordinary consumers.
// func (s *FrontendConfigStore) Replace(items []*frontendconfigv1beta1.FrontendConfig, string) error {}
