package helm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/kube"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage/driver"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
)

type Helm struct {
	namespace    string
	actionConfig *action.Configuration
}

func NewHelm(namespace string) (*Helm, error) {
	settings := cli.New()
	settings.SetNamespace(namespace)

	actionConfig := new(action.Configuration)

	baseLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	actionConfig.SetLogger(baseLogger.Handler())

	if err := actionConfig.Init(
		settings.RESTClientGetter(),
		namespace,
		"",
	); err != nil {
		return nil, fmt.Errorf("failed to initialize Helm action config: %w", err)
	}

	return &Helm{
		namespace:    namespace,
		actionConfig: actionConfig,
	}, nil
}

type InstallOpts struct {
	Values  map[string]any
	Timeout time.Duration
}

func (h *Helm) install(ctx context.Context, release string, chart chart.Charter, opts *InstallOpts) error {
	// Configure the Installer client
	installClient := action.NewInstall(h.actionConfig)
	installClient.ReleaseName = release
	installClient.Namespace = h.namespace
	installClient.CreateNamespace = true
	installClient.WaitStrategy = kube.StatusWatcherStrategy
	installClient.Timeout = opts.Timeout
	installClient.SkipSchemaValidation = true

	// Perform helm install
	_, err := installClient.RunWithContext(ctx, chart, opts.Values)
	if err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	return nil
}

type UpgradeOpts struct {
	Values  map[string]any
	Timeout time.Duration
}

func (h *Helm) upgrade(ctx context.Context, release string, chart chart.Charter, opts *UpgradeOpts) error {
	// Configure the Upgrade client
	upgradeClient := action.NewUpgrade(h.actionConfig)
	upgradeClient.Namespace = h.namespace
	upgradeClient.ServerSideApply = "true"
	upgradeClient.WaitStrategy = kube.StatusWatcherStrategy
	upgradeClient.Timeout = opts.Timeout
	upgradeClient.ForceConflicts = true
	upgradeClient.RollbackOnFailure = true
	upgradeClient.SkipSchemaValidation = true

	// Perform helm upgrade
	_, err := upgradeClient.RunWithContext(ctx, release, chart, opts.Values)
	if err != nil {
		return fmt.Errorf("upgrade failed: %w", err)
	}

	return nil
}

// InstallOrUpgrade installs a release if it does not exist, or upgrades it if it does.
func (h *Helm) InstallOrUpgrade(ctx context.Context, release string, chart chart.Charter, values map[string]any, timeout time.Duration) error {
	exists, err := h.IsReleaseExist(release)
	if err != nil {
		return fmt.Errorf("failed to check release existence: %w", err)
	}

	if !exists {
		return h.install(ctx, release, chart, &InstallOpts{Values: values, Timeout: timeout})
	}

	return h.upgrade(ctx, release, chart, &UpgradeOpts{Values: values, Timeout: timeout})
}

func (h *Helm) IsReleaseExist(release string) (bool, error) {
	client := action.NewGet(h.actionConfig)

	client.Version = 0 // to fetch the latest revision for given release

	// Run the action
	_, err := client.Run(release)
	if err != nil {
		// v4 check for 'not found' specifically
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

func (h *Helm) getReleaseManifest(release string) (string, error) {
	client := action.NewGet(h.actionConfig)
	client.Version = 0

	rel, err := client.Run(release)
	if err != nil {
		return "", fmt.Errorf("failed to get release %s: %w", release, err)
	}

	releaseData, ok := rel.(*releasev1.Release)
	if !ok || releaseData == nil {
		return "", fmt.Errorf("unexpected release type %T for %s", rel, release)
	}

	return releaseData.Manifest, nil
}

type UninstallOpts struct {
	Timeout time.Duration
}

func (h *Helm) Uninstall(release string, opts *UninstallOpts) error {
	exists, err := h.IsReleaseExist(release)
	if err != nil {
		return fmt.Errorf("failed to check '%s' release existence: %w", release, err)
	}

	if !exists {
		return driver.ErrReleaseNotFound
	}

	// Configure the Uninstall client
	uninstallClient := action.NewUninstall(h.actionConfig)
	uninstallClient.WaitStrategy = kube.StatusWatcherStrategy

	timeout := constants.HelmUninstallTimeout
	if opts != nil && opts.Timeout > 0 {
		timeout = opts.Timeout
	}
	uninstallClient.Timeout = timeout

	// Perform helm uninstall
	_, err = uninstallClient.Run(release)
	if err != nil {
		return fmt.Errorf("Uninstall failed: %w", err)
	}

	return nil
}
