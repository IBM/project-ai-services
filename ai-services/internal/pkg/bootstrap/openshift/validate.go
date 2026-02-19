package openshift

import (
	"context"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// OCPBootstrap implements the Bootstrap interface for OpenShift.
type OCPBootstrap struct {
	Helper *OCPHelper
}

func NewOCPBootstrap() (*OCPBootstrap, error) {
	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create openshift client: %w", err)
	}

	return &OCPBootstrap{
		Helper: &OCPHelper{
			client: client,
		},
	}, nil
}

// Validate validates OpenShift environment.
func (o *OCPBootstrap) Validate(skip map[string]bool) error {
	ctx := context.Background()
	var validationErrors []error

	checks := []struct {
		name  string
		check func(context.Context) error
		hint  string
	}{
		{
			"Secondary Scheduler Operator installed",
			o.validateSecondaryScheduler,
			"Install Secondary Scheduler Operator from OperatorHub",
		},
		{
			"Cert-Manager Operator installed",
			o.validateCertManager,
			"Install Cert-Manager Operator from OperatorHub",
		},
		{
			"Service Mesh 3 Operator installed",
			o.validateServiceMesh,
			"Install OpenShift Service Mesh Operator from OperatorHub",
		},
		{
			"Node Feature Discovery Operator installed",
			o.validateNodeFeatureDiscovery,
			"Install Node Feature Discovery Operator from OperatorHub",
		},
		{
			"RHOAI Operator installed and ready",
			o.validateRHOAI,
			"Install RHOAI Operator or check CSV phase",
		},
	}

	for _, check := range checks {
		if err := check.check(ctx); err != nil {
			fmt.Println(check.name)
			fmt.Printf("HINT: %s\n", check.hint)
			validationErrors = append(validationErrors, err)
		} else {
			style := lipgloss.NewStyle().Foreground(lipgloss.Color("#32BD27"))
			fmt.Printf("%s %s\n", style.Render("✓"), check.name)
		}
	}

	if len(validationErrors) > 0 {
		return fmt.Errorf("bootstrap validation failed: %d validation(s) failed", len(validationErrors))
	}

	logger.Infoln("All validations passed")

	return nil
}

func (o *OCPBootstrap) validateSecondaryScheduler(ctx context.Context) error {
	return o.Helper.ValidateOperator(ctx, "secondaryscheduler")
}

func (o *OCPBootstrap) validateCertManager(ctx context.Context) error {
	return o.Helper.ValidateOperator(ctx, "cert-manager")
}

func (o *OCPBootstrap) validateServiceMesh(ctx context.Context) error {
	return o.Helper.ValidateOperator(ctx, "servicemesh")
}

func (o *OCPBootstrap) validateNodeFeatureDiscovery(ctx context.Context) error {
	return o.Helper.ValidateOperator(ctx, "nfd")
}

func (o *OCPBootstrap) validateRHOAI(ctx context.Context) error {
	return o.Helper.ValidateOperator(ctx, "rhods-operator")
}

func (o *OCPBootstrap) Type() types.RuntimeType {
	return types.RuntimeTypeOpenShift
}

func (o *OCPBootstrap) Configure() error {
	logger.Infoln("OpenShift environment is pre-configured. Skipping configuration.")

	return nil
}
