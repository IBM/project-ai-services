package podman

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/deploy"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure"
	configureutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure/utils"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// existingCertSentinel is a non-empty placeholder passed into sslCertContent/sslKeyContent
// when the cert secret already exists from a previous run. It triggers the template
// volume-mount guard without embedding real cert bytes (the secret is already stored).
const (
	existingCertSentinel = "_existing_"

	// catalogAPIRouteID is the Caddy route ID for the catalog backend API,
	// matching the subdomain registered by RegisterCatalogRoutes.
	catalogAPIRouteID = "catalog-api"
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
	secretExists, err := deployCtx.Runtime.SecretExists(ctx, catalogconstants.CatalogSecretName)
	if err != nil {
		return fmt.Errorf("failed to check catalog secret: %w", err)
	}

	passwordHash, adminPassword, err := configureutils.CollectAdminPassword(secretExists)
	if err != nil {
		return err
	}

	caddyCtx, useExistingCert, catalogAlreadyDeployed, err := executeCatalogDeployment(ctx, deployCtx, opts, passwordHash)
	if err != nil {
		return err
	}

	// Load SSL certificates into Caddy when user-supplied paths are given.
	// When useExistingCert is true the preserved caddy-data PVC already has an
	// autosave that includes the load_files entry — Caddy resumes with the custom
	// cert active, so no Admin API call is needed and triggering an unnecessary
	// PATCH reload is avoided.
	if !useExistingCert {
		if err := caddyCtx.LoadSSLCertificates(ctx, opts.SSLCertPath, opts.SSLKeyPath); err != nil {
			return err
		}
	}

	// When reusing the preserved cert (--skip-cleanup re-configure), the
	// autosave routes carry the correct domain from the previous install.
	// Query the live catalog-api route to recover it before RegisterCatalogRoutes
	// sets DOMAIN_SUFFIX — otherwise ComputeDomainSuffix would have fallen back
	// to the host IP and routes/WaitForTLSReady would use the wrong domain.
	if useExistingCert {
		if err := recoverDomainFromCaddy(ctx, caddyCtx); err != nil {
			logger.Debugf("Could not recover domain from Caddy autosave routes: %v\n", err)
		}
	}

	// When the catalog pod was already deployed before this run, --skip-local-worker
	// was validated against LOCAL_WORKER in validateReconfigureParameters above.
	// Only run the DB-based ValidateSkipLocalWorker when the pod was freshly
	// deployed this run (catalogAlreadyDeployed=false) and a previous secret exists,
	// meaning a prior partial run created the secret but not the pod.
	isReinstall := secretExists && !catalogAlreadyDeployed

	return handlePostDeployment(ctx, caddyCtx, deployCtx, opts, adminPassword, isReinstall)
}

// executeCatalogDeployment deploys (or validates) the catalog pods and returns
// the Caddy context, a flag indicating whether the preserved cert secret should
// be loaded into Caddy without host-path validation, and whether the catalog
// was already deployed before this run (catalogAlreadyDeployed).
func executeCatalogDeployment(ctx context.Context, deployCtx *deploy.DeployContext, opts catalogUtils.PodmanConfigureOptions, passwordHash string) (*caddy.Context, bool, bool, error) {
	logger.Debugln("started configuring catalog service...")

	s := spinner.New("Configuring catalog service...")
	s.Start(ctx)

	logger.Debugln("setting up caddy context...")

	// Setup Caddy context with domain configuration and Caddyfile generation
	caddyCtx, err := setupCaddyContext(deployCtx, opts, s)
	if err != nil {
		s.Fail("failed while setting up caddy context")

		return nil, false, false, err
	}

	logger.Debugln("checking for existing resources...")

	// Check existing deployment status
	isDeployed, existingResources, err := deployCtx.CheckStatus(ctx)
	if err != nil {
		s.Fail("failed to check existing resources")

		return nil, false, false, fmt.Errorf("failed to check existing resources: %w", err)
	}

	// useExistingCert is true when the cert secret was preserved by a previous
	// --skip-cleanup uninstall and no cert paths were supplied on this run.
	// CheckStatus already queried SecretExists for CatalogCertSecretName and
	// appended it to existingResources when found, so slices.Contains is the
	// single source of truth — no extra runtime call needed.
	useExistingCert := opts.SSLCertPath == "" && opts.SSLKeyPath == "" &&
		slices.Contains(existingResources, catalogconstants.CatalogCertSecretName)

	if !isDeployed {
		if err = loadCatalogParamValues(deployCtx, passwordHash, opts.SSLCertPath, opts.SSLKeyPath, useExistingCert, opts.HttpsPort, opts.WorkerGatewayPort, opts.SkipLocalWorker); err != nil {
			s.Fail("failed to load param values")

			return nil, false, false, err
		}

		// Execute pod templates
		if err := deployCtx.ExecutePodLayers(ctx, opts.BaseDir, caddyCtx, existingResources); err != nil {
			s.Fail("failed to deploy catalog pod")

			return nil, false, false, err
		}

		s.Stop("Catalog service deployed successfully")
		logger.Infoln("-------")
	} else {
		s.Stop("Catalog service already deployed")
		logger.Infof("Existing resources: %v\n", existingResources)
		// Validate domain, HTTPS port, base directory, --skip-local-worker, and certificates haven't changed
		if err := validateReconfigureParameters(ctx, deployCtx.Runtime, &opts, caddyCtx); err != nil {
			s.Fail("validation failed during reconfigure")

			return nil, false, false, fmt.Errorf("reconfigure validation failed: %w", err)
		}
	}

	return caddyCtx, useExistingCert, isDeployed, nil
}

