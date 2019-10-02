#!/bin/bash

function usage() {
  echo "Usage: ./generate.sh -n CLUSTER -z ZONE [-p PROJECT]"
  echo
  echo "Generates gce.conf used by glbc.yaml"
  echo
  echo "  -p, --project-name       Name of the project (Optional)"
  echo "  -n, --cluster-name       Name of the cluster (Required)"
  echo "  -z, --zone               Zone the cluster is in (Required)"
  echo "  --help                   Display this help and exit"
  exit
}

function arg_check {
  # Check that the necessary arguments were provided and that they are correct.
  if [[ -z "$ZONE" || -z "$CLUSTER_NAME" ]];
  then
    usage
  fi
}

while [[ $# -gt 0 ]]
do
key="$1"
case $key in
  -h|--help)
  usage
  shift
  shift
  ;;
  -n|--cluster-name)
  CLUSTER_NAME=$2
  shift
  shift
  ;;
  -p|--project-name)
  PROJECT_ID=$2
  shift
  shift
  ;;
  -z|--zone)
  ZONE=$2
  shift
  shift
  ;;
  *)
  echo "Unknown argument $1"
  echo
  usage
  ;;
esac
done

if [[ -z $PROJECT_ID ]]; then 
  # Get the project id associated with the cluster.
  PROJECT_ID=`gcloud config list --format 'value(core.project)' 2>/dev/null`
fi

arg_check

# Populate gce.conf.gen from our template.
if [[ -z $NETWORK_NAME ]]; then
  NETWORK_NAME=$(basename $(gcloud container clusters describe $CLUSTER_NAME --project $PROJECT_ID --zone=$ZONE \
      --format='value(networkConfig.network)'))
fi
if [[ -z $SUBNETWORK_NAME ]]; then
  SUBNETWORK_NAME=$(basename $(gcloud container clusters describe $CLUSTER_NAME --project $PROJECT_ID \
      --zone=$ZONE --format='value(networkConfig.subnetwork)'))
fi

# Getting network tags is painful. Get the instance groups, map to an instance,
# and get the node tag from it (they should be the same across all nodes -- we don't
# know how to handle it, otherwise).
if [[ -z $NETWORK_TAGS ]]; then
  INSTANCE_GROUP=$(gcloud container clusters describe $CLUSTER_NAME --project $PROJECT_ID --zone=$ZONE \
      --format='flattened(nodePools[].instanceGroupUrls[].scope().segment())' | \
      cut -d ':' -f2 | tr -d '[:space:])
  INSTANCE=$(gcloud compute instance-groups list-instances $INSTANCE_GROUP --project $PROJECT_ID \
      --zone=$ZONE --format="value(instance)" --limit 1)
  NETWORK_TAGS=$(gcloud compute instances describe $INSTANCE --project $PROJECT_ID --format="value(tags.items)")
fi

sed "s/\[PROJECT\]/$PROJECT_ID/" gce.conf.temp | \
sed "s/\[NETWORK\]/$NETWORK_NAME/" | \
sed "s/\[SUBNETWORK\]/$SUBNETWORK_NAME/" | \
sed "s/\[CLUSTER_NAME\]/$CLUSTER_NAME/" | \
sed "s/\[NETWORK_TAGS\]/$NETWORK_TAGS/" | \
sed "s/\[ZONE\]/$ZONE/" > gce.conf

echo "Generated gce.conf for cluster: $CLUSTER_NAME"
