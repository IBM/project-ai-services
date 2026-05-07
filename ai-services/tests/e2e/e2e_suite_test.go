package e2e

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/bootstrap"
	"github.com/project-ai-services/ai-services/tests/e2e/cleanup"
	"github.com/project-ai-services/ai-services/tests/e2e/cli"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
	"github.com/project-ai-services/ai-services/tests/e2e/config"
	"github.com/project-ai-services/ai-services/tests/e2e/digitization"
	"github.com/project-ai-services/ai-services/tests/e2e/ingestion"
	"github.com/project-ai-services/ai-services/tests/e2e/podman"
	"github.com/project-ai-services/ai-services/tests/e2e/rag"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

var (
	cfg                         *config.Config
	runID                       string
	appName                     string
	providedAppName             string
	appRuntime                  string
	deleteExistingApp           bool
	tempDir                     string
	tempBinDir                  string
	aiServiceBin                string
	binVersion                  string
	ctx                         context.Context
	podmanReady                 bool
	templateName                string
	goldenPath                  string
	ragBaseURL                  string
	judgeBaseURL                string
	backendPort                 string
	uiPort                      string
	digitizePort                string
	digitizeUiPort              string
	summarizePort               string
	similarityPort              string
	judgePort                   string
	goldenDatasetFile           string
	defaultRagAccuracyThreshold = 0.70
	defaultMaxRetries           = 2
)

func init() {
	flag.StringVar(&providedAppName, "app-name", "", "Use existing application instead of creating one")
	flag.BoolVar(&deleteExistingApp, "delete-app", false, "Delete existing app before proceeding ahead with test run")
	flag.StringVar(&appRuntime, "runtime", "podman", "Runtime on which the app will be deployed")
}
func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "AI Services E2E Suite")
}

func getEnvWithDefault(key, defaultValue string) string {
	if envValue := os.Getenv(key); envValue != "" {
		return envValue
	}

	return defaultValue
}

var _ = ginkgo.BeforeSuite(func() {
	logger.Infoln("[SETUP] Starting AI Services E2E setup")

	ctx = context.Background()

	ginkgo.By("Loading E2E configuration")
	cfg = &config.Config{}

	ginkgo.By("Setting application runtime for digitization package")
	digitization.SetAppRuntime(appRuntime)
	logger.Infof("[SETUP] Application runtime set to: %s", appRuntime)

	ginkgo.By("Generating unique run ID")
	if runIDEnv := os.Getenv("RUN_ID"); runIDEnv != "" {
		runID = runIDEnv
	} else {
		runID = fmt.Sprintf("%d", time.Now().Unix())
	}

	ginkgo.By("Preparing runtime environment")
	tempDir = bootstrap.PrepareRuntime(runID)
	gomega.Expect(tempDir).NotTo(gomega.BeEmpty())

	ginkgo.By("Preparing temp bin directory for test binaries")
	tempBinDir = fmt.Sprintf("%s/bin", tempDir)
	bootstrap.SetTestBinDir(tempBinDir)
	logger.Infof("[SETUP] Test binary directory: %s", tempBinDir)

	ginkgo.By("Setting template name")
	templateName = "rag"

	ginkgo.By("Resolving application name")
	if providedAppName != "" {
		appName = providedAppName
		logger.Infof("[SETUP] Using provided application name: %s", appName)
	} else {
		appName = fmt.Sprintf("%s-app-%s", templateName, runID)
		logger.Infof("[SETUP] Generated application name: %s", appName)
	}

	ginkgo.By("Resolving application ports from environment")
	backendPort = getEnvWithDefault("RAG_BACKEND_PORT", "5100")
	uiPort = getEnvWithDefault("RAG_UI_PORT", "3100")
	digitizePort = getEnvWithDefault("DIGITIZE_PORT", "4100")
	digitizeUiPort = getEnvWithDefault("DIGITIZE_UI_PORT", "7100")
	summarizePort = getEnvWithDefault("SUMMARIZE_PORT", "6100")
	similarityPort = getEnvWithDefault("SIMILARITY_PORT", "9100")
	judgePort = getEnvWithDefault("LLM_JUDGE_PORT", "8000")
	if ragAccuracyThreshold, err := strconv.ParseFloat(
		getEnvWithDefault("RAG_ACCURACY_THRESHOLD", "0.70"),
		64,
	); err == nil {
		defaultRagAccuracyThreshold = ragAccuracyThreshold
	} else {
		logger.Warningf("[SETUP][WARN] Invalid RAG_ACCURACY_THRESHOLD, using default %.2f", defaultRagAccuracyThreshold)
	}
	logger.Infof("[SETUP] Ports: backend=%s ui=%s digitize=%s digitizeUi = %s summarize=%s similarity=%s judge=%s | accuracy=%.2f", backendPort, uiPort, digitizePort, digitizeUiPort, summarizePort, similarityPort, judgePort, defaultRagAccuracyThreshold)

	ginkgo.By("Building or verifying ai-services CLI")
	var err error
	aiServiceBin, err = bootstrap.BuildOrVerifyCLIBinary(ctx)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(aiServiceBin).NotTo(gomega.BeEmpty())
	cfg.AIServiceBin = aiServiceBin

	ginkgo.By("Getting ai-services version")
	binVersion, err = bootstrap.CheckBinaryVersion(aiServiceBin)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	logger.Infof("[SETUP] ai-services version: %s", binVersion)

	ginkgo.By("Checking Podman environment (non-blocking)")
	err = bootstrap.CheckPodman()
	if err != nil {
		podmanReady = false
		logger.Warningf("[SETUP] [WARNING] Podman not available: %v - will be installed via bootstrap configure", err)
	} else {
		podmanReady = true
		logger.Infoln("[SETUP] Podman environment verified")
	}

	ginkgo.By("Checking if existing app needs to be deleted")
	if deleteExistingApp {
		//fetch existing application details
		psOutput, err := cli.ApplicationPS(ctx, cfg, "", appRuntime)
		if err != nil {
			logger.Errorf("Error fetching delete application name")
			ginkgo.Fail("Error fetching delete application name")
		}

		//fetch application to be deleted
		deleteAppName := cli.GetApplicationNameFromPSOutput(psOutput)
		if deleteAppName != "" {
			//delete existing application
			_, err := cli.DeleteAppSkipCleanup(ctx, cfg, deleteAppName, appRuntime)
			if err != nil {
				logger.Errorf("Error deleting existing app: %s", deleteAppName)
				ginkgo.Fail("Existing application could not be deleted")
			}
			logger.Infof("[SETUP] Deleted existing app: %s", deleteAppName)
		} else {
			logger.Infof("[SETUP] No existing application found to delete")
		}

	}

	logger.Infoln("[SETUP] ================================================")
	logger.Infoln("[SETUP] E2E Environment Ready")
	logger.Infof("[SETUP] Binary:   %s", aiServiceBin)
	logger.Infof("[SETUP] Version:  %s", binVersion)
	logger.Infof("[SETUP] TempDir:  %s", tempDir)
	logger.Infof("[SETUP] RunID:    %s", runID)
	logger.Infof("[SETUP] Podman:   %v", podmanReady)
	logger.Infoln("[SETUP] ================================================")
})

