package application

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"strings"
	"text/template"
	"time"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/assets"
	appBootstrap "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/bootstrap"
	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apiModels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	appFlags "github.com/project-ai-services/ai-services/internal/pkg/cli/constants/application"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/flagvalidator"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	cliutils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/image"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

const (
	// Polling configuration.
	pollInterval       = 20 * time.Second
	pollTimeout        = 20 * time.Minute
	paramSplitParts    = 2
	expectedParamParts = 2
)

// Variables for flags placeholder.
var (
	// common flags.
	templateName string
	rawArgParams []string
	argParams    map[string]string

	// podman flags.
	skipModelDownload     bool
	skipImageDownload     bool
	skipChecks            []string
	valuesFiles           []string
	rawArgImagePullPolicy string
	legacyCreate          bool

	// openshift flags.
	timeout time.Duration
)

var createCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Deploys an application",
	Long: `Deploys an application with the provided application name based on the template
Arguments
- [name]: Application name (Required)

Examples:
# Deploy with default mode (5 Spyre cards)
ai-services application create rag --template rag --runtime podman

# Deploy with default mode (4 Spyre cards - reranker on CPU)
ai-services application create rag --template rag --runtime podman --params reranker.vllm-cpu=true

# Deploy with default mode (CPU mode)
ai-services application create rag --template rag --runtime podman --params reranker.vllm-cpu=true,llm.vllm-cpu=true

# Deploy with legacy mode
ai-services application create rag --template rag --runtime podman --legacy
	`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// Check if podman runtime is being used on unsupported platform
		if err := utils.CheckPodmanPlatformSupport(vars.RuntimeFactory.GetRuntimeType()); err != nil {
			return err
		}

		// Build and run flag validator
		flagValidator := buildFlagValidator()
		if err := flagValidator.Validate(cmd); err != nil {
			return err
		}

		appName := args[0]

		return utils.VerifyAppName(appName)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		ctx := context.Background()

		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		if err := doBootstrapValidate(); err != nil {
			return err
		}

		rt := vars.RuntimeFactory.GetRuntimeType()
		// When legacyCreate is true and runtime is podman, use the older/stable code path
		// For openshift runtime, always use the older/stable code path regardless of legacy flag
		if legacyCreate && rt == types.RuntimeTypePodman {
			// Create application instance using factory
			appFactory := application.NewFactory(rt)
			app, err := appFactory.Create(appName)
			if err != nil {
				return fmt.Errorf("failed to create application instance: %w", err)
			}

			opts := appTypes.CreateOptions{
				Name:              appName,
				TemplateName:      templateName,
				SkipModelDownload: skipModelDownload,
				SkipImageDownload: skipImageDownload,
				ArgParams:         argParams,
				ValuesFiles:       valuesFiles,
				ImagePullPolicy:   image.ImagePullPolicy(rawArgImagePullPolicy),
				Timeout:           timeout,
			}

			return app.Create(ctx, opts)
		}

		// Default: use catalog way of deploying application
		// For openshift runtime, always use the older/stable code path
		if rt == types.RuntimeTypePodman {
			return createApp(appName)
		}

		// OpenShift runtime uses the older implementation
		appFactory := application.NewFactory(rt)
		app, err := appFactory.Create(appName)
		if err != nil {
			return fmt.Errorf("failed to create application instance: %w", err)
		}

		opts := appTypes.CreateOptions{
			Name:              appName,
			TemplateName:      templateName,
			SkipModelDownload: skipModelDownload,
			SkipImageDownload: skipImageDownload,
			ArgParams:         argParams,
			ValuesFiles:       valuesFiles,
			ImagePullPolicy:   image.ImagePullPolicy(rawArgImagePullPolicy),
			Timeout:           timeout,
		}

		return app.Create(ctx, opts)
	},
}

