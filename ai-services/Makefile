GO := go
BUILDTAGS := "exclude_graphdriver_btrfs containers_image_openpgp remote"
BIN = ./bin

# This will be overriden when we build and upload binary to artifact
VERSION ?= $(shell git describe --tags --always)
GITCOMMIT ?= $(shell git rev-parse --short HEAD)
BUILDDATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

BUILD_CMD = CGO_ENABLED=0 $(GO) build $(BUILDFLAGS) \
     -o $(BIN)/ai-services \
     -tags $(BUILDTAGS) \
     -ldflags " \
    -X 'github.com/project-ai-services/ai-services/cmd/ai-services/cmd/version.Version=$(VERSION)' \
    -X 'github.com/project-ai-services/ai-services/cmd/ai-services/cmd/version.GitCommit=$(GITCOMMIT)' \
    -X 'github.com/project-ai-services/ai-services/cmd/ai-services/cmd/version.BuildDate=$(BUILDDATE)' \
     " \
     ./cmd/ai-services

TEST_CMD = ginkgo $(TEST_ARGS) -r ./tests/e2e
REPORT_DIR := tests/e2e/reports
RUN_ID := $(shell date +%s)
export RUN_ID

TEST_BASE = ginkgo -r
TEST_PKG = ./tests/e2e
TEST_ARGS ?=
JUNIT_FLAG = --junit-report=report-$(RUN_ID).xml --output-dir=$(REPORT_DIR)



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

.PHONY: test
test:
	$(TEST_BASE) $(TEST_ARGS) $(TEST_PKG)

.PHONY: test-generate-report
test-generate-report:
	@echo "Using RUN_ID=$(RUN_ID)"
	$(TEST_BASE) $(TEST_ARGS) $(JUNIT_FLAG) $(TEST_PKG)
    