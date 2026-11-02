REGISTRY ?= icr.io/ai-services-private
IMAGE ?= digitize-ui
TAG ?= v0.0.43
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

CONTAINER_BUILDER ?= podman

FULL_IMAGE := $(REGISTRY)/$(IMAGE):$(TAG)

build:
	$(CONTAINER_BUILDER) build -t $(FULL_IMAGE) .
.PHONY: build

push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(FULL_IMAGE)
ifneq ($(strip $(BRANCH_TAG)),)
	$(CONTAINER_BUILDER) tag $(FULL_IMAGE) $(REGISTRY)/$(IMAGE):$(BRANCH_TAG)
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(REGISTRY)/$(IMAGE):$(BRANCH_TAG)
endif
.PHONY: push

image-tag:
	@echo $(TAG)
.PHONY: image-tag
