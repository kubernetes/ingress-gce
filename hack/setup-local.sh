#!/bin/bash

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
    clusters=$(gcloud container clusters list --format="${fmt}" --filter="name:${clusterName}")
else
    clusters=$(gcloud container clusters list --format="${fmt}" --filter="name:${clusterName} location:${clusterLocation}")
fi
if [ "$(echo "${clusters}" | wc -l)" -gt 1 ]; then
    echo "ERROR: more than one cluster matches '${clusterName}'"
fi
parseCluster ${clusters}
if [ -z  "${clusters}" ]; then
    echo "ERROR: No cluster '${clusterName}' found"
    exit 1
fi

# VM instances names created by gke are truncated up to 24 characters
nodesPrefix=$(echo "gke-$clusterName" | head -c 24)

# Get URL of the last instance group from the instance group URLs list
instanceGroupUrl=$(gcloud  container  clusters describe "$clusterName" --zone "$zone" --format='value(instanceGroupUrls)' | awk -F";" '{print $NF}')
if [ -z "${instanceGroupUrl}" ]; then
  echo "ERROR: No instance group for cluster"
  exit 1;
fi
# Get instance group name from the URL
instanceGroupName=$(echo "${instanceGroupUrl}" | awk -F"/" '{print $NF}')
# Get zone of the instance group from the URL
instanceGroupZone=$(echo "${instanceGroupUrl}" | awk -F"/" '{print $(NF-2)}')
instance=$(gcloud compute instance-groups list-instances "${instanceGroupName}" --zone "${instanceGroupZone}" | grep  RUNNING | awk '{print $1}' | tail -n 1)
if [ -z "${instance}" ]; then
  echo "ERROR: No running instances for cluster"
  exit 1;
fi
if [ -z  "${instance}" ]; then
    echo "ERROR: No nodes matching '${clusterName}' found"
    exit 1
fi
nodeTag=$(gcloud compute instances describe "${instance}" --zone "${instanceGroupZone}" --format='value(tags.items[0])')

gceConf="/tmp/gce.conf"
echo "Writing ${gceConf}"
echo "----"
cat <<EOF |  tee ${gceConf}
[global]
token-url = nil
project-id = ${project}
network-name = ${net}
subnetwork-name = ${subnet}
node-instance-prefix = ${nodesPrefix}
node-tags = ${nodeTag}
local-zone = ${zone}
EOF

echo "Run glbc with hack/run-local-glbc.sh"
