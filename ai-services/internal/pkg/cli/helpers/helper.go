package helpers

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	v1 "github.com/containers/podman/v5/pkg/k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
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

		// Templates Pattern :- "assets/applications/<AppName>/templates/*.yaml.tmpl"
		parts := strings.Split(path, "/")

		if len(parts) >= 4 {
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

		// key should be just the template file name (Eg:- pod1.yaml.tmpl)
		tmpls[strings.TrimPrefix(path, fmt.Sprintf("%s/", rootPath))] = t
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

func ListSpyreCards() ([]string, error) {
	spyre_device_ids_list := []string{}
	cmd := exec.Command("lspci", "-d", "1014:06a7")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return spyre_device_ids_list, fmt.Errorf("failed to get PCI devices attached to lpar: %v, output: %s", err, string(out))
	}

	pci_devices_str := string(out)

	for _, pci_dev := range strings.Split(pci_devices_str, "\n") {
		logger.Logger.Debug("Spyre card detected")
		dev_id := strings.Split(pci_dev, " ")[0]
		logger.Logger.Sugar().Debug("PCI id: %s", dev_id)
		spyre_device_ids_list = append(spyre_device_ids_list, dev_id)
	}

	logger.Logger.Sugar().Info("List of discovered Spyre cards: ", spyre_device_ids_list)
	return spyre_device_ids_list, nil
}

func FindFreeSpyreCards() ([]string, error) {
	free_spyre_dev_id_list := []string{}
	dev_files, err := os.ReadDir("/dev/vfio")
	if err != nil {
		log.Fatalf("failed to check device files under /dev/vfio. Error: %v", err)
		return free_spyre_dev_id_list, err
	}

	for _, dev_file := range dev_files {
		if dev_file.Name() == "vfio" {
			continue
		}
		f, err := os.Open("/dev/vfio/" + dev_file.Name())
		if err != nil {
			logger.Logger.Debug("Device or resource busy, skipping..")
			continue
		}
		f.Close()

		// free card available to use
		dev_pci_path := fmt.Sprintf("/sys/kernel/iommu_groups/%s/devices", dev_file.Name())
		cmd := exec.Command("ls", dev_pci_path)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return free_spyre_dev_id_list, fmt.Errorf("failed to get pci address for the free spyre device: %v, output: %s", err, string(out))
		}
		pci := string(out)
		free_spyre_dev_id_list = append(free_spyre_dev_id_list, pci)
	}
	return free_spyre_dev_id_list, nil
}

type PodSpec struct {
	v1.Pod
}

func sanitizeTemplateForYaml(input []byte) []byte {
	re := regexp.MustCompile(`{{.*?}}`)
	return re.ReplaceAll(input, []byte("# template removed"))
}

func LoadPodTemplate(path string) (*PodSpec, error) {
	data, err := assets.ApplicationFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	// comment out template literals
	// Note: This will sanitize by removing the template literals
	// If we want to use with templating values, then apply templating and then read the file
	data = sanitizeTemplateForYaml(data)

	var podSpec PodSpec
	if err := yaml.Unmarshal(data, &podSpec); err != nil {
		return nil, err
	}
	return &podSpec, nil
}

func FetchPodAnnotations(podspec PodSpec) map[string]string {
	return podspec.Annotations
}

func FetchContainerNames(podspec PodSpec) []string {
	var containerNames []string
	for _, v1Container := range podspec.Spec.Containers {
		containerNames = append(containerNames, v1Container.Name)
	}
	return containerNames
}

func ParseSkipChecks(skipChecks []string) map[string]bool {
	skipMap := make(map[string]bool)
	for _, check := range skipChecks {
		parts := strings.Split(check, ",")
		for _, part := range parts {
			trimmed := strings.TrimSpace(strings.ToLower(part))
			if trimmed != "" {
				skipMap[trimmed] = true
			}
		}
	}
	return skipMap
}
