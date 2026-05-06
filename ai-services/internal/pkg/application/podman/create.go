package podman

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"text/template"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	clipodman "github.com/project-ai-services/ai-services/internal/pkg/cli/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/image"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/specs"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	envMutex sync.Mutex
)

// Create deploys a new application based on a template.
func (p *PodmanApplication) Create(ctx context.Context, opts types.CreateOptions) error {
	// Proceed to create application
	logger.Infof("Creating application '%s' using template '%s'\n", opts.Name, opts.TemplateName)
	tp := templates.NewEmbedTemplateProvider(&assets.ApplicationFS)

	// validate whether the provided template name is correct
	if err := tp.AppTemplateExist(opts.TemplateName); err != nil {
		return err
	}

	tmpls, err := tp.LoadAllTemplates(opts.TemplateName)
	if err != nil {
		return fmt.Errorf("failed to parse the templates: %w", err)
	}

	// load metadata.yml to read the app metadata
	var appMetadata templates.AppMetadata
	if err := tp.LoadMetadata(opts.TemplateName, true, &appMetadata); err != nil {
		return fmt.Errorf("failed to read the app metadata: %w", err)
	}

	if err := p.verifyPodTemplateExists(tmpls, &appMetadata); err != nil {
		return fmt.Errorf("failed to verify pod template: %w", err)
	}

	// Check if pods already exists with the given application name
	existingPods, err := helpers.CheckExistingPodsForApplication(p.runtime, opts.Name)
	if err != nil {
		return fmt.Errorf("failed while checking existing pods for application: %w", err)
	}

	// if all the pods for given application are already deployed, just log and do not proceed further
	if len(existingPods) == len(tmpls) {
		logger.Infof("Pods for given app: %s are already deployed. Please use 'ai-services application ps %s' to see the pods deployed\n", opts.Name, opts.Name)

		return nil
	}

	// ---- Validate Spyre card Requirements ----
	pciAddresses, err := p.validateAndAllocateSpyreCards(opts.TemplateName, opts.Name, tmpls)
	if err != nil {
		return err
	}

	if err := p.prepareApplicationArtifacts(ctx, opts); err != nil {
		return err
	}

	// Loop through all pod templates, render and run kube play
	logger.Infof("Total Pod Templates to be processed: %d\n", len(tmpls))

	return p.deployApplication(ctx, opts, tmpls, &appMetadata, pciAddresses)
}

func (p *PodmanApplication) validateAndAllocateSpyreCards(templateName, appName string, tmpls map[string]*template.Template) ([]string, error) {
	tp := templates.NewEmbedTemplateProvider(&assets.ApplicationFS)

	reqSpyreCardsCount, err := p.calculateReqSpyreCards(tp, utils.ExtractMapKeys(tmpls), templateName, appName)
	if err != nil {
		return nil, fmt.Errorf("failed to calculateReqSpyreCards: %w", err)
	}

	if reqSpyreCardsCount == 0 {
		return nil, nil
	}

	// calculate the actual available spyre cards
	pciAddresses, err := helpers.FindFreeSpyreCards()
	if err != nil {
		return nil, fmt.Errorf("failed to find free Spyre Cards: %w", err)
	}

	actualSpyreCardsCount := len(pciAddresses)

	// validate spyre card requirements
	if err := p.validateSpyreCardRequirements(reqSpyreCardsCount, actualSpyreCardsCount); err != nil {
		return nil, err
	}

	return pciAddresses, nil
}

func (p *PodmanApplication) prepareApplicationArtifacts(ctx context.Context, opts types.CreateOptions) error {
	// Download Container Images
	if err := p.downloadImagesForTemplate(opts.TemplateName, opts.Name, opts.ImagePullPolicy); err != nil {
		return err
	}

	// Download models if flag is set to true(default: true)
	if !opts.SkipModelDownload {
		if err := p.downloadModels(ctx, opts.TemplateName, opts.Name); err != nil {
			return err
		}
	}

	return nil
}

