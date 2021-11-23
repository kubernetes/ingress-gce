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
  echo "Usage: ./gke-self-managed.sh -n CLUSTER -z ZONE [-i IMAGE | --build-and-push] [options]"
  echo
  echo "Deploys a GLBC image and default HTTP backend to a GKE server. Note: this must"
  echo "be run in the script directory."
  echo
  echo "  -c, --cleanup            Cleanup resources created by a previous run of the script"
  echo "  -n, --cluster-name       Name of the cluster (Required)"
  echo "  -z, --zone               Zone the cluster is in (Required)"
  echo "  -i, --image-url          URL (with tag) of the glbc image. One of either this"
  echo "                           or --build-and-push is required"
  echo "  --build-and-push         If set, we will instead build the code and push the"
  echo "                           image, which will be used as the image-url. Set the"
  echo "                           REGISTRY env var or we'll use the project GCR (see"
  echo "                           the Makefile for defaults and other configuration)"
  echo "  --help                   Display this help and exit"
  echo "  --network-name           Override for the network-name gce.conf value"
  echo "  --enable-csm             If set, enable CSM mode which will create NEGs for Istio objects."
  echo "  --subnetwork-name        Override for the subnetwork-name gce.conf value"
  echo "  --network-tags           Override for the node-tags gce.conf value"
  echo "  --no-confirm             Don't ask confirmation to reenable GLBC on cleanup"
  echo "                           or to check that the API server is ready"
  echo "  --dry-run                Does not mutate the cluster and just prints all the"
  echo "                           commands we would run. This still generates local files"
  echo "                           and still will perform non-mutating queries on the cluster"
  echo "                           in a few cases. If using --build-and-push, we will still"
  echo "                           push (use --image-url to avoid this). kubectl commands"
  echo "                           will be run with kubectl --dry-run where possible."
  exit
}

# run_maybe_dry runs a command if we're not using --dry-run and otherwise just echoes
# it.
function run_maybe_dry() {
  if [[ -z $DRY_RUN ]];
  then
    eval $@
  else
    echo $@
  fi
}

# run_maybe_dry_kubectl runs a command if we're not using --dry-run and otherwise echoes
# and runs the kubectl command with --dry-run. We can't do this with all kubectl commands.
function run_maybe_dry_kubectl() {
  if [[ -z $DRY_RUN ]];
  then
    eval $@
  else
    echo "$@"
    eval $@ --dry-run
  fi
}

function arg_check {
  # Check that the necessary arguments were provided and that they are correct.
  if [[ -z "$ZONE" || -z "$CLUSTER_NAME" ]];
  then
    usage
  fi

  if [[ -n $BUILD_AND_PUSH && -n $IMAGE_URL ]];
  then
    echo "--image-url and --build-and-push are mutually exclusive."
    echo
    usage
  elif [[ -z $BUILD_AND_PUSH && -z $IMAGE_URL && -z $CLEANING ]];
  then
    echo "One of either --image-url or --build-and-push must be set."
    echo
    usage
  fi
  
  # CONTAINER_PREFIX is used in the Makefile and is the "ingress-gce" part of the image
  # name. While we could support parsing the Makefile output with a custom container
  # prefix, it's liable to break if the user has characters with special meaning in
  # them.
  if [[ -n $BUILD_AND_PUSH && -n $CONTAINER_PREFIX && $CONTAINER_PREFIX != "ingress-gce" ]];
  then
    echo "--build-and-push doesn't support a customer CONTAINER_PREFIX. Either unset that"
    echo "or use --image-url."
    echo
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
  # Get the project id associated with the cluster.
  PROJECT_ID=`gcloud config list --format 'value(core.project)' 2>/dev/null`
  # Cleanup k8s and GCP resources in same order they are created.
  # Note: The GCP service account key needs to be manually cleaned up.
  # Note: We don't delete the default-http-backend we created so that when the
  # GLBC is restored on the GKE master, the addon manager does not try to create a
  # new one.
  run_maybe_dry kubectl delete clusterrolebinding one-binding-to-rule-them-all
  run_maybe_dry kubectl delete -f ../resources/rbac.yaml
  run_maybe_dry kubectl delete configmap gce-config -n kube-system
  run_maybe_dry gcloud projects remove-iam-policy-binding ${GCLOUD_EXTRA_FLAGS} ${PROJECT_ID} \
    --member serviceAccount:glbc-service-account@${PROJECT_ID}.iam.gserviceaccount.com \
    --role roles/compute.admin
  run_maybe_dry gcloud iam service-accounts delete ${GCLOUD_EXTRA_FLAGS} glbc-service-account@${PROJECT_ID}.iam.gserviceaccount.com
  run_maybe_dry kubectl delete secret glbc-gcp-key -n kube-system
  run_maybe_dry kubectl delete -f ../resources/glbc.yaml.gen
  run_maybe_dry kubectl delete -f ../resources/default-http-backend.yaml.gen
  # Ask if user wants to reenable GLBC on the GKE master.
  while [[ $CONFIRM -eq 1 ]]; do
    echo -e "${GREEN}Script-bot: Do you want to reenable GLBC on the GKE master?${NC}"
    echo -e "${GREEN}Script-bot: Press [C | c] to continue.${NC}"
    read input
    case $input in
      [Cc]* ) break;;
      * ) echo -e "${GREEN}Script-bot: Press [C | c] to continue.${NC}"
    esac
  done
  run_maybe_dry gcloud container clusters update ${CLUSTER_NAME} --zone=${ZONE} --update-addons=HttpLoadBalancing=ENABLED
  echo -e "${GREEN}Script-bot: Cleanup successful! You need to cleanup your GCP service account key manually.${NC}"
  exit 0
}

