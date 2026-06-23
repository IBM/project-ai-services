package podman

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/deploy"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	defaultPasswordIterations = 100000
)

// DeployCatalog deploys the catalog service using the assets/catalog template for podman runtime.
func DeployCatalog(ctx context.Context, opts catalogUtils.PodmanConfigureOptions) error {
	// Create deployment context without argParams for status check
	deployCtx, err := deploy.NewDeployContext()
	if err != nil {
		return err
	}

	// Collect and hash password
	// If secret exist passwordHash will be empty
	passwordHash, err := collectAndHashPassword(deployCtx.Runtime)
	if err != nil {
		return err
	}

	caddyCtx, err := executeCatalogDeployment(ctx, deployCtx, opts, passwordHash)
	if err != nil {
		return err
	}

	// Load SSL certificates if provided
	if err := caddyCtx.LoadSSLCertificates(opts.BaseDir, opts.SSLCertPath, opts.SSLKeyPath); err != nil {
		return err
	}

	return handlePostDeployment(caddyCtx, deployCtx)
}

func executeCatalogDeployment(ctx context.Context, deployCtx *deploy.DeployContext, opts catalogUtils.PodmanConfigureOptions, passwordHash string) (*caddy.Context, error) {
	logger.Debugln("started configuring catalog service...")

	s := spinner.New("Configuring catalog service...")
	s.Start(ctx)

	logger.Debugln("setting up caddy context...")

	// Setup Caddy context with domain configuration and Caddyfile generation
	caddyCtx, err := setupCaddyContext(deployCtx, opts, s)
	if err != nil {
		s.Fail("failed while setting up caddy context")

		return nil, err
	}

	logger.Debugln("checking for existing resources...")

	// Check existing deployment status
	isDeployed, existingResources, err := deployCtx.CheckStatus()
	if err != nil {
		s.Fail("failed to check existing resources")

		return nil, fmt.Errorf("failed to check existing resources: %w", err)
	}

	if !isDeployed {
		// Prepare deployment with domain suffix computation and create Caddy context
		err = loadCatalogParamValues(deployCtx, passwordHash, opts.HttpsPort)
		if err != nil {
			s.Fail("failed to load param values")

			return nil, err
		}

		// Execute pod templates
		if err := deployCtx.ExecutePodLayers(opts.BaseDir, caddyCtx, existingResources); err != nil {
			s.Fail("failed to deploy catalog pod")

			return nil, err
		}

		s.Stop("Catalog service deployed successfully")
		logger.Infoln("-------")
	} else {
		s.Stop("Catalog service already deployed")
		logger.Infof("Existing resources: %v\n", existingResources)
		// Validate domain, HTTPS port, base directory, and certificates haven't changed
		if err := validateReconfigureParameters(deployCtx.Runtime, &opts, caddyCtx.GetDomainSuffix()); err != nil {
			s.Fail("validation failed during reconfigure")

			return nil, fmt.Errorf("reconfigure validation failed: %w", err)
		}
	}

	return caddyCtx, nil
}

// handlePostDeployment handles route registration and next steps display after catalog deployment.
func handlePostDeployment(caddyCtx *caddy.Context, deployCtx *deploy.DeployContext) error {
	logger.Debugln("handling post deployment steps...")

	// Extract route infos from deployment context
	routeInfos, err := deployCtx.ExtractRouteInfos()
	if err != nil {
		return fmt.Errorf("failed to extract route infos: %w", err)
	}

	// Register routes with Caddy and get the registered route domains
	routeDomains, err := caddy.RegisterCatalogRoutes(deployCtx.Runtime, caddyCtx, routeInfos)
	if err != nil {
		return fmt.Errorf("route registration failed: %w", err)
	}

	// Get Caddy HTTPS port for next steps display
	httpsPort, err := caddyCtx.GetHTTPSPort(deployCtx.Runtime)
	if err != nil {
		return fmt.Errorf("failed to get Caddy HTTPS port: %w", err)
	}

	// Print next steps with proxy route information
	if err := helpers.PrintNextStepsWithProxy(deployCtx.TemplateProvider, deployCtx.Runtime, catalogconstants.CatalogAppName, catalogconstants.CatalogAppTemplate, routeDomains, httpsPort); err != nil {
		// do not want to fail the overall configure if we cannot print next steps
		logger.Infof("failed to display next steps: %v\n", err)
	}

	return nil
}

