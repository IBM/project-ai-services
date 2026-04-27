package spyre

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jaypipes/ghw"
	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/check"
	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/utils"
)

const (
	spyreVendorID     = "1014"
	spyreDeviceIDRev1 = "06a7"
	spyreDeviceIDRev2 = "06a8"
	sentientGroup     = "sentient"
	vfioConfigFile    = "/etc/modprobe.d/vfio-pci.conf"
	memLimitPerCard   = 134234112 // 128MB in bytes - memory lock limit per Spyre card
)

// Package-level regex patterns compiled once for performance.
var (
	vfioOptionsPattern = regexp.MustCompile(`^options\s+(\S+)\s+(.+)$`)
	vfioConfigPattern  = regexp.MustCompile(`(\w+)=([^=\s]+)`)
	memLimitPattern    = regexp.MustCompile(`^(@sentient.+)\s+(unlimited|\d+)$`)
)

// GetNumberOfSpyreCards returns the number of Spyre cards in the system.
func GetNumberOfSpyreCards() int {
	devices, err := GetSpyreDevices()
	if err != nil {
		log.Printf("Error getting Spyre devices: %v", err)

		return 0
	}

	return len(devices)
}

// GetSpyreDevices returns all Spyre PCI devices.
func GetSpyreDevices() ([]*ghw.PCIDevice, error) {
	pci, err := utils.GetPCIInfo()
	if err != nil {
		return nil, fmt.Errorf("error getting PCI info: %v", err)
	}

	spyreDevices := make([]*ghw.PCIDevice, 0, len(pci.Devices))
	for _, device := range pci.Devices {
		if device.Vendor.ID == spyreVendorID &&
			(device.Product.ID == spyreDeviceIDRev1 || device.Product.ID == spyreDeviceIDRev2) {
			spyreDevices = append(spyreDevices, device)
		}
	}

	return spyreDevices, nil
}

// IsApplicable checks if Spyre validation is applicable to the current system.
func IsApplicable() bool {
	return GetNumberOfSpyreCards() > 0
}

// RunChecks executes all Spyre validation checks.
func RunChecks() []check.CheckResult {
	checks := []check.CheckResult{
		checkDriverConfig(),
		checkUdevRule(),
		checkMemlockConf(),
		checkVfioPciConf(),
		checkUserGroup(),
		checkVfioModule(),
		checkVfioAccessPermission(),
	}

	// Only check podman service configuration if podman is installed
	if _, err := utils.Podman(); err == nil {
		checks = append(checks, CheckPodmanServiceSupplementaryGroups())
	}

	return checks
}

// parseVfioConfigLine parses a single VFIO configuration line and returns the module name
// and its configuration key-value pairs. Returns ok=false if the line is not a valid config line
// Expected format: "options <module> key1=value1 key2=value2 ...".
func parseVfioConfigLine(line string) (module string, configs map[string]string, ok bool) {
	line = strings.TrimSpace(line)
	matches := vfioOptionsPattern.FindStringSubmatch(line)
	if matches == nil {
		return "", nil, false
	}

	module = matches[1]
	configStr := strings.TrimSpace(matches[2])
	configs = make(map[string]string)

	configMatches := vfioConfigPattern.FindAllStringSubmatch(configStr, -1)
	for _, match := range configMatches {
		key := match[1]
		value := match[2]
		configs[key] = value
	}

	return module, configs, true
}

// readConfigFileLines reads a config file and handles errors consistently
// Returns lines and true if successful, or empty slice and false on error.
func readConfigFileLines(filePath string) ([]string, bool) {
	lines, err := utils.ReadFileLines(filePath)
	if err != nil {
		log.Printf("Error reading %s: %v", filePath, err)

		return nil, false
	}

	return lines, true
}

// addDriverConfigAttribute adds a configuration attribute to the check result
// with appropriate actual and expected values based on validation status.
func addDriverConfigAttribute(confCheck *check.ConfigurationFileCheck, key string, found bool, actual, expected string) bool {
	isValid := found && actual == expected
	if isValid {
		confCheck.AddAttribute(key, true, actual, "")
	} else {
		confCheck.AddAttribute(key, false, actual, expected)
	}

	return isValid
}

