package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/cli"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
	"github.com/project-ai-services/ai-services/tests/e2e/digitization"
	"github.com/project-ai-services/ai-services/tests/e2e/extraction"
	"github.com/project-ai-services/ai-services/tests/e2e/rag"
	"github.com/project-ai-services/ai-services/tests/e2e/summarization"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// Release sanity suite (label: "sanity-0.4").
//
// A quick, happy-path-only pass that validates a release / patch build. It is label driven like
// the rest of this suite:
//
//	make test-generate-report APP_RUNTIME=podman \
//	  TEST_ARGS='--label-filter=sanity-0.4 --timeout=4h'
//
// with PATCH_BRANCH=<release-branch> exported, the ai-services binary is built from that branch
// (see bootstrap.BuildOrVerifyCLIBinary) while the test code stays on this checkout.
// CREATE_PARAMS selects the RAG shape exactly as in the full suite:
//
//	4 Spyre cards: CREATE_PARAMS="reranker.vllm-cpu=true"      5 cards: unset (template default)
//
// Flow: bootstrap + catalog -> RAG app -> ps/info/pods -> ingest document + Q&A -> delete RAG app
// -> extract service -> digitize service -> summarize service -> catalog logout/uninstall.
// The lifecycle steps are the existing specs in e2e_suite_test.go that carry the same label.
const (
	sanityLabel = "sanity-0.4"

	sanityServiceDeployTimeout = 45 * time.Minute
	sanityServiceReadyTimeout  = 25 * time.Minute
	sanityServicePollInterval  = 15 * time.Second
	sanityJobTimeout           = 20 * time.Minute
	sanityDeleteTimeout        = 15 * time.Minute
)

// isSanityRun reports whether the suite was started with a label filter selecting the sanity label.
func isSanityRun() bool {
	return strings.Contains(ginkgo.GinkgoLabelFilter(), sanityLabel)
}

// sanityActive reports whether the sanity-only stages should actually run: the sanity label is
// selected on a suite-managed podman RAG deployment. The stages delete apps, so they stay inert in
// the full suite (where later contexts rely on the RAG app) and on any unsupported setup.
func sanityActive() bool {
	return isSanityRun() && appRuntime == "podman" && templateName == "rag" && providedAppName == ""
}

// skipUnlessSanityRun skips the calling context when the sanity stages should not run.
func skipUnlessSanityRun() {
	if sanityActive() {
		return
	}

	ginkgo.Skip(fmt.Sprintf("Skipping sanity-only context - needs --label-filter=%s, --runtime=podman, "+
		"--template=rag and no --app-name (labelFilter=%q runtime=%q template=%q appName provided=%t)",
		sanityLabel, ginkgo.GinkgoLabelFilter(), appRuntime, templateName, providedAppName != ""))
}

// defineSanityContexts registers the sanity-only stages inside the main ordered Describe, right
// before teardown, so they run in this order after the shared lifecycle specs.
func defineSanityContexts() {
	defineSanityRAGQnA()
	defineSanityExtract()
	defineSanityDigitize()
	defineSanitySummarize()
}

// sanityService describes how to locate and health-check a standalone service app.
type sanityService struct {
	template  string
	logPrefix string
	urlFrom   func(infoOutput string) string
	health    func(ctx context.Context, baseURL string) error
}

func sanityAppName(template string) string {
	return fmt.Sprintf("sanity-%s-%s", template, runID)
}

// deploySanityService creates a standalone service app and blocks until its API is healthy.
func deploySanityService(svc sanityService) (app, baseURL string) {
	app = sanityAppName(svc.template)

	ctx, cancel := withTimeout(sanityServiceDeployTimeout)
	defer cancel()

	// Refresh the catalog token - its TTL may have elapsed during earlier stages.
	catalogLoginWithDiscovery(ctx, true)

	logger.Infof("[%s] creating %s app %s", svc.logPrefix, svc.template, app)
	_, err := cli.CreateApp(ctx, cfg, app, svc.template, "", cli.CreateOptions{
		SkipModelDownload: false,
		ImagePullPolicy:   "IfNotPresent",
	}, appRuntime)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	baseURL = waitForSanityServiceReady(svc, app)
	logger.Infof("[%s] %s is healthy at %s", svc.logPrefix, svc.template, baseURL)

	return app, baseURL
}

