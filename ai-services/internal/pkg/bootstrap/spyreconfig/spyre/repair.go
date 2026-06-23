package spyre

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/check"
	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/utils/selinux"
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
	maxVfioRuleParts = 4
	// dirPermissions is the default permission for creating directories.
	dirPermissions = 0755
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
	const checkResultsLen = 7
	results := make([]RepairResult, 0, checkResultsLen)

	// Create a map for easy lookup.
	checkMap := make(map[string]check.CheckResult)
	for _, chk := range checks {
		checkMap[getCheckDescription(chk)] = chk
	}

	// Fix checks in dependency order.
	// Note: User group, ulimit, and systemd slice limit configurations moved to generic bootstrap flow
	results = append(results, fixVFIODriverConfig(checkMap))
	results = append(results, fixUdevRule(checkMap))
	results = append(results, fixVFIOPCIConf(checkMap))
	results = append(results, fixVFIOModule(checkMap))
	results = append(results, fixVFIOPermissions(checkMap, RepairResult{}))
	results = append(results, fixSELinuxVFIOPolicy())
	results = append(results, fixPodmanServiceSupplementaryGroups(checkMap))

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

	const expectedRuleCount = 2
	expectedRules := make([]string, 0, expectedRuleCount)
	expectedRules = append(expectedRules, `SUBSYSTEM=="vfio", GROUP:="sentient", MODE:="0660", SECLABEL{selinux}="system_u:object_r:vfio_device_t:s0"`)
	expectedRules = append(expectedRules, `KERNEL=="vfio", GROUP:="sentient", MODE:="0660", SECLABEL{selinux}="system_u:object_r:vfio_device_t:s0"`)

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

// isSELinuxEnabledAndActive checks if SELinux is enabled and active.
func isSELinuxEnabledAndActive() (bool, string) {
	exitCode, stdout, _, err := utils.ExecuteCommand("getenforce")
	if err != nil || exitCode != 0 {
		return false, "SELinux not available or not enabled"
	}

	status := strings.TrimSpace(stdout)
	if status == "Disabled" {
		return false, "SELinux is disabled"
	}

	return true, ""
}

// fixSELinuxVFIOPolicy configures SELinux policy for VFIO device access.
// This allows containers with container_t type to access VFIO devices.
func fixSELinuxVFIOPolicy() RepairResult {
	result := ApplySELinuxPolicy(
		"SELinux VFIO policy configuration",
		"vllm_vfio_policy",
		selinux.VFIOPolicyContent,
		"SELinux VFIO policy configured successfully",
	)

	// Reload udev rules to apply SELinux labels to existing devices if policy was fixed
	if result.Status == StatusFixed {
		if err := utils.ReloadUdevRules(); err != nil {
			return RepairResult{
				CheckName: result.CheckName,
				Status:    StatusFailedToFix,
				Error:     err,
			}
		}
	}

	return result
}

// ApplySELinuxPolicy is a generic helper to apply SELinux policies.
func ApplySELinuxPolicy(checkName, policyName, policyContent, successMessage string) RepairResult {
	enabled, msg := isSELinuxEnabledAndActive()
	if !enabled {
		return RepairResult{CheckName: checkName, Status: StatusSkipped, Message: msg}
	}

	tmpDir, err := os.MkdirTemp("", "selinux_build")
	if err != nil {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusFailedToFix,
			Error:     fmt.Errorf("failed to create temp directory: %w", err),
		}
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove temp directory %s: %v\n", tmpDir, err)
		}
	}()

	if slices.Contains(selinux.CILPolicyContent, policyName) {
		// Use reinstall=true to ensure policy is updated if it already exists
		err = installSELinuxPolicyCil(tmpDir, policyName, policyContent, true)
	} else {
		// Use reinstall=true to ensure policy is updated if it already exists
		err = buildAndInstallSELinuxPolicy(tmpDir, policyName, policyContent, true)
	}
	if err != nil {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusFailedToFix,
			Error:     err,
		}
	}

	return RepairResult{CheckName: checkName, Status: StatusFixed, Message: successMessage}
}

