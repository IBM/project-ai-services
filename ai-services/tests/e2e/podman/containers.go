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

// klogPrefixRe matches the klog timestamp/source prefix added to each log line.
var klogPrefixRe = regexp.MustCompile(`^[IWEF]\d{4}\s+\d{2}:\d{2}:\d{2}\.\d+\s+\d+\s+\S+:\d+\]\s`)

// ansiEscapeRe matches ANSI/VT100 escape sequences.
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripKlogPrefix removes the klog timestamp/source prefix from a line if present.
func stripKlogPrefix(line string) string {
	if loc := klogPrefixRe.FindStringIndex(line); loc != nil {
		return line[loc[1]:]
	}

	return line
}

// stripANSI removes ANSI escape sequences from a string.
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
	// headerRe matches both the wide header (with EXPOSED PORTS) and catalog header (without).
	headerRe = regexp.MustCompile(`(?i)^APPLICATION\s+NAME\s+POD\s+ID\s+POD\s+NAME\s+STATUS\s+CREATED(\s+EXPOSED\s+PORTS)?\s+CONTAINERS\s*$`)

	// rowRe matches a pod row; the EXPOSED column is optional (catalog path omits it).
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

// ExtractPodInfo parses `application ps -o wide` output into a map of pod name to PodInfo.
func ExtractPodInfo(output string) (map[string]PodInfo, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	podInfoMap := make(map[string]PodInfo)

	// podRowRe matches pod rows; EXPOSED column is optional.
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
		line := stripKlogPrefix(strings.TrimRight(raw, " \t"))
		line = stripANSI(line)
		if line == "" {
			continue
		}

		if headerRe.MatchString(line) || separatorRe.MatchString(line) {
			continue
		}

		if m := podRowRe.FindStringSubmatch(line); m != nil {
			podID := m[podRowRe.SubexpIndex("podid")]
			podName := m[podRowRe.SubexpIndex("podname")]
			containersStr := strings.TrimSpace(m[podRowRe.SubexpIndex("containers")])
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
				currentPodInfo.Containers = append(currentPodInfo.Containers, parseContainers(containersStr)...)
				podInfoMap[currentPodName] = *currentPodInfo
			}
		}
	}

	return podInfoMap, nil
}

// parseContainers extracts container names from a comma-separated string.
func parseContainers(containersStr string) []string {
	if containersStr == "" {
		return []string{}
	}

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

// parsePodRows parses `application ps` output lines into PodRow structs.
func parsePodRows(lines []string) ([]PodRow, error) {
	var rows []PodRow

	for _, raw := range lines {
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
			continue
		}

		rows = append(rows, PodRow{
			PodName:      m[rowRe.SubexpIndex("pod")],
			Status:       m[rowRe.SubexpIndex("status")],
			ExposedPorts: m[rowRe.SubexpIndex("exposed")],
		})
	}

	return rows, nil
}

// getRestartCount returns the total restart count for a pod.
func getRestartCount(podName string, appRuntime string, appName string) (int, error) {
	if appRuntime == "openshift" {
		return getOpenshiftRestartCount(podName, appName)
	}

	return getPodmanRestartCount(podName)
}

func getPodmanRestartCount(podName string) (int, error) {
	podRes, err := common.RunCommand("podman", "pod", "inspect", podName)
	if err == nil {
		// Pod has an infra container — use the standard pod inspect path.
		return podmanPodInspectRestartCount(podName, podRes)
	}

	// No pod infra container — fall back to inspecting containers whose name starts with podName.
	return podmanNamePrefixRestartCount(podName)
}

// podmanPodInspectRestartCount sums container restarts for a pod using the `podman pod inspect` output.
func podmanPodInspectRestartCount(podName, podRes string) (int, error) {
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

	return sumContainerRestarts(podName, ctrIDs)
}

// podmanNamePrefixRestartCount sums restarts of containers whose name starts with podName.
// e.g. podName="chat-bot-ac3d07543c" matches "chat-bot-ac3d07543c-ui", "chat-bot-ac3d07543c-backend-server".
func podmanNamePrefixRestartCount(podName string) (int, error) {
	const idNameParts = 2

	psOut, err := common.RunCommand("podman", "ps", "--format", "{{.ID}} {{.Names}}")
	if err != nil {
		return 0, fmt.Errorf("failed to list containers for pod %s: %w", podName, err)
	}

	var ctrIDs []string

	for _, line := range strings.Split(strings.TrimSpace(psOut), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), " ", idNameParts)
		if len(parts) == idNameParts && strings.HasPrefix(parts[1], podName+"-") {
			ctrIDs = append(ctrIDs, parts[0])
		}
	}

	if len(ctrIDs) == 0 {
		return 0, nil
	}

	return sumContainerRestarts(podName, ctrIDs)
}

