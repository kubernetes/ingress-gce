/*
Copyright 2025 The Kubernetes Authors.

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

package l4lb

import (
	"reflect"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/annotations"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// svcNEGEventHandler is a handler used to trigger service processing on update
// of a related ServiceNetworkEndpointGroup.
// It should be added as an event handler to the ServiceNetworkEndpointGroup informer.
// Then it will enqueue a matching service to a given queue, if the service is matched by the svcFilterFunc.
type svcNEGEventHandler struct {
	logger          klog.Logger
	ServiceInformer cache.SharedIndexInformer
	svcQueue        utils.TaskQueue
	svcFilterFunc   func(*v1.Service) bool
}

func (s *svcNEGEventHandler) OnAdd(obj interface{}, isInInitialList bool) {
	// This function is directly executed by the event handler routine,
	// it should not do heavy computation but rather focus on short checks and enqueuing.
	svcNEG, ok := obj.(*negv1beta1.ServiceNetworkEndpointGroup)
	if !ok {
		s.logger.Error(nil, "ServiceNetworkEndpointGroup invalid type, got %+v", obj)
		return
	}
	svcKeys := getSvcOwnersOfSvcNEG(svcNEG, s.logger)
	for _, svcKey := range svcKeys {
		svcObj, exists, err := s.ServiceInformer.GetStore().GetByKey(svcKey)
		if err != nil {
			s.logger.Error(err, "ServiceNetworkEndpointGroup getting owner service", "svcKey", svcKey, "svcNeg", klog.KObj(svcNEG))
			continue
		}
		if !exists {
			s.logger.V(3).Info("ServiceNetworkEndpointGroup owner service not found", "svcKey", svcKey, "svcNeg", klog.KObj(svcNEG))
			continue
		}
		svc, ok := svcObj.(*v1.Service)
		if !ok {
			continue
		}
		isHandledByTheL4Controller := s.svcFilterFunc(svc)
		if !isHandledByTheL4Controller {
			continue
		}
		svcLogger := s.logger.WithValues("serviceKey", svcKey)
		svcLogger.V(3).Info("NEGs added for service, triggering resync")
		s.svcQueue.Enqueue(svc)
	}
}
func (s *svcNEGEventHandler) OnUpdate(old, cur interface{}) {
	// This function is directly executed by the event handler routine,
	// it should not do heavy computation but rather focus on short checks and enqueuing.
	curSvcNEG, ok := cur.(*negv1beta1.ServiceNetworkEndpointGroup)
	if !ok {
		s.logger.Error(nil, "ServiceNetworkEndpointGroup invalid type, got %+v", cur)
		return
	}
	oldSvcNEG, ok := old.(*negv1beta1.ServiceNetworkEndpointGroup)
	if !ok {
		s.logger.V(3).Info("ServiceNetworkEndpointGroup invalid type, got %+v", old)
		return
	}
	svcKeys := getSvcOwnersOfSvcNEG(curSvcNEG, s.logger)
	for _, svcKey := range svcKeys {
		obj, exists, err := s.ServiceInformer.GetStore().GetByKey(svcKey)
		if err != nil {
			s.logger.Error(err, "ServiceNetworkEndpointGroup getting owner service", "svcKey", svcKey, "svcNeg", klog.KObj(curSvcNEG))
			continue
		}
		if !exists {
			s.logger.Error(nil, "ServiceNetworkEndpointGroup owner service not found", "svcKey", svcKey, "svcNeg", klog.KObj(curSvcNEG))
			continue
		}
		svc, ok := obj.(*v1.Service)
		if !ok {
			continue
		}
		isHandledByTheL4Controller := s.svcFilterFunc(svc)
		if !isHandledByTheL4Controller {
			continue
		}

		oldNEGs := negReferenceListToMap(oldSvcNEG.Status.NetworkEndpointGroups)
		curNEGs := negReferenceListToMap(curSvcNEG.Status.NetworkEndpointGroups)
		if !reflect.DeepEqual(oldNEGs, curNEGs) {
			svcLogger := s.logger.WithValues("serviceKey", svcKey)
			svcLogger.V(3).Info("NEGs changed for service, triggering resync")
			s.svcQueue.Enqueue(svc)
		}
	}
}
func (s *svcNEGEventHandler) OnDelete(obj interface{}) {
	// no need for any action. The service is likely being deleted anyway.
}

func getSvcOwnersOfSvcNEG(svcNEG *negv1beta1.ServiceNetworkEndpointGroup, logger klog.Logger) []string {
	logger.V(3).Info("getSvcOwnerOfSvcNEG", "ownersLen", len(svcNEG.OwnerReferences))
	var resultKeys []string
	for _, ownersRef := range svcNEG.OwnerReferences {
		if ownersRef.Kind != "Service" {
			continue
		}
		svcName := ownersRef.Name
		svcNamespace := svcNEG.Namespace
		if svcNamespace == "" {
			svcNamespace = metav1.NamespaceDefault
		}
		svcKey := utils.ServiceKeyFunc(svcNamespace, svcName)
		resultKeys = append(resultKeys, svcKey)
	}
	return resultKeys
}

func isSubsettingILBService(svc *v1.Service) bool {
	needsILB, _ := annotations.WantsL4ILB(svc)
	return needsILB && utils.IsSubsettingL4ILBService(svc)
}

func isRBSNetLBServiceWithNEGs(svc *v1.Service) bool {
	needsNetLB, _ := annotations.WantsL4NetLB(svc)
	return needsNetLB && utils.HasL4NetLBFinalizerV3(svc)
}

func negReferenceListToMap(refs []negv1beta1.NegObjectReference) map[string]*negv1beta1.NegObjectReference {
	negRefsMap := make(map[string]*negv1beta1.NegObjectReference)
	for _, svcNEG := range refs {
		negRefsMap[svcNEG.Id] = &svcNEG
	}
	return negRefsMap
}