func doBootstrapValidate() error {
	skip := helpers.ParseSkipChecks(skipChecks)
	if len(skip) > 0 {
		logger.Warningf("Skipping validation checks (skipped: %v)\n", skipChecks)
	}

	// Create bootstrap instance based on runtime
	factory := bootstrap.NewBootstrapFactory(vars.RuntimeFactory.GetRuntimeType())

	if err := factory.Validate(skip); err != nil {
		return fmt.Errorf("bootstrap validation failed: %w", err)
	}

	return nil
}

func init() {
	initCreateCommonFlags()
	initCreatePodmanFlags()
	initCreateOpenShiftFlags()
}

func initCreateCommonFlags() {
	skipCheckDesc := appBootstrap.BuildSkipFlagDescription()
	createCmd.Flags().StringSliceVar(&skipChecks, appFlags.Create.SkipValidation, []string{}, skipCheckDesc)

	createCmd.Flags().StringVarP(&templateName, appFlags.Create.Template, "t", "", "Application template to use (required)")
	_ = createCmd.MarkFlagRequired(appFlags.Create.Template)

	createCmd.Flags().StringSliceVar(
		&rawArgParams,
		appFlags.Create.Params,
		[]string{},
		"Inline parameters to configure the application.\n\n"+
			"Format:\n"+
			"- Comma-separated key=value pairs\n"+
			"- Example: --params key1=value1,key2=value2\n\n"+
			"- Use \"ai-services application templates\" to view the list of supported parameters\n\n"+
			"Precedence:\n"+
			"- When both --values and --params are provided, --params overrides --values\n",
	)

	createCmd.Flags().StringArrayVarP(
		&valuesFiles,
		appFlags.Create.Values,
		"f",
		[]string{},
		"Specify values files to override default template values.\n\n"+
			"Usage:\n"+
			"- Can be provided multiple times; files are applied in order and later files override earlier ones\n",
	)
}

func initCreatePodmanFlags() {
	createCmd.Flags().BoolVar(
		&skipImageDownload,
		appFlags.Create.SkipImageDownload,
		false,
		"Skip container image pull/download during application creation\n\n"+
			"Use this only if the required container images already exist locally\n"+
			"Recommended for air-gapped or pre-provisioned environments\n\n"+
			"Warning:\n"+
			"- If set to true and images are missing → command will fail\n"+
			"- If left false in air-gapped environments → pull/download attempt will fail\n"+
			"Note: Supported for podman runtime only.\n",
	)
	createCmd.Flags().BoolVar(
		&skipModelDownload,
		appFlags.Create.SkipModelDownload,
		false,
		"Skip model download during application creation\n\n"+
			"Use this if local models already exist at /var/lib/ai-services/models/\n"+
			"Recommended for air-gapped networks\n\n"+
			"Warning:\n"+
			"- If set to true and models are missing → command will fail\n"+
			"- If left false in air-gapped environments → download attempt will fail\n"+
			"Note: Supported for podman runtime only.\n",
	)
	createCmd.Flags().BoolVar(&legacyCreate, "legacy", false, "Use legacy application create implementation")

	initializeImagePullPolicyFlag()

	// deprecated flags
	deprecatedPodmanFlags()
}

func initCreateOpenShiftFlags() {
	createCmd.Flags().DurationVar(
		&timeout,
		appFlags.Create.Timeout,
		0, // default
		"Timeout for the operation (e.g. 10s, 2m, 1h).\n"+
			"Note: Supported for openshift runtime only.\n",
	)
}

func initializeImagePullPolicyFlag() {
	createCmd.Flags().StringVar(
		&rawArgImagePullPolicy,
		appFlags.Create.ImagePullPolicy,
		string(image.PullIfNotPresent),
		"Image pull policy for container images required for given application. Supported values: Always, Never, IfNotPresent.\n\n"+
			"Determines when the container runtime should pull the image from the registry:\n"+
			" - Always: pull the image every time from the registry before running\n"+
			" - Never: never pull; use only local images\n"+
			" - IfNotPresent: pull only if the image isn't already present locally \n\n"+
			"Defaults to 'IfNotPresent' if not specified\n\n"+
			"In air-gapped environments → specify 'Never'\n\n"+
			"Note: Supported for podman runtime only.\n\n",
	)
}