func (p *PodmanApplication) deployApplication(ctx context.Context, opts types.CreateOptions, tmpls map[string]*template.Template, appMetadata *templates.AppMetadata, pciAddresses []string) error {
	logger.Infof("Total Pod Templates to be processed: %d\n", len(tmpls))

	s := spinner.New("Deploying application '" + opts.Name + "'...")
	s.Start(ctx)

	existingPods, err := helpers.CheckExistingPodsForApplication(p.runtime, opts.Name)
	if err != nil {
		return fmt.Errorf("failed while checking existing pods for application: %w", err)
	}

	tp := templates.NewEmbedTemplateProvider(&assets.ApplicationFS)

	// execute the pod Templates
	if err := p.executePodTemplates(tp, opts.Name, appMetadata, tmpls, pciAddresses, existingPods, opts.ValuesFiles, opts.ArgParams); err != nil {
		return err
	}

	s.Stop("Application '" + opts.Name + "' deployed successfully")

	logger.Infoln("-------")

	// print the next steps to be performed at the end of create
	if err := helpers.PrintNextSteps(tp, p.runtime, opts.Name, opts.TemplateName); err != nil {
		// do not want to fail the overall create if we cannot print next steps
		logger.Infof("failed to display next steps: %v\n", err)

		return nil //nolint:nilerr // intentionally swallow error for non-critical step
	}

	return nil
}

func (p *PodmanApplication) downloadModels(ctx context.Context, templateName, appName string) error {
	s := spinner.New("Downloading models as part of application creation...")
	s.Start(ctx)

	models, err := helpers.ListModels(templateName, appName)
	if err != nil {
		s.Fail("failed to list models")

		return err
	}

	logger.Infoln("Downloading models required for application template " + templateName + ":")

	for _, model := range models {
		s.UpdateMessage("Downloading model: " + model + "...")
		err = utils.Retry(vars.RetryCount, vars.RetryInterval, nil, func() error {
			return helpers.DownloadModel(model, utils.GetModelsPath())
		})
		if err != nil {
			s.Fail("failed to download model: " + model)

			return fmt.Errorf("failed to download model: %w", err)
		}
	}

	s.Stop("Model download completed.")

	return nil
}

func (p *PodmanApplication) verifyPodTemplateExists(tmpls map[string]*template.Template, appMetadata *templates.AppMetadata) error {
	flattenPodTemplateExecutions := utils.FlattenArray(appMetadata.PodTemplateExecutions)

	if len(flattenPodTemplateExecutions) != len(tmpls) {
		return errors.New("number of values specified in podTemplateExecutions under metadata.yml is mismatched. Please ensure all the pod template file names are specified")
	}

	// Make sure the podTemplateExecution mentioned in metadata.yaml is valid (corresponding pod template is present)
	for _, podTemplate := range flattenPodTemplateExecutions {
		if _, ok := tmpls[podTemplate]; !ok {
			return fmt.Errorf("value: %s specified in podTemplateExecutions under metadata.yml is invalid. Please ensure corresponding template file exists", podTemplate)
		}
	}

	return nil
}

func (p *PodmanApplication) validateSpyreCardRequirements(req int, actual int) error {
	if actual < req {
		return fmt.Errorf("insufficient spyre cards. Require: %d spyre cards to proceed", req)
	}

	return nil
}

func (p *PodmanApplication) calculateReqSpyreCards(tp templates.Template, podTemplateFileNames []string, appTemplateName, appName string) (int, error) {
	totalReqSpyreCounts := 0

	// Calculate Req Spyre Counts
	for _, podTemplateFileName := range podTemplateFileNames {
		// fetch pod spec
		podSpec, err := p.fetchPodSpec(tp, appTemplateName, podTemplateFileName, appName, nil, nil)
		if err != nil {
			return totalReqSpyreCounts, fmt.Errorf("failed to load pod Template: '%s' for appTemplate: '%s' with error: %w", podTemplateFileName, appTemplateName, err)
		}

		// check if pod already exists and skip counting if it does exists
		exists, err := p.runtime.PodExists(podSpec.Name)
		if err != nil {
			return totalReqSpyreCounts, fmt.Errorf("failed to check pod status: %w", err)
		}

		if exists {
			logger.Infof("Pod %s already exists, skipping spyre cards calculation", podSpec.Name, logger.VerbosityLevelDebug)

			continue
		}

		// fetch the spyreCount for all containers from the annotations
		spyreCount, _, err := p.fetchSpyreCardsFromPodAnnotations(podSpec.Annotations)
		if err != nil {
			return totalReqSpyreCounts, err
		}

		totalReqSpyreCounts += spyreCount
	}

	return totalReqSpyreCounts, nil
}

