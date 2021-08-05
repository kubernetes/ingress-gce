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

package endpointslices

import (
	"errors"
	"fmt"

	discovery "k8s.io/api/discovery/v1beta1"
	"k8s.io/client-go/tools/cache"
)

func FormatEndpointSlicesServiceKey(namespace, name string) string {
	if len(namespace) > 0 {
		return fmt.Sprintf("%s/%s", namespace, name)
	}
	return name
}

/*
Prepares Endpoint Slices Service key.
*/
func EndpointSlicesServiceKey(slice *discovery.EndpointSlice) (string, error) {
	serviceName, ok := slice.Labels[discovery.LabelServiceName]
	if !ok {
		return "", errors.New(fmt.Sprintf("Failed to find a service label inside endpoint slice %v", slice))
	}
	// Check if serviceName contains a namespace
	serviceNamespace, serviceName, err := cache.SplitMetaNamespaceKey(serviceName)
	if err != nil {
		return "", errors.New(fmt.Sprintf("Failed to find a service label inside endpoint slice %v", slice))
	}
	if len(serviceNamespace) > 0 || len(slice.GetNamespace()) == 0 {
		return FormatEndpointSlicesServiceKey(serviceNamespace, serviceName), nil
	}
	return FormatEndpointSlicesServiceKey(slice.GetNamespace(), serviceName), nil
}