# Loops until calls to the API server are succeeding.
function wait_for_api_server() {
  echo "Waiting for API server to become ready..."
  until run_maybe_dry kubectl api-resources > /dev/null 2>&1; do
    sleep 0.1
  done
}

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'
CLEANUP_HELP="Invoking me with the -c option will get you back to a clean slate."
NO_CLEANUP="Nothing has to be cleaned up :)"
PERMISSION_ISSUE="If this looks like a permissions problem, see the README."
CONFIRM=1
ENABLE_CSM=false

# Parsing command line arguments
while [[ $# -gt 0 ]]
do
key="$1"

case $key in
  -h|--help)
  usage
  shift
  shift
  ;;
  -c|--cleanup)
  CLEANING=1
  shift
  ;;
  -n|--cluster-name)
  CLUSTER_NAME=$2
  shift
  shift
  ;;
  -i|--image-url)
  IMAGE_URL=$2
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
  --network-tags)
  NETWORK_TAGS=$2
  shift
  shift
  ;;
  --build-and-push)
  BUILD_AND_PUSH=1
  shift
  ;;
  --enable-csm)
  ENABLE_CSM=true
  shift
  ;;
  --no-confirm)
  CONFIRM=0

  # --quiet flag makes gloud prompts non-interactive, ensuring this script can be
  # used in automated flows (user can also use this to provide extra, arbitrary flags).
  GCLOUD_EXTRA_FLAGS="${GCLOUD_EXTRA_FLAGS} --quiet"
  shift
  ;;
  --dry-run)
  DRY_RUN=1
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

arg_check

if [[ -n $CLEANING ]];
then
  cleanup
fi

# Get the project id associated with the cluster.
PROJECT_ID=`gcloud config list --format 'value(core.project)' 2>/dev/null`
# Store the nodePort for default-http-backend
NODE_PORT=`kubectl get svc default-http-backend -n kube-system -o yaml | grep "nodePort:" | cut -f2- -d:`
# Get the GCP user associated with the current gcloud config.
GCP_USER=`gcloud config list --format 'value(core.account)' 2>/dev/null`

# Populate gce.conf.gen from our template.
if [[ -z $NETWORK_NAME ]]; then
  NETWORK_NAME=$(basename $(gcloud container clusters describe $CLUSTER_NAME --zone=$ZONE \
      --format='value(networkConfig.network)'))
fi
if [[ -z $SUBNETWORK_NAME ]]; then
  SUBNETWORK_NAME=$(basename $(gcloud container clusters describe $CLUSTER_NAME \
      --zone=$ZONE --format='value(networkConfig.subnetwork)'))
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
  NETWORK_TAGS=$(gcloud compute instances describe $INSTANCE --zone=$ZONE --format="value(tags.items)")
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

