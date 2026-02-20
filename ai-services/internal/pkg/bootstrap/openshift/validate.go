package openshift

import (
	"context"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	SecondarySchedulerOperator = "secondaryscheduleroperator"
	CertManagerOperator        = "cert-manager-operator"
	ServiceMeshOperator        = "servicemeshoperator3"
	NFDOperator                = "nfd"
	RHOAIOperator              = "rhods-operator"
)

// Validate validates OpenShift environment.
func (o *OpenshiftBootstrap) Validate(skip map[string]bool) error {
	ctx := context.Background()
	var validationErrors []error

	checks := []struct {
		name     string
		operator string
		hint     string
	}{
		{
			"Secondary Scheduler Operator installed",
			SecondarySchedulerOperator,
			"Install Secondary Scheduler Operator from OperatorHub",
		},
		{
			"Cert-Manager Operator installed",
			CertManagerOperator,
			"Install Cert-Manager Operator from OperatorHub",
		},
		{
			"Service Mesh 3 Operator installed",
			ServiceMeshOperator,
			"Install OpenShift Service Mesh Operator from OperatorHub",
		},
		{
			"Node Feature Discovery Operator installed",
			NFDOperator,
			"Install Node Feature Discovery Operator from OperatorHub",
		},
		{
			"RHOAI Operator installed and ready",
			RHOAIOperator,
			"Install RHOAI Operator or check CSV phase",
		},
	}

	for _, check := range checks {
		if err := ValidateOperator(ctx, check.operator); err != nil {
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
