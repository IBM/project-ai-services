package cli

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
)

func ValidateBootstrapConfigureOutput(output string, appRuntime string) error {
	required := map[string][]string{
		"podman": {
			"LPAR configured successfully",
			"Bootstrap configuration completed successfully",
		},
		"openshift": {
			"Cluster configured successfully",
			"Bootstrap configuration completed successfully.",
		},
	}
	for _, r := range required[appRuntime] {
		if !strings.Contains(output, r) {
			return fmt.Errorf("bootstrap configure validation failed: missing '%s'", r)
		}
	}

	return nil
}
func ValidateBootstrapValidateOutput(output string) error {
	required := []string{
		"All validations passed",
	}
	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("bootstrap validate validation failed: missing '%s'", r)
		}
	}

	return nil
}
func ValidateBootstrapFullOutput(output string, appRuntime string) error {
	required := map[string][]string{
		"podman": {
			"All validations passed",
			"LPAR bootstrapped successfully",
		},
		"openshift": {
			"Cluster configured successfully",
			"All validations passed",
		},
	}
	for _, r := range required[appRuntime] {
		if !strings.Contains(output, r) {
			return fmt.Errorf("full bootstrap validation failed: missing '%s'", r)
		}
	}

	return nil
}

func ValidateCreateAppOutput(output, appName string) error {
	required := []string{
		fmt.Sprintf("Creating application '%s'", appName),
		fmt.Sprintf("Application '%s' deployed successfully", appName),
	}

	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("create-app validation failed: missing '%s'", r)
		}
	}

	return nil
}

func ValidateHelpCommandOutput(output string) error {
	required := []string{
		"A CLI tool for managing AI Services infrastructure.",
		"Use \"ai-services [command] --help\" for more information about a command.",
	}
	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("help command validation failed: missing '%s'", r)
		}
	}

	return nil
}

func ValidateHelpRandomCommandOutput(command string, output string) error {
	normalize := func(s string) string {
		return strings.Join(strings.Fields(s), " ")
	}

	output = normalize(output)

	type RequiredOutputs struct {
		application []string
		bootstrap   []string
		completion  []string
		version     []string
	}

	requiredOutputs := RequiredOutputs{
		application: []string{
			"The application command helps you deploy and monitor the applications",
			"ai-services application [command]",
		},
		bootstrap: []string{
			"The bootstrap command configures and validates the environment needed to run AI Services, ensuring prerequisites are met and initial configuration is completed.",
			"ai-services bootstrap [flags]",
		},
		completion: []string{
			"Generate the autocompletion script for ai-services for the specified shell.",
			"ai-services completion [command]",
		},
		version: []string{
			"Prints CLI version with more info",
			"ai-services version [flags]",
		},
	}

	v := reflect.ValueOf(requiredOutputs)
	required := v.FieldByName(command)

	for i := 0; i < required.Len(); i++ {
		r := normalize(required.Index(i).String())
		if !strings.Contains(output, r) {
			return fmt.Errorf("help random command validation failed: missing '%s'", r)
		}
	}

	return nil
}

func ValidateApplicationPS(output string) error {
	if isNoPods(output) {
		return nil
	}

	if isMinimalPSFormat(output) {
		return nil
	}

	if isExtendedPSFormat(output) {
		return nil
	}

	return fmt.Errorf("invalid application ps output format:\n%s", output)
}

func isNoPods(output string) bool {
	return strings.Contains(output, "No Pods found")
}

func isMinimalPSFormat(output string) bool {
	return containsAll(output,
		"APPLICATION NAME",
		"POD NAME",
		"STATUS",
	)
}

func isExtendedPSFormat(output string) bool {
	return containsAll(output,
		"APPLICATION NAME",
		"POD ID",
		"POD NAME",
		"STATUS",
		"CREATED",
		"CONTAINERS",
	)
}

func containsAll(output string, fields ...string) bool {
	for _, field := range fields {
		if !strings.Contains(output, field) {
			return false
		}
	}

	return true
}

func ValidateImageListOutput(output string, appRuntime string) error {
	required := map[string][]string{
		"podman": {
			"Container images for application template",
		},
		"openshift": {
			"WARNING:  Not supported for openshift runtime",
		},
	}
	for _, r := range required[appRuntime] {
		if !strings.Contains(output, r) {
			return fmt.Errorf("image list validation failed: missing '%s'", r)
		}
	}

	return nil
}

