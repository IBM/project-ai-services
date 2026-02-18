package openshift

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

type OCPBootstrap struct {
	Helper *OCPHelper
}

func NewOCPBootstrap() (*OCPBootstrap, error) {
	return &OCPBootstrap{Helper: nil}, nil
}

func (o *OCPBootstrap) getHelper() (*OCPHelper, error) {
	if o.Helper == nil {
		helper, err := NewOCPHelper()
		if err != nil {
			return nil, fmt.Errorf("failed to initialize OpenShift helper: %w", err)
		}
		o.Helper = helper
	}
	return o.Helper, nil
}

func (o *OCPBootstrap) Validate(skip map[string]bool) error {
	_, err := o.getHelper()
	if err != nil {
		return err
	}

	ctx := context.Background()
	var validationErrors []error

	checks := []struct {
		name  string
		check func(context.Context) error
		hint  string
	}{
		{
			"Spyre Cluster Policy is ready",
			o.validateSpyrePolicy,
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

func (o *OCPBootstrap) validateSpyrePolicy(ctx context.Context) error {
	return o.Helper.WaitForSpyreClusterPolicyReady(
		ctx,
		"spyreclusterpolicy",
		5*time.Minute,
	)
}

func (o *OCPBootstrap) Type() types.RuntimeType {
	return types.RuntimeTypeOpenShift
}

func (o *OCPBootstrap) Configure() error {
	logger.Infoln("OpenShift environment is pre-configured. Skipping configuration.")
	return nil
}
