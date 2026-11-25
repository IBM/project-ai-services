package amdgpu

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// rocmSMICandidates lists absolute paths where rocm-smi may be installed,
// checked in order before falling back to a PATH lookup.
// sudo strips non-standard directories (e.g. /opt/rocm/bin) from PATH, so
// probing absolute paths first ensures the binary is found regardless.
var rocmSMICandidates = []string{
	"/usr/bin/rocm-smi",
	"/opt/rocm/bin/rocm-smi",
	"/usr/local/bin/rocm-smi",
}

// resolveROCmSMI returns the path to the rocm-smi binary by checking
// rocmSMICandidates in order, then falling back to exec.LookPath.
func resolveROCmSMI() string {
	for _, candidate := range rocmSMICandidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if path, err := exec.LookPath("rocm-smi"); err == nil {
		return path
	}

	return "rocm-smi"
}

// ROCmSMICmd returns an exec.Cmd for rocm-smi with the given arguments.
func ROCmSMICmd(args ...string) *exec.Cmd {
	return exec.Command(resolveROCmSMI(), args...)
}

// IsApplicable returns true when AMD GPU devices are present on the host.
// It checks for /dev/kfd which is created by the amdgpu kernel module and
// is required for any ROCm workload.
func IsApplicable() bool {
	_, err := os.Stat("/dev/kfd")

	return err == nil
}

// idleVRAMThresholdBytes is the VRAM usage at or below which a GPU is treated
// as idle. An unused GPU still reports a small non-zero baseline (the driver
// reserves a few MiB), so an exact == 0 check never matches.
const idleVRAMThresholdBytes = 256 * 1024 * 1024

// amdVendorID is the PCI vendor ID (as printed by sysfs) of AMD/ATI devices.
const amdVendorID = "0x1002"

// amdCard is an AMD GPU found under /sys/class/drm.
type amdCard struct {
	name    string // e.g. card0
	path    string // e.g. /sys/class/drm/card0
	pciAddr string // e.g. 0038:0f:00.0
}

// FindFreeDevices returns the ROCm device ordinals (as strings, e.g. "0", "1") of
// AMD GPUs with no workload using VRAM. The values are suitable for use directly
// in ROCR_VISIBLE_DEVICES, which does not accept PCI addresses — only a device
// ordinal or GPU-<hex UUID>.
//
// It reads /sys/class/drm/card<N>/device/{vendor,mem_info_vram_used} via sysfs so
// it works inside the worker container (no rocm-smi binary required) as long as
// /sys/class/drm is mounted from the host.
//
// Ordinals are assigned by sorting all AMD GPUs by PCI address, matching the order
// in which the KFD enumerates them. They are computed over every AMD GPU on the
// host, not just the free ones, because ROCm numbers devices globally.
// A GPU is considered free when mem_info_vram_used <= idleVRAMThresholdBytes.
func FindFreeDevices(ctx context.Context) ([]string, error) {
	cards, err := discoverCards(ctx)
	if err != nil {
		return nil, err
	}

	var free []string

	for ordinal, card := range cards {
		// Read VRAM used through the card's device symlink and let the kernel
		// resolve it. The link target is relative to the real directory under
		// /sys/devices (not to /sys/class/drm/card<N>, itself a symlink), so
		// resolving it lexically would produce a non-existent path.
		vramUsed, err := readVRAMUsed(card)
		if err != nil {
			logger.DebugfCtx(ctx, "AMD GPU %s: skipping: %v\n", card.name, err)

			continue
		}

		if vramUsed <= idleVRAMThresholdBytes {
			logger.DebugfCtx(ctx, "AMD GPU %s (%s, ordinal %d) is free (mem_info_vram_used = %d)\n", card.name, card.pciAddr, ordinal, vramUsed)
			free = append(free, strconv.Itoa(ordinal))
		} else {
			logger.DebugfCtx(ctx, "AMD GPU %s (%s, ordinal %d) is occupied (mem_info_vram_used = %d)\n", card.name, card.pciAddr, ordinal, vramUsed)
		}
	}

	logger.DebugfCtx(ctx, "AMD GPU free device ordinals: %s\n", strings.Join(free, ", "))

	return free, nil
}

