package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/project-ai-services/ai-services/tests/e2e/bootstrap"
	"github.com/project-ai-services/ai-services/tests/e2e/cleanup"
	"github.com/project-ai-services/ai-services/tests/e2e/cli"
	"github.com/project-ai-services/ai-services/tests/e2e/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	cfg          *config.Config
	runID        string
	appName      string
	tempDir      string
	tempBinDir   string
	aiServiceBin string
	binVersion   string
	ctx          context.Context
	podmanReady  bool
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AI Services E2E Suite")
}

var _ = BeforeSuite(func() {
	fmt.Println("[SETUP] Starting AI Services E2E setup")

	ctx = context.Background()

	By("Loading E2E configuration")
	cfg = &config.Config{}

	By("Generating unique run ID")
	runID = fmt.Sprintf("%d", time.Now().Unix())

	By("Preparing runtime environment")
	tempDir = bootstrap.PrepareRuntime(runID)
	Expect(tempDir).NotTo(BeEmpty())

	By("Preparing temp bin directory for test binaries")
	tempBinDir = fmt.Sprintf("%s/bin", tempDir)
	bootstrap.SetTestBinDir(tempBinDir)
	fmt.Printf("[SETUP] Test binary directory: %s\n", tempBinDir)

	By("Building or verifying ai-services CLI")
	var err error
	aiServiceBin, err = bootstrap.BuildOrVerifyCLIBinary(ctx)
	Expect(err).NotTo(HaveOccurred())
	Expect(aiServiceBin).NotTo(BeEmpty())
	cfg.AIServiceBin = aiServiceBin

	By("Getting ai-services version")
	binVersion, err = bootstrap.GetBinaryVersion(aiServiceBin)
	Expect(err).NotTo(HaveOccurred())
	fmt.Printf("[SETUP] ai-services version: %s\n", binVersion)

	By("Checking Podman environment (non-blocking)")
	err = bootstrap.CheckPodman()
	if err != nil {
		podmanReady = false
		fmt.Printf("[SETUP] [WARNING] Podman not available: %v - will be installed via bootstrap configure\n", err)
	} else {
		podmanReady = true
		fmt.Printf("[SETUP] Podman environment verified\n")
	}

	fmt.Printf("[SETUP] ================================================\n")
	fmt.Printf("[SETUP] E2E Environment Ready\n")
	fmt.Printf("[SETUP] Binary:   %s\n", aiServiceBin)
	fmt.Printf("[SETUP] Version:  %s\n", binVersion)
	fmt.Printf("[SETUP] TempDir:  %s\n", tempDir)
	fmt.Printf("[SETUP] RunID:    %s\n", runID)
	fmt.Printf("[SETUP] Podman:   %v\n", podmanReady)
	fmt.Printf("[SETUP] ================================================\n\n")
})

var _ = AfterSuite(func() {
	fmt.Println("[TEARDOWN] AI Services E2E teardown")
	By("Cleaning up E2E environment")
	cleanup.CleanupTemp(tempDir)
	By("Cleanup completed")
})

var _ = Describe("AI Services End-to-End Tests", Ordered, func() {
	Context("Bootstrap Steps", func() {
		It("runs bootstrap configure", func() {
			output, err := cli.BootstrapConfigure(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(cli.ValidateBootstrapConfigureOutput(output)).To(Succeed())
		})
		It("runs bootstrap validate", func() {
			output, err := cli.BootstrapValidate(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(cli.ValidateBootstrapValidateOutput(output)).To(Succeed())
		})
		It("runs full bootstrap", func() {
			output, err := cli.Bootstrap(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(cli.ValidateBootstrapFullOutput(output)).To(Succeed())
		})
	})
	Context("Application Lifecycle", func() {
		It("creates rag application, runs health checks, and validates RAG endpoints", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
			defer cancel()

			appName = fmt.Sprintf("rag-app-%s", runID)
			pods := []string{"backend", "ui", "db"} // replace with actual pod names

			err := cli.CreateAppWithHealthAndRAG(
				ctx,
				cfg,
				appName,
				"rag",
				"ui.port=3000,backend.port=5000",
				"5000", // backend port
				"3000", //ui port
				cli.CreateOptions{
					SkipImageDownload: false,
					SkipModelDownload: false,
				},
				pods,
			)
			Expect(err).NotTo(HaveOccurred())
			fmt.Printf("[TEST] Application %s created, healthy, and RAG endpoints validated\n", appName)
		})
		It("starts the application", func() {
			err := cli.StartApp("test-app")
			Expect(err).NotTo(HaveOccurred(), "start-app command failed")
			fmt.Println("[TEST] Application started: test-app")
		})
	})
	Context("RAG / Golden Dataset Validation", func() {
		It("validates responses against golden dataset", func() {
			Skip("RAG validation not implemented yet")
		})
	})
	Context("Podman / Container Validation", func() {
		It("verifies application containers are healthy", func() {
			if !podmanReady {
				Skip("Podman not available - will be installed via bootstrap configure")
			}
			Skip("Podman container validation not implemented yet")
		})
	})
})
