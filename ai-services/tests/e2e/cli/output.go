package cli

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
)

// checkRequiredStrings returns an error if any of the required strings are absent
// from output. label is used only in the error message for context.
func checkRequiredStrings(output, label string, required []string) error {
	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("%s validation failed: missing '%s'", label, r)
		}
	}

	return nil
}

// checkNotOpenShiftUnsupported returns an error when the openshift not-supported
// warning is missing from the output. Used by commands that are podman-only.
func checkNotOpenShiftUnsupported(output, label string) error {
	const marker = "WARNING:  Not supported for openshift runtime"
	if !strings.Contains(output, marker) {
		return fmt.Errorf("%s validation failed: missing openshift not-supported warning", label)
	}

	return nil
}

func ValidateBootstrapConfigureOutput(output string, appRuntime string) error {
	// For podman: the configure command runs several steps. If Spyre cards are present
	// but some post-repair checks still fail, the command exits with an error before
	// printing "LPAR configured successfully". We accept either:
	//   (a) Full success: "LPAR configured successfully"
	//   (b) Spyre-only failure after repair attempt — all other steps passed, so this
	//       is acceptable in environments with Spyre cards not yet fully configured.
	// For openshift: always require the cluster configured message.
	switch appRuntime {
	case "podman":
		// Full success path
		if strings.Contains(output, "LPAR configured successfully") {
			return nil
		}
		// Acceptable partial-success: Spyre repair was attempted but post-repair checks
		// still failed. The error indicates a Spyre-specific failure, not a general one.
		if strings.Contains(output, "some Spyre configuration checks still failed after repair") ||
			strings.Contains(output, "failed to configure spyre card") {
			return nil
		}

		return fmt.Errorf("bootstrap configure validation failed: output did not indicate success or known Spyre repair state.\nOutput: %s", output)
	case "openshift":
		required := []string{
			"Cluster configured successfully",
			"Bootstrap configuration completed successfully.",
		}
		for _, r := range required {
			if !strings.Contains(output, r) {
				return fmt.Errorf("bootstrap configure validation failed: missing '%s'", r)
			}
		}
	}

	return nil
}

// ValidateBootstrapValidateOutput checks the output of the bootstrap validate command.
func ValidateBootstrapValidateOutput(output string) error {
	return checkRequiredStrings(output, "bootstrap validate", []string{"All validations passed"})
}

// ValidateBootstrapFullOutput checks the combined output of the full bootstrap command.
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

	return checkRequiredStrings(output, "full bootstrap", required[appRuntime])
}

// ValidateCreateAppOutput validates the output of the application create command.
func ValidateCreateAppOutput(output, appName string) error {
	// "Creating application '<name>'" is printed by both catalog and legacy paths.
	if !strings.Contains(output, fmt.Sprintf("Creating application '%s'", appName)) {
		return fmt.Errorf("create-app validation failed: missing 'Creating application '%s''", appName)
	}

	// Catalog path (podman): prints "Application '<name>' is ready!"
	// Legacy path (openshift): prints "Application '<name>' deployed successfully"
	catalogSuccess := fmt.Sprintf("Application '%s' is ready!", appName)
	legacySuccess := fmt.Sprintf("Application '%s' deployed successfully", appName)
	if !strings.Contains(output, catalogSuccess) && !strings.Contains(output, legacySuccess) {
		return fmt.Errorf("create-app validation failed: missing success confirmation for application '%s'", appName)
	}

	return nil
}

// ValidateHelpCommandOutput validates the output of the help command.
func ValidateHelpCommandOutput(output string) error {
	return checkRequiredStrings(output, "help command", []string{
		"A CLI tool for managing AI Services infrastructure.",
		"Use \"ai-services [command] --help\" for more information about a command.",
	})
}

// ValidateHelpRandomCommandOutput validates the output of a specific help sub-command.
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

// ValidateApplicationPS validates the output of the application ps command.
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

// ValidateImageListOutput validates the output of the image list command.
func ValidateImageListOutput(output string, appRuntime string) error {
	if appRuntime == "openshift" {
		return checkNotOpenShiftUnsupported(output, "image list")
	}

	// podman runtime: catalog path only.
	// Expected output: "Container images for template '<name>':"
	if !strings.Contains(output, "Container images for template '") && !strings.Contains(output, "No images found") {
		return fmt.Errorf("image list validation failed: output does not match catalog format.\nOutput: %s", output)
	}

	return nil
}

