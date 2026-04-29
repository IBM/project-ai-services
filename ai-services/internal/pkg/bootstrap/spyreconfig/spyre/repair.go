package spyre

import (
	"fmt"
	"os"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/check"
	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/utils"
)

// RepairStatus represents the status of a repair operation.
type RepairStatus string

const (
	// StatusFixed indicates the issue was successfully fixed.
	StatusFixed RepairStatus = "FIXED"
	// StatusFailedToFix indicates the repair attempt failed.
	StatusFailedToFix RepairStatus = "FAILED_TO_FIX"
	// StatusNotFixable indicates the issue cannot be automatically fixed.
	StatusNotFixable RepairStatus = "NOT_FIXABLE"
	// StatusSkipped indicates the repair was skipped.
	StatusSkipped RepairStatus = "SKIPPED"

	// expectedKeyValueParts is the expected number of parts when splitting a key:value pair.
	expectedKeyValueParts = 2
	// maxVfioRuleParts is the maximum number of comma-separated parts in a valid VFIO rule.
	maxVfioRuleParts = 3
)

// RepairResult represents the result of a repair operation.
type RepairResult struct {
	CheckName string
	Status    RepairStatus
	Message   string
	Error     error
}

// Repair attempts to fix all failed Spyre checks.
func Repair(checks []check.CheckResult) []RepairResult {
	var results []RepairResult

	// Create a map for easy lookup.
	checkMap := make(map[string]check.CheckResult)
	for _, chk := range checks {
		checkMap[getCheckDescription(chk)] = chk
	}

	// Fix checks in dependency order.
	results = append(results, fixVFIODriverConfig(checkMap))
	results = append(results, fixMemlockConf(checkMap))
	results = append(results, fixNofileConf(checkMap))
	results = append(results, fixUdevRule(checkMap))
	results = append(results, fixVFIOPCIConf(checkMap))
	userGroupResult := fixUserGroup(checkMap)
	results = append(results, userGroupResult)
	results = append(results, fixVFIOModule(checkMap))
	results = append(results, fixVFIOPermissions(checkMap, userGroupResult))
	results = append(results, fixSystemdUserSliceLimits(checkMap))

	results = append(results, fixSELinuxVFIOPolicy(checkMap))
	return results
}

// getCheckDescription extracts the description from a check.
func getCheckDescription(chk check.CheckResult) string {
	switch c := chk.(type) {
	case *check.Check:
		return c.Description
	case *check.ConfigCheck:
		return c.Description
	case *check.ConfigurationFileCheck:
		return c.Description
	case *check.PackageCheck:
		return c.Description
	case *check.FilesCheck:
		return c.Description
	default:
		return ""
	}
}

// getCheckFromMap retrieves a check from the map and returns early if skipped.
func getCheckFromMap(checkMap map[string]check.CheckResult, checkName string) (check.CheckResult, bool) {
	chk, exists := checkMap[checkName]
	if !exists || chk.GetStatus() {
		return nil, false
	}

	return chk, true
}

// fixVFIODriverConfig repairs VFIO driver configuration.
func fixVFIODriverConfig(checkMap map[string]check.CheckResult) RepairResult {
	checkName := "VFIO Driver configuration"
	chk, ok := getCheckFromMap(checkMap, checkName)
	if !ok {
		return RepairResult{CheckName: checkName, Status: StatusSkipped}
	}

	confCheck, ok := chk.(*check.ConfigurationFileCheck)
	if !ok {
		return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Message: "Invalid check type"}
	}

	// Append missing configurations.
	fileExists := utils.FileExists(confCheck.FilePath)
	for key, attr := range confCheck.Attributes {
		if !attr.Status && attr.ExpectedValue != "" {
			parts := strings.Split(key, ":")
			if len(parts) != expectedKeyValueParts {
				continue
			}
			var sb strings.Builder
			// Only add newline if file already exists and has content.
			if fileExists {
				sb.WriteString("\n")
			}
			sb.WriteString("options ")
			sb.WriteString(parts[0])
			sb.WriteString(" ")
			sb.WriteString(parts[1])
			sb.WriteString("=")
			sb.WriteString(attr.ExpectedValue)
			if err := utils.AppendToFile(confCheck.FilePath, sb.String()); err != nil {
				return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Error: err}
			}
			fileExists = true // After first write, file exists
		}
	}

	return RepairResult{CheckName: checkName, Status: StatusFixed}
}

