REGISTRY?=icr.io/ai-services-private
IMAGE=rag
TAG?=v0.0.80
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

CONTAINER_BUILDER?=podman
build:
	$(CONTAINER_BUILDER) build $(CREDS_ARG) -t $(REGISTRY)/$(IMAGE):$(TAG) .
.PHONY: build

GOOS=linux GOARCH=ppc64le

push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(REGISTRY)/$(IMAGE):$(TAG)
.PHONY: push

image-tag:
	@echo $(TAG)
.PHONY: image-tag

# ── Local dev: run each service directly (no container) ─────────────────────
# Each service is a standalone FastAPI app started via its module entry point.
# PORT can be overridden:  make run-similarity PORT=8080
# LOG_LEVEL can be overridden: make run-chatbot LOG_LEVEL=debug

run-similarity:
	PORT=$${PORT:-7000} python -m similarity.app
.PHONY: run-similarity

run-chatbot:
	PORT=$${PORT:-5000} python -m chatbot.app
.PHONY: run-chatbot

run-summarize:
	PORT=$${PORT:-6000} python -m summarize.app
.PHONY: run-summarize
