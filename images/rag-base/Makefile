REGISTRY?=icr.io/ai-services-private
IMAGE=rag-base
TAG?=v0.0.16
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

CONTAINER_BUILDER?=podman
build:
	cd docling && $(CONTAINER_BUILDER) build -t docling:v2.60.1 . && cd ..
	$(CONTAINER_BUILDER) build -t $(REGISTRY)/$(IMAGE):$(TAG) .
.PHONY: build

push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(REGISTRY)/$(IMAGE):$(TAG)
.PHONY: push

image-tag:
	@echo $(TAG)
.PHONY: image-tag
