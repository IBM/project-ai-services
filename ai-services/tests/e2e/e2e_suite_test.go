package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
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
	"github.com/project-ai-services/ai-services/tests/e2e/podman"
	"github.com/project-ai-services/ai-services/tests/e2e/rag"
	"github.com/project-ai-services/ai-services/tests/e2e/similarity"
	"github.com/project-ai-services/ai-services/tests/e2e/summarization"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

var (
	cfg                         *config.Config
	runID                       string
	appName                     string
	providedAppName             string
	providedTemplate            string
	appRuntime                  string
	deleteExistingApp           bool
	runFailureTests             bool
	tempDir                     string
	tempBinDir                  string
	aiServiceBin                string
	binVersion                  string
	ctx                         context.Context
	podmanReady                 bool
	templateName                string
	goldenPath                  string
	ragBaseURL                  string
	summarizeBaseURL            string
	judgeBaseURL                string
	backendPort                 string
	uiPort                      string
	digitizePort                string
	digitizeUiPort              string
	summarizePort               string
	similarityPort              string
	judgePort                   string
	goldenDatasetFile           string
	defaultRagAccuracyThreshold = 0.70 //nolint:mnd
	defaultMaxRetries           = 2    //nolint:mnd
	// catalogBackendURL is captured by the catalog configure step and used for the pre-create login.
	catalogBackendURL string
	// createParams holds the value of CREATE_PARAMS env var and is passed as --params to
	// 'application create'. Empty string means use the template defaults (5-card Spyre setup).
	createParams string
	// appPsWideOutput caches the last 'application ps -o wide' result so that
	// subsequent specs (pods existence, logs) can reuse it without a second CLI call.
	appPsWideOutput string
	backupAppName   string
)

func init() {
	flag.StringVar(&providedAppName, "app-name", "", "Use existing application instead of creating one")
	flag.StringVar(&providedTemplate, "template", "rag", "Template to use for application creation (rag, summarize, digitize)")
	flag.BoolVar(&deleteExistingApp, "delete-app", false, "Delete existing app before proceeding ahead with test run")
	flag.StringVar(&appRuntime, "runtime", "podman", "Runtime on which the app will be deployed")
	flag.BoolVar(&runFailureTests, "run-failure-tests", false,
		"Opt in to running failure test suites (bootstrap, catalog, similarity). "+
			"Failure tests are skipped by default to prevent accidental execution during a normal suite run.")
}

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	// Set suite timeout to 24h to prevent Ginkgo's 1-hour default from firing.
	// Individual spec budgets are enforced via NodeTimeout/SpecTimeout/context.WithTimeout.
	suiteConfig, _ := ginkgo.GinkgoConfiguration()
	suiteConfig.Timeout = 24 * time.Hour //nolint:mnd
	ginkgo.RunSpecs(t, "AI Services E2E Suite",
		ginkgo.Label("e2e"),
		suiteConfig,
	)
}

func getEnvWithDefault(key, defaultValue string) string {
	if envValue := os.Getenv(key); envValue != "" {
		return envValue
	}

	return defaultValue
}

// testFilePath resolves a path relative to this test file's directory.
func testFilePath(rel string) string {
	_, filename, _, ok := runtime.Caller(1)
	if !ok {
		return ""
	}

	return filepath.Join(filepath.Dir(filename), rel)
}

// withTimeout returns a context.Background-rooted context with the given timeout.
func withTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// expectErrResp asserts no transport error and a non-nil error response body.
func expectErrResp(err error, errorResp any) {
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(errorResp).NotTo(gomega.BeNil())
}

// deleteDigitizeDocsFromJob deletes every document returned by a completed job
// status. Used for intra-loop cleanup where the same file would otherwise
// trigger 409 RESOURCE_LOCKED on the next CreateJob call.
func deleteDigitizeDocsFromJob(baseURL string, docs []digitization.DocumentStatus) {
	for _, doc := range docs {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = digitization.DeleteDocument(cleanCtx, baseURL, doc.ID)
		cleanCancel()
	}
}

// waitForSummarizeURL polls application info until the summarize-api URL is
// present, or the ctx deadline fires (which calls ginkgo.Fail). Returns the URL.
func waitForSummarizeURL(logPrefix string, timeout time.Duration) string {
	pollCtx, pollCancel := context.WithTimeout(context.Background(), timeout)
	defer pollCancel()

	const pollInterval = 15 * time.Second
	var (
		infoOutput string
		resultURL  string
	)
	for {
		var infoErr error
		infoOutput, infoErr = cli.ApplicationInfo(pollCtx, cfg, appName, appRuntime)
		if infoErr != nil {
			logger.Warningf("[%s] application info error while polling for summarize URL: %v", logPrefix, infoErr)
		} else {
			resultURL = cli.ExtractCatalogSummarizeURL(infoOutput)
			if resultURL != "" {
				break
			}
		}
		if pollCtx.Err() != nil {
			ginkgo.Fail(fmt.Sprintf(
				"[%s] Timed out waiting for summarize-api URL in 'application info' for app %q.\nLast output:\n%s",
				logPrefix, appName, infoOutput))
		}
		logger.Infof("[%s] summarize-api URL not yet present — retrying in %s", logPrefix, pollInterval)
		select {
		case <-pollCtx.Done():
			ginkgo.Fail(fmt.Sprintf("[%s] Timed out waiting for summarize-api URL for app %q", logPrefix, appName))
		case <-time.After(pollInterval):
		}
	}
	return resultURL
}

// waitForSummarizeHealthy polls /health on the given summarize URL until the
// service reports healthy, or the ctx deadline fires (which calls ginkgo.Fail).
func waitForSummarizeHealthy(logPrefix, baseURL string, timeout time.Duration) {
	healthCtx, healthCancel := context.WithTimeout(context.Background(), timeout)
	defer healthCancel()

	const pollInterval = 15 * time.Second
	logger.Infof("[%s] Waiting for summarize-api to be healthy at %s/health", logPrefix, baseURL)
	for {
		if err := summarization.HealthCheck(healthCtx, baseURL); err == nil {
			logger.Infof("[%s] summarize-api is healthy", logPrefix)
			return
		} else {
			if healthCtx.Err() != nil {
				ginkgo.Fail(fmt.Sprintf(
					"[%s] Timed out waiting for summarize-api to become healthy at %s — last error: %v",
					logPrefix, baseURL, err))
			}
			logger.Infof("[%s] summarize-api not yet healthy (%v) — retrying in %s", logPrefix, err, pollInterval)
			select {
			case <-healthCtx.Done():
				ginkgo.Fail(fmt.Sprintf("[%s] Timed out waiting for summarize-api to become healthy at %s", logPrefix, baseURL))
			case <-time.After(pollInterval):
			}
		}
	}
}

// catalogLoginWithDiscovery logs in to the catalog, auto-discovering the server URL; fatal=true fails the suite on error.
func catalogLoginWithDiscovery(loginCtx context.Context, fatal bool) {
	_, loginUsername, loginPassword := bootstrap.GetCatalogCreds()
	loginInsecure := bootstrap.GetCatalogInsecure()
	if loginPassword == "" {
		return
	}

	serverURL := catalogBackendURL
	if serverURL == "" {
		serverURL = os.Getenv("CATALOG_SERVER_URL")
	}
	if serverURL == "" {
		if infoOut, infoErr := cli.CatalogInfo(loginCtx, cfg, appRuntime); infoErr == nil {
			serverURL = cli.ExtractCatalogBackendURL(infoOut)
		}
	}
	if serverURL == "" || loginUsername == "" {
		logger.Warningf("[TEST] Skipping catalog login — server URL or credentials not available")

		return
	}

	_, loginErr := cli.CatalogLogin(loginCtx, cfg, serverURL, loginUsername, loginPassword, appRuntime, loginInsecure)
	if loginErr != nil {
		if fatal {
			ginkgo.Fail(fmt.Sprintf("Catalog login failed: %v (server: %s, user: %s)", loginErr, serverURL, loginUsername))
		} else {
			logger.Warningf("[SETUP] [WARNING] BeforeSuite catalog login failed (non-fatal): %v", loginErr)
		}

		return
	}

	logger.Infof("[TEST] Catalog login successful (server: %s, user: %s)", serverURL, loginUsername)
}

const (
	invalidJobID = "invalid-job-id-123"
	invalidDocID = "invalid-doc-id-123"

	// unreachableSummarizeURL points to a host that will never accept TCP
	// connections, used in the connectivity failure test to exercise
	// transport-level error handling.  The .invalid TLD is guaranteed by
	// RFC 2606 to never resolve in DNS.
	unreachableSummarizeURL = "http://summarize.invalid.ais-failure-test.example.com:9999"
)

// jobStartDelay lets the service begin processing before asserting in-progress state.
const jobStartDelay = 2 * time.Second

