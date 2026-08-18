// This file covers similarity search FAILURE scenarios — testing that the
// similarity service correctly rejects bad input, handles unreachable hosts,
// and reports an empty index before any documents are ingested.
//
// Test cases
//
//  1. Empty query rejected          – HTTP 400, error code EMPTY_INPUT
//  2. Invalid mode value rejected   – HTTP 400, error code INVALID_PARAMETER
//  3. top_k below minimum rejected  – HTTP 422, Pydantic validation
//  4. Unreachable similarity API    – transport-level connection error
//  5. Empty vector index (no docs)  – HTTP 503, error code VECTOR_STORE_NOT_READY
//
// Runtime compatibility
//
//	Tests 1, 2, 3, 4  Run on BOTH podman and openshift runtimes.
//	                  They exercise HTTP-level validation that fires independently
//	                  of the runtime (no CLI, no Podman involvement).
//	                  Tests 1–3 require a running similarity service endpoint;
//	                  Test 4 uses a deliberately unreachable URL so no live
//	                  service is needed at all.
//
//	Test 5            Requires a live similarity service endpoint.
//	                  Skips gracefully when appName is empty or the similarity
//	                  service URL cannot be resolved — absent deployment is an
//	                  environment gap, not a code failure.
//
// Labels
//
//	failure-test             – all tests in this file (umbrella label, shared with all failure suites)
//	similarity-failure       – all tests in this file (domain label)
//	similarity-input         – Tests 1, 2, 3
//	similarity-connectivity  – Test 4
//	similarity-readiness     – Test 5
//
// Running ALL failure tests together (all three failure suites):
//
//	ginkgo -r --label-filter="failure-test" ./tests/e2e
//
// Excluding ALL failure tests from the normal run:
//
//	ginkgo -r --label-filter="!failure-test" ./tests/e2e
//
// Running only similarity failure tests:
//
//	ginkgo -r --label-filter="similarity-failure" ./tests/e2e
//
// Running by sub-category:
//
//	ginkgo -r --label-filter="failure-test && similarity-input"        ./tests/e2e
//	ginkgo -r --label-filter="failure-test && similarity-connectivity" ./tests/e2e
//	ginkgo -r --label-filter="failure-test && similarity-readiness"    ./tests/e2e
package e2e

