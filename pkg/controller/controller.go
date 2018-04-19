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

	compute "google.golang.org/api/compute/v1"
	apiv1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	unversionedcore "k8s.io/client-go/kubernetes/typed/core/v1"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	multierror "github.com/hashicorp/go-multierror"
	"k8s.io/client-go/tools/record"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller/translator"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/mapper"
	"k8s.io/ingress-gce/pkg/tls"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	// DefaultFirewallName is the name to user for firewall rules created
	// by an L7 controller when the --fireall-rule is not used.
	DefaultFirewallName = ""
	// Frequency to poll on local stores to sync.
	storeSyncPollPeriod = 5 * time.Second
)

var (
	keyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc
)

// LoadBalancerController watches the kubernetes api and adds/removes services
// from the loadbalancer, via loadBalancerConfig.
type LoadBalancerController struct {
	ctx *context.ControllerContext

	ingLister  utils.StoreToIngressLister
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
// - clusterManager: A ClusterManager capable of creating all cloud resources
//	 required for L7 loadbalancing.
// - resyncPeriod: Watchers relist from the Kubernetes API server this often.
func NewLoadBalancerController(ctx *context.ControllerContext, clusterManager *ClusterManager, negEnabled bool, stopCh chan struct{}) (*LoadBalancerController, error) {
	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(glog.Infof)
	broadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{
		Interface: ctx.KubeClient.Core().Events(""),
	})
	lbc := LoadBalancerController{
		ctx:                 ctx,
		ingLister:           utils.StoreToIngressLister{Store: ctx.IngressInformer.GetStore()},
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
	lbc.Translator = translator.New(lbc.ctx, lbc.CloudClusterManager.ClusterNamer,
		ctx.ServiceInformer.GetIndexer(),
		ctx.NodeInformer.GetIndexer(),
		ctx.PodInformer.GetIndexer(),
		endpointIndexer,
		negEnabled)

	lbc.tlsLoader = &tls.TLSCertsFromSecretsLoader{Client: lbc.ctx.KubeClient}

	glog.V(3).Infof("Created new loadbalancer controller")

	return &lbc, nil
}

// Implements MCIEnqueue
func (lbc *LoadBalancerController) EnqueueAllIngresses() error {
	ings, err := lbc.ingLister.ListGCEIngresses()
	if err != nil {
		return err
	}
	for _, ing := range ings.Items {
		lbc.ingQueue.Enqueue(&ing)
	}
	return nil
}

// Implements MCIEnqueue
func (lbc *LoadBalancerController) EnqueueIngress(ing *extensions.Ingress) {
	lbc.ingQueue.Enqueue(ing)
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
		if !utils.IsGCEIngress(&ing) {
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

// sync manages Ingress create/updates/deletes
func (lbc *LoadBalancerController) sync(key string) (retErr error) {
	if !lbc.hasSynced() {
		time.Sleep(storeSyncPollPeriod)
		return fmt.Errorf("waiting for stores to sync")
	}
	glog.V(3).Infof("Syncing %v", key)

	gceIngresses, err := lbc.ingLister.ListGCEIngresses()
	if err != nil {
		return err
	}

	svcMappers := lbc.ctx.ServiceMappers()
	// gceNodePorts contains ServicePort's for all Ingresses.
	var gceNodePorts []backends.ServicePort
	for _, gceIngress := range gceIngresses.Items {
		for cluster, svcMapper := range svcMappers {
			svcPorts, _, err := servicePorts(&gceIngress, svcMapper)
			if err != nil {
				glog.Infof("Error getting NodePort's for cluster %v: %v", cluster, err.Error())
			}
			gceNodePorts = append(gceNodePorts, svcPorts...)
		}
	}

	lbNames := lbc.ingLister.Store.ListKeys()
	obj, ingExists, err := lbc.ingLister.Store.GetByKey(key)
	if err != nil {
		return err
	}

	if !ingExists {
		glog.V(2).Infof("Ingress %q no longer exists, triggering GC", key)
		resourceManagers := lbc.ctx.ResourceManagers()
		for cluster, resourceManager := range resourceManagers {
			ingNamespace, ingName, err := cache.SplitMetaNamespaceKey(key)
			if err != nil {
				return fmt.Errorf("Error extracting (namespace,name) pair from key %v: %v", key, err)
			}
			err = resourceManager.DeleteTargetIngress(ingName, ingNamespace)
			if err != nil {
				return fmt.Errorf("Error deleting target ingress %v/%v in cluster %v: %v", ingNamespace, ingName, cluster, err)
			}
		}
		// GC will find GCE resources that were used for this ingress and delete them.
		return lbc.CloudClusterManager.GC(lbNames, gceNodePorts)
	}

	// Get ingress and DeepCopy for assurance that we don't pollute other goroutines with changes.
	ing, ok := obj.(*extensions.Ingress)
	if !ok {
		return fmt.Errorf("invalid object (not of type Ingress), type was %T", obj)
	}
	ing = ing.DeepCopy()

	ensureErr := lbc.ensureIngress(key, ing, svcMappers, gceNodePorts)
	if ensureErr != nil {
		lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, "Sync", fmt.Sprintf("Error during sync: %v", ensureErr.Error()))
	}

	// Garbage collection will occur regardless of an error occurring. If an error occurred,
	// it could have been caused by quota issues; therefore, garbage collecting now may
	// free up enough quota for the next sync to pass.
	if gcErr := lbc.CloudClusterManager.GC(lbNames, gceNodePorts); gcErr != nil {
		retErr = fmt.Errorf("error during sync %v, error during GC %v", retErr, gcErr)
	}

	return ensureErr
}

func (lbc *LoadBalancerController) ensureIngress(key string, ing *extensions.Ingress, svcMappers map[string]mapper.ClusterServiceMapper, gceNodePorts []backends.ServicePort) error {
	var ingNodePorts []backends.ServicePort
	var backendToServicePorts map[extensions.IngressBackend]backends.ServicePort
	for cluster, svcMapper := range svcMappers {
		svcPorts, m, err := servicePorts(ing, svcMapper)
		ingNodePorts = svcPorts
		backendToServicePorts = m
		if err != nil {
			// TODO(rramkumar): Clean this up, it's very ugly.
			switch err.(type) {
			case *multierror.Error:
				// Emit an event for each error in the multierror.
				merr := err.(*multierror.Error)
				for _, e := range merr.Errors {
					msg := fmt.Sprintf("Error getting NodePort's for cluster %v: %v", cluster, e)
					lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, "Service", msg)
				}
			default:
				msg := fmt.Sprintf("%v", err)
				lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeWarning, "Service", msg)
			}
		}
		// TODO(rramkumar): In each iteration, pass svcPorts to a validator which progressively
		// validates that each list of ServicePort's it gets is consistent with the previous.
		// For now, we are going to do no validation.
	}

	nodeNames, err := getReadyNodeNames(listers.NewNodeLister(lbc.nodeLister))
	if err != nil {
		return err
	}

	var igs []*compute.InstanceGroup
	if lbc.ctx.MC.MCIEnabled {
		resourceManagers := lbc.ctx.ResourceManagers()
		for cluster, resourceManager := range resourceManagers {
			targetIng, ensureErr := resourceManager.EnsureTargetIngress(ing)
			if ensureErr != nil {
				return fmt.Errorf("Error ensuring target ingress %v/%v in cluster %v: %v", ing.Namespace, ing.Name, cluster, ensureErr)
			}
			annotationVals, annotationErr := instanceGroupsAnnotation(targetIng)
			if annotationErr != nil {
				return fmt.Errorf("Error getting instance group annotations from target ingress %v/%v in cluster %v: %v", ing.Namespace, ing.Name, cluster, annotationErr)
			}
			if len(annotationVals) == 0 {
				// If a target ingress does not have the annotation yet,
				// then just return and wait for the ingress to be requeued.
				glog.V(3).Infof("Could not find instance group annotation for target ingress %v/%v in cluster %v. Requeueing ingress...", ing.Namespace, ing.Name, cluster)
				return nil
			}
			for _, val := range annotationVals {
				ig, err := lbc.CloudClusterManager.instancePool.Get(val.Name, utils.TrimZoneLink(val.Zone))
				if err != nil {
					return fmt.Errorf("Error getting instance groups for target ingress %v/%v in cluster %v: %v", ing.Namespace, ing.Name, cluster, err)
				}
				glog.V(3).Infof("Found instance group %v for target ingress %v/v in cluster %v", ig.Name, ing.Namespace, ing.Name, cluster)
				igs = append(igs, ig)
			}
		}
	} else {
		igs, err = lbc.CloudClusterManager.EnsureInstanceGroupsAndPorts(nodeNames, ingNodePorts)
		if err != nil {
			return err
		}
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

	// Continue syncing this specific GCE ingress.
	lb, err := lbc.toRuntimeInfo(ing)
	if err != nil {
		return err
	}

	// Create the backend services and higher-level LB resources.
	if err = lbc.CloudClusterManager.EnsureLoadBalancer(lb, ingNodePorts, igs); err != nil {
		return err
	}

	negEndpointPorts := lbc.Translator.GatherEndpointPorts(gceNodePorts)
	// Ensure firewall rule for the cluster and pass any NEG endpoint ports.
	if err = lbc.CloudClusterManager.EnsureFirewall(nodeNames, negEndpointPorts, lbc.ctx.MC.MCIEnabled); err != nil {
		if fwErr, ok := err.(*firewalls.FirewallXPNError); ok {
			// XPN: Raise an event and ignore the error.
			lbc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeNormal, "XPN", fwErr.Message)
		} else {
			return err
		}
	}

	// If NEG enabled, link the backend services to the NEGs.
	if lbc.negEnabled {
		for _, svcPort := range ingNodePorts {
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
		return fmt.Errorf("unable to get loadbalancer: %v", err)
	}

	urlMap, err := lbc.Translator.ToURLMap(ing, backendToServicePorts)
	if err != nil {
		return fmt.Errorf("error converting to URLMap: %v", err)
	}

	if err := l7.UpdateUrlMap(urlMap); err != nil {
		return fmt.Errorf("error updating URLMap: %v", err)
	}

	if err := lbc.updateIngressStatus(l7, ing); err != nil {
		return fmt.Errorf("error updating ingress status: %v", err)
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
	annotations := loadbalancers.GetLBAnnotations(l7, currIng.Annotations, lbc.CloudClusterManager.backendPool)
	if err := updateAnnotations(lbc.ctx.KubeClient, ing.Name, ing.Namespace, annotations); err != nil {
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

// servicePorts converts an Ingress to its ServicePort's using a specific ClusterServiceMapper.
func servicePorts(ing *extensions.Ingress, svcMapper mapper.ClusterServiceMapper) ([]backends.ServicePort, map[extensions.IngressBackend]backends.ServicePort, error) {
	var svcPorts []backends.ServicePort
	backendToServiceMap, err := svcMapper.Services(ing)
	if err != nil {
		return nil, nil, err
	}
	backendToServicePortsMap, err := backends.ServicePorts(backendToServiceMap)
	if err != nil {
		return nil, nil, err
	}
	for _, svcPort := range backendToServicePortsMap {
		svcPorts = append(svcPorts, svcPort)
	}
	return svcPorts, backendToServicePortsMap, nil
}
