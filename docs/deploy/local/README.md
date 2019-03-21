# Overview

The guide below assumes that you have a k8s cluster on GCP already up an running
and that any existing Ingress-GCE controller that came out of the box with that
setup is turned off. See [here](../../contrib/cluster-setup.md) for more info on
how to setup such a cluster.

## Setup GCE configuration

In order to configure its Google Compute Engine (GCE) client correctly, the
Ingress-GCE controller needs to know about some of your cluster configuration.
A skeleton configuration file can be found [here](../resources/gce.conf). An
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

Once you have filled out the config, store it somewhere.

## Setup GCE permissions

When running locally, the Ingress-GCE controller looks on the local machine
for credentials to create GCE networking resources. Specifically it looks for a
json file specified at the GOOGLE_APPLICATION_CREDENTIALS variable. Given this,
it is most desirable to follow these steps:

1. Create a Service Account in GCP and give the account Compute Admin permissions

2. Create a key for the Service Account and download it

Then run the following:

```console
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/key/file
```

## Run the controller

First build the binary:

```console
make build
```

Proxy connections to your k8s cluster via your local machine:

```console
kubectl proxy --port=8080
```

Then run the controller:

```console
./bin/amd64/glbc --apiserver-host=http://localhost:8080 --running-in-cluster=false --logtostderr --v=3 --config-file-path=/path/to/gce.conf
```