// waitForSanityServiceReady polls 'application info' for the service URL, then its /health.
func waitForSanityServiceReady(svc sanityService, app string) string {
	pollCtx, pollCancel := context.WithTimeout(context.Background(), sanityServiceReadyTimeout)
	defer pollCancel()

	var (
		baseURL string
		lastErr error
	)

	for {
		if baseURL == "" {
			info, err := cli.ApplicationInfo(pollCtx, cfg, app, appRuntime)
			if err != nil {
				lastErr = err
			} else {
				baseURL = svc.urlFrom(info)
			}
		}

		if baseURL != "" {
			err := svc.health(pollCtx, baseURL)
			if err == nil {
				return baseURL
			}
			lastErr = err
		}

		select {
		case <-pollCtx.Done():
			ginkgo.Fail(fmt.Sprintf("[%s] timed out waiting for the %s service of app %q to become healthy (url=%q): %v",
				svc.logPrefix, svc.template, app, baseURL, lastErr))
		case <-time.After(sanityServicePollInterval):
		}
	}
}

// deleteSanityApp removes an app so its Spyre cards are free for the next stage. Best effort: a
// failed delete is logged rather than failing the spec, so the original failure stays visible.
func deleteSanityApp(logPrefix, app string) {
	if app == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), sanityDeleteTimeout)
	defer cancel()

	if _, err := cli.DeleteAppSkipCleanup(ctx, cfg, app, appRuntime); err != nil {
		logger.Warningf("[%s] deleting app %s failed (non-fatal): %v", logPrefix, app, err)

		return
	}

	logger.Infof("[%s] app %s deleted", logPrefix, app)
}

// defineSanityRAGQnA ingests a document into the RAG app created by the shared lifecycle specs
// and asks questions about it. Like Application Backup And Restore it uses fixed prompts against
// test_doc.pdf, but instead of comparing before/after-restore answers it asserts that each answer
// is non-empty and grounded in the document. The RAG app is deleted afterwards (even on failure)
// to free its Spyre cards for the service stages.
func defineSanityRAGQnA() {
	ginkgo.Context("Sanity RAG ingestion and Q&A", ginkgo.Ordered, ginkgo.Label("spyre-dependent", sanityLabel), func() {
		var digitizeBaseURL string

		ginkgo.BeforeAll(skipUnlessSanityRun)

		ginkgo.AfterAll(func() {
			if !sanityActive() {
				return
			}
			if digitizeBaseURL != "" {
				cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cleanCancel()
				if err := digitization.DeleteAllDocuments(cleanCtx, digitizeBaseURL); err != nil {
					logger.Warningf("[SANITY-RAG] document cleanup failed (non-fatal): %v", err)
				}
			}
			deleteSanityApp("SANITY-RAG", appName)
		})

		ginkgo.It("ingests the test document and answers questions from it", func() {
			ctx, cancel := withTimeout(75 * time.Minute)
			defer cancel()

			catalogLoginWithDiscovery(ctx, true)

			infoOutput, err := cli.WaitForApplicationInfoURLs(ctx, cfg, appName, appRuntime, 8*time.Minute, 15*time.Second)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ragBaseURL, err = cli.GetBaseURL(infoOutput, backendPort)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			digitizeBaseURL = cli.ExtractDigitizeURL(infoOutput)
			gomega.Expect(digitizeBaseURL).NotTo(gomega.BeEmpty())

			ginkgo.By("waiting for the LLM to be ready")
			llmCtx, llmCancel := context.WithTimeout(ctx, 40*time.Minute)
			defer llmCancel()
			gomega.Expect(rag.WaitForRAGBackendReady(llmCtx, ragBaseURL, 30*time.Second)).To(gomega.Succeed())

			ginkgo.By("ingesting the test document")
			if err := digitization.DeleteAllDocuments(ctx, digitizeBaseURL); err != nil {
				logger.Warningf("[SANITY-RAG] pre-ingest document cleanup failed (non-fatal): %v", err)
			}
			gomega.Expect(digitization.IngestTestDocumentViaDigitizeAPI(ctx, digitizeBaseURL, "sanity-ingestion-"+runID)).
				To(gomega.Succeed())

			// Same prompts as Application Backup And Restore.
			prompts := []struct{ question, keyword string }{
				{question: "What is PowerVC?", keyword: "powervc"},
				{question: "How is a spyre card different from a GPU?", keyword: "spyre"},
			}
			for _, prompt := range prompts {
				ginkgo.By("asking: " + prompt.question)
				response, askErr := rag.AskRAG(ctx, ragBaseURL, prompt.question)
				gomega.Expect(askErr).NotTo(gomega.HaveOccurred())
				gomega.Expect(strings.TrimSpace(response)).NotTo(gomega.BeEmpty())
				gomega.Expect(strings.ToLower(response)).To(gomega.ContainSubstring(prompt.keyword),
					"answer to %q does not mention %q", prompt.question, prompt.keyword)
				logger.Infof("[SANITY-RAG] Q: %s\n[SANITY-RAG] A: %s", prompt.question, response)
			}
		})
	})
}

