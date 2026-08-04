package summarization

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
)

const (
	getCallTimeout  = 10 * time.Second
	postCallTimeout = 120 * time.Second // Longer timeout for summarization
	pollInterval    = 5 * time.Second   // Polling interval for job status
)

// SetAppRuntime stores the application runtime for summarization tests.
func SetAppRuntime(string) {}

// httpConfigForTimeout returns a common.HTTPClientConfig with the specified timeout.
func httpConfigForTimeout(timeout time.Duration) common.HTTPClientConfig {
	return common.HTTPClientConfig{
		Timeout:            timeout,
		InsecureSkipVerify: true,
		PoolConnections:    false, // Summarization uses short-lived clients
	}
}

// GetTestPDFPath returns the path to a test PDF file relative to this package.
func GetTestPDFPath() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}

	testDir := filepath.Dir(filename)

	return filepath.Join(filepath.Dir(testDir), "ingestion", "docs", "test_doc.pdf")
}

// GetTestTXTPath returns the path to a test TXT file relative to this package.
func GetTestTXTPath() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}

	testDir := filepath.Dir(filename)

	return filepath.Join(filepath.Dir(testDir), "ingestion", "docs", "sample_txt.txt")
}

// JobCreatedResponse represents the response when a job is created.
type JobCreatedResponse struct {
	JobID string `json:"job_id"`
}

// JobStatus represents the status of a job.
type JobStatus string

const (
	JobStatusAccepted   JobStatus = "accepted"
	JobStatusInProgress JobStatus = "in_progress"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
)

// DocumentInfo represents document information in job detail response.
type DocumentInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// JobMetadata represents metadata for chunked summarization.
type JobMetadata struct {
	TotalChunks     int    `json:"total_chunks"`
	CompletedChunks int    `json:"completed_chunks"`
	FailedChunks    int    `json:"failed_chunks"`
	Phase           string `json:"phase"`
}

// JobDetailResponse represents the response when getting job details.
type JobDetailResponse struct {
	JobID       string       `json:"job_id"`
	JobName     *string      `json:"job_name,omitempty"`
	Status      JobStatus    `json:"status"`
	SubmittedAt string       `json:"submitted_at"`
	CompletedAt *string      `json:"completed_at"`
	Document    DocumentInfo `json:"document"`
	Error       *string      `json:"error"`
	Metadata    *JobMetadata `json:"metadata,omitempty"`
}

// JobResultResponse represents the response when getting job result.
type JobResultResponse struct {
	Data  map[string]interface{} `json:"data"`
	Meta  map[string]interface{} `json:"meta"`
	Usage map[string]interface{} `json:"usage"`
}

// PaginationInfo represents pagination metadata.
type PaginationInfo struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// JobsListResponse represents the response when listing jobs.
type JobsListResponse struct {
	Pagination PaginationInfo      `json:"pagination"`
	Data       []JobDetailResponse `json:"data"`
}

// HealthCheckResponse represents the health check response.
type HealthCheckResponse struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

// ErrorResponse is an alias to common.ErrorResponse for backward compatibility.
type ErrorResponse = common.ErrorResponse

// ─────────────────────────────────────────────────────────────────────────────
// URL helpers — centralize endpoint construction
// ─────────────────────────────────────────────────────────────────────────────

// healthURL constructs the health check endpoint URL.
func healthURL(baseURL string) string {
	return fmt.Sprintf("%s/health", baseURL)
}

// jobsURL constructs the jobs list/delete endpoint URL.
func jobsURL(baseURL string) string {
	return fmt.Sprintf("%s/v1/summarize/jobs", baseURL)
}

// jobURL constructs a job detail endpoint URL.
func jobURL(baseURL, jobID string) string {
	return fmt.Sprintf("%s/v1/summarize/jobs/%s", baseURL, jobID)
}

// jobResultURL constructs a job result endpoint URL.
func jobResultURL(baseURL, jobID string) string {
	return fmt.Sprintf("%s/v1/summarize/jobs/%s/result", baseURL, jobID)
}

