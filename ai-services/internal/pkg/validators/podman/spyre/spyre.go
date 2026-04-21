package spyre

import (
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/spyre"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type SpyreRule struct{}

func NewSpyreRule() *SpyreRule {
	return &SpyreRule{}
}

func (r *SpyreRule) Name() string {
	return "spyre"
}

func (r *SpyreRule) Description() string {
	return "Validates IBM Spyre Accelerator configuration."
}

// Verify performs comprehensive Spyre validation.
func (r *SpyreRule) Verify() error {
	logger.Infoln("Running comprehensive Spyre validation...", logger.VerbosityLevelDebug)

	// Check if Spyre cards are present
	if !spyre.IsApplicable() {
		return fmt.Errorf("IBM Spyre Accelerator is not attached to the LPAR")
	}

	numCards := spyre.GetNumberOfSpyreCards()
	logger.Infof("Detected %d Spyre card(s)", numCards)

	// Run all validation checks
	checks := spyre.RunChecks()

	// Collect validation errors
	var validationErrors []string
	for _, check := range checks {
		if !check.GetStatus() {
			validationErrors = append(validationErrors, check.String())
		}
	}

	if len(validationErrors) > 0 {
		return fmt.Errorf("spyre configuration validation failed:\n%s",
			strings.Join(validationErrors, "\n"))
	}

	logger.Infoln("✓ All Spyre configuration checks passed", logger.VerbosityLevelDebug)

	return nil
}

func (r *SpyreRule) Message() string {
	return "IBM Spyre Accelerator is properly configured"
}

func (r *SpyreRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *SpyreRule) Hint() string {
	return "IBM Spyre Accelerator hardware is required and must be properly configured. Run 'ai-services bootstrap configure' to fix configuration issues."
}

// Made with Bob