func (p *PodmanApplication) fetchPodSpec(tp templates.Template, appTemplateName, podTemplateFileName, appName string, valuesFiles []string, argParams map[string]string) (*models.PodSpec, error) {
	podSpec, err := tp.LoadPodTemplateWithValues(appTemplateName, podTemplateFileName, appName, valuesFiles, argParams)
	if err != nil {
		return nil, fmt.Errorf("failed to load pod Template: '%s' for appTemplate: '%s' with error: %w", podTemplateFileName, appTemplateName, err)
	}

	return podSpec, nil
}

func (p *PodmanApplication) fetchSpyreCardsFromPodAnnotations(annotations map[string]string) (int, map[string]int, error) {
	var spyreCards int
	// spyreCardContainerMap: Key -> containerName, Value -> SpyreCardCounts
	spyreCardContainerMap := map[string]int{}

	isSpyreCardAnnotation := func(annotation string) (string, bool) {
		matches := vars.SpyreCardAnnotationRegex.FindStringSubmatch(annotation)
		if matches == nil {
			return "", false
		}

		return matches[1], true
	}

	for annotationKey, val := range annotations {
		if containerName, ok := isSpyreCardAnnotation(annotationKey); ok {
			valInt, err := strconv.Atoi(val)
			if err != nil {
				return 0, spyreCardContainerMap, fmt.Errorf("failed to convert to int. Provided val: %s is not of int type", val)
			}
			// Replace with container name
			spyreCardContainerMap[containerName] = valInt
			spyreCards += valInt
		}
	}

	return spyreCards, spyreCardContainerMap, nil
}

func (p *PodmanApplication) downloadImagesForTemplate(templateName, appName string, imagePullPolicy image.ImagePullPolicy) error {
	// create Images struct and run with the specified policy
	img := &image.Images{
		Runtime:     p.runtime,
		App:         appName,
		AppTemplate: templateName,
	}

	return img.Run(imagePullPolicy)
}

func (p *PodmanApplication) executePodTemplates(tp templates.Template,
	appName string, appMetadata *templates.AppMetadata,
	tmpls map[string]*template.Template, pciAddresses []string, existingPods []string,
	valuesFiles []string, argParams map[string]string) error {
	// Load values for template rendering
	values, err := tp.LoadValues(appMetadata.Name, valuesFiles, argParams)
	if err != nil {
		return fmt.Errorf("failed to load params for application: %w", err)
	}

	globalParams := map[string]any{
		"AppName":         appName,
		"AppTemplateName": appMetadata.Name,
		"Version":         appMetadata.Version,
		"BaseDir":         utils.GetBaseDir(),
		"Values":          values,
		// Key -> container name
		// Value -> range of key-value env pairs
		"env": map[string]map[string]string{},
	}

	// looping over each layer of podTemplateExecutions
	for i, layer := range appMetadata.PodTemplateExecutions {
		logger.Infof("\n Executing Layer %d/%d: %v\n", i+1, len(appMetadata.PodTemplateExecutions), layer)
		logger.Infoln("-------")
		var wg sync.WaitGroup
		errCh := make(chan error, len(layer))

		// for each layer, fetch all the pod Template Names and do the pod deploy
		for _, podTemplateName := range layer {
			wg.Add(1)
			go func(t string) {
				defer wg.Done()
				if err := p.executePodTemplateLayer(tp, tmpls, globalParams, pciAddresses, existingPods, podTemplateName, appName, valuesFiles, argParams); err != nil {
					errCh <- err
				}
			}(podTemplateName)
		}

		wg.Wait()
		close(errCh)

		// collect all errors for this layer
		var errs []error
		for e := range errCh {
			errs = append(errs, fmt.Errorf("layer %d: %w", i+1, e))
		}

		// If an error exist for a given layer, then return (do not process further layers)
		if len(errs) > 0 {
			return errors.Join(errs...)
		}

		logger.Infof("Layer %d completed\n", i+1)
	}

	return nil
}

