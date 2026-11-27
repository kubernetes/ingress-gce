/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package serviceconsistency provides the ConsistencyStore of the services
// managed by the L4 controllers, and the helpers that record the controllers'
// writes to those services in it.
package serviceconsistency

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/utils/consistency"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/klog/v2"
)

// GroupResource identifies core v1 Services in the ConsistencyStore.
var GroupResource = schema.GroupResource{Resource: "services"}

// NewStore returns the ConsistencyStore shared by the L4 controllers. A
// single store is shared because they mutate the same services through the
// same informer, so each controller also waits for the others' writes to
// become visible before reconciling.
//
// Sharing the store does not serialize the controllers. Each has its own
// queue, so one can start syncing a service while another's write to it is
// still in flight and not yet recorded. That sync reconciles from the cache
// as it would without the store: service patches carry no resourceVersion
// precondition.
//
// The store checks recorded writes against the service informer's store, and
// drops the record of a service when the informer observes that it is gone,
// so that records do not leak or stall a service recreated under the same
// name. It is a no-op store when enabled is false or informer stores do not
// track the ResourceVersion they have applied.
func NewStore(serviceInformer cache.SharedIndexInformer, enabled bool, logger klog.Logger) consistency.ConsistencyStore {
	if !enabled {
		return consistency.NewNoopConsistencyStore()
	}
	if !consistency.Supported() {
		logger.Error(nil, "Informer stores do not track their ResourceVersion (client-go AtomicFIFO feature gate is disabled), running L4 controllers without the consistency store")
		return consistency.NewNoopConsistencyStore()
	}
	store := consistency.NewConsistencyStore(map[schema.GroupResource]consistency.LastSyncRVGetter{
		GroupResource: serviceInformer.GetIndexer(),
	}, logger)
	serviceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			clearReplacedRecord(store, oldObj, newObj)
		},
		DeleteFunc: func(obj interface{}) {
			clearRecord(store, obj)
		},
	})
	logger.V(0).Info("L4 controllers will use the service consistency store")
	return store
}

// Ref returns the ConsistencyStore reference for a service.
func Ref(namespace, name string) consistency.ObjectRef {
	return consistency.ObjectRef{
		Resource:       GroupResource,
		NamespacedName: types.NamespacedName{Namespace: namespace, Name: name},
	}
}

// clearRecord drops the ConsistencyStore record of the service carried by an
// informer delete event, unwrapping tombstones. The UID keeps a deletion
// event for an old incarnation from dropping the record of a service that has
// been recreated since.
func clearRecord(store consistency.ConsistencyStore, obj interface{}) {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}
	svc, ok := obj.(*v1.Service)
	if !ok {
		return
	}
	store.Clear(Ref(svc.Namespace, svc.Name), svc.UID)
}

// clearReplacedRecord drops the ConsistencyStore record of a service that was
// deleted and recreated under the same name without the informer observing
// the deletion. After a relist the informer reports that as an update whose
// old and new objects have different UIDs, not as a delete event.
func clearReplacedRecord(store consistency.ConsistencyStore, oldObj, newObj interface{}) {
	oldSvc, ok := oldObj.(*v1.Service)
	if !ok {
		return
	}
	newSvc, ok := newObj.(*v1.Service)
	if !ok || oldSvc.UID == newSvc.UID {
		return
	}
	store.Clear(Ref(oldSvc.Namespace, oldSvc.Name), oldSvc.UID)
}

// RecordWrite records in the ConsistencyStore that a controller wrote a
// service and received writtenSvc in the API server's response, so that the
// next sync of the service waits until the informer has observed it.
func RecordWrite(store consistency.ConsistencyStore, writtenSvc *v1.Service) {
	if writtenSvc == nil {
		return
	}
	store.WroteAt(Ref(writtenSvc.Namespace, writtenSvc.Name), writtenSvc.UID, writtenSvc.ResourceVersion)
}

// EnsureCacheReady returns a *consistency.ConsistencyError while the service
// informer has not yet observed the last recorded write to the service with
// the given queue key. A sync returns that error to have the key requeued
// instead of reconciling from a stale cache.
func EnsureCacheReady(store consistency.ConsistencyStore, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// Not a namespace/name key, so no write was recorded for it.
		return nil
	}
	return store.EnsureReady(Ref(namespace, name))
}

// PatchMetadata patches the metadata of svc and records the write in the
// ConsistencyStore.
func PatchMetadata(client coreclient.CoreV1Interface, store consistency.ConsistencyStore, svc *v1.Service, newObjectMeta metav1.ObjectMeta) error {
	patchedSvc, err := patch.PatchServiceObjectMetadata(client, svc, newObjectMeta)
	if err != nil {
		return err
	}
	RecordWrite(store, patchedSvc)
	return nil
}
