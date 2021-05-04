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

package firewalls

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"

	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/common/operator"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller/translator"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

var (
	// queueKey is a "fake" key which can be enqueued to a task queue.
	queueKey = &v1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "queueKey"},
	}

	ErrNoILBIngress = errors.New("no ILB Ingress found")
)

// FirewallController synchronizes the firewall rule for all ingresses.
type FirewallController struct {
	ctx          *context.ControllerContext
	firewallPool SingleFirewallPool
	queue        utils.TaskQueue
	translator   *translator.Translator
	nodeLister   cache.Indexer
	hasSynced    func() bool
}

// NewFirewallController returns a new firewall controller.
func NewFirewallController(
	ctx *context.ControllerContext,
	portRanges []string) *FirewallController {
	firewallPool := NewFirewallPool(ctx.Cloud, ctx.ClusterNamer, gce.L7LoadBalancerSrcRanges(), portRanges)

	fwc := &FirewallController{
		ctx:          ctx,
		firewallPool: firewallPool,
		translator:   translator.NewTranslator(ctx),
		nodeLister:   ctx.NodeInformer.GetIndexer(),
		hasSynced:    ctx.HasSynced,
	}

	fwc.queue = utils.NewPeriodicTaskQueue("", "firewall", fwc.sync)

	// Ingress event handlers.
	ctx.IngressInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addIng := obj.(*v1.Ingress)
			if !utils.IsGCEIngress(addIng) && !utils.IsGCEMultiClusterIngress(addIng) {
				return
			}
			fwc.queue.Enqueue(queueKey)
		},
		DeleteFunc: func(obj interface{}) {
			delIng := obj.(*v1.Ingress)
			if !utils.IsGCEIngress(delIng) && !utils.IsGCEMultiClusterIngress(delIng) {
				return
			}
			fwc.queue.Enqueue(queueKey)
		},
		UpdateFunc: func(old, cur interface{}) {
			curIng := cur.(*v1.Ingress)
			if !utils.IsGCEIngress(curIng) && !utils.IsGCEMultiClusterIngress(curIng) {
				return
			}
			fwc.queue.Enqueue(queueKey)
		},
	})

	// Service event handlers.
	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			svc := obj.(*apiv1.Service)
			ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesService(svc).AsList()
			if len(ings) > 0 {
				fwc.queue.Enqueue(queueKey)
			}
		},
		UpdateFunc: func(old, cur interface{}) {
			if !reflect.DeepEqual(old, cur) {
				svc := cur.(*apiv1.Service)
				ings := operator.Ingresses(ctx.Ingresses().List()).ReferencesService(svc).AsList()
				if len(ings) > 0 {
					fwc.queue.Enqueue(queueKey)
				}
			}
		},
	})

	return fwc
}

// ToSvcPorts is a helper method over translator.TranslateIngress to process a list of ingresses.
// TODO(rramkumar): This is a copy of code in controller.go. Extract this into
// something shared.
func (fwc *FirewallController) ToSvcPorts(ings []*v1.Ingress) []utils.ServicePort {
	var knownPorts []utils.ServicePort
	for _, ing := range ings {
		urlMap, _ := fwc.translator.TranslateIngress(ing, fwc.ctx.DefaultBackendSvcPort.ID, fwc.ctx.ClusterNamer)
		knownPorts = append(knownPorts, urlMap.AllServicePorts()...)
	}
	return knownPorts
}

func (fwc *FirewallController) Run() {
	defer fwc.shutdown()
	fwc.queue.Run()
}

// This should only be called when the process is being terminated.
func (fwc *FirewallController) shutdown() {
	klog.Infof("Shutting down Firewall Controller")
	fwc.queue.Shutdown()
}

func (fwc *FirewallController) sync(key string) error {
	if !fwc.hasSynced() {
		time.Sleep(context.StoreSyncPollPeriod)
		return fmt.Errorf("waiting for stores to sync")
	}
	klog.V(3).Infof("Syncing firewall")

	gceIngresses := operator.Ingresses(fwc.ctx.Ingresses().List()).Filter(func(ing *v1.Ingress) bool {
		return utils.IsGCEIngress(ing)
	}).AsList()

	// If there are no more ingresses, then delete the firewall rule.
	if len(gceIngresses) == 0 {
		fwc.firewallPool.GC()
		return nil
	}

	// gceSvcPorts contains the ServicePorts used by only single-cluster ingress.
	gceSvcPorts := fwc.ToSvcPorts(gceIngresses)
	nodeNames, err := utils.GetReadyNodeNames(listers.NewNodeLister(fwc.nodeLister))
	if err != nil {
		return err
	}
	negPorts := fwc.translator.GatherEndpointPorts(gceSvcPorts)

	// check if any nodeport based service backend exists
	// if so, then need to include nodePort ranges for firewall
	needNodePort := false
	for _, svcPort := range gceSvcPorts {
		if !svcPort.NEGEnabled {
			needNodePort = true
			break
		}
	}

	var additionalRanges []string
	ilbRange, err := fwc.ilbFirewallSrcRange(gceIngresses)
	if err != nil {
		if err != features.ErrSubnetNotFound && err != ErrNoILBIngress {
			return err
		}
	} else {
		additionalRanges = append(additionalRanges, ilbRange)
	}

	var additionalPorts []string
	if flags.F.EnableBackendConfigHealthCheck {
		hcPorts := fwc.getCustomHealthCheckPorts(gceSvcPorts)
		additionalPorts = append(additionalPorts, hcPorts...)
	}
	additionalPorts = append(additionalPorts, negPorts...)

	// Ensure firewall rule for the cluster and pass any NEG endpoint ports.
	if err := fwc.firewallPool.Sync(nodeNames, additionalPorts, additionalRanges, needNodePort); err != nil {
		if fwErr, ok := err.(*FirewallXPNError); ok {
			// XPN: Raise an event on each ingress
			for _, ing := range gceIngresses {
				if annotations.FromIngress(ing).SuppressFirewallXPNError() {
					continue
				}
				fwc.ctx.Recorder(ing.Namespace).Eventf(ing, apiv1.EventTypeNormal, "XPN", fwErr.Message)
			}
		} else {
			return err
		}
	}
	return nil
}

func (fwc *FirewallController) ilbFirewallSrcRange(gceIngresses []*v1.Ingress) (string, error) {
	ilbEnabled := false
	for _, ing := range gceIngresses {
		if utils.IsGCEL7ILBIngress(ing) {
			ilbEnabled = true
			break
		}
	}

	if ilbEnabled {
		L7ILBSrcRange, err := features.ILBSubnetSourceRange(fwc.ctx.Cloud, fwc.ctx.Cloud.Region())
		if err != nil {
			return "", err
		}
		return L7ILBSrcRange, nil
	}

	return "", ErrNoILBIngress
}

func (fwc *FirewallController) getCustomHealthCheckPorts(svcPorts []utils.ServicePort) []string {
	var result []string

	for _, svcPort := range svcPorts {
		if svcPort.BackendConfig != nil && svcPort.BackendConfig.Spec.HealthCheck != nil && svcPort.BackendConfig.Spec.HealthCheck.Port != nil {
			result = append(result, strconv.FormatInt(*svcPort.BackendConfig.Spec.HealthCheck.Port, 10))
		}
	}

	return result
}
