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
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/golang/glog"
	compute "google.golang.org/api/compute/v1"

	apiv1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	unversionedcore "k8s.io/client-go/kubernetes/typed/core/v1"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	backendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/healthchecks"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud/meta"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller/translator"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/tls"
	"k8s.io/ingress-gce/pkg/utils"
)

// LoadBalancerController watches the kubernetes api and adds/removes services
// from the loadbalancer, via loadBalancerConfig.
type LoadBalancerController struct {
	ctx *context.ControllerContext

	ingLister  utils.StoreToIngressLister
	nodeLister cache.Indexer
	nodes      *NodeController

	// TODO: Watch secrets
	ingQueue   utils.TaskQueue
	Translator *translator.Translator
	stopCh     chan struct{}
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

	// Resource pools.
	instancePool instances.NodePool
	l7Pool       loadbalancers.LoadBalancerPool

	// syncer implementation for backends
	backendSyncer backends.Syncer
	// linker implementations for backends
	negLinker backends.Linker
	igLinker  backends.Linker
}

// NewLoadBalancerController creates a controller for gce loadbalancers.
func NewLoadBalancerController(
	ctx *context.ControllerContext,
	stopCh chan struct{}) *LoadBalancerController {

	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(glog.Infof)
	broadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{
		Interface: ctx.KubeClient.Core().Events(""),
	})
	healthChecker := healthchecks.NewHealthChecker(ctx.Cloud, ctx.HealthCheckPath, ctx.DefaultBackendHealthCheckPath, ctx.ClusterNamer, ctx.DefaultBackendSvcPortID.Service)
	instancePool := instances.NewNodePool(ctx.Cloud, ctx.ClusterNamer)
	backendPool := backends.NewPool(ctx.Cloud, ctx.ClusterNamer, true)
	lbc := LoadBalancerController{
		ctx:           ctx,
		ingLister:     utils.StoreToIngressLister{Store: ctx.IngressInformer.GetStore()},
		nodeLister:    ctx.NodeInformer.GetIndexer(),
		Translator:    translator.NewTranslator(ctx),
		tlsLoader:     &tls.TLSCertsFromSecretsLoader{Client: ctx.KubeClient},
		stopCh:        stopCh,
		hasSynced:     ctx.HasSynced,
		nodes:         NewNodeController(ctx, instancePool),
		instancePool:  instancePool,
		l7Pool:        loadbalancers.NewLoadBalancerPool(ctx.Cloud, ctx.ClusterNamer),
		backendSyncer: backends.NewBackendSyncer(backendPool, healthChecker, ctx.ClusterNamer, ctx.BackendConfigEnabled),
		negLinker:     backends.NewNEGLinker(backendPool, ctx.Cloud, ctx.ClusterNamer),
		igLinker:      backends.NewInstanceGroupLinker(instancePool, backendPool, ctx.ClusterNamer),
	}

	lbc.ingQueue = utils.NewPeriodicTaskQueue("ingresses", lbc.sync)

	// Ingress event handlers.
	ctx.IngressInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addIng := obj.(*extensions.Ingress)
			if !utils.IsGCEIngress(addIng) && !utils.IsGCEMultiClusterIngress(addIng) {
				glog.V(4).Infof("Ignoring add for ingress %v based on annotation %v", addIng.Name, annotations.IngressClassKey)
				return
			}

			glog.V(3).Infof("Ingress %v/%v added, enqueuing", addIng.Namespace, addIng.Name)
			lbc.ctx.Recorder(addIng.Namespace).Eventf(addIng, apiv1.EventTypeNormal, "ADD", fmt.Sprintf("%s/%s", addIng.Namespace, addIng.Name))
			lbc.ingQueue.Enqueue(obj)
		},
		DeleteFunc: func(obj interface{}) {
			delIng := obj.(*extensions.Ingress)
			if !utils.IsGCEIngress(delIng) && !utils.IsGCEMultiClusterIngress(delIng) {
				glog.V(4).Infof("Ignoring delete for ingress %v based on annotation %v", delIng.Name, annotations.IngressClassKey)
				return
			}

			glog.V(3).Infof("Ingress %v/%v deleted, enqueueing", delIng.Namespace, delIng.Name)
			lbc.ingQueue.Enqueue(obj)
		},
		UpdateFunc: func(old, cur interface{}) {
			curIng := cur.(*extensions.Ingress)
			if !utils.IsGCEIngress(curIng) && !utils.IsGCEMultiClusterIngress(curIng) {
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

	// Service event handlers.
	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			svc := obj.(*apiv1.Service)
			ings := lbc.ctx.IngressesForService(svc)
			lbc.ingQueue.Enqueue(convert(ings)...)
		},
		UpdateFunc: func(old, cur interface{}) {
			if !reflect.DeepEqual(old, cur) {
				svc := cur.(*apiv1.Service)
				ings := lbc.ctx.IngressesForService(svc)
				lbc.ingQueue.Enqueue(convert(ings)...)
			}
		},
		// Ingress deletes matter, service deletes don't.
	})

	// BackendConfig event handlers.
	if ctx.BackendConfigEnabled {
		ctx.BackendConfigInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				beConfig := obj.(*backendconfigv1beta1.BackendConfig)
				ings := lbc.ctx.IngressesForBackendConfig(beConfig)
				lbc.ingQueue.Enqueue(convert(ings)...)

			},
			UpdateFunc: func(old, cur interface{}) {
				if !reflect.DeepEqual(old, cur) {
					beConfig := cur.(*backendconfigv1beta1.BackendConfig)
					ings := lbc.ctx.IngressesForBackendConfig(beConfig)
					lbc.ingQueue.Enqueue(convert(ings)...)
				}
			},
			DeleteFunc: func(obj interface{}) {
				beConfig := obj.(*backendconfigv1beta1.BackendConfig)
				ings := lbc.ctx.IngressesForBackendConfig(beConfig)
				lbc.ingQueue.Enqueue(convert(ings)...)
			},
		})
	}

	// Register health check on controller context.
	ctx.AddHealthCheck("ingress", func() error {
		_, err := backendPool.Get("foo", meta.VersionGA)

		// If this container is scheduled on a node without compute/rw it is
		// effectively useless, but it is healthy. Reporting it as unhealthy
		// will lead to container crashlooping.
		if utils.IsHTTPErrorCode(err, http.StatusForbidden) {
			glog.Infof("Reporting cluster as healthy, but unable to list backends: %v", err)
			return nil
		}
		return err
	})

	glog.V(3).Infof("Created new loadbalancer controller")

	return &lbc
}

