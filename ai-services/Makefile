REGISTRY?=icr.io/ai-services-private
IMAGE=ai-services
TAG?=0.0.3
CONTAINER_BUILDER?=podman
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

GO := go
# Build tags
BUILDTAGS := exclude_graphdriver_btrfs containers_image_openpgp remote
BIN = ./bin

# This will be overridden when we build and upload binary to artifact
VERSION ?= $(shell git describe --tags --always)
GITCOMMIT ?= $(shell git rev-parse --short HEAD)
BUILDDATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# Multi-architecture build configuration
# Supported OS: darwin, linux
# Supported architectures: amd64 (x86), ppc64le, arm64
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# Define all target platforms
PLATFORMS := \
	darwin/amd64 \
	darwin/arm64 \
	linux/amd64 \
	linux/ppc64le

BUILD_CMD = CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(BUILDFLAGS) \
     -o $(BIN)/ai-services-$(GOOS)-$(GOARCH) \
     -tags "$(BUILDTAGS)" \
     -ldflags " \
    -X 'github.com/project-ai-services/ai-services/cmd/ai-services/cmd/version.Version=$(VERSION)' \
    -X 'github.com/project-ai-services/ai-services/cmd/ai-services/cmd/version.GitCommit=$(GITCOMMIT)' \
    -X 'github.com/project-ai-services/ai-services/cmd/ai-services/cmd/version.BuildDate=$(BUILDDATE)' \
     " \
     ./cmd/ai-services

REPORT_DIR := tests/e2e/reports
RUN_ID := $(shell date +%s)
export RUN_ID

TEST_BASE = ginkgo -r
TEST_PKG = ./tests/e2e
TEST_ARGS ?=
JUNIT_FLAG = --junit-report=report-$(RUN_ID).xml --output-dir=$(REPORT_DIR)

TEST_CMD = $(TEST_BASE) $(TEST_ARGS) $(TEST_PKG) \
	-- $(if $(APP_NAME),--app-name=$(APP_NAME),) \
        $(if $(DELETE_APP),--delete-app=$(DELETE_APP),) \
	$(if $(APP_RUNTIME),--runtime=$(APP_RUNTIME),)

.PHONY: default
default:
ifdef TEST
	$(TEST_CMD)
else
	$(BUILD_CMD)
endif

.PHONY: bin
bin:
	$(BUILD_CMD)

# Build binaries for all supported platforms
.PHONY: binaries
binaries:
	@echo "Building ai-services for all platforms..."
	@mkdir -p $(BIN)
	@$(foreach platform,$(PLATFORMS), \
		$(eval OS := $(word 1,$(subst /, ,$(platform)))) \
		$(eval ARCH := $(word 2,$(subst /, ,$(platform)))) \
		echo "Building for $(OS)/$(ARCH)..." && \
		CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) $(GO) build $(BUILDFLAGS) \
			-o $(BIN)/ai-services-$(OS)-$(ARCH) \
			-tags "$(BUILDTAGS)" \
			-ldflags " \
				-X 'github.com/project-ai-services/ai-services/cmd/ai-services/cmd/version.Version=$(VERSION)' \
				-X 'github.com/project-ai-services/ai-services/cmd/ai-services/cmd/version.GitCommit=$(GITCOMMIT)' \
				-X 'github.com/project-ai-services/ai-services/cmd/ai-services/cmd/version.BuildDate=$(BUILDDATE)' \
			" \
			./cmd/ai-services || exit 1; \
	)
	@echo "All binaries built successfully in $(BIN)/"
	@ls -lh $(BIN)/

# Build binary for specific OS and architecture
# Usage: make binary GOOS=linux GOARCH=ppc64le
.PHONY: binary
binary:
	@echo "Building ai-services for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(BIN)
	$(BUILD_CMD)
	@echo "Binary built: $(BIN)/ai-services-$(GOOS)-$(GOARCH)"

# Container image build
build:
	@echo "Building container image with catalog API enabled"
	$(CONTAINER_BUILDER) build --build-arg CMD_VERSION=$(VERSION) \
    --build-arg GITCOMMIT=$(GITCOMMIT) --build-arg BUILDDATE=$(BUILDDATE) \
    --build-arg BUILDTAGS="$(BUILDTAGS)" -t $(REGISTRY)/$(IMAGE):$(TAG) .
.PHONY: build

push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(REGISTRY)/$(IMAGE):$(TAG)
.PHONY: push

test:
	$(TEST_CMD)

test-generate-report:
	@echo "Using RUN_ID=$(RUN_ID)"
	$(TEST_BASE) $(TEST_ARGS) $(JUNIT_FLAG) $(TEST_PKG) \
     -- $(if $(APP_NAME),--app-name=$(APP_NAME),) \
        $(if $(DELETE_APP),--delete-app=$(DELETE_APP),) \
        $(if $(APP_RUNTIME),--runtime=$(APP_RUNTIME),)

.PHONY: swagger
swagger:
	@echo "Generating Swagger documentation..."
	@swag init -g internal/pkg/catalog/apiserver/apiserver.go -o docs --parseDependency --parseInternal
	@echo "Swagger docs generated at docs/"

.PHONY: swagger-fmt
swagger-fmt:
	@echo "Formatting Swagger comments..."
	@swag fmt --exclude tests

image-tag:
	@echo $(TAG)
