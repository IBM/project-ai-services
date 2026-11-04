package e2e

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/cli"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
	"github.com/project-ai-services/ai-services/tests/e2e/translation"
)

const (
	translateShortTimeout = 30 * time.Second
	translateSyncTimeout  = 2 * time.Minute

	// oversizedUploadBytes exceeds the default MAX_UPLOAD_SIZE_MB (10 MB).
	oversizedUploadBytes = 11 * 1024 * 1024
)

// translateBaseURL is resolved from TRANSLATE_URL, from the existing application in BeforeSuite,
// or from 'application info' in the BeforeAll below.
var translateBaseURL string

// rawJobGetFn is the signature shared by the raw GET helpers for a single job resource.
type rawJobGetFn func(ctx context.Context, baseURL, jobID string) ([]byte, int, error)

// ── helpers ───────────────────────────────────────────────────────────────────

// waitForTranslateURL polls 'application info' until the translate-api URL is present
// or the timeout fires (which fails the spec). Returns the URL.
func waitForTranslateURL(logPrefix string, timeout time.Duration) string {
	pollCtx, pollCancel := context.WithTimeout(context.Background(), timeout)
	defer pollCancel()

	const pollInterval = 15 * time.Second

	for {
		infoOut, infoErr := cli.ApplicationInfo(pollCtx, cfg, appName, appRuntime)
		if infoErr != nil {
			logger.Warningf("[%s] application info error while polling for translate URL: %v", logPrefix, infoErr)
		} else if url := cli.ExtractCatalogTranslateURL(infoOut); url != "" {
			return url
		}

		logger.Infof("[%s] translate-api URL not yet present - retrying in %s", logPrefix, pollInterval)

		select {
		case <-pollCtx.Done():
			ginkgo.Fail(fmt.Sprintf("[%s] Timed out waiting for translate-api URL for app %q", logPrefix, appName))
		case <-time.After(pollInterval):
		}
	}
}

// waitForTranslateHealthy polls /health until the service reports healthy
// or the timeout fires (which fails the spec).
func waitForTranslateHealthy(logPrefix, baseURL string, timeout time.Duration) {
	healthCtx, healthCancel := context.WithTimeout(context.Background(), timeout)
	defer healthCancel()

	const pollInterval = 15 * time.Second

	logger.Infof("[%s] Waiting for translate-api to be healthy at %s/health", logPrefix, baseURL)

	for {
		err := translation.HealthCheck(healthCtx, baseURL)
		if err == nil {
			logger.Infof("[%s] translate-api is healthy", logPrefix)

			return
		}

		logger.Infof("[%s] translate-api not yet healthy (%v) - retrying in %s", logPrefix, err, pollInterval)

		select {
		case <-healthCtx.Done():
			ginkgo.Fail(fmt.Sprintf("[%s] Timed out waiting for translate-api at %s - last error: %v", logPrefix, baseURL, err))
		case <-time.After(pollInterval):
		}
	}
}

// translateCtx skips the spec when no translate URL is known, otherwise returns a context limited to d.
func translateCtx(d time.Duration) (context.Context, context.CancelFunc) {
	if translateBaseURL == "" {
		ginkgo.Skip("translateBaseURL not set — skipping translation spec")
	}

	return context.WithTimeout(context.Background(), d)
}

// expectTranslation translates text synchronously and validates the response body:
//   - the translation is non-empty, differs from the input and still contains keepToken
//     (a value that must survive translation unchanged, such as a year);
//   - target_language echoes the request, and source_language is the language the text is in
//     (wantSource, lower-case) - when the request left it to auto-detect, the service may also
//     report "auto" if detection confidence was too low;
//   - token usage is reported and consistent.
func expectTranslation(text, sourceLang, targetLang, wantSource, keepToken, label string) {
	ctx, cancel := translateCtx(translateSyncTimeout)
	defer cancel()

	resp, err := translation.TranslateText(ctx, translateBaseURL, text, sourceLang, targetLang)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	gomega.Expect(resp.Translation()).NotTo(gomega.BeEmpty())
	gomega.Expect(resp.Translation()).NotTo(gomega.Equal(text), "translation should differ from the input")
	gomega.Expect(resp.Translation()).To(gomega.ContainSubstring(keepToken))

	gomega.Expect(resp.Data.TargetLanguage).To(gomega.Equal(strings.ToLower(targetLang)))

	if requested := strings.ToLower(sourceLang); requested == "" || requested == "auto" {
		gomega.Expect(resp.Data.SourceLanguage).To(gomega.BeElementOf(wantSource, "auto"))
	} else {
		gomega.Expect(resp.Data.SourceLanguage).To(gomega.Equal(wantSource))
	}

	gomega.Expect(resp.Usage.InputTokens).To(gomega.BeNumerically(">", 0))
	gomega.Expect(resp.Usage.OutputTokens).To(gomega.BeNumerically(">", 0))
	gomega.Expect(resp.Usage.TotalTokens).To(gomega.Equal(resp.Usage.InputTokens + resp.Usage.OutputTokens))

	logger.Infof("[TRANSLATE] %s: %s", label, resp.Translation())
}

