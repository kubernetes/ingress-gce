package filteredinformer

import (
	"time"

	"k8s.io/client-go/tools/cache"
)

// ProviderConfigFilteredInformer wraps a SharedIndexInformer to provide a ProviderConfig filtered view.
type ProviderConfigFilteredInformer struct {
	cache.SharedIndexInformer
	providerConfigName string
	allowMissing       bool
}

// NewProviderConfigFilteredInformer creates a new ProviderConfigFilteredInformer.
func NewProviderConfigFilteredInformer(informer cache.SharedIndexInformer, providerConfigName string, allowMissing bool) cache.SharedIndexInformer {
	return &ProviderConfigFilteredInformer{
		SharedIndexInformer: informer,
		providerConfigName:  providerConfigName,
		allowMissing:        allowMissing,
	}
}

// AddEventHandler adds an event handler that only processes events for the specified ProviderConfig.
func (i *ProviderConfigFilteredInformer) AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	return i.SharedIndexInformer.AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: i.providerConfigFilter,
			Handler:    handler,
		},
	)
}

// AddEventHandlerWithResyncPeriod adds an event handler with resync period.
func (i *ProviderConfigFilteredInformer) AddEventHandlerWithResyncPeriod(handler cache.ResourceEventHandler, resyncPeriod time.Duration) (cache.ResourceEventHandlerRegistration, error) {
	return i.SharedIndexInformer.AddEventHandlerWithResyncPeriod(
		cache.FilteringResourceEventHandler{
			FilterFunc: i.providerConfigFilter,
			Handler:    handler,
		},
		resyncPeriod,
	)
}

// providerConfigFilter filters objects based on the provider config.
func (i *ProviderConfigFilteredInformer) providerConfigFilter(obj interface{}) bool {
	return isObjectInProviderConfig(obj, i.providerConfigName, i.allowMissing)
}

func (i *ProviderConfigFilteredInformer) GetStore() cache.Store {
	return &providerConfigFilteredCache{
		Indexer:            i.SharedIndexInformer.GetIndexer(),
		providerConfigName: i.providerConfigName,
		allowMissing:       i.allowMissing,
	}
}

func (i *ProviderConfigFilteredInformer) GetIndexer() cache.Indexer {
	return &providerConfigFilteredCache{
		Indexer:            i.SharedIndexInformer.GetIndexer(),
		providerConfigName: i.providerConfigName,
		allowMissing:       i.allowMissing,
	}
}
