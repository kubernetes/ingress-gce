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

ALL_PHASES=(composite backendconfig frontendconfig svcneg providerconfig serviceattachment)
PHASES_TO_RUN=()
UPDATE_VIOLATIONS=false
RUN_GEN_HELPERS=true
RUN_GEN_REGISTER=true
RUN_GEN_CLIENT=true
RUN_GEN_OPENAPI=true

usage() {
    echo "Usage: $0 [options] [phase...]"
    echo ""
    echo "Options:"
    echo "  --update-api-known-violations   Update the API known violations files."
    echo "  --no-gen-helpers                Disable running gen_helpers."
    echo "  --no-gen-register               Disable running gen_register."
    echo "  --no-gen-client                 Disable running gen_client."
    echo "  --no-gen-openapi                Disable running gen_openapi."
    echo ""
    echo "If no phases are specified, all phases will be run."
    echo "Available phases: ${ALL_PHASES[*]}"
    exit 1
}

# Parse arguments
while [[ "$#" -gt 0 ]]; do
    case "$1" in
        -h|--help|help)
            usage
            ;;
        --update-api-known-violations)
            UPDATE_VIOLATIONS=true
            shift
            ;;
        --no-gen-helpers)
            RUN_GEN_HELPERS=false
            shift
            ;;
        --no-gen-register)
            RUN_GEN_REGISTER=false
            shift
            ;;
        --no-gen-client)
            RUN_GEN_CLIENT=false
            shift
            ;;
        --no-gen-openapi)
            RUN_GEN_OPENAPI=false
            shift
            ;;
        *)
            # check if arg is a valid phase
            found=0
            for phase in "${ALL_PHASES[@]}"; do
                if [[ "$phase" == "$1" ]]; then
                    found=1
                    break
                fi
done

            if [[ ${found} -eq 0 ]]; then
                echo "Error: Invalid phase '$1'"
                usage
            fi
            PHASES_TO_RUN+=("$1")
            shift
            ;;
    esac
done

if [ ${#PHASES_TO_RUN[@]} -eq 0 ]; then
    PHASES_TO_RUN=("${ALL_PHASES[@]}")
fi

should_run() {
    local phase_name="$1"
    for phase in "${PHASES_TO_RUN[@]}"; do
        if [ "$phase" == "$phase_name" ]; then
            return 0
        fi
    done
    return 1
}

generate_for_api() {
  # $1: api name (e.g. backendconfig)
  # $2: client package path (e.g. pkg/backendconfig)
  # $3: versions (comma-separated) (e.g. v1beta1)
  local api_name="$1"
  local client_package="$2"
  local versions_csv="$3"

  local apis_root="pkg/apis"
  local go_pkg_root="k8s.io/ingress-gce"

  echo "[API] ${api_name}, client package: ${client_package}, versions: ${versions_csv}"

  local api_packages=()
  IFS=',' read -ra versions <<< "$versions_csv"
  for version in "${versions[@]}"; do
    api_packages+=("${apis_root}/${api_name}/${version}")
  done

  if [[ "${RUN_GEN_HELPERS}" == "true" ]]; then
    echo "[GEN] gen_helpers"
    for api_package in "${api_packages[@]}"; do
      echo "[GEN] helpers for ${api_package}..."
      kube::codegen::gen_helpers \
        --boilerplate "${BOILERPLATE_TXT}" \
        "${api_package}"
    done
  fi

  if [[ "${RUN_GEN_REGISTER}" == "true" ]]; then
    echo "[GEN] gen_register"
    for api_package in "${api_packages[@]}"; do
      echo "[GEN] register for ${api_package}..."
      kube::codegen::gen_register \
        --boilerplate "${BOILERPLATE_TXT}" \
        "${api_package}"
    done
  fi

  if [[ "${RUN_GEN_CLIENT}" == "true" ]]; then
    echo "[GEN] client for ${api_name}..."
    kube::codegen::gen_client \
      --boilerplate "${BOILERPLATE_TXT}" \
      --with-watch \
      --output-dir "${client_package}" \
      --output-pkg "${go_pkg_root}/${client_package}" \
      --one-input-api "${api_name}" \
      "${apis_root}"
  fi

  local update_report_arg=""
  if [[ "${UPDATE_VIOLATIONS}" == "true" ]]; then
    update_report_arg="--update-report"
  fi

  if [[ "${RUN_GEN_OPENAPI}" == "true" ]]; then
    echo "[GEN] gen_openapi"

    for api_package in "${api_packages[@]}"; do
      echo "[GEN] openapi for ${api_package}..."
      local report_filename="hack/${api_package//\//-}.openapi-violations.txt"
      echo "[GEN] - using violations file '${report_filename}'"
      kube::codegen::gen_openapi \
        --boilerplate "${BOILERPLATE_TXT}" \
        --output-dir "${api_package}" \
        --output-pkg "k8s.io/ingress-gce/${api_package}" \
        --report-filename "${report_filename:-/dev/null}" \
        ${update_report_arg} \
        "${api_package}"
    done
  fi
}

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SCRIPT_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
BOILERPLATE_TXT="${ROOT}/hack/boilerplate.go.txt"

export GOBIN="${SCRIPT_ROOT}/tools/bin"
export PATH="${GOBIN}:${PATH}"
GOPATH="$(go env GOPATH)"
export GOPATH

echo "Using following variables for code generation:"
echo ""
echo "ROOT=${ROOT}"
echo "SCRIPT_ROOT=${SCRIPT_ROOT}"
echo "GOPATH=${GOPATH}"
echo ""

CODEGEN=k8s.io/code-generator@v0.31.12
CODEGEN_PKG="$(go env GOMODCACHE)/${CODEGEN}"
CODEGEN_SCRIPT="${CODEGEN_PKG}/kube_codegen.sh"
echo "Using codegen script ${CODEGEN_SCRIPT}"

if [[ ! -e "${CODEGEN_SCRIPT}" ]]; then
	echo "Installing code generator..."
	go get "${CODEGEN}"
fi
# shellcheck disable=SC1090
source "${CODEGEN_SCRIPT}"

echo ""
echo ""

if should_run "composite"; then
  echo "[GEN] composite types"
  pushd "${ROOT}" >/dev/null
  go run "pkg/composite/gen/main.go"
  popd >/dev/null
fi

if should_run "backendconfig"; then
  generate_for_api "backendconfig" "pkg/backendconfig/client" "v1beta1,v1"
fi

if should_run "frontendconfig"; then
  generate_for_api "frontendconfig" "pkg/frontendconfig/client" "v1beta1"
fi

if should_run "svcneg"; then
  generate_for_api "svcneg" "pkg/svcneg/client" "v1beta1"
fi

if should_run "providerconfig"; then
  generate_for_api "providerconfig" "pkg/providerconfig/client" "v1"
fi

if should_run "serviceattachment"; then
  generate_for_api "serviceattachment" "pkg/serviceattachment/client" "v1beta1,v1"
fi
