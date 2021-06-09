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

# **NOTE** When adding new CRDs. Make sure to include the new rbac permissions in docs/deploy/resources/rbac.yaml

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname ${BASH_SOURCE})/..
CODEGEN_PKG=${CODEGEN_PKG:-$(cd ${SCRIPT_ROOT}; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}
OPENAPI_PKG=k8s.io/kube-openapi

echo "Generating composite types"
go run ${SCRIPT_ROOT}/pkg/composite/gen/main.go

echo "Performing code generation for BackendConfig CRD"
${CODEGEN_PKG}/generate-groups.sh \
  "deepcopy,client,informer,lister" \
  k8s.io/ingress-gce/pkg/backendconfig/client k8s.io/ingress-gce/pkg/apis \
  "backendconfig:v1beta1 backendconfig:v1" \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt

echo "Generating openapi for BackendConfig v1beta1"
go install ${OPENAPI_PKG}/cmd/openapi-gen
${GOPATH}/bin/openapi-gen \
  --output-file-base zz_generated.openapi \
  --input-dirs k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1\
  --output-package k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1 \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt

echo "Generating openapi for BackendConfig v1"
go install ${OPENAPI_PKG}/cmd/openapi-gen
${GOPATH}/bin/openapi-gen \
  --output-file-base zz_generated.openapi \
  --input-dirs k8s.io/ingress-gce/pkg/apis/backendconfig/v1\
  --output-package k8s.io/ingress-gce/pkg/apis/backendconfig/v1 \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt

echo "Performing code generation for FrontendConfig CRD"
${CODEGEN_PKG}/generate-groups.sh \
  "deepcopy,client,informer,lister" \
  k8s.io/ingress-gce/pkg/frontendconfig/client k8s.io/ingress-gce/pkg/apis \
  "frontendconfig:v1beta1" \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt

echo "Generating openapi for FrontendConfig v1beta1"
go install ${OPENAPI_PKG}/cmd/openapi-gen
${GOPATH}/bin/openapi-gen \
  --output-file-base zz_generated.openapi \
  --input-dirs k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1\
  --output-package k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1 \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt

echo "Performing code generation for ServiceNetworkEndpointGroup CRD"
${CODEGEN_PKG}/generate-groups.sh \
  "deepcopy,client,informer,lister" \
  k8s.io/ingress-gce/pkg/svcneg/client k8s.io/ingress-gce/pkg/apis \
  "svcneg:v1beta1" \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt

echo "Generating openapi for ServiceNetworkEndpointGroup v1beta1"
go install ${OPENAPI_PKG}/cmd/openapi-gen
${GOPATH}/bin/openapi-gen \
  --output-file-base zz_generated.openapi \
  --input-dirs k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1\
  --output-package k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1 \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt

echo "Performing code generation for ServiceAttachment CRD"
${CODEGEN_PKG}/generate-groups.sh \
  "deepcopy,client,informer,lister" \
  k8s.io/ingress-gce/pkg/serviceattachment/client k8s.io/ingress-gce/pkg/apis \
  "serviceattachment:v1beta1" \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt

echo "Generating openapi for ServiceAttachment v1beta1"
go install ${OPENAPI_PKG}/cmd/openapi-gen
${GOPATH}/bin/openapi-gen \
  --output-file-base zz_generated.openapi \
  --input-dirs k8s.io/ingress-gce/pkg/apis/serviceattachment/v1beta1\
  --output-package k8s.io/ingress-gce/pkg/apis/serviceattachment/v1beta1 \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt

echo "Performing code generation for GCPIngressParams CRD"
${CODEGEN_PKG}/generate-groups.sh \
  "deepcopy,informer,lister" \
  k8s.io/ingress-gce/pkg/ingparams/client k8s.io/ingress-gce/pkg/apis \
  "ingparams:v1beta1" \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt \

# Separate client generation to overwrite default Plural name
${CODEGEN_PKG}/generate-groups.sh \
  "client, informer, lister" \
  k8s.io/ingress-gce/pkg/ingparams/client k8s.io/ingress-gce/pkg/apis \
  "ingparams:v1beta1" \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt \
  --plural-exceptions=GCPIngressParams:GCPIngressParams

echo "Generating openapi for GCPIngressParams v1beta1"
go install ${OPENAPI_PKG}/cmd/openapi-gen
${GOPATH}/bin/openapi-gen \
  --output-file-base zz_generated.openapi \
  --input-dirs k8s.io/ingress-gce/pkg/apis/ingparams/v1beta1\
  --output-package k8s.io/ingress-gce/pkg/apis/ingparams/v1beta1 \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt
