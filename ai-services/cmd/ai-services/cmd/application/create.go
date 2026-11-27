package application

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/assets"
	appBootstrap "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/bootstrap"
	cmdcommon "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/common"
	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/appcreate"
	appFlags "github.com/project-ai-services/ai-services/internal/pkg/cli/constants/application"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/flagvalidator"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	cliutils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/image"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

const (
	// Polling configuration.
	pollInterval = 20 * time.Second
	pollTimeout  = 20 * time.Minute
)

// Variables for flags placeholder.
var (
	// common flags.
	templateName string
	rawArgParams []string
	argParams    map[string]string
	rawArgModels []string
	legacyCreate bool
	workerName   string

	// podman flags.
	skipModelDownload     bool
	skipImageDownload     bool
	skipChecks            []string
	valuesFiles           []string
	rawArgImagePullPolicy string

	// openshift flags.
	timeout time.Duration
)

var createCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Deploys an application",
	Long: `Deploys an application with the provided application name based on the template

Arguments:
  [name] : Application name (required)
`,
	Example: createExample(),
	Args:    cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// --runtime is required only for legacy mode; in the default flow the
		// runtime is resolved from the Worker record registered with the catalog.
		if legacyCreate && runtimeType == "" {
			return fmt.Errorf("required flag(s) \"runtime\" not set (required with --legacy)")
		}

		// Only validate platform support when runtime is explicitly given.
		if runtimeType != "" {
			if err := utils.CheckPodmanPlatformSupport(vars.RuntimeFactory.GetRuntimeType()); err != nil {
				return err
			}
		}

		// Build and run flag validator (runtime may be empty for default flow).
		flagValidator := buildFlagValidator()
		if err := flagValidator.Validate(cmd); err != nil {
			return err
		}

		appName := args[0]

		return utils.VerifyAppName(appName)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		ctx := cmd.Context()

		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		// When legacyCreate is true, use the older/stable code path.
		// This path requires --runtime (enforced in PreRunE above).
		if legacyCreate {
			rt := vars.RuntimeFactory.GetRuntimeType()

			if err := cmdcommon.DoBootstrapValidate(ctx, skipChecks); err != nil {
				return err
			}

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
		return createApp(ctx, appName)
	},
}

