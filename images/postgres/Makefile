REGISTRY?=icr.io/ai-services-private
IMAGE=postgres
TAG?=18
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

# Image references
IMAGE_TAG := $(REGISTRY)/$(IMAGE):$(TAG)

CONTAINER_BUILDER?=podman
build:
	$(CONTAINER_BUILDER) build -t $(IMAGE_TAG) .
.PHONY: build

push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(IMAGE_TAG)
.PHONY: push

image-tag:
	@echo $(TAG)
.PHONY: image-tag

# Development and testing targets
run:
	$(CONTAINER_BUILDER) run -d \
		--name postgres-dev \
		-e POSTGRES_PASSWORD=devpassword \
		-p 5432:5432 \
		-v postgres-data:/var/lib/pgsql \
		$(IMAGE_TAG)
.PHONY: run

stop:
	$(CONTAINER_BUILDER) stop postgres-dev
	$(CONTAINER_BUILDER) rm postgres-dev
.PHONY: stop

logs:
	$(CONTAINER_BUILDER) logs -f postgres-dev
.PHONY: logs

shell:
	$(CONTAINER_BUILDER) exec -it postgres-dev bash
.PHONY: shell

psql:
	$(CONTAINER_BUILDER) exec -it postgres-dev psql -U postgres
.PHONY: psql

clean:
	-$(CONTAINER_BUILDER) stop postgres-dev
	-$(CONTAINER_BUILDER) rm postgres-dev
	-$(CONTAINER_BUILDER) volume rm postgres-data
.PHONY: clean

test:
	@echo "Testing PostgreSQL image..."
	$(CONTAINER_BUILDER) run --rm \
		-e POSTGRES_PASSWORD=testpassword \
		$(IMAGE_TAG) postgres --version
	@echo "PostgreSQL image test passed!"
.PHONY: test

help:
	@echo "PostgreSQL Container Image Makefile"
	@echo ""
	@echo "Available targets:"
	@echo "  build        - Build the PostgreSQL image for current platform"
	@echo "  push         - Push the image to registry"
	@echo "  image-tag    - Print the full image tag"
	@echo "  run          - Run a development PostgreSQL container"
	@echo "  stop         - Stop and remove the development container"
	@echo "  logs         - Show logs from development container"
	@echo "  shell        - Open a shell in the development container"
	@echo "  psql         - Open psql client in the development container"
	@echo "  clean        - Remove development container and volume"
	@echo "  test         - Run basic image tests"
	@echo "  help         - Show this help message"
	@echo ""
	@echo "Variables:"
	@echo "  REGISTRY     - Container registry (default: $(REGISTRY))"
	@echo "  IMAGE        - Image name (default: $(IMAGE))"
	@echo "  TAG          - Image tag (default: $(TAG))"
	@echo "  CONTAINER_BUILDER - Container builder tool (default: $(CONTAINER_BUILDER))"
	@echo ""
	@echo "Examples:"
	@echo "  make build"
	@echo "  make push REGISTRY=myregistry.io"
.PHONY: help
