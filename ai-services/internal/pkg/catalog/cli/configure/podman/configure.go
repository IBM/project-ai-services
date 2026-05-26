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
	catalogAppTemplate = "catalog"
	dirPerm            = 0o755
	filePerm           = 0o644
	kindSecret         = "Secret"
)

// DeployCatalog deploys the catalog service using the assets/catalog template for podman runtime.
func DeployCatalog(ctx context.Context, podmanURI, passwordHash, baseDir string, argParams map[string]string) error {
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

	// collect all secret names used as part of deployment
	isDeployed, existingResources, err := checkCatalogStatus(rt, tp, tmpls, argParams)
	if err != nil {
		s.Fail("failed to check existing resources")

		return fmt.Errorf("failed to check existing resources: %w", err)
	}

	if isDeployed {
		s.Stop("Catalog service already deployed")
		logger.Infof("Catalog pod already exists: %v\n", existingResources)

		return nil
	}

	// Prepare values with configure-specific configuration
	values, err := prepareCatalogValues(tp, podmanURI, passwordHash, argParams)
	if err != nil {
		s.Fail("failed to load values")

		return fmt.Errorf("failed to load values: %w", err)
	}

	// Generate and write Caddyfile before deploying
	if err := generateCaddyfile(baseDir, values); err != nil {
		s.Fail("failed to generate Caddyfile")

		return fmt.Errorf("failed to generate Caddyfile: %w", err)
	}

	// Execute pod templates
	if err := executePodLayers(rt, tp, tmpls, appMetadata, values, baseDir, argParams, s, existingResources); err != nil {
		return err
	}

	s.Stop("Catalog service deployed successfully")
	logger.Infoln("-------")

	return handlePostDeployment(rt, tp, argParams)
}

// handlePostDeployment handles route registration and next steps display after catalog deployment.
func handlePostDeployment(rt *podman.PodmanClient, tp templates.Template, argParams map[string]string) error {
	// Register routes with Caddy and get the registered route domains
	routeDomains, httpsPort, err := registerCatalogRoutes(rt, tp, catalogAppTemplate, argParams)
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
func prepareCatalogValues(tp templates.Template, podmanURI, passwordHash string, argParams map[string]string) (map[string]any, error) {
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

	// Load values from catalog
	return tp.LoadValues(catalogAppTemplate, nil, argParams)
}

// executePodLayers executes all pod template layers.
func executePodLayers(rt *podman.PodmanClient, tp templates.Template, tmpls map[string]*template.Template,
	appMetadata *templates.AppMetadata, values map[string]any, baseDir string, argParams map[string]string,
	s *spinner.Spinner, existingResources []string) error {
	for i, layer := range appMetadata.PodTemplateExecutions {
		logger.Infof("\n Executing Layer %d/%d: %v\n", i+1, len(appMetadata.PodTemplateExecutions), layer)
		logger.Infoln("-------")

		if err := executeLayer(rt, tp, tmpls, layer, appMetadata.Version, values, baseDir, argParams, i, existingResources); err != nil {
			s.Fail("failed to deploy catalog pod")

			return err
		}

		logger.Infof("Layer %d completed\n", i+1)
	}

	return nil
}

// executeLayer executes a single layer of pod templates.
func executeLayer(rt *podman.PodmanClient, tp templates.Template, tmpls map[string]*template.Template,
	layer []string, version string, values map[string]any, baseDir string, argParams map[string]string,
	layerIndex int, existingResources []string) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(layer))

	// for each layer, fetch all the pod Template Names and do the pod deploy
	for _, podTemplateName := range layer {
		wg.Add(1)
		go func(t string) {
			defer wg.Done()
			if err := executePodTemplate(rt, tp, tmpls, t, catalogAppTemplate, catalogconstants.CatalogAppName, values, version, nil, baseDir, argParams, existingResources); err != nil {
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
	valuesFiles []string, baseDir string, argParams map[string]string, existingResources []string) error {
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

	logger.Infof("Generated Caddyfile to: %s\n", caddyfilePath)

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
func registerCatalogRoutes(rt *podman.PodmanClient, tp templates.Template, appTemplateName string, argParams map[string]string) (map[string]string, string, error) {
	// Extract routes from all templates
	routeInfos, err := extractAllRoutesFromTemplates(tp, appTemplateName, argParams)
	if err != nil {
		return nil, "", fmt.Errorf("failed to extract routes from templates: %w", err)
	}

	if len(routeInfos) == 0 {
		logger.Infof("No templates found with routes annotation, skipping route registration\n")

		return nil, "", nil
	}

	// Find Caddy pod from templates
	caddyPodName, err := findCaddyPodNameFromTemplates(tp, appTemplateName, argParams)
	if err != nil {
		return nil, "", fmt.Errorf("failed to find Caddy pod: %w", err)
	}

	logger.Infof("Found Caddy pod: %s\n", caddyPodName)

	// Build route domains map
	routeDomains := make(map[string]string)

	// Register routes for each template that has them
	var registrationErrors []error
	for _, info := range routeInfos {
		logger.Infof("Registering routes for pod: %s\n", info.PodName)

		// Register routes and get the built routes back
		routes, err := proxy.RegisterRoutesForAppAndReturn(rt, catalogconstants.CatalogAppName, constants.CaddyServerName, info.RoutesAnnotation, caddyPodName, info.PodName)
		if err != nil {
			registrationErrors = append(registrationErrors, fmt.Errorf("pod %s: %w", info.PodName, err))

			continue
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

	// Return error if any routes failed to register
	if len(registrationErrors) > 0 {
		return nil, "", fmt.Errorf("failed to register routes for %d pod(s): %w", len(registrationErrors), errors.Join(registrationErrors...))
	}

	// Get Caddy HTTPS port
	httpsPort, err := getCaddyHTTPSPort(rt, caddyPodName)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get Caddy HTTPS port: %w", err)
	}

	logger.Infof("Successfully registered routes for %d pod(s)\n", len(routeInfos))

	return routeDomains, httpsPort, nil
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

	// Build routes from annotations to get domains
	for _, info := range routeInfos {
		routes, err := proxy.BuildRoutesFromAnnotation(info.RoutesAnnotation, hostIP, info.PodName)
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
