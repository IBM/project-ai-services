package helpers

import (
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

func FetchApplicationTemplatesNames() ([]string, error) {
	apps := []string{}

	err := fs.WalkDir(assets.ApplicationFS, "applications", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		// Templates Pattern :- "assets/applications/<AppName>/*.yaml.tmpl"
		parts := strings.Split(path, "/")

		if len(parts) >= 3 {
			appName := parts[1]
			if slices.Contains(apps, appName) {
				return nil
			}
			apps = append(apps, appName)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return apps, nil
}

// LoadAllTemplates -> Loads all templates under a specified root path
func LoadAllTemplates(rootPath string) (map[string]*template.Template, error) {
	tmpls := make(map[string]*template.Template)

	err := fs.WalkDir(assets.ApplicationFS, rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".tmpl") {
			return nil
		}

		t, err := template.ParseFS(assets.ApplicationFS, path)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		tmpls[path] = t
		return nil
	})
	return tmpls, err
}

type HealthStatus string

const (
	Ready    HealthStatus = "healthy"
	Starting HealthStatus = "starting"
	NotReady HealthStatus = "unhealthy"
)

func WaitForContainerReadiness(runtime runtime.Runtime, containerNameOrId string, timeout time.Duration) error {
	var containerStatus *define.InspectContainerData
	var err error

	deadline := time.Now().Add(timeout)

	for {
		// fetch the container status
		containerStatus, err = runtime.InspectContainer(containerNameOrId)
		if err != nil {
			return fmt.Errorf("failed to check container status: %w", err)
		}

		healthStatus := containerStatus.State.Health

		if healthStatus == nil {
			return nil
		}

		if healthStatus.Status == string(Ready) {
			return nil
		}

		// if deadline exeeds, stop the readiness check
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for readiness")
		}

		// every 2 seconds inspect the container
		time.Sleep(2 * time.Second)
	}
}

func FetchContainerStartPeriod(runtime runtime.Runtime, containerNameOrId string) (time.Duration, error) {
	// fetch the container stats
	containerStats, err := runtime.InspectContainer(containerNameOrId)
	if err != nil {
		return 0, fmt.Errorf("failed to check container stats: %w", err)
	}

	// Healthcheck settings live under Config.Healthcheck
	if containerStats.Config == nil || containerStats.Config.Healthcheck == nil {
		return -1, nil
	}

	healthCheck := containerStats.Config.Healthcheck

	return healthCheck.StartPeriod, nil
}
