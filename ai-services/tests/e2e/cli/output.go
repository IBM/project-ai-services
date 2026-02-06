package cli

import (
	"fmt"
	"reflect"
	"strings"
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
	hasHeader :=
		strings.Contains(output, "APPLICATION NAME") &&
			strings.Contains(output, "POD NAME") &&
			strings.Contains(output, "STATUS")

	if !hasHeader {
		return fmt.Errorf("invalid application ps output format")
	}

	return nil
}

func ValidateStopAppOutput(output string) error {
	if !strings.Contains(output, "Proceeding to stop pods") {
		return fmt.Errorf("stop app validation failed")
	}

	return nil
}

func ValidatePodsExitedAfterStop(psOutput, appName string) error {
	mainPods := []string{
		"vllm-server",
		// "milvus",
		"chat-bot",
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
				"main pod %s not in Exited state for app %s",
				podName,
				appName,
			)
		}
	}

	fmt.Println("[TEST] Main pods are in Exited state")

	return nil
}

func ValidateDeleteAppOutput(output, appName string) error {
	for _, r := range []string{
		"Proceeding with deletion",
		"Application data cleaned up successfully",
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