func (lbc *LoadBalancerController) Init() {
	// TODO(rramkumar): Try to get rid of this "Init".
	lbc.instancePool.Init(lbc.Translator)
	lbc.backendSyncer.Init(lbc.Translator)
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
	// TODO(rramkumar): Do we need deleteAll? Can we get rid of its' flag?
	if deleteAll {
		glog.Infof("Shutting down cluster manager.")
		if err := lbc.l7Pool.Shutdown(); err != nil {
			return err
		}
		// The backend pool will also delete instance groups.
		return lbc.backendSyncer.Shutdown()
	}
	return nil
}

// sync manages Ingress create/updates/deletes
func (lbc *LoadBalancerController) sync(key string) (retErr error) {
	if !lbc.hasSynced() {
		time.Sleep(context.StoreSyncPollPeriod)
		return fmt.Errorf("waiting for stores to sync")
	}
	glog.V(3).Infof("Syncing %v", key)

	gceIngresses, err := lbc.ingLister.ListGCEIngresses()
	if err != nil {
		return err
	}
	// gceSvcPorts contains the ServicePorts used by only single-cluster ingress.
	gceSvcPorts := lbc.ToSvcPorts(&gceIngresses)
	nodeNames, err := utils.GetReadyNodeNames(listers.NewNodeLister(lbc.nodeLister))
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
		// GC will find GCE resources that were used for this ingress and delete them.
		return lbc.gc(lbNames, gceSvcPorts)
	}

	// Get ingress and DeepCopy for assurance that we don't pollute other goroutines with changes.
	ing, ok := obj.(*extensions.Ingress)
	if !ok {
		return fmt.Errorf("invalid object (not of type Ingress), type was %T", obj)
	}
	ing = ing.DeepCopy()

	ensureErr := lbc.ensureIngress(ing, nodeNames)
	if ensureErr != nil {
		lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, "Sync", fmt.Sprintf("Error during sync: %v", ensureErr.Error()))
	}

	// Garbage collection will occur regardless of an error occurring. If an error occurred,
	// it could have been caused by quota issues; therefore, garbage collecting now may
	// free up enough quota for the next sync to pass.
	if gcErr := lbc.gc(lbNames, gceSvcPorts); gcErr != nil {
		retErr = fmt.Errorf("error during sync %v, error during GC %v", retErr, gcErr)
	}

	return ensureErr
}

func (lbc *LoadBalancerController) ensureIngress(ing *extensions.Ingress, nodeNames []string) error {
	urlMap, errs := lbc.Translator.TranslateIngress(ing, lbc.ctx.DefaultBackendSvcPortID)
	if errs != nil {
		return fmt.Errorf("error while evaluating the ingress spec: %v", joinErrs(errs))
	}

	ingSvcPorts := urlMap.AllServicePorts()
	igs, err := lbc.ensureInstanceGroupsAndPorts(ingSvcPorts, nodeNames)
	if err != nil {
		return err
	}

	if utils.IsGCEMultiClusterIngress(ing) {
		// Add instance group names as annotation on the ingress and return.
		if ing.Annotations == nil {
			ing.Annotations = map[string]string{}
		}
		if err = setInstanceGroupsAnnotation(ing.Annotations, igs); err != nil {
			return err
		}
		if err = updateAnnotations(lbc.ctx.KubeClient, ing.Name, ing.Namespace, ing.Annotations); err != nil {
			return err
		}
		return nil
	}

	// Sync the backends
	if err := lbc.backendSyncer.Sync(ingSvcPorts); err != nil {
		return err
	}

	// Get the zones our groups live in.
	zones, err := lbc.Translator.ListZones()
	var groupKeys []backends.GroupKey
	for _, zone := range zones {
		groupKeys = append(groupKeys, backends.GroupKey{Zone: zone})
	}

	// Link backends to groups.
	for _, sp := range ingSvcPorts {
		var linkErr error
		if sp.NEGEnabled {
			// Link backend to NEG's if the backend has NEG enabled.
			linkErr = lbc.negLinker.Link(sp, groupKeys)
		} else {
			// Otherwise, link backend to IG's.
			linkErr = lbc.igLinker.Link(sp, groupKeys)
		}
		if linkErr != nil {
			return linkErr
		}
	}

	lb, err := lbc.toRuntimeInfo(ing, urlMap)
	if err != nil {
		return err
	}
	// Create higher-level LB resources.
	if err := lbc.l7Pool.Sync(lb); err != nil {
		return err
	}

	// Get the loadbalancer and update the ingress status.
	l7, err := lbc.l7Pool.Get(lb.Name)
	if err != nil {
		return fmt.Errorf("unable to get loadbalancer: %v", err)
	}
	if err := lbc.updateIngressStatus(l7, ing); err != nil {
		return fmt.Errorf("update ingress status error: %v", err)
	}

	return nil
}

