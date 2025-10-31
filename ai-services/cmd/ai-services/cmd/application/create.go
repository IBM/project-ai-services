package application

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

const (
	applicationTemplatesPath = "applications/"
)

var (
	extraContainerReadinessTimeout = 5 * time.Minute
	templateName                   string
	envMutex                       sync.Mutex
)

var createCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Deploys an application",
	Long: `Deploys an application with the provided application name based on the template
		Arguments
		- [name]: Application name (Required)
	`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return errors.New("you must provide an application name")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]

		cmd.Printf("Creating application '%s' using template '%s'\n", appName, templateName)

		// set SMT level to target value, assuming it is running with root privileges (part of validation in bootstrap)
		err := setSMTLevel()
		if err != nil {
			return fmt.Errorf("failed to set SMT level: %w", err)
		}

		// Fetch all the application Template names
		appTemplateNames, err := helpers.FetchApplicationTemplatesNames()
		if err != nil {
			return fmt.Errorf("failed to list templates: %w", err)
		}

		var appTemplateName string

		if index := fetchAppTemplateIndex(appTemplateNames, templateName); index == -1 {
			return errors.New("provided template name is wrong. Please provide a valid template name")
		} else {
			appTemplateName = appTemplateNames[index]
		}

		applicationPodTemplatesPath := applicationTemplatesPath + appTemplateName

		tmpls, err := helpers.LoadAllTemplates(applicationPodTemplatesPath)
		if err != nil {
			return fmt.Errorf("failed to parse the templates: %w", err)
		}

		metadataFilePath := applicationPodTemplatesPath + "/metadata.yaml"

		// load metadata.yml to fetch the dependencies list
		appMetadata, err := helpers.LoadMetadata(metadataFilePath)
		if err != nil {
			return fmt.Errorf("failed to read app metadata: %w", err)
		}

		if err := verifyPodTemplateExists(tmpls, appMetadata); err != nil {
			return fmt.Errorf("failed to verify pod template: %w", err)
		}

		// ---- Validate Spyre card Requirements ----

		// calculate the required spyre cards
		reqSpyreCardsCount, err := calculateReqSpyreCards(utils.ExtractMapKeys(tmpls), applicationPodTemplatesPath)
		if err != nil {
			return err
		}

		// calculate the actual available spyre cards

		// TODO: calculate actual spyre cards count available (Assume it returns list of PCI Addresses)
		// For now using the below dummy values to proceed
		actualSpyreCardsCount := reqSpyreCardsCount
		pciAddresses := []string{}
		for i := range actualSpyreCardsCount {
			pciAddresses = append(pciAddresses, fmt.Sprintf("%d", i+1))
		}

		// validate spyre card requirements
		if err := validateSpyreCardRequirements(reqSpyreCardsCount, actualSpyreCardsCount); err != nil {
			return err
		}

		// ---- ! ----

		// podman connectivity
		runtime, err := podman.NewPodmanClient()
		if err != nil {
			return fmt.Errorf("failed to connect to podman: %w", err)
		}

		// Loop through all pod templates, render and run kube play
		cmd.Printf("Total Pod Templates to be processed: %d\n", len(tmpls))

		if err := executePodTemplates(runtime, appName, appMetadata.PodTemplateExecutions, tmpls, applicationPodTemplatesPath, pciAddresses); err != nil {
			return err
		}

		return nil
	},
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
		fmt.Printf("No SMT level specified in metadata.yaml. Keeping it to current level: %d\n", currentSMTlevel)
		return nil
	}

	// 3. Check if SMT level is already set to target value
	if currentSMTlevel == *targetSMTLevel {
		// already set
		fmt.Printf("SMT level is already set to %d\n", *targetSMTLevel)
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

	fmt.Printf("SMT level set to %d successfully.\n", *targetSMTLevel)

	return nil
}

func getTargetSMTLevel() (*int, error) {
	appTemplateNames, err := helpers.FetchApplicationTemplatesNames()
	if err != nil {
		return nil, fmt.Errorf("failed to list templates: %w", err)
	}

	var appTemplateName string

	if index := fetchAppTemplateIndex(appTemplateNames, templateName); index == -1 {
		return nil, errors.New("provided template name is wrong. Please provide a valid template name")
	} else {
		appTemplateName = appTemplateNames[index]
	}

	metadataFilePath := applicationTemplatesPath + appTemplateName + "/metadata.yaml"

	appMetadata, err := helpers.LoadMetadata(metadataFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read app metadata: %w", err)
	}
	return appMetadata.SMTLevel, nil
}

func init() {
	createCmd.Flags().StringVarP(&templateName, "template-name", "t", "", "Template name to use (required)")
	createCmd.MarkFlagRequired("template-name")
}

// fetchAppTemplateIndex -> Returns the index of app template if exists, otherwise -1
func fetchAppTemplateIndex(appTemplateNames []string, templateName string) int {
	appTemplateIndex := -1

	for index, appTemplateName := range appTemplateNames {
		if strings.EqualFold(appTemplateName, templateName) {
			appTemplateIndex = index
			break
		}
	}

	return appTemplateIndex
}

