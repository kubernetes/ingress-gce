#!/bin/bash

# Run commands that build and push images
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
REPO_ROOT=$(git rev-parse --show-toplevel)
cd ${REPO_ROOT}

BINARIES=${BINARIES:-"e2e-test"}
ALL_ARCH=${ALL_ARCH:-"amd64 arm64"}
REGISTRY=${REGISTRY:-"gcr.io/example"}
VERSION=${VERSION:-"test"}

echo "building all `bin/<arch>/<binary-name>`.."
make build ALL_ARCH="${ALL_ARCH}" BINARIES="${BINARIES}"

# To create cross compiled images
echo "setting up docker buildx.."
docker buildx install
docker buildx create --use

for binary in ${BINARIES}
do
    MULTIARCH_IMAGE="${REGISTRY}/ingress-gce-${binary}"
    DOCKER_PARAMETERS=""
    for arch in ${ALL_ARCH}
    do
        echo "building & pushing a docker image for ${binary} (${arch}).."
        # creates arch dependant dockerfiles for every binary
        sed                                     \
            -e 's|ARG_ARCH|${arch}|g' \
            -e 's|ARG_BIN|${binary}|g' \
            Dockerfile.${binary} > .dockerfile-${arch}.${binary}

        # buildx builds and pushes images for any arch
        IMAGE_NAME="${REGISTRY}/ingress-gce-${binary}-$arch"
        docker buildx build --platform=linux/$arch \
            -f .dockerfile-${arch}.${binary} \
            -t ${IMAGE_NAME}:${VERSION} --push .
        docker pull ${IMAGE_NAME}:${VERSION}
        DOCKER_PARAMETERS="$DOCKER_PARAMETERS ${IMAGE_NAME}:${VERSION}"
    done

    echo "creating a multiatch manifest (${MULTIARCH_IMAGE}) from a list of images.."
    docker buildx imagetools create -t "${MULTIARCH_IMAGE}:${VERSION}" ${DOCKER_PARAMETERS}

    echo "done, a result image: ${MULTIARCH_IMAGE}:${VERSION}." 
done
