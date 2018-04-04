# Post-Release Steps

This document explains steps that need to be taken after a new release
of the ingress-gce controller has been cut and pushed.

## Update Manifest

The [glbc.manifest](https://github.com/kubernetes/kubernetes/blob/master/cluster/gce/manifests/glbc.manifest) in the main kubernetes repository needs to be updated
to use the new image. Make sure to not only update the image for the container but also update the
name in the top-level metadata field as well as version field under metadata.labels.

[Example PR](https://github.com/kubernetes/kubernetes/pull/62075)

## Update e2e Tests

Our e2e tests need to be updated in order to make use of the new release.

### ci-ingress-gce-upgrade-e2e

In [config.json](https://github.com/kubernetes/test-infra/blob/master/jobs/config.json),
find the json block called `ci-ingress-gce-upgrade-e2e`. In this block, modify
the environment variable `GCE_GLBC_IMAGE` to point to the latest release image.

[Example PR](https://github.com/kubernetes/test-infra/pull/7534)

### ci-ingress-gce-downgrade-e2e

In [nodes_util.go](https://github.com/kubernetes/kubernetes/blob/master/test/e2e/framework/nodes_util.go),
find a function called ingressUpgradeGCE(). In this function,
find the comment `Downgrade to latest release image`. Below this comment,
you will find the variable `command` being set. Update the image reference
in the set logic for that variable to the latest release image.

[Example PR](https://github.com/kubernetes/kubernetes/pull/62079)
