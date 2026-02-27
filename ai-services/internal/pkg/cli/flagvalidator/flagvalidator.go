package flagvalidator

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// FlagScope defines the scope of a flag (which runtime it applies to).
type FlagScope string

const (
	// FlagScopeCommon indicates the flag is valid for all runtimes.
	FlagScopeCommon FlagScope = "common"
	// FlagScopePodman indicates the flag is only valid for Podman runtime.
	FlagScopePodman FlagScope = "podman"
	// FlagScopeOpenShift indicates the flag is only valid for OpenShift runtime.
	FlagScopeOpenShift FlagScope = "openshift"
)

// FlagDefinition defines a flag with its scope and validation function.
type FlagDefinition struct {
	// Name is the flag name (without dashes).
	Name string
	// Scope defines which runtime(s) this flag applies to.
	Scope FlagScope
	// ValidateFunc is an optional custom validation function.
	// If nil, only scope validation is performed.
	ValidateFunc func(cmd *cobra.Command) error
}

// FlagValidator validates flags based on runtime type and custom validation rules.
type FlagValidator struct {
	runtimeType runtimeTypes.RuntimeType
	flags       []FlagDefinition
}

// NewFlagValidator creates a new flag validator for the given runtime type.
func NewFlagValidator(runtimeType runtimeTypes.RuntimeType) *FlagValidator {
	return &FlagValidator{
		runtimeType: runtimeType,
		flags:       []FlagDefinition{},
	}
}

// RegisterFlag registers a flag definition for validation.
func (v *FlagValidator) RegisterFlag(flag FlagDefinition) {
	v.flags = append(v.flags, flag)
}

// RegisterFlags registers multiple flag definitions for validation.
func (v *FlagValidator) RegisterFlags(flags []FlagDefinition) {
	v.flags = append(v.flags, flags...)
}

// Validate validates all registered flags against the current runtime.
// It checks:
// 1. Scope validation - ensures flags are only used with compatible runtimes.
// 2. Custom validation - runs any custom validation functions defined for the flags.
func (v *FlagValidator) Validate(cmd *cobra.Command) error {
	var errors []string

	for _, flag := range v.flags {
		// Check if flag was actually set by the user
		if !cmd.Flags().Changed(flag.Name) {
			continue
		}

		// Validate scope
		if err := v.validateScope(flag); err != nil {
			errors = append(errors, err.Error())

			continue
		}

		// Run custom validation if provided
		if flag.ValidateFunc != nil {
			if err := flag.ValidateFunc(cmd); err != nil {
				errors = append(errors, fmt.Sprintf("flag --%s: %v", flag.Name, err))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("flag validation failed:\n  - %s", strings.Join(errors, "\n  - "))
	}

	return nil
}

// validateScope checks if a flag's scope is compatible with the current runtime.
func (v *FlagValidator) validateScope(flag FlagDefinition) error {
	// Common flags are always valid
	if flag.Scope == FlagScopeCommon {
		return nil
	}

	// Check runtime-specific flags
	switch v.runtimeType {
	case runtimeTypes.RuntimeTypePodman:
		if flag.Scope != FlagScopePodman {
			return fmt.Errorf(
				"flag '--%s' is only supported for %s runtime (current runtime: %s)\nUse --runtime flag to set the correct runtime or use -h for more info",
				flag.Name,
				flag.Scope,
				v.runtimeType,
			)
		}
	case runtimeTypes.RuntimeTypeOpenShift:
		if flag.Scope != FlagScopeOpenShift {
			return fmt.Errorf(
				"flag '--%s' is only supported for %s runtime (current runtime: %s)\nUse --runtime flag to set the correct runtime or use -h for more info",
				flag.Name,
				flag.Scope,
				v.runtimeType,
			)
		}
	default:
		return fmt.Errorf("unknown runtime type: %s", v.runtimeType)
	}

	return nil
}

// ValidateFunc is a helper type for creating validation functions.
type ValidateFunc func(cmd *cobra.Command) error

// FlagValidatorBuilder provides a fluent interface for building flag validators.
type FlagValidatorBuilder struct {
	validator *FlagValidator
}

// NewFlagValidatorBuilder creates a new builder for the given runtime type.
func NewFlagValidatorBuilder(runtimeType runtimeTypes.RuntimeType) *FlagValidatorBuilder {
	return &FlagValidatorBuilder{
		validator: NewFlagValidator(runtimeType),
	}
}

// AddCommonFlag adds a common flag (valid for all runtimes).
func (b *FlagValidatorBuilder) AddCommonFlag(name string, validateFunc ValidateFunc) *FlagValidatorBuilder {
	b.validator.RegisterFlag(FlagDefinition{
		Name:         name,
		Scope:        FlagScopeCommon,
		ValidateFunc: validateFunc,
	})

	return b
}

// AddPodmanFlag adds a Podman-specific flag.
func (b *FlagValidatorBuilder) AddPodmanFlag(name string, validateFunc ValidateFunc) *FlagValidatorBuilder {
	b.validator.RegisterFlag(FlagDefinition{
		Name:         name,
		Scope:        FlagScopePodman,
		ValidateFunc: validateFunc,
	})

	return b
}

// AddOpenShiftFlag adds an OpenShift-specific flag.
func (b *FlagValidatorBuilder) AddOpenShiftFlag(name string, validateFunc ValidateFunc) *FlagValidatorBuilder {
	b.validator.RegisterFlag(FlagDefinition{
		Name:         name,
		Scope:        FlagScopeOpenShift,
		ValidateFunc: validateFunc,
	})

	return b
}

// Build returns the configured flag validator.
func (b *FlagValidatorBuilder) Build() *FlagValidator {
	return b.validator
}

// Made with Bob
