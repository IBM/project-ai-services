// This file covers digitize-service FAILURE scenarios — testing that the
// digitize microservice correctly rejects invalid input, refuses to operate on
// active resources, and enforces deduplication when a file is already ingested.
//
// Test cases
//
//  1. List documents with invalid status filter    – HTTP 400, INVALID_REQUEST
//  2. Re-submit an already-ingested file           – HTTP 409, RESOURCE_LOCKED
//  3. DELETE job in accepted state                 – HTTP 409, RESOURCE_LOCKED
//  4. DELETE job in in_progress state              – HTTP 409, RESOURCE_LOCKED
//  5. DELETE document from in_progress job         – HTTP 409, RESOURCE_LOCKED
//
// Runtime compatibility
//
//	All tests require a running digitize-backend endpoint reachable via the
//	application info URL.  Tests skip gracefully when --app-name is not
//	provided (environment gap, not a code failure).
//
// Labels
//
//	failure-test             – all tests in this file (umbrella label, shared with all failure suites)
//	digitize-failure         – all tests in this file (domain label)
//	digitize-input           – TC-1
//	digitize-deduplication   – TC-2
//	digitize-active-job      – TC-3, TC-4
//	digitize-active-doc      – TC-5
//
// Running ALL failure tests together (all failure suites):
//
//	ginkgo -r --label-filter="failure-test" ./tests/e2e
//
// Excluding ALL failure tests from the normal run:
//
//	ginkgo -r --label-filter="!failure-test" ./tests/e2e
//
// Running only digitize failure tests:
//
//	ginkgo -r --label-filter="digitize-failure" ./tests/e2e
//
// Running by sub-category:
//
//	ginkgo -r --label-filter="failure-test && digitize-input"          ./tests/e2e
//	ginkgo -r --label-filter="failure-test && digitize-deduplication"  ./tests/e2e
//	ginkgo -r --label-filter="failure-test && digitize-active-job"     ./tests/e2e
//	ginkgo -r --label-filter="failure-test && digitize-active-doc"     ./tests/e2e
package e2e

