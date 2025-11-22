package helpers

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/domain/entities/types"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

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
		if pci_dev == "" {
			continue
		}
		logger.Infoln("Spyre card detected", 1)
		dev_id := strings.Split(pci_dev, " ")[0]
		logger.Infof("PCI id: %s\n", dev_id, 1)
		spyre_device_ids_list = append(spyre_device_ids_list, dev_id)
	}

	logger.Infoln("List of discovered Spyre cards: "+strings.Join(spyre_device_ids_list, ", "), 1)
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
			logger.Infoln("Device or resource busy, skipping..", 1)
			continue
		}
		if err := f.Close(); err != nil {
			logger.Infoln("Failed to close the device file handle", 1)
		}

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

// CheckExistingPodsForApplication checks if there are pods already existing for the given application name
func CheckExistingPodsForApplication(runtime runtime.Runtime, appName string) ([]string, error) {
	// var podsExists bool
	var podsToSkip []string
	resp, err := runtime.ListPods(map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", appName)},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	var pods []*types.ListPodsReport
	if val, ok := resp.([]*types.ListPodsReport); ok {
		pods = val
	}

	if len(pods) == 0 {
		logger.Infof("No existing pods found for application: %s\n", appName)
		return nil, nil
	}

	logger.Infoln("Checking status of existing pods...")
	for _, pod := range pods {
		logger.Infof("Existing pod found: %s with status: %s\n", pod.Name, pod.Status)
		podsToSkip = append(podsToSkip, pod.Name)
	}
	return podsToSkip, nil
}
