package bootstrap

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// validationResult holds the outcome of a single rule execution.
type validationResult struct {
	err        error
	shouldStop bool
}

// Validate runs all validation checks.
func (p *BootstrapFactory) Validate(skip map[string]bool) error {
	ctx := context.Background()
	rules := GetRulesForRuntime()

	var validationErrors []error

	for _, rule := range rules {
		ruleName := rule.Name()
		if skip[ruleName] {
			logger.Warningf("%s check skipped; Proceeding without validation may result in deployment failure.", ruleName)

			continue
		}

		result := executeRule(ctx, rule)

		// Handle critical failures that require immediate exit
		if result.shouldStop {
			return result.err
		}

		// Collect non-critical errors
		if result.err != nil {
			validationErrors = append(validationErrors, result.err)
		}
	}

	if len(validationErrors) > 0 {
		return fmt.Errorf("%d validation check(s) failed", len(validationErrors))
	}

	logger.Infoln("All validations passed")

	return nil
}

// GetRulesForRuntime returns the appropriate validation rules based on the runtime type.
func GetRulesForRuntime() []validators.Rule {
	rt := vars.RuntimeFactory.GetRuntimeType()
	switch rt {
	case types.RuntimeTypePodman:
		return validators.PodmanRegistry.Rules()
	case types.RuntimeTypeOpenShift:
		return validators.OpenshiftRegistry.Rules()
	default:
		return nil
	}
}

// executeRule runs a single validation rule, handles errors based on validation level,
// and returns whether execution should continue or stop immediately.
func executeRule(ctx context.Context, rule validators.Rule) validationResult {
	ruleName := rule.Name()
	s := spinner.New("Validating " + ruleName + " ...")
	s.Start(ctx)

	err := rule.Verify()
	if err != nil {
		s.StopWithHint(err.Error(), rule.Hint())

		// Handle based on validation level
		switch rule.Level() {
		case constants.ValidationLevelCritical:
			// Critical failures require immediate exit
			return validationResult{
				err:        fmt.Errorf("%s: %w", ruleName, err),
				shouldStop: true,
			}
		case constants.ValidationLevelError:
			// Error level
			return validationResult{
				err: fmt.Errorf("%s: %w", ruleName, err),
			}
		case constants.ValidationLevelWarning:
			// Warning level
			s.Stop("Warning: " + err.Error())

			return validationResult{}
		}
	}
	s.Stop(rule.Message())

	return validationResult{}
}
