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

// Validate runs all validation checks.
func (p *BootstrapFactory) Validate(skip map[string]bool) error {
	var validationErrors []error
	ctx := context.Background()

	var rules []validators.Rule

	rt := vars.RuntimeFactory.GetRuntimeType()
	switch rt {
	case types.RuntimeTypePodman:
		rules = validators.PodmanRegistry.Rules()
	case types.RuntimeTypeOpenShift:
		rules = validators.OpenshiftRegistry.Rules()
	}

	for _, rule := range rules {
		ruleName := rule.Name()
		if skip[ruleName] {
			logger.Warningf("%s check skipped; Proceeding without validation may result in deployment failure.", ruleName)

			continue
		}

		s := spinner.New("Validating " + ruleName + " ...")
		s.Start(ctx)
		err := rule.Verify()

		if err != nil {
			s.Fail(err.Error())
			s.StopWithHint(err.Error(), rule.Hint())

			// exit right away if user is not root as other checks require root privileges
			if ruleName == "root" {
				return fmt.Errorf("root privileges are required for validation")
			}

			switch rule.Level() {
			case constants.ValidationLevelError:
				s.Fail(err.Error())
				validationErrors = append(validationErrors, fmt.Errorf("%s: %w", ruleName, err))
			case constants.ValidationLevelWarning:
				s.Stop("Warning: " + err.Error())
			}
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