// buildJobCreateURL constructs the job creation URL with query parameters.
func buildJobCreateURL(baseURL, level, jobName string, stream bool) string {
	url := fmt.Sprintf("%s?stream=%t", jobsURL(baseURL), stream)
	if level != "" {
		url += fmt.Sprintf("&level=%s", level)
	}
	if jobName != "" {
		url += fmt.Sprintf("&job_name=%s", jobName)
	}

	return url
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP helpers — delegate to common utilities
// ─────────────────────────────────────────────────────────────────────────────

// doGET sends a GET request and returns the response body and status code.
func doGET(ctx context.Context, url string) ([]byte, int, error) {
	return common.DoGET(ctx, url, httpConfigForTimeout(getCallTimeout))
}

// doPOST sends a POST request with the given body and content type.
func doPOST(ctx context.Context, url string, body *bytes.Buffer, contentType string) ([]byte, int, error) {
	return common.DoPOST(ctx, url, body, contentType, httpConfigForTimeout(postCallTimeout))
}

// doDELETE sends a DELETE request.
func doDELETE(ctx context.Context, url string) ([]byte, int, error) {
	return common.DoDELETE(ctx, url, httpConfigForTimeout(getCallTimeout))
}

// HealthCheck performs a health check on the summarize service.
func HealthCheck(ctx context.Context, baseURL string) error {
	respBody, statusCode, err := doGET(ctx, healthURL(baseURL))
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}

	if statusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status %d: %s", statusCode, string(respBody))
	}

	logger.Infof("[SUMMARIZE] Health check passed")

	return nil
}

// createMultipartBody creates a multipart form body with a file.
func createMultipartBody(filePath string) (*bytes.Buffer, *multipart.Writer, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return nil, nil, fmt.Errorf("failed to copy file: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, nil, fmt.Errorf("failed to close writer: %w", err)
	}

	return body, writer, nil
}

// CreateJobWithFile creates a new summarization job with a file upload.
func CreateJobWithFile(ctx context.Context, baseURL, filePath, level, jobName string, stream bool) (*JobCreatedResponse, error) {
	url := buildJobCreateURL(baseURL, level, jobName, stream)

	body, writer, err := createMultipartBody(filePath)
	if err != nil {
		return nil, err
	}

	respBody, statusCode, err := doPOST(ctx, url, body, writer.FormDataContentType())
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

	logger.Infof("[SUMMARIZE] Job created: %s", jobResp.JobID)

	return &jobResp, nil
}

// CreateJobWithText creates a job from text by creating a temporary file.
//nolint:cyclop // Test helper function, complexity acceptable
func CreateJobWithText(ctx context.Context, baseURL, text, level, jobName string, stream bool) (*JobCreatedResponse, error) {
	tmpFile, err := createTempTextFile(text)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = os.Remove(tmpFile) //nolint:errcheck // Cleanup, error not critical
	}()

	// Use the file-based creation
	return CreateJobWithFile(ctx, baseURL, tmpFile, level, jobName, stream)
}

// createTempTextFile creates a temporary text file with the given content.
func createTempTextFile(text string) (string, error) {
	tmpFile, err := os.CreateTemp("", "summarize-text-*.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		_ = tmpFile.Close() //nolint:errcheck // Cleanup, error not critical
	}()

	if _, err := tmpFile.WriteString(text); err != nil {
		_ = os.Remove(tmpFile.Name()) //nolint:errcheck // Cleanup on error path

		return "", fmt.Errorf("failed to write to temp file: %w", err)
	}

	return tmpFile.Name(), nil
}

// JobResult represents a completed summarization job with its summary text.
type JobResult struct {
	Detail  *JobDetailResponse
	Summary string
}

