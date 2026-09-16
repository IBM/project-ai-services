package helm

import (
	"context"
	"errors"
	"fmt"
	"time"

	helmchart "helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/storage/driver"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// InstallOrUpgrade installs or upgrades a Helm release from a loaded chart,
// optionally injecting templateID as a --set override.
// Called by both LocalHelmManager and the worker dispatcher.
func InstallOrUpgrade(ctx context.Context, release, namespace string, chart helmchart.Charter, values map[string]any, templateID string, timeout time.Duration) error {
	h, err := NewHelm(namespace)
	if err != nil {
		return fmt.Errorf("helm: create client for namespace %q: %w", namespace, err)
	}

	overrides := make(map[string]any, len(values)+1)
	for k, v := range values {
		overrides[k] = v
	}
	if templateID != "" {
		overrides["templateID"] = templateID
	}

	return h.InstallOrUpgrade(ctx, release, chart, overrides, timeout)
}

// GetReleaseManifest returns the raw multi-document YAML manifest for release
// in namespace as stored by Helm. Called by the worker dispatcher.
func GetReleaseManifest(namespace, release string) (string, error) {
	h, err := NewHelm(namespace)
	if err != nil {
		return "", fmt.Errorf("helm: create client for namespace %q: %w", namespace, err)
	}

	return h.getReleaseManifest(release)
}

// UninstallRelease removes a Helm release from the given namespace.
// If the release does not exist the call is a no-op (returns nil).
// Called by both LocalHelmManager and the worker dispatcher.
func UninstallRelease(ctx context.Context, release, namespace string) error {
	h, err := NewHelm(namespace)
	if err != nil {
		return fmt.Errorf("helm: create client for namespace %q: %w", namespace, err)
	}

	if err := h.Uninstall(release, nil); err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			logger.InfofCtx(ctx, "helm: release %q not found in namespace %q — skipping uninstall\n", release, namespace)

			return nil
		}

		return err
	}

	return nil
}
