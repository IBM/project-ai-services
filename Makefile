# Default registry for image updates
REGISTRY?=icr.io/ai-services-cicd

# Update image tags in values.yaml files
# Usage:
#   make update-tags                    # Auto-detect modified Makefiles
#   make update-tags SYNC=true          # Sync ALL Makefiles tags with values.yaml files
#   make update-tags REGISTRY=custom    # Use custom registry
#   make update-tags SYNC=true REGISTRY=custom  # Sync with custom registry
.PHONY: update-tags
update-tags:
	@echo "Updating image tags in values.yaml files..."
	@if [ "$(SYNC)" = "true" ]; then \
		python3 hack/update_image_tags.py --sync --registry $(REGISTRY); \
	else \
		python3 hack/update_image_tags.py --registry $(REGISTRY); \
	fi

# Help target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  update-tags           - Update image tags in values.yaml files"
	@echo ""
	@echo "Variables:"
	@echo "  REGISTRY=<url>        - Container registry URL (default: icr.io/ai-services-cicd)"
	@echo "  SYNC=true             - Sync ALL Makefiles instead of auto-detecting"
	@echo ""
	@echo "Examples:"
	@echo "  make update-tags                           # Auto-detect modified Makefiles"
	@echo "  make update-tags SYNC=true                 # Sync ALL Makefiles tags with values.yaml files"
	@echo "  make update-tags REGISTRY=custom-registry  # Use custom registry"
	@echo "  make update-tags SYNC=true REGISTRY=icr.io/custom  # Sync with custom registry"

# Made with Bob