func createExample() string {
	return `  # Deploy using the local worker, with the models the template recommends
  ai-services application create rag --template rag

  # Deploy to a specific remote worker
  ai-services application create rag --template rag --worker node-1

  # Deploy a new model from the catalog for a component type
  ai-services application create rag --template rag --model llm=ibm-granite/granite-4.1-8b-fp8

  # Reuse a registered model by its name; a remote model is attached as a connector
  ai-services application create rag --template rag --model llm=my-granite --model reranker=my-reranker

  # Deploy in CPU mode: the provider is chosen with --params, and each model is the provider's default
  ai-services application create rag --template rag --params reranker.vllm-cpu=true,llm.vllm-cpu=true

  # Choose the provider with --params and the model with --model
  ai-services application create rag --template rag --params llm.vllm-cpu=true --model llm=ibm-granite/granite-4.1-8b-fp8

  # Deploy with legacy mode (requires --runtime)
  ai-services application create rag --template rag --legacy --runtime podman`
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

	createCmd.Flags().StringVar(&workerName, appFlags.Create.WorkerName, workerconstants.LocalWorkerName,
		"Name of a connected remote worker to deploy to.\n"+
			fmt.Sprintf("Defaults to %q (local deployment).\n", workerconstants.LocalWorkerName)+
			"Example: --worker node-1\n")

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

	createCmd.Flags().StringSliceVar(
		&rawArgModels,
		appFlags.Create.Model,
		[]string{},
		"Model to use for a component type of the application. Repeatable or comma-separated.\n\n"+
			"Format: --model <component_type>=<model>\n"+
			"- Example: --model llm=ibm-granite/granite-4.1-8b-fp8\n\n"+
			"<model> is looked up in this order:\n"+
			"- a catalog model ID: deploys that model anew\n"+
			"- the name of a registered model: reuses it; it must be running (local) or connected (remote).\n"+
			"  A remote model is attached as a connector\n\n"+
			"The provider is chosen with --params, e.g. --params llm.vllm-cpu=true. Without --model, a component\n"+
			"type uses the model the template recommends for its provider, else the provider's default model.\n"+
			"A provider cannot be chosen for a registered model: it brings its own.\n"+
			"Use \"ai-services application templates models --template <template>\" to see the component types and their models.\n",
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

	createCmd.Flags().BoolVar(&legacyCreate, appFlags.Create.Legacy, false, "Use legacy application create implementation")
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
// When --runtime is not provided (new default flow), the runtime is resolved from the
// Worker record by the catalog; all create flags are common-scoped or only validated
// in the legacy path, so an empty runtime is safe here.
func buildFlagValidator() *flagvalidator.FlagValidator {
	// Use the package-level runtimeType (may be "" in the default flow).
	builder := flagvalidator.NewFlagValidatorBuilder(runtimeTypes.RuntimeType(runtimeType))

	// Register common flags with their validation functions
	builder.
		AddCommonFlag(appFlags.Create.SkipValidation, cmdcommon.ValidateSkipChecksFlag).
		AddCommonFlag(appFlags.Create.Template, validateTemplateFlag).
		AddCommonFlag(appFlags.Create.Params, validateParamsFlag).
		AddCommonFlag(appFlags.Create.Model, validateModelFlag).
		AddCommonFlag(appFlags.Create.Values, validateValuesFlag).
		AddCommonFlag(appFlags.Create.Legacy, nil)

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
	// do template validation in legacy mode
	// In default mode, templates are validated against catalog API
	if legacyCreate {
		tp := templates.NewEmbedTemplateProvider(&assets.ApplicationFS)
		if err := tp.AppTemplateExist(templateName); err != nil {
			return err
		}

		return nil
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

	// do template validation in legacy mode
	// In default mode, params are validated against catalog API schemas
	if legacyCreate {
		// Validate params against template values (legacy mode)
		tp := templates.NewEmbedTemplateProvider(&assets.ApplicationFS)
		_, err = tp.LoadValues(templateName, nil, argParams)
		if err != nil {
			return fmt.Errorf("failed to load params: %w", err)
		}

		return nil
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

// validateModelFlag validates the --model flag. It only checks the syntax; the models are
// resolved against the catalog when the application is created.
func validateModelFlag(cmd *cobra.Command) error {
	if legacyCreate {
		return fmt.Errorf("--%s is not supported with --legacy", appFlags.Create.Model)
	}

	_, err := appcreate.ParseModelFlags(rawArgModels)

	return err
}

func createApp(ctx context.Context, appName string) error {
	// 1. Initialize catalog client
	appClient, err := catalogClient.NewApplicationClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create application client: %w", err)
	}

	// 2. Check if application already exists
	if err := checkApplicationExists(ctx, appClient, appName); err != nil {
		return err
	}

	// 3. Build the catalog API payload
	built, err := buildCatalogPayload(ctx, appClient, appName)
	if err != nil {
		return err
	}

	// 4. Create application via catalog API
	logger.Infof("Creating application '%s' using template '%s'...\n", appName, templateName)
	var resp *catalogClient.CreateApplicationResponse
	err = utils.Retry(ctx, vars.RetryCount, vars.RetryInterval, nil, func() error {
		var createErr error
		resp, createErr = appClient.CreateApplication(ctx, built)

		return createErr
	})
	if err != nil {
		return fmt.Errorf("failed to create application after %d retries: %w", vars.RetryCount, err)
	}

	logger.Infof("Application creation initiated (ID: %s)\n", resp.ID)

	// 5. Poll for application status
	return pollApplicationStatus(ctx, appClient, appName, resp.ID)
}

// checkApplicationExists checks if an application with the given name already exists.
func checkApplicationExists(ctx context.Context, appClient *catalogClient.ApplicationClient, appName string) error {
	existingApp, err := cliutils.GetAppByName(ctx, appClient, appName)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return err
	}

	if existingApp != nil {
		return fmt.Errorf("application with name '%s' already exists", appName)
	}

	return nil
}

// buildCatalogPayload builds the catalog API payload for the given template.
// It uses CatalogSource so that custom bundle templates (not in the embedded catalog)
// are resolved via the catalog API.
func buildCatalogPayload(ctx context.Context, appClient *catalogClient.ApplicationClient, appName string) (*catalogClient.CreateApplicationRequest, error) {
	source, err := catalogClient.NewCatalogSource(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialise catalog source: %w", err)
	}

	// Resolve the target runtime once here so all downstream calls use the same value.
	deployRT, err := resolveDeployRuntimeType(ctx)
	if err != nil {
		return nil, err
	}

	modelClient, err := catalogClient.NewModelClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create model client: %w", err)
	}

	models, err := appcreate.ParseModelFlags(rawArgModels)
	if err != nil {
		return nil, err
	}

	builder := appcreate.NewBuilder(appClient, appcreate.NewModelResolver(modelClient), appcreate.Options{
		Runtime:    deployRT,
		WorkerName: workerName,
		Params:     argParams,
		Models:     models,
	})

	// Try architecture first; fall through to service on not-found.
	arch, err := source.LoadArchitecture(ctx, templateName)
	if err == nil {
		return builder.BuildArchitecture(ctx, arch, appName)
	}

	svc, err := source.LoadService(ctx, templateName)
	if err == nil {
		return builder.BuildService(ctx, svc.ID, appName)
	}

	return nil, fmt.Errorf("template '%s' not found as architecture or service", templateName)
}

// resolveDeployRuntimeType returns the RuntimeType for the target worker by
// querying the catalog. The worker must be registered and connected; an error
// is returned otherwise so that the create call fails fast before any API work.
func resolveDeployRuntimeType(ctx context.Context) (runtimeTypes.RuntimeType, error) {
	wc, err := catalogClient.NewWorkerClient(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create worker client: %w", err)
	}

	workers, err := wc.ListWorkers(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list workers: %w", err)
	}

	for _, w := range workers {
		if strings.EqualFold(w.Name, workerName) {
			if w.RuntimeType == "" {
				return "", fmt.Errorf("worker %q has no runtime type", workerName)
			}

			return runtimeTypes.RuntimeType(w.RuntimeType), nil
		}
	}

	return "", fmt.Errorf("worker %q is not registered or not connected; run 'worker join' first", workerName)
}

// pollApplicationStatus polls the application status until it's ready or fails.
func pollApplicationStatus(ctx context.Context, appClient *catalogClient.ApplicationClient, appName, id string) error {
	logger.Infof("Waiting for application '%s' to be ready...\n", appName)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	timeout := time.After(pollTimeout)

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for application '%s' to be ready", appName)

		case <-ticker.C:
			app, err := appClient.GetApplicationWithRefresh(ctx, id)
			if err != nil {
				return fmt.Errorf("failed to get application status: %w", err)
			}

			done, err := handleApplicationStatus(ctx, app, appName)
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
func handleApplicationStatus(ctx context.Context, app *catalogTypes.Application, appName string) (bool, error) {
	switch app.Status {
	case "Running":
		logger.Infof("Application '%s' is ready!\n", appName)

		// Print next steps after successful deployment
		if err := printNextSteps(ctx, app); err != nil {
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
func printNextSteps(ctx context.Context, app *catalogTypes.Application) error {
	appClient, err := catalogClient.NewApplicationClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create application client: %w", err)
	}

	// Get full application details with services
	application, err := appClient.GetApplication(ctx, app.ID)
	if err != nil {
		return fmt.Errorf("failed to get application: %w", err)
	}

	// Fetch pod/container status for vars_file.yaml-driven status population
	appPS, err := appClient.GetApplicationPS(ctx, app.ID)
	if err != nil {
		return fmt.Errorf("failed to get application pods for next steps: %w", err)
	}

	// Every application is always deployed through a worker (WorkerID is NOT NULL).
	// Read the runtime type directly from the worker record — no CLI fallback needed.
	rt := runtimeTypes.RuntimeType(application.Worker.RuntimeType)

	// InstanceSlug is derived from the application UUID — same as at deploy time
	instanceSlug := catalogUtils.GenerateInstanceSlug(app.ID)

	logger.Infoln("\nNext Steps:")
	logger.Infoln("-------")

	for _, service := range application.Services {
		printServiceNextSteps(ctx, appClient, appPS, service, instanceSlug, rt)
	}

	// Print the info command regardless of whether any service has next.md
	logger.Infof("\n- For detailed endpoint information, use: `ai-services application info %s`\n", application.Name)

	return nil
}

// printServiceNextSteps renders and prints the next.md template for a single service.
func printServiceNextSteps(ctx context.Context, appClient *catalogClient.ApplicationClient, appPS *catalogTypes.ApplicationPSResponse, service catalogTypes.ApplicationService, instanceSlug string, rt runtimeTypes.RuntimeType) {
	params := map[string]string{}
	params["SERVICE_NAME"] = service.Type

	// Populate endpoint URLs from the service endpoints stored in the DB
	for _, endpoint := range service.Endpoints {
		urlType, urlTypeOk := endpoint["type"].(string)
		url, urlOk := endpoint["url"].(string)
		if urlTypeOk && urlOk {
			params[strings.ToUpper(urlType)+"_URL"] = url
		}
	}

	rawFiles, err := appClient.GetServiceSteps(ctx, service.CatalogID, string(rt))
	if err != nil {
		logger.Warningf("Failed to load next steps for service '%s': %v\n", service.CatalogID, err)

		return
	}

	// Populate status params generically from vars_file.yaml
	if err := populateStatusFromVarsFile(rawFiles, params, appPS.Services, instanceSlug, rt); err != nil {
		logger.WarningfCtx(ctx, "failed to populate status for '%s': %v\n", service.CatalogID, err)
	}

	tmpls, err := parseStepsTemplates(rawFiles)
	if err != nil {
		logger.Warningf("Failed to parse next steps templates for service '%s': %v\n", service.CatalogID, err)

		return
	}

	if err = printNextStepsMD(tmpls, params); err != nil {
		logger.Warningf("Failed to render next steps for service '%s': %v\n", service.CatalogID, err)
	}
}

// printNextStepsMD renders and prints the next.md template for a service.
func printNextStepsMD(tmpls map[string]*template.Template, params map[string]string) error {
	tmpl, ok := tmpls["next.md"]
	if !ok {
		// next.md doesn't exist for this service, skip
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

	return nil
}