func ValidatePullImageOutput(output, templateName string, appRuntime string) error {
	required := map[string][]string{
		"podman": {
			"Downloading the images for the application",
		},
		"openshift": {
			"WARNING:  Not supported for openshift runtime",
		},
	}
	for _, r := range required[appRuntime] {
		if !strings.Contains(output, r) {
			return fmt.Errorf("pull image validation failed: missing '%s'", r)
		}
	}

	return nil
}

func ValidateStopAppOutputPodman(output string) error {
	if !strings.Contains(output, "Proceeding to stop pods") {
		return fmt.Errorf("podman stop app validation failed")
	}

	return nil
}

func ValidateStopAppOutputOpenshift(output string) (err error) {
	if !strings.Contains(output, "WARNING:  Not implemented") {
		return fmt.Errorf("openshift stop app validation failed")
	}

	return nil
}

func ValidateStartAppOutputOpenshift(output string) (err error) {
	if !strings.Contains(output, "WARNING:  Not supported for openshift runtime") {
		return fmt.Errorf("openshift start app validation failed")
	}

	return nil
}

func ValidatePodsExitedAfterStop(psOutput, appName, appRuntime string) error {
	isMainPod := func(pod string) bool {
		for _, p := range common.ExpectedPodSuffixes[appRuntime] {
			if pod == p {
				return true
			}
		}

		return false
	}

	for line := range strings.SplitSeq(psOutput, "\n") {
		line = strings.TrimSpace(line)

		if line == "" ||
			strings.HasPrefix(line, "APPLICATION") ||
			strings.HasPrefix(line, "──") {
			continue
		}

		parts := strings.Fields(line)
		podName := parts[len(parts)-2]
		status := parts[len(parts)-1]

		if isMainPod(podName) && status != "Exited" {
			return fmt.Errorf(
				"main pod %s not in Exited state for app %s",
				podName,
				appName,
			)
		}
	}

	logger.Infof("[TEST] Main pods are in Exited state")

	return nil
}

func ValidateDeleteAppOutput(output, appName string) error {
	for _, r := range []string{
		"Proceeding with deletion",
	} {
		if !strings.Contains(output, r) {
			return fmt.Errorf("delete app validation failed: missing '%s'", r)
		}
	}

	return nil
}

func ValidateNoPodsAfterDelete(psOutput string) error {
	for line := range strings.SplitSeq(psOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" ||
			strings.HasPrefix(line, "APPLICATION") ||
			strings.HasPrefix(line, "──") ||
			strings.HasPrefix(line, "No Pods found") {
			continue
		}

		return fmt.Errorf("pods still exist after delete")
	}
	logger.Infof("[TEST] No pods present after delete")

	return nil
}

func ValidateApplicationInfo(output, appName, templateName string) error {
	required := []string{
		fmt.Sprintf("Application Name: %s", appName),
		fmt.Sprintf("Application Template: %s", templateName),
		"Version:",
		"Info:",
		"Day N:",
	}

	if templateName == "rag" {
		required = append(required,
			"Q&A Chatbot is available to use at ",
			"Q&A API is available to use at ",
			"Add documents to your RAG application using the Digitize Documents UI: ",
			"Digitize Documents API is available to use at ",
			"Use this endpoint for programmatic access and direct API integration.",
			"Summarize API is available to use at ",
			"Use this endpoint for document summarization via programmatic access.",
		)
	}

	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("application info validation failed: missing '%s'", r)
		}
	}

	return nil
}

func getFirstWord(s string) string {
	firstSpaceIndex := strings.Index(s, " ")
	if firstSpaceIndex != -1 {
		return s[:firstSpaceIndex]
	}
	// If no space is found, the string is a single word, so return an empty string
	return s
}

func processTemplateOutput(output string) []string {
	output = strings.ReplaceAll(output, "\nAvailable application templates:\n", "")
	output = strings.ReplaceAll(output, "\n\n", "\n")
	arrOutput := strings.Split(output, "- ")
	arrOutput = arrOutput[1:]

	return arrOutput
}

func ValidateModelListOutput(output string, templateName string, appRuntime string) error {
	requiredOutputs := map[string]map[string][]string{
		"podman": {
			"rag": {
				"BAAI/bge-reranker-v2-m3",
				"ibm-granite/granite-embedding-278m-multilingual",
				"ibm-granite/granite-3.3-8b-instruct",
			},
			"rag-cpu": {
				"BAAI/bge-reranker-v2-m3",
				"ibm-granite/granite-embedding-278m-multilingual",
				"ibm-granite/granite-3.3-8b-instruct",
			},
		},
		"openshift": {
			"rag": {
				"WARNING:  Not supported for openshift runtime",
			},
		},
	}

	required, ok := requiredOutputs[appRuntime][templateName]
	if !ok {
		return fmt.Errorf("model list validation failed")
	}

	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("model list validation failed: expected model '%s' not found in output", r)
		}
	}

	return nil
}