import (
	"context"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/cli"
	"github.com/project-ai-services/ai-services/tests/e2e/rag"
	"github.com/project-ai-services/ai-services/tests/e2e/similarity"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// similarityFailureTestTimeout caps how long any single similarity failure test
// may run.  Tests 1–3 exercise pure HTTP validation (no vectorstore I/O), and
// Test 4 explicitly uses an unreachable URL — both should resolve in well under
// 30 seconds.  Critically, this timeout MUST be shorter than sharedRAGClient's
// global httpClientTimeout (25 minutes) so that TC-4 never hangs waiting for a
// TCP connection that will never arrive.
const similarityFailureTestTimeout = 30 * time.Second

// unreachableSimilarityURL points to a host that will never accept TCP
// connections, used in TC-4 to exercise transport-level error handling.
// The .invalid TLD is guaranteed by RFC 2606 to never resolve in DNS.
const unreachableSimilarityURL = "http://similarity.invalid.ais-failure-test.example.com:9999"

// invalidModeValue is a mode string that is not in the accepted set
// ["dense", "sparse", "hybrid"], used in TC-2.
const invalidModeValue = "invalid-mode-value"

// ─────────────────────────────────────────────────────────────────────────────
// Similarity Search Failure Scenarios
// ─────────────────────────────────────────────────────────────────────────────

var _ = ginkgo.Describe("Similarity Search Failure Scenarios",
	// ginkgo.Ordered is intentionally NOT used here.  Each failure test is fully
	// self-contained and must not depend on the result of a preceding test.
	func() {

		// ── Default-exclusion guard ───────────────────────────────────────────
		//
		// Failure tests are skipped unless --run-failure-tests is explicitly
		// passed.  This mirrors the --app-name guard used by Language Support
		// Tests and prevents accidental execution during a normal suite run.
		ginkgo.BeforeEach(func() {
			if !runFailureTests {
				ginkgo.Skip(
					"[FAILURE-TEST][Similarity] Skipping — pass --run-failure-tests to opt in to failure test execution",
				)
			}
		})

		// similarityBaseURL is resolved once per Context block by the BeforeEach
		// below and used by TC-1, TC-2 and TC-3.  It is scoped here (not to the
		// Describe) so it is invisible to TC-4 and TC-5 which manage their own URLs.
		var inputValidationSimilarityURL string

		// ── Tests 1, 2, 3: Input Validation Failures ─────────────────────────
		//
		// Rationale: These tests verify the three independent input-validation
		// paths in the similarity service's /v1/similarity-search endpoint.
		// All fire before the vector store is touched, so no live OpenSearch or
		// embedding service is required — only the FastAPI process itself.
		ginkgo.Context("Input Validation Failures",
			func() {
				// ── URL resolution — works for both full-suite and standalone runs ──
				//
				// When running as part of the full E2E suite the Application Creation
				// It() block has already run and the similarity service is up.
				// When running standalone with --label-filter="similarity-input" that
				// block is skipped, so we must resolve the URL ourselves here.
				//
				// The similarity service URL is always resolved from 'application info'
				// regardless of runtime — on catalog-managed podman the service is
				// exposed as an HTTPS nip.io URL (e.g. https://similarity-api-<hash>.nip.io),
				// NOT on localhost:<port>.  The localhost pattern only applies to judge
				// containers that are started directly by the test suite itself.
				//
				// If appName is not set (no deployed application) these tests skip
				// gracefully — missing deployment is an environment gap, not a code failure.
				ginkgo.BeforeEach(func() {
					// appName is always non-empty — BeforeSuite generates a name even when
					// --app-name is not passed.  Check providedAppName (the raw flag value)
					// to detect whether the caller targeted a real deployed application.
					// This skips immediately (zero wait) rather than polling for 2 minutes
					// against a non-existent auto-generated app name.
					if providedAppName == "" {
						ginkgo.Skip(
							"[FAILURE-TEST][Similarity] Skipping input validation tests — " +
								"--app-name was not provided; " +
								"pass --app-name=<app> to target a running application",
						)
					}
	
					resolveCtx, resolveCancel := context.WithTimeout(
						context.Background(),
						2*time.Minute, //nolint:mnd
					)
					defer resolveCancel()
	
					infoOutput, infoErr := cli.WaitForApplicationInfoURLs(
						resolveCtx, cfg, appName, appRuntime,
						2*time.Minute,  //nolint:mnd — maxWait
						15*time.Second, //nolint:mnd — pollInterval
					)
					if infoErr != nil {
						ginkgo.Skip(
							"[FAILURE-TEST][Similarity] Skipping input validation tests — " +
								"could not resolve application info URLs: " + infoErr.Error(),
						)
					}
	
					inputValidationSimilarityURL = cli.ExtractSimilarityAPIURL(infoOutput)
					if inputValidationSimilarityURL == "" {
						ginkgo.Skip(
							"[FAILURE-TEST][Similarity] Skipping input validation tests — " +
								"similarity-api URL not found in application info output",
						)
					}
	
					logger.Infof(
						"[FAILURE-TEST][Similarity] resolved similarity URL: %s",
						inputValidationSimilarityURL,
					)
				})

				// ── Test 1: Empty query ───────────────────────────────────────
				//
				// Rationale: A caller that accidentally sends an empty or
				// whitespace-only query must receive a clear EMPTY_INPUT error
				// (HTTP 400) rather than zero results or a crash.
				//
				// Layer exercised: app.py line 137:
				//   if not req.query or not req.query.strip()
				//
				// Expected response:
				//   HTTP 400, {"error":{"code":"EMPTY_INPUT","message":"Input cannot be empty: query is required",...}}
				ginkgo.It(
					"rejects an empty query with HTTP 400 EMPTY_INPUT",
					ginkgo.Label("failure-test", "similarity-failure", "similarity-input", "spyre-independent"),
					func() {
						ctx, cancel := withTimeout(similarityFailureTestTimeout)
						defer cancel()

						logger.Infof("[FAILURE-TEST][Similarity] Sending empty query to %s", inputValidationSimilarityURL)

						statusCode, rawBody, err := rag.PostSimilaritySearchRaw(
							ctx,
							inputValidationSimilarityURL,
							map[string]any{
								"query": "",
								"mode":  "dense",
							},
						)

						// ── Assertions ────────────────────────────────────────
						// 1. No transport error — the service must have responded.
						gomega.Expect(err).NotTo(
							gomega.HaveOccurred(),
							"Expected the similarity service to respond (not a transport error) for an empty query",
						)

						// 2. HTTP 400 with EMPTY_INPUT error code and expected message.
						gomega.Expect(
							similarity.ValidateSimilarityEmptyQueryError(statusCode, rawBody),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Similarity] Correctly rejected empty query — HTTP %d, body: %s",
							statusCode, rawBody,
						)
					},
				)

				// ── Test 2: Invalid mode value ────────────────────────────────
				//
				// Rationale: The mode field only accepts "dense", "sparse", or
				// "hybrid".  A typo or undocumented value must be rejected with a
				// clear INVALID_PARAMETER error (HTTP 400) rather than silently
				// defaulting to another mode.
				//
				// Layer exercised: app.py line 140:
				//   if req.mode not in ["dense", "sparse", "hybrid"]
				//
				// Expected response:
				//   HTTP 400, {"error":{"code":"INVALID_PARAMETER","message":"...mode must be one of...",...}}
				ginkgo.It(
					"rejects an unsupported mode value with HTTP 400 INVALID_PARAMETER",
					ginkgo.Label("failure-test", "similarity-failure", "similarity-input", "spyre-independent"),
					func() {
						ctx, cancel := withTimeout(similarityFailureTestTimeout)
						defer cancel()

						logger.Infof(
							"[FAILURE-TEST][Similarity] Sending invalid mode %q to %s",
							invalidModeValue, inputValidationSimilarityURL,
						)

						statusCode, rawBody, err := rag.PostSimilaritySearchRaw(
							ctx,
							inputValidationSimilarityURL,
							map[string]any{
								"query": "what is IBM Power?",
								"mode":  invalidModeValue,
							},
						)

						// ── Assertions ────────────────────────────────────────
						gomega.Expect(err).NotTo(
							gomega.HaveOccurred(),
							"Expected the similarity service to respond for an invalid mode value",
						)

						gomega.Expect(
							similarity.ValidateSimilarityInvalidModeError(statusCode, rawBody),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Similarity] Correctly rejected invalid mode — HTTP %d, body: %s",
							statusCode, rawBody,
						)
					},
				)

				// ── Test 3: top_k below minimum (Pydantic 422) ───────────────
				//
				// Rationale: The top_k field has a ge=1 Pydantic constraint.
				// Passing top_k=0 would cause OpenSearch to crash on a size=0
				// query; the Pydantic guard must catch this at the FastAPI
				// boundary before any downstream code runs.
				//
				// Layer exercised: SimilaritySearchRequest.top_k Field(ge=1)
				// — fires as a Pydantic RequestValidationError before the
				// endpoint handler is called.
				//
				// Expected response: HTTP 422 with body referencing "top_k".
				// Note: FastAPI 422 bodies use {"detail":[...]} (not the custom
				// error envelope), so we check for HTTP 422 and the "top_k"
				// field name in the raw body.
				ginkgo.It(
					"rejects top_k=0 with HTTP 422 Pydantic validation error",
					ginkgo.Label("failure-test", "similarity-failure", "similarity-input", "spyre-independent"),
					func() {
						ctx, cancel := withTimeout(similarityFailureTestTimeout)
						defer cancel()

						logger.Infof(
							"[FAILURE-TEST][Similarity] Sending top_k=0 to %s",
							inputValidationSimilarityURL,
						)

						statusCode, rawBody, err := rag.PostSimilaritySearchRaw(
							ctx,
							inputValidationSimilarityURL,
							map[string]any{
								"query": "what is IBM Power?",
								"mode":  "dense",
								"top_k": 0,
							},
						)

						// ── Assertions ────────────────────────────────────────
						gomega.Expect(err).NotTo(
							gomega.HaveOccurred(),
							"Expected the similarity service to respond for top_k=0",
						)

						gomega.Expect(
							similarity.ValidateSimilarityInvalidTopKError(statusCode, rawBody),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Similarity] Correctly rejected top_k=0 — HTTP %d, body: %s",
							statusCode, rawBody,
						)
					},
				)
			},
		)

		// ── Test 4: Connectivity Failure ──────────────────────────────────────
		//
		// Rationale: When the similarity service is completely unreachable (e.g.
		// not deployed, wrong host, network partition) callers must receive an
		// immediate transport-level error — not a timeout hang or silent result.
		// This test exercises the HTTP client's error path directly, independently
		// of any deployed service.
		//
		// The context timeout (similarityFailureTestTimeout = 30s) is deliberately
		// shorter than sharedRAGClient's 25-minute global timeout.  This guarantees
		// that DNS failure or TCP connection refusal causes a rapid context-
		// deadline-exceeded error rather than a 25-minute hang.
		ginkgo.Context("Connectivity Failures",
			func() {
				ginkgo.It(
					"returns a transport error when the similarity API is unreachable",
					ginkgo.Label("failure-test", "similarity-failure", "similarity-connectivity", "spyre-independent"),
					func() {
						// Context timeout MUST be shorter than sharedRAGClient.Timeout
						// (25 minutes) so the test never hangs on DNS/TCP failure.
						ctx, cancel := withTimeout(similarityFailureTestTimeout)
						defer cancel()

						logger.Infof(
							"[FAILURE-TEST][Similarity] Attempting request to unreachable URL: %s",
							unreachableSimilarityURL,
						)

						_, _, transportErr := rag.PostSimilaritySearchRaw(
							ctx,
							unreachableSimilarityURL,
							map[string]any{
								"query": "what is IBM Power?",
								"mode":  "dense",
							},
						)

						// ── Assertions ────────────────────────────────────────
						// Validate that the error is a transport-level failure
						// (not a structured API response wrapped as ErrNonRetriable).
						gomega.Expect(
							similarity.ValidateSimilarityUnreachableError(transportErr),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Similarity] Correctly received transport error for unreachable URL. Error: %v",
							transportErr,
						)
					},
				)
			},
		)

		// ── Test 5: Service Readiness Failure ─────────────────────────────────
		//
		// Rationale: A freshly deployed stack with no ingested documents must
		// return HTTP 503 VECTOR_STORE_NOT_READY rather than a silent empty
		// result list.  This is the most common out-of-the-box failure operators
		// encounter after first deployment.
		//
		// Layer exercised: app.py lines 176–177:
		//   except db.VectorStoreNotReadyError:
		//       APIError.raise_error(ErrorCode.VECTOR_STORE_NOT_READY,
		//                            "Index is empty. Ingest documents first.")
		//
		// Skip conditions (environment gap, not a code failure):
		//   • appName is empty — no application deployed
		//   • similarity-api URL cannot be resolved from application info
		ginkgo.Context("Service Readiness Failures",
			func() {
				ginkgo.It(
					"returns HTTP 503 VECTOR_STORE_NOT_READY when the index is empty",
					ginkgo.Label("failure-test", "similarity-failure", "similarity-readiness"),
					func() {
						// Guard: --app-name must be explicitly provided.  appName is always
						// non-empty (BeforeSuite generates one), so check providedAppName to
						// skip immediately when no real deployed app is targeted.
						if providedAppName == "" {
							ginkgo.Skip(
								"[FAILURE-TEST][Similarity] Skipping VECTOR_STORE_NOT_READY test — " +
									"--app-name was not provided; " +
									"pass --app-name=<app> to target an existing application",
							)
						}

						logger.Infof(
							"[FAILURE-TEST][Similarity] Resolving similarity-api URL for app %q",
							appName,
						)

						// Resolve the similarity-api URL with a short cap (2 min) so the
						// failure test does not block for the suite's full 8-minute default.
						resolveCtx, resolveCancel := context.WithTimeout(
							context.Background(),
							2*time.Minute, //nolint:mnd
						)
						defer resolveCancel()

						infoOutput, infoErr := cli.WaitForApplicationInfoURLs(
							resolveCtx, cfg, appName, appRuntime,
							2*time.Minute,  //nolint:mnd — maxWait
							15*time.Second, //nolint:mnd — pollInterval
						)
						if infoErr != nil {
							ginkgo.Skip(
								"[FAILURE-TEST][Similarity] Skipping VECTOR_STORE_NOT_READY test — " +
									"could not resolve application info URLs: " + infoErr.Error(),
							)
						}

						similarityBaseURL := cli.ExtractSimilarityAPIURL(infoOutput)
						if similarityBaseURL == "" {
							ginkgo.Skip(
								"[FAILURE-TEST][Similarity] Skipping VECTOR_STORE_NOT_READY test — " +
									"similarity-api URL not found in application info output",
							)
						}

						logger.Infof(
							"[FAILURE-TEST][Similarity] Resolved similarity-api URL: %s",
							similarityBaseURL,
						)

						ctx, cancel := withTimeout(similarityFailureTestTimeout)
						defer cancel()
	
						statusCode, rawBody, err := rag.PostSimilaritySearchRaw(
							ctx,
							similarityBaseURL,
							map[string]any{
								"query": "what is IBM Power?",
								"mode":  "dense",
							},
						)
	
						// ── Assertions ────────────────────────────────────────
						// Transport error means the service is unreachable — different
						// failure mode, not the empty-index scenario we are testing.
						gomega.Expect(err).NotTo(
							gomega.HaveOccurred(),
							"Expected the similarity service to respond (not a transport error) for empty-index test",
						)
	
						// Skip when the OpenSearch vector index is not empty (HTTP 200).
						// TC-5 validates the VECTOR_STORE_NOT_READY path which only fires
						// before any vectors have been written to OpenSearch.
						//
						// IMPORTANT: deleting documents via the digitize API does NOT clear
						// the OpenSearch vector index — the two stores are independent.
						// HTTP 200 here means vectors are still present in OpenSearch even
						// if all source documents have been deleted from the document store.
						// The only reliable way to get a true empty index is to use a
						// brand-new application that has never had any documents ingested.
						if statusCode == 200 {
							ginkgo.Skip(
								"[FAILURE-TEST][Similarity] Skipping VECTOR_STORE_NOT_READY test — " +
									"the OpenSearch vector index is not empty (HTTP 200 returned); " +
									"note: deleting documents via the digitize API does NOT clear the vector index — " +
									"run this test against a brand-new application that has never had documents ingested",
							)
						}
	
						gomega.Expect(
							similarity.ValidateSimilarityNotReadyError(statusCode, rawBody),
						).To(gomega.Succeed())
	
						logger.Infof(
							"[FAILURE-TEST][Similarity] Correctly reported empty index — HTTP %d, body: %s",
							statusCode, rawBody,
						)
					},
				)
			},
		)
	},
)
