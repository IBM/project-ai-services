REGISTRY?=icr.io/ai-services-private
IMAGE=litellm
# When updating LITELLM_VERSION:
# 1. Run update-patch to regenerate patches/litellm/pyproject-toml.patch
# 2. Manually update patches/litellm/watsonx-passthrough.patch if the patched file changed upstream
# 3. Run verify-patch to confirm all patches apply cleanly
# 4. Run update-checksums to regenerate the checksums
LITELLM_VERSION?=1.106.4
LITELLM_SHA256?=dc58da933f25e8efec12348a4821b71e15e358d97093821576073fee37c49589
IMAGE_REVISION?=0
PYTHON_VERSION?=3.14
# Node.js version for baking Prisma CLI — Active LTS version
NODE_VERSION?=24.21.0
NODE_SHA256?=51c5d53066ee92920b61783b03b06ecb99f3661160f7b15ca77e8474115a67bc
# Rust toolchain version used to build the litellm wheels and the prisma engines
RUST_VERSION?=1.94.1
# protoc version from https://github.com/protocolbuffers/protobuf/releases
# PROTOC_SHA256 is the sha256 of protoc-$(PROTOC_VERSION)-linux-ppcle_64.zip
PROTOC_VERSION?=34.1
PROTOC_SHA256?=9160f1336e266dd0b389a29c4ffabded6cc9b80ab29f2da423106d6fe75b2ce2
TAG?=v$(LITELLM_VERSION)-$(IMAGE_REVISION)
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

CONTAINER_BUILDER?=podman

# Dev env vars — override on the command line or export before running
INSTRUCT_MODEL_NAME?=my-instruct-model
INSTRUCT_MODEL?=watsonx/ibm/granite-4-h-small
WATSONX_APIKEY?=
WATSONX_PROJECT_ID?=
WATSONX_URL?=https://us-south.ml.cloud.ibm.com
LITELLM_MASTER_KEY?=sk-0987612345
DB_USER?=postgres
DB_PASSWORD?=devpassword
DB_HOST?=localhost
DB_PORT?=5432
DB_NAME?=litellm
DATABASE_URL?=postgresql://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)

run:
	$(CONTAINER_BUILDER) run -d \
		--name litellm-dev \
		--net host \
		-e INSTRUCT_MODEL_NAME="$(INSTRUCT_MODEL_NAME)" \
		-e INSTRUCT_MODEL="$(INSTRUCT_MODEL)" \
		-e WATSONX_APIKEY="$(WATSONX_APIKEY)" \
		-e WATSONX_PROJECT_ID="$(WATSONX_PROJECT_ID)" \
		-e WATSONX_URL="$(WATSONX_URL)" \
		-e LITELLM_MASTER_KEY="$(LITELLM_MASTER_KEY)" \
		-e DATABASE_URL="$(DATABASE_URL)" \
		$(REGISTRY)/$(IMAGE):$(TAG) \
		litellm -c /app/config.yaml --detailed_debug
.PHONY: run

clean:
	-$(CONTAINER_BUILDER) stop litellm-dev
	-$(CONTAINER_BUILDER) rm litellm-dev
.PHONY: clean

build:
	$(CONTAINER_BUILDER) build -t $(REGISTRY)/$(IMAGE):$(TAG) \
		--jobs 2 \
		--build-arg VERSION=$(LITELLM_VERSION) \
		--build-arg LITELLM_SHA256=$(LITELLM_SHA256) \
		--build-arg PYTHON_VERSION=$(PYTHON_VERSION) \
		--build-arg NODE_VERSION=$(NODE_VERSION) \
		--build-arg NODE_SHA256=$(NODE_SHA256) \
		--build-arg RUST_VERSION=$(RUST_VERSION) \
		--build-arg PROTOC_VERSION=$(PROTOC_VERSION) \
		--build-arg PROTOC_SHA256=$(PROTOC_SHA256) \
		.
.PHONY: build

push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(REGISTRY)/$(IMAGE):$(TAG)
.PHONY: push

image-tag:
	@echo $(TAG)
.PHONY: image-tag

update-patch:
	cd ./patches/litellm && ./pyproject-toml.sh $(LITELLM_VERSION)
.PHONY: update-patch

# Fetch the upstream source files for LITELLM_VERSION, apply each patch, and
# show the resulting diff so you can confirm the patches apply cleanly.
verify-patch:
	./patches/litellm/verify-patch.sh $(LITELLM_VERSION)
.PHONY: verify-patch

update-checksums:
	$(eval NEW_LITELLM_SHA256 := $(shell curl -fsSL https://github.com/BerriAI/litellm/archive/refs/tags/v$(LITELLM_VERSION).tar.gz | sha256sum | cut -d' ' -f1))
	$(eval NEW_NODE_SHA256 := $(shell curl -fsSL https://nodejs.org/dist/v$(NODE_VERSION)/node-v$(NODE_VERSION)-linux-ppc64le.tar.gz | sha256sum | cut -d' ' -f1))
	$(eval NEW_PROTOC_SHA256 := $(shell curl -fsSL https://github.com/protocolbuffers/protobuf/releases/download/v$(PROTOC_VERSION)/protoc-$(PROTOC_VERSION)-linux-ppcle_64.zip | sha256sum | cut -d' ' -f1))
	sed -i "s|^LITELLM_SHA256?=.*|LITELLM_SHA256?=$(NEW_LITELLM_SHA256)|" $(MAKEFILE_LIST)
	sed -i "s|^NODE_SHA256?=.*|NODE_SHA256?=$(NEW_NODE_SHA256)|" $(MAKEFILE_LIST)
	sed -i "s|^PROTOC_SHA256?=.*|PROTOC_SHA256?=$(NEW_PROTOC_SHA256)|" $(MAKEFILE_LIST)
	@echo "Updated LITELLM_SHA256 to $(NEW_LITELLM_SHA256)"
	@echo "Updated NODE_SHA256    to $(NEW_NODE_SHA256)"
	@echo "Updated PROTOC_SHA256  to $(NEW_PROTOC_SHA256)"
.PHONY: update-checksums
