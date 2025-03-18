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
    clusterLocation=$(echo ${selfLink} | sed 's+.*/locations/\([-a-z0-9]*\)/.*+\1+')
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
    exit 1
fi
if [ -z  "${clusters}" ]; then
    echo "ERROR: No cluster '${clusterName}' found"
    exit 1
fi

parseCluster ${clusters}


zone_regex="^[a-z]+-[a-z]+[0-9]-[a-z]$"
region_regex="^[a-z]+-[a-z]+[0-9]$"

# Find running instance and instanceGroupZone
if [[ ${zone} =~ $zone_regex ]]; then
    echo "Zonal cluster: ${zone}"
    instanceGroupUrl=$(gcloud  container  clusters describe "$clusterName" --zone "$zone" --format='value(instanceGroupUrls)' | awk -F";" '{print $NF}')
    if [ -z "${instanceGroupUrl}" ]; then
        echo "ERROR: No instance group for cluster"
        exit 1;
    fi
    # Get instance group name from the URL
    instanceGroupName=$(echo "${instanceGroupUrl}" | awk -F"/" '{print $NF}')
    # Get zone of the instance group from the URL
    instanceGroupZone=$(echo "${instanceGroupUrl}" | awk -F"/" '{print $(NF-2)}')
    instance=$(gcloud compute instance-groups list-instances "${instanceGroupName}" --zone "${instanceGroupZone}" --format='value(instance)' --filter='status:RUNNING' | head -n 1)

elif [[ ${clusterLocation} =~ $region_regex ]]; then
    echo "Regional cluster: ${clusterLocation}"
    instanceGroupUrls=$(gcloud container clusters describe $clusterName --region $clusterLocation --format='value(instanceGroupUrls)')
    instance=""
    instanceGroupZone=""
    while read -r igUrl; do
        igName=$(echo ${igUrl} | awk -F"/" '{print $NF}')
        # Extract zone from the instance group URL
        igZone=$(echo ${igUrl} | sed 's+.*/zones/\([^/]*\)/.*+\1+')
        # List instances in the current instance group
        running_instances=$(gcloud compute instance-groups list-instances ${igName} --zone ${igZone} --format='value(instance)' --filter='status:RUNNING')
        if [ ! -z "${running_instances}" ]; then
            # Pick the first running instance found
            instance=$(echo "${running_instances}" | head -n 1)
            instanceGroupZone=${igZone}
            break
        fi
    done <<< "$(echo ${instanceGroupUrls} | tr ' ' '\n')"
else
    echo "ERROR: Location '${clusterLocation}' is not valid"
fi

if [ -z "${instance}" ]; then
    echo "ERROR: No running instance found for cluster ${clusterName}"
    exit 1;
fi

nodeTag=$(gcloud compute instances describe "${instance}" --zone "${instanceGroupZone}" --format='value(tags.items[0])')


# VM instances names created by gke are truncated up to 24 characters
nodesPrefix=$(echo "gke-$clusterName" | head -c 24)


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
local-zone = ${instanceGroupZone}
EOF

echo "Run glbc with hack/run-local-glbc.sh"
