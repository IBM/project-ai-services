package digitization

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
)

// ErrJobNotFound is returned by GetJobStatus on HTTP 404; WaitForJobCompletion treats it as terminal to avoid infinite polling after cleanup.
var ErrJobNotFound = errors.New("job not found (404)")

// getCallTimeout is the end-to-end deadline for a status-poll round-trip (dial + TLS + headers + body); 30 s covers nip.io TLS and slow Spyre pods.
var getCallTimeout = 30 * time.Second //nolint:mnd

// postCallTimeout is the end-to-end deadline for a POST request round-trip.
var postCallTimeout = 60 * time.Second //nolint:mnd

// docCallTimeout is 60 s to accommodate large JSON/markdown content responses over nip.io TLS.
var docCallTimeout = 60 * time.Second //nolint:mnd

// httpCfg returns a common.HTTPClientConfig for the given timeout using a pooled, TLS-insecure client.
// This mirrors the behaviour of the former sharedDigitizeTransport.
func httpCfg(timeout time.Duration) common.HTTPClientConfig {
	return common.HTTPClientConfig{
		Timeout:            timeout,
		InsecureSkipVerify: true,
		PoolConnections:    true,
	}
}

// doGet sends a GET request and returns (body, statusCode, error).
func doGet(ctx context.Context, url string, timeout time.Duration) ([]byte, int, error) {
	return common.DoGET(ctx, url, httpCfg(timeout))
}

// doDelete sends a DELETE request and returns (body, statusCode, error).
func doDelete(ctx context.Context, url string, timeout time.Duration) ([]byte, int, error) {
	return common.DoDELETE(ctx, url, httpCfg(timeout))
}

// expectError returns the digitize-typed error body when status != successStatus.
func expectError(body []byte, statusCode, successStatus int) (*ErrorResponse, error) {
	if statusCode != successStatus {
		return parseErrorResponse(body, statusCode)
	}

	return nil, fmt.Errorf("unexpected success with status code %d: %s", statusCode, string(body))
}

// GetTestPDFPath returns the path to the shared test PDF fixture.
func GetTestPDFPath() string {
	return common.GetTestPDFPath()
}

// JobCreatedResponse represents the response when a job is created.
type JobCreatedResponse struct {
	JobID string `json:"job_id"`
}

// DocumentStatus represents a document in the job status response.
type DocumentStatus struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// JobStats represents statistics about documents in a job.
type JobStats struct {
	TotalDocuments int `json:"total_documents"`
	Completed      int `json:"completed"`
	Failed         int `json:"failed"`
	InProgress     int `json:"in_progress"`
}

// JobStatusResponse represents the response when getting job status.
type JobStatusResponse struct {
	JobID       string           `json:"job_id"`
	JobName     string           `json:"job_name,omitempty"`
	Operation   string           `json:"operation"`
	Status      string           `json:"status"`
	SubmittedAt string           `json:"submitted_at"`
	CompletedAt *string          `json:"completed_at"`
	Documents   []DocumentStatus `json:"documents"`
	Stats       JobStats         `json:"stats"`
	Error       *string          `json:"error"`
}

// JobsListResponse represents the response when listing jobs.
type JobsListResponse struct {
	Data       []JobStatusResponse `json:"data"`
	Pagination PaginationInfo      `json:"pagination"`
}

// PaginationInfo represents pagination metadata.
type PaginationInfo struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// DocumentDetailResponse represents a document returned by both the list and detail endpoints (same JSON shape).
type DocumentDetailResponse struct {
	ID           string         `json:"id"`
	JobID        string         `json:"job_id"`
	Name         string         `json:"name"`
	Type         string         `json:"type"`
	Status       string         `json:"status"`
	OutputFormat string         `json:"output_format"`
	SubmittedAt  string         `json:"submitted_at"`
	CompletedAt  *string        `json:"completed_at"`
	Error        any            `json:"error"`
	Metadata     map[string]any `json:"metadata"`
}

// DocumentListItem is an alias for DocumentDetailResponse; list and detail endpoints share the same JSON shape.
type DocumentListItem = DocumentDetailResponse

// DocumentsListResponse represents the response when listing documents.
type DocumentsListResponse struct {
	Data       []DocumentListItem `json:"data"`
	Pagination PaginationInfo     `json:"pagination"`
}

// DocumentContentResponse represents the document content.
type DocumentContentResponse struct {
	OutputFormat string `json:"output_format"`
	Result       any    `json:"result"`
}