// checkDriverConfig validates VFIO driver configuration
// Checks /etc/modprobe.d/vfio-pci.conf for required module options:
// - vfio-pci:ids must be "1014:06a7,1014:06a8"
// - vfio-pci:disable_idle_d3 must be "yes".
func checkDriverConfig() *check.ConfigurationFileCheck {
	confCheck := check.NewConfigurationFileCheck("VFIO Driver configuration", vfioConfigFile)

	type expectedConfig struct {
		key   string
		value string
	}

	expectedConfigs := []expectedConfig{
		{"vfio-pci:ids", "1014:06a7,1014:06a8"},
		{"vfio-pci:disable_idle_d3", "yes"},
	}

	lines, ok := readConfigFileLines(vfioConfigFile)
	if !ok {
		// Mark all expected configs as missing.
		for _, expected := range expectedConfigs {
			addDriverConfigAttribute(confCheck, expected.key, false, "", expected.value)
		}
		confCheck.SetStatus(false)

		return confCheck
	}

	// Parse all configuration lines and build a map of found configs
	foundConfigs := make(map[string]string)
	for _, line := range lines {
		module, configs, ok := parseVfioConfigLine(line)
		if !ok {
			continue
		}

		for key, value := range configs {
			configKey := fmt.Sprintf("%s:%s", module, key)
			foundConfigs[configKey] = value
		}
	}

	// Check each expected configuration
	allValid := true
	for _, expected := range expectedConfigs {
		actual, found := foundConfigs[expected.key]
		isValid := addDriverConfigAttribute(confCheck, expected.key, found, actual, expected.value)
		allValid = allValid && isValid
	}

	confCheck.SetStatus(allValid)

	return confCheck
}

// checkUdevRule validates VFIO udev rules configuration.
func checkUdevRule() *check.ConfigurationFileCheck {
	configFile := "/etc/udev/rules.d/95-vfio-3.rules"
	expectedRule := `SUBSYSTEM=="vfio", GROUP:="sentient", MODE:="0660"`
	confCheck := check.NewConfigurationFileCheck("VFIO udev rules configuration", configFile)

	status := false
	lines, ok := readConfigFileLines(configFile)
	if ok {
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			if line == expectedRule {
				status = true

				break
			}

			// Check for incorrect vfio rules.
			if strings.Contains(line, `SUBSYSTEM=="vfio"`) &&
				(strings.Contains(line, "GROUP:=") || strings.Contains(line, "MODE:=")) {
				status = false

				break
			}
		}
	}

	confCheck.AddAttribute(expectedRule, status, "", "")
	confCheck.SetStatus(status)

	return confCheck
}

// parseMemLimitLine extracts the group pattern and value from a memlock config line.
func parseMemLimitLine(line string, pattern *regexp.Regexp) (groupPattern, value string, ok bool) {
	matches := pattern.FindStringSubmatch(line)
	if matches == nil {
		return "", "", false
	}

	return matches[1], matches[2], true
}

// isMemLimitValid checks if a line's memlock value satisfies the expected limit.
func isMemLimitValid(lineValue, expectedValue string) bool {
	// If line has unlimited, it satisfies any requirement
	if lineValue == "unlimited" {
		return true
	}

	// If expected is unlimited but line isn't, it doesn't satisfy.
	if expectedValue == "unlimited" {
		return false
	}

	// Both are numeric - compare values.
	lineInt, err1 := strconv.Atoi(lineValue)
	expectedInt, err2 := strconv.Atoi(expectedValue)
	if err1 != nil || err2 != nil {
		return false
	}

	return lineInt >= expectedInt
}