// expectSyncError posts to POST /v1/translate and asserts wantStatus and, when wantMsg is set,
// that the lower-cased error message contains it.
func expectSyncError(text, sourceLang, targetLang string, wantStatus int, wantMsg string) {
	ctx, cancel := translateCtx(translateShortTimeout)
	defer cancel()

	errResp, statusCode, err := translation.TranslateTextExpectingError(ctx, translateBaseURL, text, sourceLang, targetLang)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(statusCode).To(gomega.Equal(wantStatus))
	gomega.Expect(errResp).NotTo(gomega.BeNil())

	if wantMsg != "" {
		gomega.Expect(strings.ToLower(errResp.Error.Message)).To(gomega.ContainSubstring(wantMsg))
	}

	logger.Infof("[TRANSLATE] sync error %d: %s", statusCode, errResp.Error.Message)
}

// expectRawSyncStatus posts a raw body to POST /v1/translate and asserts wantStatus with a non-empty response.
func expectRawSyncStatus(rawBody string, wantStatus int) {
	ctx, cancel := translateCtx(translateShortTimeout)
	defer cancel()

	respBody, statusCode, err := translation.TranslateRawBody(ctx, translateBaseURL,
		bytes.NewBufferString(rawBody), "application/json")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(statusCode).To(gomega.Equal(wantStatus))
	gomega.Expect(string(respBody)).NotTo(gomega.BeEmpty())
	logger.Infof("[TRANSLATE] raw sync response %d: %s", statusCode, string(respBody))
}