// HealthCheckResponse represents the health check response.
type HealthCheckResponse struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

// ErrorResponse represents an error response from the API.
type ErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Status  int    `json:"status"`
	} `json:"error,omitempty"`
}

// IsResourceLockedError reports whether err is an HTTP 409 resource-lock error.
func IsResourceLockedError(err error) bool {
	return common.IsResourceLockedError(err)
}

// IsRateLimitError reports whether err is an HTTP 429 rate-limit error.
func IsRateLimitError(err error) bool {
	return common.IsRateLimitError(err)
}

// GetDigitizeBaseURL returns the base URL for the digitize service.
func GetDigitizeBaseURL(port string) string {
	return fmt.Sprintf("http://localhost:%s", port)
}

// HealthCheck performs a health check on the digitize service.
const healthCheckRetryInterval = 10 * time.Second //nolint:mnd

// HealthCheck polls baseURL/health until HTTP 200 is returned or ctx is cancelled.
func HealthCheck(ctx context.Context, baseURL string) error {
	healthURL := fmt.Sprintf("%s/health", baseURL)
	client := common.GetHTTPClient(httpCfg(getCallTimeout))
	attempt := 0

	for {
		attempt++

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create health check request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			logger.Warningf("[DIGITIZE] Health check attempt %d failed: %v — retrying in %s",
				attempt, err, healthCheckRetryInterval)
			select {
			case <-ctx.Done():
				return fmt.Errorf("health check request failed: %w", err)
			case <-time.After(healthCheckRetryInterval):
				continue
			}
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			common.DrainAndClose(resp.Body)
			logger.Warningf("[DIGITIZE] Health check attempt %d: status %d — retrying in %s",
				attempt, resp.StatusCode, healthCheckRetryInterval)
			select {
			case <-ctx.Done():
				return fmt.Errorf("health check failed with status %d: %s", resp.StatusCode, string(body))
			case <-time.After(healthCheckRetryInterval):
				continue
			}
		}

		common.DrainAndClose(resp.Body)
		logger.Infof("[DIGITIZE] Health check passed (attempt %d)", attempt)

		return nil
	}
}

// buildJobURL constructs the job creation URL with query parameters.
func buildJobURL(baseURL, operation, outputFormat, jobName string) string {
	url := fmt.Sprintf("%s/v1/jobs?operation=%s&output_format=%s", baseURL, operation, outputFormat)
	if jobName != "" {
		url += fmt.Sprintf("&job_name=%s", jobName)
	}

	return url
}

// createMultipartBody builds a multipart form body containing one or more files.
func createMultipartBody(filePaths ...string) (*bytes.Buffer, *multipart.Writer, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for _, filePath := range filePaths {
		file, err := os.Open(filePath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
		}

		part, err := writer.CreateFormFile("files", filepath.Base(filePath))
		if err != nil {
			_ = file.Close()

			return nil, nil, fmt.Errorf("failed to create form file: %w", err)
		}

		if _, err := io.Copy(part, file); err != nil {
			_ = file.Close()

			return nil, nil, fmt.Errorf("failed to copy file: %w", err)
		}

		_ = file.Close()
	}

	if err := writer.Close(); err != nil {
		return nil, nil, fmt.Errorf("failed to close writer: %w", err)
	}

	return body, writer, nil
}

// sendJobRequest sends the HTTP POST request and returns the response body.
func sendJobRequest(ctx context.Context, url string, body *bytes.Buffer, contentType string) ([]byte, int, error) {
	return common.DoPOST(ctx, url, body, contentType, httpCfg(postCallTimeout))
}