import (
	"context"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/cli"
	"github.com/project-ai-services/ai-services/tests/e2e/digitization"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// digitizeFailureTestTimeout caps how long any single digitize failure test may
// run.  Tests that require job ingestion (TC-2 through TC-5) need a longer
// budget to allow Spyre to start processing the PDF.
const digitizeFailureTestTimeout = 20 * time.Minute //nolint:mnd

// digitizeFailureIngestionWaitTimeout caps how long TC-2 waits for the first
// ingestion job to complete before attempting the duplicate submission.
const digitizeFailureIngestionWaitTimeout = 15 * time.Minute //nolint:mnd

// digitizeFailureInProgressPollInterval is how often TC-4 and TC-5 poll for
// the job's in_progress transition.
const digitizeFailureInProgressPollInterval = 5 * time.Second //nolint:mnd

// digitizeFailureInProgressTimeout caps how long we wait for a job to enter
// in_progress before considering the poll a failure.
const digitizeFailureInProgressTimeout = 10 * time.Minute //nolint:mnd

// ─────────────────────────────────────────────────────────────────────────────
// Digitize Service Failure Scenarios
// ─────────────────────────────────────────────────────────────────────────────

var _ = ginkgo.Describe("Digitize Service Failure Scenarios",
	// ginkgo.Ordered is intentionally NOT used here.  Each failure test is fully
	// self-contained and must not depend on the result of a preceding test.
	func() {

		// ── Default-exclusion guard ───────────────────────────────────────────
		//
		// Failure tests are skipped unless --run-failure-tests is explicitly
		// passed.  This mirrors the guard used by all other failure suites and
		// prevents accidental execution during a normal suite run.
		ginkgo.BeforeEach(func() {
			if !runFailureTests {
				ginkgo.Skip(
					"[FAILURE-TEST][Digitize] Skipping — pass --run-failure-tests to opt in to failure test execution",
				)
			}
		})

		// digitizeBaseURL is resolved once per Context block by the shared
		// BeforeEach below and then used across all It() blocks in that Context.
		var digitizeBaseURL string

		// resolveDigitizeURL is the shared BeforeEach that resolves the
		// digitize-backend URL from application info.  It skips immediately
		// (zero wait) when --app-name was not provided rather than polling
		// for 2 minutes against a non-existent application.
		resolveDigitizeURL := func() {
			// providedAppName is the raw value of --app-name.  appName is always
			// non-empty (BeforeSuite generates one), so check providedAppName to
			// detect whether the caller targeted a real deployed application.
			if providedAppName == "" {
				ginkgo.Skip(
					"[FAILURE-TEST][Digitize] Skipping — " +
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
					"[FAILURE-TEST][Digitize] Skipping — " +
						"could not resolve application info URLs: " + infoErr.Error(),
				)
			}

			digitizeBaseURL = cli.ExtractDigitizeURL(infoOutput)
			if digitizeBaseURL == "" {
				ginkgo.Skip(
					"[FAILURE-TEST][Digitize] Skipping — " +
						"digitize-backend URL not found in application info output",
				)
			}

			logger.Infof("[FAILURE-TEST][Digitize] resolved digitize URL: %s", digitizeBaseURL)
		}

		// ── TC-1: Input Validation — Invalid Status Filter ────────────────────
		//
		// Rationale: The GET /v1/documents endpoint validates the 'status' query
		// parameter against the known DocStatus enum values before issuing any
		// database query.  Passing an unsupported value must return HTTP 400
		// INVALID_REQUEST immediately — no cleanup is required.
		//
		// Layer exercised: documents.py lines 96–101:
		//   allowed_statuses = {s.value for s in DocStatus}
		//   if status and status not in allowed_statuses:
		//       APIError.raise_error(ErrorCode.INVALID_REQUEST, ...)
		//
		// Expected response:
		//   HTTP 400, {"error":{"code":"INVALID_REQUEST","message":"...","status":400}}
		ginkgo.Context("Input Validation Failure",
			func() {
				ginkgo.BeforeEach(resolveDigitizeURL)

				ginkgo.It(
					"rejects an unknown document status filter with HTTP 400 INVALID_REQUEST",
					ginkgo.Label("failure-test", "digitize-failure", "digitize-input"),
					func() {
						ctx, cancel := withTimeout(30 * time.Second) //nolint:mnd
						defer cancel()

						logger.Infof(
							"[FAILURE-TEST][Digitize][TC-1] Sending GET /v1/documents?status=bogus_value to %s",
							digitizeBaseURL,
						)

						errorResp, err := digitization.ListDocumentsExpectingError(
							ctx,
							digitizeBaseURL,
							"limit=10&offset=0&status=bogus_value",
						)

						// ── Assertions ────────────────────────────────────────
						// 1. No transport error — the service must have responded.
						gomega.Expect(err).NotTo(
							gomega.HaveOccurred(),
							"Expected the digitize service to respond (not a transport error) for an invalid status filter",
						)

						// 2. HTTP 400 with INVALID_REQUEST code and a message that
						//    mentions the bad value.  Use ContainSubstring so minor
						//    wording changes in future service versions do not break
						//    the test.
						gomega.Expect(
							digitization.ValidateDigitizeInvalidRequestError(errorResp, "bogus_value"),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Digitize][TC-1] Correctly rejected invalid status filter — HTTP %d, code: %s, message: %s",
							errorResp.Error.Status, errorResp.Error.Code, errorResp.Error.Message,
						)
					},
				)
			},
		)

		// ── TC-2: Deduplication — Re-submit Already-Ingested File ─────────────
		//
		// Rationale: Once a document has been fully ingested its SHA-256 hash is
		// stored.  Submitting the same file again must return HTTP 409
		// RESOURCE_LOCKED instead of silently creating a duplicate.
		//
		// Layer exercised: jobs.py lines 268–281:
		//   existing = session.query(Document)...filter(hash == new_hash)...first()
		//   if existing:
		//       APIError.raise_error(ErrorCode.RESOURCE_LOCKED, ...)
		//
		// Setup: submit ingestion job + wait for completion; then submit same file again.
		// Cleanup: delete the completed job (removes the document hash from the DB).
		//
		// Expected response on second submit:
		//   HTTP 409, {"error":{"code":"RESOURCE_LOCKED","message":"...","status":409}}
		ginkgo.Context("Deduplication Failure",
			func() {
				ginkgo.BeforeEach(resolveDigitizeURL)

				ginkgo.It(
					"rejects a duplicate file submission with HTTP 409 RESOURCE_LOCKED",
					ginkgo.Label("failure-test", "digitize-failure", "digitize-deduplication"),
					func() {
						ctx, cancel := withTimeout(digitizeFailureTestTimeout)
						defer cancel()

						pdfPath := digitization.GetTestPDFPath()
						gomega.Expect(pdfPath).NotTo(
							gomega.BeEmpty(),
							"test PDF fixture path must be non-empty",
						)

						// ── Step 1: Submit first ingestion job ────────────────
						// If this returns 409 it means test_doc.pdf is already in the
						// DB from a previous interrupted run (cleanup didn't finish).
						// Self-heal: delete the stale document and retry once so the
						// test can run successfully without manual intervention.
						logger.Infof(
							"[FAILURE-TEST][Digitize][TC-2] Submitting initial ingestion job to %s",
							digitizeBaseURL,
						)

						firstJob, firstJobErr := digitization.CreateJob(
							ctx, digitizeBaseURL, pdfPath,
							"ingestion", "json",
							"e2e-failure-dedup-first",
						)
						if firstJobErr != nil {
							if !digitization.IsResourceLockedError(firstJobErr) {
								// Unexpected error — not a dedup 409; fail immediately.
								gomega.Expect(firstJobErr).NotTo(gomega.HaveOccurred(),
									"First ingestion job should be accepted (unexpected error)")
							}
							// 409 → stale document from a previous interrupted run.
							// List documents by name and delete them, then retry.
							logger.Warningf(
								"[FAILURE-TEST][Digitize][TC-2] First submit returned 409 — stale state detected. "+
									"Attempting self-heal by listing and deleting existing test_doc documents.",
							)
							// List documents and delete any named test_doc.pdf.
							docs, listErr := digitization.ListDocuments(ctx, digitizeBaseURL, 100, 0, "", "test_doc.pdf")
							gomega.Expect(listErr).NotTo(gomega.HaveOccurred(),
								"self-heal: failed to list documents to clean up stale state")
							for _, doc := range docs.Data {
								logger.Warningf("[FAILURE-TEST][Digitize][TC-2] self-heal: deleting stale document %s (%s)", doc.ID, doc.Name)
								_ = digitization.DeleteDocument(ctx, digitizeBaseURL, doc.ID)
							}
							// Retry the job submission.
							firstJob, firstJobErr = digitization.CreateJob(
								ctx, digitizeBaseURL, pdfPath,
								"ingestion", "json",
								"e2e-failure-dedup-first",
							)
							gomega.Expect(firstJobErr).NotTo(gomega.HaveOccurred(),
								"First ingestion job should be accepted after self-heal cleanup")
						}
						gomega.Expect(firstJob).NotTo(gomega.BeNil())
						gomega.Expect(firstJob.JobID).NotTo(gomega.BeEmpty())

						logger.Infof("[FAILURE-TEST][Digitize][TC-2] First job created: %s", firstJob.JobID)

						// Cleanup: wait for completion then delete documents AND the
						// job record.  DeleteJob only removes the job — documents
						// (and their hashes) survive independently (jobs.py L415).
						// Deleting documents here is what makes the test repeatable.
						defer digitization.CleanupJobAndDocuments(digitizeBaseURL, firstJob.JobID, "TC-2")

						// ── Step 2: Wait for ingestion to complete ────────────
						logger.Infof("[FAILURE-TEST][Digitize][TC-2] Waiting for first job %s to complete", firstJob.JobID)

						_, waitErr := digitization.WaitForJobCompletion(ctx, digitizeBaseURL, firstJob.JobID, digitizeFailureIngestionWaitTimeout)
						gomega.Expect(waitErr).NotTo(
							gomega.HaveOccurred(),
							"First ingestion job must complete before dedup test can run",
						)

						logger.Infof("[FAILURE-TEST][Digitize][TC-2] First job %s completed; submitting duplicate", firstJob.JobID)

						// ── Step 3: Re-submit the same file ──────────────────
						errorResp, err := digitization.CreateJobExpectingError(
							ctx, digitizeBaseURL, pdfPath,
							"ingestion", "json",
							"e2e-failure-dedup-second",
						)

						// ── Assertions ────────────────────────────────────────
						gomega.Expect(err).NotTo(
							gomega.HaveOccurred(),
							"Expected the digitize service to respond for a duplicate file submission",
						)

						gomega.Expect(
							digitization.ValidateDigitizeResourceLockedError(errorResp, "already"),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Digitize][TC-2] Correctly rejected duplicate submission — HTTP %d, code: %s, message: %s",
							errorResp.Error.Status, errorResp.Error.Code, errorResp.Error.Message,
						)
					},
				)
			},
		)

		// ── TC-3, TC-4, TC-5: Active Resource Protection ──────────────────────
		//
		// These tests verify that resources currently being processed by the
		// Spyre worker cannot be destroyed mid-flight.
		ginkgo.Context("Active Resource Protection Failures",
			func() {
				ginkgo.BeforeEach(resolveDigitizeURL)

				// ── TC-3: DELETE job in accepted state ────────────────────────
				//
				// Rationale: A job that has been accepted but not yet picked up by
				// the Spyre worker is in "accepted" state.  Deleting it at this
				// exact moment must return HTTP 409 RESOURCE_LOCKED rather than
				// corrupting in-flight Spyre state.
				//
				// Layer exercised: jobs.py lines 434–438:
				//   if job.status in (JobStatus.ACCEPTED, JobStatus.IN_PROGRESS):
				//       APIError.raise_error(ErrorCode.RESOURCE_LOCKED, ...)
				//
				// Setup: submit job; immediately attempt delete before it leaves
				// the accepted state.
				// Cleanup: wait for completion, then delete.
				//
				// Note: There is an inherent race — between POST 202 and our DELETE
				// the job may already have advanced to in_progress.  The service
				// returns 409 for both accepted and in_progress, so the assertion
				// holds regardless of which transient state was caught.
				//
				// Expected response:
				//   HTTP 409, {"error":{"code":"RESOURCE_LOCKED","message":"...","status":409}}
				ginkgo.It(
					"rejects DELETE of a job in accepted state with HTTP 409 RESOURCE_LOCKED",
					ginkgo.Label("failure-test", "digitize-failure", "digitize-active-job"),
					func() {
						ctx, cancel := withTimeout(digitizeFailureTestTimeout)
						defer cancel()

						pdfPath := digitization.GetTestPDFPath()
						gomega.Expect(pdfPath).NotTo(gomega.BeEmpty(), "test PDF fixture path must be non-empty")

						// ── Step 1: Submit job ────────────────────────────────
						logger.Infof(
							"[FAILURE-TEST][Digitize][TC-3] Submitting job to %s for accepted-state delete test",
							digitizeBaseURL,
						)

						jobResp, err := digitization.CreateJob(
							ctx, digitizeBaseURL, pdfPath,
							"digitization", "json",
							"e2e-failure-delete-accepted",
						)
						gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Job creation should succeed")
						gomega.Expect(jobResp).NotTo(gomega.BeNil())
						gomega.Expect(jobResp.JobID).NotTo(gomega.BeEmpty())

						logger.Infof("[FAILURE-TEST][Digitize][TC-3] Job created: %s", jobResp.JobID)

						// Cleanup: delete documents AND the job record so the
						// test is repeatable (documents survive job deletion).
						defer digitization.CleanupJobAndDocuments(digitizeBaseURL, jobResp.JobID, "TC-3")

						// ── Step 2: Immediately attempt delete ────────────────
						// The job was just accepted (202); attempt delete right away.
						// It may be in "accepted" or already "in_progress" — either
						// way the service must return 409 RESOURCE_LOCKED.
						logger.Infof("[FAILURE-TEST][Digitize][TC-3] Attempting immediate delete of job %s", jobResp.JobID)

						errorResp, err := digitization.DeleteJobExpectingError(ctx, digitizeBaseURL, jobResp.JobID)

						// ── Assertions ────────────────────────────────────────
						gomega.Expect(err).NotTo(
							gomega.HaveOccurred(),
							"Expected the digitize service to respond for a delete of an active job",
						)

						gomega.Expect(
							digitization.ValidateDigitizeResourceLockedError(errorResp, "active"),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Digitize][TC-3] Correctly rejected delete of accepted job — HTTP %d, code: %s, message: %s",
							errorResp.Error.Status, errorResp.Error.Code, errorResp.Error.Message,
						)
					},
				)

				// ── TC-4: DELETE job in in_progress state ─────────────────────
				//
				// Rationale: Mirrors TC-3 but explicitly polls until the job
				// enters in_progress before attempting the delete.  This exercises
				// the same guard code path but with the Spyre worker already active.
				//
				// Layer exercised: same as TC-3 (jobs.py L434–438).
				//
				// Setup: submit job; poll until in_progress; attempt delete.
				// Cleanup: wait for completion, then delete.
				//
				// Expected response:
				//   HTTP 409, {"error":{"code":"RESOURCE_LOCKED","message":"...","status":409}}
				ginkgo.It(
					"rejects DELETE of a job in in_progress state with HTTP 409 RESOURCE_LOCKED",
					ginkgo.Label("failure-test", "digitize-failure", "digitize-active-job"),
					func() {
						ctx, cancel := withTimeout(digitizeFailureTestTimeout)
						defer cancel()

						pdfPath := digitization.GetTestPDFPath()
						gomega.Expect(pdfPath).NotTo(gomega.BeEmpty(), "test PDF fixture path must be non-empty")

						// ── Step 1: Submit job ────────────────────────────────
						logger.Infof(
							"[FAILURE-TEST][Digitize][TC-4] Submitting job to %s for in_progress-state delete test",
							digitizeBaseURL,
						)

						jobResp, err := digitization.CreateJob(
							ctx, digitizeBaseURL, pdfPath,
							"digitization", "json",
							"e2e-failure-delete-inprogress",
						)
						gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Job creation should succeed")
						gomega.Expect(jobResp).NotTo(gomega.BeNil())
						gomega.Expect(jobResp.JobID).NotTo(gomega.BeEmpty())

						logger.Infof("[FAILURE-TEST][Digitize][TC-4] Job created: %s", jobResp.JobID)

						// Cleanup: delete documents AND the job record so the
						// test is repeatable (documents survive job deletion).
						defer digitization.CleanupJobAndDocuments(digitizeBaseURL, jobResp.JobID, "TC-4")

						// ── Step 2: Poll until in_progress ────────────────────
						logger.Infof("[FAILURE-TEST][Digitize][TC-4] Waiting for job %s to reach in_progress", jobResp.JobID)

						_, err = digitization.WaitForJobInProgress(
							ctx, digitizeBaseURL, jobResp.JobID,
							digitizeFailureInProgressTimeout,
							digitizeFailureInProgressPollInterval,
						)
						gomega.Expect(err).NotTo(
							gomega.HaveOccurred(),
							"Expected job to reach in_progress state before timing out",
						)

						// ── Step 3: Attempt delete while in_progress ──────────
						logger.Infof("[FAILURE-TEST][Digitize][TC-4] Attempting delete of in_progress job %s", jobResp.JobID)

						errorResp, err := digitization.DeleteJobExpectingError(ctx, digitizeBaseURL, jobResp.JobID)

						// ── Assertions ────────────────────────────────────────
						gomega.Expect(err).NotTo(
							gomega.HaveOccurred(),
							"Expected the digitize service to respond for a delete of an in_progress job",
						)

						gomega.Expect(
							digitization.ValidateDigitizeResourceLockedError(errorResp, "active"),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Digitize][TC-4] Correctly rejected delete of in_progress job — HTTP %d, code: %s, message: %s",
							errorResp.Error.Status, errorResp.Error.Code, errorResp.Error.Message,
						)
					},
				)

				// ── TC-5: DELETE document from in_progress job ────────────────
				//
				// Rationale: While a job is actively being processed its documents
				// are locked.  Deleting an individual document at this moment must
				// return HTTP 409 RESOURCE_LOCKED — protecting the Spyre worker
				// from operating on a document that has been removed mid-flight.
				//
				// Layer exercised: documents.py lines 241–245 (via utils/jobs.py
				// is_document_in_active_job which checks IN_PROGRESS status only):
				//   if is_document_in_active_job(session, doc_id):
				//       APIError.raise_error(ErrorCode.RESOURCE_LOCKED, ...)
				//
				// Setup: submit ingestion job; poll until in_progress; get the
				// document ID from job status; attempt document delete.
				// Cleanup: wait for completion, then delete job.
				//
				// Note: is_document_in_active_job() only checks IN_PROGRESS (not
				// ACCEPTED), so we must confirm in_progress before the delete.
				//
				// Expected response:
				//   HTTP 409, {"error":{"code":"RESOURCE_LOCKED","message":"...","status":409}}
				ginkgo.It(
					"rejects DELETE of a document from an in_progress job with HTTP 409 RESOURCE_LOCKED",
					ginkgo.Label("failure-test", "digitize-failure", "digitize-active-doc"),
					func() {
						ctx, cancel := withTimeout(digitizeFailureTestTimeout)
						defer cancel()

						pdfPath := digitization.GetTestPDFPath()
						gomega.Expect(pdfPath).NotTo(gomega.BeEmpty(), "test PDF fixture path must be non-empty")

						// ── Step 1: Submit ingestion job ──────────────────────
						logger.Infof(
							"[FAILURE-TEST][Digitize][TC-5] Submitting ingestion job to %s for active-doc delete test",
							digitizeBaseURL,
						)

						jobResp, err := digitization.CreateJob(
							ctx, digitizeBaseURL, pdfPath,
							"ingestion", "json",
							"e2e-failure-delete-active-doc",
						)
						gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Ingestion job creation should succeed")
						gomega.Expect(jobResp).NotTo(gomega.BeNil())
						gomega.Expect(jobResp.JobID).NotTo(gomega.BeEmpty())

						logger.Infof("[FAILURE-TEST][Digitize][TC-5] Job created: %s", jobResp.JobID)

						// Cleanup: delete documents AND the job record so the
						// test is repeatable (documents survive job deletion).
						defer digitization.CleanupJobAndDocuments(digitizeBaseURL, jobResp.JobID, "TC-5")

						// ── Step 2: Poll until job is in_progress ─────────────
						// is_document_in_active_job() only locks during IN_PROGRESS,
						// so we must confirm that state before attempting the delete.
						logger.Infof("[FAILURE-TEST][Digitize][TC-5] Waiting for job %s to reach in_progress", jobResp.JobID)

						jobStatus, err := digitization.WaitForJobInProgress(
							ctx, digitizeBaseURL, jobResp.JobID,
							digitizeFailureInProgressTimeout,
							digitizeFailureInProgressPollInterval,
						)
						gomega.Expect(err).NotTo(
							gomega.HaveOccurred(),
							"Expected job to reach in_progress state before timing out",
						)
						gomega.Expect(jobStatus).NotTo(gomega.BeNil())

						// ── Step 3: Pick the first document from the active job ─
						gomega.Expect(jobStatus.Documents).NotTo(
							gomega.BeEmpty(),
							"Expected at least one document in the in_progress job",
						)
						docID := jobStatus.Documents[0].ID
						gomega.Expect(docID).NotTo(gomega.BeEmpty())

						logger.Infof(
							"[FAILURE-TEST][Digitize][TC-5] Attempting delete of document %s from in_progress job %s",
							docID, jobResp.JobID,
						)

						// ── Step 4: Attempt document delete ───────────────────
						errorResp, err := digitization.DeleteDocumentExpectingError(ctx, digitizeBaseURL, docID)

						// ── Assertions ────────────────────────────────────────
						gomega.Expect(err).NotTo(
							gomega.HaveOccurred(),
							"Expected the digitize service to respond for a delete of an active document",
						)

						gomega.Expect(
							digitization.ValidateDigitizeResourceLockedError(errorResp, "active"),
						).To(gomega.Succeed())

						logger.Infof(
							"[FAILURE-TEST][Digitize][TC-5] Correctly rejected delete of active document — HTTP %d, code: %s, message: %s",
							errorResp.Error.Status, errorResp.Error.Code, errorResp.Error.Message,
						)
					},
				)
			},
		)
	},
)
