# Copyright 2018 The Kubernetes Authors.

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at

# http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

#!/bin/bash

function usage() {
  echo "Usage: ./gke-self-managed.sh -n myCluster -z myZone [options]"
  echo
  echo "  -c, --cleanup            Cleanup resources created by a previous run of the script"
  echo "  -n, --cluster-name       Name of the cluster (Required)"
  echo "  -z, --zone               Zone the cluster is in (Required)"
  echo "  --image-url              URL (with tag) of the glbc image. If not set, we will"
  echo "                           try and build and push it ourselves (set the"
  echo "                           REGISTRY env var or we'll use the project GCR)."
  echo "  --help                   Display this help and exit"
  echo "  --network-name           Override for the network-name gce.conf value"
  echo "  --subnetwork-name        Override for the subnetwork-name gce.conf value"
  echo "  --node-instance-prefix   Override for the node-instance-prefix gce.conf value"
  echo "  --network-tags           Override for the node-tags gce.conf value"
  echo "  --no-confirm             Don't ask confirmation to reenable GLBC on cleanup"
  exit
}

function arg_check {
  # Check that the necessary arguments were provided and that they are correct.
  if [[ -z "$ZONE" || -z "$CLUSTER_NAME" ]];
  then
     usage
  fi
  # Get gcloud credentials for the cluster so kubectl works automatically.
  # Any error/typo in the required command line args will be caught here.
  gcloud container clusters get-credentials ${CLUSTER_NAME} --zone=${ZONE}
  [[ $? -eq 0 ]] || error_exit "Error-bot: Command line arguments were incorrect. See above error for more info."
}

function error_exit {
  echo -e "${RED}$1${NC}" >&2
  exit 1
}

function cleanup() {
  arg_check
  # Get the project id associated with the cluster.
  PROJECT_ID=`gcloud config list --format 'value(core.project)' 2>/dev/null`
  # Cleanup k8s and GCP resources in same order they are created.
  # Note: The GCP service account key needs to be manually cleaned up.
  # Note: We don't delete the default-http-backend we created so that when the
  # GLBC is restored on the GKE master, the addon manager does not try to create a
  # new one.
  kubectl delete clusterrolebinding one-binding-to-rule-them-all
  kubectl delete -f ../resources/rbac.yaml
  kubectl delete configmap gce-config -n kube-system
  gcloud iam service-accounts delete glbc-service-account@${PROJECT_ID}.iam.gserviceaccount.com
  gcloud projects remove-iam-policy-binding ${PROJECT_ID} \
    --member serviceAccount:glbc-service-account@${PROJECT_ID}.iam.gserviceaccount.com \
    --role roles/compute.admin
  kubectl delete secret glbc-gcp-key -n kube-system
  kubectl delete -f ../resources/glbc.yaml
  # Ask if user wants to reenable GLBC on the GKE master.
  while [[ -z $NO_CONFIRM ]]; do
    echo -e "${GREEN}Script-bot: Do you want to reenable GLBC on the GKE master?${NC}"
    echo -e "${GREEN}Script-bot: Press [C | c] to continue.${NC}"
    read input
    case $input in
      [Cc]* ) break;;
      * ) echo -e "${GREEN}Script-bot: Press [C | c] to continue.${NC}"
    esac
  done
  gcloud container clusters update ${CLUSTER_NAME} --zone=${ZONE} --update-addons=HttpLoadBalancing=ENABLED
  echo -e "${GREEN}Script-bot: Cleanup successful! You need to cleanup your GCP service account key manually.${NC}"
  exit 0
}

# Loops until calls to the API server are succeeding.
function wait-for-api-server() {
  echo "Waiting for API server to become ready..."
  until kubectl api-resources > /dev/null 2>&1; do
    sleep 0.1
  done
}

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'
CLEANUP_HELP="Invoking me with the -c option will get you back to a clean slate."
NO_CLEANUP="Nothing has to be cleaned up :)"
PERMISSION_ISSUE="If this looks like a permissions problem, see the README."

# Parsing command line arguments
while [[ $# -gt 0 ]]
do
key="$1"

case $key in
  --help)
  usage
  shift
  shift
  ;;
  -c|--cleanup)
  cleanup
  shift
  shift
  ;;
  -n|--cluster-name)
  CLUSTER_NAME=$2
  shift
  shift
  ;;
  --network-name)
  NETWORK_NAME=$2
  shift
  shift
  ;;
  --subnetwork-name)
  SUBNETWORK_NAME=$2
  shift
  shift
  ;;
  --node-instance-prefix)
  NODE_INSTANCE_PREFIX=$2
  shift
  shift
  ;;
  --network-tags)
  NETWORK_TAGS=$2
  shift
  shift
  ;;
  --image-url)
  IMAGE_URL=$2
  shift
  shift
  ;;
  --no-confirm)
  NO_CONFIRM=1
  shift
  ;;
  -z|--zone)
  ZONE=$2
  shift
  ;;
  *)
  shift
  ;;
