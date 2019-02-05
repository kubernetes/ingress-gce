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
OPENAPI_PKG=${GOPATH}/src/k8s.io/kube-openapi

${CODEGEN_PKG}/generate-groups.sh \
  "deepcopy,client,informer,lister" \
  k8s.io/ingress-gce/pkg/backendconfig/client k8s.io/ingress-gce/pkg/apis \
  "backendconfig:v1beta1 backendconfig:v1" \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt

echo "Generating openapi for v1beta1"
go install ${OPENAPI_PKG}/cmd/openapi-gen
${GOPATH}/bin/openapi-gen \
  --output-file-base zz_generated.openapi \
  --input-dirs k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1\
  --output-package k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1 \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt

echo "Generating openapi for v1"
go install ${OPENAPI_PKG}/cmd/openapi-gen
${GOPATH}/bin/openapi-gen \
  --output-file-base zz_generated.openapi \
  --input-dirs k8s.io/ingress-gce/pkg/apis/backendconfig/v1\
  --output-package k8s.io/ingress-gce/pkg/apis/backendconfig/v1 \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt
