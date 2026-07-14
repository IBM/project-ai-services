package podman

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/cli"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
	"github.com/project-ai-services/ai-services/tests/e2e/config"
)

// klogPrefixRe matches the klog line prefix added by logger.Infoln in CLI mode:
//
//	I0707 06:11:25.123456   12345 logger.go:270] <actual content>
//	W0707 ...  (warnings)
//	E0707 ...  (errors)
//
// The table rows are logged via logger.Infoln so every line in CombinedOutput
// is prefixed by klog. We strip the prefix before any regex matching.
var klogPrefixRe = regexp.MustCompile(`^[IWEF]\d{4}\s+\d{2}:\d{2}:\d{2}\.\d+\s+\d+\s+\S+:\d+\]\s`)

// ansiEscapeRe matches ANSI/VT100 escape sequences emitted by lipgloss/charmbracelet.
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripKlogPrefix removes the klog timestamp/source prefix from a line if present.
// If the line does not match the prefix pattern it is returned unchanged.
func stripKlogPrefix(line string) string {
	if loc := klogPrefixRe.FindStringIndex(line); loc != nil {
		return line[loc[1]:]
	}

	return line
}

// stripANSI removes ANSI escape sequences from a string.
// The charmbracelet/lipgloss table renderer emits escape codes for bold/colour
// that would otherwise break regex matching.
func stripANSI(s string) string {
	return ansiEscapeRe.ReplaceAllString(s, "")
}

func TestPodman(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Pod Status Suite")
}

type PodInspect struct {
	RestartPolicy string `json:"RestartPolicy"`
	Containers    []struct {
		Id   string `json:"Id"`
		Name string `json:"Name"`
	} `json:"Containers"`
}
type ContainerInspect struct {
	State struct {
		RestartCount int `json:"RestartCount"`
	} `json:"State"`
	Config struct {
		Image string `json:"Image"`
	} `json:"Config"`
}

