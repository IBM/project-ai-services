package bootstrap

import (
	"context"
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/validators"
	"github.com/spf13/cobra"
)

// Validation check types
const (
	CheckRoot   = "root"
	CheckRHEL   = "rhel"
	CheckRHN    = "rhn"
	CheckPower  = "power"
	CheckRHAIIS = "rhaiis"
	CheckNUMA   = "numa"
)

// TODO: Populate this once we have the link
const troubleshootingGuide = ""

// validateCmd represents the validate subcommand of bootstrap
func validateCmd() *cobra.Command {

	var skipChecks []string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "validates the environment",
		Long: `Validate that all prerequisites and configurations are correct for bootstrapping.

This command performs comprehensive validation checks including:

System Checks:
  • Root privileges verification
  • RHEL distribution verification
  • RHEL version validation (9.6 or higher)
  • Power architecture validation
  • RHN registration status
  • NUMA node alignment on LPAR

License:
  • RHAIIS license

All checks must pass for successful bootstrap configuration.


//TODO: generate this via some program
Available checks to skip:
  root    		  - Root privileges check
  rhel            - RHEL OS and version check
  rhn             - Red Hat Network registration check
  power  		  - Power architecture check
  rhaiis   		  - RHAIIS license check
  numa			  - NUMA node check`,
		Example: `  # Run all validation checks
  aiservices bootstrap validate

  # Skip RHN registration check
  aiservices bootstrap validate --skip-validation rhn

  # Skip multiple checks
  aiservices bootstrap validate --skip-validation rhn,power
  
  # Run with verbose output
  aiservices bootstrap validate --verbose`,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {

			logger.Infoln("Running bootstrap validation...")

			skip := helpers.ParseSkipChecks(skipChecks)
			if len(skip) > 0 {
				logger.Warningln("Skipping validation checks: " + strings.Join(skipChecks, ", "))
			}

			err := RunValidateCmd(skip)
			if err != nil {
				logger.Infof("Please refer to troubleshooting guide for more information: %s", troubleshootingGuide)
				return fmt.Errorf("bootstrap validation failed: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringSliceVar(&skipChecks, "skip-validation", []string{},
		"Skip specific validation checks (comma-separated: root,rhel,rhn,power,rhaiis,numa)")

	return cmd
}

func RunValidateCmd(skip map[string]bool) error {
	var validationErrors []error
	ctx := context.Background()

	for _, rule := range validators.DefaultRegistry.Rules() {
		ruleName := rule.Name()
		if skip[ruleName] {
			continue
		}

		s := spinner.New("Validating " + ruleName + " ...")
		s.Start(ctx)
		err := rule.Verify()

		if err != nil {
			s.Fail(err.Error())

			// exit right away if user is not root as other check require root privileges
			if ruleName == CheckRoot {
				return fmt.Errorf("root privileges are required for validation")
			}
			validationErrors = append(validationErrors, fmt.Errorf("%s: %w", ruleName, err))
		} else {
			s.Stop(rule.Message())
		}
	}

	if len(validationErrors) > 0 {
		return fmt.Errorf("%d validation check(s) failed", len(validationErrors))
	}

	logger.Infoln("All validations passed")

	return nil
}
