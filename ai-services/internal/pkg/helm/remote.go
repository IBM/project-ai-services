package helm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	helmchart "helm.sh/helm/v4/pkg/chart"

	"github.com/project-ai-services/ai-services/internal/pkg/worker/payload"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// RemoteHelmManager implements HelmManager by sending Helm commands over the
// gRPC CommandStream to a remote OpenShift worker. Use it when the runtime is
// a RemoteRuntime so Helm operations flow through the same gRPC channel as all
// other runtime calls.
type RemoteHelmManager struct {
	sender    *stream.Sender
	namespace string
}

// NewRemoteHelmManager returns a RemoteHelmManager that reuses the given
// Sender (and therefore the same worker connection) as the RemoteRuntime,
// scoped to the given namespace.
func NewRemoteHelmManager(sender *stream.Sender, namespace string) HelmManager {
	return &RemoteHelmManager{sender: sender, namespace: namespace}
}

// InstallOrUpgrade implements HelmManager. It extracts the raw files from the
// loaded chart, converts them to payload.ChartFile for JSON serialisation, and
// sends COMMAND_TYPE_HELM_INSTALL to the worker over the gRPC stream.
func (m *RemoteHelmManager) InstallOrUpgrade(ctx context.Context, release string, chart helmchart.Charter, values map[string]any, templateID string, timeout time.Duration) error {
	buffered, err := MarshalChart(chart)
	if err != nil {
		return fmt.Errorf("helm: prepare chart for remote install: %w", err)
	}

	chartFiles := make([]payload.ChartFile, len(buffered))
	for i, f := range buffered {
		chartFiles[i] = payload.ChartFile{Name: f.Name, Data: f.Data}
	}

	_, err = m.sender.Send(ctx, workerpb.CommandType_COMMAND_TYPE_HELM_INSTALL, payload.HelmInstall{
		Release:    release,
		Namespace:  m.namespace,
		ChartFiles: chartFiles,
		Values:     values,
		TemplateID: templateID,
		TimeoutSec: int64(timeout.Seconds()),
	})

	return err
}

// Uninstall implements HelmManager. It sends COMMAND_TYPE_HELM_UNINSTALL to
// the worker over the gRPC stream.
func (m *RemoteHelmManager) Uninstall(ctx context.Context, release string) error {
	_, err := m.sender.Send(ctx, workerpb.CommandType_COMMAND_TYPE_HELM_UNINSTALL, payload.HelmRelease{
		Release:   release,
		Namespace: m.namespace,
	})

	return err
}

// GetReleaseManifest implements HelmManager. It sends COMMAND_TYPE_HELM_GET_MANIFEST
// to the worker and decodes the returned manifest string.
func (m *RemoteHelmManager) GetReleaseManifest(ctx context.Context, release string) (string, error) {
	result, err := m.sender.Send(ctx, workerpb.CommandType_COMMAND_TYPE_HELM_GET_MANIFEST, payload.HelmRelease{
		Release:   release,
		Namespace: m.namespace,
	})
	if err != nil {
		return "", err
	}

	var resp payload.HelmManifest
	if err := json.Unmarshal(result.GetData(), &resp); err != nil {
		return "", fmt.Errorf("helm get manifest: decode response: %w", err)
	}

	return resp.Manifest, nil
}