type OpenShiftPod struct {
	Spec struct {
		RestartPolicy string `json:"restartPolicy"`
	} `json:"spec"`
	Status struct {
		ContainerStatuses []struct {
			Name         string `json:"name"`
			RestartCount int    `json:"restartCount"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

var (
	separatorRe = regexp.MustCompile(`^[\s─-]+$`)
	// headerRe matches both the legacy wide header (with EXPOSED PORTS) and the
	// catalog wide header (without EXPOSED column).
	headerRe = regexp.MustCompile(`(?i)^APPLICATION\s+NAME\s+POD\s+ID\s+POD\s+NAME\s+STATUS\s+CREATED(\s+EXPOSED\s+PORTS)?\s+CONTAINERS\s*$`)

	// rowRe matches a pod row from both legacy and catalog ps output.
	// The EXPOSED column is optional — catalog path omits it.
	// Status is case-insensitive: legacy path emits "Running (healthy)",
	// catalog path (via getPodStatusFromAPI) emits "running (healthy)".
	rowRe = regexp.MustCompile(
		`^\s*(?:\S+\s+)?` + // optional APPLICATION NAME
			`[a-f0-9]{8,12}(?:-[a-f0-9]{3,4})?\s+` + // POD ID
			`(?P<pod>\S+)\s{2,}` + // POD NAME
			`(?P<status>(?i:Running|running|Created|created|Exited|exited)\s*(?:\((?:healthy|unhealthy)\))?)\s{2,}` +
			`(?P<created>\d+\s+\w+\s+ago)\s{2,}` +
			`(?P<exposed>(?:none|\d+(?:,\s*\d+)*)\s{2,})?`, // EXPOSED (optional)
	)
)

type PodRow struct {
	PodName      string
	Status       string
	ExposedPorts string
}

// PodInfo represents detailed information about a pod including its containers.
type PodInfo struct {
	PodID      string
	PodName    string
	Containers []string
}

// ExtractPodInfo parses the output from `ai-services application ps -o wide` and extracts a map of pod names to PodInfo containing pod ID and containers.
func ExtractPodInfo(output string) (map[string]PodInfo, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	podInfoMap := make(map[string]PodInfo)

	// podRowRe matches both legacy output (with EXPOSED column) and catalog output (without).
	// Status is case-insensitive: legacy path emits "Running (healthy)",
	// catalog path emits "running (healthy)".
	podRowRe := regexp.MustCompile(
		`^\s*(?:\S+\s+)?` + // optional APPLICATION NAME
			`(?P<podid>[a-f0-9]{8,12}(?:-[a-f0-9]{3,4})?)\s+` + // POD ID
			`(?P<podname>\S+)\s{2,}` + // POD NAME
			`(?P<status>(?i:Running|running|Created|created|Exited|exited)\s*(?:\((?:healthy|unhealthy)\))?)\s{2,}` +
			`(?P<created>\d+\s+\w+\s+ago)\s{2,}` +
			`(?:(?:none|\d+(?:,\s*\d+)*)\s{2,})?` + // EXPOSED (optional — catalog path omits it)
			`(?P<containers>.+)$`, // CONTAINERS
	)

	containerLineRe := regexp.MustCompile(`^\s+(?P<containers>.+)$`)

	var currentPodName string
	var currentPodInfo *PodInfo

	for _, raw := range lines {
		// Strip klog prefix (added by logger.Infoln in CLI mode) and ANSI escapes.
		line := stripKlogPrefix(strings.TrimRight(raw, " \t"))
		line = stripANSI(line)
		if line == "" {
			continue
		}

		// Skip header and separator lines
		if headerRe.MatchString(line) || separatorRe.MatchString(line) {
			continue
		}

		if m := podRowRe.FindStringSubmatch(line); m != nil {
			podID := m[podRowRe.SubexpIndex("podid")]
			podName := m[podRowRe.SubexpIndex("podname")]
			containersStr := strings.TrimSpace(m[podRowRe.SubexpIndex("containers")])

			// Parse containers from the line
			containers := parseContainers(containersStr)

			currentPodName = podName
			currentPodInfo = &PodInfo{
				PodID:      podID,
				PodName:    podName,
				Containers: containers,
			}
			podInfoMap[podName] = *currentPodInfo

			continue
		}

		if currentPodInfo != nil {
			if m := containerLineRe.FindStringSubmatch(line); m != nil {
				containersStr := strings.TrimSpace(m[containerLineRe.SubexpIndex("containers")])
				containers := parseContainers(containersStr)

				// Append to current pod's containers
				currentPodInfo.Containers = append(currentPodInfo.Containers, containers...)
				podInfoMap[currentPodName] = *currentPodInfo
			}
		}
	}

	return podInfoMap, nil
}

// parseContainers extracts container names from a container string.
func parseContainers(containersStr string) []string {
	if containersStr == "" {
		return []string{}
	}

	// Split by comma to handle multiple containers.
	parts := strings.Split(containersStr, ",")
	containers := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if idx := strings.Index(part, "("); idx != -1 {
			containerName := strings.TrimSpace(part[:idx])
			if containerName != "" {
				containers = append(containers, containerName)
			}
		} else if part != "" {
			containers = append(containers, part)
		}
	}

	return containers
}

// parsePodRows parses the output lines from `ai-services application ps` into PodRow structs.
func parsePodRows(lines []string) ([]PodRow, error) {
	var rows []PodRow

	for _, raw := range lines {
		// Strip klog prefix (added by logger.Infoln in CLI mode) and ANSI escapes.
		line := stripKlogPrefix(strings.TrimRight(raw, " \t"))
		line = stripANSI(line)
		if line == "" {
			continue
		}
		if headerRe.MatchString(line) || separatorRe.MatchString(line) {
			continue
		}

		m := rowRe.FindStringSubmatch(line)
		if m == nil {
			continue // ignore container continuation noise
		}

		rows = append(rows, PodRow{
			PodName:      m[rowRe.SubexpIndex("pod")],
			Status:       m[rowRe.SubexpIndex("status")],
			ExposedPorts: m[rowRe.SubexpIndex("exposed")],
		})
	}

	return rows, nil
}

// getRestartCount inspects a pod and its containers and returns the total restart count.
func getRestartCount(podName string, appRuntime string, appName string) (int, error) {
	if appRuntime == "openshift" {
		// OpenShift: use oc get pod with JSON output
		return getOpenshiftRestartCount(podName, appName)
	}

	// Podman: use podman pod inspect
	return getPodmanRestartCount(podName)
}

func getPodmanRestartCount(podName string) (int, error) {
	podRes, err := common.RunCommand("podman", "pod", "inspect", podName)
	if err != nil {
		return 0, fmt.Errorf("failed to inspect pod %s: %w", podName, err)
	}

	var podData []PodInspect
	if err := json.Unmarshal([]byte(podRes), &podData); err != nil {
		return 0, fmt.Errorf("failed to parse pod inspect for %s: %w", podName, err)
	}
	if len(podData) == 0 {
		return 0, fmt.Errorf("no pod inspect data for %s", podName)
	}

	pod := podData[0]
	if pod.RestartPolicy == "no" {
		return 0, nil
	}

	ctrIDs := make([]string, 0, len(pod.Containers))
	for _, ctr := range pod.Containers {
		ctrIDs = append(ctrIDs, ctr.Id)
	}

	args := append([]string{"inspect"}, ctrIDs...)
	ctrRes, err := common.RunCommand("podman", args...)
	if err != nil {
		return 0, fmt.Errorf("failed to inspect containers in pod %s: %w", podName, err)
	}

	var allContainers []ContainerInspect
	if err := json.Unmarshal([]byte(ctrRes), &allContainers); err != nil {
		return 0, fmt.Errorf("failed to parse container inspect: %w", err)
	}

	totalRestarts := 0
	for _, ctr := range allContainers {
		totalRestarts += ctr.State.RestartCount
	}

	return totalRestarts, nil
}

func getOpenshiftRestartCount(podName string, appName string) (int, error) {
	podRes, err := common.RunCommand("oc", "get", "pod", podName, "-o", "json", "-n", appName)
	if err != nil {
		return 0, fmt.Errorf("failed to get pod %s: %w", podName, err)
	}

	var osPod OpenShiftPod
	if err := json.Unmarshal([]byte(podRes), &osPod); err != nil {
		return 0, fmt.Errorf("failed to parse OpenShift pod JSON for %s: %w", podName, err)
	}

	// Check restart policy
	if osPod.Spec.RestartPolicy == "Never" {
		return 0, nil
	}

	// Sum restart counts from all containers
	totalRestarts := 0
	for _, ctr := range osPod.Status.ContainerStatuses {
		totalRestarts += ctr.RestartCount
	}

	return totalRestarts, nil
}
func waitUntil(
	timeout time.Duration,
	interval time.Duration,
	condition func() (bool, error),
) error {
	deadline := time.Now().Add(timeout)

	for {
		done, err := condition()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout after %s", timeout)
		}
		time.Sleep(interval)
	}
}

func waitForPodRunningNoCrash(ctx context.Context, cfg *config.Config, appName, podName string, appRuntime string) error {
	min := 5
	sec := 30

	return waitUntil(time.Duration(min)*time.Minute, time.Duration(sec)*time.Second, func() (bool, error) {
		psWideArgs := []string{"-o", "wide"}
		res, err := cli.ApplicationPS(ctx, cfg, appName, appRuntime, psWideArgs...)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		if err != nil {
			return false, err
		}
		rows, err := parsePodRows(strings.Split(strings.TrimSpace(res), "\n"))
		if err != nil {
			return false, err
		}
		for _, row := range rows {
			if row.PodName != podName {
				continue
			}
			// Case-insensitive: catalog path emits "running (healthy)", legacy emits "Running (healthy)".
			statusLower := strings.ToLower(row.Status)
			healthy := strings.HasPrefix(statusLower, "running (healthy)") ||
				statusLower == "created"
			if !healthy {
				return false, nil
			}
			restarts, err := getRestartCount(podName, appRuntime, appName)
			if err != nil {
				return false, err
			}
			if restarts > 0 {
				return false, fmt.Errorf("pod %s restarted %d times", podName, restarts)
			}

			return true, nil
		}

		return false, fmt.Errorf("pod %s not found", podName)
	})
}

// VerifyContainers checks if application pods are healthy and their restart counts are zero.
//
// Pod naming conventions differ by runtime and path:
//
//	OpenShift:             <suffix>-<replicaset>-<random>   (e.g. backend-58c65dd449-pc6np)
//	Podman catalog path:   <service-id>-<slug>              (e.g. opensearch-62ef2bd81e, chat-bot-ffa3ca77a6)
//	Podman legacy path:    <appName>--<suffix>              (e.g. rag-app-123--opensearch)  [removed]
//
// For both OpenShift and podman catalog path we do prefix matching:
// the suffix from ExpectedPodSuffixes is used as a prefix to find the pod.
func VerifyContainers(ctx context.Context, cfg *config.Config, widePSOutput string, appName string, appRuntime string) error {
	logger.Infof("[Podman] verifying containers for app: %s", appName)

	if strings.TrimSpace(widePSOutput) == "" {
		ginkgo.Skip("No pods found — skipping pod health validation")

		return nil
	}
	actualPods, err := extractActualPods(ctx, widePSOutput, cfg, appName, appRuntime)
	if err != nil {
		return err
	}

	for _, suffix := range common.ExpectedPodSuffixes[appRuntime] {
		var foundPodName string

		// Both OpenShift and podman catalog path use dynamic pod names that begin
		// with the service suffix — match by prefix.
		expectedPrefix := suffix + "-"
		for podName := range actualPods {
			if strings.HasPrefix(podName, expectedPrefix) {
				foundPodName = podName

				break
			}
		}

		gomega.Expect(foundPodName).NotTo(gomega.BeEmpty(),
			"expected a pod with prefix %q to exist (available: %v)", expectedPrefix, podKeys(actualPods))

		restartCount, err := getRestartCount(foundPodName, appRuntime, appName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		ginkgo.GinkgoWriter.Printf("[RestartCount] pod=%s restarts=%d\n", foundPodName, restartCount)
		gomega.Expect(restartCount).To(gomega.BeNumerically("<=", 0),
			fmt.Sprintf("pod %s restarted %d times", foundPodName, restartCount))
	}

	return nil
}

// podKeys returns the keys of a map[string]bool as a slice — used in error messages.
func podKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	return keys
}

func extractActualPods(ctx context.Context, widePSOutput string, cfg *config.Config, appName string, appRuntime string) (map[string]bool, error) {
	lines := strings.Split(strings.TrimSpace(widePSOutput), "\n")
	rows, err := parsePodRows(lines)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pod rows: %w", err)
	}
	for _, row := range rows {
		// Status is lowercase "running (healthy)" from catalog path,
		// uppercase "Running (healthy)" from legacy podman path.
		statusLower := strings.ToLower(row.Status)
		ok := strings.HasPrefix(statusLower, "running (healthy)") ||
			strings.HasPrefix(statusLower, "running(healthy)") ||
			statusLower == "created"
		if !ok {
			if err := waitForPodRunningNoCrash(ctx, cfg, appName, row.PodName, appRuntime); err != nil {
				return nil, fmt.Errorf("pod %s is not healthy (status=%s)", row.PodName, row.Status)
			}
		}
	}
	actualPods := make(map[string]bool)
	for _, row := range rows {
		actualPods[row.PodName] = true
	}

	return actualPods, nil
}

// VerifyExposedPorts checks that the application exposes the expected port numbers.
func VerifyExposedPorts(appName string, expectedPorts []string, appRuntime string, widePsOutput string) error {
	if strings.TrimSpace(widePsOutput) == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(widePsOutput), "\n")
	rows, err := parsePodRows(lines)
	if err != nil {
		return fmt.Errorf("failed to parse pod rows: %w", err)
	}
	var ports []string

	for _, row := range rows {
		if row.ExposedPorts == "" || row.ExposedPorts == "none" {
			continue
		}
		splitPorts := strings.Split(row.ExposedPorts, ",")
		for _, p := range splitPorts {
			p = strings.TrimSpace(p)
			if p != "" {
				ports = append(ports, p)
			}
		}
	}
	gomega.Expect(ports).NotTo(gomega.BeEmpty(), "no exposed ports found for application %s", appName)
	gomega.Expect(ports).To(gomega.HaveLen(len(expectedPorts)), "expected %d exposed ports, found %d", len(expectedPorts), len(ports))
	gomega.Expect(ports).To(gomega.ConsistOf(expectedPorts), "exposed ports do not match expected ports")

	return nil
}

// GetOpenshiftRoutes retrieves the OpenShift routes for the given application namespace.
func GetOpenshiftRoutes(appName string) (string, error) {
	response, err := common.RunCommand("oc", "get", "routes", "-n", appName)
	if err != nil {
		return "", fmt.Errorf("failed to get routes: %w", err)
	}

	return response, nil
}
