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
	tempDir      string
	tempBinDir   string
	aiServiceBin string
	binVersion   string
	ctx          context.Context
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AI Services E2E Suite")
}

var _ = BeforeSuite(func() {
	fmt.Println("Starting AI Services E2E setup")

	// Create a context for setup
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
	Expect(err).NotTo(HaveOccurred(), "failed to get/build ai-services binary")
	Expect(aiServiceBin).NotTo(BeEmpty(), "ai-services binary path is empty")

	// Set the binary path in config for CLI helpers
	cfg.AIServiceBin = aiServiceBin

	By("Getting ai-services version")
	binVersion, err = bootstrap.GetBinaryVersion(aiServiceBin)
	Expect(err).NotTo(HaveOccurred(), "failed to get binary version")
	fmt.Printf("[SETUP] ai-services version: %s\n", binVersion)

	By("Checking Podman environment")
	err = bootstrap.CheckPodman()
	Expect(err).NotTo(HaveOccurred(), "podman is not available")

	By("E2E setup completed")
	fmt.Printf("[SETUP] ================================================\n")
	fmt.Printf("[SETUP] E2E Environment Ready\n")
	fmt.Printf("[SETUP] Binary:  %s\n", aiServiceBin)
	fmt.Printf("[SETUP] Version: %s\n", binVersion)
	fmt.Printf("[SETUP] TempDir: %s\n", tempDir)
	fmt.Printf("[SETUP] RunID:   %s\n", runID)
	fmt.Printf("[SETUP] ================================================\n")
})

var _ = AfterSuite(func() {
	fmt.Println("AI Services E2E teardown")

	By("Cleaning up E2E environment")
	cleanup.CleanupTemp(tempDir)

	By("Cleanup completed")
})

var _ = Describe("AI Services End-to-End Tests", Ordered, func() {

	Context("CLI Commands", func() {

		It("bootstraps AI Services", func() {
			cli.Bootstrap()
		})

		It("creates an application", func() {
			cli.CreateApp("test-app")
		})

		It("starts the application", func() {
			cli.StartApp("test-app")
		})

	})

	Context("RAG / Golden Dataset Validation", func() {
		It("validates responses against golden dataset", func() {
			Skip("RAG validation not implemented yet")
		})
	})

	Context("Podman / Container Validation", func() {
		It("verifies application containers are healthy", func() {
			Skip("Podman container validation not implemented yet")
		})
	})
})
