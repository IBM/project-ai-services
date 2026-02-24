package helm

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/storage/driver"
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

func (h *Helm) Install(release string, chart chart.Charter, opts *InstallOpts) error {
	// Configure the Installer client
	installClient := action.NewInstall(h.actionConfig)
	installClient.ReleaseName = release
	installClient.Namespace = h.namespace
	installClient.CreateNamespace = true
	installClient.WaitStrategy = kube.StatusWatcherStrategy
	installClient.Timeout = opts.Timeout

	// Perform helm install
	_, err := installClient.Run(chart, opts.Values)
	if err != nil {
		return fmt.Errorf("Install failed: %w", err)
	}

	return nil
}

type UpgradeOpts struct {
	Values  map[string]any
	Timeout time.Duration
}

func (h *Helm) Upgrade(release string, chart chart.Charter, opts *UpgradeOpts) error {
	// Configure the Upgrade client
	upgradeClient := action.NewUpgrade(h.actionConfig)
	upgradeClient.Namespace = h.namespace
	upgradeClient.ServerSideApply = "true"
	upgradeClient.WaitStrategy = kube.StatusWatcherStrategy
	upgradeClient.Timeout = opts.Timeout

	// Perform helm upgrade
	_, err := upgradeClient.Run(release, chart, opts.Values)
	if err != nil {
		return fmt.Errorf("Upgrade failed: %w", err)
	}

	return nil
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