func deprecatedPodmanFlags() {
	if err := createCmd.Flags().MarkDeprecated(appFlags.Create.SkipImageDownload, "use --image-pull-policy instead"); err != nil {
		panic(fmt.Sprintf("Failed to mark '%s' flag deprecated. Err: %v", appFlags.Create.SkipImageDownload, err))
	}
}

// buildFlagValidator creates and configures the flag validator with all flag definitions.
func buildFlagValidator() *flagvalidator.FlagValidator {
	runtimeType := vars.RuntimeFactory.GetRuntimeType()

	builder := flagvalidator.NewFlagValidatorBuilder(runtimeType)

	// Register common flags with their validation functions
	builder.
		AddCommonFlag(appFlags.Create.SkipValidation, validateSkipChecksFlag).
		AddCommonFlag(appFlags.Create.Template, validateTemplateFlag).
		AddCommonFlag(appFlags.Create.Params, validateParamsFlag).
		AddCommonFlag(appFlags.Create.Values, validateValuesFlag)

	// Register Podman-specific flags
	builder.
		AddPodmanFlag(appFlags.Create.SkipImageDownload, nil).
		AddPodmanFlag(appFlags.Create.SkipModelDownload, nil).
		AddPodmanFlag(appFlags.Create.ImagePullPolicy, validateImagePullPolicyFlag)

	// Register OpenShift-specific flags
	builder.
		AddOpenShiftFlag(appFlags.Create.Timeout, nil)

	return builder.Build()
}

// validateTemplateFlag validates the template flag.
func validateTemplateFlag(cmd *cobra.Command) error {
	// do template validation in legacy mode for podman runtime
	// In default mode, templates are validated against catalog API
	if legacyCreate && vars.RuntimeFactory.GetRuntimeType() == types.RuntimeTypePodman {
		tp := templates.NewEmbedTemplateProvider(&assets.ApplicationFS)
		if err := tp.AppTemplateExist(templateName); err != nil {
			return err
		}

		return nil
	}

	// Default mode for podman: skip embedded template validation (uses catalog API)
	if vars.RuntimeFactory.GetRuntimeType() == types.RuntimeTypePodman {
		return nil
	}

	// OpenShift runtime: validate against embedded templates
	tp := templates.NewEmbedTemplateProvider(&assets.ApplicationFS)
	if err := tp.AppTemplateExist(templateName); err != nil {
		return err
	}

	return nil
}

// validateParamsFlag validates the params flag.
func validateParamsFlag(cmd *cobra.Command) error {
	if len(rawArgParams) == 0 {
		return nil
	}

	var err error
	argParams, err = utils.ParseKeyValues(rawArgParams)
	if err != nil {
		return fmt.Errorf("invalid format: %w", err)
	}

	// do template validation in legacy mode for podman runtime
	// In default mode, params are validated against catalog API schemas
	if legacyCreate && vars.RuntimeFactory.GetRuntimeType() == types.RuntimeTypePodman {
		// Validate params against template values (legacy mode)
		tp := templates.NewEmbedTemplateProvider(&assets.ApplicationFS)
		_, err = tp.LoadValues(templateName, nil, argParams)
		if err != nil {
			return fmt.Errorf("failed to load params: %w", err)
		}

		return nil
	}

	// Default mode for podman: skip embedded template validation (uses catalog API)
	if vars.RuntimeFactory.GetRuntimeType() == types.RuntimeTypePodman {
		return nil
	}

	// OpenShift runtime: validate params against template values
	tp := templates.NewEmbedTemplateProvider(&assets.ApplicationFS)
	_, err = tp.LoadValues(templateName, nil, argParams)
	if err != nil {
		return fmt.Errorf("failed to load params: %w", err)
	}

	return nil
}

