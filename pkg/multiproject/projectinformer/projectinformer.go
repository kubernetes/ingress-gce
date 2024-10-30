package projectinformer

import (
	"time"

	"k8s.io/client-go/tools/cache"
)

// Informer wraps a SharedIndexInformer to provide a project filtered view.
type ProjectInformer struct {
	cache.SharedIndexInformer
	projectName string
}

// NewInformer creates a new Informer.
func NewProjectInformer(informer cache.SharedIndexInformer, projectName string) cache.SharedIndexInformer {
	return &ProjectInformer{
		SharedIndexInformer: informer,
		projectName:         projectName,
	}
}

// AddEventHandler adds an event handler that only processes events for the specified project.
func (i *ProjectInformer) AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	return i.SharedIndexInformer.AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: i.projectFilter,
			Handler:    handler,
		},
	)
}

// AddEventHandlerWithResyncPeriod adds an event handler with resync period.
func (i *ProjectInformer) AddEventHandlerWithResyncPeriod(handler cache.ResourceEventHandler, resyncPeriod time.Duration) (cache.ResourceEventHandlerRegistration, error) {
	return i.SharedIndexInformer.AddEventHandlerWithResyncPeriod(
		cache.FilteringResourceEventHandler{
			FilterFunc: i.projectFilter,
			Handler:    handler,
		},
		resyncPeriod,
	)
}

// projectFilter filters objects based on the project.
func (i *ProjectInformer) projectFilter(obj interface{}) bool {
	return isObjectInProject(obj, i.projectName)
}

func (i *ProjectInformer) GetStore() cache.Store {
	return &projectCache{
		Indexer:     i.SharedIndexInformer.GetIndexer(),
		projectName: i.projectName,
	}
}

func (i *ProjectInformer) GetIndexer() cache.Indexer {
	return &projectCache{
		Indexer:     i.SharedIndexInformer.GetIndexer(),
		projectName: i.projectName,
	}
}
