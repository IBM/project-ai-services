package podman

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"text/template"

	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/project-ai-services/ai-services/assets"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	clipodman "github.com/project-ai-services/ai-services/internal/pkg/cli/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/specs"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	catalogAppTemplate    = "catalog"
	dirPerm               = 0o755
	filePerm              = 0o644
	kindSecret            = "Secret"
	caddyCertsDirName     = "certs"
	caddyContainerDataDir = "/data/caddy"
	domainSuffixEnvVar    = "DOMAIN_SUFFIX"
	httpsPortEnvVar       = "CADDY_HTTPS_PORT"
)

// ErrCertificatesAlreadyLoaded is a sentinel error indicating certificates are already loaded and should be skipped.
var ErrCertificatesAlreadyLoaded = errors.New("certificates already loaded")

// catalogDeploymentContext holds cached values during catalog deployment to avoid redundant lookups.
type catalogDeploymentContext struct {
	// Pod identification
	caddyPodName string

	// Admin API access
	caddyAdminPort         string // Dynamic port assigned by Podman
	caddyContainerAdminURL string // http://<podName>:2019 (for container use in templates)
	caddyHostAdminURL      string // http://localhost:<port> (for host VM use in post-deployment)

	// Network configuration
	domainSuffix string
}

// getCaddyPodName retrieves the Caddy pod name, using cache if available.
func (c *catalogDeploymentContext) getCaddyPodName(tp templates.Template, appTemplateName string, argParams map[string]string) (string, error) {
	if c.caddyPodName != "" {
		return c.caddyPodName, nil
	}

	name, err := findCaddyPodNameFromTemplates(tp, appTemplateName, argParams)
	if err != nil {
		return "", err
	}

	c.caddyPodName = name

	return name, nil
}

// getCaddyHostAdminURL retrieves the Caddy admin URL for host VM use, using cache if available.
func (c *catalogDeploymentContext) getCaddyHostAdminURL(rt *podman.PodmanClient, tp templates.Template, appTemplateName string, argParams map[string]string) (string, error) {
	if c.caddyHostAdminURL != "" {
		return c.caddyHostAdminURL, nil
	}

	// Get pod name (will use cache if available)
	podName, err := c.getCaddyPodName(tp, appTemplateName, argParams)
	if err != nil {
		return "", err
	}

	// Get admin port
	adminPort, err := proxy.GetCaddyAdminPort(rt, podName)
	if err != nil {
		return "", err
	}

	c.caddyAdminPort = adminPort
	c.caddyHostAdminURL = fmt.Sprintf("http://localhost:%s", adminPort)

	return c.caddyHostAdminURL, nil
}

// DeployCatalog deploys the catalog service using the assets/catalog template for podman runtime.
func DeployCatalog(ctx context.Context, podmanURI, authFilePath, passwordHash, baseDir string, argParams map[string]string, domainName string, sslCertPath, sslKeyPath string, httpsPort int, resetCert bool) error {
	s := spinner.New("Deploying catalog service...")
	s.Start(ctx)

	// Initialize deployment context for caching
	deployCtx := &catalogDeploymentContext{}

	// Initialize and validate (don't set HTTPS port yet for reconfigure case)
	rt, tp, appMetadata, tmpls, argParams, err := initializeCatalogDeployment(argParams, httpsPort, s)
	if err != nil {
		return err
	}

	// Extract certificate domain early (needed for both fresh deploy and reconfigure)
	certDomain, err := extractCertDomainIfProvided(sslCertPath, sslKeyPath, s)
	if err != nil {
		return err
	}

	// Check existing deployment status
	isDeployed, existingResources, err := checkCatalogStatus(rt, tp, tmpls, argParams)
	if err != nil {
		s.Fail("failed to check existing resources")

		return fmt.Errorf("failed to check existing resources: %w", err)
	}

	if !isDeployed {
		// Fresh deployment path
		return handleFreshDeployment(deployCtx, rt, tp, appMetadata, tmpls, podmanURI, authFilePath, passwordHash, baseDir, domainName, certDomain, sslCertPath, sslKeyPath, argParams, existingResources, s, resetCert)
	}

	// Reconfigure path - pass the httpsPort parameter to handle default vs user-provided
	return handleReconfigure(deployCtx, rt, tp, baseDir, certDomain, domainName, sslCertPath, sslKeyPath, argParams, httpsPort, s, resetCert)
}

// handleFreshDeployment handles the fresh deployment path for catalog service.
func handleFreshDeployment(
	deployCtx *catalogDeploymentContext,
	rt *podman.PodmanClient,
	tp templates.Template,
	appMetadata *templates.AppMetadata,
	tmpls map[string]*template.Template,
	podmanURI, authFilePath, passwordHash, baseDir, domainName, certDomain, sslCertPath, sslKeyPath string,
	argParams map[string]string,
	existingResources []string,
	s *spinner.Spinner,
	resetCert bool,
) error {
	// Prepare deployment configuration
	values, err := prepareCatalogDeployment(deployCtx, tp, podmanURI, authFilePath, passwordHash, baseDir, domainName, certDomain, argParams, s)
	if err != nil {
		return err
	}

	// Execute pod templates
	if err := executePodLayers(rt, tp, tmpls, appMetadata, values, baseDir, deployCtx.caddyContainerAdminURL, deployCtx.domainSuffix, argParams, s, existingResources); err != nil {
		return err
	}

	s.Stop("Catalog service deployed successfully")
	logger.Infoln("-------")

	// Handle post-deployment operations (cert loading and route registration)
	return handlePostDeployment(deployCtx, rt, tp, baseDir, sslCertPath, sslKeyPath, argParams, false, resetCert)
}