// validateValuesFlag validates the values flag.
func validateValuesFlag(cmd *cobra.Command) error {
	for _, vf := range valuesFiles {
		if !utils.FileExists(vf) {
			return fmt.Errorf("file '%s' does not exist", vf)
		}
	}

	// Validate parameters in values files
	tp := templates.NewEmbedTemplateProvider(&assets.ApplicationFS)
	_, err := tp.LoadValues(templateName, valuesFiles, nil)
	if err != nil {
		return fmt.Errorf("failed to validate values files: %w", err)
	}

	return nil
}

// validateImagePullPolicyFlag validates the image-pull-policy flag.
func validateImagePullPolicyFlag(cmd *cobra.Command) error {
	if ok := image.ImagePullPolicy(rawArgImagePullPolicy).Valid(); !ok {
		return fmt.Errorf(
			"invalid value %q: must be one of %q, %q, %q",
			image.ImagePullPolicy(rawArgImagePullPolicy), image.PullAlways, image.PullNever, image.PullIfNotPresent,
		)
	}

	return nil
}

// validateSkipChecksFlag validates the skipChecks flag for the current runtime.
func validateSkipChecksFlag(cmd *cobra.Command) error {
	if len(skipChecks) == 0 {
		return nil
	}

	// Build valid checks dynamically from runtime
	validChecks := make(map[string]bool, len(bootstrap.GetRulesForRuntime()))
	for _, r := range bootstrap.GetRulesForRuntime() {
		validChecks[r.Name()] = true
	}

	// Validate each skip check
	for _, s := range skipChecks {
		if !validChecks[s] {
			return fmt.Errorf("invalid skip-validation value '%s' for runtime '%s'", s, vars.RuntimeFactory.GetRuntimeType())
		}
	}

	return nil
}

func createApp(appName string) error {
	// 1. Initialize catalog client
	appClient, err := catalogClient.NewApplicationClient()
	if err != nil {
		return fmt.Errorf("failed to create application client: %w", err)
	}

	// 2. Check if application already exists
	if err := checkApplicationExists(appClient, appName); err != nil {
		return err
	}

	// 3. Build the catalog API payload
	payload, err := buildCatalogPayload(appName)
	if err != nil {
		return err
	}

	// 4. Create application via catalog API
	logger.Infof("Creating application '%s' using template '%s'...\n", appName, templateName)
	resp, err := appClient.CreateApplication(payload)
	if err != nil {
		return fmt.Errorf("failed to create application: %w", err)
	}

	logger.Infof("Application creation initiated (ID: %s)\n", resp.ID)

	// 5. Poll for application status
	return pollApplicationStatus(appClient, appName)
}

// checkApplicationExists checks if an application with the given name already exists.
func checkApplicationExists(appClient *catalogClient.ApplicationClient, appName string) error {
	existingApp, err := cliutils.GetAppByName(appClient, appName)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return err
	}

	if existingApp != nil {
		return fmt.Errorf("application with name '%s' already exists", appName)
	}

	return nil
}

// buildCatalogPayload builds the catalog API payload for the given template.
func buildCatalogPayload(appName string) (*apiModels.CreateApplicationRequest, error) {
	// Initialize catalog provider
	provider, err := catalog.NewCatalogProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create catalog provider: %w", err)
	}

	// Determine if template is architecture or service
	isArchitecture := provider.ArchitectureExists(templateName)
	isService := provider.ServiceExists(templateName)

	if !isArchitecture && !isService {
		return nil, fmt.Errorf("template '%s' not found as architecture or service", templateName)
	}

	// Build the payload
	if isArchitecture {
		return buildArchitecturePayload(provider, templateName, appName)
	}

	return buildServicePayload(templateName, appName)
}

