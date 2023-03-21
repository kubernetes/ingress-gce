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
REPO_ROOT=$(git rev-parse --show-toplevel)
cd ${REPO_ROOT}

# Setup our cleanup function; as we allocate resources we set a variable to indicate they should be cleaned up
function cleanup {
  if [[ "${CLEANUP_BOSKOS:-}" == "true" ]]; then
    cleanup_boskos
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

KO_GLBC_IMAGE=$(go run github.com/google/ko@v0.12.0 build --tags ${IMAGE_TAG} --base-import-paths --push=true ./cmd/glbc/)
# remove the sha from the image name, it does not work with gce registry per example
export GCE_GLBC_IMAGE=$(echo $KO_GLBC_IMAGE | cut -d\@ -f1)
echo "GCE_GLBC_IMAGE=${GCE_GLBC_IMAGE}"


export CUSTOM_INGRESS_YAML=$(cat <<EOF
    apiVersion: v1
    kind: Pod
    metadata:
      name: l7-lb-controller
      namespace: kube-system
      annotations:
        scheduler.alpha.kubernetes.io/critical-pod: ""
        seccomp.security.alpha.kubernetes.io/pod: "docker/default"
      labels:
        k8s-app: gcp-lb-controller
        kubernetes.io/name: "GLBC"
    spec:
      priorityClassName: system-cluster-critical
      securityContext:
        runAsUser: 0
      terminationGracePeriodSeconds: 300
      hostNetwork: true
      containers:
      - image: $GCE_GLBC_IMAGE
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8086
            scheme: HTTP
          initialDelaySeconds: 30
          # healthz reaches out to GCE
          periodSeconds: 30
          timeoutSeconds: 15
          successThreshold: 1
          failureThreshold: 5
        name: l7-lb-controller
        volumeMounts:
        - mountPath: /etc/gce.conf
          name: cloudconfig
          readOnly: true
        - mountPath: /var/log/glbc.log
          name: logfile
          readOnly: false
        - mountPath: /etc/srv/kubernetes/l7-lb-controller
          name: srvkube
          readOnly: true
        resources:
          requests:
            cpu: 10m
            memory: 50Mi
        args:
        - --v=3
        - --logtostderr=false
        - --log_file=/var/log/glbc.log
        - --enable-finalizer-remove
        - --enable-finalizer-add
        - --default-backend-service=kube-system/default-http-backend
        - --kubeconfig=/etc/srv/kubernetes/l7-lb-controller/kubeconfig
        - --sync-period=600s
        - --running-in-cluster=false
        - --config-file-path=/etc/gce.conf
        - --healthz-port=8086
        - --gce-ratelimit=ga.Operations.Get,qps,10,100
        - --gce-ratelimit=alpha.Operations.Get,qps,10,100
        - --gce-ratelimit=beta.Operations.Get,qps,10,100
        - --gce-ratelimit=ga.BackendServices.Get,qps,1.8,1
        - --gce-ratelimit=beta.BackendServices.Get,qps,1.8,1
        - --gce-ratelimit=ga.HealthChecks.Get,qps,1.8,1
        - --gce-ratelimit=alpha.HealthChecks.Get,qps,1.8,1
        - --gce-ratelimit=beta.NetworkEndpointGroups.Get,qps,1.8,1
        - --gce-ratelimit=beta.NetworkEndpointGroups.AttachNetworkEndpoints,qps,1.8,1
        - --gce-ratelimit=beta.NetworkEndpointGroups.DetachNetworkEndpoints,qps,1.8,1
        - --gce-ratelimit=beta.NetworkEndpointGroups.ListNetworkEndpoints,qps,1.8,1
        - --gce-ratelimit=ga.NetworkEndpointGroups.Get,qps,1.8,1
        - --gce-ratelimit=ga.NetworkEndpointGroups.AttachNetworkEndpoints,qps,1.8,1
        - --gce-ratelimit=ga.NetworkEndpointGroups.DetachNetworkEndpoints,qps,1.8,1
        - --gce-ratelimit=ga.NetworkEndpointGroups.ListNetworkEndpoints,qps,1.8,1
      volumes:
      - hostPath:
          path: /etc/gce.conf
          type: FileOrCreate
        name: cloudconfig
      - hostPath:
          path: /var/log/glbc.log
          type: FileOrCreate
        name: logfile
      - hostPath:
          path: /etc/srv/kubernetes/l7-lb-controller
        name: srvkube
EOF
)


/workspace/test-infra/scenarios/kubernetes_e2e.py --check-leaked-resources \
  --cluster= \
  --env=GCE_ALPHA_FEATURES=NetworkEndpointGroup \
  --env=KUBE_GCE_ENABLE_IP_ALIASES=true \
  --extract=ci/latest \
  --gcp-project="${GCP_PROJECT}" \
  --gcp-zone=us-west1-b \
  --ginkgo-parallel=1 \
  --provider=gce \
  '--test_args=--ginkgo.focus=\[Feature:Ingress\]|\[Feature:NEG\]' \
  --timeout=320m
