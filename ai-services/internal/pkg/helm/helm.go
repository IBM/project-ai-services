package helm

import (
	"errors"
	"fmt"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/cli"
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

func (h *Helm) Install(release string, chart chart.Charter, values map[string]interface{}) error {
	// Configure the Installer client
	installClient := action.NewInstall(h.actionConfig)
	installClient.ReleaseName = release
	installClient.Namespace = h.namespace
	installClient.CreateNamespace = true
	//nolint:godox
	// TODO: Replace the WaitStrategy to watcher and also add timeout
	installClient.WaitStrategy = "hookOnly"

	// Perform helm install
	_, err := installClient.Run(chart, values)
	if err != nil {
		return fmt.Errorf("Install failed: %w", err)
	}

	return nil
}

func (h *Helm) Upgrade(release string, chart chart.Charter, values map[string]interface{}) error {
	// Configure the Upgrade client
	upgradeClient := action.NewUpgrade(h.actionConfig)
	upgradeClient.Namespace = h.namespace
	upgradeClient.ServerSideApply = "true"
	//nolint:godox
	// TODO: Replace the WaitStrategy to watcher and also add timeout
	upgradeClient.WaitStrategy = "hookOnly"

	// Perform helm upgrade
	_, err := upgradeClient.Run(release, chart, values)
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
