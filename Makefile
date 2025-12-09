.PHONY: latest-image-tag
latest-image-tag:
	@podman search --list-tags icr.io/${NAMESPACE}/${IMAGE} | awk 'NR>1 {print $$2}' | sort -V | tail -n 1
