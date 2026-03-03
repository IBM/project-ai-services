REGISTRY ?= icr.io/ai-services-private
IMAGE ?= catalog-ui
TAG ?= v0.0.1
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

CONTAINER_BUILDER ?= podman

FULL_IMAGE := $(REGISTRY)/$(IMAGE):$(TAG)

.PHONY: build push run stop rm logs clean image-tag

build:
	$(CONTAINER_BUILDER) build -t $(FULL_IMAGE) .

push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(FULL_IMAGE)

run:
	$(CONTAINER_BUILDER) run -d \
		--name $(IMAGE) \
		-p 3000:3000 \
		$(FULL_IMAGE)

stop:
	-$(CONTAINER_BUILDER) stop $(IMAGE)

rm:
	-$(CONTAINER_BUILDER) rm $(IMAGE)

logs:
	$(CONTAINER_BUILDER) logs -f $(IMAGE)

clean: stop rm
	-$(CONTAINER_BUILDER) rmi $(FULL_IMAGE)

image-tag:
	@echo $(TAG)
