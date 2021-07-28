#/bin/bash

# Setup the environment for running the e2e tests from your
# desktop.

set -e

parseCluster() {
    # These are all globals.
    net=$1
    subnet=$2
    zone=$3
    selfLink=$4
    net=$(echo ${net} | sed 's+.*networks/\([-a-z0-9]*\).*$+\1+')
    subnet=$(echo ${subnet} | sed 's+.*subnetworks/\([-a-z0-9]*\)$+\1+')
    project=$(echo ${selfLink} | sed 's+.*/projects/\([-a-z0-9]*\)/.*+\1+')
}

parseInstance() {
    local name=$1
    local zone=$2
    # Globals.
    nodeTag=$(gcloud compute instances describe ${name} --zone ${zone} --format='value(tags.items[0])')
}

clusterName="$1"
clusterLocation="$2"

if [ -z "${clusterName}" ]; then
    echo "Usage: $0 CLUSTER_NAME [LOCATION]"
    echo
    echo "LOCATION is optional if there is only one cluster with CLUSTER_NAME"
    exit 1
fi

fmt='value(networkConfig.network,networkConfig.subnetwork,zone,selfLink,name)'
if [ -z "$clusterLocation" ]; then
    clusters=$(gcloud container clusters list --format="${fmt}" --filter="name=${clusterName}")
else
    clusters=$(gcloud container clusters list --format="${fmt}" --filter="name=${clusterName} location=${clusterLocation}")
fi
if [ $(echo "${cluster}" | wc -l) -gt 1 ]; then
    echo "ERROR: more than one cluster matches '${clusterName}'"
fi
parseCluster ${clusters}
if [ -z  "${clusters}" ]; then
    echo "ERROR: No cluster '${clusterName}' found"
    exit 1
fi

instance=$(gcloud compute instances list --format='value(name,zone)' | grep ${clusterName} | tail -n 1)
parseInstance ${instance}
if [ -z  "${instance}" ]; then
    echo "ERROR: No nodes matching '${clusterName}' found"
    exit 1
fi

gceConf="/tmp/gce.conf"
echo "Writing ${gceConf}"
echo "----"
cat <<EOF |  tee ${gceConf}
[global]
token-url = nil
project-id = ${project}
network-name = ${net}
subnetwork-name = ${subnet}
node-instance-prefix = ${clusterName}
node-tags = ${nodeTag}
local-zone = ${zone}
EOF

echo "Run glbc with hack/run-glbc.sh"
