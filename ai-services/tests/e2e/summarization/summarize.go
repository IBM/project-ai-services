package summarization

import (
	"bytes"
	"context"
	"crypto/tls"
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
)

const (
	getCallTimeout  = 10 * time.Second
	postCallTimeout = 120 * time.Second // Longer timeout for summarization
	pollInterval    = 5 * time.Second   // Polling interval for job status
)

// getHTTPClient returns an HTTP client configured based on the runtime.
// Skips TLS certificate verification for both Podman (nip.io self-signed certs) and OpenShift.
func getHTTPClient(timeout time.Duration) *http.Client {
	// Skip TLS certificate verification for both podman (nip.io) and openshift
	// Podman uses self-signed certificates with nip.io domains
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
}

// GetTestPDFPath returns the path to a test PDF file.
func GetTestPDFPath() string {
	// Get the path to the test PDF from the ingestion test docs
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}

	// Navigate to ingestion/docs/test_doc.pdf
	testDir := filepath.Dir(filename)
	testPDFPath := filepath.Join(filepath.Dir(testDir), "ingestion", "docs", "test_doc.pdf")

	return testPDFPath
}

// GetTestTXTPath returns the path to a test TXT file.
func GetTestTXTPath() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}

	testDir := filepath.Dir(filename)
	testTXTPath := filepath.Join(filepath.Dir(testDir), "ingestion", "docs", "sample_txt.txt")

	return testTXTPath
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

