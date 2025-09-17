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

Alternatively you can run the [provided script](../../../hack/setup-local.sh)
from the root of this repo:

```console
$ hack/setup-local.sh <cluster-name>
```

## Authorize gcloud and kubectl

Once the cluster is ready prepare authorization to it.
You need to authorize both gcloud and kubectl.

```console
$ gcloud auth application-default login
$ gcloud container clusters get-credentials CLUSTER_NAME --region CLUSTER_LOCATION
```

## Run the controller

All of the following should be run from the root of this repo.

First build the binary:

For Linux users, you can use the following make target which will build the
binary in a container and place it in `bin/amd64`.

```console
make build
```

NOTE -
If you get build errors with Docker getting a permission denied while pulling the base image run the following commands:

```console
sudo addgroup --system docker
sudo usermod -aG docker $USER
newgrp docker
```

For Mac OS users or to build the binary locally and output it in the
`bin/amd64` directory run:

```console
env CGO_ENABLED=0 go build -a -o bin/amd64/glbc  k8s.io/ingress-gce/cmd/glbc
```

Run controller from the root of this repo using the [provided script](../../../hack/run-local-glbc.sh).

```console
GLBC=bin/amd64/glbc hack/run-local-glbc.sh
```

Alternatively you can run the controller manually by doing the following:

Proxy connections to your k8s cluster via your local machine:

```console
kubectl proxy --port=8080
```

Then run the controller:

```console
./bin/amd64/glbc --apiserver-host=http://localhost:8080 --running-in-cluster=false --logtostderr --v=3 --config-file-path=/path/to/gce.conf
```
