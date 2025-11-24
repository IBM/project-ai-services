REGISTRY?=icr.io/ai-services-private
IMAGE=etcd
ETCD_VERSION?=3.6.5
TAG?=${ETCD_VERSION}-2
CREDS_ARG := $(if $(and $(REGISTRY_USER),$(REGISTRY_PASSWORD)),--creds="$(REGISTRY_USER):$(REGISTRY_PASSWORD)")

CONTAINER_BUILDER?=podman
build:
	$(CONTAINER_BUILDER) build --pull=always --build-arg ETCD_VERSION=${ETCD_VERSION} -t $(REGISTRY)/$(IMAGE):$(TAG) .
.PHONY: build

push:
	$(CONTAINER_BUILDER) push $(CREDS_ARG) $(REGISTRY)/$(IMAGE):$(TAG)

.PHONY: push
