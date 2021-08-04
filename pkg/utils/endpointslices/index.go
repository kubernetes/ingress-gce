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
	"fmt"

	discovery "k8s.io/api/discovery/v1beta1"
)

const (
	// The name of the by service index in the EndpointSlices.
	EndpointSlicesByServiceIndex = "by-service-index"
)

/*
Prepares Endpoint Slices by Service index key.
The key is just normal formatting "namespace/name".
*/
func EndpointSlicesByServiceKey(namespace, serviceName string) string {
	return fmt.Sprintf("%s/%s", namespace, serviceName)
}

/*
Index function. If applied for an EndpointSlice with an associated service
it returns properly formatted service name with a namespace as a key.
Otherwise returns an empty keys array which means that the object will not
be indexed.
*/
func EndpointSlicesByServiceFunc(obj interface{}) ([]string, error) {
	es, ok := obj.(*discovery.EndpointSlice)
	if !ok {
		return []string{}, nil
	}
	serviceName, ok := es.Labels[discovery.LabelServiceName]
	if !ok {
		return []string{}, nil
	}
	return []string{EndpointSlicesByServiceKey(es.Namespace, serviceName)}, nil
}
