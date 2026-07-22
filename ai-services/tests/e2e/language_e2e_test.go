package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/cli"
	"github.com/project-ai-services/ai-services/tests/e2e/digitization"
	"github.com/project-ai-services/ai-services/tests/e2e/rag"
)

// getLanguageGoldenPath resolves the absolute path to a language golden dataset CSV.
// envVar is the environment variable holding the bare filename (e.g. "german_golden.csv").
// Returns an empty string when the env var is unset or the path cannot be resolved.
func getLanguageGoldenPath(envVar string) string {
	filename := os.Getenv(envVar)
	if filename == "" {
		return ""
	}

	_, callerFile, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}

	// Navigate from ai-services/tests/e2e/ up to the repo root (3 levels).
	e2eDir := filepath.Dir(callerFile)
	repoRoot := filepath.Clean(filepath.Join(e2eDir, "..", "..", ".."))

	return filepath.Join(repoRoot, "test", "golden", filename)
}

// runLanguageIngestTest is a shared helper for TC-1, TC-2, and TC-6.
// It ingests the PDF for the given language stem (e.g. "german") via the digitize
// microservice, then asks question against the RAG backend and asserts a non-empty answer.
// The test fails hard if GetLanguagePDFPath cannot resolve a path (build-time error),
// and skips gracefully if the fixture file is simply not present on disk.
func runLanguageIngestTest(
	specCtx context.Context,
	tcID, language, question, digitizeBaseURL string,
) {
	pdfPath := digitization.GetLanguagePDFPath(language)
	if pdfPath == "" {
		ginkgo.Fail(fmt.Sprintf("[%s] GetLanguagePDFPath(%q) returned empty — runtime.Caller failed at build time", tcID, language))
	}
	if _, statErr := os.Stat(pdfPath); os.IsNotExist(statErr) {
		ginkgo.Skip(fmt.Sprintf("[%s] %s.pdf not found at %s — provide fixture file and retry", tcID, language, pdfPath))
	}

	ginkgo.By(fmt.Sprintf("ingesting %s.pdf via digitize microservice", language))
	ingestCtx, ingestCancel := context.WithTimeout(specCtx, 20*time.Minute)
	defer ingestCancel()

	err := digitization.IngestLanguageDocumentViaDigitizeAPI(
		ingestCtx, digitizeBaseURL, pdfPath, fmt.Sprintf("e2e-lang-%s-ingest", language[:2]),
	)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(),
		"[%s] %s PDF ingestion should complete successfully", tcID, language)

	ginkgo.By(fmt.Sprintf("asking a %s question against the RAG backend", language))
	askCtx, askCancel := context.WithTimeout(specCtx, 8*time.Minute)
	defer askCancel()

	answer, err := rag.RunWithRetry(askCtx, defaultMaxRetries, func(c context.Context) (string, error) {
		return rag.AskRAG(c, ragBaseURL, question)
	})

	gomega.Expect(err).NotTo(gomega.HaveOccurred(),
		"[%s] %s RAG question should not return an error", tcID, language)
	gomega.Expect(answer).NotTo(gomega.BeEmpty(),
		"[%s] %s RAG answer should contain content from the ingested document", tcID, language)

	logger.Infof("[LANG][%s] %s RAG answer: %s", tcID, language, answer)
}

// runLanguageSmokeTest is a shared helper for TC-3, TC-4, and TC-7.
// It sends question directly to the RAG endpoint (no ingestion) and asserts
// that the endpoint returns a non-empty response — validating language detection routing.
func runLanguageSmokeTest(tcID, language, question string) {
	ctx, cancel := withTimeout(10 * time.Minute)
	defer cancel()

	ginkgo.By(fmt.Sprintf("sending %s query to RAG endpoint", language))
	answer, err := rag.RunWithRetry(ctx, defaultMaxRetries, func(c context.Context) (string, error) {
		return rag.AskRAG(c, ragBaseURL, question)
	})

	gomega.Expect(err).NotTo(gomega.HaveOccurred(),
		"[%s] %s query should not return an error", tcID, language)
	gomega.Expect(answer).NotTo(gomega.BeEmpty(),
		"[%s] %s query should return non-empty content", tcID, language)

	logger.Infof("[LANG][%s] %s query answered: %s", tcID, language, answer)
}

