package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/bootstrap"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/specs"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	extraContainerReadinessTimeout = 5 * time.Minute
	envMutex                       sync.Mutex
	retryCount                     = 3
	retryInterval                  = 5 * time.Second
)

// Variables for flags placeholder
var (
	templateName      string
	skipModelDownload bool
	skipImageDownload bool
	skipChecks        []string
	rawArgParams      []string
	argParams         map[string]string
	valuesFiles       []string
	values            map[string]any
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
		values, err = tp.LoadValues(templateName, valuesFiles, argParams)
		if err != nil {
			return fmt.Errorf("failed to load params for application: %w", err)
		}

		appName := args[0]

		return utils.VerifyAppName(appName)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]
		ctx := context.Background()

		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		skip := helpers.ParseSkipChecks(skipChecks)
		if len(skip) > 0 {
			logger.Warningf("Skipping validation checks (skipped: %v)\n", skipChecks)
		}

		// Validate the LPAR before creating the application
		logger.Infof("Validating the LPAR environment before creating application '%s'...\n", appName)
		err := bootstrap.RunValidateCmd(skip)
		if err != nil {
			return fmt.Errorf("bootstrap validation failed: %w", err)
		}

		// podman connectivity
		runtime, err := podman.NewPodmanClient()
		if err != nil {
			return fmt.Errorf("failed to connect to podman: %w", err)
		}

		// Proceed to create application
		logger.Infof("Creating application '%s' using template '%s'\n", appName, templateName)

		// set SMT level to target value, assuming it is running with root privileges (part of validation in bootstrap)
		s := spinner.New("Checking SMT level")
		s.Start(ctx)
		err = setSMTLevel()
		if err != nil {
			s.Fail("failed to set SMT level")

			return fmt.Errorf("failed to set SMT level: %w", err)
		}
		s.Stop("SMT level configured successfully")

		tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{})

		// validate whether the provided template name is correct
		if err := validators.ValidateAppTemplateExist(tp, templateName); err != nil {
			return err
		}

		tmpls, err := tp.LoadAllTemplates(templateName + "/templates")
		if err != nil {
			return fmt.Errorf("failed to parse the templates: %w", err)
		}

		// load metadata.yml to read the app metadata
		appMetadata, err := tp.LoadMetadata(templateName)
		if err != nil {
			return fmt.Errorf("failed to read the app metadata: %w", err)
		}

		if err := verifyPodTemplateExists(tmpls, appMetadata); err != nil {
			return fmt.Errorf("failed to verify pod template: %w", err)
		}

		/*
			Pod Execution Logic:
			1. Check if pods already exists with the given application name
			2. If doesn't exists, proceed to create all pods
			3. Else, skip existing pods, and create missing pods
		*/

		existingPods, err := helpers.CheckExistingPodsForApplication(runtime, appName)
		if err != nil {
			return fmt.Errorf("failed while checking existing pods for application: %w", err)
		}

		// if all the pods for given application are already deployed, just log and do not proceed further
		if len(existingPods) == len(tmpls) {
			logger.Infof("Pods for given app: %s are already deployed. Please use 'ai-services application ps %s' to see the pods deployed\n", appName, appName)

			return nil
		}

		// ---- Validate Spyre card Requirements ----

		// calculate the required spyre cards of only those pods which are not deployed yet
		reqSpyreCardsCount, err := calculateReqSpyreCards(runtime, tp, utils.ExtractMapKeys(tmpls), templateName, appName)
		if err != nil {
			return fmt.Errorf("failed to calculateReqSpyreCards: %w", err)
		}

		var pciAddresses []string
		if reqSpyreCardsCount > 0 {
			// calculate the actual available spyre cards
			pciAddresses, err = helpers.FindFreeSpyreCards()
			if err != nil {
				return fmt.Errorf("failed to find free Spyre Cards: %w", err)
			}
			actualSpyreCardsCount := len(pciAddresses)

			// validate spyre card requirements
			if err := validateSpyreCardRequirements(reqSpyreCardsCount, actualSpyreCardsCount); err != nil {
				return err
			}
		}

		// ---- Download Container Images ----
		if err := downloadImagesForTemplate(runtime, templateName, appName); err != nil {
			return err
		}

		// Download models if flag is set to true(default: true)
		if !skipModelDownload {
			s = spinner.New("Downloading models as part of application creation...")
			s.Start(ctx)
			models, err := helpers.ListModels(templateName, appName)
			if err != nil {
				s.Fail("failed to list models")

				return err
			}
			logger.Infoln("Downloading models required for application template " + templateName + ":")
			for _, model := range models {
				s.UpdateMessage("Downloading model: " + model + "...")
				err = utils.Retry(retryCount, retryInterval, nil, func() error {
					return helpers.DownloadModel(model, vars.ModelDirectory)
				})
				if err != nil {
					s.Fail("failed to download model: " + model)

					return fmt.Errorf("failed to download model: %w", err)
				}
			}
			s.Stop("Model download completed.")
		}

		// ---- ! ----

		// Loop through all pod templates, render and run kube play
		logger.Infof("Total Pod Templates to be processed: %d\n", len(tmpls))

		s = spinner.New("Deploying application '" + appName + "'...")
		s.Start(ctx)
		// execute the pod Templates
		if err := executePodTemplates(runtime, tp, appName, appMetadata, tmpls, pciAddresses, existingPods); err != nil {
			return err
		}
		s.Stop("Application '" + appName + "' deployed successfully")

		logger.Infoln("-------")

		// print the next steps to be performed at the end of create
		if err := helpers.PrintNextSteps(runtime, appName, templateName); err != nil {
			// do not want to fail the overall create if we cannot print next steps
			logger.Infof("failed to display next steps: %v\n", err)

			return nil
		}

		return nil
	},
}

