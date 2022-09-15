# Overview

This document will deploy the self managed Ingress-GCE controller in CSM(Cloud Service Mesh
) mode.

# Prepare the Cluster

The cluster should satisfy the following restrictions:
 * GKE version 1.14+
 * [IP Alias](https://cloud.google.com/kubernetes-engine/docs/how-to/alias-ips) enabled
 * Default Ingress-GCE Controller disabled.
 * [GKE Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity) enabled.

## [Option 1] Create a new Cluster

```sh
gcloud container clusters create $CLUSTER --enable-ip-alias --cluster-version 1.14 \
  --zone $ZONE --addons=HorizontalPodAutoscaling \
  --identity-namespace=PROJECT.svc.id.goog
```

## [Option 2] Updating an existing cluster

```sh
# disable Ingress-GCE controller
gcloud container clusters update $CLUSTER --zone=$ZONE --update-addons=HttpLoadBalancing=DISABLED

# enable Workload Identity
gcloud beta container clusters update $CLUSTER --zone $ZONE \
  --identity-namespace=${PROJECT}.svc.id.goog

# update node pool
gcloud beta container node-pools update $NODEPOOL_NAME \
  --cluster=$CLUSTER \
  --workload-metadata-from-node=GKE_METADATA_SERVER
```

# Create a service account

```sh
# create a service account
gcloud iam service-accounts create glbc-service-account \
  --display-name "Service Account for GLBC" --project $PROJECT

# binding compute.admin role to the service account
gcloud projects add-iam-policy-binding $PROJECT \
  --member serviceAccount:glbc-service-account@${PROJECT}.iam.gserviceaccount.com \
  --role roles/compute.admin

# binding the service account to k8s service account
gcloud iam service-accounts add-iam-policy-binding \
  --role roles/iam.workloadIdentityUser \
  --member "serviceAccount:${PROJECT}.svc.id.goog[kube-system/glbc]" \
  glbc-service-account@${PROJECT}.iam.gserviceaccount.com
```

# Create K8s Roles

```sh
# Grant permission to current GCP user to create new k8s ClusterRoles.
kubectl create clusterrolebinding one-binding-to-rule-them-all \
  --clusterrole=cluster-admin \
  --user=$(gcloud config list --project $PROJECT --format 'value(core.account)' 2>/dev/null)

kubectl create -f rbac.yaml

kubectl annotate serviceaccount \
  --namespace kube-system glbc \
  iam.gke.io/gcp-service-account=glbc-service-account@${PROJECT}.iam.gserviceaccount.com
```

# Generate configmap for ingress controller

## [Option 1] Generate gce.conf with the generate.sh script

```sh
./generate.sh -n $CLUSTER -z $ZONE -p $PROJECT

kubectl create configmap gce-config --from-file=gce.conf -n kube-system
```

## [Option 2] Raw CMDs

```sh
NETWORK_NAME=$(basename $(gcloud container clusters describe  \
  $CLUSTER --project $PROJECT --zone=$ZONE \
  --format='value(networkConfig.network)'))

SUBNETWORK_NAME=$(basename $(gcloud container clusters describe \
  $CLUSTER --project $PROJECT \
  --zone=$ZONE --format='value(networkConfig.subnetwork)'))

INSTANCE_GROUP=$(gcloud container clusters describe $CLUSTER --project $PROJECT --zone=$ZONE \
  --format='flattened(nodePools[].instanceGroupUrls[].scope().segment())' | \
  cut -d ':' -f2 | tr -d '[:space:]')

INSTANCE=$(gcloud compute instance-groups list-instances $INSTANCE_GROUP --project $PROJECT \
  --zone=$ZONE --format="value(instance)" --limit 1)

NETWORK_TAGS=$(gcloud compute instances describe $INSTANCE --project \
  $PROJECT --format="value(tags.items)")

cat <<EOF >> gce.conf
[global]
token-url = nil
project-id = $PROJECT
network-name =  $NETWORK_NAME
subnetwork-name = $SUBNETWORK_NAME
node-instance-prefix = gke-$CLUSTER
node-tags = $NETWORK_TAGS
local-zone = $ZONE
EOF

kubectl create configmap gce-config --from-file=gce.conf -n kube-system
```

# Deploy Ingress controller

```sh
kubectl create -f default-http-backend.yaml
kubectl create -f ../../resources/configmap-based-config.yaml
kubectl create -f glbc.yaml
```

# Clean up

```sh
kubectl delete -f default-http-backend.yaml
kubectl delete -f glbc.yaml
kubectl delete configmap gce-config -n kube-system
kubectl delete -f rbac.yaml
kubectl delete clusterrolebinding one-binding-to-rule-them-all
```

## [Optional] Delete service account
```sh
gcloud iam service-accounts delete glbc-service-account@${PROJECT}.iam.gserviceaccount.com

gcloud projects remove-iam-policy-binding $PROJECT \
  --member serviceAccount:glbc-service-account@${PROJECT}.iam.gserviceaccount.com \
  --role roles/compute.admin
```