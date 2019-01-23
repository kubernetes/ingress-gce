# Overview

This guide walks you through how to create a k8s cluster on GCE with the
Ingress-GCE controller. The steps below assume that you already have a project
in GCP setup and that you have a working `gcloud` binary.

## Deploy on GCE

Verify the project you want to create the cluster in is the same as the one
displayed by the following command:

```console
gcloud config list
```

Then, clone the kubernetes repo and test-infra repo:

```console
$ cd $GOPATH/src/
$ git clone https://github.com/kubernetes/kubernetes.git
$ git clone https://github.com/kubernetes/test-infra.git
```

Change into kubernetes/kubernetes and build the release tars:

```console
make quick-release
```

Finally, create the cluster:

```console
go run hack/e2e.go --up
```

Note that the above command automatically destroys any existing cluster that
was previously created with --up.
