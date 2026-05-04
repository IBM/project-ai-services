REGISTRY?=icr.io/ai-services-private
IMAGE=rag
TAG?=v0.0.99
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

CONTAINER_BUILDER?=podman
build:
	$(CONTAINER_BUILDER) build $(CREDS_ARG) -t $(REGISTRY)/$(IMAGE):$(TAG) .
.PHONY: build

push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(REGISTRY)/$(IMAGE):$(TAG)
.PHONY: push

image-tag:
	@echo $(TAG)
.PHONY: image-tag

run-similarity:
	PORT=$${PORT:-7000} python -m similarity.app
.PHONY: run-similarity

run-digitize:
	PORT=$${PORT:-4000} python -m digitize.app
.PHONY: run-digitize

run-chatbot:
	PORT=$${PORT:-5000} python -m chatbot.app
.PHONY: run-chatbot

run-summarize:
	PORT=$${PORT:-6000} python -m summarize.app
.PHONY: run-summarize
