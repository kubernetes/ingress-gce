/*
Copyright 2015 The Kubernetes Authors.

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

package controller

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/golang/glog"

	apiv1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	unversionedcore "k8s.io/client-go/kubernetes/typed/core/v1"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller/translator"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/tls"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	// DefaultFirewallName is the default firewall name.
	DefaultFirewallName = ""
	// Frequency to poll on local stores to sync.
	storeSyncPollPeriod = 5 * time.Second
)

var (
	keyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc
	// DefaultFirewallName is the name to user for firewall rules created
	// by an L7 controller when the --fireall-rule is not used.
)

// LoadBalancerController watches the kubernetes api and adds/removes services
// from the loadbalancer, via loadBalancerConfig.
type LoadBalancerController struct {
	client kubernetes.Interface
	ctx    *context.ControllerContext

	ingLister  StoreToIngressLister
	nodeLister cache.Indexer
	nodes      *NodeController
	// endpoint lister is needed when translating service target port to real endpoint target ports.
	endpointLister StoreToEndpointLister

	// TODO: Watch secrets
	CloudClusterManager *ClusterManager
	ingQueue            utils.TaskQueue
	Translator          *translator.GCE
	stopCh              chan struct{}
	// stopLock is used to enforce only a single call to Stop is active.
	// Needed because we allow stopping through an http endpoint and
	// allowing concurrent stoppers leads to stack traces.
	stopLock sync.Mutex
	shutdown bool
	// tlsLoader loads secrets from the Kubernetes apiserver for Ingresses.
	tlsLoader tls.TlsLoader
	// hasSynced returns true if all associated sub-controllers have synced.
	// Abstracted into a func for testing.
	hasSynced func() bool
	// negEnabled indicates whether NEG feature is enabled.
	negEnabled bool
}

// NewLoadBalancerController creates a controller for gce loadbalancers.
// - kubeClient: A kubernetes REST client.
// - clusterManager: A ClusterManager capable of creating all cloud resources
//	 required for L7 loadbalancing.
// - resyncPeriod: Watchers relist from the Kubernetes API server this often.
func NewLoadBalancerController(kubeClient kubernetes.Interface, stopCh chan struct{}, ctx *context.ControllerContext, clusterManager *ClusterManager, negEnabled bool) (*LoadBalancerController, error) {
	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(glog.Infof)
	broadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{
		Interface: kubeClient.Core().Events(""),
	})
	lbc := LoadBalancerController{
		client:              kubeClient,
		ctx:                 ctx,
		ingLister:           StoreToIngressLister{Store: ctx.IngressInformer.GetStore()},
		nodeLister:          ctx.NodeInformer.GetIndexer(),
		nodes:               NewNodeController(ctx, clusterManager),
		CloudClusterManager: clusterManager,
		stopCh:              stopCh,
		hasSynced:           ctx.HasSynced,
		negEnabled:          negEnabled,
	}
	lbc.ingQueue = utils.NewPeriodicTaskQueue("ingresses", lbc.sync)

	if negEnabled {
		lbc.endpointLister.Indexer = ctx.EndpointInformer.GetIndexer()
	}

	// ingress event handler
	ctx.IngressInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addIng := obj.(*extensions.Ingress)
			if !isGCEIngress(addIng) && !isGCEMultiClusterIngress(addIng) {
				glog.V(4).Infof("Ignoring add for ingress %v based on annotation %v", addIng.Name, annotations.IngressClassKey)
				return
			}

			glog.V(3).Infof("Ingress %v/%v added, enqueuing", addIng.Namespace, addIng.Name)
			lbc.ctx.Recorder(addIng.Namespace).Eventf(addIng, apiv1.EventTypeNormal, "ADD", fmt.Sprintf("%s/%s", addIng.Namespace, addIng.Name))
			lbc.ingQueue.Enqueue(obj)
		},
		DeleteFunc: func(obj interface{}) {
			delIng := obj.(*extensions.Ingress)
			if !isGCEIngress(delIng) && !isGCEMultiClusterIngress(delIng) {
				glog.V(4).Infof("Ignoring delete for ingress %v based on annotation %v", delIng.Name, annotations.IngressClassKey)
				return
			}

			glog.V(3).Infof("Ingress %v/%v deleted, enqueueing", delIng.Namespace, delIng.Name)
			lbc.ingQueue.Enqueue(obj)
		},
		UpdateFunc: func(old, cur interface{}) {
			curIng := cur.(*extensions.Ingress)
			if !isGCEIngress(curIng) && !isGCEMultiClusterIngress(curIng) {
				return
			}
			if reflect.DeepEqual(old, cur) {
				glog.V(3).Infof("Periodic enqueueing of %v/%v", curIng.Namespace, curIng.Name)
			} else {
				glog.V(3).Infof("Ingress %v/%v changed, enqueuing", curIng.Namespace, curIng.Name)
			}

			lbc.ingQueue.Enqueue(cur)
		},
	})

	// service event handler
	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: lbc.enqueueIngressForService,
		UpdateFunc: func(old, cur interface{}) {
			if !reflect.DeepEqual(old, cur) {
				lbc.enqueueIngressForService(cur)
			}
		},
		// Ingress deletes matter, service deletes don't.
	})

	var endpointIndexer cache.Indexer
	if ctx.EndpointInformer != nil {
		endpointIndexer = ctx.EndpointInformer.GetIndexer()
	}
	lbc.Translator = translator.New(lbc.ctx, lbc.CloudClusterManager,
		ctx.ServiceInformer.GetIndexer(),
		ctx.NodeInformer.GetIndexer(),
		ctx.PodInformer.GetIndexer(),
		endpointIndexer,
		negEnabled)
	lbc.tlsLoader = &tls.TLSCertsFromSecretsLoader{Client: lbc.client}

	glog.V(3).Infof("Created new loadbalancer controller")

	return &lbc, nil
}

// enqueueIngressForService enqueues all the Ingress' for a Service.
func (lbc *LoadBalancerController) enqueueIngressForService(obj interface{}) {
	svc := obj.(*apiv1.Service)
	ings, err := lbc.ingLister.GetServiceIngress(svc)
	if err != nil {
		glog.V(5).Infof("ignoring service %v: %v", svc.Name, err)
		return
	}
	for _, ing := range ings {
		if !isGCEIngress(&ing) {
			continue
		}
		lbc.ingQueue.Enqueue(&ing)
	}
}

// Run starts the loadbalancer controller.
func (lbc *LoadBalancerController) Run() {
	glog.Infof("Starting loadbalancer controller")
	go lbc.ingQueue.Run(time.Second, lbc.stopCh)
	lbc.nodes.Run(lbc.stopCh)

	<-lbc.stopCh
	glog.Infof("Shutting down Loadbalancer Controller")
}

// Stop stops the loadbalancer controller. It also deletes cluster resources
// if deleteAll is true.
func (lbc *LoadBalancerController) Stop(deleteAll bool) error {
	// Stop is invoked from the http endpoint.
	lbc.stopLock.Lock()
	defer lbc.stopLock.Unlock()

	// Only try draining the workqueue if we haven't already.
	if !lbc.shutdown {
		close(lbc.stopCh)
		glog.Infof("Shutting down controller queues.")
		lbc.ingQueue.Shutdown()
		lbc.nodes.Shutdown()
		lbc.shutdown = true
	}

	// Deleting shared cluster resources is idempotent.
	if deleteAll {
		glog.Infof("Shutting down cluster manager.")
		return lbc.CloudClusterManager.shutdown()
	}
	return nil
}

// sync manages Ingress create/updates/deletes.
func (lbc *LoadBalancerController) sync(key string) (err error) {
	if !lbc.hasSynced() {
		time.Sleep(storeSyncPollPeriod)
		return fmt.Errorf("waiting for stores to sync")
	}
	glog.V(3).Infof("Syncing %v", key)

	allIngresses, err := lbc.ingLister.ListAll()
	if err != nil {
		return err
	}
	gceIngresses, err := lbc.ingLister.ListGCEIngresses()
	if err != nil {
		return err
	}

	allNodePorts := lbc.Translator.ToNodePorts(&allIngresses)
	gceNodePorts := lbc.Translator.ToNodePorts(&gceIngresses)
	nodeNames, err := getReadyNodeNames(listers.NewNodeLister(lbc.nodeLister))
	if err != nil {
		return err
	}
	lbNames := lbc.ingLister.Store.ListKeys()

	obj, ingExists, err := lbc.ingLister.Store.GetByKey(key)
	if err != nil {
		return err
	}

	if !ingExists {
		glog.V(2).Infof("Ingress %q no longer exists, triggering GC", key)
		return lbc.CloudClusterManager.GC(lbNames, allNodePorts)
	}

	ing, ok := obj.(*extensions.Ingress)
	if !ok {
		return fmt.Errorf("invalid object (not of type Ingress), type was %T", obj)
	}
	// DeepCopy for assurance that we don't pollute other goroutines with changes.
	ing = ing.DeepCopy()

	// This performs a 2 phase checkpoint with the cloud:
	// * Phase 1 creates/verifies resources are as expected. At the end of a
	//   successful checkpoint we know that existing L7s are WAI, and the L7
	//   for the Ingress associated with "key" is ready for a UrlMap update.
	//   If this encounters an error, eg for quota reasons, we want to invoke
	//   Phase 2 right away and retry checkpointing.
	// * Phase 2 performs GC by refcounting shared resources. This needs to
	//   happen periodically whether or not stage 1 fails. At the end of a
	//   successful GC we know that there are no dangling cloud resources that
	//   don't have an associated Kubernetes Ingress/Service/Endpoint.

	var syncError error
	defer func() {
		if deferErr := lbc.CloudClusterManager.GC(lbNames, allNodePorts); deferErr != nil {
			err = fmt.Errorf("error during sync %v, error during GC %v", syncError, deferErr)
		}
		glog.V(3).Infof("Finished syncing %v", key)
	}()

	singleIngressList := &extensions.IngressList{
		Items: []extensions.Ingress{*ing},
	}
	lb, err := lbc.toRuntimeInfo(ing)
	if err != nil {
		lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, "Ingress", err.Error())
		return err
	}
	lbs := []*loadbalancers.L7RuntimeInfo{lb}

	// Get all service ports for the ingress being synced.
	ingSvcPorts := lbc.Translator.ToNodePorts(singleIngressList)

	igs, err := lbc.CloudClusterManager.Checkpoint(lbs, nodeNames, ingSvcPorts, allNodePorts, lbc.Translator.GatherEndpointPorts(gceNodePorts))
	if err != nil {
		const eventMsg = "GCE"
		if fwErr, ok := err.(*firewalls.FirewallSyncError); ok {
			lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeNormal, eventMsg, fwErr.Message)
		} else {
			lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, eventMsg, err.Error())
			syncError = err
		}
	}

	if isGCEMultiClusterIngress(ing) {
		// Add instance group names as annotation on the ingress.
		if ing.Annotations == nil {
			ing.Annotations = map[string]string{}
		}
		if err = setInstanceGroupsAnnotation(ing.Annotations, igs); err != nil {
			return err
		}
		return updateAnnotations(lbc.client, ing.Name, ing.Namespace, ing.Annotations)
	}

	if lbc.negEnabled {
		svcPorts := lbc.Translator.ToNodePorts(singleIngressList)
		for _, svcPort := range svcPorts {
			if svcPort.NEGEnabled {

				zones, err := lbc.Translator.ListZones()
				if err != nil {
					return err
				}
				if err := lbc.CloudClusterManager.backendPool.Link(svcPort, zones); err != nil {
					return err
				}
			}
		}
	}

	// Update the UrlMap of the single loadbalancer that came through the watch.
	l7, err := lbc.CloudClusterManager.l7Pool.Get(key)
	if err != nil {
		syncError = fmt.Errorf("%v, unable to get loadbalancer: %v", syncError, err)
		return syncError
	}

	if urlMap, err := lbc.Translator.ToURLMap(ing); err != nil {
		syncError = fmt.Errorf("%v, convert to url map error %v", syncError, err)
	} else if err := l7.UpdateUrlMap(urlMap); err != nil {
		lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, "UrlMap", err.Error())
		syncError = fmt.Errorf("%v, update url map error: %v", syncError, err)
	} else if err := lbc.updateIngressStatus(l7, ing); err != nil {
		lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, "Status", err.Error())
		syncError = fmt.Errorf("%v, update ingress error: %v", syncError, err)
	}
	return syncError
}

// updateIngressStatus updates the IP and annotations of a loadbalancer.
// The annotations are parsed by kubectl describe.
func (lbc *LoadBalancerController) updateIngressStatus(l7 *loadbalancers.L7, ing *extensions.Ingress) error {
	ingClient := lbc.client.Extensions().Ingresses(ing.Namespace)

	// Update IP through update/status endpoint
	ip := l7.GetIP()
	currIng, err := ingClient.Get(ing.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	currIng.Status = extensions.IngressStatus{
		LoadBalancer: apiv1.LoadBalancerStatus{
			Ingress: []apiv1.LoadBalancerIngress{
				{IP: ip},
			},
		},
	}
	if ip != "" {
		lbIPs := ing.Status.LoadBalancer.Ingress
		if len(lbIPs) == 0 || lbIPs[0].IP != ip {
			// TODO: If this update fails it's probably resource version related,
			// which means it's advantageous to retry right away vs requeuing.
			glog.Infof("Updating loadbalancer %v/%v with IP %v", ing.Namespace, ing.Name, ip)
			if _, err := ingClient.UpdateStatus(currIng); err != nil {
				return err
			}
			lbc.ctx.Recorder(ing.Namespace).Eventf(currIng, apiv1.EventTypeNormal, "CREATE", "ip: %v", ip)
		}
	}
	annotations := loadbalancers.GetLBAnnotations(l7, currIng.Annotations, lbc.CloudClusterManager.backendPool)
	if err := updateAnnotations(lbc.client, ing.Name, ing.Namespace, annotations); err != nil {
		return err
	}
	return nil
}

// toRuntimeInfo returns L7RuntimeInfo for the given ingress.
func (lbc *LoadBalancerController) toRuntimeInfo(ing *extensions.Ingress) (*loadbalancers.L7RuntimeInfo, error) {
	k, err := keyFunc(ing)
	if err != nil {
		return nil, fmt.Errorf("cannot get key for Ingress %v/%v: %v", ing.Namespace, ing.Name, err)
	}

	var tls []*loadbalancers.TLSCerts

	annotations := annotations.FromIngress(ing)
	// Load the TLS cert from the API Spec if it is not specified in the annotation.
	// TODO: enforce this with validation.
	if annotations.UseNamedTLS() == "" {
		tls, err = lbc.tlsLoader.Load(ing)
		if err != nil {
			return nil, fmt.Errorf("cannot get certs for Ingress %v/%v: %v", ing.Namespace, ing.Name, err)
		}
	}

	return &loadbalancers.L7RuntimeInfo{
		Name:         k,
		TLS:          tls,
		TLSName:      annotations.UseNamedTLS(),
		AllowHTTP:    annotations.AllowHTTP(),
		StaticIPName: annotations.StaticIPName(),
	}, nil
}

func updateAnnotations(client kubernetes.Interface, name, namespace string, annotations map[string]string) error {
	ingClient := client.Extensions().Ingresses(namespace)
	currIng, err := ingClient.Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(currIng.Annotations, annotations) {
		glog.V(3).Infof("Updating annotations of %v/%v", namespace, name)
		currIng.Annotations = annotations
		if _, err := ingClient.Update(currIng); err != nil {
			return err
		}
	}
	return nil
}
