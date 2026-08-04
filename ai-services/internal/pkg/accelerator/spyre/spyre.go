package spyre

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// ListCards lists all Spyre cards attached to the system.
func ListCards(ctx context.Context) ([]string, error) {
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
		logger.DebuglnCtx(ctx, "Spyre card detected")
		devID := strings.Split(pciDev, " ")[0]
		logger.DebugfCtx(ctx, "PCI id: %s\n", devID)
		spyreDeviceIDsList = append(spyreDeviceIDsList, devID)
	}

	logger.DebuglnCtx(ctx, "List of discovered Spyre cards: "+strings.Join(spyreDeviceIDsList, ", "))

	return spyreDeviceIDsList, nil
}

// FindFreeCards finds available (free) Spyre cards.
func FindFreeCards(ctx context.Context) ([]string, error) {
	freeSpyreDevIDList := []string{}
	devFiles, err := os.ReadDir("/dev/vfio")
	if err != nil {
		return freeSpyreDevIDList, fmt.Errorf("failed to check device files under /dev/vfio: %w", err)
	}

	for _, devFile := range devFiles {
		if devFile.Name() == "vfio" {
			continue
		}
		f, err := os.Open("/dev/vfio/" + devFile.Name())
		if err != nil {
			logger.DebugfCtx(ctx, "Device or resource busy, skipping.., err: %v", err)

			continue
		}
		if err := f.Close(); err != nil {
			logger.DebuglnCtx(ctx, "Failed to close the device file handle")
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
