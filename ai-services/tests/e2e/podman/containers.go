package podman

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
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

var (
	separatorRe = regexp.MustCompile(`^[\s─-]+$`)
	headerRe    = regexp.MustCompile(`^APPLICATION\s+NAME\s+POD\s+ID\s+POD\s+NAME\s+STATUS\s+CREATED\s+EXPOSED\s+PORTS\s$`)

	rowRe = regexp.MustCompile(
		`^\s*(?:\S+\s+)?` + // optional APPLICATION NAME
			`[a-f0-9]{12}\s+` + // POD ID
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
func getRestartCount(podName string) (int, error) {
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

func waitForPodRunningNoCrash(appName, podName string) error {
	min := 5
	sec := 30

	return waitUntil(time.Duration(min)*time.Minute, time.Duration(sec)*time.Second, func() (bool, error) {
		res, err := common.RunCommand("ai-services", "application", "ps", appName, "-o", "wide")
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
			restarts, err := getRestartCount(podName)
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
func VerifyContainers(appName string) error {
	logger.Infof("[Podman] verifying containers for app: %s", appName)
	res, err := common.RunCommand("ai-services", "application", "ps", appName, "-o", "wide")
	if err != nil {
		return fmt.Errorf("failed to run ai-services application ps: %w", err)
	}
	if strings.TrimSpace(res) == "" {
		ginkgo.Skip("No pods found — skipping pod health validation")

		return nil
	}
	lines := strings.Split(strings.TrimSpace(res), "\n")
	rows, err := parsePodRows(lines)
	if err != nil {
		return fmt.Errorf("failed to parse pod rows: %w", err)
	}
	for _, row := range rows {
		ok := strings.HasPrefix(row.Status, "Running (healthy)") || row.Status == "Created"
		if !ok {
			if err := waitForPodRunningNoCrash(appName, row.PodName); err != nil {
				return fmt.Errorf("pod %s is not healthy (status=%s)", row.PodName, row.Status)
			}
		}
	}
	actualPods := make(map[string]bool)
	for _, row := range rows {
		actualPods[row.PodName] = true
	}
	for _, suffix := range common.ExpectedPodSuffixes {
		expectedPodName := appName + "--" + suffix
		gomega.Expect(actualPods).To(gomega.HaveKey(expectedPodName), "expected pod %s to exist", expectedPodName)
		restartCount, err := getRestartCount(expectedPodName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		ginkgo.GinkgoWriter.Printf("[RestartCount] pod=%s restarts=%d\n", expectedPodName, restartCount)
		gomega.Expect(restartCount).To(gomega.BeNumerically("<=", 0),
			fmt.Sprintf("pod %s restarted %d times", expectedPodName, restartCount))
	}

	return nil
}

func VerifyExposedPorts(appName string, expectedPorts []string) error {
	res, err := common.RunCommand("ai-services", "application", "ps", appName, "-o", "wide")
	if err != nil {
		return fmt.Errorf("failed to run ai-services application ps: %w", err)
	}

	if strings.TrimSpace(res) == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(res), "\n")
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
	gomega.Expect(ports).NotTo(gomega.BeEmpty(),"no exposed ports found for application %s", appName)
	gomega.Expect(ports).To(gomega.HaveLen(len(expectedPorts)),"expected %d exposed ports, found %d",len(expectedPorts), len(ports))
	gomega.Expect(ports).To(gomega.ConsistOf(expectedPorts),"exposed ports do not match expected ports")

	return nil
}
