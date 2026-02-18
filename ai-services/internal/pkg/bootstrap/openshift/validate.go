package openshift

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	validatorsOpenshift "github.com/project-ai-services/ai-services/internal/pkg/validators/openshift"
)

const (
	spyreTimeout = 10 * time.Minute
)

// OCPBootstrap implements the Bootstrap interface for OpenShift.
type OCPBootstrap struct{}

func NewOCPBootstrap() (*OCPBootstrap, error) {
	return &OCPBootstrap{}, nil
}

// Validate validates OpenShift environment, specifically Spyre Cluster Policy.
func (o *OCPBootstrap) Validate(skip map[string]bool) error {
	ctx := context.Background()
	var validationErrors []error

	checks := []struct {
		name  string
		check func(context.Context) error
		hint  string
	}{
		{
			"Spyre Cluster Policy is ready",
			func(ctx context.Context) error {
				return validatorsOpenshift.ValidateSpyreClusterPolicy(ctx, spyreTimeout)
			},
			"Spyre cluster policy must be in ready state. Run 'oc get spyreclusterpolicy' to check status.",
		},
	}

	for _, check := range checks {
		if skip[check.name] {
			logger.Warningf("%s (skipped)", check.name)

			continue
		}

		if err := check.check(ctx); err != nil {
			fmt.Printf("%s\n", check.name)
			fmt.Printf("HINT: %s\n", check.hint)

			validationErrors = append(validationErrors, err)
		} else {
			style := lipgloss.NewStyle().Foreground(lipgloss.Color("#32BD27"))
			fmt.Printf("%s %s\n", style.Render("✓"), check.name)
		}
	}

	if len(validationErrors) > 0 {
		return fmt.Errorf("%d validation(s) failed", len(validationErrors))
	}

	logger.Infoln("All validations passed")

	return nil
}

// Type returns the runtime type.
func (o *OCPBootstrap) Type() types.RuntimeType {
	return types.RuntimeTypeOpenShift
}

// Configure is a no-op for OpenShift as it's pre-configured.
func (o *OCPBootstrap) Configure() error {
	logger.Infoln("OpenShift environment is pre-configured. Skipping configuration.")

	return nil
}
