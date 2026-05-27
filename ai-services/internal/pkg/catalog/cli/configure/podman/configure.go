package podman

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"text/template"

	"github.com/project-ai-services/ai-services/assets"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	clipodman "github.com/project-ai-services/ai-services/internal/pkg/cli/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
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
)

// DeployCatalog deploys the catalog service using the assets/catalog template for podman runtime.
func DeployCatalog(ctx context.Context, podmanURI, authFilePath, passwordHash, baseDir string, argParams map[string]string, domainName string, sslCertPath, sslKeyPath string, httpsPort int) error {
	s := spinner.New("Deploying catalog service...")
	s.Start(ctx)

	// Initialize and validate
	rt, tp, appMetadata, tmpls, argParams, err := initializeCatalogDeployment(argParams, httpsPort, s)
	if err != nil {
		return err
	}

	// Extract domain from certificate if provided
	certDomain, err := extractCertDomainIfProvided(sslCertPath, sslKeyPath, s)
	if err != nil {
		return err
	}

	// Check existing deployment status
	_, existingResources, err := checkCatalogStatus(rt, tp, tmpls, argParams)
	if err != nil {
		return err
	}

	// Check if already deployed
	if len(existingResources) > 0 {
		s.Stop("Catalog service already deployed")
		logger.Infof("Catalog pod already exists: %v\n", existingResources)

		return nil
	}

	// Deploy catalog service
	if err := deployCatalogService(rt, tp, tmpls, appMetadata, podmanURI, authFilePath, passwordHash, baseDir, argParams, s); err != nil {
		return err
	}

	s.Stop("Catalog service deployed successfully")
	logger.Infoln("-------")

	// Load SSL certificates if provided
	if err := loadSSLCertificatesIfProvided(rt, tp, baseDir, sslCertPath, sslKeyPath, argParams); err != nil {
		return err
	}

	return handlePostDeployment(rt, tp, argParams, certDomain, domainName)
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

// prepareCatalogDeployment prepares all necessary data for deployment.
func prepareCatalogDeployment(tp templates.Template, podmanURI, authFilePath, passwordHash, baseDir string, argParams map[string]string, s *spinner.Spinner) (string, string, string, map[string]any, error) {
	// Get host IP for template rendering
	hostIP, err := utils.GetHostIP()
	if err != nil {
		s.Fail("failed to get host IP")

		return "", "", "", nil, fmt.Errorf("failed to get host IP: %w", err)
	}

	// Find Caddy pod name from templates to build admin URL for container
	caddyPodName, err := findCaddyPodNameFromTemplates(tp, catalogAppTemplate, argParams)
	if err != nil {
		s.Fail("failed to find Caddy pod name")

		return "", "", "", nil, fmt.Errorf("failed to find Caddy pod name: %w", err)
	}

	// Build admin URL for container (uses internal port 2019)
	caddyAdminURL := fmt.Sprintf("http://%s:2019", caddyPodName)

	// Prepare values with configure-specific configuration
	values, err := prepareCatalogValues(tp, podmanURI, authFilePath, passwordHash, argParams)
	if err != nil {
		s.Fail("failed to load values")

		return "", "", "", nil, fmt.Errorf("failed to load values: %w", err)
	}

	// Generate and write Caddyfile before deploying
	if err := generateCaddyfile(baseDir, values); err != nil {
		s.Fail("failed to generate Caddyfile")

		return "", "", "", nil, fmt.Errorf("failed to generate Caddyfile: %w", err)
	}

	return hostIP, caddyPodName, caddyAdminURL, values, nil
}

// handlePostDeployment handles route registration and next steps display after catalog deployment.
func handlePostDeployment(rt *podman.PodmanClient, tp templates.Template, argParams map[string]string, certDomain, customDomain string) error {
	// Register routes with Caddy and get the registered route domains
	routeDomains, httpsPort, err := registerCatalogRoutes(rt, tp, catalogAppTemplate, argParams, certDomain, customDomain)
	if err != nil {
		return fmt.Errorf("route registration failed: %w", err)
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

// loadAndCheckCatalogTemplates loads templates and checks deployment status.
func loadAndCheckCatalogTemplates(rt *podman.PodmanClient, argParams map[string]string, s *spinner.Spinner) (templates.Template, *templates.AppMetadata, map[string]*template.Template, []string, error) {
	tp, appMetadata, tmpls, err := loadCatalogTemplates(s)
	if err != nil {
		s.Fail("failed to load catalog templates")
		return nil, nil, nil, nil, fmt.Errorf("failed to load catalog templates: %w", err)
	}

	// collect all secret names used as part of deployment
	isDeployed, existingResources, err := checkCatalogStatus(rt, tp, tmpls, argParams)
	if err != nil {
		s.Fail("failed to check existing resources")
		return nil, nil, nil, nil, fmt.Errorf("failed to check existing resources: %w", err)
	}

	if isDeployed {
		return tp, appMetadata, tmpls, existingResources, nil
	}

	return tp, appMetadata, tmpls, nil, nil
}

// deployCatalogService prepares values and deploys the catalog service.
func deployCatalogService(rt *podman.PodmanClient, tp templates.Template, tmpls map[string]*template.Template, appMetadata *templates.AppMetadata, podmanURI, authFilePath, passwordHash, baseDir string, argParams map[string]string, s *spinner.Spinner) error {
	// Get host IP for template rendering
	hostIP, err := utils.GetHostIP()
	if err != nil {
		s.Fail("failed to get host IP")
		return fmt.Errorf("failed to get host IP: %w", err)
	}

	// Find Caddy pod name from templates to build admin URL for container
	caddyPodName, err := findCaddyPodNameFromTemplates(tp, catalogAppTemplate, argParams)
	if err != nil {
		s.Fail("failed to find Caddy pod name")
		return fmt.Errorf("failed to find Caddy pod name: %w", err)
	}

	// Build admin URL for container (uses internal port 2019)
	caddyAdminURL := fmt.Sprintf("http://%s:2019", caddyPodName)

	values, err := prepareCatalogValues(tp, podmanURI, authFilePath, passwordHash, argParams)
	if err != nil {
		s.Fail("failed to load values")
		return fmt.Errorf("failed to load values: %w", err)
	}

	if err := generateCaddyfile(baseDir, values); err != nil {
		s.Fail("failed to generate Caddyfile")
		return fmt.Errorf("failed to generate Caddyfile: %w", err)
	}

	return executePodLayers(rt, tp, tmpls, appMetadata, values, baseDir, hostIP, caddyAdminURL, argParams, s, nil)
}

// loadSSLCertificatesIfProvided stages user-provided certificates for the Caddy pod and updates TLS config via Admin API.
func loadSSLCertificatesIfProvided(rt *podman.PodmanClient, tp templates.Template, baseDir, sslCertPath, sslKeyPath string, argParams map[string]string) error {
	if sslCertPath == "" || sslKeyPath == "" {
		return nil
	}

	caddyPodName, err := findCaddyPodNameFromTemplates(tp, catalogAppTemplate, argParams)
	if err != nil {
		return fmt.Errorf("failed to find Caddy pod: %w", err)
	}

	adminPort, err := proxy.GetCaddyAdminPort(rt, caddyPodName)
	if err != nil {
		return fmt.Errorf("failed to get Caddy admin port: %w", err)
	}

	if err := stageCertificatesForCaddy(baseDir, sslCertPath, sslKeyPath); err != nil {
		return fmt.Errorf("failed to stage certificates for Caddy: %w", err)
	}

	adminURL := fmt.Sprintf("http://localhost:%s", adminPort)
	if err := utils.LoadUserCertificates(
		filepath.Join(baseDir, "common", "caddy", caddyCertsDirName, "tls.crt"),
		filepath.Join(baseDir, "common", "caddy", caddyCertsDirName, "tls.key"),
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
	appMetadata *templates.AppMetadata, values map[string]any, baseDir, hostIP, caddyAdminURL string, argParams map[string]string,
	s *spinner.Spinner, existingResources []string) error {
	for i, layer := range appMetadata.PodTemplateExecutions {
		logger.Infof("\n Executing Layer %d/%d: %v\n", i+1, len(appMetadata.PodTemplateExecutions), layer)
		logger.Infoln("-------")

		if err := executeLayer(rt, tp, tmpls, layer, appMetadata.Version, values, baseDir, hostIP, caddyAdminURL, argParams, i, existingResources); err != nil {
			s.Fail("failed to deploy catalog pod")

			return err
		}

		logger.Infof("Layer %d completed\n", i+1)
	}

	return nil
}

// executeLayer executes a single layer of pod templates.
func executeLayer(rt *podman.PodmanClient, tp templates.Template, tmpls map[string]*template.Template,
	layer []string, version string, values map[string]any, baseDir, hostIP, caddyAdminURL string, argParams map[string]string,
	layerIndex int, existingResources []string) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(layer))

	// for each layer, fetch all the pod Template Names and do the pod deploy
	for _, podTemplateName := range layer {
		wg.Add(1)
		go func(t string) {
			defer wg.Done()
			if err := executePodTemplate(rt, tp, tmpls, t, catalogAppTemplate, catalogconstants.CatalogAppName, values, version, nil, baseDir, hostIP, caddyAdminURL, argParams, existingResources); err != nil {
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
	valuesFiles []string, baseDir, hostIP, caddyAdminURL string, argParams map[string]string, existingResources []string) error {
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
		"HostIP":          hostIP,
		"CaddyAdminURL":   caddyAdminURL,
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
func generateCaddyfile(baseDir string, values map[string]any) error {
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
	templateData := map[string]interface{}{
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

// extractAllRoutesFromTemplates extracts routes annotations from all templates that have them.
// Returns a slice of TemplateRouteInfo containing pod name and routes for each template.
func extractAllRoutesFromTemplates(tp templates.Template, appTemplateName string, argParams map[string]string) ([]TemplateRouteInfo, error) {
	// Load all templates
	tmpls, err := tp.LoadAllTemplates(appTemplateName)
	if err != nil {
		return nil, fmt.Errorf("failed to load templates: %w", err)
	}

	var routeInfos []TemplateRouteInfo

	// Loop through all templates to find those with routes annotation
	for templateName := range tmpls {
		podSpec, err := tp.LoadPodTemplateWithValues(appTemplateName, templateName, catalogconstants.CatalogAppName, nil, argParams)
		if err != nil {
			return nil, fmt.Errorf("failed to load template %s: %w", templateName, err)
		}

		// Check if this template has the routes annotation
		if podSpec.Annotations != nil {
			if routes, ok := podSpec.Annotations[constants.PodRoutesAnnotationKey]; ok {
				routeInfos = append(routeInfos, TemplateRouteInfo{
					PodName:          podSpec.Name,
					RoutesAnnotation: routes,
				})
			}
		}
	}

	return routeInfos, nil
}

// findCaddyPodNameFromTemplates finds the Caddy pod name by looking for the pod with component=proxy label in templates.
func findCaddyPodNameFromTemplates(tp templates.Template, appTemplateName string, argParams map[string]string) (string, error) {
	// Load all templates
	tmpls, err := tp.LoadAllTemplates(appTemplateName)
	if err != nil {
		return "", fmt.Errorf("failed to load templates: %w", err)
	}

	// Loop through all templates to find the Caddy pod
	for templateName := range tmpls {
		podSpec, err := tp.LoadPodTemplateWithValues(appTemplateName, templateName, catalogconstants.CatalogAppName, nil, argParams)
		if err != nil {
			return "", fmt.Errorf("failed to load template %s: %w", templateName, err)
		}

		// Check if this is the Caddy pod (component=proxy label)
		if podSpec.Labels != nil {
			if component, ok := podSpec.Labels["ai-services.io/component"]; ok && component == "proxy" {
				return podSpec.Name, nil
			}
		}
	}

	return "", fmt.Errorf("no Caddy pod found with component=proxy label in templates")
}

// registerCatalogRoutes registers routes with Caddy and returns route domains and HTTPS port.
func registerCatalogRoutes(rt *podman.PodmanClient, tp templates.Template, appTemplateName string, argParams map[string]string, certDomain, customDomain string) (map[string]string, string, error) {
	routeInfos, caddyPodName, err := prepareRouteRegistration(tp, appTemplateName, argParams)
	if err != nil {
		return nil, "", err
	}

	if len(routeInfos) == 0 {
		logger.Infof("No templates found with routes annotation, skipping route registration\n")
		return nil, "", nil
	}

	domainConfig := &proxy.DomainConfig{
		CustomDomain: customDomain,
		CertDomain:   certDomain,
	}

	routeDomains, err := registerRoutesForPods(rt, routeInfos, caddyPodName, domainConfig)
	if err != nil {
		return nil, "", err
	}

	httpsPort, err := getCaddyHTTPSPort(rt, caddyPodName)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get Caddy HTTPS port: %w", err)
	}

	logger.Infof("Successfully registered routes for %d pod(s)\n", len(routeInfos))

	return routeDomains, httpsPort, nil
}

// prepareRouteRegistration extracts route info and finds Caddy pod.
func prepareRouteRegistration(tp templates.Template, appTemplateName string, argParams map[string]string) ([]TemplateRouteInfo, string, error) {
	routeInfos, err := extractAllRoutesFromTemplates(tp, appTemplateName, argParams)
	if err != nil {
		return nil, "", fmt.Errorf("failed to extract routes from templates: %w", err)
	}

	caddyPodName, err := findCaddyPodNameFromTemplates(tp, appTemplateName, argParams)
	if err != nil {
		return nil, "", fmt.Errorf("failed to find Caddy pod: %w", err)
	}

	return routeInfos, caddyPodName, nil
}

// registerRoutesForPods registers routes for all pods and builds route domains map.
func registerRoutesForPods(rt *podman.PodmanClient, routeInfos []TemplateRouteInfo, caddyPodName string, domainConfig *proxy.DomainConfig) (map[string]string, error) {
	routeDomains := make(map[string]string)
	var registrationErrors []error

	// Get Caddy admin port for route registration
	adminPort, err := proxy.GetCaddyAdminPort(rt, caddyPodName)
	if err != nil {
		return nil, fmt.Errorf("failed to get Caddy admin port: %w", err)
	}
	adminURL := fmt.Sprintf("http://localhost:%s", adminPort)

	// Get host IP for route domain generation
	hostIP, err := utils.GetHostIP()
	if err != nil {
		return nil, fmt.Errorf("failed to get host IP: %w", err)
	}

	// Ensure domain config has HostIP set
	if domainConfig != nil && domainConfig.HostIP == "" {
		domainConfig.HostIP = hostIP
	}

	for _, info := range routeInfos {
		logger.Infof("Registering routes for pod: %s\n", info.PodName, logger.VerbosityLevelDebug)

		routes, err := proxy.RegisterRoutesForAppAndReturn(
			rt, catalogconstants.CatalogAppName, constants.CaddyServerName,
			info.RoutesAnnotation, adminURL, hostIP, info.PodName, domainConfig,
		)
		if err != nil {
			registrationErrors = append(registrationErrors, fmt.Errorf("pod %s: %w", info.PodName, err))
			continue
		}

		addRoutesToDomainMap(routes, routeDomains)
	}

	if len(registrationErrors) > 0 {
		return nil, fmt.Errorf("failed to register routes for %d pod(s): %w", len(registrationErrors), errors.Join(registrationErrors...))
	}

	return routeDomains, nil
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

// GetCatalogRouteInfo retrieves route domains and HTTPS port for the catalog service.
func GetCatalogRouteInfo(rt *podman.PodmanClient, tp templates.Template, appTemplateName string, argParams map[string]string) (map[string]string, string, error) {
	// Extract routes from all templates
	routeInfos, err := extractAllRoutesFromTemplates(tp, appTemplateName, argParams)
	if err != nil {
		return nil, "", fmt.Errorf("failed to extract routes from templates: %w", err)
	}

	if len(routeInfos) == 0 {
		return nil, "", fmt.Errorf("no templates found with routes annotation")
	}

	// Find Caddy pod from templates
	caddyPodName, err := findCaddyPodNameFromTemplates(tp, appTemplateName, argParams)
	if err != nil {
		return nil, "", fmt.Errorf("failed to find Caddy pod: %w", err)
	}

	// Get host IP for route domain generation
	hostIP, err := utils.GetHostIP()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get host IP: %w", err)
	}

	// Build route domains map
	routeDomains := make(map[string]string)

	// Build routes from annotations to get domains (using default nip.io domains)
	for _, info := range routeInfos {
		routes, err := proxy.BuildRoutesFromAnnotation(info.RoutesAnnotation, hostIP, info.PodName, nil)
		if err != nil {
			continue // Skip if routes can't be built
		}

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

	// Get Caddy HTTPS port
	httpsPort, err := getCaddyHTTPSPort(rt, caddyPodName)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get Caddy HTTPS port: %w", err)
	}

	return routeDomains, httpsPort, nil
}

// Made with Bob