// Teardown after all tests have run.
var _ = ginkgo.AfterSuite(func() {
	logger.Infoln("[TEARDOWN] AI Services E2E teardown")
	ginkgo.By("Cleaning up E2E environment")
	if err := cleanup.CleanupTemp(tempDir); err != nil {
		logger.Errorf("[TEARDOWN] cleanup failed: %v", err)
	}
	ginkgo.By("Cleanup completed")
})

var _ = ginkgo.Describe("AI Services End-to-End Tests", ginkgo.Ordered, func() {
	ginkgo.Context("Environment & CLI Sanity Tests", func() {
		ginkgo.It("runs help command", ginkgo.Label("spyre-independent"), func() {
			args := []string{"help"}
			output, err := cli.HelpCommand(ctx, cfg, args)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateHelpCommandOutput(output)).To(gomega.Succeed())
		})
		ginkgo.It("runs -h command", ginkgo.Label("spyre-independent"), func() {
			args := []string{"-h"}
			output, err := cli.HelpCommand(ctx, cfg, args)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateHelpCommandOutput(output)).To(gomega.Succeed())
		})
		ginkgo.It("runs help for a given random command", ginkgo.Label("spyre-independent"), func() {
			possibleCommands := []string{"application", "bootstrap", "completion", "version"}
			randomIndex := rand.Intn(len(possibleCommands))
			randomCommand := possibleCommands[randomIndex]
			args := []string{randomCommand, "-h"}
			output, err := cli.HelpCommand(ctx, cfg, args)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateHelpRandomCommandOutput(randomCommand, output)).To(gomega.Succeed())
		})
		ginkgo.It("runs application template command", ginkgo.Label("spyre-independent"), func() {
			output, err := cli.TemplatesCommand(ctx, cfg, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateApplicationsTemplateCommandOutput(output, appRuntime)).To(gomega.Succeed())
		})
		ginkgo.It("verifies application model list command", ginkgo.Label("spyre-independent"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()
			output, err := cli.ModelList(ctx, cfg, templateName, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateModelListOutput(output, templateName, appRuntime)).To(gomega.Succeed())
			logger.Infoln("[TEST] Application model list validated successfully!")
		})
		ginkgo.It("verifies application model download command", ginkgo.Label("spyre-independent"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()
			output, err := cli.ModelDownload(ctx, cfg, templateName, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateModelDownloadOutput(output, templateName, appRuntime)).To(gomega.Succeed())
			logger.Infoln("[TEST] Application model download validated successfully!")
		})
	})
	ginkgo.Context("Bootstrap Steps", func() {
		ginkgo.It("runs bootstrap configure", ginkgo.Label("spyre-dependent"), func() {
			output, err := cli.BootstrapConfigure(ctx, cfg, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateBootstrapConfigureOutput(output, appRuntime)).To(gomega.Succeed())
		})
		ginkgo.It("runs bootstrap validate", ginkgo.Label("spyre-dependent"), func() {
			output, err := cli.BootstrapValidate(ctx, cfg, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateBootstrapValidateOutput(output)).To(gomega.Succeed())
		})
		ginkgo.It("runs full bootstrap", ginkgo.Label("spyre-dependent"), func() {
			output, err := cli.Bootstrap(ctx, cfg, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateBootstrapFullOutput(output, appRuntime)).To(gomega.Succeed())
		})
	})
	ginkgo.Context("Application Image Command Tests", func() {
		ginkgo.It("lists images for rag template", ginkgo.Label("spyre-independent"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			err := cli.ListImage(ctx, cfg, templateName, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			logger.Infof("[TEST] Images listed successfully for %s template", templateName)
		})
		ginkgo.It("pulls images for rag template", ginkgo.Label("spyre-independent"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			err := cli.PullImage(ctx, cfg, templateName, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			logger.Infof("[TEST] Images pulled successfully for %s template", templateName)
		})
	})
	ginkgo.Context("Application Creation", func() {
		ginkgo.It("creates rag application, runs health checks and validates RAG endpoints", ginkgo.Label("spyre-dependent"), func() {
			if providedAppName != "" {
				ginkgo.Skip("Skipping creation — using existing application")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
			defer cancel()

			pods := []string{"backend", "ui", "db"} // replace with actual pod names
			params := ""
			cliOptions := cli.CreateOptions{}
			if appRuntime == "podman" {
				params = "ui.port=" + uiPort + ",backend.port=" + backendPort + ",digitize.port=" + digitizePort + ",digitizeUi.port=" + digitizeUiPort + ",summarize.port=" + summarizePort + ",similarity.port=" + similarityPort
				cliOptions = cli.CreateOptions{
					SkipModelDownload: false,
					ImagePullPolicy:   "IfNotPresent",
				}
			}
			createOutput, err := cli.CreateRAGAppAndValidate(
				ctx,
				cfg,
				appName,
				templateName,
				params,
				backendPort,
				uiPort,
				cliOptions,
				pods,
				appRuntime,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			if appRuntime == "podman" {
				ragBaseURL, err = cli.GetBaseURL(createOutput, backendPort)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				judgeBaseURL, err = cli.GetBaseURL(createOutput, judgePort)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			}
			logger.Infof("[TEST] Application %s created, healthy, and RAG endpoints validated", appName)
		})
	})
	ginkgo.Context("Application Observability", func() {
		ginkgo.It("verifies application ps output", ginkgo.Label("spyre-dependent"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			cases := map[string][]string{
				"normal": nil,
				"wide":   {"-o", "wide"},
			}

			for name, flags := range cases {
				ginkgo.By(fmt.Sprintf("running application ps %s", name))

				output, err := cli.ApplicationPS(ctx, cfg, appName, appRuntime, flags...)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cli.ValidateApplicationPS(output)).To(gomega.Succeed())
			}
		})
		ginkgo.It("verifies application info output", ginkgo.Label("spyre-dependent"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			infoOutput, err := cli.ApplicationInfo(ctx, cfg, appName, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Expect(cli.ValidateApplicationInfo(infoOutput, appName, templateName)).To(gomega.Succeed())
			logger.Infof("[TEST] Application info output validated successfully!")
		})
		ginkgo.It("Verifies pods existence, health status  and restart count", ginkgo.Label("spyre-dependent"), func() {
			if !podmanReady {
				ginkgo.Skip("Podman not available - will be installed via bootstrap configure")
			}
			psWideArgs := []string{"-o", "wide"}
			widePsOutput, err := cli.ApplicationPS(ctx, cfg, appName, appRuntime, psWideArgs...)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			err = podman.VerifyContainers(ctx, cfg, widePsOutput, appName, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "verify containers failed")
			logger.Infof("[TEST] Containers verified")
		})
		ginkgo.It("Verifies Exposed Ports/Routes of the application", ginkgo.Label("spyre-dependent"), func() {
			if !podmanReady {
				ginkgo.Skip("Podman not available - will be installed via bootstrap configure")
			}
			if appRuntime == "podman" {
				psWideArgs := []string{"-o", "wide"}
				widePsOutput, err := cli.ApplicationPS(ctx, cfg, appName, appRuntime, psWideArgs...)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				expectedPorts := []string{uiPort, backendPort, digitizePort, digitizeUiPort, summarizePort, similarityPort}
				gomega.Expect(podman.VerifyExposedPorts(appName, expectedPorts, appRuntime, widePsOutput)).NotTo(gomega.HaveOccurred(), "Verify exposed ports/routes failed")
			} else {
				output, err := podman.GetOpenshiftRoutes(appName)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cli.ValidateOpenShiftRoutes(output)).NotTo(gomega.HaveOccurred(), "Verify exposed ports/routes failed")
			}
			logger.Infof("[TEST] Exposed ports/routes verified")
		})
		ginkgo.It("verifies application logs output", ginkgo.Label("spyre-dependent"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			psWideArgs := []string{"-o", "wide"}
			widePsOutput, err := cli.ApplicationPS(ctx, cfg, appName, appRuntime, psWideArgs...)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			pods, err := podman.ExtractPodInfo(widePsOutput)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(pods).NotTo(gomega.BeEmpty(), "No pods found for application %s", appName)

			for podName, pod := range pods {

				// ---- Pod logs by NAME
				{
					logCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					logs, err := cli.ApplicationLogs(logCtx, cfg, appName, podName, "", appRuntime)
					cancel()

					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(logs).NotTo(gomega.BeEmpty())
					gomega.Expect(cli.ValidateApplicationLogs(logs, podName, "")).To(gomega.Succeed())
				}

				// ---- Pod logs by ID
				if appRuntime == "podman" {
					logCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					logs, err := cli.ApplicationLogs(logCtx, cfg, appName, pod.PodID, "", appRuntime)
					cancel()

					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(logs).NotTo(gomega.BeEmpty())
					gomega.Expect(cli.ValidateApplicationLogs(logs, pod.PodID, "")).To(gomega.Succeed())
				}

				for _, container := range pod.Containers {
					logCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					logs, err := cli.ApplicationLogs(logCtx, cfg, appName, pod.PodID, container, appRuntime)
					cancel()

					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(logs).NotTo(gomega.BeEmpty())
					gomega.Expect(cli.ValidateApplicationLogs(logs, pod.PodID, container)).To(gomega.Succeed())
				}
			}
		})
	})
	ginkgo.Context("Runtime Operations", func() {
		ginkgo.It("stops the application", ginkgo.Label("spyre-dependent"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			suffixes, ok := common.ExpectedPodSuffixes[appRuntime]
			gomega.Expect(ok).To(gomega.BeTrue(), "unknown templateName")

			pods := make([]string, 0, len(suffixes))
			for _, s := range suffixes {
				pods = append(pods, fmt.Sprintf("%s--%s", appName, s))
			}

			output, err := cli.StopAppWithPods(ctx, cfg, appName, pods, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(output).NotTo(gomega.BeEmpty())

			logger.Infof("[TEST] Application %s stopped successfully using --pod", appName)
		})
		ginkgo.It("starts application pods", ginkgo.Label("spyre-dependent"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			output, err := cli.StartApplication(
				ctx,
				cfg,
				appName,
				appRuntime,
				cli.StartOptions{
					SkipLogs: false,
				},
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(output).NotTo(gomega.BeEmpty())
			logger.Infof("[TEST] Application %s started successfully", appName)
		})

	})
	ginkgo.Context("Ingestion Tests", func() {
		ginkgo.BeforeEach(func() {
			if appRuntime == "openshift" {
				ginkgo.Skip("Skipping Ingestion Tests for OpenShift runtime")
			}
			err := ingestion.CleanDocsFolder(appName)
			if err != nil {
				ginkgo.Fail("Failed to clean application docs directory")
			}
		})
		ginkgo.It("starts document ingestion pod and validates ingestion completion", ginkgo.Label("spyre-dependent"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
			defer cancel()

			completionStr := "| /var/docs/test_doc.pdf |"
			gomega.Expect(appName).NotTo(gomega.BeEmpty())

			gomega.Expect(ingestion.PrepareDocs(appName, "test_doc.pdf")).To(gomega.Succeed())

			logs, err := ingestion.StartIngestion(ctx, cfg, appName, completionStr, false, appRuntime)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(logs).To(gomega.ContainSubstring("Ingestion started"))
			gomega.Expect(logs).To(gomega.ContainSubstring(completionStr))

			logger.Infof("[TEST] Valid File Ingestion completed successfully for application %s", appName)
		})
		ginkgo.It("ingestion should not fail while ingesting a blank pdf", ginkgo.Label("spyre-dependent"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
			defer cancel()

			completionStr := "| /var/docs/blank.pdf |"
			gomega.Expect(appName).NotTo(gomega.BeEmpty())

			gomega.Expect(ingestion.PrepareDocs(appName, "blank.pdf")).To(gomega.Succeed())

			logs, err := ingestion.StartIngestion(ctx, cfg, appName, completionStr, false, appRuntime)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(logs).To(gomega.ContainSubstring("Ingestion started"))
			gomega.Expect(logs).To(gomega.ContainSubstring(completionStr))

			logger.Infof("[TEST] Blank File Ingestion completed successfully for application %s", appName)
		})
		ginkgo.It("ingestion should fail while ingesting an invalid pdf", ginkgo.Label("spyre-dependent"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
			defer cancel()

			completionStr := "File validation failed: File has .pdf extension but unsupported format: sample_png.pdf"
			gomega.Expect(appName).NotTo(gomega.BeEmpty())

			gomega.Expect(ingestion.PrepareDocs(appName, "sample_png.pdf")).To(gomega.Succeed())

			logs, err := ingestion.StartIngestion(ctx, cfg, appName, completionStr, false, appRuntime)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(logs).To(gomega.ContainSubstring("Ingestion started"))
			gomega.Expect(logs).To(gomega.ContainSubstring(completionStr))

			logger.Infof("[TEST] Invalid PDF File Ingestion completed successfully for application %s", appName)
		})
		ginkgo.It("ingestion should not fail while ingesting an invalid file", ginkgo.Label("spyre-dependent"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
			defer cancel()

			completionStr := "File validation failed: Only PDF files are allowed. Invalid file: sample_txt.txt"
			gomega.Expect(appName).NotTo(gomega.BeEmpty())

			gomega.Expect(ingestion.PrepareDocs(appName, "sample_txt.txt")).To(gomega.Succeed())

			logs, err := ingestion.StartIngestion(ctx, cfg, appName, completionStr, false, appRuntime)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(logs).To(gomega.ContainSubstring("Ingestion started"))
			gomega.Expect(logs).To(gomega.ContainSubstring(completionStr))

			logger.Infof("[TEST] Invalid File Ingestion completed successfully for application %s", appName)
		})
	})
	ginkgo.Context("RAG Golden Dataset Validation", ginkgo.Label("golden-dataset-validation"), func() {
		ginkgo.BeforeAll(func() {
			if appRuntime == "openshift" {
				ginkgo.Skip("Skipping RAG Golden Dataset Validation for OpenShift runtime")
			}
			if appName == "" {
				ginkgo.Fail("Application name is not set")
			}

			logger.Infof("[RAG] Setting golden dataset path")
			goldenDatasetFile = bootstrap.GetGoldenDatasetFile()
			if goldenDatasetFile == "" {
				ginkgo.Fail("GOLDEN_DATASET_FILE environment variable is not set")
			}

			_, filename, _, _ := runtime.Caller(0)                        // returns the file path of this test file (e2e_suite_test.go)
			e2eDir := filepath.Dir(filename)                              // resolves ai-services/tests/e2e
			repoRoot := filepath.Clean(filepath.Join(e2eDir, "../../..")) // navigates to the workspace root

			goldenPath = filepath.Join(
				repoRoot,
				"test",
				"golden",
				goldenDatasetFile,
			)
			logger.Infof("[RAG] Golden dataset file: %s", goldenPath)

			logger.Infof("[RAG] Fetching application info to derive RAG and Judge URLs")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			infoOutput, err := cli.ApplicationInfo(ctx, cfg, appName, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			if err := cli.ValidateApplicationInfo(infoOutput, appName, templateName); err != nil {
				ginkgo.Fail(fmt.Sprintf("Golden dataset validation requires a valid running application: %v", err))
			}

			ragBaseURL, err = cli.GetBaseURL(infoOutput, backendPort)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			judgeBaseURL, err = cli.GetBaseURL(infoOutput, judgePort)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			logger.Infof("[RAG] RAG Base URL: %s", ragBaseURL)
			logger.Infof("[RAG] Judge Base URL: %s", judgeBaseURL)

			logger.Infof("[RAG] Setting up LLM-as-Judge")
			if err := rag.SetupLLMAsJudge(ctx, cfg, runID); err != nil {
				ginkgo.Fail(fmt.Sprintf("failed to setup LLM-as-Judge: %v", err))
			}
		})

		ginkgo.AfterAll(func() {
			if appRuntime == "openshift" {
				ginkgo.Skip("Skipping Judge cleanup for OpenShift runtime")
			}
			if err := rag.CleanupLLMAsJudge(runID); err != nil {
				logger.Warningf("[RAG][WARN] Judge cleanup failed: %v", err)
			}
		})

		ginkgo.It("validates RAG answers against golden dataset", ginkgo.Label("spyre-dependent"), func() {
			logger.Infof("[RAG] Starting golden dataset validation")
			cases, err := rag.LoadGoldenCSV(goldenPath)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cases).NotTo(gomega.BeEmpty())

			total := len(cases)
			results := make([]rag.EvalResult, 0, total)
			passed := 0

			for i, tc := range cases {
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
				defer cancel()

				result := rag.EvalResult{
					Question: tc.Question,
					Passed:   false,
				}

				// 1. Ask RAG
				ragAns, ragErr := rag.RunWithRetry(ctx, defaultMaxRetries, func(ctx context.Context) (string, error) {
					return rag.AskRAG(ctx, ragBaseURL, tc.Question)
				})

				if ragErr != nil {
					result.Details = fmt.Sprintf("RAG request failed: %v", ragErr)
					results = append(results, result)

					continue
				}

				// 2. Ask Judge with format retry
				verdict, reason, err := rag.AskJudgeWithFormatRetry(
					ctx,
					defaultMaxRetries,
					judgeBaseURL,
					tc.Question,
					ragAns,
					tc.GoldenAnswer,
				)
				if err != nil {
					result.Details = fmt.Sprintf("Judge failed: %v", err)
					results = append(results, result)

					continue
				}

				result.Passed = verdict == "YES"
				result.Details = reason

				if result.Passed {
					passed++
				}

				results = append(results, result)
				logger.Infof("[RAG] Evaluated question %d/%d | verdict=%s | reason=%s", i+1, total, verdict, reason)
			}

			accuracy := float64(passed) / float64(total)
			rag.PrintValidationSummary(results, accuracy)

			if accuracy < defaultRagAccuracyThreshold {
				ginkgo.Fail(fmt.Sprintf(
					"RAG accuracy %.2f below threshold %.2f",
					accuracy,
					defaultRagAccuracyThreshold,
				))
			}

			logger.Infof("[RAG] Golden dataset validation completed")
		})
	})
	ginkgo.Context("Clean Ingestion Docs", func() {
		ginkgo.It("cleans the ingestion docs from the db", ginkgo.Label("spyre-dependent"), func() {
			if appRuntime == "openshift" {
				ginkgo.Skip("Skipping Clean Ingestion Docs for OpenShift runtime")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()

			completionStr := "DB cleanup completed successfully"
			gomega.Expect(appName).NotTo(gomega.BeEmpty())

			logs, err := ingestion.StartIngestion(ctx, cfg, appName, completionStr, true, appRuntime)

			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(logs).To(gomega.ContainSubstring(completionStr))

			logger.Infof("[TEST] Clean Ingestion completed successfully for application %s", appName)
		})
	})
	ginkgo.Context("Digitization Tests", ginkgo.Label("spyre-dependent", "digitization-tests"), func() {
		var digitizeBaseURL string
		var createdJobIDs []string
		var createdDocIDs []string

		ginkgo.BeforeAll(func() {
			if appName == "" {
				ginkgo.Fail("Application name is not set")
			}

			logger.Infof("[DIGITIZE] Setting up digitization tests")

			// Get the digitize base URL
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			infoOutput, err := cli.ApplicationInfo(ctx, cfg, appName, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			if err := cli.ValidateApplicationInfo(infoOutput, appName, templateName); err != nil {
				ginkgo.Fail(fmt.Sprintf("Digitization tests require a valid running application: %v", err))
			}

			if appRuntime == "podman" {
				digitizeBaseURL, err = cli.GetBaseURL(infoOutput, digitizePort)
			} else {
				urlList := cli.ExtractURLsFromOutput(infoOutput)
				if len(urlList) == 0 {
					ginkgo.Fail("No urls extracted from application info output")
				} else {
					digitizeBaseURL = strings.Replace(urlList[0], "ui", "digitize-api", 1)
				}

			}

			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			logger.Infof("[DIGITIZE] Digitize Base URL: %s", digitizeBaseURL)
		})

		ginkgo.AfterEach(func() {
			// Cleanup: delete created jobs and documents
			// Wait for jobs to complete before cleanup to avoid resource locked errors
			ctx := context.Background()
			for _, jobID := range createdJobIDs {
				// Wait for job completion before deleting
				_, _ = digitization.WaitForJobCompletion(ctx, digitizeBaseURL, jobID, 10*time.Minute)
				_ = digitization.DeleteJob(ctx, digitizeBaseURL, jobID)
			}
			for _, docID := range createdDocIDs {
				_ = digitization.DeleteDocument(ctx, digitizeBaseURL, docID)
			}
			createdJobIDs = nil
			createdDocIDs = nil
		})

		ginkgo.It("should pass health check", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			err := digitization.HealthCheck(ctx, digitizeBaseURL)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			logger.Infof("[TEST] Digitization service health check passed")
		})

		ginkgo.It("should complete full digitization workflow with job and document operations", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
			defer cancel()

			pdfPath := digitization.GetTestPDFPath()
			gomega.Expect(pdfPath).NotTo(gomega.BeEmpty())

			// Step 1: Create digitization job
			logger.Infof("[TEST] Step 1: Creating digitization job")
			jobResp, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "digitization", "json", "e2e-combined-workflow")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(jobResp).NotTo(gomega.BeNil())
			gomega.Expect(jobResp.JobID).NotTo(gomega.BeEmpty())
			createdJobIDs = append(createdJobIDs, jobResp.JobID)
			logger.Infof("[TEST] Created digitization job: %s", jobResp.JobID)

			// Step 2: Get job status immediately after creation
			logger.Infof("[TEST] Step 2: Getting job status")
			status, err := digitization.GetJobStatus(ctx, digitizeBaseURL, jobResp.JobID)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(status.JobID).To(gomega.Equal(jobResp.JobID))
			logger.Infof("[TEST] Job status retrieved: %s", status.Status)

			// Step 3: Wait for job completion (only wait ONCE for all checks)
			logger.Infof("[TEST] Step 3: Waiting for job completion")
			finalStatus, err := digitization.WaitForJobCompletion(ctx, digitizeBaseURL, jobResp.JobID, 10*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(finalStatus.Status).To(gomega.Equal("completed"))
			logger.Infof("[TEST] Digitization job completed: %s", jobResp.JobID)

			// Step 4: List jobs with pagination
			logger.Infof("[TEST] Step 4: Listing jobs with pagination")
			jobsList, err := digitization.ListJobs(ctx, digitizeBaseURL, false, 20, 0, "", "")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(jobsList.Data).NotTo(gomega.BeEmpty())
			logger.Infof("[TEST] Listed %d jobs", len(jobsList.Data))

			// Step 5: Get latest job
			logger.Infof("[TEST] Step 5: Getting latest job")
			latestJobsList, err := digitization.ListJobs(ctx, digitizeBaseURL, true, 1, 0, "", "")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(latestJobsList.Data).To(gomega.HaveLen(1))
			gomega.Expect(latestJobsList.Data[0].JobID).To(gomega.Equal(jobResp.JobID))
			logger.Infof("[TEST] Latest job retrieved: %s", latestJobsList.Data[0].JobID)

			// Step 6: List jobs with filters (digitization only)
			logger.Infof("[TEST] Step 6: Listing jobs with operation filter")
			filteredJobsList, err := digitization.ListJobs(ctx, digitizeBaseURL, false, 20, 0, "", "digitization")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			for _, job := range filteredJobsList.Data {
				gomega.Expect(job.Operation).To(gomega.Equal("digitization"))
			}
			logger.Infof("[TEST] Listed %d digitization jobs with filter", len(filteredJobsList.Data))

			// Step 7: Get document ID from completed job
			logger.Infof("[TEST] Step 7: Getting document details")
			gomega.Expect(finalStatus.Documents).NotTo(gomega.BeEmpty())
			docID := finalStatus.Documents[0].ID
			createdDocIDs = append(createdDocIDs, docID)

			// Step 8: Get document details
			doc, err := digitization.GetDocument(ctx, digitizeBaseURL, docID)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(doc.ID).To(gomega.Equal(docID))
			gomega.Expect(doc.JobID).To(gomega.Equal(jobResp.JobID))
			gomega.Expect(doc.Name).To(gomega.Equal("test_doc.pdf"))
			gomega.Expect(doc.Type).To(gomega.Equal("digitization"))
			gomega.Expect(doc.Status).To(gomega.Equal("completed"))
			gomega.Expect(doc.OutputFormat).To(gomega.Equal("json"))
			logger.Infof("[TEST] Document details retrieved: %s (filename: %s)", doc.ID, doc.Name)

			// Step 9: Get document content
			logger.Infof("[TEST] Step 8: Getting document content")
			content, err := digitization.GetDocumentContent(ctx, digitizeBaseURL, docID)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(content.Result).NotTo(gomega.BeNil())
			gomega.Expect(content.OutputFormat).To(gomega.Equal("json"))
			// For JSON format, Result should be a map
			resultMap, ok := content.Result.(map[string]interface{})
			gomega.Expect(ok).To(gomega.BeTrue(), "Result should be a map for JSON format")
			gomega.Expect(resultMap).NotTo(gomega.BeEmpty())
			logger.Infof("[TEST] Document content retrieved successfully")

			// Step 10: List all documents
			logger.Infof("[TEST] Step 9: Listing all documents")
			docsList, err := digitization.ListDocuments(ctx, digitizeBaseURL, 20, 0, "", "")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(docsList).NotTo(gomega.BeNil())
			gomega.Expect(docsList.Data).NotTo(gomega.BeEmpty())
			logger.Infof("[TEST] Listed %d documents", len(docsList.Data))

			// Step 11: List documents filtered by status
			logger.Infof("[TEST] Step 10: Listing documents filtered by status 'completed'")
			filteredDocsList, err := digitization.ListDocuments(ctx, digitizeBaseURL, 20, 0, "completed", "")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(filteredDocsList).NotTo(gomega.BeNil())
			for _, doc := range filteredDocsList.Data {
				gomega.Expect(doc.Status).To(gomega.Equal("completed"))
			}
			logger.Infof("[TEST] Listed %d completed documents", len(filteredDocsList.Data))

			// Step 12: List documents filtered by name
			logger.Infof("[TEST] Step 11: Listing documents filtered by name 'test_doc.pdf'")
			nameFilteredDocsList, err := digitization.ListDocuments(ctx, digitizeBaseURL, 20, 0, "", "test_doc.pdf")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(nameFilteredDocsList).NotTo(gomega.BeNil())
			for _, doc := range nameFilteredDocsList.Data {
				gomega.Expect(doc.Name).To(gomega.Equal("test_doc.pdf"))
			}
			logger.Infof("[TEST] Listed %d documents with name 'test_doc.pdf'", len(nameFilteredDocsList.Data))

			logger.Infof("[TEST] ✓ Full digitization workflow completed successfully")
		})

		ginkgo.It("should complete full ingestion workflow", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			defer cancel()

			pdfPath := digitization.GetTestPDFPath()
			gomega.Expect(pdfPath).NotTo(gomega.BeEmpty())

			logger.Infof("[TEST] Creating ingestion job")
			jobResp, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "ingestion", "json", "e2e-combined-ingestion")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			createdJobIDs = append(createdJobIDs, jobResp.JobID)

			logger.Infof("[TEST] Waiting for ingestion job completion")
			finalStatus, err := digitization.WaitForJobCompletion(ctx, digitizeBaseURL, jobResp.JobID, 15*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(finalStatus.Status).To(gomega.Equal("completed"))

			logger.Infof("[TEST] ✓ Ingestion job completed: %s", jobResp.JobID)
		})

		ginkgo.It("should support different output formats", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()

			pdfPath := digitization.GetTestPDFPath()
			formats := []string{"json", "md", "txt"}

			// Process formats sequentially to avoid exceeding concurrent limit
			for _, format := range formats {
				jobResp, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "digitization", format, fmt.Sprintf("e2e-format-%s", format))
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				createdJobIDs = append(createdJobIDs, jobResp.JobID)

				// Wait for each job to complete before starting the next
				finalStatus, err := digitization.WaitForJobCompletion(ctx, digitizeBaseURL, jobResp.JobID, 8*time.Minute)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(finalStatus.Status).To(gomega.Equal("completed"))

				logger.Infof("[TEST] %s format job completed", format)
			}
		})

		ginkgo.It("should handle job lifecycle including active job protection and deletion", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
			defer cancel()

			pdfPath := digitization.GetTestPDFPath()
			gomega.Expect(pdfPath).NotTo(gomega.BeEmpty())

			// Step 1: Create job
			logger.Infof("[TEST] Step 1: Creating job for lifecycle test")
			jobResp, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "digitization", "json", "e2e-job-lifecycle")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			createdJobIDs = append(createdJobIDs, jobResp.JobID)
			logger.Infof("[TEST] Created job: %s", jobResp.JobID)

			// Step 2: Try to delete active job (should fail with 409)
			logger.Infof("[TEST] Step 2: Testing active job deletion protection")
			time.Sleep(2 * time.Second) // Wait for job to start processing
			err = digitization.DeleteJob(ctx, digitizeBaseURL, jobResp.JobID)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(digitization.IsResourceLockedError(err)).To(gomega.BeTrue(),
				"Expected resource locked error (409), got: %v", err)
			logger.Infof("[TEST] ✓ Active job deletion correctly failed with resource locked error")

			// Step 3: Wait for job completion
			logger.Infof("[TEST] Step 3: Waiting for job completion")
			_, err = digitization.WaitForJobCompletion(ctx, digitizeBaseURL, jobResp.JobID, 10*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			logger.Infof("[TEST] Job completed successfully")

			// Step 4: Delete completed job (should succeed)
			logger.Infof("[TEST] Step 4: Deleting completed job")
			err = digitization.DeleteJob(ctx, digitizeBaseURL, jobResp.JobID)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			logger.Infof("[TEST] ✓ Completed job deleted successfully")

			// Step 5: Verify job is deleted (should return 404)
			logger.Infof("[TEST] Step 5: Verifying job deletion")
			_, err = digitization.GetJobStatus(ctx, digitizeBaseURL, jobResp.JobID)
			gomega.Expect(err).To(gomega.HaveOccurred())
			logger.Infof("[TEST] ✓ Job deletion verified (404 returned)")

			// Remove from cleanup list since we already deleted it
			createdJobIDs = createdJobIDs[:len(createdJobIDs)-1]

			logger.Infof("[TEST] ✓ Job lifecycle test completed successfully")
		})

		ginkgo.It("should handle document lifecycle including protection and deletion", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
			defer cancel()

			pdfPath := digitization.GetTestPDFPath()
			gomega.Expect(pdfPath).NotTo(gomega.BeEmpty())

			// Step 1: Create job
			logger.Infof("[TEST] Step 1: Creating job for document lifecycle test")
			jobResp, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "digitization", "json", "e2e-doc-lifecycle")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			createdJobIDs = append(createdJobIDs, jobResp.JobID)
			logger.Infof("[TEST] Created job: %s", jobResp.JobID)

			// Step 2: Try to delete in-progress document (should fail with 409)
			logger.Infof("[TEST] Step 2: Testing in-progress document deletion protection")
			time.Sleep(2 * time.Second) // Wait for job to start and document to be created

			// Get job status to retrieve document ID
			jobStatus, err := digitization.GetJobStatus(ctx, digitizeBaseURL, jobResp.JobID)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(jobStatus.Documents).NotTo(gomega.BeEmpty())
			docID := jobStatus.Documents[0].ID

			// Try to delete the in-progress document
			err = digitization.DeleteDocument(ctx, digitizeBaseURL, docID)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(digitization.IsResourceLockedError(err)).To(gomega.BeTrue(),
				"Expected resource locked error (409), got: %v", err)
			logger.Infof("[TEST] ✓ In-progress document deletion correctly failed with resource locked error")

			// Step 3: Wait for job completion
			logger.Infof("[TEST] Step 3: Waiting for job completion")
			finalStatus, err := digitization.WaitForJobCompletion(ctx, digitizeBaseURL, jobResp.JobID, 10*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			logger.Infof("[TEST] Job completed successfully")

			// Step 4: Delete completed document (should succeed)
			logger.Infof("[TEST] Step 4: Deleting completed document")
			gomega.Expect(finalStatus.Documents).NotTo(gomega.BeEmpty())
			docID = finalStatus.Documents[0].ID
			err = digitization.DeleteDocument(ctx, digitizeBaseURL, docID)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			logger.Infof("[TEST] ✓ Completed document deleted successfully")

			// Step 5: Verify document is deleted (should return 404)
			logger.Infof("[TEST] Step 5: Verifying document deletion")
			_, err = digitization.GetDocument(ctx, digitizeBaseURL, docID)
			gomega.Expect(err).To(gomega.HaveOccurred())
			logger.Infof("[TEST] ✓ Document deletion verified (404 returned)")

			logger.Infof("[TEST] ✓ Document lifecycle test completed successfully")
		})

		ginkgo.It("should delete all documents", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			defer cancel()

			// Create and complete jobs sequentially to avoid exceeding concurrent limit
			pdfPath := digitization.GetTestPDFPath()
			for i := 0; i < 2; i++ {
				jobResp, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "digitization", "json", fmt.Sprintf("e2e-delete-all-%d", i))
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				createdJobIDs = append(createdJobIDs, jobResp.JobID)

				// Wait for each job to complete before starting the next
				_, err = digitization.WaitForJobCompletion(ctx, digitizeBaseURL, jobResp.JobID, 8*time.Minute)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			}

			// Delete all documents
			err := digitization.DeleteAllDocuments(ctx, digitizeBaseURL)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Verify documents are deleted
			docsList, err := digitization.ListDocuments(ctx, digitizeBaseURL, 20, 0, "", "")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(docsList.Data).To(gomega.BeEmpty())

			logger.Infof("[TEST] All documents deleted successfully")
			createdDocIDs = nil // Clear since all are deleted
		})

		ginkgo.It("should reject multiple files for digitization operation", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			pdfPath := digitization.GetTestPDFPath()
			gomega.Expect(pdfPath).NotTo(gomega.BeEmpty())

			// Try to create a job with multiple files (using the same file twice for simplicity)
			filePaths := []string{pdfPath, pdfPath}
			errorResp, err := digitization.CreateJobWithMultipleFiles(ctx, digitizeBaseURL, filePaths, "digitization", "json", "e2e-multiple-files-test")

			// Should receive an error response, not a request error
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errorResp).NotTo(gomega.BeNil())

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("INVALID_REQUEST"))
			gomega.Expect(errorResp.Error.Message).To(gomega.Equal("Request validation failed: Only 1 file allowed for digitization."))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(400))

			logger.Infof("[TEST] Multiple files correctly rejected for digitization with error: %s", errorResp.Error.Message)
		})

		ginkgo.It("should reject third concurrent digitization job with rate limit error", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()

			pdfPath := digitization.GetTestPDFPath()
			gomega.Expect(pdfPath).NotTo(gomega.BeEmpty())

			// Create first digitization job
			job1, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "digitization", "json", "e2e-concurrent-1")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(job1).NotTo(gomega.BeNil())
			gomega.Expect(job1.JobID).NotTo(gomega.BeEmpty())
			createdJobIDs = append(createdJobIDs, job1.JobID)
			logger.Infof("[TEST] Created first digitization job: %s", job1.JobID)

			// Create second digitization job
			job2, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "digitization", "json", "e2e-concurrent-2")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(job2).NotTo(gomega.BeNil())
			gomega.Expect(job2.JobID).NotTo(gomega.BeEmpty())
			createdJobIDs = append(createdJobIDs, job2.JobID)
			logger.Infof("[TEST] Created second digitization job: %s", job2.JobID)

			// Try to create third digitization job - should fail with rate limit error
			errorResp, err := digitization.CreateJobExpectingError(ctx, digitizeBaseURL, pdfPath, "digitization", "json", "e2e-concurrent-3")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errorResp).NotTo(gomega.BeNil())

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("RATE_LIMIT_EXCEEDED"))
			gomega.Expect(errorResp.Error.Message).To(gomega.Equal("Too many requests: Too many concurrent OperationType.DIGITIZATION requests."))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(429))

			logger.Infof("[TEST] Third concurrent digitization job correctly rejected with rate limit error: %s", errorResp.Error.Message)

			// Wait for the first two jobs to complete before cleanup
			logger.Infof("[TEST] Waiting for concurrent jobs to complete before cleanup...")
			_, _ = digitization.WaitForJobCompletion(ctx, digitizeBaseURL, job1.JobID, 10*time.Minute)
			_, _ = digitization.WaitForJobCompletion(ctx, digitizeBaseURL, job2.JobID, 10*time.Minute)
		})

		ginkgo.It("should reject concurrent ingestion jobs with rate limit error", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			defer cancel()

			pdfPath := digitization.GetTestPDFPath()
			gomega.Expect(pdfPath).NotTo(gomega.BeEmpty())

			// Start the first ingestion job
			job1Resp, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "ingestion", "json", "e2e-concurrent-ingestion-1")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(job1Resp).NotTo(gomega.BeNil())
			gomega.Expect(job1Resp.JobID).NotTo(gomega.BeEmpty())
			createdJobIDs = append(createdJobIDs, job1Resp.JobID)

			// Wait a moment to ensure the first job starts processing
			time.Sleep(2 * time.Second)

			// Try to start a second ingestion job while the first is still running
			// This should fail with a 429 rate limit error
			errorResp, err := digitization.CreateJobExpectingError(ctx, digitizeBaseURL, pdfPath, "ingestion", "json", "e2e-concurrent-ingestion-2")

			// Should receive an error response, not a request error
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errorResp).NotTo(gomega.BeNil())

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("RATE_LIMIT_EXCEEDED"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("Too many requests: An ingestion job is already running"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(429))

			logger.Infof("[TEST] Concurrent ingestion job correctly rejected with rate limit error: %s", errorResp.Error.Message)

			// Wait for the first job to complete before cleanup
			_, err = digitization.WaitForJobCompletion(ctx, digitizeBaseURL, job1Resp.JobID, 15*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.It("should reject invalid PDF file for digitization operation", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Get path to invalid PDF (PNG file with .pdf extension)
			_, filename, _, ok := runtime.Caller(0)
			gomega.Expect(ok).To(gomega.BeTrue())
			testDir := filepath.Dir(filename)
			invalidPDFPath := filepath.Join(testDir, "ingestion", "docs", "sample_png.pdf")

			logger.Infof("[TEST] Testing digitization with invalid PDF file: %s", invalidPDFPath)

			// Try to create a digitization job with invalid PDF
			errorResp, err := digitization.CreateJobExpectingError(ctx, digitizeBaseURL, invalidPDFPath, "digitization", "json", "e2e-invalid-pdf-digitization")

			// Should receive an error response, not a request error
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errorResp).NotTo(gomega.BeNil())

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("UNSUPPORTED_MEDIA_TYPE"))
			gomega.Expect(errorResp.Error.Message).To(gomega.Equal("File format not supported: File has .pdf extension but unsupported format: sample_png.pdf"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(415))

			logger.Infof("[TEST] Invalid PDF correctly rejected for digitization with error: %s", errorResp.Error.Message)
		})

		ginkgo.It("should reject invalid PDF file for ingestion operation", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Get path to invalid PDF (PNG file with .pdf extension)
			_, filename, _, ok := runtime.Caller(0)
			gomega.Expect(ok).To(gomega.BeTrue())
			testDir := filepath.Dir(filename)
			invalidPDFPath := filepath.Join(testDir, "ingestion", "docs", "sample_png.pdf")

			logger.Infof("[TEST] Testing ingestion with invalid PDF file: %s", invalidPDFPath)

			// Try to create an ingestion job with invalid PDF
			errorResp, err := digitization.CreateJobExpectingError(ctx, digitizeBaseURL, invalidPDFPath, "ingestion", "json", "e2e-invalid-pdf-ingestion")

			// Should receive an error response, not a request error
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errorResp).NotTo(gomega.BeNil())

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("UNSUPPORTED_MEDIA_TYPE"))
			gomega.Expect(errorResp.Error.Message).To(gomega.Equal("File format not supported: File has .pdf extension but unsupported format: sample_png.pdf"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(415))

			logger.Infof("[TEST] Invalid PDF correctly rejected for ingestion with error: %s", errorResp.Error.Message)
		})

		ginkgo.It("should reject non-PDF file for digitization operation", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Get path to non-PDF file (TXT file)
			_, filename, _, ok := runtime.Caller(0)
			gomega.Expect(ok).To(gomega.BeTrue())
			testDir := filepath.Dir(filename)
			nonPDFPath := filepath.Join(testDir, "ingestion", "docs", "sample_txt.txt")

			logger.Infof("[TEST] Testing digitization with non-PDF file: %s", nonPDFPath)

			// Try to create a digitization job with non-PDF file
			errorResp, err := digitization.CreateJobExpectingError(ctx, digitizeBaseURL, nonPDFPath, "digitization", "json", "e2e-non-pdf-digitization")

			// Should receive an error response, not a request error
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errorResp).NotTo(gomega.BeNil())

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("UNSUPPORTED_MEDIA_TYPE"))
			gomega.Expect(errorResp.Error.Message).To(gomega.Equal("File format not supported: Only PDF files are allowed. Invalid file: sample_txt.txt"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(415))

			logger.Infof("[TEST] Non-PDF file correctly rejected for digitization with error: %s", errorResp.Error.Message)
		})

		ginkgo.It("should reject non-PDF file for ingestion operation", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Get path to non-PDF file (TXT file)
			_, filename, _, ok := runtime.Caller(0)
			gomega.Expect(ok).To(gomega.BeTrue())
			testDir := filepath.Dir(filename)
			nonPDFPath := filepath.Join(testDir, "ingestion", "docs", "sample_txt.txt")

			logger.Infof("[TEST] Testing ingestion with non-PDF file: %s", nonPDFPath)

			// Try to create an ingestion job with non-PDF file
			errorResp, err := digitization.CreateJobExpectingError(ctx, digitizeBaseURL, nonPDFPath, "ingestion", "json", "e2e-non-pdf-ingestion")

			// Should receive an error response, not a request error
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errorResp).NotTo(gomega.BeNil())

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("UNSUPPORTED_MEDIA_TYPE"))
			gomega.Expect(errorResp.Error.Message).To(gomega.Equal("File format not supported: Only PDF files are allowed. Invalid file: sample_txt.txt"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(415))

			logger.Infof("[TEST] Non-PDF file correctly rejected for ingestion with error: %s", errorResp.Error.Message)
		})

		ginkgo.It("should return 404 error when getting job with invalid ID", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			invalidJobID := "invalid-job-id-123"
			logger.Infof("[TEST] Testing GetJobStatus with invalid ID: %s", invalidJobID)

			// Try to get job status with invalid ID
			errorResp, err := digitization.GetJobStatusExpectingError(ctx, digitizeBaseURL, invalidJobID)

			// Should receive an error response, not a request error
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errorResp).NotTo(gomega.BeNil())

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("RESOURCE_NOT_FOUND"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("No job found with id"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("not found"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(404))

			logger.Infof("[TEST] ✓ GetJobStatus correctly returned 404 for invalid ID: %s", errorResp.Error.Message)
		})

		ginkgo.It("should return 404 error when getting document with invalid ID", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			invalidDocID := "invalid-doc-id-123"
			logger.Infof("[TEST] Testing GetDocument with invalid ID: %s", invalidDocID)

			// Try to get document with invalid ID
			errorResp, err := digitization.GetDocumentExpectingError(ctx, digitizeBaseURL, invalidDocID)

			// Should receive an error response, not a request error
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errorResp).NotTo(gomega.BeNil())

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("RESOURCE_NOT_FOUND"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("Document with ID"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("not found"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(404))

			logger.Infof("[TEST] ✓ GetDocument correctly returned 404 for invalid ID: %s", errorResp.Error.Message)
		})

		ginkgo.It("should return 404 error when getting document content with invalid ID", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			invalidDocID := "invalid-doc-id-123"
			logger.Infof("[TEST] Testing GetDocumentContent with invalid ID: %s", invalidDocID)

			// Try to get document content with invalid ID
			errorResp, err := digitization.GetDocumentContentExpectingError(ctx, digitizeBaseURL, invalidDocID)

			// Should receive an error response, not a request error
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errorResp).NotTo(gomega.BeNil())

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("RESOURCE_NOT_FOUND"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("Document with ID"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("not found"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(404))

			logger.Infof("[TEST] ✓ GetDocumentContent correctly returned 404 for invalid ID: %s", errorResp.Error.Message)
		})

		ginkgo.It("should return 404 error when deleting job with invalid ID", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			invalidJobID := "invalid-job-id-123"
			logger.Infof("[TEST] Testing DeleteJob with invalid ID: %s", invalidJobID)

			// Try to delete job with invalid ID
			errorResp, err := digitization.DeleteJobExpectingError(ctx, digitizeBaseURL, invalidJobID)

			// Should receive an error response, not a request error
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errorResp).NotTo(gomega.BeNil())

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("RESOURCE_NOT_FOUND"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("No job found with id"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("not found"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(404))

			logger.Infof("[TEST] ✓ DeleteJob correctly returned 404 for invalid ID: %s", errorResp.Error.Message)
		})

		//TODO- Enable once the response is fixed.
		ginkgo.XIt("should return 404 error when deleting document with invalid ID", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			invalidDocID := "invalid-doc-id-123"
			logger.Infof("[TEST] Testing DeleteDocument with invalid ID: %s", invalidDocID)

			// Try to delete document with invalid ID
			errorResp, err := digitization.DeleteDocumentExpectingError(ctx, digitizeBaseURL, invalidDocID)

			// Should receive an error response, not a request error
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errorResp).NotTo(gomega.BeNil())

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("RESOURCE_NOT_FOUND"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("Document with ID"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("not found"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(404))

			logger.Infof("[TEST] ✓ DeleteDocument correctly returned 404 for invalid ID: %s", errorResp.Error.Message)
		})

		ginkgo.It("should successfully process blank PDF file for digitization operation", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
			defer cancel()

			// Get path to blank PDF file
			_, filename, _, ok := runtime.Caller(0)
			gomega.Expect(ok).To(gomega.BeTrue())
			testDir := filepath.Dir(filename)
			blankPDFPath := filepath.Join(testDir, "ingestion", "docs", "blank.pdf")

			logger.Infof("[TEST] Testing digitization with blank PDF file: %s", blankPDFPath)

			// Create digitization job with blank PDF
			jobResp, err := digitization.CreateJob(ctx, digitizeBaseURL, blankPDFPath, "digitization", "json", "e2e-blank-pdf-digitization")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(jobResp).NotTo(gomega.BeNil())
			gomega.Expect(jobResp.JobID).NotTo(gomega.BeEmpty())
			createdJobIDs = append(createdJobIDs, jobResp.JobID)
			logger.Infof("[TEST] Created digitization job with blank PDF: %s", jobResp.JobID)

			// Wait for job completion
			logger.Infof("[TEST] Waiting for blank PDF digitization job completion")
			finalStatus, err := digitization.WaitForJobCompletion(ctx, digitizeBaseURL, jobResp.JobID, 10*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(finalStatus.Status).To(gomega.Equal("completed"))
			logger.Infof("[TEST] ✓ Blank PDF digitization job completed successfully: %s", jobResp.JobID)

			// Verify document was created
			gomega.Expect(finalStatus.Documents).NotTo(gomega.BeEmpty())
			docID := finalStatus.Documents[0].ID
			createdDocIDs = append(createdDocIDs, docID)

			// Get document details
			doc, err := digitization.GetDocument(ctx, digitizeBaseURL, docID)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(doc.Status).To(gomega.Equal("completed"))
			gomega.Expect(doc.Name).To(gomega.Equal("blank.pdf"))
			logger.Infof("[TEST] ✓ Blank PDF digitization completed successfully")
		})

		ginkgo.It("should successfully process blank PDF file for ingestion operation", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			defer cancel()

			// Get path to blank PDF file
			_, filename, _, ok := runtime.Caller(0)
			gomega.Expect(ok).To(gomega.BeTrue())
			testDir := filepath.Dir(filename)
			blankPDFPath := filepath.Join(testDir, "ingestion", "docs", "blank.pdf")

			logger.Infof("[TEST] Testing ingestion with blank PDF file: %s", blankPDFPath)

			// Create ingestion job with blank PDF
			jobResp, err := digitization.CreateJob(ctx, digitizeBaseURL, blankPDFPath, "ingestion", "json", "e2e-blank-pdf-ingestion")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(jobResp).NotTo(gomega.BeNil())
			gomega.Expect(jobResp.JobID).NotTo(gomega.BeEmpty())
			createdJobIDs = append(createdJobIDs, jobResp.JobID)
			logger.Infof("[TEST] Created ingestion job with blank PDF: %s", jobResp.JobID)

			// Wait for job completion
			logger.Infof("[TEST] Waiting for blank PDF ingestion job completion")
			finalStatus, err := digitization.WaitForJobCompletion(ctx, digitizeBaseURL, jobResp.JobID, 15*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(finalStatus.Status).To(gomega.Equal("completed"))
			logger.Infof("[TEST] ✓ Blank PDF ingestion job completed successfully: %s", jobResp.JobID)

			// Verify document was created
			gomega.Expect(finalStatus.Documents).NotTo(gomega.BeEmpty())
			docID := finalStatus.Documents[0].ID
			createdDocIDs = append(createdDocIDs, docID)

			// Get document details
			doc, err := digitization.GetDocument(ctx, digitizeBaseURL, docID)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(doc.Status).To(gomega.Equal("completed"))
			gomega.Expect(doc.Name).To(gomega.Equal("blank.pdf"))
			logger.Infof("[TEST] ✓ Blank PDF ingestion completed successfully")
		})
	})
	ginkgo.Context("Application Teardown", func() {
		ginkgo.It("deletes the application using --skip-cleanup", ginkgo.Label("spyre-dependent"), func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()

			output, err := cli.DeleteAppSkipCleanup(ctx, cfg, appName, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(output).NotTo(gomega.BeEmpty())

			logger.Infof("[TEST] Application %s deleted successfully using --skip-cleanup", appName)
		})
	})
})
