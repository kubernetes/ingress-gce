/*
Copyright 2019 The Kubernetes Authors.
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

package namer

// BackendNamer is an interface to name GCE backend resources. It wraps backend
// naming policy of namer.Namer.
type BackendNamer interface {
	// IGBackend constructs the name for a backend service targeting instance groups.
	IGBackend(nodePort int64) string
	// NEG returns the gce neg name based on the service namespace, name
	// and target port.
	NEG(namespace, name string, Port int32) string
	// InstanceGroup constructs the name for an Instance Group.
	InstanceGroup() string
	// NamedPort returns the name for a named port.
	NamedPort(port int64) string
	// NameBelongsToCluster checks if a given backend resource name is tagged with
	// this cluster's UID.
	NameBelongsToCluster(resourceName string) bool
}
