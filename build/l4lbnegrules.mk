# Copyright 2026 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

#
# Multi-arch `docker buildx` rules for the L4 LB and NEG controller images
# (l4lb-controller, neg-controller). Included from the top-level Makefile
# after build/rules.mk. glbc and the other images keep using build/rules.mk.
#
# Each image has a self-contained multi-stage Dockerfile.<name> that
# cross-compiles Go in a builder stage, so no bin/ handoff is needed.
#
# Reuses REGISTRY, VERSION, GIT_COMMIT and BUILD_IMAGE from the Makefile.
#

IMAGE_NAMES          ?= l4lb-controller neg-controller
# Images are named $(REGISTRY)/$(IMAGE_PREFIX)-<name>:$(VERSION),
# e.g. gcr.io/k8s-image-staging/gcp-l4lb-controller:v1.2.3.
IMAGE_PREFIX         ?= gcp
IMAGE_PLATFORMS      ?= linux/amd64,linux/arm64
# load    - single (host) platform, loaded into the local docker daemon
# push    - multi-arch index pushed to $(REGISTRY)
# oci     - OCI image-layout tarball per image in $(IMAGE_OUTPUT_DIR)
# oci-dir - OCI image-layout directory per image in $(IMAGE_OUTPUT_DIR)
# none    - build only; result stays in the buildx cache
IMAGE_OUTPUT         ?= load
IMAGE_OUTPUT_DIR     ?= _output/images
# Extra image tags in addition to $(VERSION), e.g. IMAGE_TAGS="latest foo".
IMAGE_TAGS           ?=
# Name of the buildx builder instance (docker-container driver) used for the
# builds. Override to reuse an existing builder, e.g. in CI.
IMAGE_BUILDX_BUILDER ?= ingress-gce
# Extra flags for docker buildx build, e.g. --provenance=false, --sbom=true, --cache-to ...
IMAGE_BUILDX_FLAGS   ?=

HOST_ARCH := $(shell uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')

# $* (the image name) is expanded when the recipe runs.
ifeq ($(IMAGE_OUTPUT),load)
  # docker can only load a single platform; default to the host's.
  IMAGE_PLATFORMS := linux/$(HOST_ARCH)
  image_output_flags = --load
else ifeq ($(IMAGE_OUTPUT),push)
  image_output_flags = --push
else ifeq ($(IMAGE_OUTPUT),oci)
  image_output_flags = --output type=oci,dest=$(IMAGE_OUTPUT_DIR)/$(IMAGE_PREFIX)-$*-$(VERSION).tar
else ifeq ($(IMAGE_OUTPUT),oci-dir)
  image_output_flags = --output type=oci,tar=false,dest=$(IMAGE_OUTPUT_DIR)/$(IMAGE_PREFIX)-$*-$(VERSION)
else ifeq ($(IMAGE_OUTPUT),none)
  image_output_flags = --output type=cacheonly
else
  $(error IMAGE_OUTPUT must be one of: load push oci oci-dir none)
endif

IMAGE_TARGETS := $(addprefix image-,$(IMAGE_NAMES))

.PHONY: images images-push images-oci images-help buildx-builder $(IMAGE_TARGETS)

# Build all images in IMAGE_NAMES with the selected IMAGE_OUTPUT.
images: $(IMAGE_TARGETS)

# Build one image, e.g. `make image-neg-controller IMAGE_OUTPUT=oci`.
# Static pattern rule (rather than `image-%:`) so the targets can be .PHONY
# and unknown names fail with "No rule to make target".
$(IMAGE_TARGETS): image-%: buildx-builder
	@case "$(IMAGE_OUTPUT)" in oci|oci-dir) mkdir -p $(IMAGE_OUTPUT_DIR);; esac
	docker buildx build \
	  --builder $(IMAGE_BUILDX_BUILDER) \
	  --platform $(IMAGE_PLATFORMS) \
	  --file Dockerfile.$* \
	  --build-arg GOLANG_IMAGE=$(BUILD_IMAGE) \
	  --build-arg VERSION=$(VERSION) \
	  --build-arg GIT_COMMIT=$(GIT_COMMIT) \
	  --tag $(REGISTRY)/$(IMAGE_PREFIX)-$*:$(VERSION) \
	  $(foreach t,$(IMAGE_TAGS),--tag $(REGISTRY)/$(IMAGE_PREFIX)-$*:$(t)) \
	  $(image_output_flags) \
	  $(IMAGE_BUILDX_FLAGS) \
	  .

# Shortcuts for the push and OCI tarball outputs.
images-push:
	@$(MAKE) --no-print-directory images IMAGE_OUTPUT=push

images-oci:
	@$(MAKE) --no-print-directory images IMAGE_OUTPUT=oci

# A docker-container builder is required for multi-platform builds and the oci exporter.
buildx-builder:
	@docker buildx inspect $(IMAGE_BUILDX_BUILDER) >/dev/null 2>&1 || \
	  docker buildx create --name $(IMAGE_BUILDX_BUILDER) --driver docker-container --bootstrap

images-help:
	@echo "  images                   build $(IMAGE_NAMES) (IMAGE_OUTPUT=$(IMAGE_OUTPUT))"
	@echo "  image-<name>             build a single image, e.g. image-neg-controller"
	@echo "  images-push              push multi-arch images to \$$(REGISTRY)"
	@echo "  images-oci               write OCI tarballs to $(IMAGE_OUTPUT_DIR)/"
	@echo ""
	@echo "  IMAGE_OUTPUT             load|push|oci|oci-dir|none (default: load)"
	@echo "  IMAGE_PREFIX             image name prefix (default: $(IMAGE_PREFIX), e.g. $(IMAGE_PREFIX)-l4lb-controller)"
	@echo "  IMAGE_PLATFORMS          default: linux/amd64,linux/arm64 (host arch for load)"
	@echo "  IMAGE_TAGS               extra tags besides \$$(VERSION)"
	@echo "  IMAGE_BUILDX_FLAGS       extra docker buildx flags, e.g. --provenance=false"
