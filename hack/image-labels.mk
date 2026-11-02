# Shared make snippet that adds the common OCI labels to every image build.
# Per-image labels (title, description) stay in each Containerfile.
#
# Usage (from a Makefile in a sub-directory):
#   include <relative path>/hack/image-labels.mk
#   build:
#   	$(CONTAINER_BUILDER) build $(IMAGE_LABEL_ARGS) -t <image> .
#
# Optional: set CONTAINERFILE (path relative to the build dir, default
# "Containerfile") when the file is named differently or lives elsewhere.
GIT_REPO ?= https://github.com/ibm/project-ai-services
GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
IMAGE_VENDOR ?= IBM
IMAGE_LICENSES ?= Apache-2.0

# Repo-relative path of the directory make is invoked from (e.g. "images/caddy/")
REPO_PREFIX := $(shell git rev-parse --show-prefix 2>/dev/null)

# $(call image_labels,<containerfile path relative to cwd>)
image_labels = \
	--label org.opencontainers.image.source=$(GIT_REPO) \
	--label org.opencontainers.image.revision=$(GIT_COMMIT) \
	--label org.opencontainers.image.created=$(BUILD_DATE) \
	--label org.opencontainers.image.vendor=$(IMAGE_VENDOR) \
	--label org.opencontainers.image.licenses=$(IMAGE_LICENSES) \
	--label com.github.project-ai-services.containerfile=$(REPO_PREFIX)$(1) \
	--label com.github.project-ai-services.containerfile.url=$(GIT_REPO)/blob/$(GIT_COMMIT)/$(REPO_PREFIX)$(1)

IMAGE_LABEL_ARGS = $(call image_labels,$(or $(CONTAINERFILE),Containerfile))
