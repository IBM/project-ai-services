REGISTRY?=icr.io/ai-services-private
IMAGE=ai-services-mcp
TAG?=latest
CONTAINER_BUILDER?=podman
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

GO := go
BIN = ./bin
BINARY = ai-services-mcp
CMD = ./cmd/ai-services-mcp/main.go

VERSION ?= $(shell git describe --tags --always)
GITCOMMIT ?= $(shell git rev-parse --short HEAD)
BUILDDATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

PLATFORMS := \
	darwin/amd64 \
	darwin/arm64 \
	linux/amd64 \
	linux/ppc64le

.PHONY: build build-binary build-optimized build-container push clean clean-coverage \
        test test-coverage test-coverage-html lint run install \
        cross-compile binary image-tag help

# Build container image (used by CI via build-and-publish-image.yml)
build:
	$(CONTAINER_BUILDER) build -t $(REGISTRY)/$(IMAGE):$(TAG) -f Containerfile .

# Build the Go binary (native)
build-binary:
	$(GO) build -o $(BIN)/$(BINARY) $(CMD)

# Build with size optimizations (native)
build-optimized:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
	$(GO) build -ldflags="-s -w" -o $(BIN)/$(BINARY)-$(GOOS)-$(GOARCH) $(CMD)

# Build container image
build-container:
	$(CONTAINER_BUILDER) build -t $(REGISTRY)/$(IMAGE):$(TAG) -f Containerfile .

# Push container image
push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(REGISTRY)/$(IMAGE):$(TAG)

# Build binary for a specific OS/ARCH
# Usage: make binary GOOS=linux GOARCH=ppc64le
binary:
	@echo "Building $(BINARY) for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(BIN)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
	$(GO) build -ldflags="-s -w" -o $(BIN)/$(BINARY)-$(GOOS)-$(GOARCH) $(CMD)
	@echo "Binary built: $(BIN)/$(BINARY)-$(GOOS)-$(GOARCH)"

# Cross-compile for all supported platforms
cross-compile: clean
	@echo "Building $(BINARY) for all platforms..."
	@mkdir -p $(BIN)
	@$(foreach platform,$(PLATFORMS), \
		$(eval OS := $(word 1,$(subst /, ,$(platform)))) \
		$(eval ARCH := $(word 2,$(subst /, ,$(platform)))) \
		echo "Building for $(OS)/$(ARCH)..." && \
		CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) \
		$(GO) build -ldflags="-s -w" \
			-o $(BIN)/$(BINARY)-$(OS)-$(ARCH) \
			$(CMD) || exit 1; \
	)
	@echo "All binaries built in $(BIN)/"
	@ls -lh $(BIN)/

# Run tests
test:
	$(GO) test ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@$(GO) test -coverprofile=coverage.out ./...
	@echo ""
	@echo "Overall Coverage:"
	@echo "================"
	@$(GO) tool cover -func=coverage.out | grep "total:" | awk '{print "Total Coverage: " $$3}'

# Generate HTML coverage report
test-coverage-html: test-coverage
	@echo "Generating HTML coverage report..."
	@$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report saved to coverage.html"

# Run linter
lint:
	golangci-lint run ./...

# Clean built artifacts
clean:
	rm -f $(BIN)/$(BINARY) $(BIN)/$(BINARY)-*

# Clean test coverage files
clean-coverage:
	rm -f coverage.out coverage.html

# Run the application
run:
	./$(BIN)/$(BINARY) --help

# Install/update dependencies
install:
	$(GO) mod tidy
	$(GO) mod download

# Print the current image tag
image-tag:
	@echo $(TAG)

# Show help
help:
	@echo "Available targets:"
	@echo "  build              - Build container image (used by CI)"
	@echo "  build-binary       - Build the Go binary (native)"
	@echo "  build-optimized    - Build with size optimizations"
	@echo "  build-container    - Build container image"
	@echo "  push               - Push container image to registry"
	@echo "  binary             - Build for specific GOOS/GOARCH"
	@echo "  cross-compile      - Build for all supported platforms"
	@echo "  test               - Run tests"
	@echo "  test-coverage      - Run tests with coverage report"
	@echo "  test-coverage-html - Generate HTML coverage report"
	@echo "  lint               - Run golangci-lint"
	@echo "  clean              - Remove built artifacts"
	@echo "  clean-coverage     - Remove coverage files"
	@echo "  run                - Run the application with --help"
	@echo "  install            - Install/update dependencies"
	@echo "  image-tag          - Print current image tag"
	@echo "  help               - Show this help message"