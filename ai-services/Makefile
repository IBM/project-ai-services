REGISTRY?=icr.io/ai-services-private
IMAGE=ai-services
TAG?=0.0.1
CONTAINER_BUILDER?=podman

GO := go
# Feature flags - set ENABLE_CATALOG_API=true to include catalog API server
# Default is false - catalog API is opt-in for development/CI/CD only
ENABLE_CATALOG_API ?= false
BUILDTAGS := exclude_graphdriver_btrfs containers_image_openpgp remote
ifeq ($(ENABLE_CATALOG_API),true)
    BUILDTAGS := $(BUILDTAGS) catalog_api
endif
BIN = ./bin

# This will be overriden when we build and upload binary to artifact
VERSION ?= $(shell git describe --tags --always)
GITCOMMIT ?= $(shell git rev-parse --short HEAD)
BUILDDATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

BUILD_CMD = CGO_ENABLED=0 $(GO) build $(BUILDFLAGS) \
     -o $(BIN)/ai-services \
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
        $(if $(DELETE_APP),--delete-app=$(DELETE_APP),)

.PHONY: default
default:
ifdef TEST
	$(TEST_CMD)
else
	$(BUILD_CMD)
endif

.PHONY: build
build:
	$(BUILD_CMD)

# Container image build - catalog API enabled by default for containers
# Override with: make image ENABLE_CATALOG_API=false
image:
	@echo "Building container image with ENABLE_CATALOG_API=$(ENABLE_CATALOG_API)"
	$(CONTAINER_BUILDER) build --build-arg CMD_VERSION=$(VERSION) \
    --build-arg GITCOMMIT=$(GITCOMMIT) --build-arg BUILDDATE=$(BUILDDATE) \
    --build-arg BUILDTAGS="$(BUILDTAGS)" -t $(REGISTRY)/$(IMAGE):$(TAG) .
.PHONY: image

test:
	$(TEST_CMD)

test-generate-report:
	@echo "Using RUN_ID=$(RUN_ID)"
	$(TEST_BASE) $(TEST_ARGS) $(JUNIT_FLAG) $(TEST_PKG) \
     -- $(if $(APP_NAME),--app-name=$(APP_NAME),) \
        $(if $(DELETE_APP),--delete-app=$(DELETE_APP),)

.PHONY: swagger
swagger:
	@echo "Generating Swagger documentation..."
	@swag init -g internal/pkg/catalog/apiserver/apiserver.go -o docs --parseDependency --parseInternal
	@echo "Swagger docs generated at docs/"

.PHONY: swagger-fmt
swagger-fmt:
	@echo "Formatting Swagger comments..."
	@swag fmt --exclude tests
