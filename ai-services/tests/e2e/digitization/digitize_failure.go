package digitization

// digitize_failure.go — validator helpers and polling utilities for the
// digitize failure test suite.
//
// These helpers are intentionally separate from digitize.go so that the
// failure-test compilation surface stays self-contained and easy to review.
// They follow the same validation style used in tests/e2e/similarity/similarity.go
// (check HTTP status → parse body → check code → check message substring).

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// ListDocumentsExpectingError calls GET /v1/documents with the provided query
// string (e.g. "limit=10&offset=0&status=bogus") and returns the parsed
// ErrorResponse when the service replies with any non-200 status code.
// If the service unexpectedly returns 200, a descriptive error is returned.
func ListDocumentsExpectingError(ctx context.Context, baseURL, queryString string) (*ErrorResponse, error) {
	url := fmt.Sprintf("%s/v1/documents?%s", baseURL, queryString)

	body, statusCode, err := doGet(ctx, url, getCallTimeout)
	if err != nil {
		return nil, err
	}

	return expectError(body, statusCode, http.StatusOK)
}

// maxConsecutiveErrors is the number of consecutive GetJobStatus network
// failures that triggers an early-exit in WaitForJobInProgress.  A
// temporary blip is expected occasionally; 5 in a row indicates the service
// is down and further waiting would only waste the test slot.
const maxConsecutiveErrors = 5 //nolint:mnd