func downloadImagesForTemplate(runtime runtime.Runtime, templateName, appName string) error {
	// Fetch all images required for a given template
	images, err := helpers.ListImages(templateName, appName)
	if err != nil {
		return fmt.Errorf("failed to list container images: %w", err)
	}

	if !skipImageDownload {
		// Download container images if flag is set to false (default: false)
		logger.Infoln("Downloading container images required for application template " + templateName + ":")
		for _, image := range images {
			logger.Infoln("Downloading image: " + image + "...")
			if err := utils.Retry(retryCount, retryInterval, nil, func() error {
				return runtime.PullImage(image, nil)
			}); err != nil {
				return fmt.Errorf("failed to download image: %w", err)
			}
		}
		logger.Infoln("Downloading container images completed.")
	} else {
		logger.Infoln("Skipping container image download as per the flag --skip-image-download=true")
		// Verify that images exist locally
		lImages, err := runtime.ListImages()
		if err != nil {
			return fmt.Errorf("failed to list local images: %w", err)
		}
		// Populate a map with all existing local images (tags and digests)
		existingImages := make(map[string]bool)

		for _, lImage := range lImages {
			for _, tag := range lImage.RepoTags {
				existingImages[tag] = true
			}
			for _, digest := range lImage.RepoDigests {
				existingImages[digest] = true
			}
		}

		// Filter the requested images against the map
		var notfoundImages []string

		for _, image := range images {
			if !existingImages[image] {
				notfoundImages = append(notfoundImages, image)
			}
		}
		if len(notfoundImages) > 0 {
			return fmt.Errorf("some required images are not present locally: %v. Either pull the image manually or rerun create command without --skip-image-download flag", notfoundImages)
		}
		logger.Infoln("All required container images are present locally.")
	}

	return nil
}

func init() {
	createCmd.Flags().StringSliceVar(&skipChecks, "skip-validation", []string{},
		"Skip specific validation checks (comma-separated: root,rhel,rhn,power,numa)")
	createCmd.Flags().StringVarP(&templateName, "template", "t", "", "Application template to use (required)")
	_ = createCmd.MarkFlagRequired("template")
	// Add a flag for skipping image download
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

func getSMTLevel(output string) (int, error) {
	out := strings.TrimSpace(output)

	if !strings.HasPrefix(out, "SMT=") {
		return 0, fmt.Errorf("unexpected output: %s", out)
	}

	SMTLevelStr := strings.TrimPrefix(out, "SMT=")
	SMTlevel, err := strconv.Atoi(SMTLevelStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse SMT level: %w", err)
	}

	return SMTlevel, nil
}

func setSMTLevel() error {
	/*
		1. Fetch current SMT level
		2. Fetch the target SMT level
		3. Check if SMT level is already set to target value
		4. If not, set it to target value
		5. Verify again
	*/

	// 1. Fetch Current SMT level
	cmd := exec.Command("ppc64_cpu", "--smt")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to check current SMT level: %v, output: %s", err, string(out))
	}

	currentSMTlevel, err := getSMTLevel(string(out))
	if err != nil {
		return fmt.Errorf("failed to get current SMT level: %w", err)
	}

	// 2. Fetch the target SMT level
	targetSMTLevel, err := getTargetSMTLevel()
	if err != nil {
		return fmt.Errorf("failed to get target SMT level: %w", err)
	}

	if targetSMTLevel == nil {
		// No SMT level specified in metadata.yaml
		logger.Infof("No SMT level specified in metadata.yaml. Keeping it to current level: %d\n", currentSMTlevel)

		return nil
	}

	// 3. Check if SMT level is already set to target value
	if currentSMTlevel == *targetSMTLevel {
		// already set
		logger.Infof("SMT level is already set to %d\n", *targetSMTLevel)

		return nil
	}

	// 4. Set SMT level to target value
	arg := "--smt=" + strconv.Itoa(*targetSMTLevel)
	cmd = exec.Command("ppc64_cpu", arg)
	out, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set SMT level: %v, output: %s", err, string(out))
	}

	// 5. Verify again
	cmd = exec.Command("ppc64_cpu", "--smt")
	out, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to verify SMT level: %v, output: %s", err, string(out))
	}

	currentSMTlevel, err = getSMTLevel(string(out))
	if err != nil {
		return fmt.Errorf("failed to get SMT level after updating: %w", err)
	}

	if currentSMTlevel != *targetSMTLevel {
		return fmt.Errorf("SMT level verification failed: expected %d, got %d", targetSMTLevel, currentSMTlevel)
	}

	return nil
}

