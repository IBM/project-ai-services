package e2e

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/project-ai-services/ai-services/tests/e2e/bootstrap"
	"github.com/project-ai-services/ai-services/tests/e2e/cleanup"
	"github.com/project-ai-services/ai-services/tests/e2e/cli"
	"github.com/project-ai-services/ai-services/tests/e2e/config"
	"github.com/project-ai-services/ai-services/tests/e2e/podman"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
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
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "AI Services E2E Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	fmt.Println("[SETUP] Starting AI Services E2E setup")

	ctx = context.Background()

	ginkgo.By("Loading E2E configuration")
	cfg = &config.Config{}

	ginkgo.By("Generating unique run ID")
	runID = fmt.Sprintf("%d", time.Now().Unix())

	ginkgo.By("Preparing runtime environment")
	tempDir = bootstrap.PrepareRuntime(runID)
	gomega.Expect(tempDir).NotTo(gomega.BeEmpty())

	ginkgo.By("Preparing temp bin directory for test binaries")
	tempBinDir = fmt.Sprintf("%s/bin", tempDir)
	bootstrap.SetTestBinDir(tempBinDir)
	fmt.Printf("[SETUP] Test binary directory: %s\n", tempBinDir)

	ginkgo.By("Building or verifying ai-services CLI")
	var err error
	aiServiceBin, err = bootstrap.BuildOrVerifyCLIBinary(ctx)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(aiServiceBin).NotTo(gomega.BeEmpty())
	cfg.AIServiceBin = aiServiceBin

	ginkgo.By("Getting ai-services version")
	binVersion, err = bootstrap.CheckBinaryVersion(aiServiceBin)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	fmt.Printf("[SETUP] ai-services version: %s\n", binVersion)

	ginkgo.By("Checking Podman environment (non-blocking)")
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

// Teardown after all tests have run.
var _ = ginkgo.AfterSuite(func() {
	fmt.Println("[TEARDOWN] AI Services E2E teardown")
	ginkgo.By("Cleaning up E2E environment")
	if err := cleanup.CleanupTemp(tempDir); err != nil {
		fmt.Printf("[TEARDOWN] cleanup failed: %v\n", err)
	}
	ginkgo.By("Cleanup completed")
})

var _ = ginkgo.Describe("AI Services End-to-End Tests", ginkgo.Ordered, func() {
	ginkgo.Context("Help Command Tests", func() {
		ginkgo.It("runs help command", func() {
			args := []string{"help"}
			output, err := cli.HelpCommand(ctx, cfg, args)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateHelpCommandOutput(output)).To(gomega.Succeed())
		})
		ginkgo.It("runs -h command", func() {
			args := []string{"-h"}
			output, err := cli.HelpCommand(ctx, cfg, args)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateHelpCommandOutput(output)).To(gomega.Succeed())
		})
		ginkgo.It("runs help for a given random command", func() {
			possibleCommands := []string{"application", "bootstrap", "completion", "version"}
			randomIndex := rand.Intn(len(possibleCommands))
			randomCommand := possibleCommands[randomIndex]
			args := []string{randomCommand, "-h"}
			output, err := cli.HelpCommand(ctx, cfg, args)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateHelpRandomCommandOutput(randomCommand, output)).To(gomega.Succeed())
		})
	})
	ginkgo.Context("Bootstrap Steps", func() {
		ginkgo.It("runs bootstrap configure", func() {
			output, err := cli.BootstrapConfigure(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateBootstrapConfigureOutput(output)).To(gomega.Succeed())
		})
		ginkgo.It("runs bootstrap validate", func() {
			output, err := cli.BootstrapValidate(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateBootstrapValidateOutput(output)).To(gomega.Succeed())
		})
		ginkgo.It("runs full bootstrap", func() {
			output, err := cli.Bootstrap(ctx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateBootstrapFullOutput(output)).To(gomega.Succeed())
		})
	})
	ginkgo.Context("Application Lifecycle", func() {
		ginkgo.It("creates rag application, runs health checks and validates RAG endpoints", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
			defer cancel()

			appName = fmt.Sprintf("rag-app-%s", runID)
			pods := []string{"backend", "ui", "db"} // replace with actual pod names

			err := cli.CreateRAGAppAndValidate(
				ctx,
				cfg,
				appName,
				"rag",
				"ui.port=3100,backend.port=5100",
				"5100", // backend port
				"3100", //ui port
				cli.CreateOptions{
					SkipModelDownload: false,
					ImagePullPolicy:   "IfNotPresent",
				},
				pods,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			fmt.Printf("[TEST] Application %s created, healthy, and RAG endpoints validated\n", appName)
		})
		ginkgo.It("verifies application ps output", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			psOutput, err := cli.ApplicationPS(ctx, cfg, appName)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Expect(cli.ValidateApplicationPS(psOutput)).To(gomega.Succeed())
			fmt.Printf("[TEST] application ps output validated successfully for %s\n", appName)
		})
		ginkgo.It("stops the application", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			output, err := cli.StopApp(ctx, cfg, appName)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(output).NotTo(gomega.BeEmpty())

			fmt.Printf("[TEST] Application %s stopped successfully\n", appName)
		})
		ginkgo.It("deletes the application", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			output, err := cli.DeleteApp(ctx, cfg, appName)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(output).NotTo(gomega.BeEmpty())

			fmt.Printf("[TEST] Application %s deleted successfully\n", appName)
		})
	})
	ginkgo.XContext("RAG validation", func() {
		ginkgo.It("validates responses against golden dataset", func() {
			ginkgo.Skip("RAG response validation not implemented yet")
		})
	})
	ginkgo.XContext("Podman / Container Validation", func() {
		ginkgo.It("verifies application containers are healthy", func() {
			if !podmanReady {
				ginkgo.Skip("Podman not available - will be installed via bootstrap configure")
			}
			err := podman.VerifyContainers(appName)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "verify containers failed")
			fmt.Println("[TEST] Containers verified")
		})
	})
})