// buildAndInstallSELinuxPolicy builds and installs a SELinux policy module.
func installSELinuxPolicyCil(tmpDir, policyName, teContent string, reinstall bool) error {
	// Write the .te file
	cilPath := fmt.Sprintf("%s/%s.cil", tmpDir, policyName)
	if err := utils.WriteToFile(cilPath, teContent); err != nil {
		return fmt.Errorf("failed to write .cil file: %w", err)
	}

	// Install or update the module
	if reinstall {
		// Remove old module first
		_, _, _, _ = utils.ExecuteCommand("semodule", "-r", policyName)
	}

	// Install the module
	exitCode, _, stderr, err := utils.ExecuteCommand("semodule", "-i",
		cilPath,
		"/usr/share/udica/templates/base_container.cil",
		"/usr/share/udica/templates/net_container.cil")
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to install custom selinux policy: %v, stderr: %s", err, stderr)
	}

	return nil
}

// buildAndInstallSELinuxPolicy builds and installs a SELinux policy module.
func buildAndInstallSELinuxPolicy(tmpDir, policyName, teContent string, reinstall bool) error {
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

	// Install or update the module
	if reinstall {
		// Remove old module first
		_, _, _, _ = utils.ExecuteCommand("semodule", "-r", policyName)
	}

	// Install the module
	exitCode, _, stderr, err = utils.ExecuteCommand("semodule", "-i", ppPath)
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to install policy module: %v, stderr: %s", err, stderr)
	}

	return nil
}

// fixPodmanServiceSupplementaryGroups repairs the podman service SupplementaryGroups configuration.
//
// This function addresses the issue where Podman operations invoked via the socket (e.g., through
// systemd or remote API calls) lack access to VFIO devices because the service doesn't inherit
// the user's supplementary groups. While shell-based Podman commands work fine (inheriting the
// user's 'sentient' group), socket-based operations fail without explicit configuration.
//
// The repair process:
//  1. Creates a systemd drop-in file at /etc/systemd/system/podman.service.d/override.conf
//     containing: [Service]\nSupplementaryGroups=sentient
//  2. Reloads the systemd daemon to pick up the new configuration
//  3. Restarts both podman.service and podman.socket to apply the changes
//
// This ensures that all Podman operations, regardless of invocation method, have the necessary
// permissions to access VFIO devices (/dev/vfio/*) required for Spyre card functionality.
func fixPodmanServiceSupplementaryGroups(checkMap map[string]check.CheckResult) RepairResult {
	checkName := "Podman service SupplementaryGroups configuration"
	_, ok := getCheckFromMap(checkMap, checkName)
	if !ok {
		return RepairResult{CheckName: checkName, Status: StatusSkipped}
	}

	if err := createPodmanServiceDropIn(); err != nil {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusFailedToFix,
			Error:     err,
			Message:   err.Error(),
		}
	}

	if err := reloadAndRestartPodmanServices(); err != nil {
		return RepairResult{
			CheckName: checkName,
			Status:    StatusFailedToFix,
			Error:     err,
			Message:   err.Error(),
		}
	}

	return RepairResult{
		CheckName: checkName,
		Status:    StatusFixed,
	}
}

func createPodmanServiceDropIn() error {
	dropInDir := "/etc/systemd/system/podman.service.d"
	if err := os.MkdirAll(dropInDir, dirPermissions); err != nil {
		return err
	}

	dropInFile := dropInDir + "/override.conf"
	dropInContent := `[Service]
SupplementaryGroups=sentient
`

	return utils.WriteToFile(dropInFile, dropInContent)
}

func reloadAndRestartPodmanServices() error {
	// Reload systemd daemon
	exitCode, _, _, err := utils.ExecuteCommand("systemctl", "daemon-reload")
	if err != nil || exitCode != 0 {
		if err == nil {
			err = os.ErrInvalid
		}

		return err
	}

	// Restart podman service
	exitCode, _, _, err = utils.ExecuteCommand("systemctl", "restart", "podman.service")
	if err != nil || exitCode != 0 {
		if err == nil {
			err = os.ErrInvalid
		}

		return err
	}

	// Restart podman socket
	exitCode, _, _, err = utils.ExecuteCommand("systemctl", "restart", "podman.socket")
	if err != nil || exitCode != 0 {
		if err == nil {
			err = os.ErrInvalid
		}

		return err
	}

	return nil
}

// Made with Bob