// pollApplicationStatus polls the application status until it's ready or fails.
func pollApplicationStatus(appClient *catalogClient.ApplicationClient, appName string) error {
	logger.Infof("Waiting for application '%s' to be ready...\n", appName)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	timeout := time.After(pollTimeout)

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for application '%s' to be ready", appName)

		case <-ticker.C:
			app, err := cliutils.GetAppByName(appClient, appName)
			if err != nil {
				return fmt.Errorf("failed to get application status: %w", err)
			}

			done, err := handleApplicationStatus(app, appName)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		}
	}
}

// handleApplicationStatus handles the application status and returns (done, error).
func handleApplicationStatus(app *catalogTypes.Application, appName string) (bool, error) {
	// Status values from ai-services/internal/pkg/catalog/db/models/application.go.
	switch app.Status {
	case "Running":
		logger.Infof("Application '%s' is ready!\n", appName)

		// Print next steps after successful deployment
		if err := printNextSteps(app); err != nil {
			logger.Warningf("Failed to display next steps: %v\n", err)
		}

		return true, nil

	case "Error":
		if app.Message != "" {
			return false, fmt.Errorf("application deployment failed: %s", app.Message)
		}

		return false, fmt.Errorf("application deployment failed")

	case "Downloading", "Deploying":
		// Still in progress, continue polling.
		logger.Infof("Deploying application: %s, Status: %s, Message: %s\n", appName, app.Status, app.Message)

		return false, nil

	case "Deleting":
		return false, fmt.Errorf("application is being deleted")

	default:
		logger.Infof("Status: %s\n", app.Status)

		return false, nil
	}
}

// printNextSteps prints the next steps for the deployed application.
func printNextSteps(app *catalogTypes.Application) error {
	appClient, err := catalogClient.NewApplicationClient()
	if err != nil {
		return fmt.Errorf("failed to create application client: %w", err)
	}

	// Get full application details with services
	application, err := appClient.GetApplication(app.ID)
	if err != nil {
		return fmt.Errorf("failed to get application: %w", err)
	}

	catalogProvider, err := catalog.NewCatalogProvider()
	if err != nil {
		return fmt.Errorf("failed to create catalog provider: %w", err)
	}

	logger.Infoln("\nNext Steps:")
	logger.Infoln("-------")

	for _, service := range application.Services {
		params := map[string]string{}
		params["SERVICE_NAME"] = service.Type

		// Add endpoint URLs to params
		for _, endpoint := range service.Endpoints {
			urlType, urlTypeOk := endpoint["type"].(string)
			url, urlOk := endpoint["url"].(string)
			if urlTypeOk && urlOk {
				params[strings.ToUpper(urlType)+"_URL"] = url
			}
		}

		tmpls, err := catalogProvider.LoadServicesMD(service.CatalogID)
		if err != nil {
			logger.Warningf("Failed to load next steps for service '%s': %v\n", service.CatalogID, err)

			continue
		}

		err = printNextStepsMD(tmpls, params, application.Name)
		if err != nil {
			logger.Warningf("Failed to render next steps for service '%s': %v\n", service.CatalogID, err)
		}
	}

	return nil
}

// printNextStepsMD renders and prints the next.md template for a service.
func printNextStepsMD(tmpls map[string]*template.Template, params map[string]string, appName string) error {
	tmpl, ok := tmpls["next.md"]
	if !ok {
		// next.md doesn't exist for this service, return nil
		return nil
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, params); err != nil {
		return fmt.Errorf("failed to execute next.md: %w", err)
	}

	value := rendered.String()

	// Print the template content if available
	if strings.TrimSpace(value) != "" {
		logger.Infoln(value)
	}

	// Print the info command for all services
	logger.Infof("\n- For detailed endpoint information, use: `ai-services application info %s --runtime podman`\n", appName)

	return nil
}

