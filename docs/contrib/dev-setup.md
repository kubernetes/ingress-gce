# Overview

This document explains how to get started with developing for Ingress-GCE.
The below guide assumes you have installed the necessary binaries to run Golang.

## Get the code

It is suggested to create your own fork of the repository on Github. Once that
is done, go ahead and download the repo.

```console
cd $GOPATH/src
git clone https://github.com/[YOUR GITHUB_USER]/ingress-gce.git
```

## Unit tests

To execute the unit tests, run:

```console
make test
```

## Building

This assumes you have properly setup docker.

All ingress-gce binaries are built through a Makefile. Depending on your
requirements you can build a raw binary, a local container image,
or push an image to a remote repository. To build all binaries, run:

```console
make build
```

The resulting binaries can be found in bin/amd64/. To push an image up to a
repository, run the following:

```console
export REGISTRY=[MY CONTAINER REGISTRY]
make push
```
We suggest using [Google Container Registry](https://cloud.google.com/container-registry/docs/quickstart)
to store your images.

## Other considerations

The build uses dependencies in the `ingress/vendor` directory, which
must be installed before building a binary/image. Occasionally, you
might need to update the dependencies. In that case, you will need to install
the [dep](https://github.com/golang/dep) tool.
