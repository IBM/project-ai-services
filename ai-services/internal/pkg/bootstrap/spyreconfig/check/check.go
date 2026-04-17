package check

import (
	"fmt"
	"strings"
)

// CheckResult is the interface that all check types must implement.
type CheckResult interface {
	String() string
	GetStatus() bool
}

// Check represents a basic validation check.
type Check struct {
	Description string
	Status      bool
}

// NewCheck creates a new Check instance.
func NewCheck(description string) *Check {
	return &Check{Description: description, Status: true}
}

// SetStatus sets the status of the check.
func (c *Check) SetStatus(status bool) {
	c.Status = status
}

// GetStatus returns the status of the check.
func (c *Check) GetStatus() bool {
	return c.Status
}

// String returns a string representation of the check.
func (c *Check) String() string {
	status := "PASS"
	if !c.Status {
		status = "FAIL"
	}

	return fmt.Sprintf("[%s] %s", status, c.Description)
}

// ConfigCheck represents a configuration check with multiple config items.
type ConfigCheck struct {
	Check
	Configs map[string]bool
}

// NewConfigCheck creates a new ConfigCheck instance.
func NewConfigCheck(description string) *ConfigCheck {
	return &ConfigCheck{
		Check:   Check{Description: description, Status: true},
		Configs: make(map[string]bool),
	}
}

// AddConfig adds a configuration item and its status.
func (cc *ConfigCheck) AddConfig(name string, status bool) {
	cc.Configs[name] = status
	if !status {
		cc.Status = false
	}
}

// String returns a string representation of the config check.
func (cc *ConfigCheck) String() string {
	var sb strings.Builder
	status := "PASS"
	if !cc.Status {
		status = "FAIL"
	}
	fmt.Fprintf(&sb, "[%s] %s\n", status, cc.Description)
	for name, status := range cc.Configs {
		configStatus := "OK"
		if !status {
			configStatus = "MISSING"
		}
		sb.WriteString(fmt.Sprintf("  - %s: %s\n", name, configStatus))
	}

	return sb.String()
}

// ConfigurationFileCheck represents a check for configuration file contents.
type ConfigurationFileCheck struct {
	Check
	FilePath   string
	Attributes map[string]*AttributeCheck
}

// AttributeCheck represents a single attribute check.
type AttributeCheck struct {
	Key           string
	Status        bool
	CurrentValue  string
	ExpectedValue string
}

// NewConfigurationFileCheck creates a new ConfigurationFileCheck instance.
func NewConfigurationFileCheck(description, filePath string) *ConfigurationFileCheck {
	return &ConfigurationFileCheck{
		Check:      Check{Description: description, Status: true},
		FilePath:   filePath,
		Attributes: make(map[string]*AttributeCheck),
	}
}

// AddAttribute adds an attribute check.
func (cfc *ConfigurationFileCheck) AddAttribute(key string, status bool, currentValue, expectedValue string) {
	if key == "" {
		return // Skip empty keys
	}
	cfc.Attributes[key] = &AttributeCheck{
		Key:           key,
		Status:        status,
		CurrentValue:  currentValue,
		ExpectedValue: expectedValue,
	}
	if !status {
		cfc.Status = false
	}
}

// String returns a string representation of the configuration file check.
func (cfc *ConfigurationFileCheck) String() string {
	var sb strings.Builder
	status := "PASS"
	if !cfc.Status {
		status = "FAIL"
	}
	fmt.Fprintf(&sb, "[%s] %s (File: %s)\n", status, cfc.Description, cfc.FilePath)
	for _, attr := range cfc.Attributes {
		attrStatus := "OK"
		if !attr.Status {
			attrStatus = "MISSING/INCORRECT"
		}
		sb.WriteString(fmt.Sprintf("  - %s: %s", attr.Key, attrStatus))
		if attr.ExpectedValue != "" {
			sb.WriteString(fmt.Sprintf(" (Expected: %s)", attr.ExpectedValue))
		}
		if attr.CurrentValue != "" {
			sb.WriteString(fmt.Sprintf(" (Current: %s)", attr.CurrentValue))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// PackageCheck represents a package installation check.
type PackageCheck struct {
	Check
	PackageName string
}

// NewPackageCheck creates a new PackageCheck instance.
func NewPackageCheck(description, packageName string, status bool) *PackageCheck {
	return &PackageCheck{
		Check:       Check{Description: description, Status: status},
		PackageName: packageName,
	}
}

// String returns a string representation of the package check.
func (pc *PackageCheck) String() string {
	status := "INSTALLED"
	if !pc.Status {
		status = "NOT INSTALLED"
	}

	return fmt.Sprintf("[%s] %s: %s", status, pc.Description, pc.PackageName)
}

// FilesCheck represents a check for multiple files.
type FilesCheck struct {
	Check
	Files map[string]bool
}

// NewFilesCheck creates a new FilesCheck instance.
func NewFilesCheck(description string) *FilesCheck {
	return &FilesCheck{
		Check: Check{Description: description, Status: true},
		Files: make(map[string]bool),
	}
}

// AddFile adds a file and its status.
func (fc *FilesCheck) AddFile(path string, status bool) {
	fc.Files[path] = status
	if !status {
		fc.Status = false
	}
}

// String returns a string representation of the files check.
func (fc *FilesCheck) String() string {
	var sb strings.Builder
	status := "PASS"
	if !fc.Status {
		status = "FAIL"
	}
	fmt.Fprintf(&sb, "[%s] %s\n", status, fc.Description)
	for path, fileStatus := range fc.Files {
		fileStatusStr := "OK"
		if !fileStatus {
			fileStatusStr = "FAIL"
		}
		fmt.Fprintf(&sb, "  - %s: %s\n", path, fileStatusStr)
	}

	return sb.String()
}

// Made with Bob