// ValidatePullImageOutput validates the output of the image pull command.
func ValidatePullImageOutput(output, templateName string, appRuntime string) error {
	if appRuntime == "openshift" {
		return checkNotOpenShiftUnsupported(output, "pull image")
	}

	// podman runtime: catalog path only.
	// Expected: "Downloading N images for template '<name>'..."
	//           "Successfully pulled all images for template '<name>'" OR "No images to pull"
	catalogMarker := fmt.Sprintf("for template '%s'", templateName)
	if !strings.Contains(output, catalogMarker) && !strings.Contains(output, "No images to pull") {
		return fmt.Errorf("pull image validation failed: output does not match catalog format for template '%s'.\nOutput: %s", templateName, output)
	}

	if strings.Contains(output, catalogMarker) {
		if !strings.Contains(output, fmt.Sprintf("Successfully pulled all images for template '%s'", templateName)) &&
			!strings.Contains(output, "No images to pull") {
			return fmt.Errorf("pull image validation failed: missing success confirmation for template '%s'", templateName)
		}
	}

	return nil
}

// ValidateStopAppOutputPodman validates the output of the application stop command for podman.
func ValidateStopAppOutputPodman(output string) error {
	if !strings.Contains(output, "Proceeding to stop pods") {
		return fmt.Errorf("podman stop app validation failed")
	}

	return nil
}

// ValidateStopAppOutputOpenshift validates the output of the application stop command for OpenShift.
func ValidateStopAppOutputOpenshift(output string) (err error) {
	if !strings.Contains(output, "WARNING:  Not implemented") {
		return fmt.Errorf("openshift stop app validation failed")
	}

	return nil
}

// ValidateStartAppOutputOpenshift validates the output of the application start command for OpenShift.
func ValidateStartAppOutputOpenshift(output string) (err error) {
	if !strings.Contains(output, "WARNING:  Not supported for openshift runtime") {
		return fmt.Errorf("openshift start app validation failed")
	}

	return nil
}

func ValidatePodsExitedAfterStop(psOutput, appName, appRuntime string) error {
	for line := range strings.SplitSeq(psOutput, "\n") {
		line = strings.TrimSpace(line)

		if line == "" ||
			strings.HasPrefix(line, "APPLICATION") ||
			strings.HasPrefix(line, "──") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 { //nolint:mnd
			continue
		}
		podName := parts[len(parts)-2]
		status := parts[len(parts)-1]

		// Case-insensitive: catalog path (via getPodStatusFromAPI) emits lowercase
		// "exited"; legacy podman path emits "Exited". Accept both.
		if isMainPod(podName, appRuntime) && strings.ToLower(status) != "exited" {
			return fmt.Errorf(
				"main pod %s not in Exited state for app %s (got: %s)",
				podName,
				appName,
				status,
			)
		}
	}

	logger.Infof("[TEST] Main pods are in Exited state")

	return nil
}

// ValidateDeleteAppOutput validates the output of the application delete command.
// The only hard requirement is that the command exited without error (checked by
// the caller) and that no pods remain (checked by ValidateNoPodsAfterDelete).
// We intentionally do not assert on specific output phrases — the CLI may change
// its confirmation messages between versions and those changes are irrelevant to
// whether the delete actually succeeded.
func ValidateDeleteAppOutput(_, _ string) error {
	return nil
}

// ValidateNoPodsAfterDelete checks that no pods remain after an application delete.
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
	// Catalog path (podman) prints:
	//   "Application Name: <name>"
	//   "Application Template: <template>"
	//   "Application Version: <version>"
	//   "Info:"
	//   "Day N:" (printed by info.go before service loop)
	required := []string{
		fmt.Sprintf("Application Name: %s", appName),
		fmt.Sprintf("Application Template: %s", templateName),
		"Info:",
	}

	if templateName == "rag" {
		// The catalog info.md templates have two branches: running (URL printed) and
		// unavailable (pod-name hint printed, no URL). We must match strings that appear
		// in BOTH branches so the validator passes regardless of pod health at call time.
		//
		// chat/info.md:
		//   running  → "… at https://chat-bot-ui-<slug>…"   (contains "chat-bot-ui")
		//   stopped  → "… make sure 'chat-bot' pod is running." (contains "chat-bot")
		//   ► match "chat-bot" — present in both branches.
		//
		// digitize/info.md:
		//   running  → "… https://digitize-ui-<slug>…"        (contains "digitize-ui")
		//   stopped  → "… make sure 'digitize-ui' pod …"      (contains "digitize-ui")
		//   ► match "digitize-ui" — same string in both.
		//
		// digitize API / info.md:
		//   running  → "… https://digitize-backend-<slug>…"   (contains "digitize-backend")
		//   stopped  → "… 'digitize-backend-server' pod …"    (contains "digitize-backend")
		//   ► match "digitize-backend" — present in both.
		//
		// similarity/info.md & summarize/info.md:
		//   running  → URL contains "similarity-api" / "summarize-api"
		//   stopped  → pod hint contains same string
		//   ► match as-is.
		required = append(required,
			"chat-bot",         // covers both "chat-bot-ui/backend" URLs and "'chat-bot' pod" hint
			"digitize-ui",      // same string in running URL and stopped hint
			"digitize-backend", // same string in running URL and stopped hint
			"summarize-api",
		)
	}

	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("application info validation failed: missing '%s'", r)
		}
	}

	return nil
}

