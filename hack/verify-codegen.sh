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

SCRIPT_ROOT=$(dirname "${BASH_SOURCE}")/..
_tmp="$(mktemp -d -t "ingress-gce.XXXXXX")"

cleanup() {
 git worktree remove -f "${_tmp}"
}

trap "cleanup" EXIT SIGINT

git worktree add -f -q "${_tmp}" HEAD
cd "${_tmp}"

# Update generated code
hack/update-codegen.sh

# Test for diffs
diffs=$(git status --porcelain | wc -l)
if [[ ${diffs} -gt 0 ]]; then
  git status >&2
  git diff >&2
  echo "Generated files need to be updated" >&2
  echo "Please run 'hack/update-codegen.sh'" >&2
  exit 1
fi

echo "Generated files are up to date"
echo