package podman

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"text/template"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	clipodman "github.com/project-ai-services/ai-services/internal/pkg/cli/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/specs"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	catalogAppName  = "ai-services"
	catalogAppTemplate = "catalog"
	caddyDataDir    = "/var/lib/ai-services/catalog/caddy"
)

// DeployCatalog deploys the catalog service using the assets/catalog template for podman runtime.
func DeployCatalog(ctx context.Context, podmanURI, passwordHash string, argParams map[string]string) error {
	s := spinner.New("Deploying catalog service...")
	s.Start(ctx)

	// Initialize runtime
	rt, err := podman.NewPodmanClient()
	if err != nil {
		s.Fail("failed to initialize podman client")
		return fmt.Errorf("failed to initialize podman client: %w", err)
	}

	// Load template provider and metadata
	tp, appMetadata, tmpls, err := loadCatalogTemplates(s)
	if err != nil {
		s.Fail("failed to load catalog templates")
		return fmt.Errorf("failed to load catalog templates: %w", err)
	}

	// Check if catalog pod already exists
	existingPods, err := helpers.CheckExistingPodsForApplication(rt, catalogAppName)
	if err != nil {
		s.Fail("failed to check existing pods")
		return fmt.Errorf("failed to check existing pods: %w", err)
	}

	if len(existingPods) == len(tmpls) {
		s.Stop("Catalog service already deployed")
		logger.Infof("Catalog pod already exists: %v\n", existingPods)
		return nil
	}

	// Setup HTTPS configuration
	httpsConfig, err := setupHTTPSConfig(s)
	if err != nil {
		s.Fail("failed to setup HTTPS configuration")
		return fmt.Errorf("failed to setup HTTPS configuration: %w", err)
	}

	// Prepare values with configure-specific configuration
	values, err := prepareCatalogValues(tp, podmanURI, passwordHash, argParams, httpsConfig)
	if err != nil {
		s.Fail("failed to load values")
		return fmt.Errorf("failed to load values: %w", err)
	}

	// Execute pod templates
	if err := executePodLayers(rt, tp, tmpls, appMetadata, values, argParams, s); err != nil {
		return err
	}

	s.Stop("Catalog service deployed successfully")

	// Register catalog routes with Caddy
	if err := registerCatalogRoutes(rt, httpsConfig); err != nil {
		logger.Infof("Warning: Failed to register catalog routes with Caddy: %v", err)
	}

	logger.Infoln("-------")

	// Print next steps
	if err := printNextSteps(tp, rt, httpsConfig); err != nil {
		logger.Infof("failed to display next steps: %v\n", err)
	}

	return nil
}

// HTTPSConfig contains HTTPS configuration
type HTTPSConfig struct {
	Domain string
	HostIP string
}

// setupHTTPSConfig prepares HTTPS configuration
func setupHTTPSConfig(s *spinner.Spinner) (*HTTPSConfig, error) {
	config := &HTTPSConfig{}

	// Get host IP
	hostIP, err := utils.GetHostIP()
	if err != nil {
		return nil, fmt.Errorf("failed to detect host IP: %w", err)
	}
	config.HostIP = hostIP

	// Use nip.io with host IP
	config.Domain = fmt.Sprintf("%s.nip.io", hostIP)
	logger.Infof("Using nip.io domain: %s", config.Domain)

	// Create Caddy configuration directory and Caddyfile
	if err := createCaddyConfig(config); err != nil {
		return nil, fmt.Errorf("failed to create Caddy configuration: %w", err)
	}

	return config, nil
}

// createCaddyConfig creates the Caddy data directory and Caddyfile
func createCaddyConfig(config *HTTPSConfig) error {
	// Create data directory
	if err := os.MkdirAll(caddyDataDir, 0755); err != nil {
		return fmt.Errorf("failed to create Caddy data directory: %w", err)
	}

	// Create Caddyfile in /data/caddy directory (Caddy's default location)
	caddyfilePath := filepath.Join(caddyDataDir, "Caddyfile")
	caddyfileContent := `{
	# Admin API - accessible from host
	admin 0.0.0.0:2019

	# Configure HTTPS server
	servers :443 {
		name ai_services_server
	}
}

# Default HTTPS configuration with self-signed certificates
:443 {
	tls internal
}

# Routes will be dynamically added via Caddy Admin API
`

	if err := os.WriteFile(caddyfilePath, []byte(caddyfileContent), 0644); err != nil {
		return fmt.Errorf("failed to write Caddyfile: %w", err)
	}

	logger.Infof("Created Caddyfile at %s", caddyfilePath)
	return nil
}

