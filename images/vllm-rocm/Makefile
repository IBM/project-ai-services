REGISTRY?=icr.io/ai-services-private
IMAGE=vllm-rocm
PYTHON_VERSION?=3.13
VLLM_VERSION?=0.28.0
ROCM_VERSION?=7.14
TRITON_VERSION?=3.6.0
ROCM_GPG_URL?=https://public.dhe.ibm.com/software/server/POWER/Linux/AMD/RPM-GPG-KEY-PAMD
ROCM_REPO_URL?=https://public.dhe.ibm.com/software/server/POWER/Linux/AMD/ROCm/RHEL/10/ppc64le/
IMAGE_REVISION?=0
TAG?=$(VLLM_VERSION)-rocm$(ROCM_VERSION)-$(IMAGE_REVISION)
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

# Image references
IMAGE_TAG := $(REGISTRY)/$(IMAGE):$(TAG)

CONTAINER_BUILDER?=podman
build:
	$(CONTAINER_BUILDER) build \
		--build-arg PYTHON_VERSION=$(PYTHON_VERSION) \
		--build-arg VLLM_VERSION=$(VLLM_VERSION) \
		--build-arg ROCM_VERSION=$(ROCM_VERSION) \
		--build-arg TRITON_VERSION=$(TRITON_VERSION) \
		--build-arg ROCM_GPG_URL=$(ROCM_GPG_URL) \
		--build-arg ROCM_REPO_URL=$(ROCM_REPO_URL) \
		-t $(IMAGE_TAG) .
.PHONY: build

push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(IMAGE_TAG)
ifneq ($(strip $(BRANCH_TAG)),)
	$(CONTAINER_BUILDER) tag $(IMAGE_TAG) $(REGISTRY)/$(IMAGE):$(BRANCH_TAG)
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(REGISTRY)/$(IMAGE):$(BRANCH_TAG)
endif
.PHONY: push

image-tag:
	@echo $(TAG)
.PHONY: image-tag
