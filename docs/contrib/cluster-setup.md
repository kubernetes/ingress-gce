# Overview

This doc outlines the steps needed to run Ingress-GCE binary locally:
* Setup of a dev GKE cluster.
* Authorization configuration to it.
* Buldinging and running the Ingress-GCE bibnary locally.

## Create the cluster

First step is to create a dev GKE cluster:
```console
$ gcloud container clusters create CLUSTER_NAME
```
Once the cluster is ready you need to disable HTTP Load Balancing.
```console
$ gcloud container clusters update CLUSTER_NAME --update-addons=HttpLoadBalancing=DISABLED
```
You can also do this from the Cloud Console.
The HTTP Load Balancing option is avaialbe under Networking section.

## Authorize gcloud and kubectl

Once the cluster is ready prepare authorization to it.
You need to authorize both gcloud and kubectl.
```console
$ gcloud auth application-default login
$ gcloud container clusters get-credentials CLUSTER_NAME --region CLUSTER_LOCATION
```

## Build, configure and run Ingress-GCE

Build the Ingress-GCE server
```console
$ make build
```
Prepare configuration of the cluster.
An
example fully-specified config looks something like this:
```console
[global]
token-url = nil
project-id = foo-project
network-name = foo-network
subnetwork-name = foo-subnetwork
node-instance-prefix = gke-foo-cluster
node-tags = my-custom-network-tags
local-zone=us-central1-c
```
You can run the [provided script](../../hack/setup-local.sh)
from the root of this repo:
```console
$ hack/setup-local.sh <cluster-name>
```
Or you can fill the config yourself and store it in the `/tmp/gce.conf`.

Once that is ready you can run the Ingress-GCE server.
```console
$ GLBC=bin/amd64/glbc hack/run-local-glbc.sh
```
