package deployment

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/repository/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	podmanRuntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// DeploymentExecutor orchestrates the complete deployment process.
// It uses the DeploymentPlanner to create a plan and then executes it
// using the appropriate runtime-specific deployer.
type DeploymentExecutor struct {
	planner         *DeploymentPlanner
	catalogProvider *catalog.CatalogProvider
	appRepo         repository.ApplicationRepository
	serviceRepo     repository.ServiceRepository
	componentRepo   repository.ComponentRepository
}

// NewDeploymentExecutor creates a new DeploymentExecutor instance.
func NewDeploymentExecutor(
	catalogProvider *catalog.CatalogProvider,
	appRepo repository.ApplicationRepository,
	serviceRepo repository.ServiceRepository,
	componentRepo repository.ComponentRepository,
) *DeploymentExecutor {
	return &DeploymentExecutor{
		planner:         NewDeploymentPlanner(catalogProvider, componentRepo),
		catalogProvider: catalogProvider,
		appRepo:         appRepo,
		serviceRepo:     serviceRepo,
		componentRepo:   componentRepo,
	}
}

// ExecuteWithPlan executes deployment using an existing plan.
// This is used when the plan has already been created and database records inserted.
func (e *DeploymentExecutor) ExecuteWithPlan(
	ctx context.Context,
	plan *DeploymentPlan,
	req apimodels.CreateApplicationRequest,
	runtimeType types.RuntimeType,
) error {
	// Execute deployment based on runtime type using the provided plan
	if err := e.executeDeployment(ctx, plan, req, runtimeType); err != nil {
		return fmt.Errorf("failed to execute deployment: %w", err)
	}

	return nil
}

// executeDeployment executes the deployment plan using the appropriate runtime deployer.
func (e *DeploymentExecutor) executeDeployment(
	ctx context.Context,
	plan *DeploymentPlan,
	req apimodels.CreateApplicationRequest,
	runtimeType types.RuntimeType,
) error {
	switch runtimeType {
	case types.RuntimeTypePodman:
		return e.executePodmanDeployment(ctx, plan, req)
	case types.RuntimeTypeOpenShift:
		return fmt.Errorf("OpenShift deployment not yet implemented")
	default:
		return fmt.Errorf("unsupported runtime type: %s", runtimeType)
	}
}

// executePodmanDeployment executes deployment for Podman runtime.
// Handles both architecture and standalone service deployments.
func (e *DeploymentExecutor) executePodmanDeployment(
	ctx context.Context,
	plan *DeploymentPlan,
	req apimodels.CreateApplicationRequest,
) error {
	// Initialize Podman runtime client
	rt, err := podmanRuntime.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to initialize Podman runtime: %w", err)
	}

	// Create podman deployer
	deployer := podman.NewPodmanDeployer(
		rt,
		e.catalogProvider,
		e.appRepo,
		e.serviceRepo,
		e.componentRepo,
	)

	// Execute deployment - handles both architectures and standalone services
	return deployer.ExecuteDeployment(ctx, plan, req)
}

// Made with Bob
