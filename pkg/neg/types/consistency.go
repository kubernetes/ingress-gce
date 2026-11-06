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

package types

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/flags"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	networkingv1beta1 "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned/typed/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/utils/consistency"
	"k8s.io/klog/v2"
)

// SvcNegGroupResource identifies ServiceNetworkEndpointGroup CRs in the
// ConsistencyStore. It is derived from the generated scheme so it cannot
// drift from the group the client actually writes to.
var SvcNegGroupResource = negv1beta1.SchemeGroupVersion.WithResource("servicenetworkendpointgroups").GroupResource()

// SvcNegRef returns the ConsistencyStore reference for a
// ServiceNetworkEndpointGroup CR.
func SvcNegRef(namespace, name string) consistency.ObjectRef {
	return consistency.ObjectRef{
		Resource:       SvcNegGroupResource,
		NamespacedName: types.NamespacedName{Namespace: namespace, Name: name},
	}
}

// RecordSvcNegWrite records in the ConsistencyStore that the controller wrote
// a ServiceNetworkEndpointGroup CR and observed written in the API server's
// response. A nil CR is ignored so callers can pass a failed write's result
// directly.
func RecordSvcNegWrite(store consistency.ConsistencyStore, written *negv1beta1.ServiceNetworkEndpointGroup) {
	if written == nil {
		return
	}
	store.WroteAt(SvcNegRef(written.Namespace, written.Name), written.UID, written.ResourceVersion)
}

// NewRecordingSvcNegClient wraps a SvcNeg client so that every write it
// performs (create, update, status update, patch) is recorded in the
// ConsistencyStore. Recording at the client rather than at each call site
// means a write path added later cannot forget to record and silently
// reintroduce the staleness the store exists to catch. Reads pass through
// unchanged; deletes are not recorded because their records are cleared when
// the informer observes the deletion.
func NewRecordingSvcNegClient(client svcnegclient.Interface, store consistency.ConsistencyStore) svcnegclient.Interface {
	return &recordingSvcNegClient{Interface: client, store: store}
}

type recordingSvcNegClient struct {
	svcnegclient.Interface
	store consistency.ConsistencyStore
}

func (c *recordingSvcNegClient) NetworkingV1beta1() networkingv1beta1.NetworkingV1beta1Interface {
	return &recordingNetworkingV1beta1{NetworkingV1beta1Interface: c.Interface.NetworkingV1beta1(), store: c.store}
}

type recordingNetworkingV1beta1 struct {
	networkingv1beta1.NetworkingV1beta1Interface
	store consistency.ConsistencyStore
}

func (n *recordingNetworkingV1beta1) ServiceNetworkEndpointGroups(namespace string) networkingv1beta1.ServiceNetworkEndpointGroupInterface {
	return &recordingSvcNegs{ServiceNetworkEndpointGroupInterface: n.NetworkingV1beta1Interface.ServiceNetworkEndpointGroups(namespace), store: n.store}
}

type recordingSvcNegs struct {
	networkingv1beta1.ServiceNetworkEndpointGroupInterface
	store consistency.ConsistencyStore
}

func (s *recordingSvcNegs) Create(ctx context.Context, cr *negv1beta1.ServiceNetworkEndpointGroup, opts metav1.CreateOptions) (*negv1beta1.ServiceNetworkEndpointGroup, error) {
	written, err := s.ServiceNetworkEndpointGroupInterface.Create(ctx, cr, opts)
	RecordSvcNegWrite(s.store, written)
	return written, err
}

func (s *recordingSvcNegs) Update(ctx context.Context, cr *negv1beta1.ServiceNetworkEndpointGroup, opts metav1.UpdateOptions) (*negv1beta1.ServiceNetworkEndpointGroup, error) {
	written, err := s.ServiceNetworkEndpointGroupInterface.Update(ctx, cr, opts)
	RecordSvcNegWrite(s.store, written)
	return written, err
}

func (s *recordingSvcNegs) UpdateStatus(ctx context.Context, cr *negv1beta1.ServiceNetworkEndpointGroup, opts metav1.UpdateOptions) (*negv1beta1.ServiceNetworkEndpointGroup, error) {
	written, err := s.ServiceNetworkEndpointGroupInterface.UpdateStatus(ctx, cr, opts)
	RecordSvcNegWrite(s.store, written)
	return written, err
}

func (s *recordingSvcNegs) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*negv1beta1.ServiceNetworkEndpointGroup, error) {
	written, err := s.ServiceNetworkEndpointGroupInterface.Patch(ctx, name, pt, data, opts, subresources...)
	RecordSvcNegWrite(s.store, written)
	return written, err
}

// NewSvcNegConsistencyStore returns the ConsistencyStore for a NEG
// controller, backed by the given SvcNeg informer. It returns a no-op store
// when the feature is disabled by flag, no informer is available, or the
// informer cannot report its last synced ResourceVersion; callers detect the
// no-op store with consistency.IsNoop and keep their older mitigations then.
func NewSvcNegConsistencyStore(svcNegInformer cache.SharedIndexInformer, logger klog.Logger) consistency.ConsistencyStore {
	if !flags.F.EnableConsistencyStore {
		return consistency.NewNoopConsistencyStore()
	}
	if svcNegInformer == nil {
		logger.Error(nil, "SvcNeg informer is not configured, running the NEG controller without the consistency store")
		return consistency.NewNoopConsistencyStore()
	}
	getter, ok := svcNegInformer.(consistency.LastSyncRVGetter)
	if !ok {
		logger.Error(nil, "SvcNeg informer does not implement LastSyncResourceVersion, running the NEG controller without the consistency store")
		return consistency.NewNoopConsistencyStore()
	}
	logger.V(0).Info("NEG controller will use the SvcNeg consistency store")
	return consistency.NewConsistencyStore(map[schema.GroupResource]consistency.LastSyncRVGetter{
		SvcNegGroupResource: getter,
	})
}