// fixMemlockConf repairs user memlock configuration.
func fixMemlockConf(checkMap map[string]check.CheckResult) RepairResult {
	checkName := "User memlock configuration"
	chk, ok := getCheckFromMap(checkMap, checkName)
	if !ok {
		return RepairResult{CheckName: checkName, Status: StatusSkipped}
	}

	confCheck, ok := chk.(*check.ConfigurationFileCheck)
	if !ok {
		return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Message: "Invalid check type"}
	}

	// Read existing file.
	lines, err := utils.ReadFileLines(confCheck.FilePath)
	if err != nil && !os.IsNotExist(err) {
		return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Error: err}
	}

	// Remove old @sentient lines.
	var updatedLines []string
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "@sentient") {
			updatedLines = append(updatedLines, line)
		}
	}

	// Add new configuration.
	for key, attr := range confCheck.Attributes {
		if !attr.Status {
			updatedLines = append(updatedLines, key)
		}
	}

	// Write back.
	content := strings.Join(updatedLines, "\n")
	if err := utils.WriteToFile(confCheck.FilePath, content); err != nil {
		return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Error: err}
	}

	msg := "Memlock limit set. User must be in sentient group: sudo usermod -aG sentient <user>"

	return RepairResult{CheckName: checkName, Status: StatusFixed, Message: msg}
}

// fixNofileConf repairs user nofile limit configuration.
func fixNofileConf(checkMap map[string]check.CheckResult) RepairResult {
	checkName := "User nofile limit configuration"
	chk, ok := getCheckFromMap(checkMap, checkName)
	if !ok {
		return RepairResult{CheckName: checkName, Status: StatusSkipped}
	}

	confCheck, ok := chk.(*check.ConfigurationFileCheck)
	if !ok {
		return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Message: "Invalid check type"}
	}

	// Read existing file.
	lines, err := utils.ReadFileLines(confCheck.FilePath)
	if err != nil && !os.IsNotExist(err) {
		return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Error: err}
	}

	// Remove old @sentient nofile lines.
	var updatedLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip lines that configure nofile for @sentient group
		if strings.HasPrefix(trimmed, "@sentient") && strings.Contains(trimmed, "nofile") {
			continue
		}
		updatedLines = append(updatedLines, line)
	}

	// Add new configuration.
	for key, attr := range confCheck.Attributes {
		if !attr.Status {
			updatedLines = append(updatedLines, key)
		}
	}

	// Write back.
	content := strings.Join(updatedLines, "\n")
	if err := utils.WriteToFile(confCheck.FilePath, content); err != nil {
		return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Error: err}
	}

	msg := "File descriptor limit set. User must be in sentient group and re-login for changes to take effect"

	return RepairResult{CheckName: checkName, Status: StatusFixed, Message: msg}
}

// fixUdevRule repairs VFIO udev rules.
func fixUdevRule(checkMap map[string]check.CheckResult) RepairResult {
	checkName := "VFIO udev rules configuration"
	chk, ok := getCheckFromMap(checkMap, checkName)
	if !ok {
		return RepairResult{CheckName: checkName, Status: StatusSkipped}
	}

	confCheck, ok := chk.(*check.ConfigurationFileCheck)
	if !ok {
		return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Message: "Invalid check type"}
	}

	expectedRules := []string{
		`SUBSYSTEM=="vfio", GROUP:="sentient", MODE:="0660"`,
		`KERNEL=="vfio", GROUP:="sentient", MODE:="0660"`,
	}

	// Read existing file if it exists.
	var updatedLines []string
	if utils.FileExists(confCheck.FilePath) {
		lines, err := utils.ReadFileLines(confCheck.FilePath)
		if err != nil {
			return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Error: err}
		}

		// Remove redundant vfio rules.
		for _, line := range lines {
			if !isVFIORuleRedundant(strings.TrimSpace(line)) {
				updatedLines = append(updatedLines, line)
			}
		}
	}

	// Add the correct rules at the beginning.
	updatedLines = append(expectedRules, updatedLines...)

	// Write back.
	content := strings.Join(updatedLines, "\n") + "\n"
	if err := utils.WriteToFile(confCheck.FilePath, content); err != nil {
		return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Error: err}
	}

	// Note: Udev rules are reloaded by fixVFIOPermissions() which runs after this function.
	return RepairResult{CheckName: checkName, Status: StatusFixed}
}