// updateIngressStatus updates the IP and annotations of a loadbalancer.
// The annotations are parsed by kubectl describe.
func (lbc *LoadBalancerController) updateIngressStatus(l7 *loadbalancers.L7, ing *extensions.Ingress) error {
	ingClient := lbc.ctx.KubeClient.Extensions().Ingresses(ing.Namespace)

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
	annotations, err := loadbalancers.GetLBAnnotations(l7, currIng.Annotations, lbc.backendSyncer)
	if err != nil {
		return err
	}

	if err := updateAnnotations(lbc.ctx.KubeClient, ing.Name, ing.Namespace, annotations); err != nil {
		return err
	}
	return nil
}

// toRuntimeInfo returns L7RuntimeInfo for the given ingress.
func (lbc *LoadBalancerController) toRuntimeInfo(ing *extensions.Ingress, urlMap *utils.GCEURLMap) (*loadbalancers.L7RuntimeInfo, error) {
	k, err := utils.KeyFunc(ing)
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
		UrlMap:       urlMap,
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

// ToSvcPorts is a helper method over translator.TranslateIngress to process a list of ingresses.
// Note: This method is used for GC.
func (lbc *LoadBalancerController) ToSvcPorts(ings *extensions.IngressList) []utils.ServicePort {
	var knownPorts []utils.ServicePort
	for _, ing := range ings.Items {
		urlMap, _ := lbc.Translator.TranslateIngress(&ing, lbc.ctx.DefaultBackendSvcPortID)
		knownPorts = append(knownPorts, urlMap.AllServicePorts()...)
	}
	return knownPorts
}

func (lbc *LoadBalancerController) ensureInstanceGroupsAndPorts(svcPorts []utils.ServicePort, nodeNames []string) ([]*compute.InstanceGroup, error) {
	ports := []int64{}
	for _, p := range uniq(svcPorts) {
		if !p.NEGEnabled {
			ports = append(ports, p.NodePort)
		}
	}

	// Create instance groups and set named ports.
	igs, err := lbc.instancePool.EnsureInstanceGroupsAndPorts(lbc.ctx.ClusterNamer.InstanceGroup(), ports)
	if err != nil {
		return nil, err
	}
	// Add/remove instances to the instance groups.
	if err = lbc.instancePool.Sync(nodeNames); err != nil {
		return nil, err
	}

	return igs, nil
}

// gc garbage collects unused resources.
// - lbNames are the names of L7 loadbalancers we wish to exist. Those not in
//   this list are removed from the cloud.
// - nodePorts are the ports for which we want BackendServies. BackendServices
//   for ports not in this list are deleted.
// This method ignores googleapi 404 errors (StatusNotFound).
func (lbc *LoadBalancerController) gc(lbNames []string, nodePorts []utils.ServicePort) error {
	// On GC:
	// * Loadbalancers need to get deleted before backends.
	// * Backends are refcounted in a shared pool.
	// * We always want to GC backends even if there was an error in GCing
	//   loadbalancers, because the next Sync could rely on the GC for quota.
	// * There are at least 2 cases for backend GC:
	//   1. The loadbalancer has been deleted.
	//   2. An update to the url map drops the refcount of a backend. This can
	//      happen when an Ingress is updated, if we don't GC after the update
	//      we'll leak the backend.
	lbErr := lbc.l7Pool.GC(lbNames)
	beErr := lbc.backendSyncer.GC(nodePorts)
	if lbErr != nil {
		return lbErr
	}
	if beErr != nil {
		return beErr
	}

	// TODO(ingress#120): Move this to the backend pool so it mirrors creation
	if len(lbNames) == 0 {
		igName := lbc.ctx.ClusterNamer.InstanceGroup()
		glog.Infof("Deleting instance group %v", igName)
		if err := lbc.instancePool.DeleteInstanceGroup(igName); err != err {
			return err
		}
		glog.V(2).Infof("Shutting down firewall as there are no loadbalancers")
	}

	return nil
}
