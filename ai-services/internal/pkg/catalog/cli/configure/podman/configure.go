package podman

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/deploy"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	caddyFileIndent   = 10
	certContentIndent = 4
)

// DeployCatalog deploys the catalog service using the assets/catalog template for podman runtime.
func DeployCatalog(ctx context.Context, opts catalogUtils.PodmanConfigureOptions) error {
	// Create deployment context without argParams for status check
	deployCtx, err := deploy.NewDeployContext()
	if err != nil {
		return err
	}

	// Collect and hash password.
	// If secret exists passwordHash will be empty.
	passwordHash, err := catalogUtils.CollectAndHashPassword(ctx, deployCtx.Runtime)
	if err != nil {
		return err
	}

	caddyCtx, err := executeCatalogDeployment(ctx, deployCtx, opts, passwordHash)
	if err != nil {
		return err
	}

	// Load SSL certificates if provided
	if err := caddyCtx.LoadSSLCertificates(ctx, opts.BaseDir, opts.SSLCertPath, opts.SSLKeyPath); err != nil {
		return err
	}

	return handlePostDeployment(ctx, caddyCtx, deployCtx)
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
	isDeployed, existingResources, err := deployCtx.CheckStatus(ctx)
	if err != nil {
		s.Fail("failed to check existing resources")

		return nil, fmt.Errorf("failed to check existing resources: %w", err)
	}

	if !isDeployed {
		// Prepare deployment with domain suffix computation and create Caddy context
		err = loadCatalogParamValues(deployCtx, passwordHash, opts.SSLCertPath, opts.SSLKeyPath, opts.HttpsPort, opts.WorkerGatewayPort)
		if err != nil {
			s.Fail("failed to load param values")

			return nil, err
		}

		// Execute pod templates
		if err := deployCtx.ExecutePodLayers(ctx, opts.BaseDir, caddyCtx, existingResources); err != nil {
			s.Fail("failed to deploy catalog pod")

			return nil, err
		}

		s.Stop("Catalog service deployed successfully")
		logger.Infoln("-------")
	} else {
		s.Stop("Catalog service already deployed")
		logger.Infof("Existing resources: %v\n", existingResources)
		// Validate domain, HTTPS port, base directory, and certificates haven't changed
		if err := validateReconfigureParameters(ctx, deployCtx.Runtime, &opts, caddyCtx); err != nil {
			s.Fail("validation failed during reconfigure")

			return nil, fmt.Errorf("reconfigure validation failed: %w", err)
		}
	}

	return caddyCtx, nil
}

// handlePostDeployment handles route registration and next steps display after catalog deployment.
func handlePostDeployment(ctx context.Context, caddyCtx *caddy.Context, deployCtx *deploy.DeployContext) error {
	logger.Debugln("handling post deployment steps...")

	// Extract route infos from deployment context
	routeInfos, err := deployCtx.ExtractRouteInfos()
	if err != nil {
		return fmt.Errorf("failed to extract route infos: %w", err)
	}

	// Register routes with Caddy and get the registered route URLs
	routeURLs, err := caddy.RegisterCatalogRoutes(ctx, deployCtx.Runtime, caddyCtx, routeInfos)
	if err != nil {
		return fmt.Errorf("route registration failed: %w", err)
	}

	// Print next steps with proxy route information
	if err := helpers.PrintNextStepsWithProxy(ctx, deployCtx.TemplateProvider, deployCtx.Runtime, catalogconstants.CatalogAppName, catalogconstants.CatalogAppTemplate, routeURLs); err != nil {
		// do not want to fail the overall configure if we cannot print next steps
		logger.Infof("failed to display next steps: %v\n", err)
	}

	return nil
}

// loadCatalogParamValues prepares all necessary data for deployment including domain suffix computation.
func loadCatalogParamValues(deployCtx *deploy.DeployContext, passwordHash, sslCertPath, sslKeyPath string, httpsPort, workerGatewayPort int) error {
	logger.Debugln("loading catalog service param values...")

	// Generate argument parameters
	argParams, err := generateArgParams(passwordHash, sslCertPath, sslKeyPath, httpsPort, workerGatewayPort)
	if err != nil {
		return fmt.Errorf("failed to generate arg params: %w", err)
	}
	// Fill caddy config

	// Prepare values with configure-specific configuration
	err = deployCtx.PrepareValues(argParams)
	if err != nil {
		return fmt.Errorf("failed to load values: %w", err)
	}

	return nil
}

// generateArgParams generates the argument parameters for template rendering.
func generateArgParams(passwordHash, sslCertPath, sslKeyPath string, httpsPort, workerGatewayPort int) (map[string]string, error) {
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
			authFileContent = []byte("{}")
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

	// Caddy configuration
	caddyFileContent, err := caddy.GetCaddyFileContent()
	if err != nil {
		return nil, fmt.Errorf("failed to generate caddy file: %w", err)
	}
	var sslCertContent, sslKeyContent string
	if sslCertPath != "" && sslKeyPath != "" {
		certbyte, keyBytes, _, err := utils.ReadAndParseCertificates(sslCertPath, sslKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load ssl certs: %w", err)
		}
		sslCertContent = string(certbyte)
		sslKeyContent = string(keyBytes)
	}

	// Set configure-specific values
	argParams := make(map[string]string)
	argParams[configure.ArgParamAdminPasswordHash] = passwordHash
	argParams[configure.ArgParamRuntime] = "podman"
	argParams[configure.ArgParamPodmanAuthFileContent] = authFileBase64
	argParams[configure.ArgParamPodmanURI] = podmanSocketPath
	argParams[configure.ArgParamDBPassword] = dbPassword
	argParams[configure.ArgParamCaddyHTTPSPort] = fmt.Sprintf("%d", httpsPort)
	argParams[configure.ArgParamWorkerGatewayPort] = fmt.Sprintf("%d", workerGatewayPort)
	argParams[configure.ArgParamCaddyFileContent] = utils.IndentString(caddyFileContent, caddyFileIndent)
	argParams[configure.ArgParamSSLCertFileContent] = utils.IndentString(sslCertContent, certContentIndent)
	argParams[configure.ArgParamSSLKeyFileContent] = utils.IndentString(sslKeyContent, certContentIndent)

	return argParams, nil
}

// setupCaddyContext sets up the Caddy context with domain configuration and Caddyfile generation.
// This function:
// 1. Gets the Caddy pod name from deployment context templates
// 2. Computes domain configuration (cert domain extraction + domain suffix resolution)
// 3. Creates Caddy context with pod name and domain suffix.
func setupCaddyContext(deployCtx *deploy.DeployContext, opts catalogUtils.PodmanConfigureOptions, s *spinner.Spinner) (*caddy.Context, error) {
	// Get Caddy pod name from deployment context (templates)
	caddyPodName, err := deployCtx.GetCaddyPodName()
	if err != nil {
		s.Fail("failed to find Caddy pod name")

		return nil, fmt.Errorf("failed to find Caddy pod name: %w", err)
	}

	// Compute domain configuration (cert domain extraction + domain suffix resolution)
	domainSuffix, err := utils.ComputeDomainSuffix(opts.SSLCertPath, opts.SSLKeyPath, opts.DomainName)
	if err != nil {
		s.Fail("failed to calculate domain")

		return nil, err
	}

	logger.Debugf("Using domain suffix: %s\n", domainSuffix)

	// Create Caddy context with pod name and domain suffix (NO template dependencies)
	caddyCtx := caddy.NewContext(caddyPodName, domainSuffix)

	return caddyCtx, nil
}

// Made with Bob
