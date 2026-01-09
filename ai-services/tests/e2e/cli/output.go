package cli

import (
	"fmt"
	"reflect"
	"strings"
	"regexp"
)

func ValidateBootstrapConfigureOutput(output string) error {
	required := []string{
		"LPAR configured successfully",
		"Bootstrap configuration completed successfully",
	}
	for _, r := range required {
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
func ValidateBootstrapFullOutput(output string) error {
	required := []string{
		"LPAR configured successfully",
		"All validations passed",
	}
	for _, r := range required {
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
			"Bootstrap and configure the infrastructure required for AI Services.",
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
		r := required.Index(i).String()
		if !strings.Contains(output, r) {
			return fmt.Errorf("help random command validation failed: missing '%s'", r)
		}
	}
	return nil
}

func ValidateApplicationPS(output string) error {
	if strings.Contains(output, "No Pods found") {
		return nil
	}

	if strings.Contains(output, "APPLICATION NAME") &&
		strings.Contains(output, "POD NAME") &&
		strings.Contains(output, "STATUS") {
		return nil
	}

	if strings.Contains(output, "APPLICATION NAME") &&
		strings.Contains(output, "POD ID") &&
		strings.Contains(output, "POD NAME") &&
		strings.Contains(output, "STATUS") &&
		strings.Contains(output, "CREATED") &&
		strings.Contains(output, "CONTAINERS") {
		return nil
	}

	return fmt.Errorf("invalid application ps output format")
}

func ValidateImageListOutput(output string) error {
	required := []string{
		"Container images for application template",
	}
	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("image list validation failed: missing '%s'", r)
		}
	}
	return nil
}

func ValidatePullImageOutput(output, templateName string) error {
	required := []string{
		"Downloading the images for the application",
	}
	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("pull image validation failed: missing '%s'", r)
		}
	}
	return nil
}

func ValidateStopAppOutput(output string) error {
	if !strings.Contains(output, "Proceeding to stop pods") {
		return fmt.Errorf("stop app validation failed")
	}
	return nil
}

func ValidatePodsExitedAfterStop(
	psOutput string,
	appName string,
	templateName string,
) error {

	mainPodsByType := map[string][]string{
		"rag": {
			"vllm-server",
			"milvus",
			"chat-bot",
		},
	}

	mainPods, ok := mainPodsByType[templateName]
	if !ok {
		return fmt.Errorf("unknown application type: %s", templateName)
	}

	isMainPod := func(pod string) bool {
		for _, m := range mainPods {
			if strings.Contains(pod, m) {
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
				"main pod %s not in Exited state for app %s (type: %s)",
				podName,
				appName,
				templateName,
			)
		}
	}

	fmt.Printf("[TEST] Main pods for %s are in Exited state\n", templateName)
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

	fmt.Println("[TEST] No pods present after delete")
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
			"Chatbot UI is available to use at",
			"Chatbot Backend is available to use at",
			"If you want to serve any more new documents via this RAG application, add them inside",
			fmt.Sprintf("/var/lib/ai-services/applications/%s/docs", appName),
			"If you want to do the ingestion again, execute below command",
			fmt.Sprintf("ai-services application start %s --pod=%s--ingest-docs", appName, appName),
			"In case if you want to clean the documents added to the db, execute below command",
			fmt.Sprintf("ai-services application start %s --pod=%s--clean-docs", appName, appName),
		)

		uiURLPattern := regexp.MustCompile(
			`Chatbot UI is available to use at\s+http://[0-9.]+:[0-9]+`,
		)
		if !uiURLPattern.MatchString(output) {
			return fmt.Errorf("application info validation failed: missing or invalid Chatbot UI URL")
		}

		backendURLPattern := regexp.MustCompile(
			`Chatbot Backend is available to use at\s+http://[0-9.]+:[0-9]+`,
		)
		if !backendURLPattern.MatchString(output) {
			return fmt.Errorf("application info validation failed: missing or invalid Chatbot Backend URL")
		}
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

func ValidateApplicationsTemplateCommandOutput(output string) error {
	type RequiredOutputs struct {
		rag []string
		ee  []string
	}
	requiredOutputs := RequiredOutputs{
		rag: []string{
			"Description: Retrieval Augmented Generation (RAG) application that combines a vector database, a large language model, and a retrieval mechanism to provide accurate and context-aware responses based on ingested documents.",
			"ui.port:  Host port for the RAG UI. If unspecified, a random available port is assigned. Specify a port number to use a custom value.",
			"backend.port:  Host port for the OpenAI-compatible RAG service. Defaults to unexposed; assign a port to enable external access.",
			"milvus.memoryLimit:  Sets the memory limit for the Milvus service(Default: 4Gi). Override by passing a value with a unit suffix (e.g., Mi, Gi).",
		},
	}

	arrOutput := processTemplateOutput(output)
	for _, value := range arrOutput {
		appName := getFirstWord(value)
		appName = strings.TrimSpace(appName)
		v := reflect.ValueOf(requiredOutputs)
		required := v.FieldByName(appName)

		for i := 0; i < required.Len(); i++ {
			r := required.Index(i).String()
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
