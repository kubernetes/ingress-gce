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

package serviceextension

import (
	"fmt"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"k8s.io/ingress-gce/pkg/annotations"
	serviceextensionv1alpha1 "k8s.io/ingress-gce/pkg/apis/serviceextension/v1alpha1"
)

// GetServicesForServiceExtension returns all services that reference the given
// serviceExtension.
func GetServicesForServiceExtension(svcLister cache.Indexer, svcExt *serviceextensionv1alpha1.ServiceExtension) []*apiv1.Service {
	svcs := []*apiv1.Service{}
	for _, obj := range svcLister.List() {
		svc := obj.(*apiv1.Service)
		if svc.Namespace != svcExt.Namespace ||
			annotations.FromService(svc).ServiceExtensionName() != svcExt.Name {
			continue
		}
		svcs = append(svcs, svc)
	}
	return svcs
}

// GetServiceExtensionForService returns the serviceExtension (if any) that is
// referenced the given service.
func GetServiceExtensionForService(svcExtLister cache.Indexer, svc *apiv1.Service) (*serviceextensionv1alpha1.ServiceExtension, error) {
	svcExtName := annotations.FromService(svc).ServiceExtensionName()
	if svcExtName == "" {
		return nil, nil
	}
	obj, exists, err := svcExtLister.Get(
		&serviceextensionv1alpha1.ServiceExtension{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcExtName,
				Namespace: svc.Namespace,
			},
		})
	if !exists {
		return nil, fmt.Errorf("serviceExtension %v/%v not found in store", svc.Namespace, svcExtName)
	}
	if err != nil {
		return nil, err
	}
	return obj.(*serviceextensionv1alpha1.ServiceExtension), nil
}
