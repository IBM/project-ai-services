# Shared make snippet that supplies the build args consumed by the OCI image
# labels declared at the end of every Containerfile (source, revision, created,
# and the exact Containerfile location).
#
# Usage (from a Makefile in a sub-directory):
#   include <relative path>/hack/image-labels.mk
#   build:
#   	$(CONTAINER_BUILDER) build $(IMAGE_LABEL_ARGS) -t <image> .
GIT_REPO ?= https://github.com/ibm/project-ai-services
GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

IMAGE_LABEL_ARGS = \
	--build-arg GIT_REPO=$(GIT_REPO) \
	--build-arg GIT_COMMIT=$(GIT_COMMIT) \
	--build-arg BUILD_DATE=$(BUILD_DATE)