// CreateJob creates a new digitization or ingestion job.
func CreateJob(ctx context.Context, baseURL, filePath, operation, outputFormat, jobName string) (*JobCreatedResponse, error) {
	url := buildJobURL(baseURL, operation, outputFormat, jobName)

	body, writer, err := createMultipartBody(filePath)
	if err != nil {
		return nil, err
	}

	respBody, statusCode, err := sendJobRequest(ctx, url, body, writer.FormDataContentType())
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusAccepted {
		return nil, fmt.Errorf("unexpected status code %d: %s", statusCode, string(respBody))
	}

	var jobResp JobCreatedResponse
	if err := json.Unmarshal(respBody, &jobResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	logger.Infof("[DIGITIZE] Job created: %s ", jobResp.JobID)

	return &jobResp, nil
}

// GetJobStatus retrieves the status of a specific job.
func GetJobStatus(ctx context.Context, baseURL, jobID string) (*JobStatusResponse, error) {
	body, statusCode, err := doGet(ctx, fmt.Sprintf("%s/v1/jobs/%s", baseURL, jobID), getCallTimeout)
	if err != nil {
		return nil, err
	}

	// 404 → job deleted or never existed; return ErrJobNotFound so callers can distinguish "gone" from transient errors.
	if statusCode == http.StatusNotFound {
		return nil, ErrJobNotFound
	}

	var jobStatus JobStatusResponse
	if err := common.ValidateStatusAndUnmarshal(body, statusCode, http.StatusOK, &jobStatus); err != nil {
		return nil, err
	}

	return &jobStatus, nil
}

// handleJobStatus processes the job status and returns appropriate result or error.
func handleJobStatus(status *JobStatusResponse, jobID string) (*JobStatusResponse, error, bool) {
	logger.Infof("[DIGITIZE] Job %s status: %s", jobID, status.Status)

	switch status.Status {
	case "completed":
		return status, nil, true
	case "failed":
		errMsg := "unknown error"
		if status.Error != nil {
			errMsg = *status.Error
		}

		return status, fmt.Errorf("job failed: %s", errMsg), true
	case "accepted", "pending", "in_progress":
		// Transient states (accepted/pending/in_progress): keep polling.
		return nil, nil, false
	default:
		return status, fmt.Errorf("unknown job status: %s", status.Status), true
	}
}

// WaitForJobCompletion polls job status every 15 s until completed, failed, timeout, or ErrJobNotFound (404 treated as terminal).
func WaitForJobCompletion(ctx context.Context, baseURL, jobID string, timeout time.Duration) (*JobStatusResponse, error) {
	const pollInterval = 15 * time.Second

	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for job %s to complete after %s", jobID, timeout)
		}

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		status, err := GetJobStatus(ctx, baseURL, jobID)
		if err != nil {
			// 404 → job deleted externally; treat as terminal.
			if errors.Is(err, ErrJobNotFound) {
				logger.Infof("[DIGITIZE] Job %s not found (404) — treating as complete (already deleted)", jobID)

				return nil, nil
			}
			logger.Warningf("[DIGITIZE] Failed to get job status for %s: %v — retrying in %s", jobID, err, pollInterval)
		} else {
			result, resultErr, done := handleJobStatus(status, jobID)
			if done {
				return result, resultErr
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// ListJobs retrieves a list of all jobs.
func ListJobs(ctx context.Context, baseURL string, latest bool, limit, offset int, status, operation string) (*JobsListResponse, error) {
	url := fmt.Sprintf("%s/v1/jobs?latest=%t&limit=%d&offset=%d", baseURL, latest, limit, offset)
	if status != "" {
		url += fmt.Sprintf("&status=%s", status)
	}
	if operation != "" {
		url += fmt.Sprintf("&operation=%s", operation)
	}

	body, statusCode, err := doGet(ctx, url, getCallTimeout)
	if err != nil {
		return nil, err
	}

	var jobsList JobsListResponse
	if err := common.ValidateStatusAndUnmarshal(body, statusCode, http.StatusOK, &jobsList); err != nil {
		return nil, err
	}

	return &jobsList, nil
}

// DeleteJob deletes a specific job.
func DeleteJob(ctx context.Context, baseURL, jobID string) error {
	body, statusCode, err := doDelete(ctx, fmt.Sprintf("%s/v1/jobs/%s", baseURL, jobID), getCallTimeout)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code %d: %s", statusCode, string(body))
	}

	logger.Infof("[DIGITIZE] Job deleted: %s", jobID)

	return nil
}

// ListDocuments retrieves documents with optional status/name filters; pass empty strings to list all.
func ListDocuments(ctx context.Context, baseURL string, limit, offset int, status, name string) (*DocumentsListResponse, error) {
	url := fmt.Sprintf("%s/v1/documents?limit=%d&offset=%d", baseURL, limit, offset)
	if status != "" {
		url += fmt.Sprintf("&status=%s", status)
	}
	if name != "" {
		url += fmt.Sprintf("&name=%s", name)
	}

	body, statusCode, err := doGet(ctx, url, getCallTimeout)
	if err != nil {
		return nil, err
	}

	var docsList DocumentsListResponse
	if err := common.ValidateStatusAndUnmarshal(body, statusCode, http.StatusOK, &docsList); err != nil {
		return nil, err
	}

	return &docsList, nil
}

// GetDocument retrieves detailed information about a specific document.
func GetDocument(ctx context.Context, baseURL, docID string) (*DocumentDetailResponse, error) {
	body, statusCode, err := doGet(ctx, fmt.Sprintf("%s/v1/documents/%s", baseURL, docID), getCallTimeout)
	if err != nil {
		return nil, err
	}

	var doc DocumentDetailResponse
	if err := common.ValidateStatusAndUnmarshal(body, statusCode, http.StatusOK, &doc); err != nil {
		return nil, err
	}

	return &doc, nil
}

// GetDocumentContent retrieves the content of a specific document.
func GetDocumentContent(ctx context.Context, baseURL, docID string) (*DocumentContentResponse, error) {
	body, statusCode, err := doGet(ctx, fmt.Sprintf("%s/v1/documents/%s/content", baseURL, docID), docCallTimeout)
	if err != nil {
		return nil, err
	}

	var content DocumentContentResponse
	if err := common.ValidateStatusAndUnmarshal(body, statusCode, http.StatusOK, &content); err != nil {
		return nil, err
	}

	return &content, nil
}

// DeleteDocument deletes a specific document.
func DeleteDocument(ctx context.Context, baseURL, docID string) error {
	body, statusCode, err := doDelete(ctx, fmt.Sprintf("%s/v1/documents/%s", baseURL, docID), getCallTimeout)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code %d: %s", statusCode, string(body))
	}

	logger.Infof("[DIGITIZE] Document deleted: %s", docID)

	return nil
}

// DeleteAllDocuments deletes all documents.
func DeleteAllDocuments(ctx context.Context, baseURL string) error {
	body, statusCode, err := doDelete(ctx, fmt.Sprintf("%s/v1/documents?confirm=true", baseURL), docCallTimeout)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code %d: %s", statusCode, string(body))
	}

	logger.Infof("[DIGITIZE] All documents deleted")

	return nil
}

