REGISTRY?=icr.io/ai-services-private
IMAGE=caddy
CADDY_VERSION?=v2.11.4
IMAGE_REVISION?=0
TAG?=$(CADDY_VERSION)-$(IMAGE_REVISION)
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

# Image references
IMAGE_TAG := $(REGISTRY)/$(IMAGE):$(TAG)

CONTAINER_BUILDER?=podman
build:
	$(CONTAINER_BUILDER) build --build-arg CADDY_VERSION=$(CADDY_VERSION) -t $(IMAGE_TAG) .
.PHONY: build

push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(IMAGE_TAG)
.PHONY: push

image-tag:
	@echo $(TAG)
.PHONY: image-tag