func verifyPodTemplateExists(tmpls map[string]*template.Template, appMetadata *helpers.AppMetadata) error {
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

func executePodTemplates(runtime runtime.Runtime, appName string, podTemplateExecutions [][]string,
	tmpls map[string]*template.Template, podTemplatesPath string, pciAddresses []string) error {

	globalParams := map[string]any{
		"AppName": appName,
		// Key -> container name
		// Value -> range of key-value env pairs
		"env": map[string]map[string]string{},
	}

	// looping over each layer of podTemplateExecutions
	for i, layer := range podTemplateExecutions {
		fmt.Printf("\n Executing Layer %d: %v\n", i+1, layer)
		fmt.Println("-------")
		var wg sync.WaitGroup
		errCh := make(chan error, len(layer))

		// for each layer, fetch all the pod Template Names and do the pod deploy
		for _, podTemplateName := range layer {
			wg.Add(1)
			go func(t string) {
				defer wg.Done()
				fmt.Printf("Processing template: %s...\n", podTemplateName)

				// Shallow Copy globalParams Map
				params := utils.CopyMap(globalParams)

				podTemplateFilePath := podTemplatesPath + "/" + podTemplateName

				// get the env params for a given pod
				env, err := returnEnvParamsForPod(podTemplateFilePath, &pciAddresses)
				if err != nil {
					errCh <- err
				}
				params["env"] = env

				podTemplate := tmpls[podTemplateName]

				var rendered bytes.Buffer
				if err := podTemplate.Execute(&rendered, params); err != nil {
					errCh <- err
				}

				// Wrap the bytes in a bytes.Reader
				reader := bytes.NewReader(rendered.Bytes())

				if err := deployPodAndReadinessCheck(runtime, podTemplateName, reader); err != nil {
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

		fmt.Printf("Layer %d completed\n", i+1)
	}

	return nil
}

func deployPodAndReadinessCheck(runtime runtime.Runtime, name string, body io.Reader) error {

	kubeReport, err := podman.RunPodmanKubePlay(body)
	if err != nil {
		return fmt.Errorf("failed pod creation: %w", err)
	}

	fmt.Printf("Successfully ran podman kube play for %s\n", name)

	for _, pod := range kubeReport.Pods {
		fmt.Printf("Performing Pod Readiness check...: %s\n", pod.ID)
		for _, container := range pod.Containers {
			fmt.Printf("Doing Container Readiness check...: %s\n", container.ID)

			// getting the Start Period set for a container
			startPeriod, err := helpers.FetchContainerStartPeriod(runtime, container.ID)
			if err != nil {
				return fmt.Errorf("fetching container start period failed: %w", err)
			}

			if startPeriod == -1 {
				fmt.Println("No container health check is set. Hence skipping readiness check")
				continue
			}

			// configure readiness timeout by appending start period with additional extra timeout
			readinessTimeout := startPeriod + extraContainerReadinessTimeout

			fmt.Printf("Setting the Waiting Readiness Timeout: %s\n", readinessTimeout)

			if err := helpers.WaitForContainerReadiness(runtime, container.ID, readinessTimeout); err != nil {
				return fmt.Errorf("readiness check failed!: %w", err)
			}
			fmt.Printf("Container: %s is ready\n", container.ID)
			fmt.Println("-------")
		}
		fmt.Printf("Pod: %s has been successfully deployed and ready!\n", pod.ID)
		fmt.Println("-------")
	}

	fmt.Println("-------\n-------")
	return nil
}

func validateSpyreCardRequirements(req int, actual int) error {
	if actual < req {
		return fmt.Errorf("insufficient spyre cards. Require: %d spyre cards to proceed", req)
	}
	return nil
}

func calculateReqSpyreCards(podTemplateFileNames []string, podTemplatesPath string) (int, error) {
	totalReqSpyreCounts := 0

	// Calculate Req Spyre Counts
	for _, podTemplateFileName := range podTemplateFileNames {

		podTemplateFilePath := podTemplatesPath + "/" + podTemplateFileName

		// load the pod Template
		podSpec, err := helpers.LoadPodTemplate(podTemplateFilePath)
		if err != nil {
			return totalReqSpyreCounts, fmt.Errorf("failed to load pod Template: %s with error: %w", podTemplateFilePath, err)
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

func returnEnvParamsForPod(podTemplateFilePath string, pciAddresses *[]string) (map[string]map[string]string, error) {
	env := map[string]map[string]string{}
	podSpec, err := helpers.LoadPodTemplate(podTemplateFilePath)
	if err != nil {
		return env, fmt.Errorf("failed to load pod Template: %s with error: %w", podTemplateFilePath, err)
	}

	podAnnotations := helpers.FetchPodAnnotations(*podSpec)
	podContainerNames := helpers.FetchContainerNames(*podSpec)

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
		// The pod doesnt require any spyre cards. // populate the given container with empty map
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