func ValidateModelDownloadOutput(output string, templateName string, appRuntime string) error {
	required := map[string][]string{
		"podman": {
			fmt.Sprintf("Downloaded Models in application template%s:", templateName),
			"Downloading model ibm-granite/granite-embedding-278m-multilingual to /var/lib/ai-services/models",
			"Downloading model ibm-granite/granite-3.3-8b-instruct to /var/lib/ai-services/models",
			"Downloading model BAAI/bge-reranker-v2-m3 to /var/lib/ai-services/models",
			"Model downloaded successfully",
		},
		"openshift": {
			"WARNING:  Not supported for openshift runtime",
		},
	}
	for _, r := range required[appRuntime] {
		if !strings.Contains(output, r) {
			return fmt.Errorf("model download validation failed: missing '%s'", r)
		}
	}

	return nil
}

func ValidateApplicationsTemplateCommandOutput(output string, appRuntime string) error {
	requiredOutputs := map[string]map[string][]string{
		"podman": {
			"rag": {
				"Description: Retrieval Augmented Generation (RAG) application that combines a vector database, a large language model, and a retrieval mechanism to provide accurate and context-aware responses based on ingested documents.",
				"ui.port:  Host port for the RAG UI. If unspecified, a random available port is assigned. Specify a port number to use a custom value.",
				"backend.port:  Host port for the OpenAI-compatible RAG service. Defaults to unexposed; assign a port to enable external access.",
				"summarize.port:  Host port for the Summarize API. If unspecified, a random available port is assigned. Specify a port number to use a custom value.",
				"similarity.port:  Host port for the Similarity Search API. If unspecified, a random available port is assigned. Specify a port number to use a custom value.",
				"digitize.port:  Host port for the DIGITIZE API. If unspecified, a random available port is assigned. Specify a port number to use a custom value.",
				"digitizeUi.port:  Host port for the DIGITIZE UI. If unspecified, a random available port is assigned. Specify a port number to use a custom value.",
				"opensearch.memoryLimit:  Sets the memory limit for the Opensearch service(Default: 8Gi). Override by passing a value with a unit suffix (e.g., Mi, Gi).",
				"opensearch.auth.password:  Password for OpenSearch authentication. Must be at least 15 characters and contain at least one uppercase letter, one lowercase letter, one digit, and one special character. Avoid common words, predictable patterns, or dictionary terms. Use this to override the default admin password.",
			},
			"rag-cpu": {
				"Description: Retrieval Augmented Generation (RAG) application that combines a vector database, a large language model, and a retrieval mechanism to provide accurate and context-aware responses based on ingested documents.",
				"ui.port:  Host port for the RAG UI. If unspecified, a random available port is assigned. Specify a port number to use a custom value.",
				"backend.port:  Host port for the OpenAI-compatible RAG service. Defaults to unexposed; assign a port to enable external access.",
				"summarize.port:  Host port for the Summarize API. If unspecified, a random available port is assigned. Specify a port number to use a custom value.",
				"similarity.port:  Host port for the Similarity Search API. If unspecified, a random available port is assigned. Specify a port number to use a custom value.",
				"digitize.port:  Host port for the DIGITIZE API. If unspecified, a random available port is assigned. Specify a port number to use a custom value.",
				"digitizeUi.port:  Host port for the DIGITIZE UI. If unspecified, a random available port is assigned. Specify a port number to use a custom value.",
				"opensearch.memoryLimit:  Sets the memory limit for the Opensearch service(Default: 8Gi). Override by passing a value with a unit suffix (e.g., Mi, Gi).",
				"opensearch.auth.password:  Password for OpenSearch authentication. Must be at least 15 characters and contain at least one uppercase letter, one lowercase letter, one digit, and one special character. Avoid common words, predictable patterns, or dictionary terms. Use this to override the default admin password.",
			},
		},
		"openshift": {
			"rag": {
				"Description: Retrieval Augmented Generation (RAG) application that combines a vector database, a large language model, and a retrieval mechanism to provide accurate and context-aware responses based on ingested documents.",
				"opensearch.memoryLimit:  Sets the memory limit for the Opensearch service(Default: 8Gi). Override by passing a value with a unit suffix (e.g., Mi, Gi).",
				"opensearch.storage:  Sets the storage limit for the Opensearch service(Default: 10Gi). Override by passing a value with a unit suffix (e.g., Mi, Gi).",
				"opensearch.auth.password:  Password for OpenSearch authentication. Must be at least 15 characters and contain at least one uppercase letter, one lowercase letter, one digit, and one special character. Avoid common words, predictable patterns, or dictionary terms. Use this to override the default admin password.",
			},
		},
	}

	arrOutput := processTemplateOutput(output)
	for _, value := range arrOutput {
		appName := getFirstWord(value)
		appName = strings.TrimSpace(appName)
		required, ok := requiredOutputs[appRuntime][appName]
		if !ok {
			continue
		}

		for _, r := range required {
			if !strings.Contains(output, r) {
				return fmt.Errorf("application template command validation failed for app:%s missing '%s'", appName, r)
			}
		}
	}

	return nil
}