// defineSanityExtract deploys the extract service and runs the happy-path sync + async extraction.
func defineSanityExtract() {
	ginkgo.Context("Sanity Extract service", ginkgo.Ordered, ginkgo.Label("spyre-dependent", sanityLabel), func() {
		var (
			svcApp  string
			baseURL string
			jobIDs  []string
		)

		requiredFields := []string{"sender_info", "shipping_date", "tracking_number", "items"}

		ginkgo.BeforeAll(func() {
			skipUnlessSanityRun()
			// Set before deploying so AfterAll can clean up a partially created app if deploy fails.
			svcApp = sanityAppName("extract")
			svcApp, baseURL = deploySanityService(sanityService{
				template:  "extract",
				logPrefix: "SANITY-EXTRACT",
				urlFrom:   cli.ExtractExtractURL,
				health:    extraction.HealthCheck,
			})
		})

		ginkgo.AfterAll(func() {
			if sanityActive() {
				deleteSanityApp("SANITY-EXTRACT", svcApp)
			}
		})

		ginkgo.AfterEach(func() {
			for _, id := range jobIDs {
				cleanCtx, cleanCancel := context.WithTimeout(context.Background(), time.Minute)
				_ = extraction.DeleteJob(cleanCtx, baseURL, id)
				cleanCancel()
			}
			jobIDs = nil
		})

		ginkgo.It("extracts the required shipping fields synchronously", func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			resp, err := extraction.Extract(ctx, baseURL, extraction.NewShippingUpdateRequest(extraction.ShippingUpdateExample().Text))
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(resp).NotTo(gomega.BeNil())

			missing, ok := common.AllFieldsPresent(resp.GetData(), requiredFields)
			gomega.Expect(ok).To(gomega.BeTrue(), "expected field %q in extraction response", missing)
			gomega.Expect(common.FieldHasValue(resp.GetData(), "sender_info")).To(gomega.BeTrue())

			items, isArray := resp.GetData()["items"].([]interface{})
			gomega.Expect(isArray).To(gomega.BeTrue(), "items must be an array")
			gomega.Expect(items).NotTo(gomega.BeEmpty())
		})

		ginkgo.It("completes an async extraction job with the required fields", func() {
			ctx, cancel := withTimeout(15 * time.Minute)
			defer cancel()

			detail, result, err := extraction.SubmitAndVerifyJob(ctx, baseURL,
				extraction.NewShippingUpdateRequest(extraction.FedExShippingText),
				fmt.Sprintf("sanity-extract-%d", time.Now().Unix()), sanityJobTimeout)
			if detail != nil {
				jobIDs = append(jobIDs, detail.JobID)
			}
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(detail.Status).To(gomega.Equal(common.JobStatusCompleted))

			missing, ok := common.AllFieldsPresent(result.GetData(), requiredFields)
			gomega.Expect(ok).To(gomega.BeTrue(), "expected field %q in async extraction result", missing)
		})
	})
}

