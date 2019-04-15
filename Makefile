# Copyright 2016 The Kubernetes Authors.
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
# `make help` will show commonly used targets.
#

# Golang package.
PKG := k8s.io/ingress-gce

# List of binaries to build. You must have a matching Dockerfile.BINARY
# for each BINARY.
CONTAINER_BINARIES ?= \
	404-server \
	e2e-test \
	echo \
	fuzzer \
	glbc

# Latest commit hash for current branch.
GIT_COMMIT := $(shell git rev-parse HEAD)

# Registry to push to.
REGISTRY ?= gcr.io/google_containers

ARCH ?= amd64
ALL_ARCH := amd64

# Image to use for building.
BUILD_IMAGE ?= golang:1.12.3-alpine
# Containers will be named: $(CONTAINER_PREFIX)-$(BINARY)-$(ARCH):$(VERSION).
CONTAINER_PREFIX ?= ingress-gce

# This version-strategy uses git tags to set the version string
VERSION ?= $(shell git describe --tags --always --dirty)

# Set to 1 to print more verbose output from the build.
VERBOSE ?= 0

# Include standard build rules.
include build/rules.mk

# Additional rule to build an image of glbc for e2e testing.
# TODO(rramkumar): Find a way not to use "latest" as the tag.
push-e2e:
	@$(MAKE) --no-print-directory containers push