// buildArchitecturePayload builds the payload for an architecture deployment.
func buildArchitecturePayload(provider *catalog.CatalogProvider, archID, appName string) (*apiModels.CreateApplicationRequest, error) {
	// Load architecture metadata
	arch, err := provider.LoadArchitecture(archID)
	if err != nil {
		return nil, fmt.Errorf("failed to load architecture: %w", err)
	}

	// Create application client for API calls
	appClient, err := catalogClient.NewApplicationClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create application client: %w", err)
	}

	// Get deploy options for the architecture
	deployOptions, err := appClient.GetArchitectureDeployOptions(archID)
	if err != nil {
		return nil, fmt.Errorf("failed to get deploy options: %w", err)
	}

	// Build services list using deploy options
	services := make([]apiModels.Service, 0, len(arch.Services))
	for _, svcRef := range arch.Services {
		// Find the corresponding deploy options service
		var svcDeployOpts *catalogTypes.DeployOptionsService
		for i := range deployOptions.Services {
			if deployOptions.Services[i].ID == svcRef.ID {
				svcDeployOpts = &deployOptions.Services[i]

				break
			}
		}

		if svcDeployOpts == nil {
			return nil, fmt.Errorf("deploy options not found for service '%s'", svcRef.ID)
		}

		svc, err := buildServiceEntryWithDeployOptions(appClient, svcRef.ID, svcDeployOpts)
		if err != nil {
			return nil, fmt.Errorf("failed to build service '%s': %w", svcRef.ID, err)
		}
		services = append(services, svc)
	}

	return &apiModels.CreateApplicationRequest{
		CatalogID: archID,
		Name:      appName,
		Services:  services,
		Version:   arch.Version,
	}, nil
}

// buildServicePayload builds the payload for a standalone service deployment.
func buildServicePayload(serviceID, appName string) (*apiModels.CreateApplicationRequest, error) {
	// Create application client for API calls
	appClient, err := catalogClient.NewApplicationClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create application client: %w", err)
	}

	// Get deploy options for the service
	deployOptions, err := appClient.GetServiceDeployOptions(serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get deploy options: %w", err)
	}

	svc, err := buildServiceEntryWithDeployOptions(appClient, serviceID, deployOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to build service: %w", err)
	}

	return &apiModels.CreateApplicationRequest{
		CatalogID: serviceID,
		Name:      appName,
		Services:  []apiModels.Service{svc},
		Version:   svc.Version,
	}, nil
}

// buildServiceEntryWithDeployOptions builds a single service entry with its components using deploy options.
func buildServiceEntryWithDeployOptions(appClient *catalogClient.ApplicationClient, serviceID string, deployOptions *catalogTypes.DeployOptionsService) (apiModels.Service, error) {
	// Build components list from deploy options
	components := make([]apiModels.Component, 0, len(deployOptions.Components))
	for _, compDeployOpt := range deployOptions.Components {
		// Get component configuration from argParams (provider-specific params)
		providerParams := extractComponentParamsForService(serviceID, compDeployOpt.Type, argParams)

		// Determine provider ID and get its params
		providerID, userParams := selectProviderFromDeployOptions(compDeployOpt, providerParams)
		if providerID == "" {
			return apiModels.Service{}, fmt.Errorf("no provider found for component type '%s'", compDeployOpt.Type)
		}

		// Find the selected provider to get its version
		var providerVersion string
		var providerFound bool
		for _, p := range compDeployOpt.Providers {
			if p.ID == providerID {
				providerVersion = p.Version
				providerFound = true

				break
			}
		}

		// If provider not found in deploy options, return error
		if !providerFound {
			return apiModels.Service{}, fmt.Errorf("provider '%s' not found in deploy options for component type '%s'", providerID, compDeployOpt.Type)
		}

		// Fetch schema and apply defaults, merging with user params
		componentParamsAny, err := applySchemaDefaults(appClient, compDeployOpt.Type, providerID, userParams)
		if err != nil {
			logger.Warningf("Failed to apply schema defaults for %s/%s: %v\n", compDeployOpt.Type, providerID, err)
			// Continue with user-provided params only
			componentParamsAny = make(map[string]any)
			for k, v := range userParams {
				componentParamsAny[k] = v
			}
		}

		components = append(components, apiModels.Component{
			ComponentType: compDeployOpt.Type,
			ProviderID:    providerID,
			Params:        componentParamsAny,
			Version:       providerVersion,
		})
	}

	// Extract service-level parameters (excluding component params)
	serviceParams := extractServiceParams(serviceID, deployOptions.Components, argParams)

	return apiModels.Service{
		CatalogID:  serviceID,
		Components: components,
		Params:     serviceParams,
		Version:    deployOptions.Version,
	}, nil
}