// registerCatalogRoutes registers catalog service routes with Caddy
func registerCatalogRoutes(rt *podman.PodmanClient, config *HTTPSConfig) error {
	logger.Infof("Registering catalog routes with Caddy...")

	// Wait for Caddy to be ready
	caddyClient := proxy.NewCaddyClient()
	if err := caddyClient.HealthCheck(); err != nil {
		return fmt.Errorf("caddy is not available: %w", err)
	}

	// Register route for catalog UI
	catalogUIRoute := proxy.Route{
		ID:              "route-ai-services--catalog-ui",
		Host:            utils.ConstructServiceDomain(catalogAppName, "catalog", config.Domain),
		UpstreamAddress: fmt.Sprintf("%s--catalog:8081", catalogAppName),
		Terminal:        true,
	}

	if err := caddyClient.RegisterRoute(catalogUIRoute); err != nil {
		return fmt.Errorf("failed to register catalog UI route: %w", err)
	}

	logger.Infof("Successfully registered catalog routes")
	return nil
}

// printNextSteps prints the next steps after deployment
func printNextSteps(tp templates.Template, rt *podman.PodmanClient, config *HTTPSConfig) error {
	// Construct catalog URL (port 443 is default for HTTPS, so omit it)
	catalogURL := utils.ConstructServiceURL(
		"https",
		utils.ConstructServiceDomain(catalogAppName, "catalog", config.Domain),
		443,
	)

	// Print custom next steps
	logger.Infoln("Next Steps:")
	logger.Infoln("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logger.Infoln("")
	logger.Infof("1. Access the Catalog UI via HTTPS:\n")
	logger.Infof("   %s\n", catalogURL)
	logger.Infoln("")
	logger.Infof("   Note: Using self-signed certificate for %s.\n", config.Domain)
	logger.Infof("         Your browser may show a security warning.\n")
	logger.Infoln("")
	logger.Infoln("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	return nil
}

// loadCatalogTemplates loads the catalog template provider, metadata, and templates.
func loadCatalogTemplates(s *spinner.Spinner) (templates.Template, *templates.AppMetadata, map[string]*template.Template, error) {
	tp := templates.NewEmbedTemplateProvider(&assets.CatalogFS, "")

	// Load metadata from catalog/podman
	appMetadata, err := tp.LoadMetadata(catalogAppTemplate, true)
	if err != nil {
		s.Fail("failed to load catalog metadata")
		return nil, nil, nil, fmt.Errorf("failed to load catalog metadata: %w", err)
	}

	// Load all templates from catalog
	tmpls, err := tp.LoadAllTemplates(catalogAppTemplate)
	if err != nil {
		s.Fail("failed to load catalog templates")
		return nil, nil, nil, fmt.Errorf("failed to load catalog templates: %w", err)
	}

	return tp, appMetadata, tmpls, nil
}

// prepareCatalogValues prepares the values map with configure-specific configuration.
func prepareCatalogValues(tp templates.Template, podmanURI, passwordHash string, argParams map[string]string, httpsConfig *HTTPSConfig) (map[string]any, error) {
	if argParams == nil {
		argParams = make(map[string]string)
	}

	// Generate database password
	dbPassword, err := utils.GenerateRandomPassword(utils.DefaultPasswordLength)
	if err != nil {
		return nil, fmt.Errorf("failed to generate database password: %w", err)
	}

	// Base64 encode the database password for Kubernetes secret
	dbPasswordBase64 := base64.StdEncoding.EncodeToString([]byte(dbPassword))

	// Set configure-specific values
	argParams["backend.adminPasswordHash"] = passwordHash
	argParams["backend.runtime"] = "podman"
	argParams["backend.podman.uri"] = podmanURI
	argParams["db.password"] = dbPasswordBase64

	// Set Caddy configuration
	argParams["caddy.httpsPort"] = "443"
	argParams["caddy.domain"] = httpsConfig.Domain
	argParams["caddy.externalPort"] = "443"

	// Load values from catalog
	return tp.LoadValues(catalogAppTemplate, nil, argParams)
}

// executePodLayers executes all pod template layers.
func executePodLayers(rt *podman.PodmanClient, tp templates.Template, tmpls map[string]*template.Template,
	appMetadata *templates.AppMetadata, values map[string]any, argParams map[string]string, s *spinner.Spinner) error {
	for i, layer := range appMetadata.PodTemplateExecutions {
		logger.Infof("\n Executing Layer %d/%d: %v\n", i+1, len(appMetadata.PodTemplateExecutions), layer)
		logger.Infoln("-------")

		if err := executeLayer(rt, tp, tmpls, layer, appMetadata.Version, values, argParams, i); err != nil {
			s.Fail("failed to deploy catalog pod")
			return err
		}

		logger.Infof("Layer %d completed\n", i+1)
	}

	return nil
}

// executeLayer executes a single layer of pod templates.
func executeLayer(rt *podman.PodmanClient, tp templates.Template, tmpls map[string]*template.Template,
	layer []string, version string, values map[string]any, argParams map[string]string, layerIndex int) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(layer))

	// for each layer, fetch all the pod Template Names and do the pod deploy
	for _, podTemplateName := range layer {
		wg.Add(1)
		go func(t string) {
			defer wg.Done()
			if err := executePodTemplate(rt, tp, tmpls, t, catalogAppTemplate, catalogAppName, values, version, nil, argParams); err != nil {
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
	valuesFiles []string, argParams map[string]string) error {
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
		"Values":          values,
		"env":             map[string]map[string]string{},
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

// Made with Bob
