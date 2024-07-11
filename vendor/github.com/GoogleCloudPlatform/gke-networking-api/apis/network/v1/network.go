/*
Copyright 2024 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package v1

// DefaultNetworkIfEmpty takes a string corresponding to a network name and makes
// sure that if it is empty then it is set to the default network. This comes
// from the idea that a network is like a namespace, where an empty network is
// the same as the default. Use before comparisons of networks.
func DefaultNetworkIfEmpty(s string) string {
	if s == "" {
		return DefaultPodNetworkName
	}
	return s
}

// IsDefaultNetwork takes a network name and returns if it is a default network.
// Both DefaultNetworkName and DefaultPodNetworkName are considered as default network for compatibility purposes.
// DefaultNetworkName will eventually be removed.
func IsDefaultNetwork(networkName string) bool {
	return networkName == DefaultNetworkName || networkName == DefaultPodNetworkName
}

// InUse is true if the network is referenced by NetworkInterface or Pod objects.
func (n *Network) InUse() bool {
	if n.Annotations == nil {
		return false
	}
	val, ok := n.Annotations[NetworkInUseAnnotationKey]
	return ok && val == NetworkInUseAnnotationValTrue
}
