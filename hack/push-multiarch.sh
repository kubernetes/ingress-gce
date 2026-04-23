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

BINARIES=${BINARIES:-"psc-e2e-test neg-e2e-test ingress-controller-e2e-test"}
ALL_ARCH=${ALL_ARCH:-"amd64 arm64"}
REGISTRY=${REGISTRY:-"gcr.io/example"}
VERSION=${VERSION:-"test"}
ADDITIONAL_TAGS=${ADDITIONAL_TAGS:-""}

echo BINARIES=${BINARIES}
echo ALL_ARCH=${ALL_ARCH}
echo REGISTRY=${REGISTRY}
echo VERSION=${VERSION}
echo ADDITIONAL_TAGS=${ADDITIONAL_TAGS}

echo "building all binaries"
make all-build ALL_ARCH="${ALL_ARCH}" CONTAINER_BINARIES="${BINARIES}"

# To create cross compiled images
echo "setting up docker buildx.."
docker buildx install
docker buildx create --use

# Download crane cli
curl -sL "https://github.com/google/go-containerregistry/releases/download/v0.21.5/go-containerregistry_$(uname -s)_$(uname -m).tar.gz" | tar xvzf - krane

for binary in ${BINARIES}
do
    # "arm64 amd64" ---> "linux/arm64,linux/amd64"
    PLATFORMS="linux/$(echo ${ALL_ARCH} | sed 's~ ~,linux/~g')"
    echo "docker buildx platform parameters: ${PLATFORMS}"
    BIN_IMG_REGISTRY=${REGISTRY}/ingress-gce-${binary}
    MULTIARCH_IMAGE="${BIN_IMG_REGISTRY}:${VERSION}"
    echo "building ${MULTIARCH_IMAGE} image.."

    tags="--tag ${MULTIARCH_IMAGE}"
    for tag in ${ADDITIONAL_TAGS}
    do
        tags+=" --tag ${REGISTRY}/ingress-gce-${binary}:${tag}"
    done

    docker buildx build --push  \
        --platform ${PLATFORMS}  \
        ${tags} \
        -f Dockerfile.${binary} .
    echo "done, pushed $MULTIARCH_IMAGE image"

    # read the manifest to tag all arch specific images
    # the manifest is parsed to extract the digests of the per arch images plus
    # the attestation manifests.
    # the extracted digests are then used to tag the images so that later
    # cleanup can work reliably
    MANIFEST_JSON=$(./krane manifest "$MULTIARCH_IMAGE")

    if [[ $? -ne 0 ]]; then
      echo "Error: Failed to fetch manifest for $MULTIARCH_IMAGE"
      exit 1
    fi

    if [[ -z "$MANIFEST_JSON" ]]; then
      echo "Error: Manifest for $MULTIARCH_IMAGE is empty"
      exit 1
    fi

    # --- Extract digests using jq ---
    # Process substitution is used to feed the variable content to jq
    AMD64_DIGEST=$(jq -r '.manifests[] | select(.platform.architecture == "amd64").digest' <<< "$MANIFEST_JSON")
    ARM64_DIGEST=$(jq -r '.manifests[] | select(.platform.architecture == "arm64").digest' <<< "$MANIFEST_JSON")

    # Extract all attestation digests into a bash array
    readarray -t ATTESTATION_DIGESTS < <(jq -r '.manifests[] | select(.annotations["vnd.docker.reference.type"] == "attestation-manifest").digest' <<< "$MANIFEST_JSON")

    # Extract attestation digests based on the image they reference
    ATTESTATION_FOR_AMD64_DIGEST=$(jq -r '.manifests[] | select(.annotations["vnd.docker.reference.digest"] == "'"$AMD64_DIGEST"'").digest' <<< "$MANIFEST_JSON")
    ATTESTATION_FOR_ARM64_DIGEST=$(jq -r '.manifests[] | select(.annotations["vnd.docker.reference.digest"] == "'"$ARM64_DIGEST"'").digest' <<< "$MANIFEST_JSON")

    # --- Output the variables ---
    echo "--- Extracted Digests ---"
    echo "MULTIARCH_IMAGE=$MULTIARCH_IMAGE"
    echo "AMD64_DIGEST=$AMD64_DIGEST"
    echo "ARM64_DIGEST=$ARM64_DIGEST"

    echo "ATTESTATION_FOR_AMD64_DIGEST=$ATTESTATION_FOR_AMD64_DIGEST"
    echo "ATTESTATION_FOR_ARM64_DIGEST=$ATTESTATION_FOR_ARM64_DIGEST"

    if [[ -n "$AMD64_DIGEST" ]]; then
      ./krane tag "$BIN_IMG_REGISTRY@$AMD64_DIGEST" "${VERSION}-amd64"
    fi
    if [[ -n "$ARM64_DIGEST" ]]; then
      ./krane tag "$BIN_IMG_REGISTRY@$ARM64_DIGEST" "${VERSION}-arm64"
    fi
    if [[ -n "$ATTESTATION_FOR_AMD64_DIGEST" ]]; then
      ./krane tag "$BIN_IMG_REGISTRY@$ATTESTATION_FOR_AMD64_DIGEST" "${VERSION}-amd64-attestation"
    fi
    if [[ -n "$ATTESTATION_FOR_ARM64_DIGEST" ]]; then
      ./krane tag "$BIN_IMG_REGISTRY@$ATTESTATION_FOR_ARM64_DIGEST" "${VERSION}-arm64-attestation"
    fi


   # Tag arch specific images for the legacy registries
   for arch in ${ALL_ARCH}
   do
       # krane is a variation of crane that supports k8s auth
       ./krane copy --platform linux/${arch} ${MULTIARCH_IMAGE} ${REGISTRY}/ingress-gce-${binary}-${arch}:${VERSION}
       for tag in ${ADDITIONAL_TAGS}
       do
           ./krane copy --platform linux/${arch} ${MULTIARCH_IMAGE} ${REGISTRY}/ingress-gce-${binary}-${arch}:${tag}
       done
   done
  echo "images are copied to arch specific registries"
done