// isVFIORuleRedundant checks if a udev rule is redundant.
func isVFIORuleRedundant(rule string) bool {
	if rule == "" || !strings.Contains(rule, `SUBSYSTEM=="vfio"`) {
		return false
	}

	parts := strings.Split(rule, ",")
	if len(parts) > maxVfioRuleParts {
		return false
	}

	hasGroup := false
	hasMode := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		hasGroup = hasGroup || strings.Contains(part, "GROUP")
		hasMode = hasMode || strings.Contains(part, "MODE")
	}

	return len(parts) <= 3 && (len(parts) == 1 || hasGroup || hasMode)
}

// fixVFIOPCIConf repairs VFIO PCI module configuration.
func fixVFIOPCIConf(checkMap map[string]check.CheckResult) RepairResult {
	checkName := "VFIO module dep configuration"
	chk, ok := getCheckFromMap(checkMap, checkName)
	if !ok {
		return RepairResult{CheckName: checkName, Status: StatusSkipped}
	}

	confCheck, ok := chk.(*check.ConfigurationFileCheck)
	if !ok {
		return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Message: "Invalid check type"}
	}

	// If file doesn't exist or attributes are missing, create with expected modules.
	expectedModules := []string{"vfio-pci", "vfio_iommu_spapr_tce"}

	if len(confCheck.Attributes) == 0 {
		return createModulesFile(confCheck.FilePath, expectedModules, checkName)
	}

	return appendMissingModules(confCheck, checkName)
}

// createModulesFile creates a new modules file with expected modules.
func createModulesFile(filePath string, modules []string, checkName string) RepairResult {
	for _, mod := range modules {
		if err := utils.AppendToFile(filePath, mod+"\n"); err != nil {
			return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Error: err}
		}
	}

	return RepairResult{CheckName: checkName, Status: StatusFixed}
}

// appendMissingModules appends missing modules to an existing file.
func appendMissingModules(confCheck *check.ConfigurationFileCheck, checkName string) RepairResult {
	for key, attr := range confCheck.Attributes {
		if !attr.Status {
			if err := utils.AppendToFile(confCheck.FilePath, "\n"+key); err != nil {
				return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Error: err}
			}
		}
	}

	return RepairResult{CheckName: checkName, Status: StatusFixed}
}

// fixUserGroup repairs user group configuration.
func fixUserGroup(checkMap map[string]check.CheckResult) RepairResult {
	checkName := "User group configuration"
	chk, ok := getCheckFromMap(checkMap, checkName)
	if !ok {
		return RepairResult{CheckName: checkName, Status: StatusSkipped}
	}

	configCheck, ok := chk.(*check.ConfigCheck)
	if !ok {
		return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Message: "Invalid check type"}
	}

	// Create missing groups.
	for groupName, status := range configCheck.Configs {
		if !status {
			if err := utils.CreateGroup(groupName); err != nil {
				return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Error: err}
			}
		}
	}

	return RepairResult{CheckName: checkName, Status: StatusFixed}
}

// fixVFIOModule repairs VFIO kernel module.
func fixVFIOModule(checkMap map[string]check.CheckResult) RepairResult {
	checkName := "VFIO kernel module loaded"
	_, ok := getCheckFromMap(checkMap, checkName)
	if !ok {
		return RepairResult{CheckName: checkName, Status: StatusSkipped}
	}

	if err := utils.LoadKernelModule("vfio_pci"); err != nil {
		return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Error: err}
	}

	return RepairResult{CheckName: checkName, Status: StatusFixed}
}

