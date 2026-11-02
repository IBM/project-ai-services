REGISTRY?=icr.io/ai-services-private
CONTAINER_BUILDER?=podman
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

SERVICE_MAKEFILE := $(SERVICE)/Makefile

ifeq ($(strip $(SERVICE)),)
$(error SERVICE is required. Usage: make build SERVICE=chatbot)
endif

ifeq ($(wildcard $(SERVICE_MAKEFILE)),)
$(error Unknown service '$(SERVICE)'. Expected services/<name>/Makefile)
endif

include $(SERVICE_MAKEFILE)

build:
	$(CONTAINER_BUILDER) build $(CREDS_ARG) -f $(SERVICE)/Containerfile -t $(REGISTRY)/$(IMAGE):$(TAG) .
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
