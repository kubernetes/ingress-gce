package namespacedinformer

import (
	"time"

	"k8s.io/client-go/tools/cache"
)

// Informer wraps a SharedIndexInformer to provide a namespaced view.
type NamespacedInformer struct {
	cache.SharedIndexInformer
	namespace string
}

// NewInformer creates a new Informer.
func NewNamespacedInformer(informer cache.SharedIndexInformer, namespace string) cache.SharedIndexInformer {
	return &NamespacedInformer{
		SharedIndexInformer: informer,
		namespace:           namespace,
	}
}

// AddEventHandler adds an event handler that only processes events for the specified namespace.
func (i *NamespacedInformer) AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	return i.SharedIndexInformer.AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: i.namespaceFilter,
			Handler:    handler,
		},
	)
}

// AddEventHandlerWithResyncPeriod adds an event handler with resync period.
func (i *NamespacedInformer) AddEventHandlerWithResyncPeriod(handler cache.ResourceEventHandler, resyncPeriod time.Duration) (cache.ResourceEventHandlerRegistration, error) {
	return i.SharedIndexInformer.AddEventHandlerWithResyncPeriod(
		cache.FilteringResourceEventHandler{
			FilterFunc: i.namespaceFilter,
			Handler:    handler,
		},
		resyncPeriod,
	)
}

// namespaceFilter filters objects based on the namespace.
func (i *NamespacedInformer) namespaceFilter(obj interface{}) bool {
	return isObjectInNamespace(obj, i.namespace)
}

func (i *NamespacedInformer) GetStore() cache.Store {
	return &namespacedCache{
		Indexer:   i.SharedIndexInformer.GetIndexer(),
		namespace: i.namespace,
	}
}

func (i *NamespacedInformer) GetIndexer() cache.Indexer {
	return &namespacedCache{
		Indexer:   i.SharedIndexInformer.GetIndexer(),
		namespace: i.namespace,
	}
}