// fixVFIOPermissions repairs VFIO device permissions.
func fixVFIOPermissions(checkMap map[string]check.CheckResult, userGroupResult RepairResult) RepairResult {
	checkName := "VFIO device permission"
	_, ok := getCheckFromMap(checkMap, checkName)
	if !ok {
		return RepairResult{CheckName: checkName, Status: StatusSkipped}
	}

	// Check if user group was successfully fixed.
	if userGroupResult.Status != StatusFixed && userGroupResult.Status != StatusSkipped {
		return RepairResult{CheckName: checkName, Status: StatusNotFixable,
			Message: "User group must be fixed first"}
	}

	// Reload udev rules.
	if err := utils.ReloadUdevRules(); err != nil {
		return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Error: err}
	}

	return RepairResult{CheckName: checkName, Status: StatusFixed}
}

// fixSystemdUserSliceLimits configures systemd user slice limits for rootless podman.
// This ensures that containers started by non-root users have proper ulimits
func fixSystemdUserSliceLimits(checkMap map[string]check.CheckResult) RepairResult {
	checkName := "Systemd user slice limits configuration"
	chk, ok := getCheckFromMap(checkMap, checkName)
	if !ok {
		return RepairResult{CheckName: checkName, Status: StatusSkipped}
	}

	// Skip if check passed
	if chk.GetStatus() {
		return RepairResult{CheckName: checkName, Status: StatusSkipped}
	}

	// Get the SUDO_USER to configure their slice
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser == "" {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusNotFixable,
			Message:   "Not running via sudo, cannot configure user slice",
		}
	}

	// Get user ID
	exitCode, stdout, stderr, err := utils.ExecuteCommand("id", "-u", sudoUser)
	if err != nil || exitCode != 0 {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusFailedToFix,
			Error:     fmt.Errorf("failed to get user ID: %v, stderr: %s", err, stderr),
		}
	}

	userID := strings.TrimSpace(stdout)
	sliceDir := fmt.Sprintf("/etc/systemd/system/user-%s.slice.d", userID)
	limitsFile := fmt.Sprintf("%s/limits.conf", sliceDir)

	// Create directory
	if err := os.MkdirAll(sliceDir, dirPermissions); err != nil {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusFailedToFix,
			Error:     fmt.Errorf("failed to create directory %s: %w", sliceDir, err),
		}
	}

	// Write limits configuration
	limitsContent := `[Slice]
LimitNOFILE=134217728
LimitMEMLOCK=infinity
`
	if err := utils.WriteToFile(limitsFile, limitsContent); err != nil {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusFailedToFix,
			Error:     fmt.Errorf("failed to write limits file: %w", err),
		}
	}

	// Reload systemd daemon
	exitCode, _, stderr, err = utils.ExecuteCommand("systemctl", "daemon-reload")
	if err != nil || exitCode != 0 {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusFailedToFix,
			Error:     fmt.Errorf("failed to reload systemd: %v, stderr: %s", err, stderr),
		}
	}

	return RepairResult{
		CheckName: checkName,
		Status:    StatusFixed,
		Message:   fmt.Sprintf("Configured systemd slice limits for user %s (UID: %s)", sudoUser, userID),
	}
}

// fixSELinuxVFIOPolicy configures SELinux policy for VFIO device access.
// This allows containers with container_t type to access VFIO devices.
func fixSELinuxVFIOPolicy(checkMap map[string]check.CheckResult) RepairResult {
	checkName := "SELinux VFIO policy configuration"

	// Check if SELinux is enabled
	exitCode, stdout, _, err := utils.ExecuteCommand("getenforce")
	if err != nil || exitCode != 0 {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusSkipped,
			Message:   "SELinux not available or not enabled",
		}
	}

	status := strings.TrimSpace(stdout)
	if status == "Disabled" {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusSkipped,
			Message:   "SELinux is disabled",
		}
	}

	// Check if VFIO devices exist
	if !utils.FileExists("/dev/vfio") {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusSkipped,
			Message:   "VFIO devices not found",
		}
	}

	// Check if policy is already installed
	exitCode, stdout, _, err = utils.ExecuteCommand("semodule", "-l")
	if err == nil && exitCode == 0 && strings.Contains(stdout, "vllm_vfio_policy") {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusSkipped,
			Message:   "SELinux VFIO policy already installed",
		}
	}

	// Create temporary directory for building the policy
	tmpDir, err := os.MkdirTemp("", "selinux_build")
	if err != nil {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusFailedToFix,
			Error:     fmt.Errorf("failed to create temp directory: %w", err),
		}
	}
	defer os.RemoveAll(tmpDir)

	// Build and install the policy
	if err := buildAndInstallSELinuxPolicy(tmpDir); err != nil {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusFailedToFix,
			Error:     err,
		}
	}

	// Update file context database
	if err := updateSELinuxFileContext(); err != nil {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusFailedToFix,
			Error:     err,
		}
	}

	// Apply labels to existing devices
	if err := applySELinuxLabels(); err != nil {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusFailedToFix,
			Error:     err,
		}
	}

	return RepairResult{
		CheckName: checkName,
		Status:    StatusFixed,
		Message:   "SELinux VFIO policy configured successfully",
	}
}

