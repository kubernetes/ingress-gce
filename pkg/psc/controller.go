/*
Copyright 2021 The Kubernetes Authors.

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
package psc

import (
	context2 "context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	alpha "google.golang.org/api/compute/v0.alpha"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/ingress-gce/pkg/annotations"
	sav1alpha1 "k8s.io/ingress-gce/pkg/apis/serviceattachment/v1alpha1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/psc/metrics"
	serviceattachmentclient "k8s.io/ingress-gce/pkg/serviceattachment/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/patch"
	sautils "k8s.io/ingress-gce/pkg/utils/serviceattachment"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/util/slice"
	"k8s.io/legacy-cloud-providers/gce"
)

func init() {
	// register prometheus metrics
	metrics.RegisterMetrics()
}

const (
	svcKind = "service"

	// SvcAttachmentGCError is the service attachment GC error event reason
	SvcAttachmentGCError = "ServiceAttachmentGCError"
	// ServiceAttachmentFinalizer used by the psc controller to ensure Service Attachment CRs
	// are deleted after the corresponding Service Attachments are deleted
	ServiceAttachmentFinalizerKey = "networking.gke.io/service-attachment-finalizer"

	// ServiceAttachmentGCPeriod is the interval at which Service Attachment GC will run
	ServiceAttachmentGCPeriod = 2 * time.Minute
)

// Controller is a private service connect (psc) controller
// It watches ServiceAttachment resources and creates, deletes, and manages
// corresponding GCE Service Attachment resources
type Controller struct {
	client kubernetes.Interface

	cloud              *gce.Cloud
	saClient           serviceattachmentclient.Interface
	svcAttachmentQueue workqueue.RateLimitingInterface

	saNamer             namer.ServiceAttachmentNamer
	svcAttachmentLister cache.Indexer
	serviceLister       cache.Indexer
	recorder            func(string) record.EventRecorder
	collector           metrics.PSCMetricsCollector

	hasSynced func() bool
}

func NewController(ctx *context.ControllerContext) *Controller {
	saNamer := namer.NewServiceAttachmentNamer(ctx.ClusterNamer, string(ctx.KubeSystemUID))
	controller := &Controller{
		client:              ctx.KubeClient,
		cloud:               ctx.Cloud,
		saClient:            ctx.SAClient,
		saNamer:             saNamer,
		svcAttachmentLister: ctx.SAInformer.GetIndexer(),
		svcAttachmentQueue:  workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		serviceLister:       ctx.ServiceInformer.GetIndexer(),
		hasSynced:           ctx.HasSynced,
		recorder:            ctx.Recorder,
		collector:           ctx.ControllerMetrics,
	}

	ctx.SAInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueServiceAttachment,
		UpdateFunc: func(old, cur interface{}) {
			controller.enqueueServiceAttachment(cur)
		},
	})
	return controller
}

// Run waits for the initial sync and will process keys in the queue and run GC
// until signaled
func (c *Controller) Run(stopChan <-chan struct{}) {
	wait.PollUntil(5*time.Second, func() (bool, error) {
		klog.V(2).Infof("Waiting for initial sync")
		return c.hasSynced(), nil
	}, stopChan)

	klog.V(2).Infof("Starting private service connect controller")
	defer func() {
		klog.V(2).Infof("Shutting down private service connect controller")
		c.svcAttachmentQueue.ShutDown()
	}()

	go wait.Until(func() { c.serviceAttachmentWorker(stopChan) }, time.Second, stopChan)

	go func() {
		// Wait a GC period before starting to ensure that resources have enough time to sync
		time.Sleep(ServiceAttachmentGCPeriod)
		wait.Until(c.garbageCollectServiceAttachments, ServiceAttachmentGCPeriod, stopChan)
	}()

	<-stopChan
}

// serviceAttachmentWorker keeps processing service attachment keys in the queue
// until stopChan has been signaled
func (c *Controller) serviceAttachmentWorker(stopChan <-chan struct{}) {
	processKey := func() {
		key, quit := c.svcAttachmentQueue.Get()
		if quit {
			return
		}
		defer c.svcAttachmentQueue.Done(key)
		err := c.processServiceAttachment(key.(string))
		c.handleErr(err, key)
	}

	for {
		select {
		case <-stopChan:
			return
		default:
			processKey()
		}
	}
}

// handleErr will check for an error and report it as an event on the provided
// service attachment cr
func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		c.svcAttachmentQueue.Forget(key)
		return
	}
	eventMsg := fmt.Sprintf("error processing service attachment %q: %q", key, err)
	klog.Errorf(eventMsg)
	if obj, exists, err := c.svcAttachmentLister.GetByKey(key.(string)); err != nil {
		klog.Warningf("failed to retrieve service attachment %q from the store: %q", key.(string), err)
	} else if exists {
		svcAttachment := obj.(*sav1alpha1.ServiceAttachment)
		c.recorder(svcAttachment.Namespace).Eventf(svcAttachment, v1.EventTypeWarning, "ProcessServiceAttachmentFailed", eventMsg)
	}
	c.svcAttachmentQueue.AddRateLimited(key)
}

// enqueueServiceAttachment adds the service attachment object to the queue
func (c *Controller) enqueueServiceAttachment(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Errorf("Failed to generate service attachment key: %q", err)
		return
	}
	c.svcAttachmentQueue.Add(key)
}

// processServiceAttachment will process a service attachment key and will gather all
// information required (forwarding rule and subnet URLs) and create and update
// corresponding GCE Service Attachments. If provided a key that does not exist in the
// store, processServiceAttachment will return with no error
func (c *Controller) processServiceAttachment(key string) error {
	start := time.Now()
	// NOTE: Error will be used to send metrics about whether the sync loop was successful
	// Please reuse and set err before returning
	var err error
	defer func() {
		metrics.PublishPSCProcessMetrics(metrics.SyncProcess, err, start)
		metrics.PublishLastProcessTimestampMetrics(metrics.SyncProcess)
		c.collector.SetServiceAttachment(key, metrics.PSCState{InSuccess: err == nil})
	}()

	var namespace, name string
	namespace, name, err = cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	obj, exists, err := c.svcAttachmentLister.GetByKey(key)
	if err != nil {
		return fmt.Errorf("errored getting service from store: %q", err)
	}

	if !exists {
		// Allow Garbage Collection to Delete Service Attachment
		klog.V(2).Infof("Service attachment %s/%s does not exist in store. Will be cleaned up by GC", namespace, name)
		return nil
	}
	klog.V(2).Infof("Processing Service attachment %s/%s", namespace, name)

	svcAttachment := obj.(*sav1alpha1.ServiceAttachment)
	var updatedCR *sav1alpha1.ServiceAttachment
	updatedCR, err = c.ensureSAFinalizer(svcAttachment)
	if err != nil {
		return fmt.Errorf("Errored adding finalizer on ServiceAttachment CR %s/%s: %s", namespace, name, err)
	}
	if err = validateResourceReference(updatedCR.Spec.ResourceRef); err != nil {
		return err
	}

	frURL, err := c.getForwardingRule(namespace, updatedCR.Spec.ResourceRef.Name)
	if err != nil {
		return fmt.Errorf("failed to find forwarding rule: %q", err)
	}

	subnetURLs, err := c.getSubnetURLs(updatedCR.Spec.NATSubnets)
	if err != nil {
		return fmt.Errorf("failed to find nat subnets: %q", err)
	}

	saName := c.saNamer.ServiceAttachment(namespace, name, string(updatedCR.UID))
	desc := sautils.ServiceAttachmentDesc{URL: updatedCR.SelfLink}
	gceSvcAttachment := &alpha.ServiceAttachment{
		ConnectionPreference:   svcAttachment.Spec.ConnectionPreference,
		Name:                   saName,
		NatSubnets:             subnetURLs,
		ProducerForwardingRule: frURL,
		Region:                 c.cloud.Region(),
		Description:            desc.String(),
	}

	gceSAKey, err := composite.CreateKey(c.cloud, saName, meta.Regional)
	if err != nil {
		return fmt.Errorf("failed to create key for GCE Service Attachment: %q", err)
	}

	existingSA, err := c.cloud.Compute().AlphaServiceAttachments().Get(context2.Background(), gceSAKey)
	if err != nil && !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		return fmt.Errorf("failed querying for GCE Service Attachment: %q", err)
	}

	if existingSA != nil {
		klog.V(4).Infof("Found existing service attachment %s", existingSA.Name)
		err = validateUpdate(existingSA, gceSvcAttachment)
		if err != nil {
			return fmt.Errorf("invalid Service Attachment Update: %q", err)
		}
		klog.V(4).Infof("Finished processing service attachment %s/%s", namespace, name)
		return nil
	}

	klog.V(2).Infof("Creating service attachment %s", saName)
	if err = c.cloud.Compute().AlphaServiceAttachments().Insert(context2.Background(), gceSAKey, gceSvcAttachment); err != nil {
		return fmt.Errorf("failed to create GCE Service Attachment: %q", err)
	}
	klog.V(2).Infof("Created service attachment %s", saName)

	_, err = c.updateServiceAttachmentStatus(updatedCR, gceSAKey)
	klog.V(2).Infof("Updated Service Attachment %s/%s status", updatedCR.Namespace, updatedCR.Name)
	return err
}

// garbageCollectServiceAttachments queries for all Service Attachments CR that have been marked
// for deletion and will delete the corresponding GCE Service Attachment resource. If the GCE
// resource has successfully been deleted, the finalizer is removed from the service attachment
// cr.
func (c *Controller) garbageCollectServiceAttachments() {
	klog.V(2).Infof("Staring Service Attachment Garbage Collection")
	defer func() {
		klog.V(2).Infof("Finished Service Attachment Garbage Collection")
		metrics.PublishLastProcessTimestampMetrics(metrics.GCProcess)
	}()
	crs := c.svcAttachmentLister.List()
	for _, obj := range crs {
		sa := obj.(*sav1alpha1.ServiceAttachment)
		if sa.GetDeletionTimestamp().IsZero() {
			continue
		}
		key, err := cache.MetaNamespaceKeyFunc(sa)
		if err != nil {
			klog.V(4).Infof("failed to generate key for service attachment: %s/%s: %w", sa.Namespace, sa.Name, err)
		} else {
			c.collector.DeleteServiceAttachment(key)
		}
		c.deleteServiceAttachment(sa)
	}
}

// deleteServiceAttachment attemps to delete the GCE Service Attachment resource
// that corresponds to the provided CR. If successful, the finalizer on the CR
// will be removed.
func (c *Controller) deleteServiceAttachment(sa *sav1alpha1.ServiceAttachment) {
	start := time.Now()
	// NOTE: Error will be used to send metrics about whether the sync loop was successful
	// Please reuse and set err before returning
	var err error
	resourceID, err := cloud.ParseResourceURL(sa.Status.ServiceAttachmentURL)
	var gceName string
	if err != nil {
		klog.Errorf("failed to parse service attachment url %s/%s: %s", sa.Namespace, sa.Name, err)
	} else {
		gceName = resourceID.Key.Name
	}

	defer metrics.PublishPSCProcessMetrics(metrics.GCProcess, err, start)

	// If the name was not found from the ServiceAttachmentURL generate it from the service
	// attachment CR. Since the CR's UID is used, the name found will be unique to this CR
	// and can safely be deleted if found.
	if gceName == "" {
		klog.V(2).Infof("could not find name from service attachment url on %s/%s, generating name based on CR", sa.Namespace, sa.Name)
		gceName = c.saNamer.ServiceAttachment(sa.Namespace, sa.Name, string(sa.UID))
	}

	klog.V(2).Infof("Deleting Service Attachment %s", gceName)
	if err = c.ensureDeleteGCEServiceAttachment(gceName); err != nil {
		eventMsg := fmt.Sprintf("Failed to Garbage Collect Service Attachment %s/%s: %q", sa.Namespace, sa.Name, err)
		klog.Errorf(eventMsg)
		c.recorder(sa.Namespace).Eventf(sa, v1.EventTypeWarning, SvcAttachmentGCError, eventMsg)
		return
	}
	klog.V(2).Infof("Deleted Service Attachment %s", gceName)

	klog.V(2).Infof("Removing finalizer on Service Attachment %s/%s", sa.Namespace, sa.Name)
	if err = c.ensureSAFinalizerRemoved(sa); err != nil {
		eventMsg := fmt.Sprintf("Failed to remove finalizer on ServiceAttachment %s/%s: %q", sa.Namespace, sa.Name, err)
		klog.Errorf(eventMsg)
		c.recorder(sa.Namespace).Eventf(sa, v1.EventTypeWarning, SvcAttachmentGCError, eventMsg)
		return
	}
	klog.V(2).Infof("Removed finalizer on Service Attachment %s/%s", sa.Namespace, sa.Name)
}

// getForwardingRule returns the URL of the forwarding rule based by using the service resource
// and querying GCE. On ILB subsetting services, the forwarding rule annotation is used to find
// the forwarding rule name. Otherwise the name is generated based on the service resource.
func (c *Controller) getForwardingRule(namespace, svcName string) (string, error) {

	svcKey := fmt.Sprintf("%s/%s", namespace, svcName)
	obj, exists, err := c.serviceLister.GetByKey(svcKey)
	if err != nil {
		return "", fmt.Errorf("errored getting service %s/%s: %q", namespace, svcName, err)
	}

	if !exists {
		return "", fmt.Errorf("failed to get Service %s/%s", namespace, svcName)
	}

	svc := obj.(*v1.Service)

	// Check for annotation that has forwarding rule name on the service resource by looking for
	// the TCP or UDP key. If it exists, then use the value as the forwarding rule name.
	frName, ok := svc.Annotations[annotations.TCPForwardingRuleKey]
	if !ok {
		if frName, ok = svc.Annotations[annotations.UDPForwardingRuleKey]; !ok {
			// The annotation only exists for ILB Subsetting LBs. If no annotation exists, fallback
			// to finding the name by regenerating the name using the svc resource
			frName = cloudprovider.DefaultLoadBalancerName(svc)
			klog.V(2).Infof("no forwarding rule annotation exists on %s/%s, falling back to autogenerated forwarding rule name: %s", svc.Namespace, svc.Name, frName)
		}
	}
	fwdRule, err := c.cloud.Compute().ForwardingRules().Get(context2.Background(), meta.RegionalKey(frName, c.cloud.Region()))
	if err != nil {
		return "", fmt.Errorf("failed to get Forwarding Rule %s: %q", frName, err)
	}

	// Verify that the forwarding rule found has the IP expected in Service.Status
	foundMatchingIP := false
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		if ing.IP == fwdRule.IPAddress {
			klog.V(2).Infof("verified %s has matching ip to service %s/%s", frName, svc.Namespace, svc.Name)
			foundMatchingIP = true
			break
		}
	}

	if foundMatchingIP {
		return fwdRule.SelfLink, nil
	}
	return "", fmt.Errorf("forwarding rule does not have matching IPAddr")
}

// getSubnetURLs will query GCE and gather all the URLs of the provided subnet names
func (c *Controller) getSubnetURLs(subnets []string) ([]string, error) {

	var subnetURLs []string
	for _, subnetName := range subnets {
		subnet, err := c.cloud.Compute().Subnetworks().Get(context2.Background(), meta.RegionalKey(subnetName, c.cloud.Region()))
		if err != nil {
			return subnetURLs, fmt.Errorf("failed to find Subnetwork %s/%s: %q", c.cloud.Region(), subnetName, err)
		}
		subnetURLs = append(subnetURLs, subnet.SelfLink)

	}
	return subnetURLs, nil
}

// updateServiceAttachmentStatus updates the CR's status with the GCE Service Attachment URL
// and the producer forwarding rule
func (c *Controller) updateServiceAttachmentStatus(cr *sav1alpha1.ServiceAttachment, gceSAKey *meta.Key) (*sav1alpha1.ServiceAttachment, error) {
	gceSA, err := c.cloud.Compute().AlphaServiceAttachments().Get(context2.Background(), gceSAKey)
	if err != nil {
		return cr, fmt.Errorf("failed to query GCE Service Attachment: %q", err)
	}

	updatedSA := cr.DeepCopy()
	updatedSA.Status.ServiceAttachmentURL = gceSA.SelfLink
	updatedSA.Status.ForwardingRuleURL = gceSA.ProducerForwardingRule

	klog.V(2).Infof("Updating Service Attachment %s/%s status", cr.Namespace, cr.Name)
	return c.patchServiceAttachment(cr, updatedSA)
}

// patchServiceAttachment patches the originalSA CR to the desired updatedSA CR
func (c *Controller) patchServiceAttachment(originalSA, updatedSA *sav1alpha1.ServiceAttachment) (*sav1alpha1.ServiceAttachment, error) {
	patchBytes, err := patch.MergePatchBytes(originalSA, updatedSA)
	if err != nil {
		return originalSA, err
	}
	return c.saClient.NetworkingV1alpha1().ServiceAttachments(originalSA.Namespace).Patch(context2.Background(), updatedSA.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
}

// ensureGCEDeleteServiceAttachment deletes the GCE Service Attachment resource with provided
// name. BadRequest or NotFound errors are ignored and imply the service attachment
// resource does not exist
func (c *Controller) ensureDeleteGCEServiceAttachment(name string) error {
	saKey, err := composite.CreateKey(c.cloud, name, meta.Regional)
	if err != nil {
		return fmt.Errorf("failed to create key for service attachment %q", name)
	}
	_, err = c.cloud.Compute().AlphaServiceAttachments().Get(context2.Background(), saKey)
	if err != nil {
		if utils.IsHTTPErrorCode(err, http.StatusNotFound) || utils.IsHTTPErrorCode(err, http.StatusBadRequest) {
			return nil
		}
		return fmt.Errorf("failed querying for service attachment %q: %q", name, err)
	}

	return c.cloud.Compute().AlphaServiceAttachments().Delete(context2.Background(), saKey)
}

// ensureSAFinalizer ensures that the Service Attachment finalizer exists on the provided
// CR. If it does not, the CR will be patched with the finalizer
func (c *Controller) ensureSAFinalizer(saCR *sav1alpha1.ServiceAttachment) (*sav1alpha1.ServiceAttachment, error) {
	if len(saCR.Finalizers) != 0 {
		for _, finalizer := range saCR.Finalizers {
			if finalizer == ServiceAttachmentFinalizerKey {
				return saCR, nil
			}
		}
	}

	updatedCR := saCR.DeepCopy()

	if updatedCR.Finalizers == nil {
		updatedCR.Finalizers = []string{}
	}
	updatedCR.Finalizers = append(updatedCR.Finalizers, ServiceAttachmentFinalizerKey)
	return c.patchServiceAttachment(saCR, updatedCR)
}

// ensureSAFinalizerRemoved ensures that the Service Attachment finalizer is removed
// from the provided CR.
func (c *Controller) ensureSAFinalizerRemoved(cr *sav1alpha1.ServiceAttachment) error {
	updatedCR := cr.DeepCopy()
	updatedCR.Finalizers = slice.RemoveString(updatedCR.Finalizers, ServiceAttachmentFinalizerKey, nil)
	_, err := c.patchServiceAttachment(cr, updatedCR)
	return err
}

// validateResourceReference will validate that the provided resource reference is
// for a K8s Service
func validateResourceReference(ref v1.TypedLocalObjectReference) error {
	if ref.APIGroup != nil && *ref.APIGroup != "" {
		return fmt.Errorf("invalid resource reference: %s, apiGroup must be emptry or nil", *ref.APIGroup)
	}

	if strings.ToLower(ref.Kind) != svcKind {
		return fmt.Errorf("invalid resource reference %s, kind must be %q", ref.Kind, svcKind)
	}
	return nil
}

// validateUpdate will validate whether ServiceAttachment matches the GCE Service Attachment
// resource. If not, validateUpdate will return an error, since GCE Service Attachments cannot
// be updated after creation
func validateUpdate(existingSA, desiredSA *alpha.ServiceAttachment) error {
	if existingSA.ConnectionPreference != desiredSA.ConnectionPreference {
		return fmt.Errorf("serviceAttachment connection preference cannot be updated from %s to %s", existingSA.ConnectionPreference, desiredSA.ConnectionPreference)
	}

	existingFR, err := cloud.ParseResourceURL(existingSA.ProducerForwardingRule)
	if err != nil {
		return fmt.Errorf("serviceAttachment existing forwarding rule has malformed URL: %q", err)
	}
	desiredFR, err := cloud.ParseResourceURL(desiredSA.ProducerForwardingRule)
	if err != nil {
		return fmt.Errorf("serviceAttachment desired forwarding rule has malformed URL: %q", err)
	}
	if !reflect.DeepEqual(existingFR, desiredFR) {
		return fmt.Errorf("serviceAttachment forwarding rule cannot be updated from %s to %s", existingSA.ProducerForwardingRule, desiredSA.ProducerForwardingRule)
	}

	if len(existingSA.NatSubnets) != len(desiredSA.NatSubnets) {
		return fmt.Errorf("serviceAttachment NAT Subnets cannot be updated")
	} else {
		subnets := make(map[string]*cloud.ResourceID)
		for _, subnet := range existingSA.NatSubnets {
			existingSN, err := cloud.ParseResourceURL(subnet)
			if err != nil {
				return fmt.Errorf("serviceAttachment existing subnet has malformed URL: %q", err)
			}
			subnets[existingSN.Key.Name] = existingSN

			for _, subnet := range desiredSA.NatSubnets {
				desiredSN, err := cloud.ParseResourceURL(subnet)
				if err != nil {
					return fmt.Errorf("serviceAttachment desired subnet has malformed URL: %q", err)
				}

				if !reflect.DeepEqual(subnets[desiredSN.Key.Name], desiredSN) {
					return fmt.Errorf("serviceAttachment NAT Subnets cannot be updated, found new subnet: %s", desiredSN.Key.Name)
				}
			}
		}
	}
	return nil
}

// SvcAttachmentKeyFunc provides the service attachment key used
// by the svcAttachmentLister
func SvcAttachmentKeyFunc(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}