// isMemLimitConfigValid checks if memlock configuration is valid.
func isMemLimitConfigValid(configFile, expectedConf string) bool {
	lines, err := utils.ReadFileLines(configFile)
	if err != nil {
		log.Printf("Error reading %s: %v", configFile, err)

		return false
	}

	// Parse expected configuration once.
	expectedGroup, expectedValue, ok := parseMemLimitLine(expectedConf, memLimitPattern)
	if !ok {
		return false
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines.
		if line == "" {
			continue
		}

		// Check for exact match first.
		if line == expectedConf {
			return true
		}

		// Parse current line.
		lineGroup, lineValue, ok := parseMemLimitLine(line, memLimitPattern)
		if !ok {
			continue
		}

		// Check if this line matches the expected group pattern.
		if lineGroup == expectedGroup {
			return isMemLimitValid(lineValue, expectedValue)
		}
	}

	return false
}

// checkMemlockConf validates user memlock configuration.
func checkMemlockConf() *check.ConfigurationFileCheck {
	numCards := GetNumberOfSpyreCards()
	memlimit := numCards * memLimitPerCard
	expectedConf := fmt.Sprintf("@sentient - memlock %d", memlimit)
	configFile := "/etc/security/limits.d/memlock.conf"

	confCheck := check.NewConfigurationFileCheck("User memlock configuration", configFile)

	status := isMemLimitConfigValid(configFile, expectedConf)
	confCheck.AddAttribute(expectedConf, status, "", "")
	confCheck.SetStatus(status)

	return confCheck
}

// checkVfioPciConf validates VFIO module dep configuration.
func checkVfioPciConf() *check.ConfigurationFileCheck {
	configFile := "/etc/modules-load.d/vfio-pci.conf"
	expectedModules := []string{"vfio-pci", "vfio_iommu_spapr_tce"}
	confCheck := check.NewConfigurationFileCheck("VFIO module dep configuration", configFile)

	status := true
	lines, ok := readConfigFileLines(configFile)
	if !ok {
		status = false
	} else {
		remainingModules := make(map[string]bool)
		for _, mod := range expectedModules {
			remainingModules[mod] = true
		}

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if _, exists := remainingModules[line]; exists {
				confCheck.AddAttribute(line, true, "", "")
				delete(remainingModules, line)
			}
		}

		for mod := range remainingModules {
			confCheck.AddAttribute(mod, false, "", "")
			status = false
		}
	}

	confCheck.SetStatus(status)

	return confCheck
}

// checkUserGroup validates user group configuration.
func checkUserGroup() *check.ConfigCheck {
	userGroupCheck := check.NewConfigCheck("User group configuration")

	status := utils.GroupExists(sentientGroup)
	userGroupCheck.AddConfig(sentientGroup, status)
	userGroupCheck.SetStatus(status)

	return userGroupCheck
}

// checkVfioModule validates VFIO kernel module is loaded.
func checkVfioModule() *check.Check {
	moduleCheck := check.NewCheck("VFIO kernel module loaded")

	status := utils.IsModuleLoaded("vfio_pci")
	moduleCheck.SetStatus(status)

	return moduleCheck
}

// checkVfioAccessPermission validates VFIO device permissions.
func checkVfioAccessPermission() *check.FilesCheck {
	vfioDir := "/dev/vfio/"
	permCheck := check.NewFilesCheck("VFIO device permission")

	// Read directory entries (combines existence check with read).
	entries, err := os.ReadDir(vfioDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("No %s directory", vfioDir)
		} else {
			log.Printf("Failed to read directory %s: %v", vfioDir, err)
		}
		permCheck.SetStatus(false)

		return permCheck
	}

	gid, err := utils.GetGroupID(sentientGroup)
	if err != nil {
		log.Printf("Failed to get group ID for %s: %v", sentientGroup, err)
		permCheck.SetStatus(false)

		return permCheck
	}

	status := true
	for _, entry := range entries {
		// Skip /dev/vfio/vfio file.
		if entry.Name() == "vfio" {
			continue
		}

		fullPath := filepath.Join(vfioDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			log.Printf("Failed to get info for %s: %v", fullPath, err)

			continue
		}

		// Check character devices only.
		if info.Mode()&os.ModeCharDevice == 0 {
			continue
		}

		fileStatus, err := checkVfioDevicePermission(fullPath, gid)
		if err != nil {
			log.Printf("Failed to check permissions for %s: %v", fullPath, err)
			permCheck.AddFile(fullPath, false)
			status = false

			continue
		}

		permCheck.AddFile(fullPath, fileStatus)
		status = status && fileStatus
	}

	permCheck.SetStatus(status)

	return permCheck
}

