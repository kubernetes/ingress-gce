all: gofmt build-binary-in-docker run-test-in-docker clean cross

TAG?=dev
REGISTRY?=eu.gcr.io/managed-certs-gke
NAME=managed-certificate-controller
DOCKER_IMAGE=${REGISTRY}/${NAME}:${TAG}

# Builds the managed certs controller binary
build-binary: clean deps
	godep go build -o ${NAME}

# Builds the managed certs controller binary using a docker builder image
build-binary-in-docker: docker-builder
	docker run -v `pwd`:/gopath/src/github.com/GoogleCloudPlatform/gke-managed-certs/ ${NAME}-builder:latest bash -c 'cd /gopath/src/github.com/GoogleCloudPlatform/gke-managed-certs && make build-binary'

clean:
	rm -f ${NAME}

cross:
	/google/data/ro/teams/opensource/cross .

deps:
	go get github.com/tools/godep

# Builds and pushes a docker image with managed certs controller binary
docker:
	docker build --pull -t ${DOCKER_IMAGE} .
	docker push ${DOCKER_IMAGE}

# Builds and pushes a docker image with managed certs controller binary - for CI, activates a service account before pushing
docker-ci:
	docker build --pull -t ${DOCKER_IMAGE} .
	gcloud auth activate-service-account --key-file=/etc/service-account/service-account.json
	gcloud auth configure-docker
	docker push ${DOCKER_IMAGE}

# Builds a builder image, i. e. an image used to later build a managed certs binary.
docker-builder:
	docker build -t ${NAME}-builder builder

# Formats go source code with gofmt
gofmt:
	gofmt -w main.go
	gofmt -w pkg
	gofmt -w http-hello

# Builds the managed certs controller binary, then a docker image with this binary, and pushes the image, for dev
release: build-binary-in-docker run-test-in-docker docker clean
	make -C http-hello

# Builds the managed certs controller binary, then a docker image with this binary, and pushes the image, for continuous integration
release-ci: build-binary-in-docker run-test-in-docker docker-ci
	make -C http-hello

run-test-in-docker: docker-builder
	docker run -v `pwd`:/gopath/src/github.com/GoogleCloudPlatform/gke-managed-certs/ ${NAME}-builder:latest bash -c 'cd /gopath/src/github.com/GoogleCloudPlatform/gke-managed-certs && make test'

test:
	godep go test ./pkg/... -cover

.PHONY: all build-binary build-binary-in-docker build-dev clean cross deps docker docker-builder docker-ci release release-ci run-test-in-docker test