// extractServiceParams extracts service-level parameters from argParams, excluding component params.
// Format: {serviceID}.{param} -> {param}.
// Excludes: {serviceID}.{componentType}.{param} (those are component params).
// Nested dot-notation keys (e.g. backend.systemPrompt) are expanded into nested maps.
func extractServiceParams(serviceID string, components []catalogTypes.DeployOptionsComponent, allParams map[string]string) map[string]any {
	serviceParams := make(map[string]any)
	servicePrefix := serviceID + "."

	// Build a set of component types for this service.
	componentTypes := make(map[string]bool)
	for _, comp := range components {
		componentTypes[comp.Type] = true
	}

	for key, value := range allParams {
		after, ok := strings.CutPrefix(key, servicePrefix)
		if !ok {
			continue
		}

		// Check if this is a component parameter by seeing if it starts with a known component type.
		isComponentParam := isComponentParameter(after, componentTypes)

		// Only add to service params if it's not a component param.
		if !isComponentParam {
			setNestedParam(serviceParams, after, value)
		}
	}

	return serviceParams
}

// setNestedParam sets a value in a nested map using a dot-notation key.
// e.g. setNestedParam(m, "backend.systemPrompt", "hello") → m["backend"]["systemPrompt"] = "hello".
func setNestedParam(m map[string]any, dotKey string, value string) {
	parts := strings.SplitN(dotKey, ".", paramSplitParts)
	if len(parts) == 1 {
		m[dotKey] = value

		return
	}

	nested, ok := m[parts[0]].(map[string]any)
	if !ok {
		nested = make(map[string]any)
		m[parts[0]] = nested
	}

	setNestedParam(nested, parts[1], value)
}

// isComponentParameter checks if a parameter belongs to a component.
func isComponentParameter(param string, componentTypes map[string]bool) bool {
	for compType := range componentTypes {
		if strings.HasPrefix(param, compType+".") {
			return true
		}
	}

	return false
}

// extractComponentParamsForService extracts parameters for a specific component type from argParams.
// Supports provider-specific params:
// - Provider only: {componentType}.{providerID} (e.g., llm.vllm-cpu) - selects provider with defaults.
// - Provider with params: {componentType}.{providerID}.{param} (e.g., llm.vllm-cpu.model).
// - Service-specific: {serviceID}.{componentType}.{providerID}[.{param}] (e.g., chat.llm.vllm-cpu or chat.llm.vllm-cpu.model).
// Returns a map with provider as key and params as value.
func extractComponentParamsForService(serviceID string, componentType string, allParams map[string]string) map[string]map[string]string {
	providerParams := make(map[string]map[string]string)

	// Extract global component params: {componentType}.{providerID}[.{param}].
	extractProviderParams(componentType+".", allParams, providerParams)

	// Extract service-specific component params (these override global).
	extractProviderParams(serviceID+"."+componentType+".", allParams, providerParams)

	return providerParams
}

// extractProviderParams extracts provider parameters from allParams with the given prefix.
func extractProviderParams(prefix string, allParams map[string]string, providerParams map[string]map[string]string) {
	for key, value := range allParams {
		after, ok := strings.CutPrefix(key, prefix)
		if !ok {
			continue
		}

		// Split to get providerID and optional param.
		parts := strings.SplitN(after, ".", paramSplitParts)
		if len(parts) < 1 {
			continue
		}

		providerID := parts[0]
		if providerParams[providerID] == nil {
			providerParams[providerID] = make(map[string]string)
		}

		// If there's a param name, add it; otherwise just mark provider as selected.
		if len(parts) == expectedParamParts {
			paramName := parts[1]
			providerParams[providerID][paramName] = value
		}
	}
}