// handlePostDeployment handles route registration, login verification,
// local worker join, and next steps display after catalog deployment.
func handlePostDeployment(ctx context.Context, caddyCtx *caddy.Context, deployCtx *deploy.DeployContext, opts catalogUtils.PodmanConfigureOptions, adminPassword string, isReinstall bool) error {
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

	// Wait for Caddy's TLS layer to be ready for the catalog API before attempting login.
	// Route registration triggers a Caddy config reload that briefly interrupts
	// TLS, causing "tls: internal error" if login is attempted immediately.
	catalogAPIURL := routeURLs[catalogconstants.CatalogAPIRouteKey]
	if err := caddy.WaitForTLSReady(ctx, catalogAPIURL); err != nil {
		return fmt.Errorf("catalog API TLS readiness check failed: %w", err)
	}

	// Login to the catalog API — this both verifies the admin password and gives
	// us a client to reuse for local worker registration without a second login.
	catalogClient, err := configure.LoginToCatalog(ctx, catalogAPIURL, adminPassword)
	if err != nil {
		return fmt.Errorf("admin password verification failed: %w", err)
	}

	// Validate --skip-local-worker has not changed since the original install.
	if err := configure.ValidateSkipLocalWorker(ctx, catalogClient, isReinstall, opts.SkipLocalWorker); err != nil {
		return err
	}

	if !opts.SkipLocalWorker {
		if err := JoinAsLocalWorker(ctx, deployCtx.Runtime, opts, catalogClient); err != nil {
			return fmt.Errorf("worker join failed: %v", err)
		}
	}

	// Print next steps with proxy route information
	if err := helpers.PrintNextStepsWithProxy(ctx, deployCtx.TemplateProvider, deployCtx.Runtime, catalogconstants.CatalogAppName, catalogconstants.CatalogAppTemplate, routeURLs); err != nil {
		// do not want to fail the overall configure if we cannot print next steps
		logger.Infof("failed to display next steps: %v\n", err)
	}

	return nil
}

// recoverDomainFromCaddy queries the live Caddy Admin API for the catalog-api
// route and extracts its domain suffix, then updates caddyCtx. This is used
// after --skip-cleanup re-configure when the autosave already has the correct
// domain from the previous install but ComputeDomainSuffix had no cert paths
// to work with and would have fallen back to the host IP.
// Errors are non-fatal — the caller logs and continues with the host-IP domain.
func recoverDomainFromCaddy(ctx context.Context, caddyCtx *caddy.Context) error {
	proxyManager, err := caddyCtx.CreateProxyManager(ctx)
	if err != nil {
		return err
	}

	route, err := proxyManager.GetRouteByID(ctx, catalogAPIRouteID)
	if err != nil {
		return err
	}

	// route.Domain is the full host (e.g. "catalog-api.powervm-spyre-pok.cis.ibm.net").
	// Strip the "catalog-api." prefix to get the domain suffix.
	const prefix = catalogAPIRouteID + "."
	if strings.HasPrefix(route.Domain, prefix) {
		caddyCtx.SetDomainSuffix(strings.TrimPrefix(route.Domain, prefix))
	}

	return nil
}

