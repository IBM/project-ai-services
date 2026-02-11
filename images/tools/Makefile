REGISTRY?=icr.io/ai-services-private
IMAGE=tools
TAG?=0.6
SR_VERSION=2.2.5-2
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

# Fix: Execute shell command to get the architecture
# We map 'x86_64' to 'amd64' and 'aarch64' to 'arm64' to match standard OCI platform naming.
UNAME_M := $(shell uname -m)
ifeq ($(UNAME_M),x86_64)
    PLATFORM ?= amd64
else ifeq ($(UNAME_M),aarch64)
    PLATFORM ?= arm64
else
    PLATFORM ?= $(UNAME_M)
endif

# Image references
BASE_REF    := $(REGISTRY)/$(IMAGE)
ARCH_TAG    := $(BASE_REF):$(TAG)-$(PLATFORM)
MANIFEST_TAG := $(BASE_REF):$(TAG)

FULL_IMAGE_TAG = $(REGISTRY)/$(IMAGE):$(TAG)-$(PLATFORM)

CONTAINER_BUILDER?=podman
build:
	$(CONTAINER_BUILDER) build --platform linux/$(PLATFORM) --build-arg SR_VERSION=$(SR_VERSION) -t $(ARCH_TAG) .
.PHONY: build

push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(ARCH_TAG)
.PHONY: push


ARCHES ?= amd64 arm64 ppc64le
manifest:
	@echo "Creating manifest $(MANIFEST_TAG) for architectures: $(ARCHES)"
	-$(CONTAINER_BUILDER) manifest rm $(MANIFEST_TAG)
	$(CONTAINER_BUILDER) manifest create $(MANIFEST_TAG)
	@set -e; \
	for arch in $(ARCHES); do \
		echo "Adding $(BASE_REF):$(TAG)-$$arch to manifest..."; \
		$(CONTAINER_BUILDER) manifest add $(CREDS_ARG) $(MANIFEST_TAG) docker://$(BASE_REF):$(TAG)-$$arch; \
	done
	$(CONTAINER_BUILDER) manifest push $(CREDS_ARG) --all $(MANIFEST_TAG) docker://$(MANIFEST_TAG)
.PHONY: manifest

image-tag:
	@echo $(TAG)-$(PLATFORM)
.PHONY: image-tag
