REGISTRY?=icr.io/ai-services-private
IMAGE=rag-ui
TAG?=v0.0.3

CONTAINER_BUILDER?=podman
build:
	$(CONTAINER_BUILDER) build -t $(REGISTRY)/$(IMAGE):$(TAG) .
.PHONY: build

push:
	$(CONTAINER_BUILDER) push $(REGISTRY)/$(IMAGE):$(TAG)
.PHONY: push
