package spyre

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// ListCards lists all Spyre cards attached to the system.
func ListCards() ([]string, error) {
	spyreDeviceIDsList := []string{}
	cmd := exec.Command("lspci", "-d", "1014:06a7")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return spyreDeviceIDsList, fmt.Errorf("failed to get PCI devices attached to lpar: %v, output: %s", err, string(out))
	}

	pciDevicesStr := string(out)

	for _, pciDev := range strings.Split(pciDevicesStr, "\n") {
		if pciDev == "" {
			continue
		}
		logger.Infoln("Spyre card detected", logger.VerbosityLevelDebug)
		devID := strings.Split(pciDev, " ")[0]
		logger.Infof("PCI id: %s\n", devID, logger.VerbosityLevelDebug)
		spyreDeviceIDsList = append(spyreDeviceIDsList, devID)
	}

	logger.Infoln("List of discovered Spyre cards: "+strings.Join(spyreDeviceIDsList, ", "), logger.VerbosityLevelDebug)

	return spyreDeviceIDsList, nil
}

// FindFreeCards finds available (free) Spyre cards.
func FindFreeCards() ([]string, error) {
	freeSpyreDevIDList := []string{}
	devFiles, err := os.ReadDir("/dev/vfio")
	if err != nil {
		log.Fatalf("failed to check device files under /dev/vfio. Error: %v", err)

		return freeSpyreDevIDList, err
	}

	for _, devFile := range devFiles {
		if devFile.Name() == "vfio" {
			continue
		}
		f, err := os.Open("/dev/vfio/" + devFile.Name())
		if err != nil {
			logger.Infof("Device or resource busy, skipping.., err: %v", err, logger.VerbosityLevelDebug)

			continue
		}
		if err := f.Close(); err != nil {
			logger.Infoln("Failed to close the device file handle", logger.VerbosityLevelDebug)
		}

		// free card available to use
		devPCIPath := fmt.Sprintf("/sys/kernel/iommu_groups/%s/devices", devFile.Name())
		cmd := exec.Command("ls", devPCIPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return freeSpyreDevIDList, fmt.Errorf("failed to get pci address for the free spyre device: %v, output: %s", err, string(out))
		}
		pci := string(out)
		freeSpyreDevIDList = append(freeSpyreDevIDList, pci)
	}

	return freeSpyreDevIDList, nil
}

// Made with Bob
