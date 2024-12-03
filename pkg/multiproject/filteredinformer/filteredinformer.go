package filteredinformer

import (
	"time"

	"k8s.io/client-go/tools/cache"
)

// ClusterSliceFilteredInformer wraps a SharedIndexInformer to provide a project filtered view.
type ClusterSliceFilteredInformer struct {
	cache.SharedIndexInformer
	clusterSliceName string
}

// NewClusterSliceFilteredInformer creates a new ClusterSliceFilteredInformer.
func NewClusterSliceFilteredInformer(informer cache.SharedIndexInformer, clusterSliceName string) cache.SharedIndexInformer {
	return &ClusterSliceFilteredInformer{
		SharedIndexInformer: informer,
		clusterSliceName:    clusterSliceName,
	}
}

// AddEventHandler adds an event handler that only processes events for the specified project.
func (i *ClusterSliceFilteredInformer) AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	return i.SharedIndexInformer.AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: i.clusterSliceFilter,
			Handler:    handler,
		},
	)
}

// AddEventHandlerWithResyncPeriod adds an event handler with resync period.
func (i *ClusterSliceFilteredInformer) AddEventHandlerWithResyncPeriod(handler cache.ResourceEventHandler, resyncPeriod time.Duration) (cache.ResourceEventHandlerRegistration, error) {
	return i.SharedIndexInformer.AddEventHandlerWithResyncPeriod(
		cache.FilteringResourceEventHandler{
			FilterFunc: i.clusterSliceFilter,
			Handler:    handler,
		},
		resyncPeriod,
	)
}

// clusterSliceFilter filters objects based on the cluster slice.
func (i *ClusterSliceFilteredInformer) clusterSliceFilter(obj interface{}) bool {
	return isObjectInClusterSlice(obj, i.clusterSliceName)
}

func (i *ClusterSliceFilteredInformer) GetStore() cache.Store {
	return &clusterSliceFilteredCache{
		Indexer:          i.SharedIndexInformer.GetIndexer(),
		clusterSliceName: i.clusterSliceName,
	}
}

func (i *ClusterSliceFilteredInformer) GetIndexer() cache.Indexer {
	return &clusterSliceFilteredCache{
		Indexer:          i.SharedIndexInformer.GetIndexer(),
		clusterSliceName: i.clusterSliceName,
	}
}