sed "s/\[PROJECT\]/$PROJECT_ID/" ../resources/gce.conf | \
sed "s/\[NETWORK\]/$NETWORK_NAME/" | \
sed "s/\[SUBNETWORK\]/$SUBNETWORK_NAME/" | \
sed "s/\[CLUSTER_NAME\]/$CLUSTER_NAME/" | \
sed "s/\[NETWORK_TAGS\]/$NETWORK_TAGS/" | \
sed "s/\[ZONE\]/$ZONE/" > ../resources/gce.conf.gen

# If we're using build-and-push, do this early so errors are caught before we start
# making cluster changes.
if [[ -n $BUILD_AND_PUSH ]]; then
  echo "build-and-push is set. This may take a few minutes."
  if [[ -z $REGISTRY ]]; then
    REGISTRY="gcr.io/$PROJECT_ID"
    echo "REGISTRY is not set. Defaulting to: $REGISTRY"
  fi
  # Extract just the URL (with tag) from the `make push` output. `sed -n` makes it
  # not print normally while the `/../../p` makes it print just what it matched.
  MAKE_COMMAND="cd ../../../; REGISTRY=\"$REGISTRY\" make only-push-glbc 2>&1"
  echo "Make command is: ${MAKE_COMMAND}"
  MAKE_PUSH_OUTPUT=$(eval "${MAKE_COMMAND}")
  MAKE_PUSH_CODE=$?
  if [[ $MAKE_PUSH_CODE -eq 0 ]]; then
    IMAGE_URL=$(head -n 1 $(ls -t ../../../.*_ingress-gce-glbc-*-push | head -1))
    if [[ $? -eq 1 ]];
    then
        error_exit "Error-bot: Issue geting the image url consider providing --image-url yourself"
    fi
    echo "Pushed new glbc image to: $IMAGE_URL"
  else
    error_exit "Error-bot: Issue building and pushing image. Make exited with code $MAKE_PUSH_CODE. If a push error, consider setting REGISTRY or providing --image-url yourself. Output from make:\n$MAKE_PUSH_OUTPUT"
  fi
fi

# And format the GLBC YAML with the image URL (either from --image-url or parsed from)
# our Makefile output.
sed "s|\[IMAGE_URL\]|$IMAGE_URL|" ../resources/glbc.yaml | \
sed -e "s|\[ENABLE_CSM\]|$ENABLE_CSM|" > ../resources/glbc.yaml.gen

# Grant permission to current GCP user to create new k8s ClusterRole's.
run_maybe_dry_kubectl kubectl create clusterrolebinding one-binding-to-rule-them-all --clusterrole=cluster-admin --user=${GCP_USER}
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue creating a k8s ClusterRoleBinding. ${PERMISSION_ISSUE} ${NO_CLEANUP}"

# Inject gce.conf onto the user node as a ConfigMap.
# This config map is mounted as a volume in glbc.yaml
run_maybe_dry_kubectl kubectl create configmap gce-config --from-file=../resources/gce.conf.gen -n kube-system
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue creating gce.conf ConfigMap. ${CLEANUP_HELP}"

# Create new GCP service account.
run_maybe_dry gcloud iam service-accounts create glbc-service-account ${GCLOUD_EXTRA_FLAGS} \
  --display-name \"Service Account for GLBC\"
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue creating a GCP service account. ${PERMISSION_ISSUE} ${CLEANUP_HELP}"

# Give the GCP service account the appropriate roles.
run_maybe_dry gcloud projects add-iam-policy-binding ${GCLOUD_EXTRA_FLAGS} ${PROJECT_ID} \
  --member serviceAccount:glbc-service-account@${PROJECT_ID}.iam.gserviceaccount.com \
  --role roles/compute.admin
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue creating IAM role binding for service account. ${PERMISSION_ISSUE} ${CLEANUP_HELP}"

# Create key for the GCP service account.
run_maybe_dry gcloud iam service-accounts keys create ${GCLOUD_EXTRA_FLAGS} \
  key.json \
  --iam-account glbc-service-account@${PROJECT_ID}.iam.gserviceaccount.com
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue creating GCP service account key. ${PERMISSION_ISSUE} ${CLEANUP_HELP}"

