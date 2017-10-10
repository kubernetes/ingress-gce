all: push

# 0.0 shouldn't clobber any released builds
TAG=0.9.7
PREFIX=gcr.io/google_containers/glbc
PKG=k8s.io/ingress-gce

test:
	go test $(shell go list ./... | grep -v /vendor/)

server:
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags '-w' -o glbc ${PKG}/cmd/glbc

container: server
	docker build --pull -t $(PREFIX):$(TAG) .

push: container
	gcloud docker -- push $(PREFIX):$(TAG)

clean:
	rm -f glbc