// handleReconfigure handles the reconfigure path for catalog service.
func handleReconfigure(
	deployCtx *catalogDeploymentContext,
	rt *podman.PodmanClient,
	tp templates.Template,
	baseDir, certDomain, domainName, sslCertPath, sslKeyPath string,
	argParams map[string]string,
	httpsPort int,
	s *spinner.Spinner,
	resetCert bool,
) error {
	s.Stop("Catalog service already deployed, validating configuration...")

	// Validate reconfigure parameters and get existing domain
	// Pass httpsPort to determine if user provided it or if it's the default
	existingDomain, err := validateReconfigureParameters(rt, certDomain, domainName, argParams, httpsPort)
	if err != nil {
		return err
	}

	// Use existing domain for operations
	deployCtx.domainSuffix = existingDomain

	// Handle post-deployment operations (cert loading and route registration)
	return handlePostDeployment(deployCtx, rt, tp, baseDir, sslCertPath, sslKeyPath, argParams, true, resetCert)
}

// initializeCatalogDeployment handles initialization and validation steps.
func initializeCatalogDeployment(argParams map[string]string, httpsPort int, s *spinner.Spinner) (
	*podman.PodmanClient,
	templates.Template,
	*templates.AppMetadata,
	map[string]*template.Template,
	map[string]string,
	error,
) {
	// Initialize runtime
	rt, err := podman.NewPodmanClient()
	if err != nil {
		s.Fail("failed to initialize podman client")

		return nil, nil, nil, nil, nil, fmt.Errorf("failed to initialize podman client: %w", err)
	}

	// Load template provider and metadata
	tp, appMetadata, tmpls, err := loadCatalogTemplates(s)
	if err != nil {
		s.Fail("failed to load catalog templates")

		return nil, nil, nil, nil, nil, fmt.Errorf("failed to load catalog templates: %w", err)
	}

	// Set httpsPort in argParams
	if argParams == nil {
		argParams = make(map[string]string)
	}
	argParams["caddy.httpsPort"] = fmt.Sprintf("%d", httpsPort)

	return rt, tp, appMetadata, tmpls, argParams, nil
}

// handlePostDeployment handles certificate loading, route registration and next steps display after catalog deployment.
func handlePostDeployment(deployCtx *catalogDeploymentContext, rt *podman.PodmanClient, tp templates.Template, baseDir, sslCertPath, sslKeyPath string, argParams map[string]string, isReconfigure bool, resetCert bool) error {
	// Get Caddy admin URL from cache (will fetch and cache if not already cached)
	adminURL, err := deployCtx.getCaddyHostAdminURL(rt, tp, catalogAppTemplate, argParams)
	if err != nil {
		return fmt.Errorf("failed to get Caddy admin URL: %w", err)
	}

	// Load SSL certificates if provided (allow cert updates during reconfigure only with --reset-cert)
	if err := loadSSLCertificatesIfProvided(deployCtx, rt, tp, baseDir, sslCertPath, sslKeyPath, argParams, isReconfigure, resetCert); err != nil {
		return fmt.Errorf("failed to load SSL certificates: %w", err)
	}

	// Get Caddy pod name from cache
	caddyPodName := deployCtx.caddyPodName

	// Register routes with Caddy and get the registered route domains
	routeDomains, err := registerCatalogRoutes(rt, tp, catalogAppTemplate, argParams, deployCtx.domainSuffix, adminURL)
	if err != nil {
		return fmt.Errorf("route registration failed: %w", err)
	}

	// Get Caddy HTTPS port for next steps display
	httpsPort, err := getCaddyHTTPSPort(rt, caddyPodName)
	if err != nil {
		return fmt.Errorf("failed to get Caddy HTTPS port: %w", err)
	}

	// Print next steps with proxy route information
	if err := helpers.PrintNextStepsWithProxy(tp, rt, catalogconstants.CatalogAppName, catalogAppTemplate, routeDomains, httpsPort); err != nil {
		// do not want to fail the overall configure if we cannot print next steps
		logger.Infof("failed to display next steps: %v\n", err)
	}

	return nil
}

// extractCertDomainIfProvided extracts domain from certificate if SSL cert/key are provided.
func extractCertDomainIfProvided(sslCertPath, sslKeyPath string, s *spinner.Spinner) (string, error) {
	if sslCertPath == "" || sslKeyPath == "" {
		return "", nil
	}

	certDomain, err := utils.ExtractDomainFromCertificate(sslCertPath)
	if err != nil {
		s.Fail("failed to extract domain from certificate")

		return "", fmt.Errorf("failed to extract domain from certificate: %w", err)
	}

	logger.Infof("Extracted domain from certificate: %s\n", certDomain, logger.VerbosityLevelDebug)

	return certDomain, nil
}

