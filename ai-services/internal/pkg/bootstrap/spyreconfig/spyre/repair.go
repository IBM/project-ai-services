package spyre

import (
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
	results = append(results, fixUdevRule(checkMap))
	results = append(results, fixVFIOPCIConf(checkMap))
	userGroupResult := fixUserGroup(checkMap)
	results = append(results, userGroupResult)
	results = append(results, fixVFIOModule(checkMap))
	results = append(results, fixVFIOPermissions(checkMap, userGroupResult))

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

	expectedRule := `SUBSYSTEM=="vfio", GROUP:="sentient", MODE:="0660"`

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

	// Add the correct rule at the beginning.
	updatedLines = append([]string{expectedRule}, updatedLines...)

	// Write back.
	content := strings.Join(updatedLines, "\n") + "\n"
	if err := utils.WriteToFile(confCheck.FilePath, content); err != nil {
		return RepairResult{CheckName: checkName, Status: StatusFailedToFix, Error: err}
	}

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

// Made with Bob