// buildAndInstallSELinuxPolicy builds and installs the SELinux policy module.
func buildAndInstallSELinuxPolicy(tmpDir string) error {
	const policyName = "vllm_vfio_policy"
	const teContent = `
module vllm_vfio_policy 1.0;

require {
    type container_t;
    type vfio_device_t;
    class chr_file { ioctl open read write getattr };
}

# Allow container_t (vLLM) to access vfio_device_t
allow container_t vfio_device_t:chr_file { ioctl open read write getattr };
`

	// Write the .te file
	tePath := fmt.Sprintf("%s/%s.te", tmpDir, policyName)
	if err := utils.WriteToFile(tePath, teContent); err != nil {
		return fmt.Errorf("failed to write .te file: %w", err)
	}

	// Compile .te -> .mod
	modPath := fmt.Sprintf("%s/%s.mod", tmpDir, policyName)
	exitCode, _, stderr, err := utils.ExecuteCommand("checkmodule", "-M", "-m", "-o", modPath, tePath)
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to compile policy module: %v, stderr: %s", err, stderr)
	}

	// Package .mod -> .pp
	ppPath := fmt.Sprintf("%s/%s.pp", tmpDir, policyName)
	exitCode, _, stderr, err = utils.ExecuteCommand("semodule_package", "-o", ppPath, "-m", modPath)
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to package policy module: %v, stderr: %s", err, stderr)
	}

	// Install the module
	exitCode, _, stderr, err = utils.ExecuteCommand("semodule", "-i", ppPath)
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to install policy module: %v, stderr: %s", err, stderr)
	}

	return nil
}

// updateSELinuxFileContext updates the SELinux file context database.
func updateSELinuxFileContext() error {
	const vfioPath = "/dev/vfio(/.*)?"
	const label = "vfio_device_t"

	// Check if context already exists
	exitCode, stdout, _, err := utils.ExecuteCommand("semanage", "fcontext", "-l")
	if err == nil && exitCode == 0 && strings.Contains(stdout, vfioPath) {
		return nil // Already exists
	}

	// Add file context
	exitCode, _, stderr, err := utils.ExecuteCommand("semanage", "fcontext", "-a", "-t", label, vfioPath)
	if err != nil || exitCode != 0 {
		// Check if it's an "already exists" error
		if strings.Contains(stderr, "already exists") {
			return nil
		}
		return fmt.Errorf("failed to add file context: %v, stderr: %s", err, stderr)
	}

	return nil
}

// applySELinuxLabels applies SELinux labels to existing VFIO devices.
func applySELinuxLabels() error {
	if !utils.FileExists("/dev/vfio") {
		// No VFIO devices to label - this is OK
		return nil
	}

	exitCode, _, stderr, err := utils.ExecuteCommand("restorecon", "-Rv", "/dev/vfio")
	if err != nil || exitCode != 0 {
		// Check if it's a permission denied error - this can happen if SELinux is in permissive mode
		// or if the devices are already correctly labeled
		if strings.Contains(stderr, "Permission denied") {
			// Not a fatal error - labels will be applied when devices are accessed
			return nil
		}
		return fmt.Errorf("failed to apply labels: %v, stderr: %s", err, stderr)
	}

	return nil
}
// Made with Bob