// prepareCatalogDeployment prepares all necessary data for deployment including domain suffix computation.
func prepareCatalogDeployment(deployCtx *catalogDeploymentContext, tp templates.Template, podmanURI, authFilePath, passwordHash, baseDir, domainName, certDomain string, argParams map[string]string, s *spinner.Spinner) (map[string]any, error) {
	// Compute domain suffix using priority: certDomain > customDomain > hostIP.nip.io
	domainSuffix, err := computeDomainSuffix(certDomain, domainName)
	if err != nil {
		s.Fail("failed to compute domain suffix")

		return nil, fmt.Errorf("failed to compute domain suffix: %w", err)
	}
	deployCtx.domainSuffix = domainSuffix

	logger.Infof("Using domain suffix: %s\n", domainSuffix, logger.VerbosityLevelDebug)

	// Get Caddy pod name from cache (will be cached for subsequent calls)
	caddyPodName, err := deployCtx.getCaddyPodName(tp, catalogAppTemplate, argParams)
	if err != nil {
		s.Fail("failed to find Caddy pod name")

		return nil, fmt.Errorf("failed to find Caddy pod name: %w", err)
	}

	// Build admin URL for container (uses internal port 2019) and cache it
	deployCtx.caddyContainerAdminURL = fmt.Sprintf("http://%s:2019", caddyPodName)

	// Prepare values with configure-specific configuration
	values, err := prepareCatalogValues(tp, podmanURI, authFilePath, passwordHash, argParams)
	if err != nil {
		s.Fail("failed to load values")

		return nil, fmt.Errorf("failed to load values: %w", err)
	}

	// Generate and write Caddyfile before deploying
	if err := generateCaddyfile(baseDir); err != nil {
		s.Fail("failed to generate Caddyfile")

		return nil, fmt.Errorf("failed to generate Caddyfile: %w", err)
	}

	return values, nil
}

// loadSSLCertificatesIfProvided stages user-provided certificates for the Caddy pod and updates TLS config via Admin API.
// During reconfigure, it validates certificate changes and blocks unauthorized updates unless --reset-cert is provided.
func loadSSLCertificatesIfProvided(deployCtx *catalogDeploymentContext, rt *podman.PodmanClient, tp templates.Template, baseDir, sslCertPath, sslKeyPath string, argParams map[string]string, isReconfigure bool, resetCert bool) error {
	if sslCertPath == "" || sslKeyPath == "" {
		return nil
	}

	// Define staged certificate paths
	stagedCertPath := filepath.Join(baseDir, "common", "caddy", caddyCertsDirName, "tls.crt")
	stagedKeyPath := filepath.Join(baseDir, "common", "caddy", caddyCertsDirName, "tls.key")

	// Handle reconfigure scenario
	if isReconfigure && !resetCert {
		// Only validate if --reset-cert is NOT provided
		if err := validateCertificateUpdate(sslCertPath, sslKeyPath, stagedCertPath, stagedKeyPath); err != nil {
			// Check if this is the special "already loaded" error
			if errors.Is(err, ErrCertificatesAlreadyLoaded) {
				return nil // Skip loading, not an actual error
			}
			return err
		}
	}

	// Fresh deployment: proceed with certificate loading
	// Get Caddy admin URL from cache (will fetch and cache if not already cached)
	adminURL, err := deployCtx.getCaddyHostAdminURL(rt, tp, catalogAppTemplate, argParams)
	if err != nil {
		return fmt.Errorf("failed to get Caddy admin URL: %w", err)
	}

	logger.Infof("Loading certificates\n")

	if err := stageCertificatesForCaddy(baseDir, sslCertPath, sslKeyPath); err != nil {
		return fmt.Errorf("failed to stage certificates for Caddy: %w", err)
	}

	if err := utils.LoadUserCertificates(
		stagedCertPath,
		stagedKeyPath,
		filepath.Join(caddyContainerDataDir, caddyCertsDirName, "tls.crt"),
		filepath.Join(caddyContainerDataDir, caddyCertsDirName, "tls.key"),
		adminURL,
	); err != nil {
		return fmt.Errorf("failed to load certificates via Admin API: %w", err)
	}

	return nil
}

func stageCertificatesForCaddy(baseDir, sslCertPath, sslKeyPath string) error {
	caddyDataDir := filepath.Join(baseDir, "common", "caddy")
	certDir := filepath.Join(caddyDataDir, caddyCertsDirName)
	if err := os.MkdirAll(certDir, dirPerm); err != nil {
		return fmt.Errorf("failed to create Caddy cert directory: %w", err)
	}

	stagedCertPath := filepath.Join(certDir, "tls.crt")
	stagedKeyPath := filepath.Join(certDir, "tls.key")

	certBytes, err := os.ReadFile(sslCertPath)
	if err != nil {
		return fmt.Errorf("failed to read certificate file: %w", err)
	}

	keyBytes, err := os.ReadFile(sslKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read key file: %w", err)
	}

	if err := os.WriteFile(stagedCertPath, certBytes, filePerm); err != nil {
		return fmt.Errorf("failed to write staged certificate file: %w", err)
	}

	if err := os.WriteFile(stagedKeyPath, keyBytes, filePerm); err != nil {
		return fmt.Errorf("failed to write staged key file: %w", err)
	}

	return nil
}