// SubmitAndVerifyJob submits a job, waits for completion, and returns the result.
// Pass filePath for file-based jobs or text for text-based jobs (creates a temporary file).
func SubmitAndVerifyJob(
	ctx context.Context,
	baseURL string,
	filePath string,
	text string,
	level string,
	jobName string,
	stream bool,
	jobTimeout time.Duration,
) (*JobResult, error) {
	var (
		jobResp *JobCreatedResponse
		err     error
	)

	if filePath != "" {
		jobResp, err = CreateJobWithFile(ctx, baseURL, filePath, level, jobName, stream)
	} else {
		jobResp, err = CreateJobWithText(ctx, baseURL, text, level, jobName, stream)
	}

	if err != nil {
		return nil, fmt.Errorf("create job: %w", err)
	}

	// Brief pause to let the service begin processing before polling.
	time.Sleep(2 * time.Second) //nolint:mnd

	detail, err := WaitForJobCompletion(ctx, baseURL, jobResp.JobID, jobTimeout)
	if err != nil {
		return nil, fmt.Errorf("wait for job %s: %w", jobResp.JobID, err)
	}

	result, err := GetJobResult(ctx, baseURL, jobResp.JobID)
	if err != nil {
		return nil, fmt.Errorf("get result for job %s: %w", jobResp.JobID, err)
	}

	summary, ok := result.Data["summary"].(string)
	if !ok || summary == "" {
		return nil, fmt.Errorf("job %s result missing non-empty summary", jobResp.JobID)
	}

	return &JobResult{Detail: detail, Summary: summary}, nil
}

// SubmitJobExpectingFailure submits a job and waits for it to fail.
// Returns the job detail so callers can assert on the Error field.
func SubmitJobExpectingFailure(
	ctx context.Context,
	baseURL string,
	text string,
	level string,
	jobName string,
	jobTimeout time.Duration,
) (*JobDetailResponse, error) {
	jobResp, err := CreateJobWithText(ctx, baseURL, text, level, jobName, false)
	if err != nil {
		return nil, fmt.Errorf("create job: %w", err)
	}

	// Brief pause to let the service begin processing before polling.
	time.Sleep(2 * time.Second) //nolint:mnd

	detail, err := WaitForJobCompletion(ctx, baseURL, jobResp.JobID, jobTimeout)
	// WaitForJobCompletion returns a non-nil error when status == Failed; that is
	// expected here, so we return the detail alongside the error so the caller can
	// inspect both.
	return detail, err
}

// GetJobDetail retrieves the details of a specific job.
func GetJobDetail(ctx context.Context, baseURL, jobID string) (*JobDetailResponse, error) {
	respBody, statusCode, err := doGET(ctx, jobURL(baseURL, jobID))
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d: %s", statusCode, string(respBody))
	}

	var jobDetail JobDetailResponse
	if err := json.Unmarshal(respBody, &jobDetail); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &jobDetail, nil
}

// GetJobResult retrieves the result of a completed job.
func GetJobResult(ctx context.Context, baseURL, jobID string) (*JobResultResponse, error) {
	respBody, statusCode, err := doGET(ctx, jobResultURL(baseURL, jobID))
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d: %s", statusCode, string(respBody))
	}

	var result JobResultResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// WaitForJobCompletion waits for a job to complete.
//nolint:cyclop // Polling logic, complexity acceptable for test helper
func WaitForJobCompletion(ctx context.Context, baseURL, jobID string, timeout time.Duration) (*JobDetailResponse, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("timeout waiting for job completion")
			}

			detail, err := GetJobDetail(ctx, baseURL, jobID)
			if err != nil {
				logger.Warningf("[SUMMARIZE] Failed to get job detail: %v", err)

				continue
			}

			logger.Infof("[SUMMARIZE] Job %s status: %s", jobID, detail.Status)

			switch detail.Status {
			case JobStatusCompleted:
				return detail, nil
			case JobStatusFailed:
				errMsg := "unknown error"
				if detail.Error != nil {
					errMsg = *detail.Error
				}

				return detail, fmt.Errorf("job failed: %s", errMsg)
			case JobStatusAccepted, JobStatusInProgress:
				continue
			default:
				return detail, fmt.Errorf("unknown job status: %s", detail.Status)
			}
		}
	}
}