// loadCatalogParamValues prepares all necessary data for deployment.
func loadCatalogParamValues(deployCtx *deploy.DeployContext, passwordHash, sslCertPath, sslKeyPath string, useExistingCert bool, httpsPort, workerGatewayPort int, skipLocalWorker bool) error {
	logger.Debugln("loading catalog service param values...")

	// Generate argument parameters
	argParams, err := generateArgParams(passwordHash, sslCertPath, sslKeyPath, useExistingCert, httpsPort, workerGatewayPort, skipLocalWorker)
	if err != nil {
		return fmt.Errorf("failed to generate arg params: %w", err)
	}

	// Prepare values with configure-specific configuration
	if err := deployCtx.PrepareValues(argParams); err != nil {
		return fmt.Errorf("failed to load values: %w", err)
	}

	return nil
}

// generateArgParams generates the argument parameters for template rendering.
func generateArgParams(passwordHash, sslCertPath, sslKeyPath string, useExistingCert bool, httpsPort, workerGatewayPort int, skipLocalWorker bool) (map[string]string, error) {
	dbPassword, err := utils.GenerateRandomPassword()
	if err != nil {
		return nil, fmt.Errorf("failed to generate database password: %w", err)
	}

	authFileBase64, err := utils.ReadAuthFileBase64()
	if err != nil {
		return nil, err
	}

	// Determine the podman URI
	// Strip unix:// prefix from podmanURI for hostPath volume mount
	// The CONTAINER_HOST env var needs the full URI, but the hostPath needs just the file path
	podmanURI, err := utils.ResolvePodmanURI()
	if err != nil {
		return nil, fmt.Errorf("failed to generate podman uri: %w", err)
	}

	caddyFileContent, err := caddy.GetCaddyFileContent()
	if err != nil {
		return nil, fmt.Errorf("failed to generate caddy file: %w", err)
	}

	sslCertContent, sslKeyContent, err := readSSLContents(sslCertPath, sslKeyPath, useExistingCert)
	if err != nil {
		return nil, err
	}

	argParams := make(map[string]string)
	argParams[configure.ArgParamAdminPasswordHash] = passwordHash
	argParams[configure.ArgParamRuntime] = "podman"
	argParams[configure.ArgParamPodmanAuthFileContent] = authFileBase64
	argParams[configure.ArgParamPodmanURI] = strings.TrimPrefix(podmanURI, "unix://")
	argParams[configure.ArgParamDBPassword] = dbPassword
	argParams[constants.ArgParamCaddyHTTPSPort] = fmt.Sprintf("%d", httpsPort)
	argParams[configure.ArgParamWorkerGatewayPort] = fmt.Sprintf("%d", workerGatewayPort)
	argParams[configure.ArgParamLocalWorker] = strconv.FormatBool(!skipLocalWorker)
	argParams[constants.ArgParamCaddyFileContent] = utils.IndentString(caddyFileContent, utils.CaddyFileIndent)
	argParams[constants.ArgParamSSLCertFileContent] = utils.IndentString(sslCertContent, utils.CertContentIndent)
	argParams[constants.ArgParamSSLKeyFileContent] = utils.IndentString(sslKeyContent, utils.CertContentIndent)

	return argParams, nil
}

// readSSLContents reads and returns the PEM contents of the cert and key files.
// When useExistingCert is true the caddy pod template needs a non-empty
// sslCertContent value to render the /etc/secret/ssl volume mount; the cert
// secret is already preserved and skipped by existingResources, so the sentinel
// bytes never land in the secret.
// Returns empty strings when no cert is configured.
func readSSLContents(certPath, keyPath string, useExistingCert bool) (string, string, error) {
	if useExistingCert {
		return existingCertSentinel, existingCertSentinel, nil
	}

	if certPath == "" || keyPath == "" {
		return "", "", nil
	}

	certBytes, keyBytes, _, err := utils.ReadAndParseCertificates(certPath, keyPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to load ssl certs: %w", err)
	}

	return string(certBytes), string(keyBytes), nil
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