func checkCatalogStatus(rt *podman.PodmanClient, tp templates.Template, tmpls map[string]*template.Template, argParams map[string]string) (bool, []string, error) {
	catalogSecrets, err := collectSecretNames(tp, tmpls, argParams)
	if err != nil {
		return false, nil, err
	}

	existingResources, err := helpers.CheckExistingResourcesForApplication(rt, catalogconstants.CatalogAppName, catalogSecrets)
	if err != nil {
		return false, nil, err
	}

	return len(existingResources) == len(tmpls), existingResources, nil
}

// loadCatalogTemplates loads the catalog template provider, metadata, and templates.
func loadCatalogTemplates(s *spinner.Spinner) (templates.Template, *templates.AppMetadata, map[string]*template.Template, error) {
	tp := templates.NewEmbedTemplateProvider(&assets.CatalogFS, "")

	// Load metadata from catalog/podman
	var appMetadata templates.AppMetadata
	if err := tp.LoadMetadata(catalogAppTemplate, true, &appMetadata); err != nil {
		s.Fail("failed to load catalog metadata")

		return nil, nil, nil, fmt.Errorf("failed to load catalog metadata: %w", err)
	}

	// Load all templates from catalog
	tmpls, err := tp.LoadAllTemplates(catalogAppTemplate)
	if err != nil {
		s.Fail("failed to load catalog templates")

		return nil, nil, nil, fmt.Errorf("failed to load catalog templates: %w", err)
	}

	return tp, &appMetadata, tmpls, nil
}

// prepareCatalogValues prepares the values map with configure-specific configuration.
func prepareCatalogValues(tp templates.Template, podmanURI, authFilePath, passwordHash string, argParams map[string]string) (map[string]any, error) {
	if argParams == nil {
		argParams = make(map[string]string)
	}

	// Generate database password
	dbPassword, err := utils.GenerateRandomPassword()
	if err != nil {
		return nil, fmt.Errorf("failed to generate database password: %w", err)
	}

	// Read and encode auth file content for secret
	authFileContent, err := os.ReadFile(authFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read auth file from %s: %w", authFilePath, err)
	}

	// Base64 encode the auth file content for Kubernetes secret
	authFileBase64 := base64.StdEncoding.EncodeToString(authFileContent)

	// Strip unix:// prefix from podmanURI for hostPath volume mount
	// The CONTAINER_HOST env var needs the full URI, but the hostPath needs just the file path
	podmanSocketPath := strings.TrimPrefix(podmanURI, "unix://")

	// Set configure-specific values
	argParams["backend.adminPasswordHash"] = passwordHash
	argParams["backend.runtime"] = "podman"
	argParams["backend.podman.authFileContent"] = authFileBase64
	argParams["backend.podman.uri"] = podmanSocketPath
	argParams["db.password"] = dbPassword

	// Load values from catalog
	return tp.LoadValues(catalogAppTemplate, nil, argParams)
}

// executePodLayers executes all pod template layers.
func executePodLayers(rt *podman.PodmanClient, tp templates.Template, tmpls map[string]*template.Template,
	appMetadata *templates.AppMetadata, values map[string]any, baseDir, caddyAdminURL, domainSuffix string, argParams map[string]string,
	s *spinner.Spinner, existingResources []string) error {
	for i, layer := range appMetadata.PodTemplateExecutions {
		logger.Infof("\n Executing Layer %d/%d: %v\n", i+1, len(appMetadata.PodTemplateExecutions), layer)
		logger.Infoln("-------")

		if err := executeLayer(rt, tp, tmpls, layer, appMetadata.Version, values, baseDir, caddyAdminURL, domainSuffix, argParams, i, existingResources); err != nil {
			s.Fail("failed to deploy catalog pod")

			return err
		}

		logger.Infof("Layer %d completed\n", i+1)
	}

	return nil
}