func ValidateVersionCommandOutput(output string, version string, commit string) error {
	required := []string{
		"Version: " + version,
		"GitCommit: " + commit,
		"BuildDate: ",
	}
	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("version command validation failed: missing '%s'", r)
		}
	}

	return nil
}

func isMainPod(pod string, appRuntime string) bool {
	// Skip clean-docs and ingest-docs pods as they are not in running state after start
	if strings.Contains(pod, "clean-docs") || strings.Contains(pod, "ingest-docs") {
		return false
	}

	for _, m := range common.ExpectedPodSuffixes[appRuntime] {
		if strings.Contains(pod, m) {
			return true
		}
	}

	return false
}

func ValidatePodsRunningAfterStart(psOutput, appName, appRuntime string) error {
	for line := range strings.SplitSeq(psOutput, "\n") {
		line = strings.TrimSpace(line)

		if line == "" ||
			strings.HasPrefix(line, "APPLICATION") ||
			strings.HasPrefix(line, "──") {
			continue
		}

		parts := strings.Fields(line)
		podName := parts[len(parts)-2]
		status := parts[len(parts)-1]

		if isMainPod(podName, appRuntime) && !strings.Contains(status, "Running") {
			return fmt.Errorf(
				"main pod %s not running after start for app %s",
				podName,
				appName,
			)
		}
	}

	logger.Infof("[TEST] Main pods are running after start")

	return nil
}

func ValidateStartAppOutput(output string) error {
	if !strings.Contains(output, "Proceeding to start pods") &&
		!strings.Contains(output, "started successfully") {
		return fmt.Errorf("podman start app validation failed")
	}

	return nil
}

func ValidateApplicationLogs(output, podName, containerNameOrID string) error {
	required := []string{
		"Press Ctrl+C to exit the logs",
		"Fetching logs for",
	}

	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("application logs validation failed: missing '%s'", r)
		}
	}

	return nil
}

func GetApplicationNameFromPSOutput(psOutput string) (appName string) {
	lines := strings.Split(psOutput, "\n")
	parts := strings.Fields(lines[2])
	if len(parts) > 0 {
		return parts[0]
	}

	return ""
}

// ValidateOpenShiftRoutes validates the presence of required routes in the OpenShift runtime.
func ValidateOpenShiftRoutes(output string) error {
	requiredRoutes := []string{
		"backend",
		"digitize-api",
		"digitize-ui",
		"summarize-api",
		"ui",
	}

	foundRoutes := make(map[string]bool)

	// Parse the output line by line
	extractOpenshiftRoutes(output, requiredRoutes, foundRoutes)

	// Verify all required routes were found
	var missingRoutes []string
	for _, route := range requiredRoutes {
		if !foundRoutes[route] {
			missingRoutes = append(missingRoutes, route)
		}
	}

	if len(missingRoutes) > 0 {
		return fmt.Errorf("missing required routes: %v", missingRoutes)
	}

	logger.Infof("[TEST] All 5 required OpenShift routes validated successfully")

	return nil
}

func extractOpenshiftRoutes(output string, requiredRoutes []string, foundRoutes map[string]bool) {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and header lines
		if line == "" || strings.HasPrefix(line, "NAME") || strings.HasPrefix(line, "──") {
			continue
		}

		// Extract the route name (first field)
		fields := strings.Fields(line)
		if len(fields) > 0 {
			routeName := fields[0]
			// Check if this route is one of the required ones
			for _, required := range requiredRoutes {
				if routeName == required {
					foundRoutes[required] = true

					break
				}
			}
		}
	}
}