// ErrorResponse represents an error response from the API.
type ErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`    // HTTP status code as number
		Message string `json:"message"` // Error message
		Status  string `json:"status"`  // Error status string (e.g., "UNSUPPORTED_FILE_TYPE")
	} `json:"error,omitempty"`
}

// GetSummarizeBaseURL returns the base URL for the summarize service.
func GetSummarizeBaseURL(port string) string {
	return fmt.Sprintf("http://localhost:%s", port)
}

// HealthCheck performs a health check on the summarize service.
func HealthCheck(ctx context.Context, baseURL string) error {
	url := fmt.Sprintf("%s/health", baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	client := getHTTPClient(getCallTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		return fmt.Errorf("health check failed with status %d: %s", resp.StatusCode, string(body))
	}

	logger.Infof("[SUMMARIZE] Health check passed")

	return nil
}

// buildJobURL constructs the job creation URL with query parameters.
func buildJobURL(baseURL, level, jobName string, stream bool) string {
	url := fmt.Sprintf("%s/v1/summarize/jobs?stream=%t", baseURL, stream)
	if level != "" {
		url += fmt.Sprintf("&level=%s", level)
	}
	if jobName != "" {
		url += fmt.Sprintf("&job_name=%s", jobName)
	}

	return url
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

// sendJobRequest sends the HTTP request and returns the response body.
func sendJobRequest(ctx context.Context, url string, body *bytes.Buffer, contentType string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	client := getHTTPClient(postCallTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// CreateJobWithFile creates a new summarization job with a file upload.
func CreateJobWithFile(ctx context.Context, baseURL, filePath, level, jobName string, stream bool) (*JobCreatedResponse, error) {
	url := buildJobURL(baseURL, level, jobName, stream)

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

	logger.Infof("[SUMMARIZE] Job created: %s", jobResp.JobID)
	return &jobResp, nil
}

// CreateJobWithText creates a new summarization job with text input.
// Note: The API requires a file upload, so we create a temporary text file.
//nolint:cyclop // Test helper function, complexity acceptable
func CreateJobWithText(ctx context.Context, baseURL, text, level, jobName string, stream bool) (*JobCreatedResponse, error) {
	// Create temporary file
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

// createJobRequest creates and sends a job creation request.
func createJobRequest(ctx context.Context, url, filePath string) (*http.Response, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		_ = file.Close() //nolint:errcheck // Cleanup, error not critical
	}()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("failed to copy file content: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := getHTTPClient(postCallTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(respBody))
	}

	var jobResp JobCreatedResponse
	if err := json.Unmarshal(respBody, &jobResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	logger.Infof("[SUMMARIZE] Job created: %s", jobResp.JobID)
	return &jobResp, nil
}

// GetJobDetail retrieves the details of a specific job.
func GetJobDetail(ctx context.Context, baseURL, jobID string) (*JobDetailResponse, error) {
	url := fmt.Sprintf("%s/v1/summarize/jobs/%s", baseURL, jobID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(getCallTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var jobDetail JobDetailResponse
	if err := json.Unmarshal(body, &jobDetail); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &jobDetail, nil
}

// GetJobResult retrieves the result of a completed job.
func GetJobResult(ctx context.Context, baseURL, jobID string) (*JobResultResponse, error) {
	url := fmt.Sprintf("%s/v1/summarize/jobs/%s/result", baseURL, jobID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(getCallTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var result JobResultResponse
	if err := json.Unmarshal(body, &result); err != nil {
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

			if detail.Status == JobStatusCompleted {
				return detail, nil
			}

			if detail.Status == JobStatusFailed {
				errMsg := "unknown error"
				if detail.Error != nil {
					errMsg = *detail.Error
				}
				return detail, fmt.Errorf("job failed: %s", errMsg)
			case JobStatusAccepted, JobStatusInProgress:
				// Continue waiting
				continue
			default:
				return detail, fmt.Errorf("unknown job status: %s", detail.Status)
			}
		}
	}
}

// ListJobs retrieves a list of all jobs.
func ListJobs(ctx context.Context, baseURL string, limit, offset int, status, jobName string) (*JobsListResponse, error) {
	url := fmt.Sprintf("%s/v1/summarize/jobs?limit=%d&offset=%d", baseURL, limit, offset)
	if status != "" {
		url += fmt.Sprintf("&status=%s", status)
	}
	if jobName != "" {
		url += fmt.Sprintf("&job_name=%s", jobName)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(getCallTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var jobsList JobsListResponse
	if err := json.Unmarshal(body, &jobsList); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &jobsList, nil
}

// DeleteJob deletes a specific job.
func DeleteJob(ctx context.Context, baseURL, jobID string) error {
	url := fmt.Sprintf("%s/v1/summarize/jobs/%s", baseURL, jobID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(getCallTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	logger.Infof("[SUMMARIZE] Job deleted: %s", jobID)
	return nil
}

// DeleteAllJobs deletes all jobs.
func DeleteAllJobs(ctx context.Context, baseURL string) error {
	url := fmt.Sprintf("%s/v1/summarize/jobs?confirm=true", baseURL)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(getCallTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	logger.Infof("[SUMMARIZE] All jobs deleted")
	return nil
}

// parseErrorResponse parses the response body as an error response.
func parseErrorResponse(respBody []byte, statusCode int) (*ErrorResponse, error) {
	var errorResp ErrorResponse
	if err := json.Unmarshal(respBody, &errorResp); err != nil {
		return nil, fmt.Errorf("failed to parse error response (status %d): %w, body: %s", statusCode, err, string(respBody))
	}
	return &errorResp, nil
}

// CreateJobExpectingError creates a job and returns error response if status is not 202.
func CreateJobExpectingError(ctx context.Context, baseURL, filePath, level, jobName string, stream bool) (*ErrorResponse, int, error) {
	url := buildJobURL(baseURL, level, jobName, stream)

	body, writer, err := createMultipartBody(filePath)
	if err != nil {
		return nil, 0, err
	}

	respBody, statusCode, err := sendJobRequest(ctx, url, body, writer.FormDataContentType())
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

// CreateJobWithTextExpectingError creates a job with text and returns error response if status is not 202.
// Note: The API requires a file upload, so we create a temporary text file.
//nolint:cyclop // Test helper function, complexity acceptable
func CreateJobWithTextExpectingError(ctx context.Context, baseURL, text, level, jobName string, stream bool) (*ErrorResponse, int, error) {
	// Create temporary file
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

// createJobErrorRequest creates and sends a job creation request expecting an error.
func createJobErrorRequest(ctx context.Context, url, filePath string) (*http.Response, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		_ = file.Close() //nolint:errcheck // Cleanup, error not critical
	}()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("failed to copy file content: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := getHTTPClient(postCallTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	// If not accepted, parse as error response
	if resp.StatusCode != http.StatusAccepted {
		errorResp, parseErr := parseErrorResponse(respBody, resp.StatusCode)
		if parseErr != nil {
			return nil, resp.StatusCode, parseErr
		}
		return errorResp, resp.StatusCode, nil
	}

	return nil, resp.StatusCode, fmt.Errorf("unexpected success with status code %d: %s", resp.StatusCode, string(respBody))
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