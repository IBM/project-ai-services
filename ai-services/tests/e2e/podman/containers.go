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
	headerRe    = regexp.MustCompile(`^APPLICATION\s+NAME\s+POD\s+ID\s+POD\s+NAME\s+STATUS\s+CREATED\s+EXPOSED\s+PORTS\s$`)

	rowRe = regexp.MustCompile(
		`^\s*(?:\S+\s+)?` + // optional APPLICATION NAME
			`[a-f0-9]{8,12}(?:-[a-f0-9]{3,4})?\s+` + // POD ID (supports both formats: 12 hex chars or 8-12 hex + hyphen + 3-4 hex)
			`(?P<pod>\S+)\s{2,}` + // POD NAME
			`(?P<status>Running\s+\((?:healthy|unhealthy)\)|Created)\s{2,}` +
			`(?P<created>\d+\s+\w+\s+ago)\s{2,}` +
			`(?P<exposed>none|\d+(?:,\s*\d+)*)\s+`,
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

	podRowRe := regexp.MustCompile(
		`^\s*(?:\S+\s+)?` + // optional APPLICATION NAME
			`(?P<podid>[a-f0-9]{8,12}(?:-[a-f0-9]{3,4})?)\s+` + // POD ID (both formats)
			`(?P<podname>\S+)\s{2,}` + // POD NAME
			`(?P<status>Running\s+\((?:healthy|unhealthy)\)|Created)\s{2,}` +
			`(?P<created>\d+\s+\w+\s+ago)\s{2,}` +
			`(?P<exposed>none|\d+(?:,\s*\d+)*)\s+` + // EXPOSED (supports multiple ports)
			`(?P<containers>.+)$`, // CONTAINERS
	)

	containerLineRe := regexp.MustCompile(`^\s+(?P<containers>.+)$`)

	var currentPodName string
	var currentPodInfo *PodInfo

	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t")
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

	var containers []string
	// Split by comma to handle multiple containers
	parts := strings.Split(containersStr, ",")

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
	rows := []PodRow{}

	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t")
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
			healthy := strings.HasPrefix(row.Status, "Running (healthy)") ||
				row.Status == "Created"
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
		var expectedPodName string
		var found bool

		if appRuntime == "openshift" {
			// For OpenShift, pod names have dynamic suffixes (e.g., backend-58c65dd449-pc6np)
			// Check if any actual pod starts with the expected prefix
			podName := ""
			expectedPrefix := suffix + "-"
			for podName = range actualPods {
				if strings.HasPrefix(podName, expectedPrefix) {
					expectedPodName = podName
					found = true

					break
				}
			}
			gomega.Expect(found).To(gomega.BeTrue(), "expected pod with prefix %s to exist", expectedPrefix)
			restartCount, err := getRestartCount(podName, appRuntime, appName)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("[RestartCount] pod=%s restarts=%d\n", expectedPodName, restartCount)
			gomega.Expect(restartCount).To(gomega.BeNumerically("<=", 0),
				fmt.Sprintf("pod %s restarted %d times", expectedPodName, restartCount))
		} else {
			// For podman, use exact pod name matching
			expectedPodName = appName + "--" + suffix
			gomega.Expect(actualPods).To(gomega.HaveKey(expectedPodName), "expected pod %s to exist", expectedPodName)
			restartCount, err := getRestartCount(expectedPodName, appRuntime, appName)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			ginkgo.GinkgoWriter.Printf("[RestartCount] pod=%s restarts=%d\n", expectedPodName, restartCount)
			gomega.Expect(restartCount).To(gomega.BeNumerically("<=", 0),
				fmt.Sprintf("pod %s restarted %d times", expectedPodName, restartCount))
		}
	}

	return nil
}

func extractActualPods(ctx context.Context, widePSOutput string, cfg *config.Config, appName string, appRuntime string) (map[string]bool, error) {
	lines := strings.Split(strings.TrimSpace(widePSOutput), "\n")
	rows, err := parsePodRows(lines)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pod rows: %w", err)
	}
	for _, row := range rows {
		ok := strings.HasPrefix(row.Status, "Running (healthy)") || row.Status == "Created"
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

func GetOpenshiftRoutes(appName string) (string, error) {
	response, err := common.RunCommand("oc", "get", "routes", "-n", appName)
	if err != nil {
		return "", fmt.Errorf("failed to get routes: %w", err)
	}

	return response, nil
}