// discoverCards returns every AMD GPU under /sys/class/drm sorted by PCI address,
// so a card's index in the result is its ROCm device ordinal.
func discoverCards(ctx context.Context) ([]amdCard, error) {
	const drmBase = "/sys/class/drm"

	entries, err := os.ReadDir(drmBase)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", drmBase, err)
	}

	var cards []amdCard

	for _, entry := range entries {
		name := entry.Name()
		// Only process card<N> entries, not renderD<N> or other symlinks.
		if !strings.HasPrefix(name, "card") || strings.Contains(name, "-") {
			continue
		}

		cardPath := drmBase + "/" + name

		// Skip non-AMD display devices (e.g. a BMC VGA) so they do not shift ordinals.
		vendor, err := os.ReadFile(cardPath + "/device/vendor")
		if err != nil || strings.TrimSpace(string(vendor)) != amdVendorID {
			logger.DebugfCtx(ctx, "DRM %s: skipping, not an AMD device (vendor read err: %v)\n", name, err)

			continue
		}

		// Read the device symlink value — this is a pure string read, the target
		// does not need to exist inside the container.
		// e.g. /sys/class/drm/card0/device → ../../../0038:0f:00.0
		deviceLink, err := os.Readlink(cardPath + "/device")
		if err != nil {
			logger.DebugfCtx(ctx, "AMD GPU %s: skipping, cannot read device symlink: %v\n", name, err)

			continue
		}

		cards = append(cards, amdCard{name: name, path: cardPath, pciAddr: filepath.Base(deviceLink)})
	}

	sort.Slice(cards, func(i, j int) bool { return cards[i].pciAddr < cards[j].pciAddr })

	return cards, nil
}

// readVRAMUsed returns the card's current VRAM usage in bytes.
func readVRAMUsed(card amdCard) (int64, error) {
	raw, err := os.ReadFile(card.path + "/device/mem_info_vram_used")
	if err != nil {
		return 0, fmt.Errorf("cannot read mem_info_vram_used: %w", err)
	}

	vramUsed, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse mem_info_vram_used %q: %w", string(raw), err)
	}

	return vramUsed, nil
}

// ListDevices returns the list of AMD GPU PCI addresses found via lspci.
// AMD GPU PCI vendor ID is 1002. Only display/compute class devices are counted
// (PCI class 0300 VGA Compatible Controller, 0302 3D Controller, 0380 Display
// Controller) — this excludes AMD audio devices (class 0403, printed as
// "Audio device") that share the same vendor ID and would otherwise inflate
// the GPU count.
//
// lspci output format (one device per line):
//
//	<addr> <Class>: <Vendor> <Description>
//	e.g. "0b:00.0 Display controller: Advanced Micro Devices..."
func ListDevices(ctx context.Context) ([]string, error) {
	cmd := exec.Command("lspci", "-d", "1002:")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list AMD GPU PCI devices: %w, output: %s", err, string(out))
	}

	// gpuClasses are the substrings lspci uses in the class field of its
	// single-line output for GPU compute/display devices.
	gpuClasses := []string{
		"VGA compatible controller",
		"3D controller",
		"Display controller",
	}

	var devices []string

	for _, line := range strings.Split(string(out), "\n") {
		// Each line is "<addr> <class>: …". Extract the class portion between
		// the first space and the colon.
		pciAddr, classAndDesc, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}

		lineClass, _, ok := strings.Cut(classAndDesc, ":")
		if !ok {
			continue
		}

		if isGPUClass(lineClass, gpuClasses) {
			logger.DebugfCtx(ctx, "AMD GPU detected, PCI address: %s\n", pciAddr)
			devices = append(devices, pciAddr)
		}
	}

	sort.Strings(devices)

	logger.DebuglnCtx(ctx, "AMD GPU devices found: "+strings.Join(devices, ", "))

	return devices, nil
}

// isGPUClass reports whether class matches one of gpuClasses, ignoring case.
func isGPUClass(class string, gpuClasses []string) bool {
	for _, gpuClass := range gpuClasses {
		if strings.EqualFold(class, gpuClass) {
			return true
		}
	}

	return false
}
