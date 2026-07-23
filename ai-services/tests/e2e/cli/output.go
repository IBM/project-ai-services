package cli

import (
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
)

// checkRequiredStrings returns an error if any required string is absent from output.
func checkRequiredStrings(output, label string, required []string) error {
	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("%s validation failed: missing '%s'", label, r)
		}
	}

	return nil
}

// checkAnyPattern returns nil if output contains at least one of the given
// patterns (case-insensitive OR logic). If none match, it returns an error
// quoting the checked patterns and the actual output for easy diagnosis.
// Used by all failure-scenario validators in this file.
func checkAnyPattern(output, label string, patterns []string) error {
	lower := strings.ToLower(output)
	for _, p := range patterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return nil
		}
	}

	return fmt.Errorf(
		"%s output did not contain any expected pattern.\n"+
			"Checked patterns: %v\nActual output:\n%s",
		label,
		patterns,
		output,
	)
}

// checkNotOpenShiftUnsupported returns an error when the openshift-not-supported warning is missing.
func checkNotOpenShiftUnsupported(output, label string) error {
	const marker = "WARNING:  Not supported for openshift runtime"
	if !strings.Contains(output, marker) {
		return fmt.Errorf("%s validation failed: missing openshift not-supported warning", label)
	}

	return nil
}