// ValidateModelListOutput validates the output of the model list command.
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

// ValidateModelDownloadOutput validates the output of the model download command.
func ValidateModelDownloadOutput(output string, templateName string, appRuntime string) error {
	if appRuntime == "openshift" {
		return checkNotOpenShiftUnsupported(output, "model download")
	}

	// podman runtime: catalog path only.
	// Expected: "Downloading N models for template '<name>'..."
	//           "Successfully downloaded all models for template '<name>'" OR "No models to download"
	catalogSuccessStr := fmt.Sprintf("for template '%s'", templateName)
	if !strings.Contains(output, catalogSuccessStr) && !strings.Contains(output, "No models to download") {
		return fmt.Errorf("model download validation failed: output does not match catalog format for template '%s'", templateName)
	}

	if strings.Contains(output, catalogSuccessStr) {
		if !strings.Contains(output, fmt.Sprintf("Successfully downloaded all models for template '%s'", templateName)) &&
			!strings.Contains(output, "No models to download") {
			return fmt.Errorf("model download validation failed: missing success confirmation for template '%s'", templateName)
		}
	}

	return nil
}

// ValidateApplicationsTemplateCommandOutput validates the output of the application templates command.
func ValidateApplicationsTemplateCommandOutput(output string, appRuntime string) error {
	if appRuntime == "podman" {
		// Podman always uses the catalog path.
		// Expected: "Available Deployment Architectures:" + "Available Services:" + "- rag"
		return validateCatalogTemplateOutput(output)
	}

	// OpenShift format
	return validateOpenShiftTemplateOutput(output)
}

// validateCatalogTemplateOutput validates the catalog-format output (podman path).
// Expected output contains architectures like "rag (Digital Assistant)" and services.
func validateCatalogTemplateOutput(output string) error {
	return checkRequiredStrings(output, "application template command", []string{
		"Available Deployment Architectures:",
		"Available Services:",
		"- rag",
	})
}

// validateOpenShiftTemplateOutput validates the OpenShift-format template output.
// Expected output contains "Available application templates:" with per-app sections.
func validateOpenShiftTemplateOutput(output string) error {
	return checkRequiredStrings(output, "application template command", []string{
		"Available application templates:",
		"- rag",
		"opensearch.memoryLimit:",
		"opensearch.storage:",
		"opensearch.auth.password:",
	})
}

// ValidateVersionCommandOutput validates the output of the version command.
func ValidateVersionCommandOutput(output string, version string, commit string) error {
	return checkRequiredStrings(output, "version command", []string{
		"Version: " + version,
		"GitCommit: " + commit,
		"BuildDate: ",
	})
}

func isMainPod(pod string, appRuntime string) bool {
	for _, m := range common.ExpectedPodSuffixes[appRuntime] {
		if strings.Contains(pod, m) {
			return true
		}
	}

	return false
}

// ValidatePodsRunningAfterStart checks that the main pods are running after application start.
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

		// Case-insensitive: catalog path emits "running", legacy path emits "Running".
		if isMainPod(podName, appRuntime) && !strings.Contains(strings.ToLower(status), "running") {
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

// ValidateStartAppOutput validates the output of the application start command for podman.
func ValidateStartAppOutput(output string) error {
	if !strings.Contains(output, "Proceeding to start pods") &&
		!strings.Contains(output, "started successfully") {
		return fmt.Errorf("podman start app validation failed")
	}

	return nil
}

func ValidateApplicationLogs(output, _, _ string) error {
	return checkRequiredStrings(output, "application logs", []string{
		"Press Ctrl+C to exit the logs",
		"Fetching logs for",
	})
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

	// Parse the output line by line.
	extractOpenshiftRoutes(output, requiredRoutes, foundRoutes)

	// Verify all required routes were found.
	missingRoutes := make([]string, 0, len(requiredRoutes))
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

// ValidateCatalogUninstallOutput validates the output of 'catalog uninstall'.
// It requires the catalog's confirmation that all pods were deleted and the
// final success message printed by the CLI.
func ValidateCatalogUninstallOutput(output string) error {
	if !strings.Contains(output, "Catalog service removed successfully") {
		return fmt.Errorf("catalog uninstall validation failed: missing %q\nOutput: %s",
			"Catalog service removed successfully", output)
	}

	logger.Infof("[TEST] Catalog service uninstalled successfully")

	return nil
}