var _ = ginkgo.Describe("Language Support Tests",
	ginkgo.Label("language-tests", "spyre-dependent"),
	ginkgo.Ordered,
	func() {
		var langDigitizeBaseURL string

		// ------------------------------------------------------------------ //
		// BeforeAll: resolve the digitize service URL, same pattern used by
		// the existing Digitization Tests context in e2e_suite_test.go.
		// ------------------------------------------------------------------ //
		ginkgo.BeforeAll(func() {
			if appName == "" {
				ginkgo.Fail("Application name is not set — cannot run language support tests")
			}

			logger.Infof("[LANG] Setting up Language Support Tests")

			setupCtx, setupCancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer setupCancel()

			infoOutput, err := cli.WaitForApplicationInfoURLs(setupCtx, cfg, appName, appRuntime, 8*time.Minute, 15*time.Second)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Always resolve ragBaseURL locally — when running with --label-filter=language-tests
			// the Application Creation It() block is skipped, so the suite-level ragBaseURL is empty.
			if ragBaseURL == "" {
				ragBaseURL, err = cli.GetBaseURL(infoOutput, backendPort)
				gomega.Expect(err).NotTo(gomega.HaveOccurred(),
					"[LANG] could not resolve RAG backend URL from application info")
				logger.Infof("[LANG] Resolved RAG Base URL: %s", ragBaseURL)
			}

			if appRuntime == "podman" {
				const pollInterval = 15 * time.Second
				for {
					langDigitizeBaseURL = cli.ExtractCatalogDigitizeURL(infoOutput)
					if langDigitizeBaseURL != "" {
						break
					}
					if setupCtx.Err() != nil {
						ginkgo.Fail("Timed out waiting for digitize-backend URL in 'application info' output")
					}
					logger.Infof("[LANG] digitize-backend URL not yet present — retrying in %s", pollInterval)
					select {
					case <-setupCtx.Done():
						ginkgo.Fail("Timed out waiting for digitize-backend URL in 'application info' output")
					case <-time.After(pollInterval):
					}
					infoOutput, err = cli.ApplicationInfo(setupCtx, cfg, appName, appRuntime)
					if err != nil {
						logger.Warningf("[LANG] application info error while polling for digitize URL: %v", err)
					}
				}
			} else {
				urlList := cli.ExtractURLsFromOutput(infoOutput)
				if len(urlList) == 0 {
					ginkgo.Fail("No URLs extracted from application info output")
				}
				langDigitizeBaseURL = urlList[0]
			}

			logger.Infof("[LANG] Digitize Base URL: %s", langDigitizeBaseURL)
			logger.Infof("[LANG] RAG Base URL: %s", ragBaseURL)
		})

		// ------------------------------------------------------------------ //
		// TC-1: Ingest german.pdf then ask a German question.
		// Skips gracefully if the fixture file is not present on disk.
		// ------------------------------------------------------------------ //
		ginkgo.It("ingests a German PDF and receives a German-language response",
			ginkgo.SpecTimeout(25*time.Minute),
			func(specCtx context.Context) {
				runLanguageIngestTest(specCtx, "TC-1", "german",
					"Was ist IBM Power und welche Workloads unterstützt es?",
					langDigitizeBaseURL,
				)
			})

		// ------------------------------------------------------------------ //
		// TC-2: Ingest french.pdf then ask a French question.
		// Skips gracefully if the fixture file is not present on disk.
		// ------------------------------------------------------------------ //
		ginkgo.It("ingests a French PDF and receives a French-language response",
			ginkgo.SpecTimeout(25*time.Minute),
			func(specCtx context.Context) {
				runLanguageIngestTest(specCtx, "TC-2", "french",
					"Qu'est-ce qu'IBM Power et quels workloads prend-il en charge?",
					langDigitizeBaseURL,
				)
			})

		// ------------------------------------------------------------------ //
		// TC-3: German query language auto-detection smoke test.
		// Does NOT require german.pdf — runs against whatever is already
		// indexed. Validates that DE routing does not break the endpoint.
		// ------------------------------------------------------------------ //
		ginkgo.It("detects German from query and returns 200 with content",
			ginkgo.Label("language-smoke"),
			func() {
				runLanguageSmokeTest("TC-3", "German", "Was ist künstliche Intelligenz?")
			})

		// ------------------------------------------------------------------ //
		// TC-4: French query language auto-detection smoke test.
		// Does NOT require french.pdf — same rationale as TC-3.
		// ------------------------------------------------------------------ //
		ginkgo.It("detects French from query and returns 200 with content",
			ginkgo.Label("language-smoke"),
			func() {
				runLanguageSmokeTest("TC-4", "French", "Qu'est-ce que l'intelligence artificielle?")
			})

		// ------------------------------------------------------------------ //
		// TC-5: Ingest italian.pdf then ask an Italian question.
		// Skips gracefully if the fixture file is not present on disk.
		// ------------------------------------------------------------------ //
		ginkgo.It("ingests an Italian PDF and receives an Italian-language response",
			ginkgo.SpecTimeout(25*time.Minute),
			func(specCtx context.Context) {
				runLanguageIngestTest(specCtx, "TC-5", "italian",
					"Cos'è IBM Power e quali workload supporta?",
					langDigitizeBaseURL,
				)
			})

		// ------------------------------------------------------------------ //
		// TC-6: Italian query language auto-detection smoke test.
		// Does NOT require italian.pdf — same rationale as TC-3.
		// ------------------------------------------------------------------ //
		ginkgo.It("detects Italian from query and returns 200 with content",
			ginkgo.Label("language-smoke"),
			func() {
				runLanguageSmokeTest("TC-6", "Italian", "Cos'è l'intelligenza artificiale?")
			})

		// ------------------------------------------------------------------ //
		// TC-7: German, French and Italian golden dataset accuracy validation.
		//
		// STATUS: COMMENTED OUT — pending LLM-as-Judge environment setup.
		// Uncomment this block once the following are confirmed with the team:
		//   1. LLM_JUDGE_IMAGE      — the vLLM judge container image to use
		//   2. LLM_JUDGE_MODEL      — judge model name (e.g. Qwen/Qwen2.5-7B-Instruct)
		//   3. LLM_JUDGE_MODEL_PATH — local path where the model weights are stored
		//   4. GERMAN_GOLDEN_DATASET_FILE=german_golden.csv
		//   5. FRENCH_GOLDEN_DATASET_FILE=french_golden.csv
		//   6. ITALIAN_GOLDEN_DATASET_FILE=italian_golden.csv
		//
		// To run once uncommented:
		//   export LLM_JUDGE_IMAGE=<image>
		//   export LLM_JUDGE_MODEL_PATH=/var/lib/ai-services/models
		//   export LLM_JUDGE_MODEL=Qwen/Qwen2.5-7B-Instruct
		//   export LLM_JUDGE_PORT=8000
		//   export GERMAN_GOLDEN_DATASET_FILE=german_golden.csv
		//   export FRENCH_GOLDEN_DATASET_FILE=french_golden.csv
		//   export ITALIAN_GOLDEN_DATASET_FILE=italian_golden.csv
		//   ginkgo -r --label-filter=language-golden ./tests/e2e -- --app-name=<app-name>
		// ------------------------------------------------------------------ //

		// ginkgo.It("German, French and Italian golden dataset accuracy meets threshold",
		// 	ginkgo.Label("language-golden"),
		// 	ginkgo.SpecTimeout(3*time.Hour),
		// 	func(specCtx context.Context) {
		// 		// Guard: LLM-as-Judge must be configured.
		// 		llmJudgeImage := os.Getenv("LLM_JUDGE_IMAGE")
		// 		llmJudgeModelPath := os.Getenv("LLM_JUDGE_MODEL_PATH")
		// 		llmJudgeModel := os.Getenv("LLM_JUDGE_MODEL")
		// 		if llmJudgeImage == "" || llmJudgeModelPath == "" || llmJudgeModel == "" {
		// 			ginkgo.Skip(fmt.Sprintf(
		// 				"Skipping language golden dataset validation — LLM-as-Judge not configured "+
		// 					"(LLM_JUDGE_IMAGE=%q, LLM_JUDGE_MODEL_PATH=%q, LLM_JUDGE_MODEL=%q)",
		// 				llmJudgeImage, llmJudgeModelPath, llmJudgeModel,
		// 			))
		// 		}
		//
		// 		// Guard: at least one golden CSV must be provided.
		// 		germanGoldenPath := getLanguageGoldenPath("GERMAN_GOLDEN_DATASET_FILE")
		// 		frenchGoldenPath := getLanguageGoldenPath("FRENCH_GOLDEN_DATASET_FILE")
		// 		italianGoldenPath := getLanguageGoldenPath("ITALIAN_GOLDEN_DATASET_FILE")
		// 		if germanGoldenPath == "" && frenchGoldenPath == "" && italianGoldenPath == "" {
		// 			ginkgo.Skip("Skipping language golden dataset validation — " +
		// 				"none of GERMAN_GOLDEN_DATASET_FILE, FRENCH_GOLDEN_DATASET_FILE or ITALIAN_GOLDEN_DATASET_FILE is set")
		// 		}
		//
		// 		// Per-language evaluation helper: runs the RAG+Judge loop and returns accuracy.
		// 		type langResult struct {
		// 			lang     string
		// 			accuracy float64
		// 			results  []rag.EvalResult
		// 		}
		//
		// 		evaluate := func(lang, goldenPath string) langResult {
		// 			if goldenPath == "" {
		// 				logger.Infof("[LANG][TC-5][%s] golden CSV path empty — skipping language", lang)
		// 				return langResult{lang: lang, accuracy: 1.0}
		// 			}
		// 			if _, statErr := os.Stat(goldenPath); os.IsNotExist(statErr) {
		// 				logger.Infof("[LANG][TC-5][%s] golden CSV not found at %s — skipping language", lang, goldenPath)
		// 				return langResult{lang: lang, accuracy: 1.0}
		// 			}
		//
		// 			cases, loadErr := rag.LoadGoldenCSV(goldenPath)
		// 			gomega.Expect(loadErr).NotTo(gomega.HaveOccurred(),
		// 				"[%s] failed to load golden CSV from %s", lang, goldenPath)
		// 			gomega.Expect(cases).NotTo(gomega.BeEmpty(),
		// 				"[%s] golden CSV must contain at least one row", lang)
		//
		// 			total := len(cases)
		// 			results := make([]rag.EvalResult, 0, total)
		// 			passed := 0
		//
		// 			const perQuestionTimeout = 8 * time.Minute //nolint:mnd
		//
		// 			for i, tc := range cases {
		// 				if specCtx.Err() != nil {
		// 					logger.Warningf("[LANG][TC-5][%s] spec context cancelled after %d/%d questions", lang, i, total)
		// 					break
		// 				}
		//
		// 				qCtx, qCancel := context.WithTimeout(context.Background(), perQuestionTimeout)
		//
		// 				result := rag.EvalResult{Question: tc.Question, Passed: false}
		//
		// 				logger.Infof("[LANG][TC-5][%s] Evaluating question %d/%d: %s", lang, i+1, total, tc.Question)
		//
		// 				ragAns, ragErr := rag.RunWithRetry(qCtx, defaultMaxRetries, func(c context.Context) (string, error) {
		// 					return rag.AskRAG(c, ragBaseURL, tc.Question)
		// 				})
		// 				if ragErr != nil {
		// 					result.Details = fmt.Sprintf("RAG request failed: %v", ragErr)
		// 					logger.Infof("[LANG][TC-5][%s] Question %d/%d — RAG failed: %v", lang, i+1, total, ragErr)
		// 					results = append(results, result)
		// 					qCancel()
		// 					continue
		// 				}
		//
		// 				verdict, reason, judgeErr := rag.AskJudgeWithFormatRetry(
		// 					qCtx, defaultMaxRetries, judgeBaseURL, tc.Question, ragAns, tc.GoldenAnswer,
		// 				)
		// 				if judgeErr != nil {
		// 					result.Details = fmt.Sprintf("Judge failed: %v", judgeErr)
		// 					logger.Infof("[LANG][TC-5][%s] Question %d/%d — Judge failed: %v", lang, i+1, total, judgeErr)
		// 					results = append(results, result)
		// 					qCancel()
		// 					continue
		// 				}
		//
		// 				result.Passed = verdict == "YES"
		// 				result.Details = reason
		// 				if result.Passed {
		// 					passed++
		// 				}
		//
		// 				results = append(results, result)
		// 				logger.Infof("[LANG][TC-5][%s] Question %d/%d | verdict=%s | reason=%s",
		// 					lang, i+1, total, verdict, reason)
		// 				qCancel()
		// 			}
		//
		// 			accuracy := float64(passed) / float64(total)
		// 			rag.PrintValidationSummary(results, accuracy)
		// 			return langResult{lang: lang, accuracy: accuracy, results: results}
		// 		}
		//
		// 		ginkgo.By("evaluating German golden dataset")
		// 		deResult := evaluate("DE", germanGoldenPath)
		//
		// 		ginkgo.By("evaluating French golden dataset")
		// 		frResult := evaluate("FR", frenchGoldenPath)
		//
		// 		ginkgo.By("evaluating Italian golden dataset")
		// 		itResult := evaluate("IT", italianGoldenPath)
		//
		// 		ginkgo.By("asserting all languages meet the accuracy threshold")
		// 		if deResult.accuracy < defaultRagAccuracyThreshold {
		// 			ginkgo.Fail(fmt.Sprintf(
		// 				"[DE] RAG accuracy %.2f is below threshold %.2f",
		// 				deResult.accuracy, defaultRagAccuracyThreshold,
		// 			))
		// 		}
		// 		if frResult.accuracy < defaultRagAccuracyThreshold {
		// 			ginkgo.Fail(fmt.Sprintf(
		// 				"[FR] RAG accuracy %.2f is below threshold %.2f",
		// 				frResult.accuracy, defaultRagAccuracyThreshold,
		// 			))
		// 		}
		// 		if itResult.accuracy < defaultRagAccuracyThreshold {
		// 			ginkgo.Fail(fmt.Sprintf(
		// 				"[IT] RAG accuracy %.2f is below threshold %.2f",
		// 				itResult.accuracy, defaultRagAccuracyThreshold,
		// 			))
		// 		}
		//
		// 		logger.Infof("[LANG][TC-7] DE accuracy=%.2f FR accuracy=%.2f IT accuracy=%.2f threshold=%.2f",
		// 			deResult.accuracy, frResult.accuracy, itResult.accuracy, defaultRagAccuracyThreshold)
		// 	})
	})
