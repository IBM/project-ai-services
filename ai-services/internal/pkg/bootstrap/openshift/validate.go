package openshift

import (
	"context"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	SecondarySchedulerOperator = "secondaryscheduler"
	CertManagerOperator        = "cert-manager"
	ServiceMeshOperator        = "servicemesh"
	NFDOperator                = "nfd"
	RHOAIOperator              = "rhods-operator"
)

// Validate validates OpenShift environment.
func (o *OpenshiftBootstrap) Validate(skip map[string]bool) error {
	ctx := context.Background()
	var validationErrors []error

	checks := []struct {
		name  string
		check func(context.Context) error
		hint  string
	}{
		{
			"Secondary Scheduler Operator installed",
			validateSecondaryScheduler,
			"Install Secondary Scheduler Operator from OperatorHub",
		},
		{
			"Cert-Manager Operator installed",
			validateCertManager,
			"Install Cert-Manager Operator from OperatorHub",
		},
		{
			"Service Mesh 3 Operator installed",
			validateServiceMesh,
			"Install OpenShift Service Mesh Operator from OperatorHub",
		},
		{
			"Node Feature Discovery Operator installed",
			validateNodeFeatureDiscovery,
			"Install Node Feature Discovery Operator from OperatorHub",
		},
		{
			"RHOAI Operator installed and ready",
			validateRHOAI,
			"Install RHOAI Operator or check CSV phase",
		},
	}

	for _, check := range checks {
		// Optional: Add skip logic here if you want to support the 'skip' map
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

func validateSecondaryScheduler(ctx context.Context) error {
	return ValidateOperator(ctx, SecondarySchedulerOperator)
}

func validateCertManager(ctx context.Context) error {
	return ValidateOperator(ctx, CertManagerOperator)
}

func validateServiceMesh(ctx context.Context) error {
	return ValidateOperator(ctx, ServiceMeshOperator)
}

func validateNodeFeatureDiscovery(ctx context.Context) error {
	return ValidateOperator(ctx, NFDOperator)
}

func validateRHOAI(ctx context.Context) error {
	return ValidateOperator(ctx, RHOAIOperator)
}