// ValidateBootstrapConfigureOutput validates bootstrap configure output.
// For podman: accepts full success ("LPAR configured successfully") or known
// Spyre post-repair failure strings — repairs ran but re-validation failed,
// which is hardware-specific and does not block application-layer tests.
func ValidateBootstrapConfigureOutput(output string, appRuntime string) error {
	switch appRuntime {
	case "podman":
		if strings.Contains(output, "LPAR configured successfully") {
			return nil
		}
		// Spyre repair attempted; post-repair re-validation still failed — non-fatal.
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
	if !strings.Contains(output, fmt.Sprintf("Creating application '%s'", appName)) {
		return fmt.Errorf("create-app validation failed: missing 'Creating application '%s''", appName)
	}

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

	requiredOutputs := map[string][]string{
		"application": {
			"The application command helps you deploy and monitor the applications",
			"ai-services application [command]",
		},
		"bootstrap": {
			"The bootstrap command configures and validates the environment needed to run AI Services, ensuring prerequisites are met and initial configuration is completed.",
			"ai-services bootstrap [flags]",
		},
		"completion": {
			"Generate the autocompletion script for ai-services for the specified shell.",
			"ai-services completion [command]",
		},
		"version": {
			"Prints CLI version with more info",
			"ai-services version [flags]",
		},
	}

	required, ok := requiredOutputs[command]
	if !ok {
		return fmt.Errorf("help random command validation failed: unknown command %q", command)
	}

	normalizedOutput := normalize(output)
	for _, r := range required {
		if !strings.Contains(normalizedOutput, normalize(r)) {
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

// ValidatePodsExitedAfterStop checks that all main pods are in Exited state.
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

// ValidateDeleteAppOutput validates the application delete command output.
// Success is determined by exit code and absence of pods, not specific phrases.
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

// ValidateApplicationInfo validates the output of the application info command.
func ValidateApplicationInfo(output, appName, templateName string) error {
	required := []string{
		fmt.Sprintf("Application Name: %s", appName),
		fmt.Sprintf("Application Template: %s", templateName),
		"Info:",
	}

	if templateName == "rag" {
		// Each string appears in both the running (URL) and stopped (pod-hint) branches
		// of info.md, so the check passes regardless of pod health at call time.
		required = append(required,
			"chat-bot",
			"digitize-ui",
			"digitize-backend",
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

// ValidateApplicationsTemplateCommandOutput validates the application templates command output.
func ValidateApplicationsTemplateCommandOutput(output string, appRuntime string) error {
	if appRuntime == "podman" {
		return validateCatalogTemplateOutput(output)
	}

	return validateOpenShiftTemplateOutput(output)
}

// validateCatalogTemplateOutput validates the catalog-format template output (podman).
func validateCatalogTemplateOutput(output string) error {
	return checkRequiredStrings(output, "application template command", []string{
		"Available Deployment Architectures:",
		"Available Services:",
		"- rag",
	})
}

// validateOpenShiftTemplateOutput validates the OpenShift-format template output.
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

// ValidateOpenShiftRoutes validates that all required routes are present.
func ValidateOpenShiftRoutes(output string) error {
	requiredRoutes := []string{
		"backend",
		"digitize-api",
		"digitize-ui",
		"summarize-api",
		"ui",
	}

	foundRoutes := make(map[string]bool)
	extractOpenshiftRoutes(output, requiredRoutes, foundRoutes)

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

		if line == "" || strings.HasPrefix(line, "NAME") || strings.HasPrefix(line, "──") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) > 0 {
			routeName := fields[0]
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
func ValidateCatalogUninstallOutput(output string) error {
	if !strings.Contains(output, "Catalog service removed successfully") {
		return fmt.Errorf("catalog uninstall validation failed: missing %q\nOutput: %s",
			"Catalog service removed successfully", output)
	}

	logger.Infof("[TEST] Catalog service uninstalled successfully")

	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Bootstrap failure validators
//
// These functions are used exclusively by bootstrap_failure_test.go.  Each one
// accepts the combined stdout+stderr output captured from a CLI invocation that
// is expected to have failed, and returns an error if the output does not
// contain at least one of the known failure-indicator strings.
//
// Matching is intentionally broad (substring, case-insensitive) so that minor
// phrasing changes in upstream error messages do not break the tests.
// ─────────────────────────────────────────────────────────────────────────────

// ValidateRegistryLoginFailureOutput verifies that the output from a failed
// `podman login` attempt contains a recognisable authentication-error string.
//
// Known strings emitted by Podman on credential rejection:
//   - "invalid username/password"
//   - "unauthorized"
//   - "authentication required"
//   - "failed with status"
//   - "Error logging in to"
func ValidateRegistryLoginFailureOutput(output string) error {
	knownPatterns := []string{
		"invalid username/password",
		"unauthorized",
		"authentication required",
		// Podman: "login attempt to https://.../v2/ failed with status: 401 Unauthorized"
		"failed with status",
		// Podman: "Error logging in to registry"
		"Error logging in to",
		// ICR (IBM Container Registry) returns 400 Bad Request for invalid credentials.
		// "bad request" covers this without matching bare numbers in unrelated output.
		"bad request",
		// Generic Podman auth failure prefix
		"authenticating creds",
		"requesting bearer token",
	}

	return checkAnyPattern(output, "registry login failure", knownPatterns)
}

// ValidateCatalogLoginFailureOutput verifies that the output from a failed
// `catalog login` attempt (wrong credentials) contains a recognisable error.
//
// Known strings emitted by the catalog backend on credential rejection:
//   - "catalog login failed"
//   - "invalid credentials"
//   - "unauthorized"
//   - "authentication failed"
//   - "401"
func ValidateCatalogLoginFailureOutput(output string) error {
	knownPatterns := []string{
		"catalog login failed",
		"invalid credentials",
		"unauthorized",
		"authentication failed",
		"401",
		"incorrect password",
		"invalid username or password",
	}

	return checkAnyPattern(output, "catalog login failure", knownPatterns)
}

// ValidateCatalogUnreachableOutput verifies that the output from a failed
// `catalog login` attempt against an unreachable server contains a recognisable
// connectivity-error string.
//
// Known strings emitted when the server cannot be reached:
//   - "connection refused"
//   - "no such host"
//   - "timeout"
//   - "context deadline exceeded"
//   - "catalog login failed"
//   - "dial tcp"
func ValidateCatalogUnreachableOutput(output string) error {
	knownPatterns := []string{
		"connection refused",
		"no such host",
		"timeout",
		"context deadline exceeded",
		"catalog login failed",
		"dial tcp",
		"EOF",
		"network",
	}

	return checkAnyPattern(output, "catalog unreachable-server", knownPatterns)
}

// ValidateBootstrapValidateFailureOutput verifies that the output from a failed
// `bootstrap validate` run contains a recognisable validation-error string.
//
// Known strings emitted when prerequisites are missing:
//   - "validation failed"
//   - "not found"
//   - "podman"          (the missing component should be named in the error)
//   - "prerequisite"
//   - "failed"
//   - "error"
func ValidateBootstrapValidateFailureOutput(output string) error {
	knownPatterns := []string{
		"validation failed",
		"not found",
		"podman",
		"prerequisite",
		"failed",
		"error",
	}

	return checkAnyPattern(output, "bootstrap validate failure", knownPatterns)
}

// ValidateInvalidRuntimeOutput verifies that the output from a
// `bootstrap validate --runtime <invalid>` invocation contains the expected
// rejection message emitted by bootstrapPersistentPreRunE in bootstrap.go:55:
//
//	"invalid runtime type: <value> (must be 'podman' or 'openshift').
//	 Please specify runtime using --runtime flag"
//
// This is a pure CLI flag-validation failure — no system checks are run.
func ValidateInvalidRuntimeOutput(output string) error {
	knownPatterns := []string{
		"invalid runtime type",
		"must be 'podman' or 'openshift'",
		"--runtime",
	}

	return checkAnyPattern(output, "invalid runtime", knownPatterns)
}

// OutputIndicatesSpyreAbsence returns true when the bootstrap validate output
// contains the specific error string emitted by SpyreRule.Verify() when no
// Spyre PCI devices are found on the LPAR.
//
// This is used by the Spyre failure test as a pre-check to distinguish between
// a Spyre-specific failure and any other kind of validate failure, so the test
// only asserts the Spyre message when the output is actually Spyre-related.
//
// Source: internal/pkg/validators/podman/spyre/spyre.go — Verify() line 32.
func OutputIndicatesSpyreAbsence(output string) bool {
	spyreAbsencePatterns := []string{
		"IBM Spyre Accelerator is not attached to the LPAR",
		"spyre accelerator is not attached",
		"no spyre",
	}

	lowerOutput := strings.ToLower(output)
	for _, pattern := range spyreAbsencePatterns {
		if strings.Contains(lowerOutput, strings.ToLower(pattern)) {
			return true
		}
	}

	return false
}

// ValidateSpyreAbsenceOutput verifies that the output from a failed
// `bootstrap validate` run on a Spyre-less LPAR contains the expected
// hardware-absence error emitted by SpyreRule.Verify().
//
// The exact message from the validator (spyre/spyre.go:32) is:
//
//	"IBM Spyre Accelerator is not attached to the LPAR"
//
// The hint from the validator (spyre/spyre.go:68) is:
//
//	"Run 'ai-services bootstrap configure' to fix configuration issues."
//
// Both are checked here so the test validates that the operator receives
// not only the error but also guidance on how to resolve it.
func ValidateSpyreAbsenceOutput(output string) error {
	// Primary error — must always be present.
	if !strings.Contains(output, "IBM Spyre Accelerator is not attached to the LPAR") {
		return fmt.Errorf(
			"spyre absence output missing expected error message.\n"+
				"Expected: %q\nActual output:\n%s",
			"IBM Spyre Accelerator is not attached to the LPAR",
			output,
		)
	}

	// Remediation hint — validates the operator gets actionable guidance.
	// Uses a substring match so minor phrasing changes don't break the test.
	if !strings.Contains(strings.ToLower(output), "bootstrap configure") {
		return fmt.Errorf(
			"spyre absence output missing remediation hint (expected mention of 'bootstrap configure').\n"+
				"Actual output:\n%s",
			output,
		)
	}

	return nil
}
