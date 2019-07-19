# Overview

This doc outlines the steps needed to setup a dev cluster for testing changes
made to the Ingress-GCE controller. Typically we test out controller changes
by running the binary locally but this still means we need a working cluster
to test against.

## Create the cluster

We recommend to create a k8s cluster in GCE. For instructions on how to do
that, go [here](../deploy/gce/README.md)

## Remove the default controller

Once the cluster is created, we need to delete the existing Ingress-GCE controller
that comes by default:

```console
kubectl delete pod l7-lb-controller -n kube-system
```

This ensures we can run our own copy of the controller locally.
