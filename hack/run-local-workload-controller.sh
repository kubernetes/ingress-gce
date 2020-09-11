#!/bin/bash

# Run workload-controller. First run `setup-local.sh` to set things up.
#
# Files touched: /tmp/kubectl-proxy.log /tmp/workload-controller.log

GOOGLE_APPLICATION_CREDENTIALS="${HOME}/.config/gcloud/application_default_credentials.json"

if [ ! -r ${GOOGLE_APPLICATION_CREDENTIALS} ]; then
    echo "You must login your application default credentials"
    echo "$ gcloud auth application-default login"
    exit 1
fi

GCECONF=${GCECONF:-/tmp/gce.conf}
WLC=${WLC:-./workload-controller}
PORT=${PORT:-7127}
V=${V:-3}

echo "GCECONF=${GCECONF} WLC=${WLC} PORT=${PORT} V=${V}"

if [ ! -x "${WLC}" ]; then
    echo "ERROR: No ${WLC} executable found" >&2
    exit 1
fi

echo "$(date) start" >> /tmp/kubectl-proxy.log
kubectl proxy --port="${PORT}" \
    >> /tmp/kubectl-proxy.log &

PROXY_PID=$!
cleanup() {
    echo "Killing proxy (pid=${PROXY_PID})"
    kill ${PROXY_PID}
}
trap cleanup EXIT

sleep 2 # Wait for proxy to start up
${WLC} \
    --apiserver-host=http://localhost:${PORT} \
    --running-in-cluster=false \
    --logtostderr --v=${V} \
    --config-file-path=${GCECONF} \
    2>&1 | tee -a /tmp/workload-controller.log
