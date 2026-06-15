REGISTRY?=icr.io/ai-services-private
IMAGE=litellm
# 1. Must run update-patch after updating the Litellm version
# 2. Manually regenerate the patches/watsonx-passthrough.patch
LITELLM_VERSION?=1.86.5
IMAGE_REVISION?=0
TAG?=v$(LITELLM_VERSION)-$(IMAGE_REVISION)
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

CONTAINER_BUILDER?=podman
build:
	$(CONTAINER_BUILDER) build -t $(REGISTRY)/$(IMAGE):$(TAG) --build-arg VERSION=$(LITELLM_VERSION) .
.PHONY: build

push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(REGISTRY)/$(IMAGE):$(TAG)
.PHONY: push

image-tag:
	@echo $(TAG)
.PHONY: image-tag

update-patch:
	cd ./patches && ./pyproject-toml.sh $(LITELLM_VERSION)
.PHONY: update-patch
