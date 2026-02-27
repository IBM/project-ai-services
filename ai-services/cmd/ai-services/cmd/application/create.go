package application

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	appBootstrap "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/bootstrap"
	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap"
	appFlags "github.com/project-ai-services/ai-services/internal/pkg/cli/constants/application"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/flagvalidator"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/image"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
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
	imagePullPolicy       image.ImagePullPolicy

	// openshift flags.
	timeout time.Duration
)

var createCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Deploys an application",
	Long: `Deploys an application with the provided application name based on the template
		Arguments
		- [name]: Application name (Required)
	`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
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

		// Create application instance using factory
		appFactory := application.NewFactory(vars.RuntimeFactory.GetRuntimeType())
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
			ImagePullPolicy:   imagePullPolicy,
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
	initCommonFlags()
	initPodmanFlags()
	initOpenShiftFlags()
}

func initCommonFlags() {
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

func initPodmanFlags() {
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
			"Supported for podman runtime only.\n",
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
			"Supported for podman runtime only.\n",
	)

	initializeImagePullPolicyFlag()

	// deprecated flags
	deprecatedPodmanFlags()
}

func initOpenShiftFlags() {
	createCmd.Flags().DurationVar(
		&timeout,
		appFlags.Create.Timeout,
		0, // default
		"Timeout for the operation (e.g. 10s, 2m, 1h).\n"+
			"Supported for openshift runtime only.\n",
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
			"Supported for podman runtime only.\n\n",
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
		AddCommonFlag(appFlags.Create.SkipValidation, nil).
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
	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{})
	if err := validators.ValidateAppTemplateExist(tp, templateName); err != nil {
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

	// Validate params against template values
	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{})
	_, err = tp.LoadValues(templateName, valuesFiles, argParams)
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

	return nil
}

// validateImagePullPolicyFlag validates the image-pull-policy flag.
func validateImagePullPolicyFlag(cmd *cobra.Command) error {
	imagePullPolicy = image.ImagePullPolicy(rawArgImagePullPolicy)
	if ok := imagePullPolicy.Valid(); !ok {
		return fmt.Errorf(
			"invalid value %q: must be one of %q, %q, %q",
			imagePullPolicy, image.PullAlways, image.PullNever, image.PullIfNotPresent,
		)
	}

	return nil
}

// Made with Bob
