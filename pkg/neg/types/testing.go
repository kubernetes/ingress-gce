/*
Copyright 2020 The Kubernetes Authors.

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
	"time"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	informerv1 "k8s.io/client-go/informers/core/v1"
	informernetworking "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	negfake "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned/fake"
	informersvcneg "k8s.io/ingress-gce/pkg/svcneg/client/informers/externalversions/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	namespace     = apiv1.NamespaceAll
	resyncPeriod  = 1 * time.Second
	kubeSystemUID = "kube-system-uid"
	clusterID     = "clusterid"
)

// TestContext provides controller context for testing
type TestContext struct {
	KubeClient   kubernetes.Interface
	SvcNegClient svcnegclient.Interface
	Cloud        *gce.Cloud

	NegNamer NetworkEndpointGroupNamer
	L4Namer  namer.L4ResourcesNamer

	IngressInformer  cache.SharedIndexInformer
	PodInformer      cache.SharedIndexInformer
	ServiceInformer  cache.SharedIndexInformer
	NodeInformer     cache.SharedIndexInformer
	EndpointInformer cache.SharedIndexInformer
	SvcNegInformer   cache.SharedIndexInformer

	KubeSystemUID types.UID
	ResyncPeriod  time.Duration
}

func NewTestContext() *TestContext {
	kubeClient := fake.NewSimpleClientset()
	return NewTestContextWithKubeClient(kubeClient)
}

func NewTestContextWithKubeClient(kubeClient kubernetes.Interface) *TestContext {
	negClient := negfake.NewSimpleClientset()
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	MockNetworkEndpointAPIs(fakeGCE)

	clusterNamer := namer.NewNamer(clusterID, "")
	l4namer := namer.NewL4Namer(kubeSystemUID, clusterNamer)

	return &TestContext{
		KubeClient:       kubeClient,
		SvcNegClient:     negClient,
		Cloud:            fakeGCE,
		NegNamer:         clusterNamer,
		L4Namer:          l4namer,
		IngressInformer:  informernetworking.NewIngressInformer(kubeClient, namespace, resyncPeriod, utils.NewNamespaceIndexer()),
		PodInformer:      informerv1.NewPodInformer(kubeClient, namespace, resyncPeriod, utils.NewNamespaceIndexer()),
		ServiceInformer:  informerv1.NewServiceInformer(kubeClient, namespace, resyncPeriod, utils.NewNamespaceIndexer()),
		EndpointInformer: informerv1.NewEndpointsInformer(kubeClient, namespace, resyncPeriod, utils.NewNamespaceIndexer()),
		NodeInformer:     informerv1.NewNodeInformer(kubeClient, resyncPeriod, utils.NewNamespaceIndexer()),
		SvcNegInformer:   informersvcneg.NewServiceNetworkEndpointGroupInformer(negClient, namespace, resyncPeriod, utils.NewNamespaceIndexer()),
		KubeSystemUID:    kubeSystemUID,
		ResyncPeriod:     resyncPeriod,
	}
}
