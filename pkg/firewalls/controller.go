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
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/cache"
	gcpfirewallv1 "k8s.io/cloud-provider-gcp/crd/apis/gcpfirewall/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/common/operator"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller/translator"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

var (
	// queueKey is a "fake" key which can be enqueued to a task queue.
	queueKey = &v1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "queueKey"},
	}

	ErrNoILBIngress  = errors.New("no ILB Ingress found")
	ErrNoRXLBIngress = errors.New("no Regional External Ingress found")
)

// FirewallController synchronizes the firewall rule for all ingresses.
type FirewallController struct {
	ctx                           *context.ControllerContext
	firewallPool                  SingleFirewallPool
	queue                         utils.TaskQueue
	translator                    *translator.Translator
	zoneGetter                    *zonegetter.ZoneGetter
	hasSynced                     func() bool
	enableIngressRegionalExternal bool
	stopCh                        <-chan struct{}

	logger klog.Logger
}

type compositeFirewallPool struct {
	pools []SingleFirewallPool
}

func (comp *compositeFirewallPool) Sync(nodeNames, additionalPorts, additionalRanges []string, allowNodePort bool) error {
	var errList []error
	for _, singlePool := range comp.pools {
		err := singlePool.Sync(nodeNames, additionalPorts, additionalRanges, allowNodePort)
		if err != nil {
			errList = append(errList, err)
		}
	}
	return utilerrors.NewAggregate(errList)

}

func (comp *compositeFirewallPool) GC() error {
	var errList []error
	for _, singlePool := range comp.pools {
		err := singlePool.GC()
		if err != nil {
			errList = append(errList, err)
		}
	}
	return utilerrors.NewAggregate(errList)
}

// NewFirewallController returns a new firewall controller.
func NewFirewallController(
	ctx *context.ControllerContext,
	portRanges []string,
	enableCR, disableFWEnforcement, enableRegionalXLB bool,
	stopCh <-chan struct{},
	logger klog.Logger,
) *FirewallController {
	logger = logger.WithName("FirewallController")
	compositeFirewallPool := &compositeFirewallPool{}
	if enableCR {
		firewallCRPool := NewFirewallCRPool(ctx.FirewallClient, ctx.Cloud, ctx.ClusterNamer, gce.L7LoadBalancerSrcRanges(), portRanges, disableFWEnforcement, logger)
		compositeFirewallPool.pools = append(compositeFirewallPool.pools, firewallCRPool)
	}
	if !disableFWEnforcement {
		firewallPool := NewFirewallPool(ctx.Cloud, ctx.ClusterNamer, gce.L7LoadBalancerSrcRanges(), portRanges, logger)
		compositeFirewallPool.pools = append(compositeFirewallPool.pools, firewallPool)
	}

	fwc := &FirewallController{
		ctx:                           ctx,
		zoneGetter:                    ctx.ZoneGetter,
		firewallPool:                  compositeFirewallPool,
		translator:                    ctx.Translator,
		hasSynced:                     ctx.HasSynced,
		enableIngressRegionalExternal: enableRegionalXLB,
		stopCh:                        stopCh,
		logger:                        logger,
	}

	fwc.queue = utils.NewPeriodicTaskQueue("", "firewall", fwc.sync, logger)

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

	if enableCR {
		// FW CRs will be updated/deleted by the PFW controller or the user. Ingress controller need to watch such events
		// and act accordingly.
		ctx.FirewallInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			// We enqueue the sync task for all update events for these two reasons:
			// 1. Make sure the CR is consistent with the FW configuration.
			// 2. Force the controller to read the CR status and populate proper errors.
			UpdateFunc: func(old, cur interface{}) {
				curFW := cur.(*gcpfirewallv1.GCPFirewall)
				name := fwc.ctx.ClusterNamer.FirewallRule()
				if name != curFW.Name {
					return
				}
				fwc.queue.Enqueue(queueKey)
			},
			// In case the firewall CR is deleted accidentally, we need to reconcile the firewall CR.
			// If no ingress is left, nothing will be added.
			DeleteFunc: func(obj interface{}) {
				fwc.queue.Enqueue(queueKey)
			},
		})
	}

	return fwc
}

// ToSvcPorts is a helper method over translator.TranslateIngress to process a list of ingresses.
// TODO(rramkumar): This is a copy of code in controller.go. Extract this into
// something shared.
func (fwc *FirewallController) ToSvcPorts(ings []*v1.Ingress) []utils.ServicePort {
	var knownPorts []utils.ServicePort
	for _, ing := range ings {
		urlMap, _, _ := fwc.translator.TranslateIngress(ing, fwc.ctx.DefaultBackendSvcPort.ID, fwc.ctx.ClusterNamer)
		knownPorts = append(knownPorts, urlMap.AllServicePorts()...)
	}
	return knownPorts
}