// prepareCatalogDeployment prepares all necessary data for deployment including domain suffix computation.
func loadCatalogParamValues(deployCtx *deploy.DeployContext, passwordHash string, httpsPort int) error {
	logger.Debugln("loading catalog service param values...")

	// Generate argument parameters
	argParams, err := generateArgParams(passwordHash, httpsPort)
	if err != nil {
		return fmt.Errorf("failed to generate arg params: %w", err)
	}

	// Prepare values with configure-specific configuration
	err = deployCtx.PrepareValues(argParams)
	if err != nil {
		return fmt.Errorf("failed to load values: %w", err)
	}

	return nil
}

// generateArgParams generates the argument parameters for template rendering.
func generateArgParams(passwordHash string, httpsPort int) (map[string]string, error) {
	// Generate database password
	dbPassword, err := utils.GenerateRandomPassword()
	if err != nil {
		return nil, fmt.Errorf("failed to generate database password: %w", err)
	}

	// Determine auth file path
	// Read and encode auth file content for secret
	// If auth file doesn't exist, use empty content
	authFilePath, err := utils.GetAuthFilePath()
	if err != nil {
		return nil, fmt.Errorf("failed to get auth file path: %w", err)
	}

	authFileContent, err := os.ReadFile(authFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Auth file doesn't exist - user hasn't logged into podman
			logger.Warningln("Podman auth file not found. Deployment may fail since deployment may require pulling images.")
			logger.Warningln("If you need to update registry credentials later, you can use the '--reset-podman-auth' flag after running 'podman login'.")
			authFileContent = []byte{}
		} else {
			return nil, fmt.Errorf("failed to read auth file from %s: %w", authFilePath, err)
		}
	}

	// Base64 encode the auth file content for Kubernetes secret
	authFileBase64 := base64.StdEncoding.EncodeToString(authFileContent)

	// Determine the podman URI
	// Strip unix:// prefix from podmanURI for hostPath volume mount
	// The CONTAINER_HOST env var needs the full URI, but the hostPath needs just the file path
	podmanURI, err := utils.ResolvePodmanURI()
	if err != nil {
		return nil, fmt.Errorf("failed to generate podman uri: %w", err)
	}
	podmanSocketPath := strings.TrimPrefix(podmanURI, "unix://")

	// Set configure-specific values
	argParams := make(map[string]string)
	argParams["backend.adminPasswordHash"] = passwordHash
	argParams["backend.runtime"] = "podman"
	argParams["backend.podman.authFileContent"] = authFileBase64
	argParams["backend.podman.uri"] = podmanSocketPath
	argParams["db.password"] = dbPassword
	argParams["caddy.httpsPort"] = fmt.Sprintf("%d", httpsPort)

	return argParams, nil
}

// setupCaddyContext sets up the Caddy context with domain configuration and Caddyfile generation.
// This function:
// 1. Gets the Caddy pod name from deployment context templates
// 2. Computes domain configuration (cert domain extraction + domain suffix resolution)
// 3. Creates Caddy context with pod name and domain suffix
// 4. Generates and writes Caddyfile.
func setupCaddyContext(deployCtx *deploy.DeployContext, opts catalogUtils.PodmanConfigureOptions, s *spinner.Spinner) (*caddy.Context, error) {
	// Get Caddy pod name from deployment context (templates)
	caddyPodName, err := deployCtx.GetCaddyPodName()
	if err != nil {
		s.Fail("failed to find Caddy pod name")

		return nil, fmt.Errorf("failed to find Caddy pod name: %w", err)
	}

	// Compute domain configuration (cert domain extraction + domain suffix resolution)
	domainSuffix, err := caddy.ComputeDomainConfig(opts.SSLCertPath, opts.SSLKeyPath, opts.DomainName)
	if err != nil {
		s.Fail("failed to calculate domain")

		return nil, err
	}

	logger.Debugf("Using domain suffix: %s\n", domainSuffix)

	// Create Caddy context with pod name and domain suffix (NO template dependencies)
	caddyCtx := caddy.NewContext(caddyPodName, domainSuffix)

	// Generate and write Caddyfile before deploying
	if err := caddy.GenerateCaddyfile(opts.BaseDir); err != nil {
		s.Fail("failed to generate Caddyfile")

		return nil, fmt.Errorf("failed to generate Caddyfile: %w", err)
	}

	return caddyCtx, nil
}

// Made with Bob