// sumContainerRestarts inspects the given containers and returns their total restart count.
func sumContainerRestarts(podName string, ctrIDs []string) (int, error) {
	args := append([]string{"inspect"}, ctrIDs...)

	ctrRes, err := common.RunCommand("podman", args...)
	if err != nil {
		return 0, fmt.Errorf("failed to inspect containers for pod %s: %w", podName, err)
	}

	var allContainers []ContainerInspect
	if err := json.Unmarshal([]byte(ctrRes), &allContainers); err != nil {
		return 0, fmt.Errorf("failed to parse container inspect for pod %s: %w", podName, err)
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

	if osPod.Spec.RestartPolicy == "Never" {
		return 0, nil
	}

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

// podmanContainerPodNames queries `podman ps` directly and returns a set of pod-level name
// prefixes derived from container names (e.g. "chat-bot-ac3d07543c-ui" → "chat-bot-ac3d07543c").
// This is used as a fallback when a service pod has no infra container and therefore does not
// appear in `podman pod list` (and thus not in `application ps` output).
func podmanContainerPodNames() map[string]bool {
	out, err := common.RunCommand("podman", "ps", "--format", "{{.Names}}")
	if err != nil {
		return nil
	}

	podNames := make(map[string]bool)

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		// Container names have the form "<pod-name>-<container-role>" (e.g. "chat-bot-ac3d07543c-ui").
		// Derive the pod name by stripping the last dash-separated segment.
		if idx := strings.LastIndex(name, "-"); idx > 0 {
			podNames[name[:idx]] = true
		}
	}

	return podNames
}

// findPodByPrefix returns a pod name from the set that starts with prefix, or "" if none matches.
func findPodByPrefix(pods map[string]bool, prefix string) string {
	for podName := range pods {
		if strings.HasPrefix(podName, prefix) {
			return podName
		}
	}

	return ""
}

// allSuffixesPresent reports whether a pod exists for every expected service suffix.
func allSuffixesPresent(pods map[string]bool, suffixes []string) bool {
	for _, suffix := range suffixes {
		if findPodByPrefix(pods, suffix+"-") == "" {
			return false
		}
	}

	return true
}

// collectActualPods extracts pods from the ps output and, for podman, merges in pod names derived
// from direct `podman ps` output. This catches services whose infra container is absent and thus
// invisible to `podman pod list` (and therefore to `application ps`).
func collectActualPods(ctx context.Context, psOutput string, cfg *config.Config, appName, appRuntime string) (map[string]bool, error) {
	actualPods, err := extractActualPods(ctx, psOutput, cfg, appName, appRuntime)
	if err != nil {
		return nil, err
	}

	if appRuntime == "podman" {
		for podName := range podmanContainerPodNames() {
			actualPods[podName] = true
		}
	}

	return actualPods, nil
}

// waitForExpectedPods retries fetching ps output until all expected pods are present or the timeout
// elapses. The catalog API silently omits pods that aren't running yet, so a single snapshot may be
// missing late-starting services. On timeout the latest pod set is returned so callers can assert on it.
func waitForExpectedPods(
	ctx context.Context,
	cfg *config.Config,
	widePSOutput, appName, appRuntime string,
	suffixes []string,
	pollTimeout, pollInterval time.Duration,
) (map[string]bool, error) {
	deadline := time.Now().Add(pollTimeout)

	for attempt := 0; ; attempt++ {
		psOutput := widePSOutput
		if attempt > 0 {
			// Not the first iteration — re-fetch fresh output.
			freshOut, fetchErr := cli.ApplicationPS(ctx, cfg, appName, appRuntime, "-o", "wide")
			if fetchErr != nil {
				return nil, fmt.Errorf("failed to refresh application ps: %w", fetchErr)
			}
			psOutput = freshOut
		}

		actualPods, err := collectActualPods(ctx, psOutput, cfg, appName, appRuntime)
		if err != nil {
			return nil, err
		}

		if allSuffixesPresent(actualPods, suffixes) || time.Now().After(deadline) {
			return actualPods, nil
		}

		logger.Infof("[Podman] not all pods present yet for app %s (available: %v) — retrying in %s", appName, podKeys(actualPods), pollInterval)
		time.Sleep(pollInterval)
	}
}

// VerifyContainers checks that all expected pods are healthy and have zero restarts; matches pods by prefix for both OpenShift and podman catalog paths.
// It retries the ps call until all expected pods appear (up to 10 minutes) to handle services that start
// after the initial ps snapshot was taken (e.g. chat-bot pods that come up later than components).
// For podman, any pod prefix still missing from the catalog API output is resolved via a direct
// `podman ps` fallback — this handles services whose infra container is absent and therefore
// invisible to `podman pod list`.
func VerifyContainers(ctx context.Context, cfg *config.Config, widePSOutput string, appName, appRuntime, templateName string) error {
	logger.Infof("[Podman] verifying containers for app: %s", appName)

	if strings.TrimSpace(widePSOutput) == "" {
		ginkgo.Skip("No pods found — skipping pod health validation")

		return nil
	}

	// Prefer template-aware suffix map; fall back to legacy runtime-only map for unknown templates.
	suffixes, ok := common.ExpectedPodSuffixesByTemplate[templateName][appRuntime]
	if !ok {
		suffixes = common.ExpectedPodSuffixes[appRuntime]
	}

	const (
		pollTimeout  = 10 * time.Minute
		pollInterval = 30 * time.Second
	)

	actualPods, err := waitForExpectedPods(ctx, cfg, widePSOutput, appName, appRuntime, suffixes, pollTimeout, pollInterval)
	if err != nil {
		return err
	}

	for _, suffix := range suffixes {
		// Match by prefix: both OpenShift and podman catalog path use dynamic pod names beginning with the service suffix.
		expectedPrefix := suffix + "-"
		foundPodName := findPodByPrefix(actualPods, expectedPrefix)

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
		// Status may be lowercase (catalog) or uppercase (legacy podman) — normalise before comparison.
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