// executeLayer executes a single layer of pod templates.
func executeLayer(rt *podman.PodmanClient, tp templates.Template, tmpls map[string]*template.Template,
	layer []string, version string, values map[string]any, baseDir, caddyAdminURL, domainSuffix string, argParams map[string]string,
	layerIndex int, existingResources []string) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(layer))

	// for each layer, fetch all the pod Template Names and do the pod deploy
	for _, podTemplateName := range layer {
		wg.Add(1)
		go func(t string) {
			defer wg.Done()
			if err := executePodTemplate(rt, tp, tmpls, t, catalogAppTemplate, catalogconstants.CatalogAppName, values, version, nil, baseDir, caddyAdminURL, domainSuffix, argParams, existingResources); err != nil {
				errCh <- err
			}
		}(podTemplateName)
	}

	wg.Wait()
	close(errCh)

	// collect all errors for this layer
	errs := make([]error, 0, len(layer))
	for e := range errCh {
		errs = append(errs, fmt.Errorf("layer %d: %w", layerIndex+1, e))
	}

	// If an error exist for a given layer, then return (do not process further layers)
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// executePodTemplate executes a single pod template.
func executePodTemplate(rt *podman.PodmanClient, tp templates.Template, tmpls map[string]*template.Template,
	podTemplateName, appTemplateName, appName string, values map[string]any, version string,
	valuesFiles []string, baseDir, caddyAdminURL, domainSuffix string, argParams map[string]string, existingResources []string) error {
	logger.Infof("Processing template: %s\n", podTemplateName)

	// Fetch pod spec
	podSpec, err := tp.LoadPodTemplateWithValues(appTemplateName, podTemplateName, appName, valuesFiles, argParams)
	if err != nil {
		return fmt.Errorf("failed to load pod template: %w", err)
	}

	// Prepare template parameters
	params := map[string]any{
		"AppName":         appName,
		"AppTemplateName": appTemplateName,
		"Version":         version,
		"BaseDir":         baseDir,
		"CaddyAdminURL":   caddyAdminURL,
		"DomainSuffix":    domainSuffix,
		"Values":          values,
		"env":             map[string]map[string]string{},
	}

	// filter out resources
	if slices.Contains(existingResources, podSpec.Name) {
		logger.Infof("%s: Skipping resource deploy as '%s' it already exists", podTemplateName, podSpec.Name)

		return nil
	}

	// Get the template
	podTemplate := tmpls[podTemplateName]

	// Render template
	var rendered bytes.Buffer
	if err := podTemplate.Execute(&rendered, params); err != nil {
		return fmt.Errorf("failed to render pod template: %w", err)
	}

	// Deploy the pod with readiness checks
	reader := bytes.NewReader(rendered.Bytes())
	podDeployOptions := clipodman.ConstructPodDeployOptions(specs.FetchPodAnnotations(*podSpec))

	if err := clipodman.DeployPodAndReadinessCheck(rt, podSpec, podTemplateName, reader, podDeployOptions); err != nil {
		return fmt.Errorf("failed to deploy pod: %w", err)
	}

	return nil
}

// generateCaddyfile copies the static Caddyfile to the caddy directory.
func generateCaddyfile(baseDir string) error {
	// Read the Caddyfile template
	caddyfileContent, err := assets.CatalogFS.ReadFile("catalog/podman/Caddyfile.tmpl")
	if err != nil {
		return fmt.Errorf("failed to read Caddyfile template: %w", err)
	}

	// Parse the Caddyfile as a template
	tmpl, err := template.New("Caddyfile.tmpl").Parse(string(caddyfileContent))
	if err != nil {
		return fmt.Errorf("failed to parse Caddyfile template: %w", err)
	}

	// Prepare template data with the server name constant
	templateData := map[string]any{
		"CaddyServerName": constants.CaddyServerName,
	}

	// Execute the template
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, templateData); err != nil {
		return fmt.Errorf("failed to execute Caddyfile template: %w", err)
	}

	// Ensure directory exists and write Caddyfile
	caddyDir := filepath.Join(baseDir, "common", "caddy")
	if err := os.MkdirAll(caddyDir, dirPerm); err != nil {
		return fmt.Errorf("failed to create caddy directory: %w", err)
	}

	caddyfilePath := filepath.Join(caddyDir, "Caddyfile")
	if err := os.WriteFile(caddyfilePath, rendered.Bytes(), filePerm); err != nil {
		return fmt.Errorf("failed to write Caddyfile: %w", err)
	}

	return nil
}

func collectSecretNames(tp templates.Template, tmpls map[string]*template.Template, argParams map[string]string) ([]string, error) {
	secretNames := make([]string, 0)

	for podTemplateName := range tmpls {
		podSpec, err := tp.LoadPodTemplateWithValues(catalogAppTemplate, podTemplateName, catalogconstants.CatalogAppName, nil, argParams)
		if err != nil {
			return nil, fmt.Errorf("failed to load pod template %s: %w", podTemplateName, err)
		}

		if podSpec.Kind == kindSecret {
			secretNames = append(secretNames, podSpec.Name)
		}
	}

	return secretNames, nil
}

// TemplateRouteInfo holds route information extracted from a template.
type TemplateRouteInfo struct {
	PodName          string
	RoutesAnnotation string
}

// processPodTemplates loads all templates and processes each pod spec with the provided callback.
// This helper function eliminates duplicate template loading and iteration logic.
func processPodTemplates(tp templates.Template, appTemplateName string, argParams map[string]string,
	processor func(templateName string, podSpec *models.PodSpec) error) error {
	// Load all templates once
	tmpls, err := tp.LoadAllTemplates(appTemplateName)
	if err != nil {
		return fmt.Errorf("failed to load templates: %w", err)
	}

	// Iterate through templates and process each pod spec
	for templateName := range tmpls {
		podSpec, err := tp.LoadPodTemplateWithValues(appTemplateName, templateName,
			catalogconstants.CatalogAppName, nil, argParams)
		if err != nil {
			return fmt.Errorf("failed to load template %s: %w", templateName, err)
		}

		if err := processor(templateName, podSpec); err != nil {
			return err
		}
	}

	return nil
}

