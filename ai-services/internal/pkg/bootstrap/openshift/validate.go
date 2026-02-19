package openshift

import (
	"context"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

func NewOCPBootstrap() (*OCPHelper, error) {
	// 1. Actually create the client
	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize openshift client: %w", err)
	}

	// 2. Inject the client into the helper
	return &OCPHelper{
		client: client,
	}, nil
}

// Validate validates OpenShift environment.
func (o *OCPHelper) Validate(skip map[string]bool) error {
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
			logger.Infoln(check.name)
			logger.Infof("HINT: %s\n", check.hint)
			validationErrors = append(validationErrors, err)
		} else {
			style := lipgloss.NewStyle().Foreground(lipgloss.Color("#32BD27"))
			logger.Infoln(fmt.Sprintf("%s %s", style.Render("✓"), check.name))
		}
	}

	if len(validationErrors) > 0 {
		return fmt.Errorf("bootstrap validation failed: %d validation(s) failed", len(validationErrors))
	}

	logger.Infoln("All validations passed")

	return nil
}

func (o *OCPHelper) validateSecondaryScheduler(ctx context.Context) error {
	return o.ValidateOperator(ctx, "secondaryscheduler")
}

func (o *OCPHelper) validateCertManager(ctx context.Context) error {
	return o.ValidateOperator(ctx, "cert-manager")
}

func (o *OCPHelper) validateServiceMesh(ctx context.Context) error {
	return o.ValidateOperator(ctx, "servicemesh")
}

func (o *OCPHelper) validateNodeFeatureDiscovery(ctx context.Context) error {
	return o.ValidateOperator(ctx, "nfd")
}

func (o *OCPHelper) validateRHOAI(ctx context.Context) error {
	return o.ValidateOperator(ctx, "rhods-operator")
}

func (o *OCPHelper) Type() types.RuntimeType {
	return types.RuntimeTypeOpenShift
}

func (o *OCPHelper) Configure() error {
	logger.Infoln("OpenShift environment is pre-configured. Skipping configuration.")

	return nil
}