func (fwc *FirewallController) Run() {
	defer func() {
		fwc.logger.Info("Shutting down firewall controller")
		fwc.shutdown()
		fwc.logger.Info("Firewall controller shut down")
	}()
	fwc.logger.Info("Starting firewall controller")
	go fwc.queue.Run()
	<-fwc.stopCh
}

// This should only be called when the process is being terminated.
func (fwc *FirewallController) shutdown() {
	fwc.logger.Info("Shutting down Firewall Controller")
	fwc.queue.Shutdown()
}

func (fwc *FirewallController) sync(key string) error {
	if !fwc.hasSynced() {
		time.Sleep(context.StoreSyncPollPeriod)
		return fmt.Errorf("waiting for stores to sync")
	}
	fwc.logger.V(3).Info("Syncing firewall")

	gceIngresses := operator.Ingresses(fwc.ctx.Ingresses().List()).Filter(func(ing *v1.Ingress) bool {
		return utils.IsGCEIngress(ing)
	}).AsList()

	// If there are no more ingresses, then delete the firewall rule.
	if len(gceIngresses) == 0 {
		if err := fwc.firewallPool.GC(); err != nil {
			fwc.logger.Error(err, "Could not garbage collect firewall pool, got error")
		}
		return nil
	}

	// gceSvcPorts contains the ServicePorts used by only single-cluster ingress.
	gceSvcPorts := fwc.ToSvcPorts(gceIngresses)
	nodes, err := fwc.zoneGetter.ListNodes(zonegetter.CandidateNodesFilter, fwc.logger)
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
	if fwc.enableIngressRegionalExternal {
		rxlbRange, err := fwc.rxlbFirewallSrcRange(gceIngresses)
		fwc.logger.Info("fwc.rxlbFirewallsSrcRange", "rxlbRange", rxlbRange, "err", err)
		if err != nil {
			if err != features.ErrSubnetNotFound && err != ErrNoRXLBIngress {
				return err
			}
		} else {
			additionalRanges = append(additionalRanges, rxlbRange)
		}
	}

	var additionalPorts []string
	hcPorts := fwc.getCustomHealthCheckPorts(gceSvcPorts)
	additionalPorts = append(additionalPorts, hcPorts...)
	additionalPorts = append(additionalPorts, negPorts...)

	// Ensure firewall rule for the cluster and pass any NEG endpoint ports.
	if err := fwc.firewallPool.Sync(utils.GetNodeNames(nodes), additionalPorts, additionalRanges, needNodePort); err != nil {
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
		L7ILBSrcRange, err := features.ILBSubnetSourceRange(fwc.ctx.Cloud, fwc.ctx.Cloud.Region(), fwc.logger)
		if err != nil {
			return "", err
		}
		return L7ILBSrcRange, nil
	}

	return "", ErrNoILBIngress
}

func (fwc *FirewallController) rxlbFirewallSrcRange(gceIngresses []*v1.Ingress) (string, error) {
	rxlbEnabled := false
	for _, ing := range gceIngresses {
		if utils.IsGCEL7XLBRegionalIngress(ing) {
			rxlbEnabled = true
			klog.Infof("Found Regional XLB Enabled on ingress %s/%s, requires regional xlb firewall.", ing.Namespace, ing.Name)
			break
		}
	}

	if rxlbEnabled {
		rxlbSourceRange, err := features.RXLBSubnetSourceRange(fwc.ctx.Cloud, fwc.ctx.Cloud.Region(), fwc.logger)
		if err != nil {
			return "", err
		}
		return rxlbSourceRange, nil
	}

	return "", ErrNoRXLBIngress
}

func (fwc *FirewallController) getCustomHealthCheckPorts(svcPorts []utils.ServicePort) []string {
	var result []string

	for _, svcPort := range svcPorts {
		if svcPort.BackendConfig != nil && svcPort.BackendConfig.Spec.HealthCheck != nil && svcPort.BackendConfig.Spec.HealthCheck.Port != nil {
			result = append(result, strconv.FormatInt(*svcPort.BackendConfig.Spec.HealthCheck.Port, 10))
		}
	}
	if flags.F.EnableTransparentHealthChecks {
		result = append(result, strconv.FormatInt(int64(flags.F.THCPort), 10))
	}

	return result
}