// WaitForJobInProgress polls GET /v1/jobs/{jobID} every pollInterval until the
// job transitions into "in_progress" status, or until timeout expires.
//
// Returns the most-recent JobStatusResponse (which will have Status == "in_progress")
// on success, or a descriptive error on timeout, terminal state, or too many
// consecutive network errors.
//
// Use this in failure tests that must attempt a disruptive action (delete job,
// delete document) while the Spyre worker is actively processing.
func WaitForJobInProgress(ctx context.Context, baseURL, jobID string, timeout, pollInterval time.Duration) (*JobStatusResponse, error) {
	deadline := time.Now().Add(timeout)
	consecutiveErrors := 0

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for job %s to reach in_progress after %s", jobID, timeout)
		}

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		status, err := GetJobStatus(ctx, baseURL, jobID)
		if err != nil {
			consecutiveErrors++
			logger.Warningf("[DIGITIZE] WaitForJobInProgress: error polling job %s (%d/%d): %v",
				jobID, consecutiveErrors, maxConsecutiveErrors, err)
			if consecutiveErrors >= maxConsecutiveErrors {
				return nil, fmt.Errorf("WaitForJobInProgress: aborting after %d consecutive errors polling job %s: %w",
					maxConsecutiveErrors, jobID, err)
			}
		} else {
			consecutiveErrors = 0
			logger.Infof("[DIGITIZE] WaitForJobInProgress: job %s status = %q", jobID, status.Status)

			switch status.Status {
			case "in_progress":
				return status, nil
			case "completed", "failed":
				return nil, fmt.Errorf("job %s reached terminal state %q before entering in_progress", jobID, status.Status)
			}
			// accepted / pending / unknown: keep polling
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// maxStaleDocListLimit is the page size used by DeleteStaleDocumentsByName.
// A single test fixture (test_doc.pdf) will never have more than a handful
// of stale copies, so 100 is a safe upper bound.
const maxStaleDocListLimit = 100 //nolint:mnd

// DeleteStaleDocumentsByName lists all documents matching the given filename
// and deletes each one.  Use this to clear leftover documents from a previous
// interrupted test run before retrying a job submission that would otherwise
// be rejected with 409 RESOURCE_LOCKED due to hash deduplication.
//
// Returns an error if the list call fails; individual delete failures are
// logged as warnings and do not stop processing the remaining documents.
func DeleteStaleDocumentsByName(ctx context.Context, baseURL, filename string) error {
	docs, err := ListDocuments(ctx, baseURL, maxStaleDocListLimit, 0, "", filename)
	if err != nil {
		return fmt.Errorf("DeleteStaleDocumentsByName: failed to list documents named %q: %w", filename, err)
	}

	for _, doc := range docs.Data {
		logger.Warningf("[DIGITIZE] DeleteStaleDocumentsByName: deleting stale document %s (%s)", doc.ID, doc.Name)
		if delErr := DeleteDocument(ctx, baseURL, doc.ID); delErr != nil {
			logger.Warningf("[DIGITIZE] DeleteStaleDocumentsByName: failed to delete document %s: %v", doc.ID, delErr)
		}
	}

	return nil
}

// cleanupTimeout is the per-cleanup budget: long enough for a slow Spyre job
// to finish, short enough to avoid blocking the test runner indefinitely.
const cleanupTimeout = 5 * time.Minute //nolint:mnd

// CleanupJobAndDocuments is the repeatable-safe cleanup helper used by all
// digitize failure tests.  It:
//   1. Waits for the job to reach a terminal state (completed / failed / 404)
//   2. Fetches the final job status to collect document IDs
//   3. Deletes each document individually
//   4. Deletes the job record
//
// Important: DeleteJob only removes the job record — documents survive
// independently (confirmed in jobs.py L415).  Omitting step 3 leaves document
// hashes in the DB, causing the next run's CreateJob to return 409 immediately.
//
// tcLabel is used only for log messages (e.g. "TC-2").
func CleanupJobAndDocuments(baseURL, jobID, tcLabel string) {
	cleanCtx, cleanCancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cleanCancel()

	// Step 1: wait for terminal state so DeleteJob won't be rejected with 409.
	finalStatus, _ := WaitForJobCompletion(cleanCtx, baseURL, jobID, cleanupTimeout)

	// Step 2: collect document IDs — prefer the final status; fall back to a
	// fresh GetJobStatus call if WaitForJobCompletion returned nil status.
	if finalStatus == nil {
		finalStatus, _ = GetJobStatus(cleanCtx, baseURL, jobID)
	}

	// Step 3: delete each document.
	if finalStatus != nil {
		for _, doc := range finalStatus.Documents {
			if doc.ID == "" {
				continue
			}
			if delErr := DeleteDocument(cleanCtx, baseURL, doc.ID); delErr != nil {
				logger.Warningf(
					"[FAILURE-TEST][Digitize][%s] cleanup: failed to delete document %s: %v",
					tcLabel, doc.ID, delErr,
				)
			}
		}
	}

	// Step 4: delete the job record.
	if delErr := DeleteJob(cleanCtx, baseURL, jobID); delErr != nil {
		logger.Warningf(
			"[FAILURE-TEST][Digitize][%s] cleanup: failed to delete job %s: %v",
			tcLabel, jobID, delErr,
		)
	}
}

// ValidateDigitizeInvalidRequestError asserts that the service rejected a
// request with HTTP 400 and error code INVALID_REQUEST, and that the error
// message contains msgSubstring.
//
// Use this for input-validation failures (e.g. unsupported query-parameter
// value) where the service fires before touching the database.
func ValidateDigitizeInvalidRequestError(errorResp *ErrorResponse, msgSubstring string) error {
	if errorResp == nil {
		return fmt.Errorf("expected an INVALID_REQUEST error response, got nil")
	}

	if errorResp.Error.Status != http.StatusBadRequest {
		return fmt.Errorf("expected HTTP status 400 (INVALID_REQUEST), got %d — code: %q message: %q",
			errorResp.Error.Status, errorResp.Error.Code, errorResp.Error.Message)
	}

	if errorResp.Error.Code != "INVALID_REQUEST" {
		return fmt.Errorf("expected error code INVALID_REQUEST, got %q — message: %q",
			errorResp.Error.Code, errorResp.Error.Message)
	}

	if !strings.Contains(errorResp.Error.Message, msgSubstring) {
		return fmt.Errorf("expected error message to contain %q, got %q",
			msgSubstring, errorResp.Error.Message)
	}

	return nil
}

// ValidateDigitizeResourceLockedError asserts that the service rejected a
// request with HTTP 409 and error code RESOURCE_LOCKED, and that the error
// message contains msgSubstring.
//
// Use this for attempts to modify a resource that is currently being
// processed (active job deletion, duplicate file submission, document delete
// while job is in progress).
func ValidateDigitizeResourceLockedError(errorResp *ErrorResponse, msgSubstring string) error {
	if errorResp == nil {
		return fmt.Errorf("expected a RESOURCE_LOCKED error response, got nil")
	}

	if errorResp.Error.Status != http.StatusConflict {
		return fmt.Errorf("expected HTTP status 409 (RESOURCE_LOCKED), got %d — code: %q message: %q",
			errorResp.Error.Status, errorResp.Error.Code, errorResp.Error.Message)
	}

	if errorResp.Error.Code != "RESOURCE_LOCKED" {
		return fmt.Errorf("expected error code RESOURCE_LOCKED, got %q — message: %q",
			errorResp.Error.Code, errorResp.Error.Message)
	}

	if !strings.Contains(errorResp.Error.Message, msgSubstring) {
		return fmt.Errorf("expected error message to contain %q, got %q",
			msgSubstring, errorResp.Error.Message)
	}

	return nil
}