// expectJobAccepted submits an async job and asserts 202 with a job_id.
func expectJobAccepted(filePath, sourceLang, targetLang, jobName string) {
	ctx, cancel := translateCtx(translateShortTimeout)
	defer cancel()

	jobResp, err := translation.CreateJob(ctx, translateBaseURL, filePath, sourceLang, targetLang, jobName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(jobResp.JobID).NotTo(gomega.BeEmpty())
	logger.Infof("[TRANSLATE] job submitted: %s", jobResp.JobID)
}

// expectJobError submits an async job and asserts wantStatus and, when wantMsg is set,
// that the lower-cased error message contains it.
func expectJobError(filePath, sourceLang, targetLang string, wantStatus int, wantMsg string) {
	ctx, cancel := translateCtx(translateShortTimeout)
	defer cancel()

	errResp, statusCode, err := translation.CreateJobExpectingError(ctx, translateBaseURL, filePath, sourceLang, targetLang, "")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(statusCode).To(gomega.Equal(wantStatus))
	gomega.Expect(errResp).NotTo(gomega.BeNil())

	if wantMsg != "" {
		gomega.Expect(strings.ToLower(errResp.Error.Message)).To(gomega.ContainSubstring(wantMsg))
	}

	logger.Infof("[TRANSLATE] job rejected %d: %s", statusCode, errResp.Error.Message)
}

// expectNotFound calls get with a random, well-formed job UUID that does not exist and
// asserts 404 with a "not found" message.
func expectNotFound(get rawJobGetFn) {
	ctx, cancel := translateCtx(translateShortTimeout)
	defer cancel()

	jobID := uuid.NewString()
	respBody, statusCode, _ := get(ctx, translateBaseURL, jobID)
	gomega.Expect(statusCode).To(gomega.Equal(http.StatusNotFound))
	gomega.Expect(strings.ToLower(string(respBody))).To(gomega.ContainSubstring("not found"))
	logger.Infof("[TRANSLATE] unknown job_id %q returned 404", jobID)
}

// expectRunningOrDone submits a job and immediately calls get, which must return
// 202 (still running) or 200 (completed very fast).
func expectRunningOrDone(get rawJobGetFn) {
	ctx, cancel := translateCtx(translateShortTimeout)
	defer cancel()

	jobResp, err := translation.CreateJob(ctx, translateBaseURL, translation.GetGermanTXTPath(), "German", "English", "")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	_, statusCode, _ := get(ctx, translateBaseURL, jobResp.JobID)
	gomega.Expect(statusCode).To(gomega.BeElementOf(http.StatusAccepted, http.StatusOK))
	logger.Infof("[TRANSLATE] immediate check on job %s: status=%d", jobResp.JobID, statusCode)
}

// ── Translation test suite ────────────────────────────────────────────────────

// translationSpecs holds the translation specs. It is registered as a Context inside the
// ordered "AI Services End-to-End Tests" Describe (see e2e_suite_test.go) so the specs run
// after application creation and before teardown.
func translationSpecs() {
	// ── BeforeAll: resolve translateBaseURL ───────────────────────────────────
	ginkgo.BeforeAll(func() {
		if templateName != "translate" {
			ginkgo.Skip(fmt.Sprintf("Skipping translation tests - template is '%s', not 'translate'", templateName))
		}

		// TRANSLATE_URL lets CI point at a service that runs independently of the CLI deploy flow.
		if url := os.Getenv("TRANSLATE_URL"); url != "" {
			translateBaseURL = url
			logger.Infof("[TRANSLATE] Using TRANSLATE_URL from env: %s", translateBaseURL)

			return
		}

		// Refresh the catalog session before any CLI call (token TTL may have elapsed).
		loginCtx, loginCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		catalogLoginWithDiscovery(loginCtx, true)
		loginCancel()

		if translateBaseURL == "" {
			translateBaseURL = waitForTranslateURL("TRANSLATE", 10*time.Minute)
		}
		logger.Infof("[TRANSLATE] Using translateBaseURL: %s", translateBaseURL)
		waitForTranslateHealthy("TRANSLATE", translateBaseURL, 15*time.Minute)
	})

	// ═══════════════════════════════════════════════════════════════════════════
	// 1. Health Check
	// ═══════════════════════════════════════════════════════════════════════════
	ginkgo.Context("Health Check", func() {
		ginkgo.It("service health check returns 200 OK with status ok",
			ginkgo.Label("translate", "health"), func() {
				ctx, cancel := translateCtx(translateShortTimeout)
				defer cancel()

				gomega.Expect(translation.HealthCheck(ctx, translateBaseURL)).To(gomega.Succeed())
			})
	})

	// ═══════════════════════════════════════════════════════════════════════════
	// 2. Synchronous Translation — Happy Path
	// ═══════════════════════════════════════════════════════════════════════════
	ginkgo.Context("Synchronous Translation - Happy Path", func() {
		const (
			germanText  = "Der Vertrag tritt am 1. Januar 2025 in Kraft."
			englishText = "The contract comes into force on 1 January 2025."
		)

		ginkgo.It("explicit source language translates German to English",
			ginkgo.Label("translate", "sync", "happy"), func() {
				expectTranslation(germanText, "German", "English", "german", "2025", "explicit source")
			})

		ginkgo.It("omitted source language defaults to auto-detect (German → English)",
			ginkgo.Label("translate", "sync", "happy"), func() {
				expectTranslation(germanText, "", "English", "german", "2025", "auto source")
			})

		ginkgo.It(`explicit source_language "auto" translates German to English`,
			ginkgo.Label("translate", "sync", "happy"), func() {
				expectTranslation(germanText, "auto", "English", "german", "2025", "source=auto")
			})

		ginkgo.It("translate English to German",
			ginkgo.Label("translate", "sync", "happy"), func() {
				expectTranslation(englishText, "English", "German", "english", "2025", "EN→DE")
			})

		ginkgo.It("language names are case-insensitive (ENGLISH → GERMAN)",
			ginkgo.Label("translate", "sync", "happy"), func() {
				expectTranslation(englishText, "ENGLISH", "GERMAN", "english", "2025", "case-insensitive")
			})
	})

	// ═══════════════════════════════════════════════════════════════════════════
	// 3. Synchronous Translation — Validation Errors
	// ═══════════════════════════════════════════════════════════════════════════
	ginkgo.Context("Synchronous Translation - Validation Errors", func() {
		ginkgo.It("empty text field returns 400",
			ginkgo.Label("translate", "sync", "validation"), func() {
				expectSyncError("", "", "English", http.StatusBadRequest, "text")
			})

		ginkgo.It("missing target_language returns 422",
			ginkgo.Label("translate", "sync", "validation"), func() {
				expectRawSyncStatus(`{"text":"How are you?"}`, http.StatusUnprocessableEntity)
			})

		ginkgo.It("unsupported source language returns 400",
			ginkgo.Label("translate", "sync", "validation"), func() {
				expectSyncError("Qapla'!", "Klingon", "string", http.StatusBadRequest, "")
			})

		ginkgo.It(`"auto" as target_language returns 400`,
			ginkgo.Label("translate", "sync", "validation"), func() {
				expectSyncError("How are you?", "", "auto", http.StatusBadRequest, "auto")
			})

		ginkgo.It("same source and target language returns 400",
			ginkgo.Label("translate", "sync", "validation"), func() {
				expectSyncError("How are you?", "English", "English", http.StatusBadRequest, "differ")
			})
	})

	// ═══════════════════════════════════════════════════════════════════════════
	// 4. Synchronous Translation — JSON Error
	// ═══════════════════════════════════════════════════════════════════════════
	ginkgo.Context("Synchronous Translation - JSON Error", func() {
		ginkgo.It("invalid JSON syntax returns 422",
			ginkgo.Label("translate", "sync", "json-error"), func() {
				expectRawSyncStatus(
					`{"text": "How are you?", 0 "source_language": "auto", "target_language": "German"}`,
					http.StatusUnprocessableEntity)
			})
	})

	// ═══════════════════════════════════════════════════════════════════════════
	// 5. Async Job Submission
	// ═══════════════════════════════════════════════════════════════════════════
	ginkgo.Context("Async Job Submission", func() {
		ginkgo.It("submit a .txt file job returns 202 with job_id",
			ginkgo.Label("translate", "async", "submit"), func() {
				expectJobAccepted(translation.GetGermanTXTPath(), "German", "English", "")
			})

		ginkgo.It("submit a .md file job returns 202 with job_id",
			ginkgo.Label("translate", "async", "submit"), func() {
				expectJobAccepted(translation.GetTestMDPath(), "", "German", "")
			})

		ginkgo.It("submit with optional job_name returns 202 with job_id",
			ginkgo.Label("translate", "async", "submit"), func() {
				expectJobAccepted(translation.GetTestMDPath(), "", "English", "test-job-name")
			})

		ginkgo.It("reject unsupported file type .pdf returns 415",
			ginkgo.Label("translate", "async", "submit", "validation"), func() {
				expectJobError(translation.GetPDFPath(), "", "English", http.StatusUnsupportedMediaType, ".pdf")
			})

		ginkgo.It("reject unsupported file type .docx returns 415",
			ginkgo.Label("translate", "async", "submit", "validation"), func() {
				expectJobError(translation.GetDOCXPath(), "", "German", http.StatusUnsupportedMediaType, ".docx")
			})

		ginkgo.It("reject file exceeding MAX_UPLOAD_SIZE_MB returns 413",
			ginkgo.Label("translate", "async", "submit", "validation"), func() {
				largeFile, err := common.CreateTempFile("translate-large-*.txt", strings.Repeat("a", oversizedUploadBytes))
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				defer func() { _ = os.Remove(largeFile) }()

				expectJobError(largeFile, "", "English", http.StatusRequestEntityTooLarge, "10 mb")
			})

		ginkgo.It("unsupported target_language on async job returns 400",
			ginkgo.Label("translate", "async", "submit", "validation"), func() {
				expectJobError(translation.GetTestMDPath(), "English", "string", http.StatusBadRequest, "")
			})
	})

	// ═══════════════════════════════════════════════════════════════════════════
	// 6. Async Job Lifecycle — Submission and Detail Lookup
	// ═══════════════════════════════════════════════════════════════════════════
	ginkgo.Context("Async Job Lifecycle - Submission and Detail Lookup", func() {
		ginkgo.It("submitted job detail is immediately accessible via GET /jobs/{id}",
			ginkgo.Label("translate", "async", "lifecycle"), func() {
				ctx, cancel := translateCtx(translateShortTimeout)
				defer cancel()

				jobResp, err := translation.CreateJob(ctx, translateBaseURL,
					translation.GetGermanTXTPath(), "German", "English", "")
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				// Immediately fetch detail — job must exist with a known status.
				detail, err := translation.GetJobDetail(ctx, translateBaseURL, jobResp.JobID)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(detail.Status).To(gomega.BeElementOf(
					common.JobStatusAccepted,
					common.JobStatusInProgress,
					common.JobStatusCompleted,
				))
				gomega.Expect(detail.SubmittedAt).NotTo(gomega.BeEmpty())
				logger.Infof("[TRANSLATE] submitted job %s immediately visible, status=%s", jobResp.JobID, detail.Status)
			})

		ginkgo.It("job detail for unknown job_id returns 404",
			ginkgo.Label("translate", "async", "lifecycle"), func() {
				expectNotFound(translation.GetJobDetailRaw)
			})
	})

	// ═══════════════════════════════════════════════════════════════════════════
	// 7. List Jobs
	// ═══════════════════════════════════════════════════════════════════════════
	ginkgo.Context("List Jobs", func() {
		ginkgo.It("list all jobs (default pagination) returns 200",
			ginkgo.Label("translate", "list-jobs"), func() {
				ctx, cancel := translateCtx(translateShortTimeout)
				defer cancel()

				list, err := translation.ListJobs(ctx, translateBaseURL, 20, 0, "") //nolint:mnd
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(list.Pagination.Total).To(gomega.BeNumerically(">=", 0))
				gomega.Expect(list.Pagination.Limit).To(gomega.Equal(20)) //nolint:mnd
				gomega.Expect(list.Pagination.Offset).To(gomega.Equal(0))
				gomega.Expect(list.Data).NotTo(gomega.BeNil())
				logger.Infof("[TRANSLATE] list jobs: total=%d", list.Pagination.Total)
			})

		ginkgo.It("filter by status=completed — all returned items have status=completed",
			ginkgo.Label("translate", "list-jobs"), func() {
				ctx, cancel := translateCtx(translateShortTimeout)
				defer cancel()

				list, err := translation.ListJobs(ctx, translateBaseURL, 20, 0, "completed") //nolint:mnd
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				for _, item := range list.Data {
					gomega.Expect(item.Status).To(gomega.Equal(common.JobStatusCompleted),
						"all items must have status=completed when filter is applied")
				}

				logger.Infof("[TRANSLATE] completed filter: %d items", len(list.Data))
			})

		ginkgo.It("pagination with custom limit=5 and offset=5",
			ginkgo.Label("translate", "list-jobs"), func() {
				ctx, cancel := translateCtx(translateShortTimeout)
				defer cancel()

				list, err := translation.ListJobs(ctx, translateBaseURL, 5, 5, "") //nolint:mnd
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(len(list.Data)).To(gomega.BeNumerically("<=", 5)) //nolint:mnd
				gomega.Expect(list.Pagination.Limit).To(gomega.Equal(5))        //nolint:mnd
				gomega.Expect(list.Pagination.Offset).To(gomega.Equal(5))       //nolint:mnd
				logger.Infof("[TRANSLATE] pagination: got %d items with limit=5 offset=5", len(list.Data))
			})

		ginkgo.It("invalid status filter returns 400",
			ginkgo.Label("translate", "list-jobs", "validation"), func() {
				ctx, cancel := translateCtx(translateShortTimeout)
				defer cancel()

				_, statusCode, _ := translation.ListJobsRaw(ctx, translateBaseURL, "bogus")
				gomega.Expect(statusCode).To(gomega.Equal(http.StatusBadRequest))
			})
	})

	// ═══════════════════════════════════════════════════════════════════════════
	// 8. Get Translation Result — Error Cases Only
	// ═══════════════════════════════════════════════════════════════════════════
	ginkgo.Context("Get Translation Result - Error Cases", func() {
		ginkgo.It("get result for in-progress job returns 202 or 200",
			ginkgo.Label("translate", "result"), func() {
				expectRunningOrDone(translation.GetJobResultRaw)
			})

		ginkgo.It("get result for unknown job_id returns 404",
			ginkgo.Label("translate", "result"), func() {
				expectNotFound(translation.GetJobResultRaw)
			})
	})

	// ═══════════════════════════════════════════════════════════════════════════
	// 9. Download Translated File — Error Cases Only
	// ═══════════════════════════════════════════════════════════════════════════
	ginkgo.Context("Download Translated File - Error Cases", func() {
		ginkgo.It("download for in-progress job returns 202 or 200",
			ginkgo.Label("translate", "download"), func() {
				expectRunningOrDone(translation.GetJobResultDownloadRaw)
			})

		ginkgo.It("download from unknown job_id returns 404",
			ginkgo.Label("translate", "download"), func() {
				expectNotFound(translation.GetJobResultDownloadRaw)
			})
	})
}