var _ = ginkgo.BeforeSuite(func() {
	logger.Infoln("[SETUP] Starting AI Services E2E setup")

	ctx = context.Background()

	ginkgo.By("Loading E2E configuration")
	cfg = &config.Config{}

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
	if providedTemplate != "" {
		templateName = providedTemplate
		logger.Infof("[SETUP] Using provided template: %s", templateName)
	} else {
		templateName = "rag"
		logger.Infof("[SETUP] Using default template: %s", templateName)
	}

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

	ginkgo.By("Loading application create params from environment")
	createParams = bootstrap.GetCreateParams()
	if createParams != "" {
		logger.Infof("[SETUP] CREATE_PARAMS set — application create will use: --params %q", createParams)
	} else {
		logger.Infof("[SETUP] CREATE_PARAMS not set — application create will use default template params")
	}

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

	ginkgo.By("Logging in to catalog API server (if already running)")
	if providedAppName != "" {
		// Existing app: catalog is already running — login is required before any CLI call.
		// Fatal so a missing CATALOG_PASSWORD surfaces immediately with a clear message.
		catalogLoginWithDiscovery(ctx, true)
	} else {
		// Fresh run: catalog may not be running yet — non-fatal, login happens again before 'application create'.
		catalogLoginWithDiscovery(ctx, false)
	}

	// Extract URLs from existing application (if provided) - must happen after catalog login.
	if providedAppName != "" {
		ginkgo.By("Extracting URLs from existing application")
		logger.Infof("[SETUP] Attempting to extract URL for app=%s, template=%s, runtime=%s", appName, templateName, appRuntime)

		extractCtx, extractCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer extractCancel()

		infoOut, infoErr := cli.ApplicationInfo(extractCtx, cfg, appName, appRuntime)
		if infoErr != nil {
			logger.Errorf("[SETUP] ERROR: Failed to get application info: %v", infoErr)
			logger.Errorf("[SETUP] This will cause tests to fail if they require the application URL")
		} else {
			logger.Infof("[SETUP] Application info retrieved successfully")
			if templateName == "rag" {
				ragBaseURL, _ = cli.GetBaseURL(infoOut, backendPort)
				judgeBaseURL = cli.GetJudgeBaseURL(judgePort)
				logger.Infof("[SETUP] Extracted RAG URL: %s", ragBaseURL)
			}
		}
	}

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
		// Non-fatal if ApplicationPS fails — catalog may not be running yet.
		psOutput, psErr := cli.ApplicationPS(ctx, cfg, "", appRuntime)
		if psErr != nil {
			logger.Warningf("[SETUP] [WARNING] --delete-app: ApplicationPS failed (non-fatal, catalog may not be running yet): %v", psErr)
		} else {
			deleteAppName := cli.GetApplicationNameFromPSOutput(psOutput)
			if deleteAppName != "" {
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
			output, err := cli.HelpCommand(ctx, cfg, []string{"help"})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateHelpCommandOutput(output)).To(gomega.Succeed())
		})
		ginkgo.It("runs -h command", ginkgo.Label("spyre-independent"), func() {
			output, err := cli.HelpCommand(ctx, cfg, []string{"-h"})
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
			ctx, cancel := withTimeout(1 * time.Minute)
			defer cancel()
			output, err := cli.ModelList(ctx, cfg, templateName, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateModelListOutput(output, templateName, appRuntime)).To(gomega.Succeed())
			logger.Infoln("[TEST] Application model list validated successfully!")
		})
	})
	ginkgo.Context("Catalog CLI Sanity Tests", func() {
		ginkgo.It("shows catalog apiserver help", ginkgo.Label("spyre-independent"), func() {
			output, err := cli.CatalogApiServerHelp(ctx, cfg, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateCatalogApiServerHelpOutput(output)).To(gomega.Succeed())
			logger.Infoln("[TEST] Catalog apiserver help validated successfully!")
		})
		ginkgo.It("generates password hash via catalog hashpw", ginkgo.Label("spyre-independent"), func() {
			if appRuntime != "podman" { //nolint:dupl
				ginkgo.Skip("catalog hashpw only supported for podman runtime")
			}
			catalogPassword := bootstrap.GetCatalogAdminPassword()
			if catalogPassword == "" {
				ginkgo.Skip("CATALOG_PASSWORD not set — skipping catalog hashpw test")
			}
			output, err := cli.CatalogHashpw(ctx, cfg, catalogPassword, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateCatalogHashpwOutput(output)).To(gomega.Succeed())
			logger.Infoln("[TEST] Catalog hashpw output validated successfully!")
		})
		ginkgo.It("shows catalog dbmigrate help", ginkgo.Label("spyre-independent"), func() {
			output, err := cli.CatalogDbMigrateHelp(ctx, cfg)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateCatalogDbMigrateHelpOutput(output)).To(gomega.Succeed())
			logger.Infoln("[TEST] Catalog dbmigrate help validated successfully!")
		})
	})
	ginkgo.Context("Bootstrap Steps", func() {
		ginkgo.It("runs bootstrap configure", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			if providedAppName != "" {
				ginkgo.Skip("Skipping bootstrap configure — using existing application")
			}
			output, err := cli.BootstrapConfigure(ctx, cfg, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateBootstrapConfigureOutput(output, appRuntime)).To(gomega.Succeed())
		})
		ginkgo.It("runs bootstrap validate", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			if providedAppName != "" {
				ginkgo.Skip("Skipping bootstrap validate — using existing application")
			}
			output, err := cli.BootstrapValidate(ctx, cfg, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateBootstrapValidateOutput(output)).To(gomega.Succeed())
		})
		ginkgo.It("runs full bootstrap", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			if providedAppName != "" {
				ginkgo.Skip("Skipping full bootstrap — using existing application")
			}
			output, err := cli.Bootstrap(ctx, cfg, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateBootstrapFullOutput(output, appRuntime)).To(gomega.Succeed())
		})
		ginkgo.It("ensures catalog service is running", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			if providedAppName != "" {
				ginkgo.Skip("Skipping catalog configure — using existing application")
			}
			if appRuntime != "podman" { //nolint:dupl
				ginkgo.Skip("catalog configure only supported for podman runtime")
			}
			ctx, cancel := withTimeout(10 * time.Minute)
			defer cancel()
			configureOutput, err := cli.CatalogConfigure(ctx, cfg, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateCatalogConfigureOutput(configureOutput)).To(gomega.Succeed())

			catalogBackendURL = cli.ExtractCatalogBackendURLFromConfigureOutput(configureOutput)
			if catalogBackendURL != "" {
				logger.Infof("[TEST] Catalog service is running. Backend URL: %s", catalogBackendURL)
			} else {
				infoOut, infoErr := cli.CatalogInfo(ctx, cfg, appRuntime)
				if infoErr == nil {
					catalogBackendURL = cli.ExtractCatalogBackendURL(infoOut)
				}
				logger.Infof("[TEST] Catalog service is running. Backend URL (from info): %s", catalogBackendURL)
			}
		})
		ginkgo.It("verifies catalog info output", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			if providedAppName != "" {
				ginkgo.Skip("Skipping catalog info — using existing application")
			}
			if appRuntime != "podman" {
				ginkgo.Skip("catalog info only supported for podman runtime")
			}
			ctx, cancel := withTimeout(2 * time.Minute)
			defer cancel()
			output, err := cli.CatalogInfo(ctx, cfg, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateCatalogInfoOutput(output)).To(gomega.Succeed())

			// Assert the catalog API server is actually reachable — not just that its
			// URL appears in the 'catalog info' text output.
			backendURL := cli.ExtractCatalogBackendURL(output)
			if backendURL != "" {
				healthURL := backendURL + "/health"
				httpClient := &http.Client{
					Timeout: 10 * time.Second,
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
					},
				}
				resp, httpErr := httpClient.Get(healthURL)
				gomega.Expect(httpErr).NotTo(gomega.HaveOccurred(), "catalog API /health request failed")
				if resp != nil {
					_ = resp.Body.Close()
					gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK), "catalog API /health returned non-200")
				}
				logger.Infof("[TEST] Catalog API server health check passed: %s", healthURL)
			}

			logger.Infoln("[TEST] Catalog info output validated successfully!")
		})
		ginkgo.It("verifies catalog login", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			if providedAppName != "" {
				ginkgo.Skip("Skipping catalog login — using existing application")
			}
			if appRuntime != "podman" {
				ginkgo.Skip("catalog login only supported for podman runtime")
			}
			_, catalogUsername, catalogPassword := bootstrap.GetCatalogCreds()
			catalogInsecure := bootstrap.GetCatalogInsecure()
			if catalogBackendURL == "" {
				ginkgo.Skip("catalogBackendURL not set — skipping catalog login test")
			}
			if catalogPassword == "" {
				ginkgo.Skip("CATALOG_PASSWORD not set — skipping catalog login test")
			}
			ctx, cancel := withTimeout(1 * time.Minute)
			defer cancel()
			output, err := cli.CatalogLogin(ctx, cfg, catalogBackendURL, catalogUsername, catalogPassword, appRuntime, catalogInsecure)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateCatalogLoginOutput(output)).To(gomega.Succeed())
			logger.Infoln("[TEST] Catalog login validated successfully!")
		})
		ginkgo.It("verifies catalog whoami after login", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			if providedAppName != "" {
				ginkgo.Skip("Skipping catalog whoami — using existing application")
			}
			if appRuntime != "podman" {
				ginkgo.Skip("catalog whoami only supported for podman runtime")
			}
			if catalogBackendURL == "" {
				ginkgo.Skip("catalogBackendURL not set — skipping catalog whoami test")
			}
			_, _, catalogPassword := bootstrap.GetCatalogCreds()
			if catalogPassword == "" {
				ginkgo.Skip("CATALOG_PASSWORD not set — skipping catalog whoami test")
			}
			ctx, cancel := withTimeout(1 * time.Minute)
			defer cancel()
			output, err := cli.CatalogWhoami(ctx, cfg, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateCatalogWhoamiOutput(output)).To(gomega.Succeed())
			logger.Infoln("[TEST] Catalog whoami output validated successfully!")
		})
		ginkgo.It("verifies catalog logout invalidates session", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			if providedAppName != "" {
				ginkgo.Skip("Skipping catalog logout — using existing application")
			}
			if appRuntime != "podman" {
				ginkgo.Skip("catalog logout only supported for podman runtime")
			}
			_, catalogUsername, catalogPassword := bootstrap.GetCatalogCreds()
			catalogInsecure := bootstrap.GetCatalogInsecure()
			if catalogBackendURL == "" {
				ginkgo.Skip("catalogBackendURL not set — skipping catalog logout test")
			}
			if catalogPassword == "" {
				ginkgo.Skip("CATALOG_PASSWORD not set — skipping catalog logout test")
			}

			ctx, cancel := withTimeout(2 * time.Minute)
			defer cancel()

			_, err := cli.CatalogLogin(ctx, cfg, catalogBackendURL, catalogUsername, catalogPassword, appRuntime, catalogInsecure)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			logoutOutput, err := cli.CatalogLogout(ctx, cfg, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateCatalogLogoutOutput(logoutOutput)).To(gomega.Succeed())

			_, whoamiErr := cli.CatalogWhoami(ctx, cfg, appRuntime)
			gomega.Expect(whoamiErr).To(gomega.HaveOccurred(), "whoami should fail after logout but succeeded")
			logger.Infoln("[TEST] Catalog logout invalidated session — whoami correctly rejected")

			// Re-login so downstream specs retain a valid session.
			_, err = cli.CatalogLogin(ctx, cfg, catalogBackendURL, catalogUsername, catalogPassword, appRuntime, catalogInsecure)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			logger.Infoln("[TEST] Catalog logout / session-invalidation validated successfully!")
		})
	})
	ginkgo.Context("Application Image Command Tests", func() {
		ginkgo.It("lists images for rag template", ginkgo.Label("spyre-independent", "summarization-tests"), func() {
			if providedAppName != "" {
				ginkgo.Skip("Skipping image list — using existing application")
			}
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()
			gomega.Expect(cli.ListImage(ctx, cfg, templateName, appRuntime)).To(gomega.Succeed())
			logger.Infof("[TEST] Images listed successfully for %s template", templateName)
		})
		ginkgo.It("pulls images for rag template", ginkgo.Label("spyre-independent", "summarization-tests"), func() {
			if providedAppName != "" {
				ginkgo.Skip("Skipping image pull — using existing application")
			}
			ctx, cancel := withTimeout(10 * time.Minute)
			defer cancel()
			gomega.Expect(cli.PullImage(ctx, cfg, templateName, appRuntime)).To(gomega.Succeed())
			logger.Infof("[TEST] Images pulled successfully for %s template", templateName)
		})
		ginkgo.It("verifies application model download command", ginkgo.Label("spyre-independent", "summarization-tests"), func() {
			if providedAppName != "" {
				ginkgo.Skip("Skipping model download — using existing application")
			}
			ctx, cancel := withTimeout(30 * time.Minute)
			defer cancel()
			output, err := cli.ModelDownload(ctx, cfg, templateName, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateModelDownloadOutput(output, templateName, appRuntime)).To(gomega.Succeed())
			logger.Infoln("[TEST] Application model download validated successfully!")
		})
	})
	ginkgo.Context("Application Creation", func() {
		ginkgo.It("creates application with specified template and validates endpoints", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			if providedAppName != "" {
				// Extract URLs from existing application
				ctx, cancel := withTimeout(5 * time.Minute)
				defer cancel()

				infoOut, infoErr := cli.ApplicationInfo(ctx, cfg, appName, appRuntime)
				gomega.Expect(infoErr).NotTo(gomega.HaveOccurred())

				if templateName == "rag" {
					var ragErr error
					ragBaseURL, ragErr = cli.GetBaseURL(infoOut, backendPort)
					gomega.Expect(ragErr).NotTo(gomega.HaveOccurred())
					judgeBaseURL = cli.GetJudgeBaseURL(judgePort)
					logger.Infof("[TEST] Using existing RAG application: %s (URL: %s)", appName, ragBaseURL)
				} else if templateName == "summarize" {
					summarizeBaseURL = cli.ExtractCatalogSummarizeURL(infoOut)
					gomega.Expect(summarizeBaseURL).NotTo(gomega.BeEmpty(), "Failed to extract summarize URL from existing application")
					logger.Infof("[TEST] Using existing summarization application: %s (URL: %s)", appName, summarizeBaseURL)
				} else if templateName == "digitize" {
					logger.Infof("[TEST] Using existing digitize application: %s", appName)
				}

				ginkgo.Skip("Skipping creation — using existing application")
			}

			ctx, cancel := withTimeout(45 * time.Minute)
			defer cancel()

			// Refresh the catalog token before create — the 15-min TTL may have elapsed.
			catalogLoginWithDiscovery(ctx, true)

			cliOptions := cli.CreateOptions{
				SkipModelDownload: false,
				ImagePullPolicy:   "IfNotPresent",
			}
			// Handle different templates
			if templateName == "rag" {
				pods := []string{"backend", "ui", "db"}
				createOutput, err := cli.CreateRAGAppAndValidate(
					ctx,
					cfg,
					appName,
					templateName,
					createParams,
					backendPort,
					uiPort,
					cliOptions,
					pods,
					appRuntime,
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				// Extract RAG URLs
				if appRuntime == "podman" {
					infoOut, infoErr := cli.ApplicationInfo(ctx, cfg, appName, appRuntime)
					gomega.Expect(infoErr).NotTo(gomega.HaveOccurred())
					ragBaseURL, err = cli.GetBaseURL(infoOut, backendPort)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					judgeBaseURL = cli.GetJudgeBaseURL(judgePort)
				} else {
					ragBaseURL, err = cli.GetBaseURL(createOutput, backendPort)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					judgeBaseURL, err = cli.GetBaseURL(createOutput, judgePort)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
				}
				logger.Infof("[TEST] RAG application %s created and validated", appName)
			} else if templateName == "summarize" || templateName == "digitize" {
				// Deploy standalone service (summarize or digitize).
				// URL resolution is deferred to each context's BeforeAll via application info.
				_, err := cli.CreateApp(
					ctx,
					cfg,
					appName,
					templateName,
					createParams,
					cliOptions,
					appRuntime,
				)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				logger.Infof("[TEST] %s service %s created successfully", templateName, appName)
			} else {
				ginkgo.Fail(fmt.Sprintf("Unsupported template: %s. Supported templates: rag, summarize, digitize", templateName))
			}

			logger.Infof("[TEST] Application %s created, healthy, and endpoints validated", appName)
		})
	})
	ginkgo.Context("Application Observability", func() {
		ginkgo.BeforeEach(func() {
			if providedAppName != "" {
				ginkgo.Skip("Skipping observability specs — using existing application")
			}
		})
		ginkgo.It("verifies application ps output", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			// Run normal ps first.
			normalOut, err := cli.ApplicationPS(ctx, cfg, appName, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateApplicationPS(normalOut)).To(gomega.Succeed())

			// Run wide ps and cache it for the pods-existence and logs specs below.
			appPsWideOutput, err = cli.ApplicationPS(ctx, cfg, appName, appRuntime, "-o", "wide")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(cli.ValidateApplicationPS(appPsWideOutput)).To(gomega.Succeed())
		})
		ginkgo.It("verifies application info output", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			infoOutput, err := cli.ApplicationInfo(ctx, cfg, appName, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Expect(cli.ValidateApplicationInfo(infoOutput, appName, templateName)).To(gomega.Succeed())
			logger.Infof("[TEST] Application info output validated successfully!")
		})
		ginkgo.It("Verifies pods existence, health status  and restart count", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			if !podmanReady {
				ginkgo.Skip("Podman not available - will be installed via bootstrap configure")
			}
			err := podman.VerifyContainers(ctx, cfg, appPsWideOutput, appName, appRuntime, templateName)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "verify containers failed")
			logger.Infof("[TEST] Containers verified")
		})
		ginkgo.It("Verifies Exposed Ports/Routes of the application", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			if !podmanReady {
				ginkgo.Skip("Podman not available - will be installed via bootstrap configure")
			}
			if appRuntime == "openshift" {
				output, err := podman.GetOpenshiftRoutes(appName)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cli.ValidateOpenShiftRoutes(output)).NotTo(gomega.HaveOccurred(), "Verify exposed ports/routes failed")
			} else {
				// Podman: Caddy routes by domain — no numbered ports to verify.
				logger.Infof("[TEST] Podman catalog path: skipping numeric port check (Caddy routes by domain)")
			}
			logger.Infof("[TEST] Exposed ports/routes verified")
		})
		ginkgo.It("verifies application logs output", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			// fetchAndValidateLogs calls ApplicationLogs with a 30 s per-call timeout
			// and validates the output, failing the spec immediately on any error.
			fetchAndValidateLogs := func(podRef, container string) {
				logCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				logs, err := cli.ApplicationLogs(logCtx, cfg, appName, podRef, container, appRuntime)
				cancel()
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(logs).NotTo(gomega.BeEmpty())
				gomega.Expect(cli.ValidateApplicationLogs(logs, podRef, container)).To(gomega.Succeed())
			}

			pods, err := podman.ExtractPodInfo(appPsWideOutput)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(pods).NotTo(gomega.BeEmpty(), "No pods found for application %s", appName)

			for podName, pod := range pods {
				fetchAndValidateLogs(podName, "")

				if appRuntime == "podman" {
					fetchAndValidateLogs(pod.PodID, "")
				}

				for _, container := range pod.Containers {
					fetchAndValidateLogs(pod.PodID, container)
				}
			}
		})
	})
	ginkgo.Context("Runtime Operations", func() {
		ginkgo.It("stops the application", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			if templateName == "summarize" {
				ginkgo.Skip("Skipping stop/start for summarize template — LLM reload would delay summarization tests")
			}
			ctx, cancel := withTimeout(10 * time.Minute)
			defer cancel()

			var pods []string

			if appRuntime == "podman" {
				// Discover pod names via -o wide; narrow format omits required columns.
				psOutput, psErr := cli.ApplicationPS(ctx, cfg, appName, appRuntime, "-o", "wide")
				gomega.Expect(psErr).NotTo(gomega.HaveOccurred())

				podInfoMap, parseErr := podman.ExtractPodInfo(psOutput)
				gomega.Expect(parseErr).NotTo(gomega.HaveOccurred())
				gomega.Expect(podInfoMap).NotTo(gomega.BeEmpty(), "no pods found for app %s", appName)

				for podName := range podInfoMap {
					pods = append(pods, podName)
				}
			} else {
				suffixes, ok := common.ExpectedPodSuffixes[appRuntime]
				gomega.Expect(ok).To(gomega.BeTrue(), "unknown appRuntime %s", appRuntime)

				for _, s := range suffixes {
					pods = append(pods, fmt.Sprintf("%s--%s", appName, s))
				}
			}

			output, err := cli.StopAppWithPods(ctx, cfg, appName, pods, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(output).NotTo(gomega.BeEmpty())

			logger.Infof("[TEST] Application %s stopped successfully using --pod", appName)
		})
		ginkgo.It("starts application pods", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			if templateName == "summarize" {
				ginkgo.Skip("Skipping stop/start for summarize template — LLM reload would delay summarization tests")
			}
			ctx, cancel := withTimeout(10 * time.Minute)
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
	ginkgo.Context("RAG Golden Dataset Validation", ginkgo.Label("golden-dataset-validation"), func() {
		ginkgo.BeforeAll(ginkgo.NodeTimeout(10*time.Hour), func(ctx context.Context) {
			if appRuntime == "openshift" {
				ginkgo.Skip("Skipping RAG Golden Dataset Validation for OpenShift runtime")
			}
			if appName == "" {
				ginkgo.Fail("Application name is not set")
			}

			// Skip if LLM-as-Judge env vars are not set — judge is optional.
			llmJudgeImage := os.Getenv("LLM_JUDGE_IMAGE")
			llmJudgeModelPath := os.Getenv("LLM_JUDGE_MODEL_PATH")
			llmJudgeModel := os.Getenv("LLM_JUDGE_MODEL")
			if llmJudgeImage == "" || llmJudgeModelPath == "" || llmJudgeModel == "" {
				ginkgo.Skip(fmt.Sprintf(
					"Skipping RAG Golden Dataset Validation — LLM-as-Judge not configured "+
						"(LLM_JUDGE_IMAGE=%q, LLM_JUDGE_MODEL_PATH=%q, LLM_JUDGE_MODEL=%q). "+
						"Set all three env vars to enable this context.",
					llmJudgeImage, llmJudgeModelPath, llmJudgeModel,
				))
			}

			logger.Infof("[RAG] Setting golden dataset path")
			goldenDatasetFile = bootstrap.GetGoldenDatasetFile()
			if goldenDatasetFile == "" {
				ginkgo.Skip("Skipping RAG Golden Dataset Validation — GOLDEN_DATASET_FILE environment variable is not set")
			}

			_, filename, _, ok := runtime.Caller(0)
			if !ok {
				ginkgo.Fail("runtime.Caller failed — cannot determine test file path")
			}
			e2eDir := filepath.Dir(filename)                              // resolves ai-services/tests/e2e
			repoRoot := filepath.Clean(filepath.Join(e2eDir, "../../..")) // navigates to the workspace root

			goldenPath = filepath.Join(
				repoRoot,
				"test",
				"golden",
				goldenDatasetFile,
			)
			logger.Infof("[RAG] Golden dataset file: %s", goldenPath)

			infoCtx, infoCancel := context.WithTimeout(ctx, 10*time.Minute)
			defer infoCancel()
			infoOutput, err := cli.WaitForApplicationInfoURLs(infoCtx, cfg, appName, appRuntime, 8*time.Minute, 15*time.Second)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			if err := cli.ValidateApplicationInfo(infoOutput, appName, templateName); err != nil {
				ginkgo.Fail(fmt.Sprintf("Golden dataset validation requires a valid running application: %v", err))
			}

			ragBaseURL, err = cli.GetBaseURL(infoOutput, backendPort)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			judgeBaseURL = cli.GetJudgeBaseURL(judgePort)

			logger.Infof("[RAG] RAG Base URL: %s", ragBaseURL)
			logger.Infof("[RAG] Judge Base URL: %s", judgeBaseURL)

			similarityBaseURL := cli.ExtractSimilarityAPIURL(infoOutput)
			if similarityBaseURL == "" {
				ginkgo.Fail("[RAG] similarity-api URL not found — cannot run golden dataset validation")
			}
			logger.Infof("[RAG] Waiting for similarity-api to be healthy at %s/health", similarityBaseURL)
			similarityCtx, similarityCancel := context.WithTimeout(ctx, 5*time.Minute)
			defer similarityCancel()
			if err := rag.WaitForSimilarityAPIReady(similarityCtx, similarityBaseURL, 15*time.Second); err != nil {
				ginkgo.Fail(fmt.Sprintf("[RAG] similarity-api is not healthy — cannot run golden dataset validation: %v", err))
			}

			// Phase 1: download judge model — safe before LLM is ready (no GPU contention).
			logger.Infof("[RAG] Phase 1 — downloading LLM-as-Judge model")
			if err := rag.DownloadJudgeModel(ctx, cfg); err != nil {
				ginkgo.Skip(fmt.Sprintf("[RAG] judge model download failed — skipping golden dataset validation: %v", err))
			}
			logger.Infof("[RAG] Judge model download completed")

			// Phase 2: wait for main LLM — judge container must not start until LLM is ready.
			logger.Infof("[RAG] Phase 2 — waiting for LLM to be ready via %s/v1/models", ragBaseURL)
			llmCtx, llmCancel := context.WithTimeout(ctx, 40*time.Minute)
			defer llmCancel()
			if err := rag.WaitForRAGBackendReady(llmCtx, ragBaseURL, 30*time.Second); err != nil {
				ginkgo.Fail(fmt.Sprintf("[RAG] LLM is not ready — cannot run golden dataset validation: %v", err))
			}

			freshInfoCtx, freshInfoCancel := context.WithTimeout(ctx, 2*time.Minute)
			defer freshInfoCancel()
			freshInfoOutput, freshInfoErr := cli.ApplicationInfo(freshInfoCtx, cfg, appName, appRuntime)
			if freshInfoErr != nil {
				ginkgo.Fail(fmt.Sprintf("[RAG] failed to fetch application info for digitize URL: %v", freshInfoErr))
			}
			digitizeBaseURL := cli.ExtractDigitizeURL(freshInfoOutput)
			if digitizeBaseURL == "" {
				ginkgo.Fail("[RAG] could not extract digitize-backend URL — cannot ingest documents")
			}
			logger.Infof("[RAG] Ingesting test document via digitize microservice at %s", digitizeBaseURL)
			// Clear any stale documents from a previous failed run before ingesting.
			cleanCtx, cleanCancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cleanCancel()
			if err := digitization.DeleteAllDocuments(cleanCtx, digitizeBaseURL); err != nil {
				logger.Warningf("[RAG] pre-ingest document cleanup failed (non-fatal): %v", err)
			}
			ingestCtx, ingestCancel := context.WithTimeout(ctx, 25*time.Minute)
			defer ingestCancel()
			if err := digitization.IngestTestDocumentViaDigitizeAPI(ingestCtx, digitizeBaseURL, "rag-golden-ingest-"+runID); err != nil {
				ginkgo.Fail(fmt.Sprintf("[RAG] document ingestion failed — cannot run golden dataset validation: %v", err))
			}
			logger.Infof("[RAG] Document ingestion completed successfully")

			// Phase 3: start judge container — LLM is ready and weights are on disk.
			logger.Infof("[RAG] Phase 3 — starting LLM-as-Judge container")
			judgeCtx, judgeCancel := context.WithTimeout(ctx, 30*time.Minute)
			defer judgeCancel()
			if err := rag.StartJudgeContainer(judgeCtx, cfg, runID); err != nil {
				ginkgo.Fail(fmt.Sprintf("[RAG] failed to start LLM-as-Judge container: %v", err))
			}
			logger.Infof("[RAG] LLM-as-Judge container is ready")
		})

		ginkgo.AfterAll(func() {
			if appRuntime == "openshift" {
				ginkgo.Skip("Skipping Judge cleanup for OpenShift runtime")
			}
			if err := rag.CleanupLLMAsJudge(runID); err != nil {
				logger.Warningf("[RAG][WARN] Judge cleanup failed: %v", err)
			}
			// Delete documents ingested by BeforeAll so the Digitization Tests
			// context starts with a clean slate. Without this, CreateJob returns
			// 409 RESOURCE_LOCKED for test_doc.pdf which was already processed
			// during golden dataset ingestion, causing the entire ordered
			// Digitization context to abort.
			cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cleanCancel()
			infoOut, infoErr := cli.ApplicationInfo(cleanCtx, cfg, appName, appRuntime)
			if infoErr == nil {
				if digitizeURL := cli.ExtractDigitizeURL(infoOut); digitizeURL != "" {
					if err := digitization.DeleteAllDocuments(cleanCtx, digitizeURL); err != nil {
						logger.Warningf("[RAG][WARN] post-golden document cleanup failed (non-fatal): %v", err)
					} else {
						logger.Infof("[RAG] post-golden document cleanup completed")
					}
				}
			}
		})

		ginkgo.It("validates RAG answers against golden dataset",
			ginkgo.Label("spyre-dependent"),
			ginkgo.SpecTimeout(10*time.Hour),
			func(specCtx context.Context) {
				logger.Infof("[RAG] Starting golden dataset validation")
				cases, err := rag.LoadGoldenCSV(goldenPath)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(cases).NotTo(gomega.BeEmpty())

				total := len(cases)
				results := make([]rag.EvalResult, 0, total)
				passed := 0

				if specDeadline, ok := specCtx.Deadline(); ok {
					logger.Infof("[RAG] Spec budget remaining: %s (deadline: %s)",
						time.Until(specDeadline).Round(time.Second), specDeadline.Format(time.RFC3339))
				}

				// Per-question contexts are rooted at Background, not specCtx.
				// This prevents one cancellation from cascading to all remaining questions.
				const perQuestionTimeout = 20 * time.Minute

				for i, tc := range cases {
					// Stop if the spec-level timeout has fired.
					if specCtx.Err() != nil {
						logger.Warningf("[RAG] specCtx cancelled (%v) after %d/%d questions — stopping evaluation loop",
							specCtx.Err(), i, total)
						break
					}

					qCtx, qCancel := context.WithTimeout(context.Background(), perQuestionTimeout)

					result := rag.EvalResult{
						Question: tc.Question,
						Passed:   false,
					}

					logger.Infof("[RAG] Evaluating question %d/%d: %s", i+1, total, tc.Question)

					ragAns, ragErr := rag.RunWithRetry(qCtx, defaultMaxRetries, func(c context.Context) (string, error) {
						return rag.AskRAG(c, ragBaseURL, tc.Question)
					})

					if ragErr != nil {
						result.Details = fmt.Sprintf("RAG request failed: %v", ragErr)
						logger.Infof("[RAG] Question %d/%d — RAG failed: %v", i+1, total, ragErr)
						results = append(results, result)
						qCancel()

						continue
					}

					verdict, reason, judgeErr := rag.AskJudgeWithFormatRetry(
						qCtx,
						defaultMaxRetries,
						judgeBaseURL,
						tc.Question,
						ragAns,
						tc.GoldenAnswer,
					)
					if judgeErr != nil {
						result.Details = fmt.Sprintf("Judge failed: %v", judgeErr)
						logger.Infof("[RAG] Question %d/%d — Judge failed: %v", i+1, total, judgeErr)
						results = append(results, result)
						qCancel()

						continue
					}

					result.Passed = verdict == "YES"
					result.Details = reason

					if result.Passed {
						passed++
					}

					results = append(results, result)
					logger.Infof("[RAG] Evaluated question %d/%d | verdict=%s | reason=%s", i+1, total, verdict, reason)
					qCancel()
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
	ginkgo.Context("Digitization Tests", ginkgo.Label("spyre-dependent", "digitization-tests"), func() {
		var digitizeBaseURL string
		var pdfPath string
		var createdJobIDs []string
		var createdDocIDs []string

		ginkgo.BeforeAll(func() {
			if appName == "" {
				ginkgo.Fail("Application name is not set")
			}

			logger.Infof("[DIGITIZE] Setting up digitization tests")

			// Resolve the test PDF path once for all specs in this context.
			pdfPath = digitization.GetTestPDFPath()
			gomega.Expect(pdfPath).NotTo(gomega.BeEmpty(), "test PDF path could not be resolved")

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			// digitize-backend may still be starting after chat-bot-backend and
			// similarity-api are healthy — poll separately for its URL below.
			infoOutput, err := cli.WaitForApplicationInfoURLs(ctx, cfg, appName, appRuntime, 8*time.Minute, 15*time.Second)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			if err := cli.ValidateApplicationInfo(infoOutput, appName, templateName); err != nil {
				ginkgo.Fail(fmt.Sprintf("Digitization tests require a valid running application: %v", err))
			}

			digitizeBaseURL = cli.ExtractDigitizeURL(infoOutput)
			if digitizeBaseURL == "" {
				ginkgo.Fail("No digitize-backend URL found in application info output")
			}

			logger.Infof("[DIGITIZE] Digitize Base URL: %s", digitizeBaseURL)
		})

		ginkgo.AfterEach(func() {
			for _, jobID := range createdJobIDs {
				cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 5*time.Minute)
				_, _ = digitization.WaitForJobCompletion(cleanCtx, digitizeBaseURL, jobID, 5*time.Minute)
				_ = digitization.DeleteJob(cleanCtx, digitizeBaseURL, jobID)
				cleanCancel()
			}
			for _, docID := range createdDocIDs {
				cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
				_ = digitization.DeleteDocument(cleanCtx, digitizeBaseURL, docID)
				cleanCancel()
			}
			createdJobIDs = nil
			createdDocIDs = nil
			// Wipe all remaining documents so the next spec starts with a clean
			// slate. Some specs (e.g. ingestion workflow) process test_doc.pdf
			// but never track the resulting document ID in createdDocIDs, which
			// causes the following spec to receive 409 RESOURCE_LOCKED when it
			// tries to re-process the same file.
			if digitizeBaseURL != "" {
				cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cleanCancel()
				if err := digitization.DeleteAllDocuments(cleanCtx, digitizeBaseURL); err != nil {
					logger.Warningf("[DIGITIZE] AfterEach: DeleteAllDocuments failed (non-fatal): %v", err)
				}
			}
		})

		ginkgo.It("should pass health check", func() {
			ctx, cancel := withTimeout(3 * time.Minute)
			defer cancel()

			gomega.Expect(digitization.HealthCheck(ctx, digitizeBaseURL)).To(gomega.Succeed())
			logger.Infof("[TEST] Digitization service health check passed")
		})

		ginkgo.It("should complete full digitization workflow with job and document operations", func() {
			ctx, cancel := withTimeout(12 * time.Minute)
			defer cancel()

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
			ctx, cancel := withTimeout(20 * time.Minute)
			defer cancel()

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
			ctx, cancel := withTimeout(30 * time.Minute)
			defer cancel()

			formats := []string{"json", "md", "txt"}

			for _, format := range formats {
				jobResp, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "digitization", format, fmt.Sprintf("e2e-format-%s", format))
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				createdJobIDs = append(createdJobIDs, jobResp.JobID)

				finalStatus, err := digitization.WaitForJobCompletion(ctx, digitizeBaseURL, jobResp.JobID, 8*time.Minute)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(finalStatus.Status).To(gomega.Equal("completed"))

				// Delete the document produced by this iteration before the next
				// format is attempted. The server rejects re-processing the same
				// file with 409 RESOURCE_LOCKED when a document for it already
				// exists, so each iteration must clean up after itself.
				deleteDigitizeDocsFromJob(digitizeBaseURL, finalStatus.Documents)

				logger.Infof("[TEST] %s format job completed", format)
			}
		})

		ginkgo.It("should handle job lifecycle including active job protection and deletion", func() {
			ctx, cancel := withTimeout(12 * time.Minute)
			defer cancel()

			// Step 1: Create job
			logger.Infof("[TEST] Step 1: Creating job for lifecycle test")
			jobResp, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "digitization", "json", "e2e-job-lifecycle")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			createdJobIDs = append(createdJobIDs, jobResp.JobID)
			logger.Infof("[TEST] Created job: %s", jobResp.JobID)

			// Step 2: Try to delete active job (should fail with 409)
			logger.Infof("[TEST] Step 2: Testing active job deletion protection")
			time.Sleep(jobStartDelay) // Wait for job to start processing.
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

			createdJobIDs = createdJobIDs[:len(createdJobIDs)-1]

			logger.Infof("[TEST] ✓ Job lifecycle test completed successfully")
		})

		ginkgo.It("should handle document lifecycle including protection and deletion", func() {
			ctx, cancel := withTimeout(12 * time.Minute)
			defer cancel()

			// Step 1: Create job
			logger.Infof("[TEST] Step 1: Creating job for document lifecycle test")
			jobResp, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "digitization", "json", "e2e-doc-lifecycle")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			createdJobIDs = append(createdJobIDs, jobResp.JobID)
			logger.Infof("[TEST] Created job: %s", jobResp.JobID)

			// Step 2: Try to delete in-progress document (should fail with 409)
			logger.Infof("[TEST] Step 2: Testing in-progress document deletion protection")
			time.Sleep(jobStartDelay) // Wait for job to start and document to be created.

			jobStatus, err := digitization.GetJobStatus(ctx, digitizeBaseURL, jobResp.JobID)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(jobStatus.Documents).NotTo(gomega.BeEmpty())
			docID := jobStatus.Documents[0].ID

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
			ctx, cancel := withTimeout(20 * time.Minute)
			defer cancel()

			var ownDocIDs []string
			for i := 0; i < 2; i++ {
				jobResp, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "digitization", "json", fmt.Sprintf("e2e-delete-all-%d", i))
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				createdJobIDs = append(createdJobIDs, jobResp.JobID)

				finalStatus, err := digitization.WaitForJobCompletion(ctx, digitizeBaseURL, jobResp.JobID, 8*time.Minute)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				if finalStatus != nil {
					for _, doc := range finalStatus.Documents {
						ownDocIDs = append(ownDocIDs, doc.ID)
					}
				}

				// The server rejects re-processing the same file (test_doc.pdf) with
				// 409 RESOURCE_LOCKED when a document already exists. Delete docs
				// produced by this iteration before the next iteration calls CreateJob.
				// We keep their IDs in ownDocIDs so the verification below (which
				// checks found==false) still holds — already-deleted docs are absent
				// from the list, satisfying the same invariant as DeleteAllDocuments.
				if i < 1 {
					deleteDigitizeDocsFromJob(digitizeBaseURL, finalStatus.Documents)
				}
			}

			// Delete any remaining documents (from the last iteration) and verify
			// all docs this spec created are gone.
			err := digitization.DeleteAllDocuments(ctx, digitizeBaseURL)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Verify each doc created by this spec is gone — not a global empty check.
			for _, docID := range ownDocIDs {
				docsList, listErr := digitization.ListDocuments(ctx, digitizeBaseURL, 100, 0, "", "")
				gomega.Expect(listErr).NotTo(gomega.HaveOccurred())
				found := false
				for _, d := range docsList.Data {
					if d.ID == docID {
						found = true
						break
					}
				}
				gomega.Expect(found).To(gomega.BeFalse(),
					"document %s should have been deleted by DeleteAllDocuments", docID)
			}

			logger.Infof("[TEST] All %d documents created by this spec were deleted successfully", len(ownDocIDs))
			createdDocIDs = nil
		})

		ginkgo.It("should reject multiple files for digitization operation", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			filePaths := []string{pdfPath, pdfPath}
			errorResp, err := digitization.CreateJobWithMultipleFiles(ctx, digitizeBaseURL, filePaths, "digitization", "json", "e2e-multiple-files-test")
			expectErrResp(err, errorResp)

			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("INVALID_REQUEST"))
			gomega.Expect(errorResp.Error.Message).To(gomega.Equal("Request validation failed: Only 1 file allowed for digitization."))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(400))

			logger.Infof("[TEST] Multiple files correctly rejected for digitization with error: %s", errorResp.Error.Message)
		})

		ginkgo.It("should reject third concurrent digitization job with rate limit error", func() {
			ctx, cancel := withTimeout(15 * time.Minute)
			defer cancel()

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
			expectErrResp(err, errorResp)

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
			ctx, cancel := withTimeout(20 * time.Minute)
			defer cancel()

			// Start the first ingestion job
			job1Resp, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "ingestion", "json", "e2e-concurrent-ingestion-1")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(job1Resp).NotTo(gomega.BeNil())
			gomega.Expect(job1Resp.JobID).NotTo(gomega.BeEmpty())
			createdJobIDs = append(createdJobIDs, job1Resp.JobID)

			// Wait a moment to ensure the first job starts processing.
			time.Sleep(jobStartDelay)

			// Try to start a second ingestion job while the first is still running
			// This should fail with a 429 rate limit error
			errorResp, err := digitization.CreateJobExpectingError(ctx, digitizeBaseURL, pdfPath, "ingestion", "json", "e2e-concurrent-ingestion-2")
			expectErrResp(err, errorResp)

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
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			invalidPDFPath := testFilePath(filepath.Join("ingestion", "docs", "sample_png.pdf"))
			logger.Infof("[TEST] Testing digitization with invalid PDF file: %s", invalidPDFPath)

			errorResp, err := digitization.CreateJobExpectingError(ctx, digitizeBaseURL, invalidPDFPath, "digitization", "json", "e2e-invalid-pdf-digitization")
			expectErrResp(err, errorResp)

			// Validate the error response structure.
			// Use ContainSubstring for the message so minor server-side wording
			// changes ("unsupported format" vs "invalid format") don't break the
			// test — we care that the right code, status, and filename are present.
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("UNSUPPORTED_MEDIA_TYPE"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring(".pdf extension"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("sample_png.pdf"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(415))

			logger.Infof("[TEST] Invalid PDF correctly rejected for digitization with error: %s", errorResp.Error.Message)
		})

		ginkgo.It("should reject invalid PDF file for ingestion operation", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			invalidPDFPath := testFilePath(filepath.Join("ingestion", "docs", "sample_png.pdf"))
			logger.Infof("[TEST] Testing ingestion with invalid PDF file: %s", invalidPDFPath)

			errorResp, err := digitization.CreateJobExpectingError(ctx, digitizeBaseURL, invalidPDFPath, "ingestion", "json", "e2e-invalid-pdf-ingestion")
			expectErrResp(err, errorResp)

			// Validate the error response structure.
			// Use ContainSubstring for the message — wording may differ across
			// backend versions; the code, status, and filename are the stable signals.
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("UNSUPPORTED_MEDIA_TYPE"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring(".pdf extension"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("sample_png.pdf"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(415))

			logger.Infof("[TEST] Invalid PDF correctly rejected for ingestion with error: %s", errorResp.Error.Message)
		})

		ginkgo.It("should reject non-PDF file for digitization operation", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			nonPDFPath := testFilePath(filepath.Join("ingestion", "docs", "sample_txt.txt"))
			logger.Infof("[TEST] Testing digitization with non-PDF file: %s", nonPDFPath)

			errorResp, err := digitization.CreateJobExpectingError(ctx, digitizeBaseURL, nonPDFPath, "digitization", "json", "e2e-non-pdf-digitization")
			expectErrResp(err, errorResp)

			// Validate the error response structure.
			// ContainSubstring on the filename so phrasing changes don't break this.
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("UNSUPPORTED_MEDIA_TYPE"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("sample_txt.txt"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(415))

			logger.Infof("[TEST] Non-PDF file correctly rejected for digitization with error: %s", errorResp.Error.Message)
		})

		ginkgo.It("should reject non-PDF file for ingestion operation", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			nonPDFPath := testFilePath(filepath.Join("ingestion", "docs", "sample_txt.txt"))
			logger.Infof("[TEST] Testing ingestion with non-PDF file: %s", nonPDFPath)

			errorResp, err := digitization.CreateJobExpectingError(ctx, digitizeBaseURL, nonPDFPath, "ingestion", "json", "e2e-non-pdf-ingestion")
			expectErrResp(err, errorResp)

			// Validate the error response structure.
			// ContainSubstring on the filename so phrasing changes don't break this.
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("UNSUPPORTED_MEDIA_TYPE"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("sample_txt.txt"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(415))

			logger.Infof("[TEST] Non-PDF file correctly rejected for ingestion with error: %s", errorResp.Error.Message)
		})

		ginkgo.It("should return 404 error when getting job with invalid ID", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			logger.Infof("[TEST] Testing GetJobStatus with invalid ID: %s", invalidJobID)

			errorResp, err := digitization.GetJobStatusExpectingError(ctx, digitizeBaseURL, invalidJobID)
			expectErrResp(err, errorResp)

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("RESOURCE_NOT_FOUND"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("No job found with id"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("not found"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(404))

			logger.Infof("[TEST] ✓ GetJobStatus correctly returned 404 for invalid ID: %s", errorResp.Error.Message)
		})

		ginkgo.It("should return 404 error when getting document with invalid ID", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			logger.Infof("[TEST] Testing GetDocument with invalid ID: %s", invalidDocID)

			errorResp, err := digitization.GetDocumentExpectingError(ctx, digitizeBaseURL, invalidDocID)
			expectErrResp(err, errorResp)

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("RESOURCE_NOT_FOUND"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("Document with ID"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("not found"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(404))

			logger.Infof("[TEST] ✓ GetDocument correctly returned 404 for invalid ID: %s", errorResp.Error.Message)
		})

		ginkgo.It("should return 404 error when getting document content with invalid ID", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			logger.Infof("[TEST] Testing GetDocumentContent with invalid ID: %s", invalidDocID)

			errorResp, err := digitization.GetDocumentContentExpectingError(ctx, digitizeBaseURL, invalidDocID)
			expectErrResp(err, errorResp)

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("RESOURCE_NOT_FOUND"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("Document with ID"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("not found"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(404))

			logger.Infof("[TEST] ✓ GetDocumentContent correctly returned 404 for invalid ID: %s", errorResp.Error.Message)
		})

		ginkgo.It("should return 404 error when deleting job with invalid ID", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			logger.Infof("[TEST] Testing DeleteJob with invalid ID: %s", invalidJobID)

			errorResp, err := digitization.DeleteJobExpectingError(ctx, digitizeBaseURL, invalidJobID)
			expectErrResp(err, errorResp)

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("RESOURCE_NOT_FOUND"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("No job found with id"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("not found"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(404))

			logger.Infof("[TEST] ✓ DeleteJob correctly returned 404 for invalid ID: %s", errorResp.Error.Message)
		})

		// The digitize backend treats DELETE /v1/documents/:id as idempotent:
		// when the document does not exist it cleans up the vectorstore (0 chunks
		// removed), logs a warning, and returns HTTP 204 rather than 404.
		// This is intentional server behaviour — the endpoint is "delete if exists".
		// The test expectation (404) does not match reality, so it stays pending.
		ginkgo.XIt("should return 404 error when deleting document with invalid ID", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			logger.Infof("[TEST] Testing DeleteDocument with invalid ID: %s", invalidDocID)

			errorResp, err := digitization.DeleteDocumentExpectingError(ctx, digitizeBaseURL, invalidDocID)
			expectErrResp(err, errorResp)

			// Validate the error response structure
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal("RESOURCE_NOT_FOUND"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("Document with ID"))
			gomega.Expect(errorResp.Error.Message).To(gomega.ContainSubstring("not found"))
			gomega.Expect(errorResp.Error.Status).To(gomega.Equal(404))

			logger.Infof("[TEST] ✓ DeleteDocument correctly returned 404 for invalid ID: %s", errorResp.Error.Message)
		})

		ginkgo.It("should successfully process blank PDF file for digitization operation", func() {
			ctx, cancel := withTimeout(12 * time.Minute)
			defer cancel()

			blankPDFPath := testFilePath(filepath.Join("ingestion", "docs", "blank.pdf"))

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
			ctx, cancel := withTimeout(20 * time.Minute)
			defer cancel()

			blankPDFPath := testFilePath(filepath.Join("ingestion", "docs", "blank.pdf"))

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
		// ── Export API ──────────────────────────────────────────────────────────

		ginkgo.It("should export all jobs and documents via /v1/export", func() {
			ctx, cancel := withTimeout(12 * time.Minute)
			defer cancel()

			// Seed one completed job so the export payload is non-empty.
			jobResp, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "digitization", "json", "e2e-export-seed")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			createdJobIDs = append(createdJobIDs, jobResp.JobID)

			finalStatus, err := digitization.WaitForJobCompletion(ctx, digitizeBaseURL, jobResp.JobID, 10*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(finalStatus.Status).To(gomega.Equal("completed"))
			gomega.Expect(finalStatus.Documents).NotTo(gomega.BeEmpty())
			createdDocIDs = append(createdDocIDs, finalStatus.Documents[0].ID)

			exportResp, err := digitization.ExportAllData(ctx, digitizeBaseURL, 0)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(exportResp.Status).To(gomega.Equal("completed"))
			gomega.Expect(exportResp.ExportTimestamp).NotTo(gomega.BeEmpty())
			gomega.Expect(exportResp.Data.Jobs).NotTo(gomega.BeEmpty())
			gomega.Expect(exportResp.Summary.Jobs.TotalExported).To(gomega.BeNumerically(">=", 1))
			gomega.Expect(exportResp.Pagination.TotalRecords).To(gomega.BeNumerically(">=", 1))

			// Confirm seeded job ID appears in the exported payload.
			found := false
			for _, job := range exportResp.Data.Jobs {
				if id, ok := job["job_id"].(string); ok && id == jobResp.JobID {
					found = true
					break
				}
			}
			gomega.Expect(found).To(gomega.BeTrue(), "seeded job %s should appear in export response", jobResp.JobID)
			logger.Infof("[TEST] ✓ Export returned %d job(s) and %d document(s)",
				exportResp.Summary.Jobs.TotalExported, exportResp.Summary.Documents.TotalExported)
		})

		ginkgo.It("should honour positive limit in /v1/export pagination", func() {
			ctx, cancel := withTimeout(3 * time.Minute)
			defer cancel()

			const limitOne = 1
			exportResp, err := digitization.ExportAllData(ctx, digitizeBaseURL, limitOne)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(exportResp.Pagination.Limit).To(gomega.Equal(limitOne))
			gomega.Expect(len(exportResp.Data.Jobs)).To(gomega.BeNumerically("<=", limitOne))
			logger.Infof("[TEST] ✓ Export limit=%d returned %d job(s) has_more=%v",
				limitOne, len(exportResp.Data.Jobs), exportResp.Pagination.HasMore)
		})

		// ── Import API ──────────────────────────────────────────────────────────

		ginkgo.It("should import previously exported data via /v1/import", func() {
			ctx, cancel := withTimeout(15 * time.Minute)
			defer cancel()

			// Create a completed job so there is at least one exportable record.
			jobResp, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "digitization", "json", "e2e-import-roundtrip")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			createdJobIDs = append(createdJobIDs, jobResp.JobID)

			finalStatus, err := digitization.WaitForJobCompletion(ctx, digitizeBaseURL, jobResp.JobID, 10*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(finalStatus.Status).To(gomega.Equal("completed"))
			gomega.Expect(finalStatus.Documents).NotTo(gomega.BeEmpty())
			createdDocIDs = append(createdDocIDs, finalStatus.Documents[0].ID)

			// Export then re-import the same records; existing records must be skipped (idempotent).
			exportResp, err := digitization.ExportAllData(ctx, digitizeBaseURL, 0)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(exportResp.Data.Jobs).NotTo(gomega.BeEmpty())

			importResp, err := digitization.ImportData(ctx, digitizeBaseURL, map[string]interface{}{
				"data": map[string]interface{}{
					"jobs":      exportResp.Data.Jobs,
					"documents": exportResp.Data.Documents,
				},
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(importResp.Status).To(gomega.Equal("completed"))
			gomega.Expect(importResp.Summary.Jobs.Imported+importResp.Summary.Jobs.Skipped+
				importResp.Summary.Documents.Imported+importResp.Summary.Documents.Skipped).
				To(gomega.BeNumerically(">=", 1), "import should have imported or skipped at least one record")
			gomega.Expect(importResp.Summary.Jobs.Failed).To(gomega.Equal(0), "no jobs should have failed during import")
			gomega.Expect(importResp.Summary.Documents.Failed).To(gomega.Equal(0), "no documents should have failed during import")

			logger.Infof("[TEST] ✓ Import round-trip: jobs(imported=%d skipped=%d) docs(imported=%d skipped=%d)",
				importResp.Summary.Jobs.Imported, importResp.Summary.Jobs.Skipped,
				importResp.Summary.Documents.Imported, importResp.Summary.Documents.Skipped)
		})

		// assertImport422 is a shared helper for the two import validation-error specs below.
		assertImport422 := func(ctx context.Context, payload map[string]interface{}, wantSubstr, label string) {
			_, err := digitization.ImportDataExpectingError(ctx, digitizeBaseURL, payload)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("422"), "%s: expected HTTP 422", label)
			gomega.Expect(err.Error()).To(gomega.ContainSubstring(wantSubstr), "%s: expected %q in error body", label, wantSubstr)
			logger.Infof("[TEST] ✓ %s correctly rejected (422): %v", label, err)
		}

		ginkgo.It("should return 422 when importing with missing 'data' key", func() {
			// FastAPI rejects payloads lacking the top-level "data" wrapper with 422.
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()
			assertImport422(ctx, map[string]interface{}{
				"jobs": []interface{}{}, "documents": []interface{}{},
			}, "data", "missing-data-key")
		})

		ginkgo.It("should return 422 when importing an empty data payload", func() {
			// API requires at least one job or document record.
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()
			assertImport422(ctx, map[string]interface{}{
				"data": map[string]interface{}{"jobs": []interface{}{}, "documents": []interface{}{}},
			}, "At least one", "empty-data-payload")
		})
	})

	ginkgo.Context("Summarization Tests", ginkgo.Label("summarization-tests"), func() {
		var summarizeBaseURL string
		var createdJobIDs []string

		ginkgo.BeforeAll(func() {
			if templateName != "summarize" {
				ginkgo.Skip(fmt.Sprintf("Skipping summarization tests — template is '%s', not 'summarize'", templateName))
			}
			if appName == "" {
				ginkgo.Fail("Application name is not set")
			}

			logger.Infof("[SUMMARIZE] Setting up summarization tests for app: %s", appName)

			// Ensure catalog session is active before any CLI call.
			// When --app-name is provided the catalog is already running but the
			// session token may not have been established (e.g. CATALOG_PASSWORD was
			// not set at BeforeSuite time, or the token TTL has elapsed).
			loginCtx, loginCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			catalogLoginWithDiscovery(loginCtx, true)
			loginCancel()

			summarizeBaseURL = waitForSummarizeURL("SUMMARIZE", 10*time.Minute)
			logger.Infof("[SUMMARIZE] Summarize Base URL: %s", summarizeBaseURL)
			waitForSummarizeHealthy("SUMMARIZE", summarizeBaseURL, 15*time.Minute)
		})

		ginkgo.AfterEach(func() {
			for _, jobID := range createdJobIDs {
				cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 5*time.Minute)
				_, _ = summarization.WaitForJobCompletion(cleanCtx, summarizeBaseURL, jobID, 5*time.Minute)
				_ = summarization.DeleteJob(cleanCtx, summarizeBaseURL, jobID)
				cleanCancel()
			}
			createdJobIDs = nil
		})

		ginkgo.It("verifies summarization service health", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			err := summarization.HealthCheck(ctx, summarizeBaseURL)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			logger.Infof("[TEST] ✓ Summarization service health check passed")
		})

		ginkgo.It("summarizes a PDF file with standard level", func() {
			ctx, cancel := withTimeout(15 * time.Minute)
			defer cancel()

			pdfPath := testFilePath("ingestion/docs/test_doc.pdf")
			jobName := fmt.Sprintf("pdf-summary-%d", time.Now().Unix())

			res, err := summarization.SubmitAndVerifyJob(ctx, summarizeBaseURL, pdfPath, "", "standard", jobName, false, 15*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			createdJobIDs = append(createdJobIDs, res.Detail.JobID)
			logger.Infof("[TEST] ✓ PDF summary retrieved successfully (length: %d chars)", len(res.Summary))
		})

		ginkgo.It("summarizes a TXT file with brief level", func() {
			ctx, cancel := withTimeout(15 * time.Minute)
			defer cancel()

			txtPath := testFilePath("ingestion/docs/sample_txt.txt")
			jobName := fmt.Sprintf("txt-summary-%d", time.Now().Unix())

			res, err := summarization.SubmitAndVerifyJob(ctx, summarizeBaseURL, txtPath, "", "brief", jobName, false, 15*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			createdJobIDs = append(createdJobIDs, res.Detail.JobID)
			logger.Infof("[TEST] ✓ TXT summary retrieved successfully (length: %d chars)", len(res.Summary))
		})

		ginkgo.It("summarizes text input with detailed level", func() {
			ctx, cancel := withTimeout(15 * time.Minute)
			defer cancel()

			testText := "Artificial Intelligence (AI) is transforming industries worldwide. Machine learning algorithms enable computers to learn from data and improve their performance over time. Deep learning, a subset of machine learning, uses neural networks with multiple layers to process complex patterns. Natural language processing allows machines to understand and generate human language. Computer vision enables machines to interpret and analyze visual information from the world."
			jobName := fmt.Sprintf("text-summary-%d", time.Now().Unix())

			res, err := summarization.SubmitAndVerifyJob(ctx, summarizeBaseURL, "", testText, "detailed", jobName, false, 15*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			createdJobIDs = append(createdJobIDs, res.Detail.JobID)
			logger.Infof("[TEST] ✓ Text summary retrieved successfully (length: %d chars)", len(res.Summary))
		})

		ginkgo.It("tests different summary levels produce different outputs", func() {
			ctx, cancel := withTimeout(20 * time.Minute)
			defer cancel()

			testText := "Climate change is one of the most pressing challenges facing humanity today. Rising global temperatures are causing ice caps to melt, sea levels to rise, and weather patterns to become more extreme. Scientists agree that human activities, particularly the burning of fossil fuels, are the primary drivers of this change. Renewable energy sources like solar and wind power offer promising solutions. Governments and organizations worldwide are working to reduce carbon emissions and transition to sustainable practices."

			levels := []string{"brief", "standard", "detailed"}
			summaries := make(map[string]string)

			for _, level := range levels {
				jobName := fmt.Sprintf("level-test-%s-%d", level, time.Now().Unix())

				res, err := summarization.SubmitAndVerifyJob(ctx, summarizeBaseURL, "", testText, level, jobName, false, 15*time.Minute)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				createdJobIDs = append(createdJobIDs, res.Detail.JobID)
				summaries[level] = res.Summary
				logger.Infof("[TEST] %s summary length: %d chars", level, len(res.Summary))
			}

			// Verify summaries are different
			gomega.Expect(summaries["brief"]).NotTo(gomega.Equal(summaries["standard"]))
			gomega.Expect(summaries["standard"]).NotTo(gomega.Equal(summaries["detailed"]))
			logger.Infof("[TEST] ✓ Different summary levels produce different outputs")
		})

		ginkgo.It("tests streaming mode", func() {
			ctx, cancel := withTimeout(15 * time.Minute)
			defer cancel()

			testText := "The Internet of Things (IoT) connects everyday devices to the internet, enabling them to send and receive data. Smart homes use IoT devices to automate lighting, heating, and security systems. Wearable technology tracks health metrics and fitness activities. Industrial IoT improves manufacturing efficiency and predictive maintenance."
			jobName := fmt.Sprintf("stream-test-%d", time.Now().Unix())

			res, err := summarization.SubmitAndVerifyJob(ctx, summarizeBaseURL, "", testText, "standard", jobName, true, 15*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			createdJobIDs = append(createdJobIDs, res.Detail.JobID)
			logger.Infof("[TEST] ✓ Streaming summarization job completed: %s", res.Detail.JobID)
		})

		ginkgo.It("handles empty text input - job fails with appropriate error", func() {
			ctx, cancel := withTimeout(2 * time.Minute)
			defer cancel()

			jobName := fmt.Sprintf("empty-text-%d", time.Now().Unix())

			finalStatus, err := summarization.SubmitJobExpectingFailure(ctx, summarizeBaseURL, "", "standard", jobName, 2*time.Minute)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("job failed"))
			gomega.Expect(finalStatus.Status).To(gomega.Equal(summarization.JobStatusFailed))
			gomega.Expect(finalStatus.Error).NotTo(gomega.BeNil())
			gomega.Expect(*finalStatus.Error).To(gomega.ContainSubstring("Extracted text is empty"))
			logger.Infof("[TEST] ✓ Empty text correctly failed with error: %s", *finalStatus.Error)

			time.Sleep(5 * time.Second)
			gomega.Expect(summarization.DeleteJob(ctx, summarizeBaseURL, finalStatus.JobID)).To(gomega.Succeed())
		})

		ginkgo.It("handles invalid summary level - defaults to standard", func() {
			ctx, cancel := withTimeout(2 * time.Minute)
			defer cancel()

			testText := "This is a test text for invalid level. Artificial Intelligence is transforming industries."
			jobName := fmt.Sprintf("invalid-level-%d", time.Now().Unix())

			// API accepts invalid level and treats it as "standard" — job completes.
			res, err := summarization.SubmitAndVerifyJob(ctx, summarizeBaseURL, "", testText, "invalid_level", jobName, false, 2*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			logger.Infof("[TEST] ✓ Job completed successfully with default level")

			time.Sleep(5 * time.Second)
			gomega.Expect(summarization.DeleteJob(ctx, summarizeBaseURL, res.Detail.JobID)).To(gomega.Succeed())
		})

		ginkgo.It("handles invalid file format error", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			// Create a temporary invalid file
			invalidPath := filepath.Join(os.TempDir(), fmt.Sprintf("invalid-%d.xyz", time.Now().Unix()))
			err := os.WriteFile(invalidPath, []byte("invalid content"), 0644)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			defer os.Remove(invalidPath)

			jobName := fmt.Sprintf("invalid-file-%d", time.Now().Unix())
			errorResp, statusCode, err := summarization.CreateJobExpectingError(ctx, summarizeBaseURL, invalidPath, "standard", jobName, false)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(statusCode).To(gomega.Equal(415)) // Unsupported Media Type
			gomega.Expect(errorResp).NotTo(gomega.BeNil())
			gomega.Expect(errorResp.Error.Code).To(gomega.Equal(415))
			gomega.Expect(errorResp.Error.Message).To(gomega.Or(
				gomega.ContainSubstring(".txt"),
				gomega.ContainSubstring(".pdf"),
				gomega.ContainSubstring("allowed"),
			))
			logger.Infof("[TEST] ✓ Invalid file format correctly rejected: %s", errorResp.Error.Message)
		})

		ginkgo.It("lists jobs with pagination and filters", func() {
			ctx, cancel := withTimeout(15 * time.Minute)
			defer cancel()

			// Create multiple jobs
			testText := "Test text for listing jobs."
			jobNames := []string{
				fmt.Sprintf("list-test-1-%d", time.Now().Unix()),
				fmt.Sprintf("list-test-2-%d", time.Now().Unix()),
			}

			for _, jobName := range jobNames {
				res, err := summarization.SubmitAndVerifyJob(ctx, summarizeBaseURL, "", testText, "brief", jobName, false, 15*time.Minute)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				createdJobIDs = append(createdJobIDs, res.Detail.JobID)
			}

			// List all jobs
			listResp, err := summarization.ListJobs(ctx, summarizeBaseURL, 10, 0, "", "")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(len(listResp.Data)).To(gomega.BeNumerically(">=", 2))
			logger.Infof("[TEST] ✓ Listed %d jobs", len(listResp.Data))

			// Test pagination
			listResp, err = summarization.ListJobs(ctx, summarizeBaseURL, 1, 0, "", "")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(len(listResp.Data)).To(gomega.Equal(1))
			logger.Infof("[TEST] ✓ Pagination works correctly")

			// Test filtering by job name
			listResp, err = summarization.ListJobs(ctx, summarizeBaseURL, 10, 0, "", jobNames[0])
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(len(listResp.Data)).To(gomega.BeNumerically(">=", 1))
			logger.Infof("[TEST] ✓ Job name filtering works correctly")
		})

		ginkgo.It("deletes a job successfully", func() {
			ctx, cancel := withTimeout(15 * time.Minute)
			defer cancel()

			testText := "Test text for job deletion."
			jobName := fmt.Sprintf("delete-test-%d", time.Now().Unix())

			res, err := summarization.SubmitAndVerifyJob(ctx, summarizeBaseURL, "", testText, "brief", jobName, false, 15*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			jobID := res.Detail.JobID
			logger.Infof("[TEST] Job ready for deletion: %s", jobID)

			gomega.Expect(summarization.DeleteJob(ctx, summarizeBaseURL, jobID)).To(gomega.Succeed())
			logger.Infof("[TEST] ✓ Job deleted successfully: %s", jobID)

			// Verify job is deleted (should return error)
			_, err = summarization.GetJobDetail(ctx, summarizeBaseURL, jobID)
			gomega.Expect(err).To(gomega.HaveOccurred())
			logger.Infof("[TEST] ✓ Verified job no longer exists")
		})
	})

	ginkgo.Context("Synchronous Summarization Tests", ginkgo.Label("summarization-tests"), func() {
		// summarizeBaseURL is resolved fresh in BeforeAll from the running app.
		var syncSummarizeBaseURL string

		ginkgo.BeforeAll(func() {
			if templateName != "summarize" {
				ginkgo.Skip(fmt.Sprintf("Skipping synchronous summarization tests — template is '%s', not 'summarize'", templateName))
			}
			if appName == "" {
				ginkgo.Fail("Application name is not set")
			}

			logger.Infof("[SUMMARIZE-SYNC] Setting up synchronous summarization tests for app: %s", appName)

			loginCtx, loginCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			catalogLoginWithDiscovery(loginCtx, true)
			loginCancel()

			syncSummarizeBaseURL = waitForSummarizeURL("SUMMARIZE-SYNC", 10*time.Minute)
			logger.Infof("[SUMMARIZE-SYNC] Summarize Base URL: %s", syncSummarizeBaseURL)
			waitForSummarizeHealthy("SUMMARIZE-SYNC", syncSummarizeBaseURL, 15*time.Minute)
		})

		// ── JSON body — happy path ────────────────────────────────────────────

		ginkgo.It("summarizes small text via JSON body (standard level)", func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			resp, err := summarization.SummarizeText(ctx, syncSummarizeBaseURL,
				"AI is transforming industries.", "standard")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(resp.Summary()).NotTo(gomega.BeEmpty())
			logger.Infof("[TEST] ✓ small text standard (len=%d)", len(resp.Summary()))
		})

		ginkgo.It("summarizes medium text via JSON body (standard level)", func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			resp, err := summarization.SummarizeText(ctx, syncSummarizeBaseURL,
				"Artificial intelligence is widely used in healthcare, finance, and transportation.", "standard")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(resp.Summary()).NotTo(gomega.BeEmpty())
			logger.Infof("[TEST] ✓ medium text standard (len=%d)", len(resp.Summary()))
		})

		ginkgo.It("summarizes larger text via JSON body (standard level)", func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			text := "Artificial intelligence has significantly evolved over the past decade, transforming industries " +
				"ranging from healthcare and finance to transportation and education. " +
				"Machine learning algorithms now power recommendation engines, fraud detection systems, " +
				"and predictive analytics platforms used by millions of people every day.\n\n" +
				"In healthcare, AI-assisted diagnostics can detect diseases such as cancer and diabetic " +
				"retinopathy from medical images with accuracy rivalling that of experienced clinicians. " +
				"Drug discovery pipelines are being accelerated by models that predict molecular interactions, " +
				"reducing the time and cost of bringing new treatments to market.\n\n" +
				"The financial sector relies on AI for real-time fraud detection, algorithmic trading, " +
				"credit scoring, and personalised investment advice. Natural language processing enables " +
				"chatbots and virtual assistants to handle customer queries at scale, reducing operational " +
				"costs while improving response times.\n\n" +
				"Despite these advances, significant challenges remain. Ensuring fairness, transparency, " +
				"and accountability in automated decision-making is an active area of research and regulation. " +
				"The environmental cost of training large models and the risk of job displacement in " +
				"certain sectors are also subjects of ongoing public debate."

			resp, err := summarization.SummarizeText(ctx, syncSummarizeBaseURL, text, "standard")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(resp.Summary()).NotTo(gomega.BeEmpty())
			logger.Infof("[TEST] ✓ larger text standard (len=%d)", len(resp.Summary()))
		})

		ginkgo.It("summarizes text via JSON body with 'brief' level", func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			text := "The explosion in AI's capabilities over the last few years has been immense. " +
				"And we're starting to see how AI can be applied to real business use cases at the scale needed for enterprise demands. " +
				"In 2022, IBM unveiled the IBM z16, the latest system that brought powerful AI capabilities to IBM Z for the first time."

			resp, err := summarization.SummarizeText(ctx, syncSummarizeBaseURL, text, "brief")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(resp.Summary()).NotTo(gomega.BeEmpty())
			logger.Infof("[TEST] ✓ text brief (len=%d)", len(resp.Summary()))
		})

		ginkgo.It("summarizes text via JSON body with 'detailed' level", func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			text := "Quantum computing leverages superposition and entanglement to process information differently than classical computers. " +
				"Quantum bits (qubits) can represent both 0 and 1 simultaneously, giving quantum computers an exponential advantage " +
				"for cryptography, drug discovery, and optimization problems."

			resp, err := summarization.SummarizeText(ctx, syncSummarizeBaseURL, text, "detailed")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(resp.Summary()).NotTo(gomega.BeEmpty())
			logger.Infof("[TEST] ✓ text detailed (len=%d)", len(resp.Summary()))
		})

		ginkgo.It("summarizes text via JSON body with no level (defaults to standard)", func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			resp, err := summarization.SummarizeText(ctx, syncSummarizeBaseURL,
				"AI improves efficiency", "")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(resp.Summary()).NotTo(gomega.BeEmpty())
			logger.Infof("[TEST] ✓ text no-level (len=%d)", len(resp.Summary()))
		})

		ginkgo.It("different JSON levels produce different summaries for the same text", func() {
			ctx, cancel := withTimeout(15 * time.Minute)
			defer cancel()

			text := "The Internet of Things connects billions of physical devices to the internet, enabling data collection " +
				"and remote control. Smart cities use IoT sensors to optimize traffic, energy, and public safety. " +
				"Wearable devices monitor health metrics in real time. Security and privacy remain the key challenges."

			levels := []string{"brief", "standard", "detailed"}
			summaries := make(map[string]string, len(levels))
			for _, level := range levels {
				resp, err := summarization.SummarizeText(ctx, syncSummarizeBaseURL, text, level)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(resp.Summary()).NotTo(gomega.BeEmpty())
				summaries[level] = resp.Summary()
				logger.Infof("[TEST] level=%s summary_len=%d", level, len(resp.Summary()))
			}
			gomega.Expect(summaries["brief"]).NotTo(gomega.Equal(summaries["detailed"]))
			logger.Infof("[TEST] ✓ different levels produce different summaries")
		})

		// ── JSON body — legacy length ─────────────────────────────────────────

		ginkgo.It("summarizes text via JSON body with legacy 'length' field and verifies summary_length range", func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			text := "Artificial intelligence is transforming industries across healthcare, finance, transportation, education, " +
				"manufacturing, retail, logistics, and many other domains by automating complex processes, improving efficiency, " +
				"enabling faster decision-making, reducing operational costs, enhancing customer experience, and driving innovation."

			resp, err := summarization.SummarizeTextWithLength(ctx, syncSummarizeBaseURL, text, 20)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(resp.Summary()).NotTo(gomega.BeEmpty())
			// Allow ±75% tolerance around the requested length (server does approximate word-count).
			gomega.Expect(resp.Data.SummaryLength).To(gomega.BeNumerically(">=", 5))
			gomega.Expect(resp.Data.SummaryLength).To(gomega.BeNumerically("<=", 35))
			logger.Infof("[TEST] ✓ legacy length=20, actual summary_length=%d", resp.Data.SummaryLength)
		})

		// ── JSON body — stream=true ───────────────────────────────────────────

		ginkgo.It("receives SSE chunks when stream=true via JSON body", func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			rawBody, statusCode, err := summarization.SummarizeRawBody(
				ctx, syncSummarizeBaseURL,
				bytes.NewBufferString(`{"text":"Artificial intelligence improves productivity and efficiency.","stream":true}`),
				"application/json",
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(statusCode).To(gomega.Equal(http.StatusOK))
			// SSE responses contain "data:" event lines.
			gomega.Expect(string(rawBody)).To(gomega.ContainSubstring("data:"))
			logger.Infof("[TEST] ✓ stream=true returned SSE body (len=%d)", len(rawBody))
		})

		ginkgo.It("summarizes text with stream=false via JSON body", func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			resp, err := summarization.SummarizeText(ctx, syncSummarizeBaseURL,
				"AI improves productivity", "standard")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(resp.Summary()).NotTo(gomega.BeEmpty())
			logger.Infof("[TEST] ✓ stream=false returned summary (len=%d)", len(resp.Summary()))
		})

		// ── Multipart / file upload — happy path ─────────────────────────────

		ginkgo.It("summarizes a TXT file via multipart form (standard level)", func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			resp, err := summarization.SummarizeFile(ctx, syncSummarizeBaseURL,
				testFilePath("ingestion/docs/sample_txt.txt"), "standard")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			kw, found := common.SummaryContainsAnyKeyword(resp.Summary(), summarization.TXTSummaryKeywords)
			gomega.Expect(found).To(gomega.BeTrue(),
				fmt.Sprintf("expected TXT summary to mention one of %v, got: %q", summarization.TXTSummaryKeywords, resp.Summary()))
			logger.Infof("[TEST] ✓ TXT upload standard (len=%d, keyword=%q)", len(resp.Summary()), kw)
		})

		ginkgo.It("summarizes a TXT file via multipart form with 'brief' level field", func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			resp, err := summarization.SummarizeFile(ctx, syncSummarizeBaseURL,
				testFilePath("ingestion/docs/sample_txt.txt"), "brief")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			kw, found := common.SummaryContainsAnyKeyword(resp.Summary(), summarization.TXTSummaryKeywords)
			gomega.Expect(found).To(gomega.BeTrue(),
				fmt.Sprintf("expected TXT summary to mention one of %v, got: %q", summarization.TXTSummaryKeywords, resp.Summary()))
			logger.Infof("[TEST] ✓ TXT level=brief (len=%d, keyword=%q)", len(resp.Summary()), kw)
		})

		ginkgo.It("summarizes a PDF file via multipart form (no level — default)", func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			resp, err := summarization.SummarizeFile(ctx, syncSummarizeBaseURL,
				testFilePath("ingestion/docs/sync_test.pdf"), "")
			if summarization.IsContextLimitError(err) {
				ginkgo.Skip(fmt.Sprintf("skipping — PDF exceeds model context limit: %v", err))
			}
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			kw, found := common.SummaryContainsAnyKeyword(resp.Summary(), summarization.PDFSummaryKeywords)
			gomega.Expect(found).To(gomega.BeTrue(),
				fmt.Sprintf("expected PDF summary to mention one of %v, got: %q", summarization.PDFSummaryKeywords, resp.Summary()))
			logger.Infof("[TEST] ✓ PDF no level (len=%d, keyword=%q)", len(resp.Summary()), kw)
		})

		ginkgo.It("summarizes a PDF file via multipart form with 'detailed' level field", func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			resp, err := summarization.SummarizeFile(ctx, syncSummarizeBaseURL,
				testFilePath("ingestion/docs/sync_test.pdf"), "detailed")
			if summarization.IsContextLimitError(err) {
				ginkgo.Skip(fmt.Sprintf("skipping — PDF exceeds model context limit: %v", err))
			}
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			kw, found := common.SummaryContainsAnyKeyword(resp.Summary(), summarization.PDFSummaryKeywords)
			gomega.Expect(found).To(gomega.BeTrue(),
				fmt.Sprintf("expected PDF summary to mention one of %v, got: %q", summarization.PDFSummaryKeywords, resp.Summary()))
			logger.Infof("[TEST] ✓ PDF level=detailed (len=%d, keyword=%q)", len(resp.Summary()), kw)
		})

		ginkgo.It("summarizes a PDF file via multipart form with legacy 'length' field", func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			resp, err := summarization.SummarizeFileWithLength(ctx, syncSummarizeBaseURL,
				testFilePath("ingestion/docs/sync_test.pdf"), 50)
			if summarization.IsContextLimitError(err) {
				ginkgo.Skip(fmt.Sprintf("skipping — PDF exceeds model context limit: %v", err))
			}
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(resp.Summary()).NotTo(gomega.BeEmpty())
			logger.Infof("[TEST] ✓ PDF legacy length=50 (summary_len=%d)", resp.Data.SummaryLength)
		})

		// ── Error paths — input validation ────────────────────────────────────

		ginkgo.It("returns 400 for missing input — empty JSON object {}", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			rawBody, statusCode, err := summarization.SummarizeRawBody(
				ctx, syncSummarizeBaseURL,
				bytes.NewBufferString(`{}`),
				"application/json",
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(statusCode).To(gomega.Equal(http.StatusBadRequest))
			gomega.Expect(string(rawBody)).To(gomega.ContainSubstring("error"))
			logger.Infof("[TEST] ✓ empty JSON object rejected (status=%d)", statusCode)
		})

		ginkgo.It("returns 400 for invalid field name — {\"txt\":\"AI\"}", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			rawBody, statusCode, err := summarization.SummarizeRawBody(
				ctx, syncSummarizeBaseURL,
				bytes.NewBufferString(`{"txt":"AI"}`),
				"application/json",
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(statusCode).To(gomega.Equal(http.StatusBadRequest))
			gomega.Expect(string(rawBody)).To(gomega.ContainSubstring("error"))
			logger.Infof("[TEST] ✓ invalid field rejected (status=%d)", statusCode)
		})

		ginkgo.It("returns 400 for invalid JSON body", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			rawBody, statusCode, err := summarization.SummarizeRawBody(
				ctx, syncSummarizeBaseURL,
				bytes.NewBufferString(`{"text":"AI"`), // truncated JSON
				"application/json",
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(statusCode).To(gomega.Equal(http.StatusBadRequest))
			gomega.Expect(string(rawBody)).To(gomega.ContainSubstring("error"))
			logger.Infof("[TEST] ✓ invalid JSON rejected (status=%d)", statusCode)
		})

		ginkgo.It("returns 415 for wrong Content-Type (text/plain)", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			rawBody, statusCode, err := summarization.SummarizeRawBody(
				ctx, syncSummarizeBaseURL,
				bytes.NewBufferString("AI"),
				"text/plain",
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(statusCode).To(gomega.Equal(http.StatusUnsupportedMediaType))
			gomega.Expect(string(rawBody)).To(gomega.ContainSubstring("error"))
			logger.Infof("[TEST] ✓ text/plain rejected (status=%d)", statusCode)
		})

		ginkgo.It("returns 415 for empty request (no Content-Type)", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			rawBody, statusCode, err := summarization.SummarizeRawBody(
				ctx, syncSummarizeBaseURL,
				bytes.NewBufferString(""),
				"", // no content-type
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(statusCode).To(gomega.Equal(http.StatusUnsupportedMediaType))
			gomega.Expect(string(rawBody)).To(gomega.ContainSubstring("error"))
			logger.Infof("[TEST] ✓ empty request (no content-type) rejected (status=%d)", statusCode)
		})

		ginkgo.It("returns 400 for empty text in JSON body", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			errResp, statusCode, err := summarization.SummarizeTextExpectingError(
				ctx, syncSummarizeBaseURL, "", "standard")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errResp).NotTo(gomega.BeNil())
			gomega.Expect(statusCode).To(gomega.Equal(http.StatusBadRequest))
			gomega.Expect(errResp.Error.Code).To(gomega.Equal(400))
			logger.Infof("[TEST] ✓ empty text rejected (status=%d, msg=%s)", statusCode, errResp.Error.Message)
		})

		ginkgo.It("returns 400 for empty TXT file upload", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			emptyPath := filepath.Join(os.TempDir(), fmt.Sprintf("empty-sync-%d.txt", time.Now().Unix()))
			gomega.Expect(os.WriteFile(emptyPath, []byte(""), 0644)).To(gomega.Succeed())
			defer os.Remove(emptyPath) //nolint:errcheck

			errResp, statusCode, err := summarization.SummarizeFileExpectingError(
				ctx, syncSummarizeBaseURL, emptyPath, "")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errResp).NotTo(gomega.BeNil())
			gomega.Expect(statusCode).To(gomega.Equal(http.StatusBadRequest))
			gomega.Expect(errResp.Error.Code).To(gomega.Equal(400))
			logger.Infof("[TEST] ✓ empty file rejected (status=%d, msg=%s)", statusCode, errResp.Error.Message)
		})

		ginkgo.It("returns 400 for blank PDF file upload", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			errResp, statusCode, err := summarization.SummarizeFileExpectingError(
				ctx, syncSummarizeBaseURL,
				testFilePath("ingestion/docs/blank.pdf"), "")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errResp).NotTo(gomega.BeNil())
			gomega.Expect(statusCode).To(gomega.Equal(http.StatusBadRequest))
			gomega.Expect(errResp.Error.Code).To(gomega.Equal(400))
			logger.Infof("[TEST] ✓ blank PDF rejected (status=%d, msg=%s)", statusCode, errResp.Error.Message)
		})

		ginkgo.It("returns 400 for invalid level parameter via multipart form", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			errResp, statusCode, err := summarization.SummarizeFileExpectingError(
				ctx, syncSummarizeBaseURL,
				testFilePath("ingestion/docs/sample_txt.txt"), "rejk")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errResp).NotTo(gomega.BeNil())
			gomega.Expect(statusCode).To(gomega.Equal(http.StatusBadRequest))
			gomega.Expect(errResp.Error.Code).To(gomega.Equal(400))
			logger.Infof("[TEST] ✓ invalid level rejected (status=%d, msg=%s)", statusCode, errResp.Error.Message)
		})

		ginkgo.It("returns 415 for invalid PDF file (fake bytes with .pdf extension)", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			invalidPDFPath := filepath.Join(os.TempDir(), fmt.Sprintf("invalid-sync-%d.pdf", time.Now().Unix()))
			gomega.Expect(os.WriteFile(invalidPDFPath, []byte("not a real pdf"), 0644)).To(gomega.Succeed())
			defer os.Remove(invalidPDFPath) //nolint:errcheck

			errResp, statusCode, err := summarization.SummarizeFileExpectingError(
				ctx, syncSummarizeBaseURL, invalidPDFPath, "")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errResp).NotTo(gomega.BeNil())
			gomega.Expect(statusCode).To(gomega.Equal(http.StatusUnsupportedMediaType))
			gomega.Expect(errResp.Error.Code).To(gomega.Equal(415))
			logger.Infof("[TEST] ✓ invalid PDF rejected (status=%d, msg=%s)", statusCode, errResp.Error.Message)
		})

		ginkgo.It("returns 415 for binary TXT file (random bytes with .txt extension)", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			binaryTXTPath := filepath.Join(os.TempDir(), fmt.Sprintf("invalid-sync-%d.txt", time.Now().Unix()))
			// Write 32 bytes of non-UTF8 binary content to mimic `head -c 100 /dev/urandom`.
			binaryContent := make([]byte, 32)
			for i := range binaryContent {
				binaryContent[i] = byte(i + 128) //nolint:mnd // values > 127 are non-ASCII
			}
			gomega.Expect(os.WriteFile(binaryTXTPath, binaryContent, 0644)).To(gomega.Succeed())
			defer os.Remove(binaryTXTPath) //nolint:errcheck

			errResp, statusCode, err := summarization.SummarizeFileExpectingError(
				ctx, syncSummarizeBaseURL, binaryTXTPath, "")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errResp).NotTo(gomega.BeNil())
			gomega.Expect(statusCode).To(gomega.Equal(http.StatusUnsupportedMediaType))
			gomega.Expect(errResp.Error.Code).To(gomega.Equal(415))
			logger.Infof("[TEST] ✓ binary TXT rejected (status=%d, msg=%s)", statusCode, errResp.Error.Message)
		})

		// ── TC-2: Invalid level via JSON body ─────────────────────────────────
		//
		// Rationale: The existing test "returns 400 for invalid level parameter
		// via multipart form" only covers the multipart/form-data code path.
		// The JSON body path through app.py → summ_utils.validate_summary_level()
		// is a separate dispatch branch and must be tested independently.
		//
		// Expected: HTTP 400, errResp.Error.Code == 400.
		ginkgo.It("returns 400 for invalid level value via JSON body", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			errResp, statusCode, err := summarization.SummarizeTextExpectingError(
				ctx, syncSummarizeBaseURL,
				"IBM Power Systems are designed for enterprise AI workloads.",
				"ultra-brief",
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(errResp).NotTo(gomega.BeNil())
			gomega.Expect(statusCode).To(gomega.Equal(http.StatusBadRequest))
			gomega.Expect(errResp.Error.Code).To(gomega.Equal(400))
			logger.Infof("[TEST] ✓ invalid level via JSON body rejected (status=%d, msg=%s)", statusCode, errResp.Error.Message)
		})

		// ── TC-3: Both level and length supplied ──────────────────────────────
		//
		// Rationale: "level" (abstraction-based) and "length" (legacy word-count)
		// are mutually exclusive.  Providing both in the same JSON body is
		// ambiguous and must be rejected before any LLM call is made.
		// This code path (app.py: if summary_level is not None and
		// summary_length is not None) has no existing test coverage.
		//
		// Expected: HTTP 400, response body contains "error".
		ginkgo.It("returns 400 when both level and length are supplied in JSON body", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			rawBody, statusCode, err := summarization.SummarizeRawBody(
				ctx, syncSummarizeBaseURL,
				bytes.NewBufferString(`{"text":"IBM Power Systems are designed for enterprise AI workloads.","level":"brief","length":10}`),
				"application/json",
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(statusCode).To(gomega.Equal(http.StatusBadRequest))
			gomega.Expect(string(rawBody)).To(gomega.ContainSubstring("error"))
			logger.Infof("[TEST] ✓ level+length conflict rejected (status=%d)", statusCode)
		})

		// ── TC-5: Connectivity failure — unreachable summarize API ────────────
		//
		// Rationale: When the summarization service is completely absent (not
		// deployed, wrong namespace, network partition) callers must receive an
		// immediate transport-level error — not a hang or silent empty result.
		// The context timeout (30 s) is shorter than the HTTP client default,
		// guaranteeing DNS / TCP failure causes a rapid error rather than a hang.
		// This is the only test that guards against a dead-endpoint condition.
		//
		// Expected: err is non-nil (transport-level failure).
		ginkgo.It("returns a transport error when the summarize API is unreachable", func() {
			ctx, cancel := withTimeout(30 * time.Second)
			defer cancel()

			logger.Infof("[TEST] Attempting request to unreachable summarize URL: %s", unreachableSummarizeURL)

			_, _, transportErr := summarization.SummarizeRawBody(
				ctx, unreachableSummarizeURL,
				bytes.NewBufferString(`{"text":"what is IBM Power?"}`),
				"application/json",
			)
			gomega.Expect(transportErr).To(
				gomega.HaveOccurred(),
				"Expected a transport error for an unreachable summarize API, but got nil",
			)
			logger.Infof("[TEST] ✓ transport error correctly received for unreachable URL: %v", transportErr)
		})

		// ── Concurrency ───────────────────────────────────────────────────────

		ginkgo.It("handles 32 concurrent JSON text requests without transport errors", func() {
			// Mirrors: seq 32 | parallel -j 32 make_request {}
			// Each goroutine fires simultaneously via a shared start gate.
			// Assertions:
			//   • No goroutine-level transport errors (network / TLS).
			//   • Every response is either 200 OK (success) or a documented server
			//     limit code: 429 Too Many Requests or 503 Service Unavailable.
			//   • At least one request must succeed (200).
			const concurrency = 32
			ctx, cancel := withTimeout(15 * time.Minute)
			defer cancel()

			text := "Artificial intelligence is transforming industries worldwide. " +
				"Machine learning enables computers to learn from data. " +
				"Deep learning uses neural networks to process complex patterns."

			results := summarization.RunConcurrentSummarizeText(
				ctx, syncSummarizeBaseURL, text, "standard", concurrency)

			successCount := 0
			for _, r := range results {
				gomega.Expect(r.Err).NotTo(gomega.HaveOccurred(),
					fmt.Sprintf("request #%d had a transport error", r.Index))
				gomega.Expect(r.StatusCode).To(gomega.BeElementOf(
					http.StatusOK,
					http.StatusTooManyRequests,
					http.StatusServiceUnavailable,
				), fmt.Sprintf("request #%d returned unexpected status", r.Index))
				if r.StatusCode == http.StatusOK {
					successCount++
				}
				logger.Infof("[TEST] request #%d status=%d latency=%s",
					r.Index, r.StatusCode, r.Latency.Round(time.Millisecond))
			}
			gomega.Expect(successCount).To(gomega.BeNumerically(">=", 1),
				"expected at least one concurrent request to succeed")
			logger.Infof("[TEST] ✓ concurrent text: %d/%d succeeded", successCount, concurrency)
		})

		ginkgo.It("handles 32 concurrent file upload requests without transport errors", func() {
			// Same as above but uses multipart/form-data with sync_test.pdf.
			// sync_test.pdf is intentionally small (~80 words) so no request
			// hits the 413 CONTEXT_LIMIT_EXCEEDED threshold.
			const concurrency = 32
			ctx, cancel := withTimeout(15 * time.Minute)
			defer cancel()

			results := summarization.RunConcurrentSummarizeFile(
				ctx, syncSummarizeBaseURL,
				testFilePath("ingestion/docs/sync_test.pdf"), "standard", concurrency)

			successCount := 0
			for _, r := range results {
				if summarization.IsContextLimitError(r.Err) {
					ginkgo.Skip(fmt.Sprintf("skipping — PDF exceeds model context limit on request #%d: %v", r.Index, r.Err))
				}
				gomega.Expect(r.Err).NotTo(gomega.HaveOccurred(),
					fmt.Sprintf("request #%d had a transport error", r.Index))
				gomega.Expect(r.StatusCode).To(gomega.BeElementOf(
					http.StatusOK,
					http.StatusTooManyRequests,
					http.StatusServiceUnavailable,
				), fmt.Sprintf("request #%d returned unexpected status", r.Index))
				if r.StatusCode == http.StatusOK {
					successCount++
				}
				logger.Infof("[TEST] file request #%d status=%d latency=%s",
					r.Index, r.StatusCode, r.Latency.Round(time.Millisecond))
			}
			gomega.Expect(successCount).To(gomega.BeNumerically(">=", 1),
				"expected at least one concurrent file upload to succeed")
			logger.Infof("[TEST] ✓ concurrent file: %d/%d succeeded", successCount, concurrency)
		})
	})

	ginkgo.Context("Similarity Tests", ginkgo.Label("spyre-dependent", "similarity-tests"), func() {
		var similarityBaseURL string
		var digitizeBaseURL string
		var createdJobIDs []string

		ginkgo.BeforeAll(func() {
			if appName == "" {
				ginkgo.Fail("Application name is not set")
			}

			logger.Infof("[SIMILARITY] Setting up similarity tests")

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			infoOutput, err := cli.WaitForApplicationInfoURLs(ctx, cfg, appName, appRuntime, 8*time.Minute, 15*time.Second)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			if appRuntime == "podman" {
				const similarityPollInterval = 15 * time.Second
				for {
					similarityBaseURL = cli.ExtractSimilarityAPIURL(infoOutput)
					digitizeBaseURL = cli.ExtractCatalogDigitizeURL(infoOutput)
					if similarityBaseURL != "" && digitizeBaseURL != "" {
						break
					}
					if ctx.Err() != nil {
						ginkgo.Fail("Timed out waiting for similarity-backend URL in 'application info' output")
					}
					logger.Infof("[SIMILARITY] similarity-backend URL not yet present — retrying in %s", similarityPollInterval)
					select {
					case <-ctx.Done():
						ginkgo.Fail("Timed out waiting for similarity-backend URL in 'application info' output")
					case <-time.After(similarityPollInterval):
					}
					infoOutput, err = cli.ApplicationInfo(ctx, cfg, appName, appRuntime)
					if err != nil {
						logger.Warningf("[SIMILARITY] application info error while polling for similarity URL: %v", err)
					}
				}
			} else {
				urlList := cli.ExtractURLsFromOutput(infoOutput)
				if len(urlList) == 0 {
					ginkgo.Fail("No urls extracted from application info output")
				} else {
					similarityBaseURL = urlList[0]
					digitizeBaseURL = strings.Replace(urlList[0], "ui", "digitize-api", 1)
				}
			}

			_ = err

			gomega.Expect(similarityBaseURL).NotTo(gomega.BeEmpty(),
				"could not determine similarity-api base URL")
			logger.Infof("[SIMILARITY] Similarity Base URL: %s", similarityBaseURL)
		})

		ginkgo.It("should pass health check",
			func() {
				ctx, cancel := withTimeout(30 * time.Second)
				defer cancel()

				resp, err := similarity.VerifyHealthEndpoint(ctx, similarityBaseURL)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(resp).NotTo(gomega.BeNil())
				gomega.Expect(resp.Status).NotTo(gomega.BeEmpty())
				logger.Infof("[TEST] Similarity service health check passed status=%q", resp.Status)
			})

		// Verify /v1/similarity-search with dense, sparse, and hybrid modes
		ginkgo.It("Verify /v1/similarity-search endpoint by providing different search mode such as dense, sparse or hybrid",
			func() {
				ctx, cancel := withTimeout(20 * time.Minute)
				defer cancel()

				pdfPath := digitization.GetTestPDFPath()
				gomega.Expect(pdfPath).NotTo(gomega.BeEmpty())

				// Step 1: Create digitization job
				logger.Infof("[TEST] Step 1: Creating ingestion job")
				jobResp, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "ingestion", "json", "e2e-similarity-workflow")
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(jobResp).NotTo(gomega.BeNil())
				gomega.Expect(jobResp.JobID).NotTo(gomega.BeEmpty())
				createdJobIDs = append(createdJobIDs, jobResp.JobID)
				logger.Infof("[TEST] Created ingestion job: %s", jobResp.JobID)

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
				logger.Infof("[TEST] Ingestion job completed: %s", jobResp.JobID)

				results := similarity.VerifySearchModes(ctx, similarityBaseURL)

				// Step 4: Every mode that returned a response must carry the correct score_type.
				logger.Infof("[TEST] Step 4: Verifying similarity search api")
				expectedScoreTypes := map[string]string{
					"dense":  "cosine",
					"sparse": "bm25",
					"hybrid": "hybrid",
				}
				for mode, resp := range results {
					gomega.Expect(resp).NotTo(gomega.BeNil(),
						"mode=%s: got nil response", mode)
					gomega.Expect(resp.ScoreType).To(gomega.Equal(expectedScoreTypes[mode]),
						"mode=%s: unexpected score_type", mode)
					logger.Infof("[TEST] C82598625: mode=%s score_type=%s results=%d",
						mode, resp.ScoreType, len(resp.Results))
				}
				// At least one mode must have responded successfully.
				gomega.Expect(results).NotTo(gomega.BeEmpty(),
					"all search modes failed — index may be empty or similarity-api is unreachable")
			})

		// Timing test — Verify Similarity search API includes time info in response headers or body in podman runtime
		ginkgo.It("Verify Similarity search API includes time info in response headers or body in podman runtime",
			func() {
				ctx, cancel := withTimeout(30 * time.Second)
				defer cancel()

				gomega.Expect(
					similarity.VerifyTimeInfoInResponse(ctx, similarityBaseURL),
				).To(gomega.Succeed())
				logger.Infof("[TEST] Timing info verified in similarity-api response")
			})

		// Verify /v1/similarity-search returns 400 for invalid mode
		ginkgo.It("Verify /v1/similarity-search endpoint by providing invalid parameter for mode field (400 error code)",
			func() {
				ctx, cancel := withTimeout(30 * time.Second)
				defer cancel()

				errResp, err := similarity.VerifyInvalidModeReturns400(ctx, similarityBaseURL)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(errResp).NotTo(gomega.BeNil())
				gomega.Expect(errResp.Error.Message).NotTo(gomega.BeEmpty())
				gomega.Expect(errResp.Error.Status).To(gomega.Equal(400))
				gomega.Expect(errResp.Error.Message).To(gomega.ContainSubstring("mode must be one of"))
				logger.Infof("[TEST] invalid mode correctly rejected with: %s", errResp.Error)
			})

		// Verify /v1/similarity-search with rerank=true
		ginkgo.It("Verify /v1/similarity-search endpoint by providing rerank as true",
			func() {
				ctx, cancel := withTimeout(2 * time.Minute)
				defer cancel()

				resp, err := similarity.VerifyRerankTrue(ctx, similarityBaseURL)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(resp).NotTo(gomega.BeNil())
				gomega.Expect(resp.ScoreType).To(gomega.Equal("relevance"))
				logger.Infof("[TEST] rerank=true returned score_type=%s results=%d",
					resp.ScoreType, len(resp.Results))
			})

		// Verify /v1/similarity-search returns 422 for invalid top_k
		ginkgo.It("Verify /v1/similarity-search endpoint by providing invalid value for top_k",
			func() {
				ctx, cancel := withTimeout(30 * time.Second)
				defer cancel()

				errResp, err := similarity.VerifyInvalidTopKReturns422(ctx, similarityBaseURL)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(errResp).NotTo(gomega.BeNil())
				gomega.Expect(errResp.Detail).NotTo(gomega.BeNil())
				logger.Infof("[TEST] invalid top_k correctly rejected with: %s", errResp.Error)
			})

		// Reproduce 400: Validation Error
		ginkgo.It("Reproduce 400 validation error code for similarity-search endpoint",
			func() {
				ctx, cancel := withTimeout(30 * time.Second)
				defer cancel()

				errResp, err := similarity.ReproduceValidationError(ctx, similarityBaseURL)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(errResp).NotTo(gomega.BeNil())
				gomega.Expect(errResp.Error.Message).NotTo(gomega.BeEmpty())
				gomega.Expect(errResp.Error.Status).To(gomega.Equal(400))
				logger.Infof("[TEST] 400 reproduced with error: %s", errResp.Error)
			})
	})
	ginkgo.Context("Application Backup And Restore", ginkgo.Ordered, ginkgo.Label("spyre-dependent", "app-backup-restore"), func() {
		var (
			digitizeDocID         string
			digitizeDocName       string
			digitizeDocStatus     string
			digitizeJobID         string
			digitizeJobStatus     string
			preBackupPowerVCResp  string
			preBackupSpyreGPUResp string
			opensearchBackupFile  string
			digitizeBackupFile    string
		)

		ginkgo.It("backs up and restores application data", func() {
			ctx, cancel := withTimeout(60 * time.Minute)
			defer cancel()

			catalogLoginWithDiscovery(ctx, true)

			infoOutput, err := cli.WaitForApplicationInfoURLs(ctx, cfg, appName, appRuntime, 8*time.Minute, 15*time.Second)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ragBaseURL, err = cli.GetBaseURL(infoOutput, backendPort)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			digitizeBaseURL := cli.ExtractDigitizeURL(infoOutput)
			gomega.Expect(digitizeBaseURL).NotTo(gomega.BeEmpty())

			pdfPath := digitization.GetTestPDFPath()
			gomega.Expect(pdfPath).NotTo(gomega.BeEmpty())

			// Clear any stale documents from a previous failed run before starting.
			// Without this, CreateJob returns 409 RESOURCE_LOCKED for test_doc.pdf
			// when it was already processed in a prior run.
			ginkgo.By("clearing any stale documents from previous test runs")
			if err := digitization.DeleteAllDocuments(ctx, digitizeBaseURL); err != nil {
				logger.Warningf("[BACKUP-RESTORE] pre-test document cleanup failed (non-fatal): %v", err)
			}

			// Run digitization FIRST so test_doc.pdf is written to the digitize DB.
			// Ingestion runs second to populate OpenSearch for RAG queries.
			// This order avoids a 409 RESOURCE_LOCKED: the file is not yet known to
			// the system when digitization runs, so there is no conflict.
			ginkgo.By("creating a digitization job to populate the digitize database")
			jobResp, err := digitization.CreateJob(ctx, digitizeBaseURL, pdfPath, "digitization", "json", "e2e-backup-restore-digitization")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			finalStatus, err := digitization.WaitForJobCompletion(ctx, digitizeBaseURL, jobResp.JobID, 10*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(finalStatus.Status).To(gomega.Equal("completed"))
			gomega.Expect(finalStatus.Documents).NotTo(gomega.BeEmpty())

			digitizeJobID = finalStatus.JobID
			digitizeJobStatus = finalStatus.Status
			digitizeDocID = finalStatus.Documents[0].ID

			doc, err := digitization.GetDocument(ctx, digitizeBaseURL, digitizeDocID)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			digitizeDocName = doc.Name
			digitizeDocStatus = doc.Status

			// Ingest test_doc.pdf AFTER digitization so OpenSearch is populated for
			// the RAG queries below. Digitization has already recorded the file, so
			// ingestion re-processes it without conflict (different operation type).
			ginkgo.By("ingesting test document to populate OpenSearch for RAG queries")
			gomega.Expect(digitization.IngestTestDocumentViaDigitizeAPI(ctx, digitizeBaseURL, "e2e-backup-restore-ingestion")).To(gomega.Succeed())

			ragPrompts := []struct {
				question string
				response *string
			}{
				{question: "What is PowerVC?", response: &preBackupPowerVCResp},
				{question: "How is a spyre card different from a GPU?", response: &preBackupSpyreGPUResp},
			}
			for _, prompt := range ragPrompts {
				logger.Infof("[TEST] Pre-backup RAG prompt: %s", prompt.question)
				response, askErr := rag.AskRAG(ctx, ragBaseURL, prompt.question)
				gomega.Expect(askErr).NotTo(gomega.HaveOccurred())
				gomega.Expect(strings.TrimSpace(response)).NotTo(gomega.BeEmpty())
				logger.Infof("[TEST] Pre-backup RAG response for %q: %s", prompt.question, response)
				*prompt.response = response
			}

			opensearchBackupFile = filepath.Join(tempDir, "opensearch-backup-"+runID+".tar.gz")
			digitizeBackupFile = filepath.Join(tempDir, "digitize-backup-"+runID+".tar.gz")

			_, err = cli.ApplicationBackup(ctx, cfg, appName, "opensearch", opensearchBackupFile, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			_, err = cli.ApplicationBackup(ctx, cfg, appName, "digitize", digitizeBackupFile, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			deleteOutput, deleteErr := cli.DeleteApp(ctx, cfg, appName, appRuntime)
			gomega.Expect(deleteErr).NotTo(gomega.HaveOccurred())
			gomega.Expect(deleteOutput).NotTo(gomega.BeEmpty())

			// Recreate target app after backup using the original name when the suite
			// owns the lifecycle. For caller-provided apps, restore into a fresh
			// sibling name so OpenShift cleanup lag does not block recreation of the
			// exact same application name immediately after delete.
			backupAppName = appName
			if providedAppName != "" {
				backupAppName = appName + "-restore-" + runID
			}

			createOutput, err := cli.CreateRAGAppAndValidate(
				ctx,
				cfg,
				backupAppName,
				templateName,
				createParams,
				backendPort,
				uiPort,
				cli.CreateOptions{
					SkipModelDownload: false,
					ImagePullPolicy:   "IfNotPresent",
				},
				[]string{"backend", "ui", "db"},
				appRuntime,
			)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(createOutput).NotTo(gomega.BeEmpty())

			catalogLoginWithDiscovery(ctx, true)

			_, err = cli.ApplicationRestore(ctx, cfg, backupAppName, "opensearch", opensearchBackupFile, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			_, err = cli.ApplicationRestore(ctx, cfg, backupAppName, "digitize", digitizeBackupFile, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			restoredInfoOutput, err := cli.WaitForApplicationInfoURLs(ctx, cfg, backupAppName, appRuntime, 8*time.Minute, 15*time.Second)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			restoredDigitizeBaseURL := cli.ExtractDigitizeURL(restoredInfoOutput)
			gomega.Expect(restoredDigitizeBaseURL).NotTo(gomega.BeEmpty())

			restoredJobs, err := digitization.ListJobs(ctx, restoredDigitizeBaseURL, false, 20, 0, digitizeJobStatus, "digitization")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(restoredJobs.Data).NotTo(gomega.BeEmpty())
			jobFound := false
			for _, job := range restoredJobs.Data {
				if job.JobID == digitizeJobID {
					jobFound = true
					gomega.Expect(job.Status).To(gomega.Equal(digitizeJobStatus))
					break
				}
			}
			gomega.Expect(jobFound).To(gomega.BeTrue(), "restored digitize job %s not found", digitizeJobID)

			restoredDocs, err := digitization.ListDocuments(ctx, restoredDigitizeBaseURL, 20, 0, "", digitizeDocName)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(restoredDocs.Data).NotTo(gomega.BeEmpty())
			gomega.Expect(restoredDocs.Data[0].Name).To(gomega.Equal(digitizeDocName))
			gomega.Expect(restoredDocs.Data[0].Status).To(gomega.Equal(digitizeDocStatus))

			restoredRAGBaseURL, err := cli.GetBaseURL(restoredInfoOutput, backendPort)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			for _, prompt := range ragPrompts {
				logger.Infof("[TEST] Post-restore RAG prompt: %s", prompt.question)
				restoredResponse, askErr := rag.AskRAG(ctx, restoredRAGBaseURL, prompt.question)
				gomega.Expect(askErr).NotTo(gomega.HaveOccurred())
				gomega.Expect(strings.TrimSpace(restoredResponse)).NotTo(gomega.BeEmpty())
				logger.Infof("[TEST] Post-restore RAG response for %q: %s", prompt.question, restoredResponse)
				gomega.Expect(restoredResponse).To(gomega.Equal(*prompt.response))
			}
		})

	})

	ginkgo.Context("OpenShift Application Backup And Restore",
		ginkgo.Ordered,
		ginkgo.Label("openshift-backup-restore", "app-backup-restore"),
		func() {
			// ------------------------------------------------------------------ //
			// State shared across the It block.
			// ------------------------------------------------------------------ //
			var (
				osDigitizeDocID         string
				osDigitizeDocName       string
				osDigitizeDocStatus     string
				osDigitizeJobID         string
				osDigitizeJobStatus     string
				osPreBackupPowerVCResp  string
				osPreBackupSpyreGPUResp string
				osOpensearchBackupFile  string
				osDigitizeBackupFile    string
			)

			// ------------------------------------------------------------------ //
			// TC-BR-OCP-1: mirror of the podman flow for OpenShift.
			//
			// Flow:
			//   1. Ingest doc + create digitization job on the existing app
			//   2. Capture pre-backup RAG responses
			//   3. Back up opensearch + digitize
			//   4. Delete the existing app
			//   5. Create a fresh app (sibling name when --app-name provided,
			//      same name when the suite owns the lifecycle)
			//   6. Re-login to catalog
			//   7. Restore opensearch + digitize into the new app
			//   8. Verify jobs, documents, and RAG responses match pre-backup
			// ------------------------------------------------------------------ //
			ginkgo.It("backs up and restores application data on OpenShift",
				ginkgo.SpecTimeout(90*time.Minute),
				func(specCtx context.Context) {
					// Guard: OpenShift only.
					if appRuntime != "openshift" {
						ginkgo.Skip(fmt.Sprintf(
							"[OPENSHIFT-BR] Skipping — runtime is %q, not \"openshift\"", appRuntime,
						))
					}
					// Guard: require a running app.
					if providedAppName == "" {
						ginkgo.Skip(
							"[OPENSHIFT-BR] Skipping — --app-name not provided; " +
								"pass --app-name=<app> to target a running application",
						)
					}

					catalogLoginWithDiscovery(specCtx, true)

					// ── Step 1: Resolve URLs from the running app ─────────────────
					ginkgo.By("resolving URLs from the running application")
					infoOutput, err := cli.WaitForApplicationInfoURLs(specCtx, cfg, appName, appRuntime, 8*time.Minute, 15*time.Second)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					// On OpenShift the route hostnames are "backend-<app>..." and
					// "digitize-api-<app>...". Use the OpenShift-aware extractors so
					// we don't accidentally pick the wrong service URL.
					osRAGBaseURL := cli.ExtractOpenShiftBackendURL(infoOutput)
					gomega.Expect(osRAGBaseURL).NotTo(gomega.BeEmpty(),
						"[OPENSHIFT-BR] could not extract RAG backend URL from application info")

					osDigitizeURL := cli.ExtractDigitizeURL(infoOutput)
					gomega.Expect(osDigitizeURL).NotTo(gomega.BeEmpty(),
						"[OPENSHIFT-BR] could not extract digitize URL from application info")

					logger.Infof("[OPENSHIFT-BR] RAG URL: %s  Digitize URL: %s", osRAGBaseURL, osDigitizeURL)

					// ── Step 2: Clear any stale documents from previous runs ───────
					ginkgo.By("clearing any stale documents from previous test runs")
					if err := digitization.DeleteAllDocuments(specCtx, osDigitizeURL); err != nil {
						logger.Warningf("[OPENSHIFT-BR] pre-test document cleanup failed (non-fatal): %v", err)
					}

					// ── Step 3: Run digitization FIRST to populate the digitize DB ─
					// Ingestion runs after so OpenSearch is populated for RAG queries.
					// This order avoids a 409 RESOURCE_LOCKED: the file is not yet
					// known to the system when digitization runs, so there is no conflict.
					ginkgo.By("creating a digitization job to populate the digitize database")
					osPDFPath := digitization.GetTestPDFPath()
					gomega.Expect(osPDFPath).NotTo(gomega.BeEmpty())

					jobResp, err := digitization.CreateJob(specCtx, osDigitizeURL, osPDFPath, "digitization", "json", "e2e-os-br-digitize")
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					finalStatus, err := digitization.WaitForJobCompletion(specCtx, osDigitizeURL, jobResp.JobID, 10*time.Minute)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(finalStatus.Status).To(gomega.Equal("completed"))
					gomega.Expect(finalStatus.Documents).NotTo(gomega.BeEmpty())

					osDigitizeJobID = finalStatus.JobID
					osDigitizeJobStatus = finalStatus.Status
					osDigitizeDocID = finalStatus.Documents[0].ID

					osDoc, docErr := digitization.GetDocument(specCtx, osDigitizeURL, osDigitizeDocID)
					gomega.Expect(docErr).NotTo(gomega.HaveOccurred())
					osDigitizeDocName = osDoc.Name
					osDigitizeDocStatus = osDoc.Status
					logger.Infof("[OPENSHIFT-BR] Seeded digitize job=%s doc=%s", osDigitizeJobID, osDigitizeDocName)

					// ── Step 4: Ingest test document to populate OpenSearch ────────
					// Runs after digitization so test_doc.pdf is already in the digitize
					// DB. Ingestion re-processes the file (different operation type) and
					// indexes it into OpenSearch without receiving a 409.
					ginkgo.By("ingesting test document to populate OpenSearch for RAG queries")
					gomega.Expect(
						digitization.IngestTestDocumentViaDigitizeAPI(specCtx, osDigitizeURL, "e2e-os-br-ingest"),
					).To(gomega.Succeed())
					logger.Infof("[OPENSHIFT-BR] Ingestion completed — OpenSearch populated")

					// ── Step 5: Capture pre-backup RAG responses ──────────────────
					ginkgo.By("capturing pre-backup RAG responses for known prompts")
					osRAGPrompts := []struct {
						question string
						response *string
					}{
						{question: "What is PowerVC?", response: &osPreBackupPowerVCResp},
						{question: "How is a spyre card different from a GPU?", response: &osPreBackupSpyreGPUResp},
					}
					for _, p := range osRAGPrompts {
						logger.Infof("[OPENSHIFT-BR] Pre-backup RAG prompt: %s", p.question)
						resp, askErr := rag.AskRAG(specCtx, osRAGBaseURL, p.question)
						gomega.Expect(askErr).NotTo(gomega.HaveOccurred())
						gomega.Expect(strings.TrimSpace(resp)).NotTo(gomega.BeEmpty())
						*p.response = resp
						logger.Infof("[OPENSHIFT-BR] Pre-backup response for %q: %s", p.question, resp)
					}

					// ── Step 6: Back up opensearch and digitize ───────────────────
					ginkgo.By("backing up opensearch and digitize data")
					osOpensearchBackupFile = filepath.Join(tempDir, "os-opensearch-backup-"+runID+".tar.gz")
					osDigitizeBackupFile = filepath.Join(tempDir, "os-digitize-backup-"+runID+".tar.gz")

					_, err = cli.ApplicationBackup(specCtx, cfg, appName, "opensearch", osOpensearchBackupFile, appRuntime)
					gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[OPENSHIFT-BR] opensearch backup failed")
					logger.Infof("[OPENSHIFT-BR] OpenSearch backup written to %s", osOpensearchBackupFile)

					_, err = cli.ApplicationBackup(specCtx, cfg, appName, "digitize", osDigitizeBackupFile, appRuntime)
					gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[OPENSHIFT-BR] digitize backup failed")
					logger.Infof("[OPENSHIFT-BR] Digitize backup written to %s", osDigitizeBackupFile)

					// ── Step 7: Delete the existing app ───────────────────────────
					ginkgo.By("deleting the existing application")
					deleteOutput, deleteErr := cli.DeleteApp(specCtx, cfg, appName, appRuntime)
					gomega.Expect(deleteErr).NotTo(gomega.HaveOccurred())
					gomega.Expect(deleteOutput).NotTo(gomega.BeEmpty())
					logger.Infof("[OPENSHIFT-BR] Application %s deleted", appName)

					// ── Step 8: Create a fresh app to restore into (legacy create) ─
					// Use the sibling name when --app-name was provided so OpenShift
					// namespace cleanup lag does not block immediate reuse of the name.
					// The app is created via the plain 'application create' command
					// (no URL-probing) — mirroring the legacy CLI flow.
					ginkgo.By("creating a fresh application via legacy create to restore into")
					osRestoreAppName := appName
					if providedAppName != "" {
						osRestoreAppName = appName + "-restore-" + runID
					}

					createOutput, createErr := cli.CreateApp(
						specCtx,
						cfg,
						osRestoreAppName,
						templateName,
						createParams,
						cli.CreateOptions{
							SkipModelDownload: false,
							ImagePullPolicy:   "IfNotPresent",
						},
						appRuntime,
					)
					gomega.Expect(createErr).NotTo(gomega.HaveOccurred())
					gomega.Expect(createOutput).NotTo(gomega.BeEmpty())
					logger.Infof("[OPENSHIFT-BR] Fresh application %s created", osRestoreAppName)

					// ── Step 9: Re-login then restore ─────────────────────────────
					ginkgo.By("re-logging into catalog and restoring opensearch and digitize")
					catalogLoginWithDiscovery(specCtx, true)

					_, err = cli.ApplicationRestore(specCtx, cfg, osRestoreAppName, "opensearch", osOpensearchBackupFile, appRuntime)
					gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[OPENSHIFT-BR] opensearch restore failed")
					logger.Infof("[OPENSHIFT-BR] OpenSearch restore completed")

					_, err = cli.ApplicationRestore(specCtx, cfg, osRestoreAppName, "digitize", osDigitizeBackupFile, appRuntime)
					gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[OPENSHIFT-BR] digitize restore failed")
					logger.Infof("[OPENSHIFT-BR] Digitize restore completed")

					// ── Step 10: Resolve URLs from the restored app ────────────────
					ginkgo.By("waiting for application info URLs on the restored app")
					restoredInfoOutput, infoErr := cli.WaitForApplicationInfoURLs(specCtx, cfg, osRestoreAppName, appRuntime, 8*time.Minute, 15*time.Second)
					gomega.Expect(infoErr).NotTo(gomega.HaveOccurred())

					restoredDigitizeURL := cli.ExtractDigitizeURL(restoredInfoOutput)
					gomega.Expect(restoredDigitizeURL).NotTo(gomega.BeEmpty(),
						"[OPENSHIFT-BR] could not extract digitize URL from restored app info")

					restoredRAGBaseURL := cli.ExtractOpenShiftBackendURL(restoredInfoOutput)
					gomega.Expect(restoredRAGBaseURL).NotTo(gomega.BeEmpty(),
						"[OPENSHIFT-BR] could not extract RAG backend URL from restored app info")

					// ── Step 11: Verify digitize jobs were restored ────────────────
					ginkgo.By("verifying digitize jobs were restored")
					restoredJobs, err := digitization.ListJobs(specCtx, restoredDigitizeURL, false, 20, 0, osDigitizeJobStatus, "digitization")
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(restoredJobs.Data).NotTo(gomega.BeEmpty(),
						"[OPENSHIFT-BR] no digitize jobs found after restore")

					jobFound := false
					for _, job := range restoredJobs.Data {
						if job.JobID == osDigitizeJobID {
							jobFound = true
							gomega.Expect(job.Status).To(gomega.Equal(osDigitizeJobStatus))
							break
						}
					}
					gomega.Expect(jobFound).To(gomega.BeTrue(),
						"[OPENSHIFT-BR] restored digitize job %s not found", osDigitizeJobID)
					logger.Infof("[OPENSHIFT-BR] Digitize job %s found after restore", osDigitizeJobID)

					// ── Step 12: Verify digitize documents were restored ───────────
					ginkgo.By("verifying digitize documents were restored")
					restoredDocs, err := digitization.ListDocuments(specCtx, restoredDigitizeURL, 20, 0, "", osDigitizeDocName)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					gomega.Expect(restoredDocs.Data).NotTo(gomega.BeEmpty(),
						"[OPENSHIFT-BR] no digitize documents found after restore")
					gomega.Expect(restoredDocs.Data[0].Name).To(gomega.Equal(osDigitizeDocName))
					gomega.Expect(restoredDocs.Data[0].Status).To(gomega.Equal(osDigitizeDocStatus))
					logger.Infof("[OPENSHIFT-BR] Digitize document %s restored successfully", osDigitizeDocName)

					// ── Step 13: Verify RAG responses match pre-backup ────────────
					ginkgo.By("verifying RAG responses match pre-backup responses")
					for _, p := range osRAGPrompts {
						logger.Infof("[OPENSHIFT-BR] Post-restore RAG prompt: %s", p.question)
						restoredResp, askErr := rag.AskRAG(specCtx, restoredRAGBaseURL, p.question)
						gomega.Expect(askErr).NotTo(gomega.HaveOccurred())
						gomega.Expect(strings.TrimSpace(restoredResp)).NotTo(gomega.BeEmpty())
						logger.Infof("[OPENSHIFT-BR] Post-restore response for %q: %s", p.question, restoredResp)
						gomega.Expect(restoredResp).To(gomega.Equal(*p.response),
							"[OPENSHIFT-BR] RAG response for %q changed after restore", p.question)
					}

					logger.Infof("[OPENSHIFT-BR] ✓ Backup and restore completed successfully (app=%s)", osRestoreAppName)
				})
		})

	ginkgo.Context("Application Teardown", ginkgo.Ordered, func() {
		ginkgo.It("deletes the application", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			if providedAppName != "" {
				// The app was provided by the caller — do not delete it so the
				// caller can inspect or reuse it after the run.
				ginkgo.Skip("Skipping application deletion — --app-name was provided, not managing lifecycle")
			}

			ctx, cancel := withTimeout(15 * time.Minute)
			defer cancel()

			output, err := cli.DeleteAppSkipCleanup(ctx, cfg, appName, appRuntime)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(output).NotTo(gomega.BeEmpty())

			logger.Infof("[TEST] Application %s deleted successfully", appName)
		})
		ginkgo.It("logs out from the catalog after application delete", ginkgo.Label("spyre-dependent", "summarization-tests"), func() {
			if appRuntime != "podman" {
				ginkgo.Skip("catalog logout only supported for podman runtime")
			}

			ctx, cancel := withTimeout(1 * time.Minute)
			defer cancel()

			output, err := cli.CatalogLogout(ctx, cfg, appRuntime)
			if err != nil {
				// Non-fatal: expired token or catalog already down is acceptable here.
				logger.Warningf("[TEARDOWN] [WARNING] Catalog logout failed (non-fatal): %v\nOutput: %s", err, output)

				return
			}

			gomega.Expect(cli.ValidateCatalogLogoutOutput(output)).To(gomega.Succeed())
			logger.Infof("[TEARDOWN] Catalog logout successful")
		})
		ginkgo.It("uninstalls the catalog service", ginkgo.Label("spyre-dependent"), func() {
			if appRuntime != "podman" {
				ginkgo.Skip("catalog uninstall only supported for podman runtime")
			}

			ctx, cancel := withTimeout(10 * time.Minute)
			defer cancel()

			uninstallOutput, uninstallErr := cli.CatalogUninstall(ctx, cfg, appRuntime)
			if uninstallErr != nil {
				// Non-fatal: suite results are unaffected — catalog cleanup is best-effort.
				logger.Warningf("[TEARDOWN] [WARNING] Catalog uninstall failed (non-fatal): %v\nOutput: %s", uninstallErr, uninstallOutput)

				return
			}

			logger.Infof("[TEARDOWN] Catalog service uninstalled successfully")

			// Confirm catalog is gone via catalog info.
			infoCtx, infoCancel := context.WithTimeout(ctx, 30*time.Second)
			defer infoCancel()
			infoOutput, infoErr := cli.CatalogInfo(infoCtx, cfg, appRuntime)
			if infoErr == nil && strings.Contains(infoOutput, "Catalog Backend API is available at") {
				logger.Warningf("[TEARDOWN] [WARNING] Catalog still appears to be running after uninstall — output: %s", infoOutput)
			} else {
				logger.Infof("[TEARDOWN] Catalog service confirmed not running after uninstall")
			}
		})
	})
})