// defineSanityDigitize deploys the digitize service and runs a happy-path digitization + ingestion.
func defineSanityDigitize() {
	ginkgo.Context("Sanity Digitize service", ginkgo.Ordered, ginkgo.Label("spyre-dependent", sanityLabel), func() {
		var (
			svcApp  string
			baseURL string
			pdfPath string
		)

		ginkgo.BeforeAll(func() {
			skipUnlessSanityRun()
			pdfPath = digitization.GetTestPDFPath()
			gomega.Expect(pdfPath).NotTo(gomega.BeEmpty(), "test PDF path could not be resolved")

			svcApp = sanityAppName("digitize")
			svcApp, baseURL = deploySanityService(sanityService{
				template:  "digitize",
				logPrefix: "SANITY-DIGITIZE",
				urlFrom:   cli.ExtractCatalogDigitizeURL,
				health:    digitization.HealthCheck,
			})
		})

		ginkgo.AfterAll(func() {
			if sanityActive() {
				deleteSanityApp("SANITY-DIGITIZE", svcApp)
			}
		})

		// The server answers 409 RESOURCE_LOCKED when a file already has a document, so wipe
		// documents between specs.
		ginkgo.AfterEach(func() {
			if baseURL == "" {
				return
			}
			cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cleanCancel()
			if err := digitization.DeleteAllDocuments(cleanCtx, baseURL); err != nil {
				logger.Warningf("[SANITY-DIGITIZE] document cleanup failed (non-fatal): %v", err)
			}
		})

		ginkgo.It("digitizes a PDF and returns its content", func() {
			ctx, cancel := withTimeout(15 * time.Minute)
			defer cancel()

			job, err := digitization.CreateJob(ctx, baseURL, pdfPath, "digitization", "json", "sanity-digitization")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(job.JobID).NotTo(gomega.BeEmpty())

			status, err := digitization.WaitForJobCompletion(ctx, baseURL, job.JobID, sanityJobTimeout)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(status.Status).To(gomega.Equal("completed"))
			gomega.Expect(status.Documents).NotTo(gomega.BeEmpty())

			docID := status.Documents[0].ID
			doc, err := digitization.GetDocument(ctx, baseURL, docID)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(doc.Status).To(gomega.Equal("completed"))

			content, err := digitization.GetDocumentContent(ctx, baseURL, docID)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			result, isMap := content.Result.(map[string]interface{})
			gomega.Expect(isMap).To(gomega.BeTrue(), "result should be a map for JSON output")
			gomega.Expect(result).NotTo(gomega.BeEmpty())
		})

		ginkgo.It("completes an ingestion job", func() {
			ctx, cancel := withTimeout(25 * time.Minute)
			defer cancel()

			gomega.Expect(digitization.IngestTestDocumentViaDigitizeAPI(ctx, baseURL, "sanity-ingestion-"+runID)).
				To(gomega.Succeed())
		})
	})
}

// defineSanitySummarize deploys the summarize service and runs happy-path sync + async summarization.
func defineSanitySummarize() {
	ginkgo.Context("Sanity Summarize service", ginkgo.Ordered, ginkgo.Label("spyre-dependent", sanityLabel), func() {
		const sampleText = "Artificial Intelligence (AI) is transforming industries worldwide. Machine learning " +
			"algorithms enable computers to learn from data and improve their performance over time. Deep learning, " +
			"a subset of machine learning, uses neural networks with multiple layers to process complex patterns."

		var (
			svcApp  string
			baseURL string
			jobIDs  []string
		)

		ginkgo.BeforeAll(func() {
			skipUnlessSanityRun()
			svcApp = sanityAppName("summarize")
			svcApp, baseURL = deploySanityService(sanityService{
				template:  "summarize",
				logPrefix: "SANITY-SUMMARIZE",
				urlFrom:   cli.ExtractCatalogSummarizeURL,
				health:    summarization.HealthCheck,
			})
		})

		ginkgo.AfterAll(func() {
			if sanityActive() {
				deleteSanityApp("SANITY-SUMMARIZE", svcApp)
			}
		})

		ginkgo.AfterEach(func() {
			for _, id := range jobIDs {
				cleanCtx, cleanCancel := context.WithTimeout(context.Background(), time.Minute)
				_ = summarization.DeleteJob(cleanCtx, baseURL, id)
				cleanCancel()
			}
			jobIDs = nil
		})

		ginkgo.It("summarizes text synchronously", func() {
			ctx, cancel := withTimeout(5 * time.Minute)
			defer cancel()

			resp, err := summarization.SummarizeText(ctx, baseURL, sampleText, "standard")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(strings.TrimSpace(resp.Summary())).NotTo(gomega.BeEmpty())
		})

		ginkgo.It("summarizes a PDF through an async job", func() {
			ctx, cancel := withTimeout(25 * time.Minute)
			defer cancel()

			res, err := summarization.SubmitAndVerifyJob(ctx, baseURL, testFilePath("ingestion/docs/test_doc.pdf"), "", "standard",
				fmt.Sprintf("sanity-pdf-%d", time.Now().Unix()), false, sanityJobTimeout)
			if res != nil && res.Detail != nil {
				jobIDs = append(jobIDs, res.Detail.JobID)
			}
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(strings.TrimSpace(res.Summary)).NotTo(gomega.BeEmpty())
		})
	})
}