// extractAllRoutesFromTemplates extracts routes annotations from all templates that have them.
// Returns a slice of TemplateRouteInfo containing pod name and routes for each template.
func extractAllRoutesFromTemplates(tp templates.Template, appTemplateName string, argParams map[string]string) ([]TemplateRouteInfo, error) {
	var routeInfos []TemplateRouteInfo

	err := processPodTemplates(tp, appTemplateName, argParams, func(templateName string, podSpec *models.PodSpec) error {
		// Check if this template has the routes annotation
		if podSpec.Annotations != nil {
			if routes, ok := podSpec.Annotations[constants.PodRoutesAnnotationKey]; ok {
				routeInfos = append(routeInfos, TemplateRouteInfo{
					PodName:          podSpec.Name,
					RoutesAnnotation: routes,
				})
			}
		}

		return nil
	})

	return routeInfos, err
}

// findCaddyPodNameFromTemplates finds the Caddy pod name by looking for the pod with component=proxy label in templates.
func findCaddyPodNameFromTemplates(tp templates.Template, appTemplateName string, argParams map[string]string) (string, error) {
	var caddyPodName string

	err := processPodTemplates(tp, appTemplateName, argParams, func(templateName string, podSpec *models.PodSpec) error {
		// Check if this is the Caddy pod (component=proxy label)
		if podSpec.Labels != nil {
			if component, ok := podSpec.Labels["ai-services.io/component"]; ok && component == "proxy" {
				caddyPodName = podSpec.Name
				// Return a sentinel error to stop iteration early
				return fmt.Errorf("found")
			}
		}

		return nil
	})

	// Check if we found the Caddy pod (err will be "found" sentinel)
	if caddyPodName != "" {
		return caddyPodName, nil
	}

	// If err is not nil and we didn't find the pod, it's a real error
	if err != nil && err.Error() != "found" {
		return "", err
	}

	return "", fmt.Errorf("no Caddy pod found with component=proxy label in templates")
}

// registerCatalogRoutes registers routes with Caddy and returns route domains.
func registerCatalogRoutes(rt *podman.PodmanClient, tp templates.Template, appTemplateName string, argParams map[string]string, domainSuffix, adminURL string) (map[string]string, error) {
	// Extract routes from all templates
	routeInfos, err := extractAllRoutesFromTemplates(tp, appTemplateName, argParams)
	if err != nil {
		return nil, fmt.Errorf("failed to extract routes from templates: %w", err)
	}

	if len(routeInfos) == 0 {
		logger.Infof("No templates found with routes annotation, skipping route registration\n")

		return nil, nil
	}

	// Create proxy manager once before registering routes
	proxyManager := proxy.NewCaddyManager(adminURL, constants.CaddyServerName)

	// Build route domains map
	routeDomains := make(map[string]string)

	// Register routes for each template that has them
	var registrationErrors []error
	for _, info := range routeInfos {
		logger.Infof("Registering routes for pod: %s\n", info.PodName, logger.VerbosityLevelDebug)

		// Register routes and get the built routes back
		routes, err := proxy.RegisterRoutesForAppAndReturn(rt, catalogconstants.CatalogAppName, proxyManager, info.RoutesAnnotation, domainSuffix, info.PodName)
		if err != nil {
			registrationErrors = append(registrationErrors, fmt.Errorf("pod %s: %w", info.PodName, err))

			continue
		}

		addRoutesToDomainMap(routes, routeDomains)
	}

	// Return error if any routes failed to register
	if len(registrationErrors) > 0 {
		return nil, fmt.Errorf("failed to register routes for %d pod(s): %w", len(registrationErrors), errors.Join(registrationErrors...))
	}

	logger.Infof("Successfully registered routes for %d pod(s)\n", len(routeInfos))

	return routeDomains, nil
}

// computeDomainSuffix computes the domain suffix using priority: certDomain > customDomain > hostIP.nip.io.
func computeDomainSuffix(certDomain, customDomain string) (string, error) {
	if certDomain != "" {
		return certDomain, nil
	}
	if customDomain != "" {
		return customDomain, nil
	}

	// Only get hostIP if we actually need it (when both cert and custom domains are absent)
	hostIP, err := utils.GetHostIP()
	if err != nil {
		return "", fmt.Errorf("failed to get host IP: %w", err)
	}
	if hostIP == "" {
		return "", fmt.Errorf("no non-loopback IPv4 address found")
	}

	return fmt.Sprintf("%s.nip.io", hostIP), nil
}

// addRoutesToDomainMap adds routes to the domain map with sanitized variable names.
func addRoutesToDomainMap(routes []proxy.Route, routeDomains map[string]string) {
	for _, route := range routes {
		parts := strings.Split(route.Domain, ".")
		if len(parts) > 0 {
			subdomain := parts[0]
			sanitizedSubdomain := strings.ReplaceAll(subdomain, "-", "_")
			varName := strings.ToUpper(fmt.Sprintf("%s_DOMAIN", sanitizedSubdomain))
			routeDomains[varName] = route.Domain
		}
	}
}

