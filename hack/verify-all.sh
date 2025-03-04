#!/bin/bash
#
# Copyright 2022 The Kubernetes Authors.
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

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." &> /dev/null && pwd -P)"
cd "${REPO_ROOT}"

hack/verify-gofmt.sh
hack/verify-govet.sh
# TODO: re-enable this after g1.24 linters work in this project
echo "Linters temporarily disabled, please run \`hack/verify-lint.sh\` manually using go version <1.24 to verify"
# hack/verify-lint.sh
hack/verify-codegen.sh
hack/verify-vendor.sh
