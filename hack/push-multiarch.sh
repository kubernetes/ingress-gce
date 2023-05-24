#!/bin/bash

# Run commands that build and push images

# This script is used instead of build/rules.mk
# whenever you need to build multiarch image with
# docker buildx build --platform=smth
# (see Dockerfiles in the root directory)

# ENV variables: 
# * BINARIES (example: "e2e-test")
# * ALL_ARCH (example: "amd64 arm64")
# * REGISTRY (container registry)
# * VERSION (example: "test")

set -o errexit
set -o nounset
set -o pipefail
set -o xtrace

# docker buildx is in /root/.docker/cli-plugins/docker-buildx
HOME=/root

REPO_ROOT=$(git rev-parse --show-toplevel)
cd ${REPO_ROOT}

BINARIES=${BINARIES:-"e2e-test"}
ALL_ARCH=${ALL_ARCH:-"amd64 arm64"}
REGISTRY=${REGISTRY:-"gcr.io/example"}
VERSION=${VERSION:-"test"}

echo BINARIES=${BINARIES}
echo ALL_ARCH=${ALL_ARCH}
echo REGISTRY=${REGISTRY}
echo VERSION=${VERSION}

echo "building all binaries"
make all-build ALL_ARCH="${ALL_ARCH}" CONTAINER_BINARIES="${BINARIES}"

# To create cross compiled images
echo "setting up docker buildx.."
docker buildx install
docker buildx create --use

# Download crane cli
curl -sL "https://github.com/google/go-containerregistry/releases/download/v0.15.2/go-containerregistry_$(uname -s)_$(uname -m).tar.gz" | tar xvzf - krane

for binary in ${BINARIES}
do
    # "arm64 amd64" ---> "linux/arm64,linux/amd64" 
    PLATFORMS="linux/$(echo ${ALL_ARCH} | sed 's~ ~,linux/~g')"
    echo "docker buildx platform parameters: ${PLATFORMS}"
    MULTIARCH_IMAGE="${REGISTRY}/ingress-gce-${binary}:${VERSION}"
    echo "building ${MULTIARCH_IMAGE} image.."
    docker buildx build --push  \
        --platform ${PLATFORMS}  \
        --tag  ${MULTIARCH_IMAGE} \
        -f Dockerfile.${binary} .
    echo "done, pushed $MULTIARCH_IMAGE image"

    # Tag arch specific images for the legacy registries
    for arch in ${ALL_ARCH}
    do
        # krane is a variation of crane that supports k8s auth
        ./krane copy --platform linux/${arch} ${MULTIARCH_IMAGE} ${REGISTRY}/ingress-gce-${binary}-${arch}:${VERSION}
    done
    echo "images are copied to arch specific registries"
done
