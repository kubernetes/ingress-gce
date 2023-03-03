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

SCRIPT_ROOT=$(cd "$(dirname ${BASH_SOURCE})" && pwd)
INGRESS_GCE_REPO_ROOT=$(cd "${SCRIPT_ROOT}/.." && pwd)

export GOBIN="${SCRIPT_ROOT}/tools/bin"
export PATH="${GOBIN}:${PATH}"
export GOPATH="$(go env GOPATH)"

echo "Using following variables for code generation:"
echo ""
echo "SCRIPT_ROOT=${SCRIPT_ROOT}"
echo "INGRESS_GCE_REPO_ROOT=${INGRESS_GCE_REPO_ROOT}"
echo "GOPATH=${GOPATH}"
echo ""

echo 'Output files will be generated in appropriate paths inside "${GOPATH}/src"'

echo ""
echo "Installing dependencies..."

# Go code dependencies tracked using https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module
mkdir -p "${GOBIN}"
cd "${SCRIPT_ROOT}/tools"
go install "k8s.io/kube-openapi/cmd/openapi-gen" >/dev/null
OPENAPI_PKG="${GOBIN}"

# Non-Go code dependencies (like shell scripts) need to be handled separately.
cd "${GOBIN}"
rm -rf code-generator
git clone https://github.com/kubernetes/code-generator --quiet
cd code-generator
git checkout 9c63990c847dce9e6dca44ad39f7cc4e547bd55f --quiet # https://github.com/kubernetes/code-generator/releases/tag/v0.22.17
CODEGEN_PKG="${PWD}"

echo "Dependencies installed."

echo "Generating composite types"
cd "${INGRESS_GCE_REPO_ROOT}"
go run "pkg/composite/gen/main.go"

echo "Performing code generation for BackendConfig CRD"
${CODEGEN_PKG}/generate-groups.sh \
  "deepcopy,client,informer,lister" \
  k8s.io/ingress-gce/pkg/backendconfig/client k8s.io/ingress-gce/pkg/apis \
  "backendconfig:v1beta1 backendconfig:v1" \
  --go-header-file ${SCRIPT_ROOT}/boilerplate.go.txt

echo "Generating openapi for BackendConfig v1beta1"
${OPENAPI_PKG}/openapi-gen \
  --output-file-base zz_generated.openapi \
  --input-dirs k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1 \
  --output-package k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1 \
  --go-header-file ${SCRIPT_ROOT}/boilerplate.go.txt

echo "Generating openapi for BackendConfig v1"
${OPENAPI_PKG}/openapi-gen \
  --output-file-base zz_generated.openapi \
  --input-dirs k8s.io/ingress-gce/pkg/apis/backendconfig/v1 \
  --output-package k8s.io/ingress-gce/pkg/apis/backendconfig/v1 \
  --go-header-file ${SCRIPT_ROOT}/boilerplate.go.txt

echo "Performing code generation for FrontendConfig CRD"
${CODEGEN_PKG}/generate-groups.sh \
  "deepcopy,client,informer,lister" \
  k8s.io/ingress-gce/pkg/frontendconfig/client k8s.io/ingress-gce/pkg/apis \
  "frontendconfig:v1beta1" \
  --go-header-file ${SCRIPT_ROOT}/boilerplate.go.txt

echo "Generating openapi for FrontendConfig v1beta1"
${OPENAPI_PKG}/openapi-gen \
  --output-file-base zz_generated.openapi \
  --input-dirs k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1 \
  --output-package k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1 \
  --go-header-file ${SCRIPT_ROOT}/boilerplate.go.txt

echo "Performing code generation for ServiceNetworkEndpointGroup CRD"
${CODEGEN_PKG}/generate-groups.sh \
  "deepcopy,client,informer,lister" \
  k8s.io/ingress-gce/pkg/svcneg/client k8s.io/ingress-gce/pkg/apis \
  "svcneg:v1beta1" \
  --go-header-file ${SCRIPT_ROOT}/boilerplate.go.txt

echo "Generating openapi for ServiceNetworkEndpointGroup v1beta1"
${OPENAPI_PKG}/openapi-gen \
  --output-file-base zz_generated.openapi \
  --input-dirs k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1 \
  --output-package k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1 \
  --go-header-file ${SCRIPT_ROOT}/boilerplate.go.txt

echo "Performing code generation for ServiceAttachment CRD"
${CODEGEN_PKG}/generate-groups.sh \
  "deepcopy,client,informer,lister" \
  k8s.io/ingress-gce/pkg/serviceattachment/client k8s.io/ingress-gce/pkg/apis \
  "serviceattachment:v1beta1,v1" \
  --go-header-file ${SCRIPT_ROOT}/boilerplate.go.txt

echo "Generating openapi for ServiceAttachment v1beta1"
${OPENAPI_PKG}/openapi-gen \
  --output-file-base zz_generated.openapi \
  --input-dirs k8s.io/ingress-gce/pkg/apis/serviceattachment/v1beta1 \
  --output-package k8s.io/ingress-gce/pkg/apis/serviceattachment/v1beta1 \
  --go-header-file ${SCRIPT_ROOT}/boilerplate.go.txt

echo "Generating openapi for ServiceAttachment v1"
${OPENAPI_PKG}/openapi-gen \
  --output-file-base zz_generated.openapi \
  --input-dirs k8s.io/ingress-gce/pkg/apis/serviceattachment/v1 \
  --output-package k8s.io/ingress-gce/pkg/apis/serviceattachment/v1 \
  --go-header-file ${SCRIPT_ROOT}/boilerplate.go.txt

echo "Performing code generation for GCPIngressParams CRD"
${CODEGEN_PKG}/generate-groups.sh \
  "deepcopy,informer,lister" \
  k8s.io/ingress-gce/pkg/ingparams/client k8s.io/ingress-gce/pkg/apis \
  "ingparams:v1beta1" \
  --go-header-file ${SCRIPT_ROOT}/boilerplate.go.txt

# Separate client generation to overwrite default Plural name
${CODEGEN_PKG}/generate-groups.sh \
  "client, informer, lister" \
  k8s.io/ingress-gce/pkg/ingparams/client k8s.io/ingress-gce/pkg/apis \
  "ingparams:v1beta1" \
  --go-header-file ${SCRIPT_ROOT}/boilerplate.go.txt \
  --plural-exceptions=GCPIngressParams:GCPIngressParams

echo "Generating openapi for GCPIngressParams v1beta1"
${OPENAPI_PKG}/openapi-gen \
  --output-file-base zz_generated.openapi \
  --input-dirs k8s.io/ingress-gce/pkg/apis/ingparams/v1beta1 \
  --output-package k8s.io/ingress-gce/pkg/apis/ingparams/v1beta1 \
  --go-header-file ${SCRIPT_ROOT}/boilerplate.go.txt
