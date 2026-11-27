package bootstrap

import (
	"context"
	"fmt"
	"slices"

	"github.com/project-ai-services/ai-services/internal/pkg/accelerator/amdgpu"
	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/spyre"
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
func (p *BootstrapFactory) Validate(ctx context.Context, skip map[string]bool) error {
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
// For Podman, it selects ROCmRegistry on AMD-only hosts, PodmanRegistry (Spyre) when no AMD GPU is
// detected, and the union of both on hosts that have AMD GPUs and Spyre cards.
func GetRulesForRuntime() []validators.Rule {
	rt := vars.RuntimeFactory.GetRuntimeType()
	switch rt {
	case types.RuntimeTypePodman:
		return podmanRules(amdgpu.IsApplicable(), spyre.IsApplicable())
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

	err := rule.Verify(ctx)
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

// AllPodmanRules returns every Podman rule that can apply on some host (Spyre and ROCm rules, each
// once). It is host-independent, which suits help text and flag descriptions.
func AllPodmanRules() []validators.Rule {
	return podmanRules(true, true)
}

// podmanRules selects the Podman rules for the accelerators present on the host. Without AMD GPUs
// the Spyre (default) rules apply; with only AMD GPUs the ROCm rules apply; with both, the Spyre
// rules are extended with the ROCm rules they do not already contain.
func podmanRules(hasAMDGPU, hasSpyre bool) []validators.Rule {
	switch {
	case !hasAMDGPU:
		return validators.PodmanRegistry.Rules()
	case !hasSpyre:
		return validators.ROCmRegistry.Rules()
	}

	rules := slices.Clone(validators.PodmanRegistry.Rules())
	seen := make(map[string]bool, len(rules))
	for _, r := range rules {
		seen[r.Name()] = true
	}

	for _, r := range validators.ROCmRegistry.Rules() {
		if !seen[r.Name()] {
			rules = append(rules, r)
		}
	}

	return rules
}