// selectProviderFromDeployOptions determines the provider ID for a component using deploy options.
// Priority:
// 1. If user provided provider-specific params (e.g., llm.vllm-cpu.model), use that provider.
// 2. For LLM and reranker components: Use vllm-spyre by default.
// 3. Default provider marked in deploy options.
// 4. First available provider.
func selectProviderFromDeployOptions(compDeployOpt catalogTypes.DeployOptionsComponent, providerParams map[string]map[string]string) (string, map[string]string) {
	// Check if user specified params for a specific provider.
	if providerID, params := findUserSpecifiedProvider(compDeployOpt, providerParams); providerID != "" {
		return providerID, params
	}

	// Special logic for LLM and reranker component types - prefer Spyre acceleration.
	if providerID := findSpyreProvider(compDeployOpt); providerID != "" {
		return providerID, make(map[string]string)
	}

	// Use default provider if marked.
	if providerID := findDefaultProvider(compDeployOpt); providerID != "" {
		return providerID, make(map[string]string)
	}

	// Fall back to first available provider.
	if len(compDeployOpt.Providers) > 0 {
		return compDeployOpt.Providers[0].ID, make(map[string]string)
	}

	return "", make(map[string]string)
}

// findUserSpecifiedProvider checks if user specified params for a specific provider.
func findUserSpecifiedProvider(compDeployOpt catalogTypes.DeployOptionsComponent, providerParams map[string]map[string]string) (string, map[string]string) {
	for providerID := range providerParams {
		// Verify this provider exists in deploy options.
		for _, p := range compDeployOpt.Providers {
			if p.ID == providerID {
				return providerID, providerParams[providerID]
			}
		}
	}

	return "", nil
}

// findSpyreProvider finds vllm-spyre provider if available for the component.
func findSpyreProvider(compDeployOpt catalogTypes.DeployOptionsComponent) string {
	for _, p := range compDeployOpt.Providers {
		if p.ID == "vllm-spyre" {
			return p.ID
		}
	}

	return ""
}

// findDefaultProvider finds the default provider marked in deploy options.
func findDefaultProvider(compDeployOpt catalogTypes.DeployOptionsComponent) string {
	for _, p := range compDeployOpt.Providers {
		if p.Default {
			return p.ID
		}
	}

	return ""
}

// applySchemaDefaults fetches the component provider schema and applies default values.
// User-provided params override defaults.
func applySchemaDefaults(appClient *catalogClient.ApplicationClient, componentType, providerID string, userParams map[string]string) (map[string]any, error) {
	// Fetch schema from API
	schema, err := appClient.GetComponentProviderParams(componentType, providerID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch schema: %w", err)
	}

	// Extract defaults from schema
	defaults := extractDefaultsFromSchema(schema)

	// Merge: start with defaults, override with user params
	result := make(map[string]any)
	maps.Copy(result, defaults)

	// Override with user-provided params (excluding 'provider' key)
	// Support nested parameters using dot notation (e.g., auth.password)
	for k, v := range userParams {
		if k != "provider" {
			setNestedParam(result, k, v)
		}
	}

	return result, nil
}

// extractDefaultsFromSchema extracts default values from a JSON schema.
func extractDefaultsFromSchema(schema map[string]any) map[string]any {
	defaults := make(map[string]any)

	// Check if schema has properties
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return defaults
	}

	// Extract default value for each property
	for propName, propValue := range properties {
		if propMap, ok := propValue.(map[string]any); ok {
			if defaultValue, hasDefault := propMap["default"]; hasDefault {
				defaults[propName] = defaultValue
			}
		}
	}

	return defaults
}

// Made with Bob