esac
done

arg_check

# Get the project id associated with the cluster.
PROJECT_ID=`gcloud config list --format 'value(core.project)' 2>/dev/null`
# Store the nodePort for default-http-backend
NODE_PORT=`kubectl get svc default-http-backend -n kube-system -o yaml | grep "nodePort:" | cut -f2- -d:`
# Get the GCP user associated with the current gcloud config.
GCP_USER=`gcloud config list --format 'value(core.account)' 2>/dev/null`

# Populate gce.conf.custom from our template.
if [[ -z $NETWORK_NAME ]]; then
  NETWORK_NAME=$(basename $(gcloud container clusters describe $CLUSTER_NAME --zone=$ZONE \
      --format='value(networkConfig.network)'))
fi
if [[ -z $SUBNETWORK_NAME ]]; then
  SUBNETWORK_NAME=$(basename $(gcloud container clusters describe $CLUSTER_NAME \
      --zone=$ZONE --format='value(networkConfig.subnetwork)'))
fi
if [[ -z $NODE_INSTANCE_PREFIX ]]; then
  NODE_INSTANCE_PREFIX="gke-${CLUSTER_NAME}"
fi

# Getting network tags is painful. Get the instance groups, map to an instance,
# and get the node tag from it (they should be the same across all nodes -- we don't
# know how to handle it, otherwise).
if [[ -z $NETWORK_TAGS ]]; then
  INSTANCE_GROUP=$(gcloud container clusters describe $CLUSTER_NAME --zone=$ZONE \
      --format='flattened(nodePools[].instanceGroupUrls[].scope().segment())' | \
      cut -d ':' -f2)
  INSTANCE=$(gcloud compute instance-groups list-instances $INSTANCE_GROUP \
      --zone=$ZONE --format="value(instance)" --limit 1)
  NETWORK_TAGS=$(gcloud compute instances describe $INSTANCE --format="value(tags.items)")
fi

echo
echo "== Using values =="
echo "Project ID: $PROJECT_ID"
echo "Network name: $NETWORK_NAME"
echo "Subnetwork name: $SUBNETWORK_NAME"
echo "Node instance prefix: $NODE_INSTANCE_PREFIX"
echo "Network tag: $NETWORK_TAGS"
echo "Zone: $ZONE"
echo

sed "s/YOUR CLUSTER'S PROJECT/$PROJECT_ID/" ../resources/gce.conf | \
sed "s/YOUR CLUSTER'S NETWORK/$NETWORK_NAME/" | \
sed "s/YOUR CLUSTER'S SUBNETWORK/$SUBNETWORK_NAME/" | \
sed "s/gke-YOUR CLUSTER'S NAME/$NODE_INSTANCE_PREFIX/" | \
sed "s/NETWORK TAGS FOR YOUR CLUSTER'S INSTANCE GROUP/$NETWORK_TAGS/" | \
sed "s/YOUR CLUSTER'S ZONE/$ZONE/" > ../resources/gce.conf.custom

# Then try to format the GLBC yaml. We will build and push the image if the user
# didn't provide a URL to use. We wanna do this before making any changes because
# the build could fail and that should be caught early.
if [[ -z $IMAGE_URL ]]; then
  echo "image-url is not set; we will build and push ourselves. This may take a few minutes."
  if [[ -z $REGISTRY ]]; then
    REGISTRY="gcr.io/$PROJECT_ID"
    echo "REGISTRY is not set. Defaulting to: $REGISTRY"
  fi
  # Extract just the URL (with tag) from the `make push` output. `sed -n` makes it
  # not print normally while the `/../../p` makes it print just what it matched.
  MAKE_PUSH_OUTPUT=$(cd ../../../; REGISTRY="$REGISTRY" make push 2>&1)
  MAKE_PUSH_CODE=$?
  if [[ $MAKE_PUSH_CODE -eq 0 ]]; then
    IMAGE_URL=$(sed -rn 's/pushing\s+:\s+(.+ingress-gce-glbc-.+)/\1/p' <<< $MAKE_PUSH_OUTPUT)
    echo "Pushed new glbc image to: $IMAGE_URL"
  else
    error_exit "Error-bot: Issue building and pushing image. Make exited with code $MAKE_PUSH_CODE. If a push error, consider setting REGISTRY or providing --image-url yourself. Output from make:\n$MAKE_PUSH_OUTPUT"
  fi
fi

sed "s|### IMAGE URL HERE ###|$IMAGE_URL|" ../resources/glbc.yaml > ../resources/glbc.yaml.custom

# Grant permission to current GCP user to create new k8s ClusterRole's.
kubectl create clusterrolebinding one-binding-to-rule-them-all --clusterrole=cluster-admin --user=${GCP_USER}
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue creating a k8s ClusterRoleBinding. ${PERMISSION_ISSUE} ${NO_CLEANUP}"

