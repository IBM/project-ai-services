package bootstrap

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	log "github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	logger = log.GetLogger()
)

// Validation check types
const (
	CheckRoot    = "root"
	CheckRHEL    = "rhel"
	CheckRHN     = "rhn"
	CheckPower11 = "power11"
	CheckRHAIIS  = "rhaiis"
)

// validateCmd represents the validate subcommand of bootstrap
func validateCmd() *cobra.Command {

	var skipChecks []string
	var verbose bool

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "validates the environment",
		Long: `Validate that all prerequisites and configurations are correct for bootstrapping.

This command performs comprehensive validation checks including:

System Checks:
  • Root privileges verification
  • RHEL distribution verification
  • RHEL version validation (9.6 or higher)
  • Power 11 architecture validation
  • RHN registration status

License:
  • RHAIIS license

All checks must pass for successful bootstrap configuration.


//TODO: generate this via some program
Available checks to skip:
  root    		  - Root privileges check
  rhel            - RHEL OS and version check
  rhn             - Red Hat Network registration check
  power11  		  - Power 11 architecture check
  rhaiis   		  - RHAIIS license check`,
		Example: `  # Run all validation checks
  aiservices bootstrap validate

  # Skip RHN registration check
  aiservices bootstrap validate --skip-validation rhn

  # Skip multiple checks
  aiservices bootstrap validate --skip-validation rhn,power11
  
  # Run with verbose output
  aiservices bootstrap validate --verbose`,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {

			// TODO: use klog structured logging
			if verbose {
				log.SetLogLevel(zap.DebugLevel)
				logger.Debug("Verbose mode enabled")
			}

			logger.Info("Running bootstrap validation...")

			skip := helpers.ParseSkipChecks(skipChecks)
			if len(skip) > 0 {
				logger.Warn("⚠️  WARNING: Skipping validation checks", zap.Strings("skipped", skipChecks))
			}

			err := RunValidateCmd(skip)
			if err != nil {
				return fmt.Errorf("❌ Bootstrap validation failed: %w", err)
			}

			logger.Info("✅ All validations passed")
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&skipChecks, "skip-validation", []string{},
		"Skip specific validation checks (comma-separated: root,rhel,rhn,power11,rhaiis)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output for debugging")

	return cmd
}

func RunValidateCmd(skip map[string]bool) error {
	var validationErrors []error
	// TODO: add hints for each validation error

	// 1. Root check
	if !skip[CheckRoot] {
		if err := rootCheck(); err != nil {
			// exit from this func since root permission is required to validate other steps
			return err
		}
	}

	// 2. OS and version check
	if !skip[CheckRHEL] {
		if err := validateOS(); err != nil {
			validationErrors = append(validationErrors, err)
		}
	}

	// 3. Validate RHN registration
	if !skip[CheckRHN] {
		if err := validateRHNRegistration(); err != nil {
			validationErrors = append(validationErrors, err)
		}
	}

	// 4. IBM Power Version Validation
	if !skip[CheckPower11] {
		if err := validatePowerVersion(); err != nil {
			validationErrors = append(validationErrors, err)
		}
	}

	// TODO: 5. RHAIIS Licence Validation
	if !skip[CheckRHAIIS] {
		if err := validateRHAIISLicense(); err != nil {
			validationErrors = append(validationErrors, err)
		}
	}

	// 6. Check if Spyre is attached to the system
	if !skip["spyre"] {
		if err := validateSpyreAttachment(); err != nil {
			validationErrors = append(validationErrors, err)
		}
	}

	if len(validationErrors) > 0 {
		logger.Error("❌ Validation failed with errors:")
		for i, err := range validationErrors {
			logger.Error(fmt.Sprintf("  %d. %s", i+1, err.Error()))
		}
		return fmt.Errorf("%d validation check(s) failed", len(validationErrors))
	}
	return nil
}

func rootCheck() error {
	euid := os.Geteuid()

	if euid == 0 {
		logger.Info("✅ Current user is root.")
	} else {
		logger.Error("❌ Current user is not root.")
		logger.Debug("Effective User ID", zap.Int("euid", euid))
		return fmt.Errorf("root privileges are required to run this command")
	}
	return nil
}

// validateOS checks if the OS is RHEL and version is 9.6 or higher
func validateOS() error {
	logger.Debug("Validating operating system...")

	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return err
	}

	// verify if OS is RHEL
	osInfo := string(data)
	isRHEL := strings.Contains(osInfo, "Red Hat Enterprise Linux") ||
		strings.Contains(osInfo, `ID="rhel"`) ||
		strings.Contains(osInfo, `ID=rhel`)

	if !isRHEL {
		return fmt.Errorf("❌ unsupported operating system: only RHEL is supported")
	}

	// verify if version is 9.6 or higher
	idx := strings.Index(osInfo, "VERSION_ID=")
	if idx == -1 {
		return fmt.Errorf("unable to determine OS version")
	}
	rest := osInfo[idx+len("VERSION_ID="):]
	if end := strings.IndexByte(rest, '\n'); end != -1 {
		rest = rest[:end]
	}
	version := strings.Trim(rest, `"`)

	parts := strings.Split(version, ".")
	major, _ := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}

	if major < 9 || (major == 9 && minor < 6) {
		return fmt.Errorf("❌ unsupported RHEL version: %s. Minimum required version is 9.6", version)
	}

	logger.Info("✅ Operating system is RHEL", zap.String("version", version))
	return nil
}

// validateRHNRegistration checks if the system is registered with RHN
func validateRHNRegistration() error {
	logger.Debug("Validating RHN registration...")
	cmd := exec.Command("dnf", "repolist")
	output, err := cmd.CombinedOutput()

	// Checking the output content first, as dnf may return non-zero exit code
	// even when the system is registered
	outputStr := string(output)
	if strings.Contains(outputStr, "This system is not registered") {
		return fmt.Errorf("❌ system is not registered with RHN")
	}

	if err != nil {
		return fmt.Errorf("❌ failed to check registration status: %w", err)
	}

	logger.Info("✅ System is registered with RHN")
	return nil
}

// validatePowerVersion checks if the system is running on IBM POWER11 architecture
func validatePowerVersion() error {
	logger.Debug("Validating IBM Power version...")

	if runtime.GOARCH != "ppc64le" {
		return fmt.Errorf("❌ unsupported architecture: %s. IBM Power architecture (ppc64le) is required", runtime.GOARCH)
	}

	data, err := os.ReadFile("/proc/cpuinfo")
	if err == nil && strings.Contains(strings.ToLower(string(data)), "power11") {
		logger.Info("✅ System is running on IBM Power11 architecture")
		return nil
	}

	return fmt.Errorf("❌ unsupported IBM Power version: Power11 is required")
}

// validateRHAIISLicense checks if a valid RHAIIS license is present
func validateRHAIISLicense() error {
	logger.Debug("Validating RHAIIS license...")
	return nil
}

func validateSpyreAttachment() error {
	logger.Debug("Validating Spyre attachment...")
	out, err := exec.Command("lspci").Output()
	if err != nil {
		return fmt.Errorf("❌ failed to execute lspci command: %w", err)
	}

	if !strings.Contains(string(out), "IBM Spyre Accelerator") {
		return fmt.Errorf("❌ IBM Spyre Accelerator is not attached to the LPAR")
	}

	logger.Info("✅ IBM Spyre Accelerator is attached to the LPAR")
	return nil
}
