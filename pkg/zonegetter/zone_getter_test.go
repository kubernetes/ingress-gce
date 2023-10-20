/*
Copyright 2023 The Kubernetes Authors.

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

package zonegetter

import (
	"testing"
	"time"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	informerv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils"
)

func fakeZoneGetter() *ZoneGetter {
	client := fake.NewSimpleClientset()
	resyncPeriod := 1 * time.Second
	nodeInformer := informerv1.NewNodeInformer(client, resyncPeriod, utils.NewNamespaceIndexer())
	return &ZoneGetter{
		NodeInformer: nodeInformer,
	}
}

func TestGetZoneForNode(t *testing.T) {
	nodeName := "node"
	zone := "us-central1-a"
	zoneGetter := fakeZoneGetter()
	zoneGetter.NodeInformer.GetIndexer().Add(&apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
			Name:      nodeName,
			Labels: map[string]string{
				annotations.ZoneKey: zone,
			},
		},
		Spec: apiv1.NodeSpec{
			Unschedulable: false,
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionFalse,
				},
			},
		},
	})

	ret, err := zoneGetter.GetZoneForNode(nodeName)
	if err != nil {
		t.Errorf("Expect error = nil, but got %v", err)
	}

	if zone != ret {
		t.Errorf("Expect zone = %q, but got %q", zone, ret)
	}
}