func getTargetSMTLevel() (*int, error) {
	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{})

	// validate whether the provided template name is correct
	if err := validators.ValidateAppTemplateExist(tp, templateName); err != nil {
		return nil, err
	}

	// load metadata.yml to read the app metadata
	appMetadata, err := tp.LoadMetadata(templateName)
	if err != nil {
		return nil, fmt.Errorf("failed to read the app metadata: %w", err)
	}

	return appMetadata.SMTLevel, nil
}

func verifyPodTemplateExists(tmpls map[string]*template.Template, appMetadata *templates.AppMetadata) error {
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

func executePodTemplates(runtime runtime.Runtime, tp templates.Template, appName string, appMetadata *templates.AppMetadata,
	tmpls map[string]*template.Template, pciAddresses []string, existingPods []string) error {
	globalParams := map[string]any{
		"AppName":         appName,
		"AppTemplateName": appMetadata.Name,
		"Version":         appMetadata.Version,
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
				logger.Infof("'%s': Processing template...\n", podTemplateName)

				// Shallow Copy globalParams Map
				params := utils.CopyMap(globalParams)

				// fetch pod Spec
				podSpec, err := fetchPodSpec(tp, templateName, podTemplateName, appName)
				if err != nil {
					errCh <- err
				}

				if slices.Contains(existingPods, podSpec.Name) {
					logger.Infof("%s: Skipping pod deploy as '%s' it already exists", podTemplateName, podSpec.Name)

					return
				}

				// fetch annotations from pod Spec
				podAnnotations := fetchPodAnnotations(podSpec)

				// get the env params for a given pod
				env, err := returnEnvParamsForPod(podSpec, podAnnotations, &pciAddresses)
				if err != nil {
					errCh <- fmt.Errorf("'%s': Failed to fetch env params: %w", podTemplateName, err)
				}
				params["env"] = env

				podTemplate := tmpls[podTemplateName]

				var rendered bytes.Buffer
				if err := podTemplate.Execute(&rendered, params); err != nil {
					errCh <- fmt.Errorf("'%s': Failed to parse pod template: %w", podTemplateName, err)
				}

				// Wrap the bytes in a bytes.Reader
				reader := bytes.NewReader(rendered.Bytes())

				// Deploy the Pod and do Readiness check
				if err := deployPodAndReadinessCheck(runtime, podSpec, podTemplateName, reader, constructPodDeployOptions(podAnnotations)); err != nil {
					errCh <- fmt.Errorf("'%s': Failed to deploy pod and do readiness check: %w", podTemplateName, err)
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

func deployPodAndReadinessCheck(runtime runtime.Runtime, podSpec *models.PodSpec, podTemplateName string, body io.Reader, opts map[string]string) error {
	kubeReport, err := podman.RunPodmanKubePlay(body, opts)
	if err != nil {
		return fmt.Errorf("failed pod creation: %w", err)
	}

	logger.Infof("'%s': Successfully ran podman kube play\n", podTemplateName, 2)

	// ---- Pod Readiness Checks ----
	/*
		Step1: Perform Containers Creation Check
		Step2: Perform Containers Readiness Check
	*/

	for _, pod := range kubeReport.Pods {
		pInfo, err := runtime.InspectPod(pod.ID)
		if err != nil {
			return fmt.Errorf("failed to do pod inspect for podID: '%s' with error: %w", pod.ID, err)
		}

		podName := pInfo.Name

		logger.Infof("'%s', '%s': Starting Pod Readiness check...\n", podTemplateName, podName)

		// Step1: ---- Containers Creation Check ----

		logger.Infof("'%s', '%s': Performing Containers Creation check for pod...\n", podTemplateName, podName)

		expectedContainerCount := len(specs.FetchContainerNames(*podSpec))
		containerCreationTimeout := 10 * time.Minute

		logger.Infof("'%s', '%s': Waiting for Containers Creation... Timeout set: %s\n", podTemplateName, podName, containerCreationTimeout)
		// wait for all containers for a given pod are created
		if err := helpers.WaitForContainersCreation(runtime, pod.ID, expectedContainerCount, containerCreationTimeout); err != nil {
			return fmt.Errorf("containers creation check failed for pod: '%s' with error: %w", pInfo.Name, err)
		}

		logger.Infof("'%s', '%s': Containers creation check for pod is completed\n", podTemplateName, podName)

		// Step2: ---- Containers Readiness Check ----

		for _, container := range pInfo.Containers {
			cInfo, err := runtime.InspectContainer(container.ID)
			if err != nil {
				return fmt.Errorf("failed to do container inspect for containerID: '%s' with error: %w", container.ID, err)
			}

			logger.Infof("'%s', '%s', '%s': Performing Container Readiness check...\n", podTemplateName, podName, cInfo.Name)

			// getting the Start Period set for a container
			startPeriod, err := helpers.FetchContainerStartPeriod(runtime, container.ID)
			if err != nil {
				return fmt.Errorf("fetching container: '%s' start period failed: %w", cInfo.Name, err)
			}

			if startPeriod == -1 {
				logger.Infof("No container health check is set for '%s'. Hence skipping readiness check\n", cInfo.Name, 2)

				continue
			}

			// configure readiness timeout by appending start period with additional extra timeout
			readinessTimeout := startPeriod + extraContainerReadinessTimeout

			logger.Infof("'%s', '%s', '%s': Waiting for Container Readiness... Timeout set: %s\n", podTemplateName, podName, cInfo.Name, readinessTimeout)

			if err := helpers.WaitForContainerReadiness(runtime, container.ID, readinessTimeout); err != nil {
				return fmt.Errorf("readiness check failed for container: '%s'!: %w", cInfo.Name, err)
			}
			logger.Infof("'%s', '%s', '%s': Readiness Check for the container is completed!\n", podTemplateName, podName, cInfo.Name)
			logger.Infoln("-------")
		}
		logger.Infof("'%s', '%s': Pod has been successfully deployed and ready!\n", podTemplateName, podName)
		logger.Infoln("-------")
	}

	logger.Infoln("-------\n-------")

	return nil
}

func validateSpyreCardRequirements(req int, actual int) error {
	if actual < req {
		return fmt.Errorf("insufficient spyre cards. Require: %d spyre cards to proceed", req)
	}

	return nil
}

func calculateReqSpyreCards(client *podman.PodmanClient, tp templates.Template, podTemplateFileNames []string, appTemplateName, appName string) (int, error) {
	totalReqSpyreCounts := 0

	// Calculate Req Spyre Counts
	for _, podTemplateFileName := range podTemplateFileNames {
		// fetch pod spec
		podSpec, err := fetchPodSpec(tp, appTemplateName, podTemplateFileName, appName)
		if err != nil {
			return totalReqSpyreCounts, fmt.Errorf("failed to load pod Template: '%s' for appTemplate: '%s' with error: %w", podTemplateFileName, appTemplateName, err)
		}

		// check if pod already exists and skip counting if it does exists
		exists, err := client.PodExists(podSpec.Name)
		if err != nil {
			return totalReqSpyreCounts, fmt.Errorf("failed to check pod status: %w", err)
		}

		if exists {
			logger.Infof("Pod %s already exists, skipping spyre cards calculation", podSpec.Name, 2)

			continue
		}

		// fetch the spyreCount for all containers from the annotations
		spyreCount, _, err := fetchSpyreCardsFromPodAnnotations(podSpec.Annotations)
		if err != nil {
			return totalReqSpyreCounts, err
		}

		totalReqSpyreCounts += spyreCount
	}

	return totalReqSpyreCounts, nil
}

func fetchSpyreCardsFromPodAnnotations(annotations map[string]string) (int, map[string]int, error) {
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

func fetchPodSpec(tp templates.Template, appTemplateName, podTemplateFileName, appName string) (*models.PodSpec, error) {
	podSpec, err := tp.LoadPodTemplateWithValues(appTemplateName, podTemplateFileName, appName, valuesFiles, argParams)
	if err != nil {
		return nil, fmt.Errorf("failed to load pod Template: '%s' for appTemplate: '%s' with error: %w", podTemplateFileName, appTemplateName, err)
	}

	return podSpec, nil
}

func fetchPodAnnotations(podSpec *models.PodSpec) map[string]string {
	return specs.FetchPodAnnotations(*podSpec)
}

func returnEnvParamsForPod(podSpec *models.PodSpec, podAnnotations map[string]string, pciAddresses *[]string) (map[string]map[string]string, error) {
	env := map[string]map[string]string{}
	podContainerNames := specs.FetchContainerNames(*podSpec)

	// populate env with empty map
	for _, containerName := range podContainerNames {
		env[containerName] = map[string]string{}
	}

	// fetch the spyre cards and spyre card count required for each container in a pod
	spyreCards, spyreCardContainerMap, err := fetchSpyreCardsFromPodAnnotations(podAnnotations)
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

func checkForPodStartAnnotation(podAnnotations map[string]string) string {
	if val, ok := podAnnotations[constants.PodStartAnnotationkey]; ok {
		if val == constants.PodStartOff || val == constants.PodStartOn {
			return val
		}
	}

	return ""
}

// fetchHostPortMappingFromAnnotation returns the hostPortMappings from the pod port annotations for a given pod template
// Returns:
//
//	hostPortMapping: Key -> containerPort, Value -> hostPort
//
// port annotation takes comma separated values of 'hostPort:containerPort' combination
// port annotation syntax: 'ai-services.io/ports': "<hostPart1>:<containerPort1>,<hostPart2>:<containerPort2>"
//
// Below are the hostPortMapping values based on different combinations
//  1. 'ai-services.io/ports': "8000:3000"
//     hostPortMapping = {"3000": "8000"}
//  2. 'ai-services.io/ports': "8000:3000, 8001:3001"
//     hostPortMapping = {"3000": "8000", "3001": "8001"}
//  3. 'ai-services.io/ports': ":3000"
//     hostPortMapping = {"3000": ""}
//  4. 'ai-services.io/ports': "3000:"
//     hostPortMapping = {} // Skip such values
//  5. 'ai-services.io/ports': "3000"
//     hostPortMapping = {"3000": ""}
func fetchHostPortMappingFromAnnotation(podAnnotations map[string]string) map[string]string {
	// key -> containerPort and value -> hostPort
	hostPortMapping := map[string]string{}

	portMappings, ok := podAnnotations[constants.PodPortsAnnotationKey]
	if !ok {
		// return empty map if port annotation is not present
		return hostPortMapping
	}

	portMapping := strings.SplitSeq(portMappings, ",")
	for p := range portMapping {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Find colon
		i := strings.Index(p, ":")
		if i == -1 {
			// No colon → whole thing is the containerPort
			hostPortMapping[p] = ""

			continue
		}

		// Before colon string is hostPort
		hostPort := strings.TrimSpace(p[:i])
		// After colon string is containerPort
		containerPort := strings.TrimSpace(p[i+1:])

		// If colon exists but NO value after the colon (containerPort) → then skip
		if containerPort == "" {
			continue
		}

		hostPortMapping[containerPort] = hostPort
	}

	return hostPortMapping
}

func constructPodDeployOptions(podAnnotations map[string]string) map[string]string {
	podStart := checkForPodStartAnnotation(podAnnotations)

	// construct start option
	podDeployOptions := map[string]string{}
	if podStart != "" {
		podDeployOptions["start"] = podStart
	}

	// construct publish option
	hostPortMappings := fetchHostPortMappingFromAnnotation(podAnnotations)
	podDeployOptions["publish"] = ""

	// loop over each of the hostPortMappings to construct the 'publish' option
	for containerPort, hostPort := range hostPortMappings {
		if hostPort == "0" {
			// if the host port is set to 0, then do not expose the particular containerPort
			continue
		}
		if hostPort != "" {
			// if the host port is present
			podDeployOptions["publish"] += hostPort + ":" + containerPort
		} else {
			// else just populate the containerPort, so that dynamically podman will populate
			podDeployOptions["publish"] += containerPort
		}
		podDeployOptions["publish"] += ","
	}

	return podDeployOptions
}
