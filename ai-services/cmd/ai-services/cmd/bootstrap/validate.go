package bootstrap

import (
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/validators"
	"github.com/spf13/cobra"
)

// Validation check types.
const (
	CheckRoot   = "root"
	CheckRHEL   = "rhel"
	CheckRHN    = "rhn"
	CheckPower  = "power"
	CheckRHAIIS = "rhaiis"
	CheckNuma   = "numa"
)

const troubleshootingGuide = "https://www.ibm.com/docs/aiservices?topic=services-troubleshooting"

// validateCmd represents the validate subcommand of bootstrap.
func validateCmd() *cobra.Command {
	var skipChecks []string

	cmd := &cobra.Command{
		Use:     "validate",
		Short:   "Validates the environment",
		Long:    longDescription(),
		Example: validateExample(),
		Hidden:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Once precheck passes, silence usage for any *later* internal errors.
			cmd.SilenceUsage = true

			logger.Infoln("Running bootstrap validation...")

			skip := helpers.ParseSkipChecks(skipChecks)
			if len(skip) > 0 {
				logger.Warningln("Skipping validation checks: " + strings.Join(skipChecks, ", "))
			}

			runtimeType, err := cmd.Flags().GetString("runtime")
			if err != nil {
				return fmt.Errorf("failed to get runtime flag: %w", err)
			}
			rt := types.RuntimeType(runtimeType)

			// Create bootstrap instance based on runtime
			factory := bootstrap.NewBootstrapFactory(rt)
			bootstrapInstance, err := factory.Create()
			if err != nil {
				return fmt.Errorf("failed to create bootstrap instance: %w", err)
			}

			if err := bootstrapInstance.Validate(skip); err != nil {
				logger.Infof("Please refer to troubleshooting guide for more information: %s", troubleshootingGuide)

				return fmt.Errorf("bootstrap validation failed: %w", err)
			}

			return nil
		},
	}

	skipCheckDesc := BuildSkipFlagDescription()
	cmd.Flags().StringSliceVar(&skipChecks, "skip-validation", []string{}, skipCheckDesc)

	return cmd
}

func longDescription() string {
	validationList := generateValidationList()

	return fmt.Sprintf(`Validates all prerequisites and configurations are correct for bootstrapping. 

Following scenarios are validated and are available for skipping using --skip-validation flag:
%s`, validationList)
}

func validateExample() string {
	return `  # Run all validation checks
  ai-services bootstrap validate

  # Skip RHN registration check
  ai-services bootstrap validate --skip-validation rhn

  # Skip multiple checks
  ai-services bootstrap validate --skip-validation rhn,power
  
  # Run with verbose output
  ai-services bootstrap validate --verbose`
}

func generateValidationList() string {
	var b strings.Builder

	maxLen := 0
	rules := validators.DefaultRegistry.Rules()
	for _, rule := range rules {
		if len(rule.Name()) > maxLen {
			maxLen = len(rule.Name())
		}
	}

	for i, rule := range rules {
		ruleName := rule.Name()
		description := rule.Description()
		padding := strings.Repeat(" ", maxLen-len(ruleName))
		fmt.Fprintf(&b, " - %s:%s %s", rule.Name(), padding, description)

		if i < len(rules)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func BuildSkipFlagDescription() string {
	rules := validators.DefaultRegistry.Rules()
	ruleName := make([]string, 0, len(rules))
	for _, rule := range rules {
		ruleName = append(ruleName, rule.Name())
	}

	return fmt.Sprintf("Skip specific validation checks (comma-separated: %s)", strings.Join(ruleName, ","))
}
