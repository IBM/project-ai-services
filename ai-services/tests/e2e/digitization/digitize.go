package digitization

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

var GET_CALL_TIMEOUT = 10 * time.Second
var POST_CALL_TIMEOUT = 60 * time.Second
var DOC_CALL_TIMEOUT = 30 * time.Second

// appRuntime holds the current runtime environment (podman or openshift).
var appRuntime string

// SetAppRuntime sets the application runtime for the digitize package.
func SetAppRuntime(runtime string) {
	appRuntime = runtime
}

// getHTTPClient returns an HTTP client configured based on the runtime.
// For OpenShift, it skips TLS certificate verification.
func getHTTPClient(timeout time.Duration) *http.Client {
	client := &http.Client{Timeout: timeout}

	if appRuntime == "openshift" {
		// Skip TLS certificate verification for OpenShift
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	return client
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

// DocumentListItem represents a document in the list.
type DocumentListItem struct {
	ID           string                 `json:"id"`
	JobID        string                 `json:"job_id"`
	Name         string                 `json:"name"`
	Type         string                 `json:"type"`
	Status       string                 `json:"status"`
	OutputFormat string                 `json:"output_format"`
	SubmittedAt  string                 `json:"submitted_at"`
	CompletedAt  *string                `json:"completed_at"`
	Error        interface{}            `json:"error"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// DocumentsListResponse represents the response when listing documents.
type DocumentsListResponse struct {
	Data       []DocumentListItem `json:"data"`
	Pagination PaginationInfo     `json:"pagination"`
}

// DocumentDetailResponse represents detailed document information.
type DocumentDetailResponse struct {
	ID           string                 `json:"id"`
	JobID        string                 `json:"job_id"`
	Name         string                 `json:"name"`
	Type         string                 `json:"type"`
	Status       string                 `json:"status"`
	OutputFormat string                 `json:"output_format"`
	SubmittedAt  string                 `json:"submitted_at"`
	CompletedAt  *string                `json:"completed_at"`
	Error        interface{}            `json:"error"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// DocumentContentResponse represents the document content.
type DocumentContentResponse struct {
	OutputFormat string      `json:"output_format"`
	Result       interface{} `json:"result"`
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

// GetDigitizeBaseURL returns the base URL for the digitize service.
func GetDigitizeBaseURL(port string) string {
	return fmt.Sprintf("http://localhost:%s", port)
}

// HealthCheck performs a health check on the digitize service.
func HealthCheck(ctx context.Context, baseURL string) error {
	url := fmt.Sprintf("%s/health", baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	client := getHTTPClient(GET_CALL_TIMEOUT)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		return fmt.Errorf("health check failed with status %d: %s", resp.StatusCode, string(body))
	}

	logger.Infof("[DIGITIZE] Health check passed")

	return nil
}

// buildJobURL constructs the job creation URL with query parameters.
func buildJobURL(baseURL, operation, outputFormat, jobName string) string {
	url := fmt.Sprintf("%s/v1/jobs?operation=%s&output_format=%s", baseURL, operation, outputFormat)
	if jobName != "" {
		url += fmt.Sprintf("&job_name=%s", jobName)
	}

	return url
}

// createMultipartBody creates a multipart form body with a single file.
func createMultipartBody(filePath string) (*bytes.Buffer, *multipart.Writer, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("files", filepath.Base(filePath))
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

	client := getHTTPClient(POST_CALL_TIMEOUT)
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
	url := fmt.Sprintf("%s/v1/jobs/%s", baseURL, jobID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(GET_CALL_TIMEOUT)
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

	var jobStatus JobStatusResponse
	if err := json.Unmarshal(body, &jobStatus); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
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
	case "in_progress":
		return nil, nil, false
	default:
		return status, fmt.Errorf("unknown job status: %s", status.Status), true
	}
}

// WaitForJobCompletion waits for a job to complete.
func WaitForJobCompletion(ctx context.Context, baseURL, jobID string, timeout time.Duration) (*JobStatusResponse, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("timeout waiting for job completion")
			}

			status, err := GetJobStatus(ctx, baseURL, jobID)
			if err != nil {
				logger.Warningf("[DIGITIZE] Failed to get job status: %v", err)

				continue
			}

			result, resultErr, done := handleJobStatus(status, jobID)
			if done {
				return result, resultErr
			}
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

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(GET_CALL_TIMEOUT)
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
	url := fmt.Sprintf("%s/v1/jobs/%s", baseURL, jobID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(GET_CALL_TIMEOUT)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)

		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	logger.Infof("[DIGITIZE] Job deleted: %s", jobID)

	return nil
}

// ListDocuments retrieves a list of documents with optional status and name filters.
// Pass empty strings for status and name to list all documents without filters.
func ListDocuments(ctx context.Context, baseURL string, limit, offset int, status, name string) (*DocumentsListResponse, error) {
	url := fmt.Sprintf("%s/v1/documents?limit=%d&offset=%d", baseURL, limit, offset)
	if status != "" {
		url += fmt.Sprintf("&status=%s", status)
	}
	if name != "" {
		url += fmt.Sprintf("&name=%s", name)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(GET_CALL_TIMEOUT)
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

	var docsList DocumentsListResponse
	if err := json.Unmarshal(body, &docsList); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &docsList, nil
}

// GetDocument retrieves detailed information about a specific document.
func GetDocument(ctx context.Context, baseURL, docID string) (*DocumentDetailResponse, error) {
	url := fmt.Sprintf("%s/v1/documents/%s", baseURL, docID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(GET_CALL_TIMEOUT)
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

	var doc DocumentDetailResponse
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &doc, nil
}

// GetDocumentContent retrieves the content of a specific document.
func GetDocumentContent(ctx context.Context, baseURL, docID string) (*DocumentContentResponse, error) {
	url := fmt.Sprintf("%s/v1/documents/%s/content", baseURL, docID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(DOC_CALL_TIMEOUT)
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

	var content DocumentContentResponse
	if err := json.Unmarshal(body, &content); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &content, nil
}

// DeleteDocument deletes a specific document.
func DeleteDocument(ctx context.Context, baseURL, docID string) error {
	url := fmt.Sprintf("%s/v1/documents/%s", baseURL, docID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(GET_CALL_TIMEOUT)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)

		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	logger.Infof("[DIGITIZE] Document deleted: %s", docID)

	return nil
}

// DeleteAllDocuments deletes all documents.
func DeleteAllDocuments(ctx context.Context, baseURL string) error {
	url := fmt.Sprintf("%s/v1/documents?confirm=true", baseURL)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(DOC_CALL_TIMEOUT)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)

		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	logger.Infof("[DIGITIZE] All documents deleted")

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

	// If not accepted, parse as error response
	if statusCode != http.StatusAccepted {
		return parseErrorResponse(respBody, statusCode)
	}

	return nil, fmt.Errorf("unexpected success with status code %d: %s", statusCode, string(respBody))
}

// GetJobStatusExpectingError retrieves job status and returns error response if status is not 200.
func GetJobStatusExpectingError(ctx context.Context, baseURL, jobID string) (*ErrorResponse, error) {
	url := fmt.Sprintf("%s/v1/jobs/%s", baseURL, jobID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(GET_CALL_TIMEOUT)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// If not OK, parse as error response
	if resp.StatusCode != http.StatusOK {
		return parseErrorResponse(body, resp.StatusCode)
	}

	return nil, fmt.Errorf("unexpected success with status code %d: %s", resp.StatusCode, string(body))
}

// GetDocumentExpectingError retrieves document details and returns error response if status is not 200.
func GetDocumentExpectingError(ctx context.Context, baseURL, docID string) (*ErrorResponse, error) {
	url := fmt.Sprintf("%s/v1/documents/%s", baseURL, docID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(GET_CALL_TIMEOUT)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// If not OK, parse as error response
	if resp.StatusCode != http.StatusOK {
		return parseErrorResponse(body, resp.StatusCode)
	}

	return nil, fmt.Errorf("unexpected success with status code %d: %s", resp.StatusCode, string(body))
}

// GetDocumentContentExpectingError retrieves document content and returns error response if status is not 200.
func GetDocumentContentExpectingError(ctx context.Context, baseURL, docID string) (*ErrorResponse, error) {
	url := fmt.Sprintf("%s/v1/documents/%s/content", baseURL, docID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(DOC_CALL_TIMEOUT)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// If not OK, parse as error response
	if resp.StatusCode != http.StatusOK {
		return parseErrorResponse(body, resp.StatusCode)
	}

	return nil, fmt.Errorf("unexpected success with status code %d: %s", resp.StatusCode, string(body))
}

// DeleteJobExpectingError deletes a job and returns error response if status is not 200/204.
func DeleteJobExpectingError(ctx context.Context, baseURL, jobID string) (*ErrorResponse, error) {
	url := fmt.Sprintf("%s/v1/jobs/%s", baseURL, jobID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(GET_CALL_TIMEOUT)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// If not OK or NoContent, parse as error response
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return parseErrorResponse(body, resp.StatusCode)
	}

	return nil, fmt.Errorf("unexpected success with status code %d: %s", resp.StatusCode, string(body))
}

// DeleteDocumentExpectingError deletes a document and returns error response if status is not 200/204.
func DeleteDocumentExpectingError(ctx context.Context, baseURL, docID string) (*ErrorResponse, error) {
	url := fmt.Sprintf("%s/v1/documents/%s", baseURL, docID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := getHTTPClient(GET_CALL_TIMEOUT)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// If not OK or NoContent, parse as error response
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return parseErrorResponse(body, resp.StatusCode)
	}

	return nil, fmt.Errorf("unexpected success with status code %d: %s", resp.StatusCode, string(body))
}

// createMultipartBodyWithMultipleFiles creates a multipart form body with multiple files.
func createMultipartBodyWithMultipleFiles(filePaths []string) (*bytes.Buffer, *multipart.Writer, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add multiple files
	for _, filePath := range filePaths {
		file, err := os.Open(filePath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
		}
		defer func() { _ = file.Close() }()

		part, err := writer.CreateFormFile("files", filepath.Base(filePath))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create form file: %w", err)
		}

		if _, err := io.Copy(part, file); err != nil {
			return nil, nil, fmt.Errorf("failed to copy file: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, nil, fmt.Errorf("failed to close writer: %w", err)
	}

	return body, writer, nil
}

// CreateJobWithMultipleFiles attempts to create a job with multiple files (should fail for digitization).
func CreateJobWithMultipleFiles(ctx context.Context, baseURL string, filePaths []string, operation, outputFormat, jobName string) (*ErrorResponse, error) {
	url := buildJobURL(baseURL, operation, outputFormat, jobName)

	body, writer, err := createMultipartBodyWithMultipleFiles(filePaths)
	if err != nil {
		return nil, err
	}

	respBody, statusCode, err := sendJobRequest(ctx, url, body, writer.FormDataContentType())
	if err != nil {
		return nil, err
	}

	// For this test, we expect a 400 error
	if statusCode == http.StatusBadRequest {
		return parseErrorResponse(respBody, statusCode)
	}

	return nil, fmt.Errorf("unexpected status code %d: %s", statusCode, string(respBody))
}

// Made with Bob
