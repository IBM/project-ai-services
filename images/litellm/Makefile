REGISTRY?=icr.io/ai-services-private
IMAGE=litellm
LITELLM_VERSION?=1.84.0
IMAGE_REVISION?=1
VERSION?=$(LITELLM_VERSION)-$(IMAGE_REVISION)
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

CONTAINER_BUILDER?=podman
build:
	$(CONTAINER_BUILDER) build -t $(REGISTRY)/$(IMAGE):v$(VERSION) --build-arg VERSION=$(LITELLM_VERSION) .
.PHONY: build

push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(REGISTRY)/$(IMAGE):v$(VERSION)
.PHONY: push

image-tag:
	@echo v$(VERSION)
.PHONY: image-tag

update-patch:
	cd ./patches && ./pyproject-toml.sh $(LITELLM_VERSION)
.PHONY: update-patch