// ListJobs retrieves a list of all jobs.
func ListJobs(ctx context.Context, baseURL string, limit, offset int, status, jobName string) (*JobsListResponse, error) {
	url := fmt.Sprintf("%s?limit=%d&offset=%d", jobsURL(baseURL), limit, offset)
	if status != "" {
		url += fmt.Sprintf("&status=%s", status)
	}
	if jobName != "" {
		url += fmt.Sprintf("&job_name=%s", jobName)
	}

	respBody, statusCode, err := doGET(ctx, url)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d: %s", statusCode, string(respBody))
	}

	var jobsList JobsListResponse
	if err := json.Unmarshal(respBody, &jobsList); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &jobsList, nil
}

// deleteJobWithURL performs a DELETE request and validates the response.
// Accepts 200 OK or 204 No Content as success.
func deleteJobWithURL(ctx context.Context, url string, logMsg string) error {
	_, statusCode, err := doDELETE(ctx, url)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code %d", statusCode)
	}

	logger.Infof("[SUMMARIZE] %s", logMsg)

	return nil
}

// DeleteJob deletes a specific job.
func DeleteJob(ctx context.Context, baseURL, jobID string) error {
	return deleteJobWithURL(ctx, jobURL(baseURL, jobID), fmt.Sprintf("Job deleted: %s", jobID))
}

// DeleteAllJobs deletes all jobs.
func DeleteAllJobs(ctx context.Context, baseURL string) error {
	url := fmt.Sprintf("%s?confirm=true", jobsURL(baseURL))

	return deleteJobWithURL(ctx, url, "All jobs deleted")
}

// parseErrorResponse parses the response body as an error response.
// Delegates to common.ParseErrorResponse for backward compatibility.
func parseErrorResponse(respBody []byte, statusCode int) (*ErrorResponse, error) {
	return common.ParseErrorResponse(respBody, statusCode)
}

// CreateJobExpectingError creates a job and returns error response if status is not 202.
func CreateJobExpectingError(ctx context.Context, baseURL, filePath, level, jobName string, stream bool) (*ErrorResponse, int, error) {
	url := buildJobCreateURL(baseURL, level, jobName, stream)

	body, writer, err := createMultipartBody(filePath)
	if err != nil {
		return nil, 0, err
	}

	respBody, statusCode, err := doPOST(ctx, url, body, writer.FormDataContentType())
	if err != nil {
		return nil, statusCode, err
	}

	// If not accepted, parse as error response
	if statusCode != http.StatusAccepted {
		errorResp, parseErr := parseErrorResponse(respBody, statusCode)
		if parseErr != nil {
			return nil, statusCode, parseErr
		}

		return errorResp, statusCode, nil
	}

	return nil, statusCode, fmt.Errorf("unexpected success with status code %d: %s", statusCode, string(respBody))
}

// CreateJobWithTextExpectingError creates a job from text and expects an error response.
//nolint:cyclop // Test helper function, complexity acceptable
func CreateJobWithTextExpectingError(ctx context.Context, baseURL, text, level, jobName string, stream bool) (*ErrorResponse, int, error) {
	tmpFile, err := createTempTextFile(text)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		_ = os.Remove(tmpFile) //nolint:errcheck // Cleanup, error not critical
	}()

	// Use the file-based error creation
	return CreateJobExpectingError(ctx, baseURL, tmpFile, level, jobName, stream)
}

// IsResourceLockedError checks if an error is a resource locked error (409).
func IsResourceLockedError(err error) bool {
	if err == nil {
		return false
	}

	return strings.Contains(err.Error(), "409") &&
		(strings.Contains(err.Error(), "RESOURCE_LOCKED") ||
			strings.Contains(err.Error(), "locked") ||
			strings.Contains(err.Error(), "active"))
}

// IsRateLimitError checks if an error is a rate limit error (429).
func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}

	return strings.Contains(err.Error(), "429") &&
		(strings.Contains(err.Error(), "RATE_LIMIT_EXCEEDED") ||
			strings.Contains(err.Error(), "Too many"))
}