// getCaddyHTTPSPort retrieves the host port mapped to Caddy's HTTPS port (container port 443).
func getCaddyHTTPSPort(rt *podman.PodmanClient, caddyPodName string) (string, error) {
	pod, err := rt.InspectPod(caddyPodName)
	if err != nil {
		return "", fmt.Errorf("failed to inspect Caddy pod: %w", err)
	}

	// Get port mappings from the Ports field
	// Ports is a map[string][]string where key is "containerPort/protocol" and value is list of host ports
	// Example: {"443/tcp": ["39341"], "2019/tcp": ["37249"]}
	for containerPort, hostPorts := range pod.Ports {
		// Check if this is the HTTPS port (443)
		if strings.HasPrefix(containerPort, "443/") && len(hostPorts) > 0 {
			return hostPorts[0], nil
		}
	}

	return "", fmt.Errorf("HTTPS port mapping not found in pod ports")
}

// parseRouteEntry parses a single route entry and returns the subdomain.
// Route format: "port:subdomain:type"
// Returns empty string if the entry is invalid.
func parseRouteEntry(routeEntry, podName string) string {
	// Use shared helper from proxy package
	parts, err := proxy.ParseRouteEntry(routeEntry)
	if err != nil {
		logger.Warningf("Invalid route format '%s' in pod %s: %v", routeEntry, podName, err)

		return ""
	}

	return parts.Subdomain
}

// processRouteInfo processes route information and populates the routeDomains map.
func processRouteInfo(info TemplateRouteInfo, proxyManager proxy.ProxyManager, routeDomains map[string]string) {
	// Parse routes annotation directly to extract subdomains
	// Format: "port:subdomain:type, port:subdomain:type, ..."
	// Example: "8081:catalog-ui:ui, 8080:catalog-api:api"
	for _, routeEntry := range strings.Split(info.RoutesAnnotation, ",") {
		subdomain := parseRouteEntry(routeEntry, info.PodName)
		if subdomain == "" {
			continue
		}

		// Query Caddy for this route (route ID is just the subdomain)
		actualRoute, err := proxyManager.GetRouteByID(subdomain)
		if err != nil {
			// Log warning but continue - route might not exist yet
			logger.Warningf("Failed to query route %s from Caddy: %v", subdomain, err)

			continue
		}

		// Create variable name from subdomain
		sanitizedSubdomain := strings.ReplaceAll(subdomain, "-", "_")
		varName := strings.ToUpper(fmt.Sprintf("%s_DOMAIN", sanitizedSubdomain))
		routeDomains[varName] = actualRoute.Domain
	}
}

// GetCatalogRouteInfo retrieves route domains and HTTPS port for the catalog service.
func GetCatalogRouteInfo(rt *podman.PodmanClient, tp templates.Template, appTemplateName string, argParams map[string]string) (map[string]string, string, error) {
	// Find Caddy pod from templates
	caddyPodName, err := findCaddyPodNameFromTemplates(tp, appTemplateName, argParams)
	if err != nil {
		return nil, "", fmt.Errorf("failed to find Caddy pod: %w", err)
	}

	// Get Caddy admin URL for querying specific routes
	caddyAdminPort, err := proxy.GetCaddyAdminPort(rt, caddyPodName)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get Caddy admin port: %w", err)
	}
	caddyAdminURL := fmt.Sprintf("http://localhost:%s", caddyAdminPort)

	// Extract routes from all templates (same as during registration)
	routeInfos, err := extractAllRoutesFromTemplates(tp, appTemplateName, argParams)
	if err != nil {
		return nil, "", fmt.Errorf("failed to extract routes from templates: %w", err)
	}

	// Create proxy manager
	proxyManager := proxy.NewCaddyManager(caddyAdminURL, constants.CaddyServerName)

	// Build route domains map by querying Caddy for each route
	routeDomains := make(map[string]string)
	for _, info := range routeInfos {
		processRouteInfo(info, proxyManager, routeDomains)
	}

	// Get Caddy HTTPS port
	httpsPort, err := getCaddyHTTPSPort(rt, caddyPodName)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get Caddy HTTPS port: %w", err)
	}

	return routeDomains, httpsPort, nil
}

// validateReconfigureParameters validates that domain and HTTPS port haven't changed during reconfigure.
// Returns the existing domain if validation passes, or an error if validation fails.
func validateReconfigureParameters(rt *podman.PodmanClient, certDomain, domainName string, argParams map[string]string, userProvidedHTTPSPort int) (string, error) {
	// Get existing configuration from catalog-backend pod
	existingDomain, existingHTTPSPort, err := getExistingConfigFromCatalogBackend(rt)
	if err != nil {
		return "", fmt.Errorf("failed to get existing configuration from catalog-backend: %w", err)
	}

	// Compute new domain suffix
	newDomain, err := computeDomainSuffix(certDomain, domainName)
	if err != nil {
		return "", fmt.Errorf("failed to compute domain suffix: %w", err)
	}

	// Validate domain matches
	if existingDomain != newDomain {
		return "", fmt.Errorf("domain change not allowed during reconfigure: existing=%s, new=%s. Please delete the catalog deployment and reconfigure fresh to change domain", existingDomain, newDomain)
	}

	// Handle HTTPS port validation
	if existingHTTPSPort != "" {
		// Existing deployment has HTTPS port configured
		// Check if user explicitly provided a different port
		userProvidedPortStr := fmt.Sprintf("%d", userProvidedHTTPSPort)

		if userProvidedHTTPSPort > 0 && existingHTTPSPort != userProvidedPortStr {
			return "", fmt.Errorf("HTTPS port change not allowed during reconfigure: existing=%s, new=%s. Catalog is already configured with HTTPS port %s", existingHTTPSPort, userProvidedPortStr, existingHTTPSPort)
		}
	}

	return existingDomain, nil
}

