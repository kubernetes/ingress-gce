// Copyright 2018 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mapper

import (
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
)

// ClusterServiceMapper is an interface to intuitively map an Ingress to the
// Services it defines for a specific cluster,
type ClusterServiceMapper interface {
	Services(ing *v1beta1.Ingress) (map[v1beta1.IngressBackend]v1.Service, error)
}
