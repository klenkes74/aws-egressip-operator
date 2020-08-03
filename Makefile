
# Image URL to use all building/pushing image targets
NAME ?= aws-egressip-operator
MODULE ?= github.com/klenkes74/egress-ipam-operator
REGISTRY ?= quay.io
REPOSITORY ?= $(REGISTRY)/klenkes74/aws-egressip-operator

VERSION := 1.1.0
IMG := $(REPOSITORY):$(VERSION)

BUILD_COMMIT := $(shell ./scripts/build/get-build-commit.sh)
BUILD_TIMESTAMP := $(shell ./scripts/build/get-build-timestamp.sh)
BUILD_HOSTNAME := $(shell ./scripts/build/get-build-hostname.sh)

LDFLAGS := "-X $(MODULE)/version.Version=$(VERSION) \
	-X $(MODULE)/version.Vcs=$(BUILD_COMMIT) \
	-X $(MODULE)/version.Timestamp=$(BUILD_TIMESTAMP) \
	-X $(MODULE)/version.Hostname=$(BUILD_HOSTNAME)"

all: container

lint: generate fmt vet
	golint ./pkg/... ./cmd/...

test: lint generate fmt vet
	go test ./test/... -coverprofile cover.out

# Build manager binary
manager: test
	go build -o build/_output/bin/$(NAME)  -ldflags $(LDFLAGS) github.com/klenkes74/egressip-ipam-operator/cmd/manager

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet
	go run ./cmd/manager/main.go

# Install CRDs into a cluster
install:
	cat deploy/crds/*crd.yaml | kubectl apply -f-

fmt:
	go fmt ./pkg/... ./cmd/...

vet:
	go vet ./pkg/... ./cmd/...

generate:
	go generate ./pkg/... ./cmd/...


podman-login:
	@podman login -u $(DOCKER_USER) -p $(DOCKER_PASSWORD) $(REGISTRY)


container: manager
	@podman build -t $(IMG) -f Dockerfile

push: container publish

publish:
	@podman push $(IMG)

tag-dev:
	@podman tag $(IMG) $(REPOSITORY):dev

push-dev: tag-dev publish-dev

publish-dev:
	@podman push $(REPOSITORY):dev

tag-release: container
	@podman tag $(IMG) $(REPOSITORY):$(VERSION)

push-release: tag-release
	@podman push $(REPOSITORY):$(VERSION)