# Store the key as a secret in k8s. This secret is mounted
# as a volume in glbc.yaml
run_maybe_dry kubectl create secret generic glbc-gcp-key --from-file=key.json -n kube-system
if [[ $? -eq 1 ]];
then
  error_exit "Error-bot: Issue creating a k8s secret from GCP service account key. ${PERMISSION_ISSUE} ${CLEANUP_HELP}"
fi
[[ -n $DRY_RUN ]] || rm key.json

# Turn off the glbc running on the GKE master. This will not only delete the
# glbc pod, but it will also delete the default-http-backend
# deployment + service.
run_maybe_dry gcloud container clusters update ${CLUSTER_NAME} ${GCLOUD_EXTRA_FLAGS} --zone=${ZONE} \--update-addons=HttpLoadBalancing=DISABLED
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue turning off GLBC. ${PERMISSION_ISSUE} ${CLEANUP_HELP}"

# Critical to wait for the API server to be handling responses before continuing.
wait_for_api_server

# In case the API server isn't actually ready (there's been mention of it succeeding on
# a request only to fail on the next), prompt user so that they can choose when to proceed.
while [[ $CONFIRM -eq 1 ]]; do
  echo -e "${GREEN}Script-bot: Before proceeding, please ensure your API server is accepting all requests.
Failure to do so may result in the script creating a broken state."
  echo -e "${GREEN}Script-bot: Press [C | c] to continue.${NC}"
  read input
  case $input in
    [Cc]* ) break;;
    * ) echo -e "${GREEN}Script-bot: Press [C | c] to continue.${NC}"
  esac
done


# Create a new service account for glbc and give it a
# ClusterRole allowing it access to API objects it needs.
run_maybe_dry_kubectl kubectl apply -f ../resources/rbac.yaml
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue creating the RBAC spec. ${CLEANUP_HELP}"

# Recreate the default-http-backend k8s service with the same NodePort as the
# service which was removed when turning of the glbc previously. This is to
# ensure that a brand new NodePort is not created.

# Wait till old service is removed
echo "Waiting for old GLBC service and pod to be removed..."
while true; do
  run_maybe_dry kubectl get svc -n kube-system | grep default-http-backend &>/dev/null
  if [[ $? -eq 1 ]];
  then
    break
  fi
  sleep 5
done
# Wait till old glbc pod is removed
while true; do
  run_maybe_dry kubectl get pod -n kube-system | grep default-backend &>/dev/null
  if [[ $? -eq 1 ]];
  then
    break
  fi
  sleep 5
done

# Recreates the deployment and service for the default backend.
# Note: We do awk on a copy so that the original file stays clean for future runs.
awk "1;/name: http/{ print \"    nodePort: ${NODE_PORT}\" }" ../resources/default-http-backend.yaml > ../resources/default-http-backend.yaml.gen
run_maybe_dry_kubectl kubectl create -f ../resources/default-http-backend.yaml.gen
if [[ $? -eq 1 ]];
then
  # Prompt the user to finish the last steps by themselves. We don't want to
  # have to cleanup and start all over again if we are this close to finishing.
  error_exit "Error-bot: Issue starting default backend. ${PERMISSION_ISSUE}. We are so close to being done so just manually start the default backend with NodePort: ${NODE_PORT} (see default-http-backend.yaml.gen) and create glbc.yaml.gen when ready."
fi
# Not useful to keep because the port changes each run.
rm ../resources/default-http-backend.yaml.gen

# Startup glbc
run_maybe_dry_kubectl kubectl create -f ../resources/glbc.yaml.gen
[[ $? -eq 0 ]] || manual_glbc_provision
if [[ $? -eq 1 ]];
then
  # Same idea as above, although this time we only need to prompt the user to start the glbc.
  error_exit: "Error_bot: Issue starting GLBC. ${PERMISSION_ISSUE}. We are so close to being done so just manually create glbc.yaml.gen when ready."
fi

# Do a final verification that the NodePort stayed the same for the
# default-http-backend.
NEW_NODE_PORT=`kubectl get svc default-http-backend -n kube-system -o yaml | grep "nodePort:" | cut -f2- -d:`
[[ "$NEW_NODE_PORT" == "$NODE_PORT" ]] || error_exit "Error-bot: The NodePort for the new default-http-backend service is different than the original. Please recreate this service with NodePort: ${NODE_PORT} or traffic to this service will time out."

echo -e "${GREEN}Script-bot: I'm done!${NC}"
