REGISTRY ?= icr.io/ai-services-private
IMAGE = chatbot-ui
TAG ?= v0.0.62
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

CONTAINER_BUILDER?=podman
build:
	$(CONTAINER_BUILDER) build -t $(REGISTRY)/$(IMAGE):$(TAG) .
.PHONY: build

push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(REGISTRY)/$(IMAGE):$(TAG)
ifneq ($(strip $(BRANCH_TAG)),)
	$(CONTAINER_BUILDER) tag $(REGISTRY)/$(IMAGE):$(TAG) $(REGISTRY)/$(IMAGE):$(BRANCH_TAG)
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(REGISTRY)/$(IMAGE):$(BRANCH_TAG)
endif
.PHONY: push

image-tag:
	@echo $(TAG)
.PHONY: image-tag