# Create a new service account for glbc and give it a
# ClusterRole allowing it access to API objects it needs.
kubectl create -f ../resources/rbac.yaml
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue creating the RBAC spec. ${CLEANUP_HELP}"

# Inject gce.conf onto the user node as a ConfigMap.
# This config map is mounted as a volume in glbc.yaml
kubectl create configmap gce-config --from-file=../resources/gce.conf.custom -n kube-system
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue creating gce.conf ConfigMap. ${CLEANUP_HELP}"

# Create new GCP service acccount.
gcloud iam service-accounts create glbc-service-account \
  --display-name "Service Account for GLBC"
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue creating a GCP service account. ${PERMISSION_ISSUE} ${CLEANUP_HELP}"

# Give the GCP service account the appropriate roles.
gcloud projects add-iam-policy-binding ${PROJECT_ID} \
  --member serviceAccount:glbc-service-account@${PROJECT_ID}.iam.gserviceaccount.com \
  --role roles/compute.admin
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue creating IAM role binding for service account. ${PERMISSION_ISSUE} ${CLEANUP_HELP}"

# Create key for the GCP service account.
gcloud iam service-accounts keys create \
  key.json \
  --iam-account glbc-service-account@${PROJECT_ID}.iam.gserviceaccount.com
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue creating GCP service account key. ${PERMISSION_ISSUE} ${CLEANUP_HELP}"

# Store the key as a secret in k8s. This secret is mounted
# as a volume in glbc.yaml
kubectl create secret generic glbc-gcp-key --from-file=key.json -n kube-system
if [[ $? -eq 1 ]];
then
  error_exit "Error-bot: Issue creating a k8s secret from GCP service account key. ${PERMISSION_ISSUE} ${CLEANUP_HELP}"
fi
rm key.json

# Turn off the glbc running on the GKE master. This will not only delete the
# glbc pod, but it will also delete the default-http-backend
# deployment + service.
gcloud container clusters update ${CLUSTER_NAME} --zone=${ZONE} \--update-addons=HttpLoadBalancing=DISABLED
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue turning off GLBC. ${PERMISSION_ISSUE} ${CLEANUP_HELP}"

# Critical to wait for the API server to be handling responses before continuing.
wait-for-api-server

# Recreate the default-http-backend k8s service with the same NodePort as the
# service which was removed when turning of the glbc previously. This is to
# ensure that a brand new NodePort is not created.

# Wait till old service is removed
echo "Waiting for old GLBC service and pod to be removed..."
while true; do
  kubectl get svc -n kube-system | grep default-http-backend &>/dev/null
  if [[ $? -eq 1 ]];
  then
    break
  fi
  sleep 5
done
# Wait till old glbc pod is removed
while true; do
  kubectl get pod -n kube-system | grep default-backend &>/dev/null
  if [[ $? -eq 1 ]];
  then
    break
  fi
  sleep 5
done

# Recreates the deployment and service for the default backend.
# Note: We do sed on a copy so that the original file stays clean for future runs.
sed "/name: http/a \ \ \ \ nodePort: ${NODE_PORT}" ../resources/default-http-backend.yaml > ../resources/default-http-backend.yaml.custom
kubectl create -f ../resources/default-http-backend.yaml.custom
if [[ $? -eq 1 ]];
then
  # Prompt the user to finish the last steps by themselves. We don't want to
  # have to cleanup and start all over again if we are this close to finishing.
  error_exit "Error-bot: Issue starting default backend. ${PERMISSION_ISSUE}. We are so close to being done so just manually start the default backend with NodePort: ${NODE_PORT} (see default-http-backend.yaml.custom) and create glbc.yaml.custom when ready."
fi
rm ../resources/default-http-backend.yaml.custom # Not useful to keep because the port changes each run.

# Startup glbc
kubectl create -f ../resources/glbc.yaml.custom
[[ $? -eq 0 ]] || manual_glbc_provision
if [[ $? -eq 1 ]];
then
  # Same idea as above, although this time we only need to prompt the user to start the glbc.
  error_exit: "Error_bot: Issue starting GLBC. ${PERMISSION_ISSUE}. We are so close to being done so just manually create glbc.yaml.custom when ready."
fi

# Do a final verification that the NodePort stayed the same for the
# default-http-backend.
NEW_NODE_PORT=`kubectl get svc default-http-backend -n kube-system -o yaml | grep "nodePort:" | cut -f2- -d:`
[[ "$NEW_NODE_PORT" == "$NODE_PORT" ]] || error_exit "Error-bot: The NodePort for the new default-http-backend service is different than the original. Please recreate this service with NodePort: ${NODE_PORT} or traffic to this service will time out."

echo -e "${GREEN}Script-bot: I'm done!${NC}"
