REGISTRY?=icr.io/ai-services-private
IMAGE=rag
TAG?=v0.0.4

CONTAINER_BUILDER?=podman
build:
	$(CONTAINER_BUILDER) build -t $(REGISTRY)/$(IMAGE):$(TAG) .
.PHONY: build

push:
	$(CONTAINER_BUILDER) push $(REGISTRY)/$(IMAGE):$(TAG)
.PHONY: push
