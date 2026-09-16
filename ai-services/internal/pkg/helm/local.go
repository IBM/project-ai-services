package helm

import (
	"context"
	"time"

	helmchart "helm.sh/helm/v4/pkg/chart"
)

// HelmManager defines the interface for Helm install/uninstall operations.
// The namespace is bound at construction time via NewLocalHelmManager or
// NewRemoteHelmManager so callers do not repeat it on every call.
// LocalHelmManager runs against the local kubeconfig; RemoteHelmManager
// forwards commands over the gRPC CommandStream to a remote OpenShift worker.
type HelmManager interface {
	// InstallOrUpgrade installs or upgrades a Helm release from a loaded chart.
	InstallOrUpgrade(ctx context.Context, release string, chart helmchart.Charter, values map[string]any, templateID string, timeout time.Duration) error
	// Uninstall removes a Helm release. No-op if the release does not exist.
	Uninstall(ctx context.Context, release string) error
	// GetReleaseManifest returns the raw multi-document YAML manifest for the
	// named release as stored by Helm in the target namespace.
	GetReleaseManifest(ctx context.Context, release string) (string, error)
}

// LocalHelmManager runs Helm operations directly against the local kubeconfig.
type LocalHelmManager struct {
	namespace string
}

// NewLocalHelmManager returns a HelmManager for a local OpenShift cluster
// scoped to the given namespace.
func NewLocalHelmManager(namespace string) HelmManager {
	return &LocalHelmManager{namespace: namespace}
}

func (m *LocalHelmManager) InstallOrUpgrade(ctx context.Context, release string, chart helmchart.Charter, values map[string]any, templateID string, timeout time.Duration) error {
	return InstallOrUpgrade(ctx, release, m.namespace, chart, values, templateID, timeout)
}

func (m *LocalHelmManager) Uninstall(ctx context.Context, release string) error {
	return UninstallRelease(ctx, release, m.namespace)
}

func (m *LocalHelmManager) GetReleaseManifest(_ context.Context, release string) (string, error) {
	return GetReleaseManifest(m.namespace, release)
}
