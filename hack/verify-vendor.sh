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

SCRIPT_ROOT=$(dirname "${BASH_SOURCE}")/..
_tmp="$(mktemp -d -t "ingress-gce.XXXXXX")"

cleanup() {
 git worktree remove -f "${_tmp}"
}

trap "cleanup" EXIT SIGINT

git worktree add -f -q "${_tmp}" HEAD
cd "${_tmp}"

# Update Vendor 
go mod vendor

# Test for diffs
diffs=$(git status --porcelain | wc -l)
if [[ ${diffs} -gt 0 ]]; then
  git status >&2
  git diff >&2
  echo "Vendor directory needs to be updated" >&2
  echo "Please run 'go mod vendor'" >&2
  exit 1
fi

echo "Vendor directory is up to date"
echo
