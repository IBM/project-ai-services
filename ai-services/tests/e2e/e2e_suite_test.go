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
	"github.com/project-ai-services/ai-services/tests/e2e/ingestion"
	"github.com/project-ai-services/ai-services/tests/e2e/podman"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	cfg                *config.Config
	runID              string
	appName            string
	tempDir            string
	tempBinDir         string
	aiServiceBin       string
	binVersion         string
	ctx                context.Context
	podmanReady        bool
	templateName       string
	mainPodsByTemplate map[string][]string
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

	By("Setting template name")
	templateName = "rag"

	By("Setting main pods by template")
	mainPodsByTemplate = map[string][]string{
		"rag": {
			"vllm-server",
			"milvus",
			"chat-bot",
		},
	}

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
	if err := cleanup.CleanupTemp(tempDir); err != nil {
		fmt.Printf("[TEARDOWN] cleanup failed: %v\n", err)
	}
	By("Cleanup completed")
})

var _ = Describe("AI Services End-to-End Tests", Ordered, func() {
	Context("Version Command Tests", func() {
		It("runs application version command", func() {
			args := []string{"version"}
			output, err := cli.VersionCommand(ctx, cfg, args)
			voutput, coutput, gerr := cli.GitVersionCommands(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(gerr).NotTo(HaveOccurred())
			Expect(cli.ValidateVersionCommandOutput(output, voutput, coutput)).To(Succeed())
		})
	})
	Context("Help Command Tests", func() {
		It("runs help command", func() {
			args := []string{"help"}
			output, err := cli.HelpCommand(ctx, cfg, args)
			Expect(err).NotTo(HaveOccurred())
			Expect(cli.ValidateHelpCommandOutput(output)).To(Succeed())
		})
		It("runs -h command", func() {
			args := []string{"-h"}
			output, err := cli.HelpCommand(ctx, cfg, args)
			Expect(err).NotTo(HaveOccurred())
			Expect(cli.ValidateHelpCommandOutput(output)).To(Succeed())
		})
		It("runs help for a given random command", func() {
			possibleCommands := []string{"application", "bootstrap", "completion", "version"}
			randomIndex := rand.Intn(len(possibleCommands))
			randomCommand := possibleCommands[randomIndex]
			args := []string{randomCommand, "-h"}
			output, err := cli.HelpCommand(ctx, cfg, args)
			Expect(err).NotTo(HaveOccurred())
			Expect(cli.ValidateHelpRandomCommandOutput(randomCommand, output)).To(Succeed())
		})
	})
	Context("Application Template Command Tests", func() {
		It("runs application template command", func() {
			output, err := cli.TemplatesCommand(ctx, cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(cli.ValidateApplicationsTemplateCommandOutput(output)).To(Succeed())
		})
	})
	Context("Application Model Command Tests", func() {
		It("verifies application model list command", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()
			output, err := cli.ModelList(ctx, cfg, templateName)
			Expect(err).NotTo(HaveOccurred())
			Expect(cli.ValidateModelListOutput(output, templateName)).To(Succeed())
			fmt.Printf("[TEST] Application model list validated successfully!\n")
		})
		It("verifies application model info command", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()
			output, err := cli.ModelDownload(ctx, cfg, templateName)
			Expect(err).NotTo(HaveOccurred())
			Expect(cli.ValidateModelDownloadOutput(output, templateName)).To(Succeed())
			fmt.Printf("[TEST] Application model download validated successfully!\n")
		})
	})
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
	Context("Application Image Command Tests", func() {
		It("lists images for rag template", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			err := cli.ListImage(ctx, cfg, templateName)
			Expect(err).NotTo(HaveOccurred())
			fmt.Printf("[TEST] Images listed successfully for %s template\n", templateName)
		})
		It("pulls images for rag template", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			err := cli.PullImage(ctx, cfg, templateName)
			Expect(err).NotTo(HaveOccurred())
			fmt.Printf("[TEST] Images pulled successfully for %s template\n", templateName)
		})
	})
	Context("Application Lifecycle", func() {
		It("creates rag application, runs health checks and validates RAG endpoints", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
			defer cancel()

			appName = fmt.Sprintf("%s-app-%s", templateName, runID)
			pods := []string{"backend", "ui", "db"} // replace with actual pod names

			err := cli.CreateAppWithHealthAndRAG(
				ctx,
				cfg,
				appName,
				templateName,
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
	})
	Context("Application Observability", func() {
		It("verifies application ps output", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			normalPsOutput, err := cli.ApplicationPS(ctx, cfg, appName)
			Expect(err).NotTo(HaveOccurred())
			Expect(cli.ValidateApplicationPS(normalPsOutput)).To(Succeed())
			fmt.Printf("[TEST] Application ps output validated successfully!\n")

			widePsOutput, err := cli.ApplicationPSWide(ctx, cfg, appName)
			Expect(err).NotTo(HaveOccurred())
			Expect(cli.ValidateApplicationPS(widePsOutput)).To(Succeed())
			fmt.Printf("[TEST] Application ps wide output validated successfully!\n")
		})
		It("verifies application info output", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			infoOutput, err := cli.ApplicationInfo(ctx, cfg, appName)
			Expect(err).NotTo(HaveOccurred())

			Expect(cli.ValidateApplicationInfo(infoOutput, appName, templateName)).To(Succeed())
			fmt.Printf("[TEST] Application info output validated successfully!\n")
		})
		It("Verifies pods existence, health status  and restart count", func() {
			if !podmanReady {
				Skip("Podman not available - will be installed via bootstrap configure")
			}
			err := podman.VerifyContainers(appName)
			Expect(err).NotTo(HaveOccurred(), "verify containers failed")
			fmt.Println("[TEST] Containers verified")
		})
		It("Verifies Exposed Ports of the application", func() {
			if !podmanReady {
				Skip("Podman not available - will be installed via bootstrap configure")
			}
			expected := []int{3000, 5000} // UI and backend ports
			err := podman.VerifyExposedPorts(appName, expected)
			Expect(err).NotTo(HaveOccurred(), "ports verification failed")
			fmt.Printf("[TEST] Pod Exposed Ports verified")
		})
	})
	Context("Application Teardown", func() {
		It("stops the application", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			suffixes, ok := mainPodsByTemplate[templateName]
			Expect(ok).To(BeTrue(), "unknown templateName")

			pods := make([]string, 0, len(suffixes))
			for _, s := range suffixes {
				pods = append(pods, fmt.Sprintf("%s--%s", appName, s))
			}

			output, err := cli.StopAppWithPods(ctx, cfg, appName, pods)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).NotTo(BeEmpty())

			fmt.Printf("[TEST] Application %s stopped successfully using --pod\n", appName)
		})
		It("starts application pods", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			output, err := cli.StartApplication(
				ctx,
				cfg,
				appName,
				cli.StartOptions{
					SkipLogs: false,
				},
			)

			Expect(err).NotTo(HaveOccurred())
			Expect(output).NotTo(BeEmpty())
			fmt.Printf("[TEST] Application %s started successfully\n", appName)
		})
		It("starts document ingestion pod and validates ingestion completion", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
			defer cancel()

			Expect(appName).NotTo(BeEmpty())

			Expect(ingestion.PrepareDocs(appName)).To(Succeed())

			Expect(ingestion.StartIngestion(ctx, cfg, appName)).To(Succeed())

			logs, err := ingestion.WaitForIngestionLogs(ctx, cfg, appName)
			Expect(err).ToNot(HaveOccurred())
			Expect(logs).To(ContainSubstring("Ingestion started"))
			Expect(logs).To(ContainSubstring("Processed '/var/docs/test_doc.pdf'"))

			fmt.Printf("[TEST] Ingestion completed successfully for application %s\n", appName)
		})
		It("deletes the application using --skip-cleanup", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()

			output, err := cli.DeleteAppSkipCleanup(ctx, cfg, appName)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).NotTo(BeEmpty())

			fmt.Printf("[TEST] Application %s deleted successfully using --skip-cleanup\n", appName)
		})
	})
	Context("RAG / Golden Dataset Validation", func() {
		It("validates responses against golden dataset", func() {
			Skip("RAG validation not implemented yet")
		})
	})
})