// parseErrorResponse parses body as ErrorResponse; returns a plain HTTP-status error on invalid JSON or empty payload.
func parseErrorResponse(respBody []byte, statusCode int) (*ErrorResponse, error) {
	var errorResp ErrorResponse
	if err := json.Unmarshal(respBody, &errorResp); err != nil {
		return nil, fmt.Errorf("HTTP %d: %s", statusCode, strings.TrimSpace(string(respBody)))
	}

	// Guard against empty `{}` or `{"error":{}}` responses.
	if errorResp.Error.Code == "" && errorResp.Error.Message == "" {
		return nil, fmt.Errorf("HTTP %d: empty error body: %s", statusCode, string(respBody))
	}

	return &errorResp, nil
}

// CreateJobExpectingError creates a job and returns error response if status is not 202.
func CreateJobExpectingError(ctx context.Context, baseURL, filePath, operation, outputFormat, jobName string) (*ErrorResponse, error) {
	url := buildJobURL(baseURL, operation, outputFormat, jobName)

	body, writer, err := createMultipartBody(filePath)
	if err != nil {
		return nil, err
	}

	respBody, statusCode, err := sendJobRequest(ctx, url, body, writer.FormDataContentType())
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusAccepted {
		return parseErrorResponse(respBody, statusCode)
	}

	return nil, fmt.Errorf("unexpected success with status code %d: %s", statusCode, string(respBody))
}

// GetJobStatusExpectingError retrieves job status and returns error response if status is not 200.
func GetJobStatusExpectingError(ctx context.Context, baseURL, jobID string) (*ErrorResponse, error) {
	body, statusCode, err := doGet(ctx, fmt.Sprintf("%s/v1/jobs/%s", baseURL, jobID), getCallTimeout)
	if err != nil {
		return nil, err
	}

	return expectError(body, statusCode, http.StatusOK)
}

// GetDocumentExpectingError retrieves document details and returns error response if status is not 200.
func GetDocumentExpectingError(ctx context.Context, baseURL, docID string) (*ErrorResponse, error) {
	body, statusCode, err := doGet(ctx, fmt.Sprintf("%s/v1/documents/%s", baseURL, docID), getCallTimeout)
	if err != nil {
		return nil, err
	}

	return expectError(body, statusCode, http.StatusOK)
}

