#!/bin/bash

# Copyright 2017 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname ${BASH_SOURCE})/..
CODEGEN_PKG=${CODEGEN_PKG:-$(cd ${SCRIPT_ROOT}; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}
CONTROLLER_TOOLS_PKG=${CONTROLLER_TOOLS_PKG:-$(cd ${SCRIPT_ROOT}; ls -d -a ./vendor/sigs.k8s.io/controller-tools 2>/dev/null || echo ../../sigs.k8s.io/controller-tools)}

${CODEGEN_PKG}/generate-groups.sh \
  "deepcopy,client,informer,lister" \
  k8s.io/ingress-gce/pkg/backendconfig/client k8s.io/ingress-gce/pkg/apis \
  cloud:v1beta1 \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt

echo "Generate CRD's via Kubebuilder"
go run ${CONTROLLER_TOOLS_PKG}/cmd/controller-gen/main.go all
