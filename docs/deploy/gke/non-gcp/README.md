# Overview

This document describe the process to deploy the self managed NEG controller
in a GKE On-Prem cluster.

# Set Locality Labels on Nodes

Currently, NEG controller reads GCP locality for NEGs from node labels. Use the
following commands to label ALL nodes in the GKE On-Prem cluster:

```sh
kubectl label nodes <node-name> \
  topology.kubernetes.io/zone=<GCP_ZONE>

kubectl label nodes <node-name> \
  topology.kubernetes.io/region=<GCP_REGION>
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
```

# Create K8s Roles

```sh
# Grant permission to current GCP user to create new k8s ClusterRoles.
kubectl create clusterrolebinding one-binding-to-rule-them-all \
  --clusterrole=cluster-admin \
  --user=$(gcloud config list --project $PROJECT --format 'value(core.account)' 2>/dev/null)

kubectl create -f rbac.yaml

```
# Upload GCP Service Account Key as K8s Secret

```sh
# Create key for glbc-service-account.
gcloud iam service-accounts keys create key.json --iam-account \
  glbc-service-account@${PROJECT}.iam.gserviceaccount.com

# Store the key as a secret in k8s. The secret will be mounted as a volume in
# glbc.yaml.
kubectl create secret generic glbc-gcp-key --from-file=key.json -n kube-system

rm key.json
```

# Create configmap for NEG controller
Fill in "project-id", "network-name", "local-zone" in gce.conf and run the
following command:

```sh
kubectl create configmap gce-config --from-file=gce.conf -n kube-system
```

# Deploy NEG controller

```sh
kubectl create -f default-http-backend.yaml
kubectl create -f glbc.yaml
```

# Clean up

```sh
kubectl delete -f default-http-backend.yaml
kubectl delete -f glbc.yaml
kubectl delete configmap gce-config -n kube-system
kubectl delete secret glbc-gcp-key -n kube-system
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