func (p *PodmanApplication) executePodTemplateLayer(tp templates.Template, tmpls map[string]*template.Template,
	globalParams map[string]any, pciAddresses []string, existingPods []string, podTemplateName, appName string,
	valuesFiles []string, argParams map[string]string) error {
	logger.Infof("'%s': Processing template...\n", podTemplateName)

	// Shallow Copy globalParams Map
	params := utils.CopyMap(globalParams)

	// fetch pod Spec
	podSpec, err := p.fetchPodSpec(tp, globalParams["AppTemplateName"].(string), podTemplateName, appName, valuesFiles, argParams)
	if err != nil {
		return err
	}

	if slices.Contains(existingPods, podSpec.Name) {
		logger.Infof("%s: Skipping pod deploy as '%s' it already exists", podTemplateName, podSpec.Name)

		return nil
	}

	// fetch annotations from pod Spec
	podAnnotations := p.fetchPodAnnotations(podSpec)

	// get the env params for a given pod
	env, err := p.returnEnvParamsForPod(podSpec, podAnnotations, &pciAddresses)
	if err != nil {
		return fmt.Errorf("'%s': Failed to fetch env params: %w", podTemplateName, err)
	}
	params["env"] = env

	podTemplate := tmpls[podTemplateName]

	var rendered bytes.Buffer
	if err := podTemplate.Execute(&rendered, params); err != nil {
		return fmt.Errorf("'%s': Failed to parse pod template: %w", podTemplateName, err)
	}

	// Wrap the bytes in a bytes.Reader
	reader := bytes.NewReader(rendered.Bytes())

	// Deploy the Pod and do Readiness check
	if err := clipodman.DeployPodAndReadinessCheck(p.runtime, podSpec, podTemplateName, reader, clipodman.ConstructPodDeployOptions(podAnnotations)); err != nil {
		return fmt.Errorf("'%s': Failed to deploy pod and do readiness check: %w", podTemplateName, err)
	}

	return nil
}

func (p *PodmanApplication) fetchPodAnnotations(podSpec *models.PodSpec) map[string]string {
	return specs.FetchPodAnnotations(*podSpec)
}

func (p *PodmanApplication) returnEnvParamsForPod(podSpec *models.PodSpec, podAnnotations map[string]string, pciAddresses *[]string) (map[string]map[string]string, error) {
	env := map[string]map[string]string{}
	podContainerNames := specs.FetchContainerNames(*podSpec)

	// populate env with empty map
	for _, containerName := range podContainerNames {
		env[containerName] = map[string]string{}
	}

	// fetch the spyre cards and spyre card count required for each container in a pod
	spyreCards, spyreCardContainerMap, err := p.fetchSpyreCardsFromPodAnnotations(podAnnotations)
	if err != nil {
		return env, err
	}

	if spyreCards == 0 {
		// The pod doesn't require any spyre cards. // populate the given container with empty map
		return env, nil
	}

	// Construct env for a given pod
	// Since this is a critical section as both requires pciAddresses and modifies -> wrap it in mutex
	envMutex.Lock()
	for container, spyreCount := range spyreCardContainerMap {
		if spyreCount != 0 {
			env[container] = map[string]string{string(constants.PCIAddressKey): utils.JoinAndRemove(pciAddresses, spyreCount, " ")}
		}
	}
	envMutex.Unlock()

	return env, nil
}
