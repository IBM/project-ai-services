IMAGE = ai-services
TAG ?= v0.0.514
REGISTRY ?= icr.io/ai-services-cicd
BRANCH_TAG ?= $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null | tr '/' '-' | grep -v 'main' || true)
BUILDTAGS := exclude_graphdriver_btrfs containers_image_openpgp remote
CONTAINER_BUILDER ?= podman
CREDS_ARG ?= $(if $(CONTAINER_BUILD_SECRET),--authfile $(CONTAINER_BUILD_SECRET),)
VERSION ?= $(shell git describe --tags --always)

.PHONY: all
all: build

.PHONY: build
build:
	CGO_ENABLED=1 go build \
     -tags "$(BUILDTAGS)" \
    -X 'github.com/project-ai-services/ai-services/cmd/ai-services/cmd/version.Version=$(VERSION)' \
    -o bin/ai-services cmd/ai-services/main.go

.PHONY: test-e2e-cli
test-e2e-cli:
	$(TEST_BASE) ./tests/e2e/cli/...

.PHONY: build-test-e2e-cli
build-test-e2e-cli:
	CGO_ENABLED=1 go test -c -o bin/e2e-cli.test \
			-tags "$(BUILDTAGS)" \
				-X 'github.com/project-ai-services/ai-services/cmd/ai-services/cmd/version.Version=$(VERSION)' \
					./tests/e2e/cli

.PHONY: container-build
container-build:
	$(CONTAINER_BUILDER) build --build-arg CMD_VERSION=$(VERSION) \
    --build-arg BUILDTAGS="$(BUILDTAGS)" -t $(REGISTRY)/$(IMAGE):$(TAG) .

.PHONY: container-push
container-push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(REGISTRY)/$(IMAGE):$(TAG)
ifneq ($(strip $(BRANCH_TAG)),)
	$(CONTAINER_BUILDER) tag $(REGISTRY)/$(IMAGE):$(TAG) $(REGISTRY)/$(IMAGE):$(BRANCH_TAG)
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(REGISTRY)/$(IMAGE):$(BRANCH_TAG)
endif

.PHONY: get-image-tag
get-image-tag:
	@echo $(TAG)
