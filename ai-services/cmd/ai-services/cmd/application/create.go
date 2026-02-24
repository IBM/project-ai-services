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
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/image"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
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
		var err error
		// validate params flag
		if len(rawArgParams) > 0 {
			argParams, err = utils.ParseKeyValues(rawArgParams)
			if err != nil {
				return fmt.Errorf("error validating params flag: %w", err)
			}
		}

		// validate values files
		for _, vf := range valuesFiles {
			if !utils.FileExists(vf) {
				return fmt.Errorf("values file '%s' does not exist", vf)
			}
		}

		tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{})
		if err := validators.ValidateAppTemplateExist(tp, templateName); err != nil {
			return err
		}

		// load the values and verify params arg values passed
		_, err = tp.LoadValues(templateName, valuesFiles, argParams)
		if err != nil {
			return fmt.Errorf("failed to load params for application: %w", err)
		}

		// validate ImagePullPolicy
		imagePullPolicy = image.ImagePullPolicy(rawArgImagePullPolicy)
		if ok := imagePullPolicy.Valid(); !ok {
			return fmt.Errorf(
				"invalid --image-pull-policy %q: must be one of %q, %q, %q",
				imagePullPolicy, image.PullAlways, image.PullNever, image.PullIfNotPresent,
			)
		}

		appName := args[0]

		return utils.VerifyAppName(appName)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		ctx := context.Background()

		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		//nolint:godox
		// TODO: Integrate Bootstrap validate for Openshift in create flow once ready. For now skipping it for Openshift runtime.
		if vars.RuntimeFactory.GetRuntimeType() != types.RuntimeTypeOpenShift {
			if err := doBootstrapValidate(); err != nil {
				return err
			}
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
	bootstrapInstance, err := factory.Create()
	if err != nil {
		return fmt.Errorf("failed to create bootstrap instance: %w", err)
	}

	if err := bootstrapInstance.Validate(skip); err != nil {
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
	createCmd.Flags().StringSliceVar(&skipChecks, "skip-validation", []string{}, skipCheckDesc)

	createCmd.Flags().StringVarP(&templateName, "template", "t", "", "Application template to use (required)")
	_ = createCmd.MarkFlagRequired("template")

	createCmd.Flags().StringSliceVar(
		&rawArgParams,
		"params",
		[]string{},
		"Inline parameters to configure the application.\n\n"+
			"Format:\n"+
			"- Comma-separated key=value pairs\n"+
			"- Example: --params key1=value1,key2=value2\n\n"+
			"- Use \"ai-services application templates\" to view the list of supported parameters\n\n"+
			"Precedence:\n"+
			"- When both --values and --params are provided, --params overrides --values\n",
	)
}

func initPodmanFlags() {
	createCmd.Flags().BoolVar(
		&skipImageDownload,
		"skip-image-download",
		false,
		"Skip container image pull/download during application creation\n\n"+
			"Use this only if the required container images already exist locally\n"+
			"Recommended for air-gapped or pre-provisioned environments\n\n"+
			"Warning:\n"+
			"- If set to true and images are missing → command will fail\n"+
			"- If left false in air-gapped environments → pull/download attempt will fail\n",
	)
	createCmd.Flags().BoolVar(
		&skipModelDownload,
		"skip-model-download",
		false,
		"Skip model download during application creation\n\n"+
			"Use this if local models already exist at /var/lib/ai-services/models/\n"+
			"Recommended for air-gapped networks\n\n"+
			"Warning:\n"+
			"- If set to true and models are missing → command will fail\n"+
			"- If left false in air-gapped environments → download attempt will fail\n",
	)

	createCmd.Flags().StringArrayVarP(
		&valuesFiles,
		"values",
		"f",
		[]string{},
		"Specify values.yaml files to override default template values\n\n"+
			"Usage:\n"+
			"- Can be provided multiple times\n"+
			"- Example: --values custom1.yaml --values custom2.yaml\n"+
			"- Or shorthand: -f custom1.yaml -f custom2.yaml\n\n"+
			"Notes:\n"+
			"- Files are applied in the order provided\n"+
			"- Later files override earlier ones\n",
	)

	initializeImagePullPolicyFlag()

	// deprecated flags
	deprecatedPodmanFlags()
}

func initOpenShiftFlags() {
	createCmd.Flags().DurationVar(
		&timeout,
		"timeout",
		0, // default
		"Timeout for the operation (e.g. 10s, 2m, 1h). Supported for runtime set to openshift only.",
	)
}

func initializeImagePullPolicyFlag() {
	createCmd.Flags().StringVar(
		&rawArgImagePullPolicy,
		"image-pull-policy",
		string(image.PullIfNotPresent),
		"Image pull policy for container images required for given application. Supported values: Always, Never, IfNotPresent.\n\n"+
			"Determines when the container runtime should pull the image from the registry:\n"+
			" - Always: pull the image every time from the registry before running\n"+
			" - Never: never pull; use only local images\n"+
			" - IfNotPresent: pull only if the image isn't already present locally \n\n"+
			"Defaults to 'IfNotPresent' if not specified\n\n"+
			"In air-gapped environments → specify 'Never'\n\n",
	)
}

func deprecatedPodmanFlags() {
	if err := createCmd.Flags().MarkDeprecated("skip-image-download", "use --image-pull-policy instead"); err != nil {
		panic(fmt.Sprintf("Failed to mark 'skip-image-download' flag deprecated. Err: %v", err))
	}
}

// Made with Bob
