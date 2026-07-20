package openshift

import (
	"context"
	"fmt"
	"time"

	"helm.sh/helm/v4/pkg/chart"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeOpenshift "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// DeployCatalog deploys the catalog service to OpenShift using the Helm chart.
func DeployCatalog(ctx context.Context, opts catalogutils.OpenShiftConfigureOptions) error {
	logger.Infof("Deploying catalog service to OpenShift in namespace '%s'\n", opts.Namespace)

	tp := templates.NewEmbedTemplateProvider(&assets.CatalogFS, "")

	// Step 1: Fetch the operation timeout from metadata (or use the user-supplied timeout)
	timeout, err := getOperationTimeout(ctx, tp, opts.Timeout)
	if err != nil {
		return err
	}

	// Step 2: Load the Chart from assets/catalog/openshift
	chartData, err := loadChart(ctx, tp)
	if err != nil {
		return err
	}

	// Step 3: Create OpenShift runtime for the catalog namespace
	runtime, err := runtimeOpenshift.NewOpenshiftClientWithNamespace(opts.Namespace)
	if err != nil {
		return fmt.Errorf("failed to create OpenShift client: %w", err)
	}

	// Step 4: Collect and hash password (if secret doesn't exist)
	passwordHash, err := catalogutils.CollectAndHashPassword(runtime)
	if err != nil {
		return err
	}

	// Step 5: Prepare values with argument parameters
	// Pass runtime so generateArgParams can skip re-generating the DB password
	// when catalog-db-secret already exists (avoids mismatch with existing PVC data).
	values, err := prepareValues(tp, runtime, passwordHash)
	if err != nil {
		return err
	}

	// Step 6: Deploy the catalog using Helm
	if err := deployCatalogHelm(ctx, chartData, timeout, values, opts.Namespace); err != nil {
		return err
	}

	logger.Infoln("-------")

	// Step 7: Print next steps with route URLs
	if err := helpers.PrintNextSteps(tp, runtime, catalogconstants.CatalogAppName, catalogconstants.CatalogAppTemplate); err != nil {
		logger.Infof("failed to display next steps: %v\n", err)

		return nil //nolint:nilerr // intentionally swallow error for non-critical step
	}

	return nil
}

func getOperationTimeout(ctx context.Context, tp templates.Template, timeout time.Duration) (time.Duration, error) {
	// populate the operation timeout if it's either not set or set negatively
	if timeout <= 0 {
		var appMetadata templates.AppMetadata
		if err := tp.LoadMetadata(catalogconstants.CatalogAppTemplate, false, &appMetadata); err != nil {
			return 0, fmt.Errorf("failed to read the catalog metadata: %w", err)
		}

		timeout = appMetadata.Openshift.Timeout
	}

	return timeout, nil
}

func loadChart(ctx context.Context, tp templates.Template) (chart.Charter, error) {
	s := spinner.New("Loading the Helm chart for catalog...")

	s.Start(ctx)
	chart, err := tp.LoadChart(catalogconstants.CatalogAppTemplate)
	if err != nil {
		s.Fail("failed to load the Helm chart")

		return nil, fmt.Errorf("failed to load the chart: %w", err)
	}
	s.Stop("Loaded the Helm chart successfully")

	return chart, nil
}

func prepareValues(tp templates.Template, rt *runtimeOpenshift.OpenshiftClient, passwordHash string) (map[string]any, error) {
	// Generate argument parameters
	argParams, err := generateArgParams(rt, passwordHash)
	if err != nil {
		return nil, fmt.Errorf("failed to generate arg params: %w", err)
	}

	// Load values from chart with overrides
	values, err := tp.LoadValues(catalogconstants.CatalogAppTemplate, nil, argParams)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare values: %w", err)
	}

	return values, nil
}

func generateArgParams(rt *runtimeOpenshift.OpenshiftClient, passwordHash string) (map[string]string, error) {
	argParams := make(map[string]string)
	argParams[configure.ArgParamAdminPasswordHash] = passwordHash

	dbSecretExists, err := rt.SecretExists(catalogconstants.CatalogDBSecretName)
	if err != nil {
		return nil, fmt.Errorf("failed to check db secret existence: %w", err)
	}

	if !dbSecretExists {
		dbPassword, err := utils.GenerateRandomPassword()
		if err != nil {
			return nil, fmt.Errorf("failed to generate database password: %w", err)
		}

		argParams[configure.ArgParamDBPassword] = dbPassword
	}

	return argParams, nil
}

func deployCatalogHelm(ctx context.Context, chartData chart.Charter, timeout time.Duration, values map[string]any, namespace string) error {
	s := spinner.New("Deploying catalog to OpenShift...")

	s.Start(ctx)

	// Create Helm client for the catalog namespace
	helmClient, err := helm.NewHelm(namespace)
	if err != nil {
		s.Fail("failed to create Helm client")

		return fmt.Errorf("failed to create Helm client: %w", err)
	}

	// Check if the catalog release exists
	releaseExists, err := helmClient.IsReleaseExist(catalogconstants.CatalogAppName)
	if err != nil {
		s.Fail("failed to check existing release")

		return fmt.Errorf("failed to check existing release: %w", err)
	}

	if !releaseExists {
		logger.Infof("Release '%s' does not exist, proceeding with install...", catalogconstants.CatalogAppName)
		err = helmClient.Install(catalogconstants.CatalogAppName, chartData, &helm.InstallOpts{Values: values, Timeout: timeout})
	} else {
		logger.Infof("Release '%s' already exists, proceeding with upgrade...", catalogconstants.CatalogAppName)
		err = helmClient.Upgrade(catalogconstants.CatalogAppName, chartData, &helm.UpgradeOpts{Values: values, Timeout: timeout})
	}

	if err != nil {
		s.Fail("failed to deploy catalog")

		return fmt.Errorf("failed to deploy catalog: %w", err)
	}

	s.Stop("Catalog deployed successfully")

	return nil
}

// Made with Bob
