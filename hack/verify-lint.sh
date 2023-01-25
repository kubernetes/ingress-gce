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

# Set gobin here, install dependencies after this
cd "hack/tools"
export GOBIN=$PWD/bin
export PATH=$GOBIN:$PATH
# Install golangci-lint
echo "Installing golangci-lint"
echo
go install github.com/golangci/golangci-lint/cmd/golangci-lint > /dev/null
cd "../.."

export GOLANGCI_LINT_CACHE=$PWD/.cache
echo -n "Checking linters: "
ERRS=$(golangci-lint run ./... 2>&1 || true)
if [ -n "${ERRS}" ]; then
    echo "FAIL"
    echo "${ERRS}"
    echo
    exit 1
fi
rm -rf $GOLANGCI_LINT_CACHE
echo "PASS"
echo