// GetDocumentContentExpectingError retrieves document content and returns error response if status is not 200.
func GetDocumentContentExpectingError(ctx context.Context, baseURL, docID string) (*ErrorResponse, error) {
	body, statusCode, err := doGet(ctx, fmt.Sprintf("%s/v1/documents/%s/content", baseURL, docID), docCallTimeout)
	if err != nil {
		return nil, err
	}

	return expectError(body, statusCode, http.StatusOK)
}

// DeleteJobExpectingError deletes a job and returns the error response for any non-200/204 status code.
func DeleteJobExpectingError(ctx context.Context, baseURL, jobID string) (*ErrorResponse, error) {
	body, statusCode, err := doDelete(ctx, fmt.Sprintf("%s/v1/jobs/%s", baseURL, jobID), getCallTimeout)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return parseErrorResponse(body, statusCode)
	}

	return nil, fmt.Errorf("unexpected success with status code %d: %s", statusCode, string(body))
}

// DeleteDocumentExpectingError deletes a document and returns error response if status is not 200/204.
func DeleteDocumentExpectingError(ctx context.Context, baseURL, docID string) (*ErrorResponse, error) {
	body, statusCode, err := doDelete(ctx, fmt.Sprintf("%s/v1/documents/%s", baseURL, docID), getCallTimeout)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return parseErrorResponse(body, statusCode)
	}

	return nil, fmt.Errorf("unexpected success with status code %d: %s", statusCode, string(body))
}

// CreateJobWithMultipleFiles attempts to create a job with multiple files (should fail for digitization).
func CreateJobWithMultipleFiles(ctx context.Context, baseURL string, filePaths []string, operation, outputFormat, jobName string) (*ErrorResponse, error) {
	url := buildJobURL(baseURL, operation, outputFormat, jobName)

	body, writer, err := createMultipartBody(filePaths...)
	if err != nil {
		return nil, err
	}

	respBody, statusCode, err := sendJobRequest(ctx, url, body, writer.FormDataContentType())
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusAccepted {
		return parseErrorResponse(respBody, statusCode)
	}

	return nil, fmt.Errorf("unexpected success with status code %d: %s", statusCode, string(respBody))
}

// logFailedDocDetails fetches and logs per-document error details for any failed.
func logFailedDocDetails(ctx context.Context, digitizeBaseURL string, status *JobStatusResponse) {
	if status == nil {
		return
	}
	for _, doc := range status.Documents {
		if doc.Status != "failed" {
			continue
		}
		detail, err := GetDocument(ctx, digitizeBaseURL, doc.ID)
		if err != nil {
			logger.Warningf("[INGEST] could not fetch detail for doc %s (%s): %v", doc.ID, doc.Name, err)

			continue
		}
		logger.Infof("[INGEST] doc %s (%s) failure detail: %v", doc.ID, doc.Name, detail.Error)
	}
}

// IngestTestDocumentViaDigitizeAPI ingests test_doc.pdf via the digitize microservice (operation=ingestion) so it is indexed in OpenSearch before RAG evaluation.
func IngestTestDocumentViaDigitizeAPI(ctx context.Context, digitizeBaseURL, jobName string) error {
	pdfPath := GetTestPDFPath()
	if pdfPath == "" {
		return fmt.Errorf("could not resolve test PDF path")
	}

	const ingestTimeout = 20 * time.Minute // OCR + embedding + OpenSearch indexing can take up to 20 min.

	logger.Infof("[INGEST] Submitting ingestion job for %s (job name: %s)", filepath.Base(pdfPath), jobName)

	jobResp, err := CreateJob(ctx, digitizeBaseURL, pdfPath, "ingestion", "json", jobName)
	if err != nil {
		return fmt.Errorf("failed to create ingestion job: %w", err)
	}

	logger.Infof("[INGEST] Job submitted (job_id=%s) — waiting for completion", jobResp.JobID)

	finalStatus, err := WaitForJobCompletion(ctx, digitizeBaseURL, jobResp.JobID, ingestTimeout)
	if err != nil {
		logFailedDocDetails(ctx, digitizeBaseURL, finalStatus)

		return fmt.Errorf("ingestion job %s did not complete: %w", jobResp.JobID, err)
	}

	logger.Infof("[INGEST] Job %s completed — status=%s docs=%d",
		jobResp.JobID, finalStatus.Status, len(finalStatus.Documents))

	return nil
}

// Made with Bob
