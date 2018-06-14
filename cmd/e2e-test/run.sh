#!/bin/bash
#
# Copyright 2018 The Kubernetes Authors.
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

# run.sh manages the settings required for running containerized in a
# Kubernetes cluster.

echo '--- BEGIN ---'
PROJECT=$(curl -H'Metadata-Flavor:Google' metadata.google.internal/computeMetadata/v1/project/project-id 2>/dev/null)
echo "PROJECT: ${PROJECT}"
CMD="/e2e-test -test.v -test.parallel=10 -run -project ${PROJECT} -logtostderr -inCluster -v=2"
echo "CMD: ${CMD}" $@
${CMD} "$@" 2>&1
echo "RESULT: $?"
echo '--- END ---'
