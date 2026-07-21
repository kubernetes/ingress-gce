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
# * PUSH (example: "true" or "false", default: "true")

set -o errexit
set -o nounset
set -o pipefail
set -o xtrace

# docker buildx is in /root/.docker/cli-plugins/docker-buildx in Cloud Build container
if [[ -w "/root" ]]; then
  HOME=/root
fi

REPO_ROOT=$(git rev-parse --show-toplevel)
cd ${REPO_ROOT}

BINARIES=${BINARIES:-"psc-e2e-test neg-e2e-test ingress-controller-e2e-test"}
ALL_ARCH=${ALL_ARCH:-"amd64 arm64"}
REGISTRY=${REGISTRY:-"gcr.io/example"}
VERSION=${VERSION:-"test"}
ADDITIONAL_TAGS=${ADDITIONAL_TAGS:-""}
PUSH=${PUSH:-"true"}

echo BINARIES=${BINARIES}
echo ALL_ARCH=${ALL_ARCH}
echo REGISTRY=${REGISTRY}
echo VERSION=${VERSION}
echo ADDITIONAL_TAGS=${ADDITIONAL_TAGS}
echo PUSH=${PUSH}

echo "building all binaries"
make all-build ALL_ARCH="${ALL_ARCH}" CONTAINER_BINARIES="${BINARIES}"

# To create cross compiled images
echo "setting up docker buildx.."
docker buildx install
docker buildx create --use

# Download and verify crane/krane cli
KRANE_VERSION="v0.21.5"
OS="$(uname -s)"
case "$(uname -m)" in
  x86_64|amd64) ARCH="x86_64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv6l) ARCH="armv6" ;;
  i386|i686) ARCH="i386" ;;
  ppc64le) ARCH="ppc64le" ;;
  riscv64) ARCH="riscv64" ;;
  s390x) ARCH="s390x" ;;
  *) ARCH="$(uname -m)" ;;
esac

TARBALL="go-containerregistry_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/google/go-containerregistry/releases/download/${KRANE_VERSION}/${TARBALL}"

# Pinned SHA-256 checksums for go-containerregistry v0.21.5 release archives
declare -A EXPECTED_SHA256=(
  ["go-containerregistry_Darwin_arm64.tar.gz"]="a41938cdbd8becc59e90f4bb491a557d52dce5681f9812c961854cab706f5f59"
  ["go-containerregistry_Darwin_x86_64.tar.gz"]="d5ad8d97d7c5407f761b4fd37801044473f58b79a033f6a64e84ce6d010c1b2c"
  ["go-containerregistry_Linux_arm64.tar.gz"]="3a47c6da5a0ba1ca7a93def41036d8f262a2160799e5d4ca25dba3cfa47dab41"
  ["go-containerregistry_Linux_armv6.tar.gz"]="eacb7bd81d3dc4416039a3c0b8e6c9681aedc3292b235497ffa98c19a255fea2"
  ["go-containerregistry_Linux_i386.tar.gz"]="36270acd77226feed93f99631b1ba10d59d4b9895ad79fa3ae3a4454b58b508d"
  ["go-containerregistry_Linux_ppc64le.tar.gz"]="16ff490d3b654c5e63a293ae5d37084d276e2be80462ca4a039a63faeaf75324"
  ["go-containerregistry_Linux_riscv64.tar.gz"]="32dea5e8bc279931908ddbf626e0bf2f328f7b981f088b55f9e7c2d7d640f341"
  ["go-containerregistry_Linux_s390x.tar.gz"]="416775c7936e9010e3da697d5bf80e989f68110b427d5d13404688a5e69d7e80"
  ["go-containerregistry_Linux_x86_64.tar.gz"]="9f823ae5ee25803161110f957b5fd4538f714d40cdf25dacb4914fefafd246bf"
  ["go-containerregistry_Windows_arm64.tar.gz"]="87efde0a3a724775cfe775c767dbf6e37862422c961c66a1d19726041dbddd85"
  ["go-containerregistry_Windows_x86_64.tar.gz"]="f2576640521c6962bab26e37a4b87f168fcd13bebbf9c5355c8ae33417c7ca5a"
)

SHA256="${EXPECTED_SHA256[${TARBALL}]:-}"
if [[ -z "${SHA256}" ]]; then
  echo "Error: Unsupported platform ${OS}_${ARCH} for krane download verification."
  exit 1
fi

TMP_DIR=$(mktemp -d)
trap 'rm -rf "${TMP_DIR}"' EXIT

KRANE="${TMP_DIR}/krane"
curl -sL "${DOWNLOAD_URL}" -o "${TMP_DIR}/${TARBALL}"
echo "${SHA256}  ${TMP_DIR}/${TARBALL}" | sha256sum -c -
tar -xzf "${TMP_DIR}/${TARBALL}" -C "${TMP_DIR}" krane

# function that tags the specified digest with ADDITIONAL_TAGS with suffixes
tag_image_variant() {
  local digest="$1"
  local suffix="$2"

  if [[ -z "$digest" ]]; then
    return
  fi

  "${KRANE}" tag "$BIN_IMG_REGISTRY@$digest" "${VERSION}-${suffix}"
  for tag in ${ADDITIONAL_TAGS}
  do
    "${KRANE}" tag "$BIN_IMG_REGISTRY@$digest" "${tag}-${suffix}"
  done
}

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

    BUILD_ACTION="--push"
    if [[ "${PUSH}" == "false" ]]; then
        BUILD_ACTION=""
    fi

    docker buildx build ${BUILD_ACTION}  \
        --platform ${PLATFORMS}  \
        ${tags} \
        -f Dockerfile.${binary} .

    if [[ "${PUSH}" == "true" ]]; then
        echo "done, pushed $MULTIARCH_IMAGE image"
    else
        echo "done, built $MULTIARCH_IMAGE image locally (skipped push)"
        continue
    fi

    # read the manifest to tag all arch specific images
    # the manifest is parsed to extract the digests of the per arch images plus
    # the attestation manifests.
    # the extracted digests are then used to tag the images so that later
    # cleanup can work reliably
    MANIFEST_JSON=$("${KRANE}" manifest "$MULTIARCH_IMAGE")

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

    tag_image_variant "$AMD64_DIGEST" "amd64"
    tag_image_variant "$ARM64_DIGEST" "arm64"
    tag_image_variant "$ATTESTATION_FOR_AMD64_DIGEST" "amd64-attestation"
    tag_image_variant "$ATTESTATION_FOR_ARM64_DIGEST" "arm64-attestation"


   # Tag arch specific images for the legacy registries
   for arch in ${ALL_ARCH}
   do
       # krane is a variation of crane that supports k8s auth
       "${KRANE}" copy --platform linux/${arch} ${MULTIARCH_IMAGE} ${REGISTRY}/ingress-gce-${binary}-${arch}:${VERSION}
       for tag in ${ADDITIONAL_TAGS}
       do
           "${KRANE}" copy --platform linux/${arch} ${MULTIARCH_IMAGE} ${REGISTRY}/ingress-gce-${binary}-${arch}:${tag}
       done
   done
  echo "images are copied to arch specific registries"
done
