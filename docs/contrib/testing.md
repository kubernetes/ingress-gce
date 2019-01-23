# Overview

This document briefly goes over how the e2e testing is setup for this repository. It will also go into
some detail on how you can run tests against your own cluster.

## CI 

There are two groups of e2e tests that run in Kubernetes CI.
The first group uses a currently released image of GLBC when the test cluster is brought up.
The second group uses an image built directly from HEAD of this repository.
Currently, we run tests against an image from HEAD in GCE only. On the other hand,
tests that run against a release image use both GCE and GKE.

Any test that starts with ingress-gce-* is a test which runs an image of GLBC from HEAD.
Any other test you see runs a release image of GLBC.
Check out https://k8s-testgrid.appspot.com/sig-network-gce & https://k8s-testgrid.appspot.com/sig-network-gke
for to see the results for these tests.

Every time a PR is merged to ingress-gce, Kubernetes test-infra triggers
a job that pushes a new image of GLBC for e2e testing. The ingress-gce-* jobs then use
this image when the test cluster is brought up. You can see the results of this job
at https://k8s-testgrid.appspot.com/sig-network-gce#ingress-gce-image-push.

## Running E2E tests

If you are fixing a bug or writing a new feature and want to test your changes before they
are run through CI, then you will most likely want to test your changes end-to-end before submitting.
The instructions for running the e2e tests are [here](../../cmd/e2e-test/readme.md).
Before following those instructions, ensure that you are running the controller
by following the instructions [here](../deploy/local/README.md)
