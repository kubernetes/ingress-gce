#!/bin/bash

# Copyright 2023 The Kubernetes Authors.
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
set -o xtrace

export GOPATH="$(go env GOPATH)"
KUBE_REPO_ROOT=$GOPATH/src/k8s.io/kubernetes
REPO_ROOT=$(git rev-parse --show-toplevel)
cd ${REPO_ROOT}

# Setup our cleanup function; as we allocate resources we set a variable to indicate they should be cleaned up
function cleanup {
  if [[ "${CLEANUP_BOSKOS:-}" == "true" ]]; then
    cleanup_boskos
  fi
  # shellcheck disable=SC2153
  if [[ "${DELETE_CLUSTER:-}" == "true" ]]; then
      kubetest2 ${KUBETEST2_ARGS} --down || echo "kubetest2 down failed"
  fi
}
trap cleanup EXIT

# Ensure we have a project; get one from boskos if one not provided in GCP_PROJECT
source "${REPO_ROOT}"/hack/boskos.sh
if [[ -z "${GCP_PROJECT:-}" ]]; then
  echo "GCP_PROJECT not set, acquiring project from boskos"
  acquire_project
  CLEANUP_BOSKOS="true"
fi
echo "GCP_PROJECT=${GCP_PROJECT}"

# IMAGE_REPO is used to upload images
if [[ -z "${IMAGE_REPO:-}" ]]; then
  IMAGE_REPO="gcr.io/${GCP_PROJECT}"
fi
echo "IMAGE_REPO=${IMAGE_REPO}"

cd ${REPO_ROOT}
export KO_DOCKER_REPO="${IMAGE_REPO}"
if [[ -z "${IMAGE_TAG:-}" ]]; then
  IMAGE_TAG=$(git rev-parse --short HEAD)-$(date +%Y%m%dT%H%M%S)
fi

export GCE_GLBC_IMAGE=$(go run github.com/google/ko@v0.12.0 build --tags ${IMAGE_TAG} --base-import-paths --push=true ./cmd/glbc/)
echo "GCE_GLBC_IMAGE=${GCE_GLBC_IMAGE}"

go install sigs.k8s.io/kubetest2@latest
go install sigs.k8s.io/kubetest2/kubetest2-gce@latest
go install sigs.k8s.io/kubetest2/kubetest2-tester-ginkgo@latest
kubetest2 gce -v 2 \
--repo-root=${KUBE_REPO_ROOT} \
--gcp-project=${GCP_PROJECT} \
--ingress-gce-image=${GCE_GLBC_IMAGE} \
--legacy-mode \
--build \
--up \
--down \
--test=ginkgo \
-- \
--focus-regex='\[Feature:NEG\]|Loadbalancing|LoadBalancers|Ingress' \
--skip-regex='\[Feature:kubemci\]|\[Disruptive\]|\[Feature:IngressScale\]|\[Feature:NetworkPolicy\]' \
--use-built-binaries
