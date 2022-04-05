#!/bin/sh
#
# Copyright 2016 The Kubernetes Authors.
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

if [ -z "${PKG}" ]; then
    echo "PKG must be set"
    exit 1
fi
if [ -z "${ARCH}" ]; then
    echo "ARCH must be set"
    exit 1
fi
if [ -z "${VERSION}" ]; then
    echo "VERSION must be set"
    exit 1

fi
if [ -z "${GIT_COMMIT}" ]; then
    echo "GIT_COMMIT must be set"
    exit 1
fi

export CGO_ENABLED=0
export GOARCH="${ARCH}"
if [ $GOARCH == "amd64" ]; then
    export GOBIN="$GOPATH/bin/linux_amd64"
fi

BIN_PKG="$PKG/cmd/$(basename ${TARGET})"
LD_FLAGS="-X ${PKG}/pkg/version.Version=${VERSION} -X ${PKG}/pkg/version.GitCommit=${GIT_COMMIT}"

if echo "${TARGET}" | grep '.*-test$'; then
  go test -c -ldflags "${LD_FLAGS}" -o "${TARGET}" "${BIN_PKG}"
else
  go install -installsuffix "static" -ldflags "${LD_FLAGS}" "${BIN_PKG}"
fi
