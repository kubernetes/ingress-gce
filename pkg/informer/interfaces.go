/*
Copyright 2018 The Kubernetes Authors.

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

package informer

import "k8s.io/client-go/tools/cache"

// ClusterInformerManager is an interface to manage the creation and deletion
// of SharedIndexInformer's for Ingresses and Services in a cluster.
type ClusterInformerManager interface {
	// CreateInformer creates informers for the cluster.
	CreateInformers()
	// DeleteInformer stops all informers for the cluster.
	DeleteInformers()
	// HasSynced returns true if all informers are synced.
	HasSynced() bool
	// AddHandlersForInformer adds CRUD handlers for an informer.
	AddHandlersForInformer(informerType string, handlers cache.ResourceEventHandlerFuncs)
	// GetInformers returns the informers.
	Informers() InformerPair
}
