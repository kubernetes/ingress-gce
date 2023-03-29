#!/bin/bash

# Run make commands that build and push images
# (see Dockerfiles in the root directory)

# ENV variables: 
# * BINARY (example: "e2e-test")
# * ALL_ARCH (example: "amd64 arm64")
# * REGISTRY (container registry)
# * VERBOSE (example: -3)

set -o errexit
set -o nounset
set -o pipefail
set -o xtrace
REPO_ROOT=$(git rev-parse --show-toplevel)
cd ${REPO_ROOT}

BINARY=${BINARY:-e2e-test}
ARCH=${ARCH:-"amd64"}
ALL_ARCH=${ALL_ARCH:-"amd64 arm64"}
REGISTRY=${REGISTRY:-gcr.io/example}
VERBOSE=${VERBOSE:1}

# This command builds an image of $BINARY and
# pushes it to the $REGISTRY
make push VERBOSE="${VERBOSE}" CONTAINER_BINARIES="${BINARY}" \
          REGISTRY="${REGISTRY}" ARCH="${ARCH}"
# Based on the image from the previous step,
# all-push command will try to create a multiarch image 
# (there is no $ARCH parameter)
make all-push VERBOSE="${VERBOSE}" CONTAINER_BINARIES="${BINARY}" \
              REGISTRY="${REGISTRY}" ALL_ARCH="${ALL_ARCH}"
