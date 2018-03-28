# GLBC E2E Testing

This document briefly goes over how the e2e testing is setup for this repository. It will also go into
some detail on how you can run tests against your own cluster.

## Kubernetes CI Overview

There are two groups of e2e tests that run in Kubernetes CI.
The first group uses a currently released image of GLBC when the test cluster is brought up.
The second group uses an image built directly from HEAD of this repository.
Currently, we run tests against an image from HEAD in GCE only. On the other hand,
tests that run against a release image use both GCE and GKE.

Any test that starts with ingress-gce-* is a test which runs a image of GLBC from HEAD.
Any other test you see runs a release image of GLBC.
Check out https://k8s-testgrid.appspot.com/sig-network-gce & https://k8s-testgrid.appspot.com/sig-network-gke
for to see the results for these tests.

Every time a PR is merged to ingress-gce, Kubernetes test-infra triggers
a job that pushes a new image of GLBC for e2e testing. The ingress-gce-* jobs then use
this image when the test cluster is brought up. You can see the results of this job
at https://k8s-testgrid.appspot.com/sig-network-gce#ingress-gce-image-push.

## Manual Testing

If you are fixing a bug or writing a new feature and want to test your changes before they
are run through CI, then you will most likely want to test your changes end-to-end before submitting:

1. ingress-gce-e2e:

  * `VERSION=mytag REGISTRY=gcr.io/my-test-project make push-e2e`
  * `export GCE_GLBC_IMAGE=gcr.io/my-test-project/ingress-gce-e2e-glbc-amd64:mytag`
  * `go run hack/e2e.go --up --test --down --test_args="--ginkgo.focus=\[Feature:Ingress\]"`

2. ingress-gce-upgrade-e2e

  * `VERSION=mytag REGISTRY=gcr.io/my-test-project make push-e2e`
  * `go run hack/e2e.go --up --test --down --test_args="--ginkgo.focus=\[Feature:IngressUpgrade\] --ingress-upgrade-image=gcr.io/my-test-project/ingress-gce-e2e-glbc-amd64:mytag"`



**Disclaimer:**

Note that the cluster you create should have permission to pull images from the registry
you are using to push the image to. You can either make your registry publicly readable or give explicit permission
to your cluster's project service account.
