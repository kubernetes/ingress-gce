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
  echo -e "Usage: ./script.sh -n myCluster -z myZone [-c] [-r]\n"
  echo    "  -c, --cleanup        Cleanup resources created by a previous run of the script"
  echo    "  -n, --cluster-name   Name of the cluster (Required)"
  echo    "  -z, --zone           Zone the cluster is in (Required)"
  echo -e "      --help           Display this help and exit"
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
  kubectl delete -f yaml/rbac.yaml
  kubectl delete configmap gce-config -n kube-system
  gcloud iam service-accounts delete glbc-service-account@${PROJECT_ID}.iam.gserviceaccount.com
  gcloud projects remove-iam-policy-binding ${PROJECT_ID} \
    --member serviceAccount:glbc-service-account@${PROJECT_ID}.iam.gserviceaccount.com \
    --role roles/compute.admin
  kubectl delete secret glbc-gcp-key -n kube-system
  kubectl delete -f yaml/glbc.yaml
  # Ask if user wants to reenable GLBC on the GKE master.
  while true; do
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

# Check that the gce.conf is valid for the cluster
NODE_INSTANCE_PREFIX=`cat gce.conf | grep node-instance-prefix | awk '{print $3}'`
[[ "$NODE_INSTANCE_PREFIX" == "gke-${CLUSTER_NAME}" ]] ||  error_exit "Error bot: --cluster-name does not match gce.conf. ${NO_CLEANUP}"

# Get the project id associated with the cluster.
PROJECT_ID=`gcloud config list --format 'value(core.project)' 2>/dev/null`
# Store the nodePort for default-http-backend
NODE_PORT=`kubectl get svc default-http-backend -n kube-system -o yaml | grep "nodePort:" | cut -f2- -d:`
# Get the GCP user associated with the current gcloud config.
GCP_USER=`gcloud config list --format 'value(core.account)' 2>/dev/null`

# Grant permission to current GCP user to create new k8s ClusterRole's.
kubectl create clusterrolebinding one-binding-to-rule-them-all --clusterrole=cluster-admin --user=${GCP_USER}
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue creating a k8s ClusterRoleBinding. ${PERMISSION_ISSUE} ${NO_CLEANUP}"

# Create a new service account for glbc and give it a
# ClusterRole allowing it access to API objects it needs.
kubectl create -f yaml/rbac.yaml
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue creating the RBAC spec. ${CLEANUP_HELP}"

# Inject gce.conf onto the user node as a ConfigMap.
# This config map is mounted as a volume in glbc.yaml
kubectl create configmap gce-config --from-file=gce.conf -n kube-system
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
gcloud container clusters update ${CLUSTER_NAME} --zone=${ZONE} --update-addons=HttpLoadBalancing=DISABLED
[[ $? -eq 0 ]] || error_exit "Error-bot: Issue turning of GLBC. ${PERMISSION_ISSUE} ${CLEANUP_HELP}"

# Approximate amount of time it takes the API server to start accepting all
# requests.
sleep 90
# In case the previous sleep was not enough, prompt user so that they can choose
# when to proceed.
while true; do
  echo -e "${GREEN}Script-bot: Before proceeding, please ensure your API server is accepting all requests.
Failure to do so may result in the script creating a broken state."
  echo -e "${GREEN}Script-bot: Press [C | c] to continue.${NC}" 
  read input
  case $input in
    [Cc]* ) break;;
    * ) echo -e "${GREEN}Script-bot: Press [C | c] to continue.${NC}"
  esac
done

# Recreate the default-http-backend k8s service with the same NodePort as the
# service which was removed when turning of the glbc previously. This is to
# ensure that a brand new NodePort is not created.

# Wait till old service is removed
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
sed -i "/name: http/a \ \ \ \ nodePort: ${NODE_PORT}" yaml/default-http-backend.yaml
kubectl create -f yaml/default-http-backend.yaml
if [[ $? -eq 1 ]];
then
  # Prompt the user to finish the last steps by themselves. We don't want to
  # have to cleanup and start all over again if we are this close to finishing.
  error_exit "Error-bot: Issue starting default backend. ${PERMISSION_ISSUE}. We are so close to being done so just manually start the default backend with NodePort: ${NODE_PORT} and create glbc.yaml when ready"
fi

# Startup glbc
kubectl create -f yaml/glbc.yaml
[[ $? -eq 0 ]] || manual_glbc_provision
if [[ $? -eq 1 ]];
then
  # Same idea as above, although this time we only need to prompt the user to start the glbc.
  error_exit: "Error_bot: Issue starting GLBC. ${PERMISSION_ISSUE}. We are so close to being done so just manually create glbc.yaml when ready"
fi

# Do a final verification that the NodePort stayed the same for the
# default-http-backend.
NEW_NODE_PORT=`kubectl get svc default-http-backend -n kube-system -o yaml | grep "nodePort:" | cut -f2- -d:`
[[ "$NEW_NODE_PORT" == "$NODE_PORT" ]] || error_exit "Error-bot: The NodePort for the new default-http-backend service is different than the original. Please recreate this service with NodePort: ${NODE_PORT} or traffic to this service will time out."

echo -e "${GREEN}Script-bot: I'm done!${NC}"
