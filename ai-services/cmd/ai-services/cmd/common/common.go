// Package common provides CLI helpers shared across all top-level commands
// (catalog, worker, bootstrap, etc.).
package common

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// InitAndValidateRuntimeFlag validates the runtime flag value, initialises
// vars.RuntimeFactory, and checks platform support. It must be called in
// PreRunE before any code that reads vars.RuntimeFactory.
func InitAndValidateRuntimeFlag(runtimeType string) error {
	rt := types.RuntimeType(runtimeType)
	if !rt.Valid() {
		return fmt.Errorf("invalid runtime type: %s (must be 'podman' or 'openshift'). Please specify runtime using --runtime flag", runtimeType)
	}

	vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)
	logger.Debugf("Using runtime: %s\n", rt)

	if err := utils.CheckPodmanPlatformSupport(rt); err != nil {
		return err
	}

	return validateRuntimeType(rt)
}

// ConfigureRuntimeFlag registers the --runtime / -r flag on cmd and marks it
// required. Use this in every command that accepts a runtime type.
func ConfigureRuntimeFlag(cmd *cobra.Command, runtimeType *string) {
	cmd.Flags().StringVarP(runtimeType, constants.RuntimeFlag, "r", "",
		fmt.Sprintf("runtime to use (options: %s, %s) (required)", types.RuntimeTypePodman, types.RuntimeTypeOpenShift))
	_ = cmd.MarkFlagRequired(constants.RuntimeFlag)
}

func validateRuntimeType(runtimeType types.RuntimeType) error {
	switch runtimeType {
	case types.RuntimeTypePodman, types.RuntimeTypeOpenShift:
		return nil
	default:
		return fmt.Errorf("unsupported runtime type: %s", runtimeType)
	}
}

// ValidateSkipChecksFlag validates the skip-validation flag for the current runtime.
func ValidateSkipChecksFlag(cmd *cobra.Command) error {
	skipChecks, err := cmd.Flags().GetStringSlice("skip-validation")
	if err != nil {
		return err
	}
	if len(skipChecks) == 0 {
		return nil
	}

	validChecks := make(map[string]bool, len(bootstrap.GetRulesForRuntime()))
	for _, r := range bootstrap.GetRulesForRuntime() {
		validChecks[r.Name()] = true
	}

	for _, s := range skipChecks {
		if !validChecks[s] {
			return fmt.Errorf("invalid skip-validation value '%s' for runtime '%s'", s, vars.RuntimeFactory.GetRuntimeType())
		}
	}

	return nil
}

// DoBootstrapValidate runs the bootstrap validation checks for the active runtime, skipping any requested checks.
func DoBootstrapValidate(ctx context.Context, skipChecks []string) error {
	skip := helpers.ParseSkipChecks(skipChecks)
	if len(skip) > 0 {
		logger.Warningf("Skipping validation checks (skipped: %v)\n", skipChecks)
	}

	factory := bootstrap.NewBootstrapFactory(vars.RuntimeFactory.GetRuntimeType())
	if err := factory.Validate(ctx, skip); err != nil {
		return fmt.Errorf("bootstrap validation failed: %w", err)
	}

	return nil
}