// checkVfioDevicePermission validates a single VFIO device's permissions.
func checkVfioDevicePermission(path string, expectedGid int) (bool, error) {
	fileGid, err := utils.GetFileGroupID(path)
	if err != nil {
		return false, err
	}

	return fileGid == expectedGid && utils.IsReadWriteToOwnerGroupUsers(path), nil
}

// CheckPodmanServiceSupplementaryGroups validates that the podman.service has SupplementaryGroups=sentient configured.
//
// Background:
// When Podman commands are executed directly from the shell, they inherit the user's supplementary groups,
// including the 'sentient' group which provides access to VFIO devices for Spyre cards. However, when Podman
// is invoked via the Podman socket (e.g., through systemd or remote API calls), the service runs with its own
// process context and does not automatically inherit the user's supplementary groups.
//
// Without the 'sentient' group in SupplementaryGroups, containers started via the socket will not have the
// necessary permissions to access VFIO devices (/dev/vfio/*), causing failures when trying to use Spyre cards.
//
// This check ensures that the podman.service systemd unit is configured with:
//
//	SupplementaryGroups=sentient
//
// in the [Service] section, allowing socket-based Podman operations to access VFIO devices properly.
func CheckPodmanServiceSupplementaryGroups() *check.ConfigurationFileCheck {
	serviceName := "podman.service"
	confCheck := check.NewConfigurationFileCheck("Podman service SupplementaryGroups configuration", serviceName)

	stdout, err := getServiceConfiguration(serviceName)
	if err != nil {
		confCheck.AddAttribute("SupplementaryGroups=sentient", false, "", "SupplementaryGroups=sentient")
		confCheck.SetStatus(false)

		return confCheck
	}

	found, correctValue := checkSupplementaryGroupsInConfig(stdout)
	setCheckResult(confCheck, found, correctValue)

	return confCheck
}

func getServiceConfiguration(serviceName string) (string, error) {
	exitCode, stdout, stderr, err := utils.ExecuteCommand("systemctl", "cat", serviceName)
	if err != nil || exitCode != 0 {
		log.Printf("Error reading %s: %v, stderr: %s", serviceName, err, stderr)

		return "", err
	}

	return stdout, nil
}

func checkSupplementaryGroupsInConfig(stdout string) (bool, bool) {
	lines := strings.Split(stdout, "\n")
	found := false
	correctValue := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "SupplementaryGroups=") {
			found = true
			value := strings.TrimPrefix(line, "SupplementaryGroups=")
			correctValue = isSentientGroupPresent(value)

			if correctValue {
				break
			}
		}
	}

	return found, correctValue
}

func isSentientGroupPresent(value string) bool {
	// Check if sentient group is included (exact match or in space-separated list)
	if value == sentientGroup {
		return true
	}

	// Check if it's in a space-separated list of groups
	groups := strings.Fields(value)
	for _, group := range groups {
		if group == sentientGroup {
			return true
		}
	}

	return false
}

func setCheckResult(confCheck *check.ConfigurationFileCheck, found, correctValue bool) {
	if found && correctValue {
		confCheck.AddAttribute("SupplementaryGroups=sentient", true, sentientGroup, "")
		confCheck.SetStatus(true)
	} else if found {
		confCheck.AddAttribute("SupplementaryGroups=sentient", false, "incorrect value", sentientGroup)
		confCheck.SetStatus(false)
	} else {
		confCheck.AddAttribute("SupplementaryGroups=sentient", false, "not found", sentientGroup)
		confCheck.SetStatus(false)
	}
}

// Made with Bob