// getExistingConfigFromCatalogBackend retrieves the domain and HTTPS port from the catalog-backend container.
// These values are used to validate that configuration hasn't changed during reconfigure operations.
func getExistingConfigFromCatalogBackend(rt *podman.PodmanClient) (domain string, httpsPort string, err error) {
	// Construct the catalog-backend container name dynamically
	// Pattern: {AppName}--catalog-backend (e.g., "ai-services--catalog-backend")
	// This follows the Podman naming convention: {podName}-{containerName}
	catalogBackendContainerName := fmt.Sprintf("%s--catalog-backend", catalogconstants.CatalogAppName)

	// Inspect the catalog-backend container
	stats, err := containers.Inspect(rt.Context, catalogBackendContainerName, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to inspect catalog-backend container '%s': %w", catalogBackendContainerName, err)
	}

	if stats == nil || stats.Config == nil || stats.Config.Env == nil {
		return "", "", fmt.Errorf("invalid container stats when inspecting catalog-backend container")
	}

	// Extract DOMAIN_SUFFIX and HTTPS_PORT from environment variables
	for _, envVar := range stats.Config.Env {
		// Environment variables are in format "KEY=VALUE"
		parts := strings.SplitN(envVar, "=", 2)
		if len(parts) == 2 {
			switch parts[0] {
			case domainSuffixEnvVar:
				domain = parts[1]
			case httpsPortEnvVar:
				httpsPort = parts[1]
			}
		}
	}

	if domain == "" {
		return "", "", fmt.Errorf("DOMAIN_SUFFIX environment variable not found in catalog-backend container")
	}

	// HTTPS port is optional - older deployments may not have it
	// If not found, it will be handled gracefully by the caller
	return domain, httpsPort, nil
}

// validateCertificateUpdate validates certificate changes during reconfigure.
// Returns an error if validation fails or if certificates should be skipped (with special error).
// Returns nil if certificates can proceed with loading.
func validateCertificateUpdate(newCertPath, newKeyPath, stagedCertPath, stagedKeyPath string) error {
	// Check if staged certificates exist from previous successful deployment
	_, certErr := os.Stat(stagedCertPath)
	_, keyErr := os.Stat(stagedKeyPath)

	if os.IsNotExist(certErr) || os.IsNotExist(keyErr) {
		// No staged certificates found - either:
		// 1. Previous deployment used auto-generated certs, OR
		// 2. Previous custom cert deployment failed during staging
		// In both cases, allow cert loading since domain is already validated
		logger.Infof("No existing custom certificates found, loading new certificates\n")
		return nil
	}

	// Staged certificates exist - compare content
	needsUpdate, err := certificatesNeedUpdate(newCertPath, newKeyPath, stagedCertPath, stagedKeyPath)
	if err != nil {
		return fmt.Errorf("failed to check certificate status: %w", err)
	}

	if !needsUpdate {
		// Certificates are identical - return special error to signal skip
		return ErrCertificatesAlreadyLoaded
	}

	// Certificates differ - block update
	return fmt.Errorf("certificate content change not allowed during reconfigure. Please reset cert")
}

// certificatesNeedUpdate checks if new certificates differ from existing staged certificates.
// Returns true if certificates need to be updated, false if they are identical.
func certificatesNeedUpdate(newCertPath, newKeyPath, stagedCertPath, stagedKeyPath string) (bool, error) {
	// Check if staged certificates exist
	if _, err := os.Stat(stagedCertPath); os.IsNotExist(err) {
		// No existing certificates, need to load new ones
		return true, nil
	}
	if _, err := os.Stat(stagedKeyPath); os.IsNotExist(err) {
		// No existing key, need to load new ones
		return true, nil
	}

	// Compare certificate hashes
	newCertHash, err := computeFileHash(newCertPath)
	if err != nil {
		return false, fmt.Errorf("failed to compute hash for new certificate: %w", err)
	}

	stagedCertHash, err := computeFileHash(stagedCertPath)
	if err != nil {
		return false, fmt.Errorf("failed to compute hash for staged certificate: %w", err)
	}

	// Compare key hashes
	newKeyHash, err := computeFileHash(newKeyPath)
	if err != nil {
		return false, fmt.Errorf("failed to compute hash for new key: %w", err)
	}

	stagedKeyHash, err := computeFileHash(stagedKeyPath)
	if err != nil {
		return false, fmt.Errorf("failed to compute hash for staged key: %w", err)
	}

	// If either cert or key differs, need update
	return newCertHash != stagedCertHash || newKeyHash != stagedKeyHash, nil
}

// computeFileHash computes SHA256 hash of a file.
func computeFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to compute hash: %w", err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Made with Bob
