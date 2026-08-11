package summarization

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
)

const (
	getCallTimeout  = 10 * time.Second
	postCallTimeout = 120 * time.Second // summarization calls can be slow
	pollInterval    = 5 * time.Second   // interval between job status polls
)

// TXTSummaryKeywords are representative terms from sample_txt.txt (Lorem Ipsum history).
// A valid summary is expected to mention at least one of these topics.
var TXTSummaryKeywords = []string{"lorem", "ipsum", "typesetting", "latin", "cicero", "dummy", "text", "printing"}

// PDFSummaryKeywords are representative terms from sync_test.pdf (AI overview).
// A valid summary is expected to mention at least one of these topics.
var PDFSummaryKeywords = []string{"artificial intelligence", "machine learning", "deep learning", "neural", "nlp", "computer vision", "ai", "medical"}

// SetAppRuntime is a no-op placeholder required by the test suite interface.
func SetAppRuntime(string) {}

// httpConfigForTimeout returns an HTTP client config with the given timeout.
// Connection pooling is disabled — each summarization call uses a short-lived client.
func httpConfigForTimeout(timeout time.Duration) common.HTTPClientConfig {
	return common.HTTPClientConfig{
		Timeout:            timeout,
		InsecureSkipVerify: true,
		PoolConnections:    false,
	}
}

// GetTestTXTPath returns the absolute path to sample_txt.txt.
func GetTestTXTPath() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}

	return filepath.Join(filepath.Dir(filepath.Dir(filename)), "ingestion", "docs", "sample_txt.txt")
}

// ── Async job types ───────────────────────────────────────────────────────────

// JobCreatedResponse represents the response when a summarization job is created.
type JobCreatedResponse struct {
	JobID string `json:"job_id"`
}

// JobStatus is the lifecycle state of a summarization job.
type JobStatus string

const (
	JobStatusAccepted   JobStatus = "accepted"
	JobStatusInProgress JobStatus = "in_progress"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
)

// DocumentInfo describes the document attached to a job.
type DocumentInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// JobMetadata holds chunked-summarization progress counters.
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

// JobResultResponse represents the response when getting a job result.
type JobResultResponse struct {
	Data  map[string]interface{} `json:"data"`
	Meta  map[string]interface{} `json:"meta"`
	Usage map[string]interface{} `json:"usage"`
}

// PaginationInfo holds page metadata for list responses.
type PaginationInfo struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// JobsListResponse represents the response when listing summarization jobs.
type JobsListResponse struct {
	Pagination PaginationInfo      `json:"pagination"`
	Data       []JobDetailResponse `json:"data"`
}

// HealthCheckResponse represents the health check response.
type HealthCheckResponse struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

// ErrorResponse aliases common.ErrorResponse for backward compatibility.
type ErrorResponse = common.ErrorResponse

// ── Sync summarize types — POST /v1/summarize ─────────────────────────────────

// SyncSummarizeRequest is the JSON body for POST /v1/summarize.
type SyncSummarizeRequest struct {
	Text   string `json:"text"`
	Level  string `json:"level,omitempty"`  // "brief" | "standard" | "detailed"
	Length int    `json:"length,omitempty"` // legacy word-count hint
	Stream bool   `json:"stream,omitempty"`
}

// SyncSummarizeData is the nested "data" object in the response.
type SyncSummarizeData struct {
	Summary        string `json:"summary"`
	OriginalLength int    `json:"original_length"`
	SummaryLength  int    `json:"summary_length"`
}

// SyncSummarizeResponse is the full response from POST /v1/summarize.
// Shape: {"data":{"summary":"...","original_length":N,"summary_length":N},"meta":{...},"usage":{...}}.
type SyncSummarizeResponse struct {
	Data  SyncSummarizeData      `json:"data"`
	Meta  map[string]interface{} `json:"meta,omitempty"`
	Usage map[string]interface{} `json:"usage,omitempty"`
}

// Summary returns the summary text from the response.
func (r *SyncSummarizeResponse) Summary() string {
	return r.Data.Summary
}

// ── URL helpers ───────────────────────────────────────────────────────────────

// healthURL constructs the health check endpoint URL.
func healthURL(baseURL string) string {
	return fmt.Sprintf("%s/health", baseURL)
}

// syncSummarizeURL returns the synchronous summarize endpoint URL.
func syncSummarizeURL(baseURL string) string {
	return fmt.Sprintf("%s/v1/summarize", baseURL)
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

// buildJobCreateURL builds the async job creation URL with optional query params.
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

// ── HTTP helpers ──────────────────────────────────────────────────────────────

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

// ── Service operations ────────────────────────────────────────────────────────

// HealthCheck calls GET /health and returns an error if the service is not up.
func HealthCheck(ctx context.Context, baseURL string) error {
	respBody, statusCode, err := doGET(ctx, healthURL(baseURL))
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}

	if statusCode != http.StatusOK {
		return fmt.Errorf("health check returned %d: %s", statusCode, string(respBody))
	}

	logger.Infof("[SUMMARIZE] health check passed")

	return nil
}

// createMultipartBody wraps common.BuildMultipartBody for a single file upload.
func createMultipartBody(filePath string) (*bytes.Buffer, *multipart.Writer, error) {
	return common.BuildMultipartBody("file", filePath)
}

// ── Async job helpers ─────────────────────────────────────────────────────────

// CreateJobWithFile submits an async summarization job from a file.
func CreateJobWithFile(ctx context.Context, baseURL, filePath, level, jobName string, stream bool) (*JobCreatedResponse, error) {
	body, writer, err := createMultipartBody(filePath)
	if err != nil {
		return nil, err
	}

	respBody, statusCode, err := doPOST(ctx, buildJobCreateURL(baseURL, level, jobName, stream), body, writer.FormDataContentType())
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusAccepted {
		return nil, fmt.Errorf("unexpected status %d: %s", statusCode, string(respBody))
	}

	var jobResp JobCreatedResponse
	if err := json.Unmarshal(respBody, &jobResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	logger.Infof("[SUMMARIZE] job created: %s", jobResp.JobID)

	return &jobResp, nil
}

// CreateJobWithText writes text to a temp file and submits it as an async job.
//
//nolint:cyclop
func CreateJobWithText(ctx context.Context, baseURL, text, level, jobName string, stream bool) (*JobCreatedResponse, error) {
	tmpFile, err := createTempTextFile(text)
	if err != nil {
		return nil, err
	}

	defer func() { _ = os.Remove(tmpFile) }()

	return CreateJobWithFile(ctx, baseURL, tmpFile, level, jobName, stream)
}

// createTempTextFile writes text to a temporary .txt file and returns its path.
func createTempTextFile(text string) (string, error) {
	f, err := os.CreateTemp("", "summarize-text-*.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	defer func() { _ = f.Close() }()

	if _, err := f.WriteString(text); err != nil {
		_ = os.Remove(f.Name())

		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	return f.Name(), nil
}

// JobResult pairs a completed job's detail with its extracted summary text.
type JobResult struct {
	Detail  *JobDetailResponse
	Summary string
}

// SubmitAndVerifyJob creates a job, waits for completion, and returns the result.
// Pass filePath for file-based jobs or text for text-based jobs.
func SubmitAndVerifyJob(
	ctx context.Context,
	baseURL, filePath, text, level, jobName string,
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

	time.Sleep(2 * time.Second) //nolint:mnd // let the service begin processing

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
		return nil, fmt.Errorf("job %s: missing summary in result", jobResp.JobID)
	}

	return &JobResult{Detail: detail, Summary: summary}, nil
}

// SubmitJobExpectingFailure submits a job and waits for it to reach the failed state.
// Returns the detail and the error from WaitForJobCompletion so callers can assert both.
func SubmitJobExpectingFailure(
	ctx context.Context,
	baseURL, text, level, jobName string,
	jobTimeout time.Duration,
) (*JobDetailResponse, error) {
	jobResp, err := CreateJobWithText(ctx, baseURL, text, level, jobName, false)
	if err != nil {
		return nil, fmt.Errorf("create job: %w", err)
	}

	time.Sleep(2 * time.Second) //nolint:mnd // let the service begin processing

	return WaitForJobCompletion(ctx, baseURL, jobResp.JobID, jobTimeout)
}

// GetJobDetail fetches job details from GET /v1/summarize/jobs/{id}.
func GetJobDetail(ctx context.Context, baseURL, jobID string) (*JobDetailResponse, error) {
	respBody, statusCode, err := doGET(ctx, jobURL(baseURL, jobID))
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", statusCode, string(respBody))
	}

	var detail JobDetailResponse
	if err := json.Unmarshal(respBody, &detail); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &detail, nil
}

// GetJobResult fetches the result from GET /v1/summarize/jobs/{id}/result.
func GetJobResult(ctx context.Context, baseURL, jobID string) (*JobResultResponse, error) {
	respBody, statusCode, err := doGET(ctx, jobResultURL(baseURL, jobID))
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", statusCode, string(respBody))
	}

	var result JobResultResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// WaitForJobCompletion polls job status until it reaches completed or failed.
//
//nolint:cyclop
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
				return nil, fmt.Errorf("timed out waiting for job %s", jobID)
			}

			detail, err := GetJobDetail(ctx, baseURL, jobID)
			if err != nil {
				logger.Warningf("[SUMMARIZE] get job detail failed: %v", err)

				continue
			}

			logger.Infof("[SUMMARIZE] job %s status: %s", jobID, detail.Status)

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
				return detail, fmt.Errorf("unexpected job status: %s", detail.Status)
			}
		}
	}
}

// ListJobs calls GET /v1/summarize/jobs with optional status and name filters.
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
		return nil, fmt.Errorf("unexpected status %d: %s", statusCode, string(respBody))
	}

	var list JobsListResponse
	if err := json.Unmarshal(respBody, &list); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &list, nil
}

// deleteJobWithURL sends DELETE to url and accepts 200 or 204 as success.
func deleteJobWithURL(ctx context.Context, url, logMsg string) error {
	_, statusCode, err := doDELETE(ctx, url)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d", statusCode)
	}

	logger.Infof("[SUMMARIZE] %s", logMsg)

	return nil
}

// DeleteJob deletes a single job by ID.
func DeleteJob(ctx context.Context, baseURL, jobID string) error {
	return deleteJobWithURL(ctx, jobURL(baseURL, jobID), "job deleted: "+jobID)
}

// DeleteAllJobs deletes all jobs (requires confirm=true).
func DeleteAllJobs(ctx context.Context, baseURL string) error {
	return deleteJobWithURL(ctx, jobsURL(baseURL)+"?confirm=true", "all jobs deleted")
}

// parseErrorResponse parses a non-success response body into an ErrorResponse.
func parseErrorResponse(respBody []byte, statusCode int) (*ErrorResponse, error) {
	return common.ParseErrorResponse(respBody, statusCode)
}

// CreateJobExpectingError submits a file job and returns the error response for non-202 status.
func CreateJobExpectingError(ctx context.Context, baseURL, filePath, level, jobName string, stream bool) (*ErrorResponse, int, error) {
	body, writer, err := createMultipartBody(filePath)
	if err != nil {
		return nil, 0, err
	}

	respBody, statusCode, err := doPOST(ctx, buildJobCreateURL(baseURL, level, jobName, stream), body, writer.FormDataContentType())
	if err != nil {
		return nil, statusCode, err
	}

	if statusCode != http.StatusAccepted {
		errResp, parseErr := parseErrorResponse(respBody, statusCode)
		if parseErr != nil {
			return nil, statusCode, parseErr
		}

		return errResp, statusCode, nil
	}

	return nil, statusCode, fmt.Errorf("unexpected success (status %d): %s", statusCode, string(respBody))
}

// CreateJobWithTextExpectingError writes text to a temp file and expects an error response.
//
//nolint:cyclop
func CreateJobWithTextExpectingError(ctx context.Context, baseURL, text, level, jobName string, stream bool) (*ErrorResponse, int, error) {
	tmpFile, err := createTempTextFile(text)
	if err != nil {
		return nil, 0, err
	}

	defer func() { _ = os.Remove(tmpFile) }()

	return CreateJobExpectingError(ctx, baseURL, tmpFile, level, jobName, stream)
}

// ── Error classifiers ─────────────────────────────────────────────────────────

// IsContextLimitError reports whether err is a 413 CONTEXT_LIMIT_EXCEEDED response.
// Tests encountering this should call ginkgo.Skip rather than fail.
func IsContextLimitError(err error) bool {
	if err == nil {
		return false
	}

	s := err.Error()

	return strings.Contains(s, "413") && strings.Contains(s, "CONTEXT_LIMIT_EXCEEDED")
}

// ── Concurrency helpers — POST /v1/summarize ──────────────────────────────────

// ConcurrentResult holds the outcome of one request in a concurrent load run.
type ConcurrentResult struct {
	Index      int           // 1-based request number
	StatusCode int           // HTTP response status
	Latency    time.Duration // round-trip duration
	Err        error         // non-nil on transport failure
}

// RunConcurrentSummarizeText fires n goroutines simultaneously against POST /v1/summarize
// with a JSON text body. All goroutines are released at once via a start gate,
// mirroring: seq N | parallel -j N make_request {}.
func RunConcurrentSummarizeText(ctx context.Context, baseURL, text, level string, n int) []ConcurrentResult {
	results := make([]ConcurrentResult, n)

	var wg sync.WaitGroup

	start := make(chan struct{})

	for i := range n {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()
			<-start

			payload, _ := json.Marshal(SyncSummarizeRequest{Text: text, Level: level}) //nolint:errcheck

			t0 := time.Now()
			_, statusCode, err := doPOST(ctx, syncSummarizeURL(baseURL), bytes.NewBuffer(payload), "application/json")
			results[idx] = ConcurrentResult{
				Index:      idx + 1,
				StatusCode: statusCode,
				Latency:    time.Since(t0),
				Err:        err,
			}
			logger.Infof("[SUMMARIZE-CONCURRENT] request #%d status=%d latency=%s",
				idx+1, statusCode, results[idx].Latency.Round(time.Millisecond))
		}(i)
	}

	close(start) // release all goroutines simultaneously
	wg.Wait()

	return results
}

// RunConcurrentSummarizeFile fires n goroutines simultaneously against POST /v1/summarize
// with a multipart file upload. Use a small file (e.g. sync_test.pdf) to avoid 413 errors.
func RunConcurrentSummarizeFile(ctx context.Context, baseURL, filePath, level string, n int) []ConcurrentResult {
	results := make([]ConcurrentResult, n)

	var wg sync.WaitGroup

	start := make(chan struct{})

	for i := range n {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()
			<-start

			fields := map[string]string{}
			if level != "" {
				fields["level"] = level
			}

			t0 := time.Now()
			body, writer, err := common.BuildMultipartBodyWithFields("file", filePath, fields)

			if err != nil {
				results[idx] = ConcurrentResult{Index: idx + 1, Err: err}

				return
			}

			_, statusCode, err := doPOST(ctx, syncSummarizeURL(baseURL), body, writer.FormDataContentType())
			results[idx] = ConcurrentResult{
				Index:      idx + 1,
				StatusCode: statusCode,
				Latency:    time.Since(t0),
				Err:        err,
			}
			logger.Infof("[SUMMARIZE-CONCURRENT] file request #%d status=%d latency=%s",
				idx+1, statusCode, results[idx].Latency.Round(time.Millisecond))
		}(i)
	}

	close(start)
	wg.Wait()

	return results
}

// ── Sync summarize API helpers — POST /v1/summarize ───────────────────────────

// postJSONSummarize is the shared path for all JSON-body sync callers.
// It marshals req, POSTs it, and unmarshals a 200 response.
func postJSONSummarize(ctx context.Context, baseURL string, req SyncSummarizeRequest) (*SyncSummarizeResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	respBody, statusCode, err := doPOST(ctx, syncSummarizeURL(baseURL), bytes.NewBuffer(payload), "application/json")
	if err != nil {
		return nil, err
	}

	var resp SyncSummarizeResponse
	if err := common.ValidateStatusAndUnmarshal(respBody, statusCode, http.StatusOK, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// SummarizeText calls POST /v1/summarize with a JSON body.
// level: "brief" | "standard" | "detailed" — empty defaults to standard.
func SummarizeText(ctx context.Context, baseURL, text, level string) (*SyncSummarizeResponse, error) {
	resp, err := postJSONSummarize(ctx, baseURL, SyncSummarizeRequest{Text: text, Level: level})
	if err != nil {
		return nil, err
	}

	logger.Infof("[SUMMARIZE] text summarized (level=%s len=%d)", level, len(resp.Summary()))

	return resp, nil
}

// SummarizeTextWithLength calls POST /v1/summarize using the legacy "length" field.
func SummarizeTextWithLength(ctx context.Context, baseURL, text string, length int) (*SyncSummarizeResponse, error) {
	resp, err := postJSONSummarize(ctx, baseURL, SyncSummarizeRequest{Text: text, Length: length})
	if err != nil {
		return nil, err
	}

	logger.Infof("[SUMMARIZE] text summarized (length=%d summary_len=%d)", length, len(resp.Summary()))

	return resp, nil
}


// SummarizeFile calls POST /v1/summarize with a multipart file upload.
// level is an optional form field ("brief" | "standard" | "detailed").
func SummarizeFile(ctx context.Context, baseURL, filePath, level string) (*SyncSummarizeResponse, error) {
	fields := map[string]string{}
	if level != "" {
		fields["level"] = level
	}

	body, writer, err := common.BuildMultipartBodyWithFields("file", filePath, fields)
	if err != nil {
		return nil, err
	}

	respBody, statusCode, err := doPOST(ctx, syncSummarizeURL(baseURL), body, writer.FormDataContentType())
	if err != nil {
		return nil, err
	}

	var resp SyncSummarizeResponse
	if err := common.ValidateStatusAndUnmarshal(respBody, statusCode, http.StatusOK, &resp); err != nil {
		return nil, err
	}

	logger.Infof("[SUMMARIZE] file summarized (file=%s level=%s len=%d)",
		filepath.Base(filePath), level, len(resp.Summary()))

	return &resp, nil
}

// SummarizeFileWithLength calls POST /v1/summarize with a multipart file upload
// using the legacy "length" form field.
func SummarizeFileWithLength(ctx context.Context, baseURL, filePath string, length int) (*SyncSummarizeResponse, error) {
	fields := map[string]string{"length": fmt.Sprintf("%d", length)}

	body, writer, err := common.BuildMultipartBodyWithFields("file", filePath, fields)
	if err != nil {
		return nil, err
	}

	respBody, statusCode, err := doPOST(ctx, syncSummarizeURL(baseURL), body, writer.FormDataContentType())
	if err != nil {
		return nil, err
	}

	var resp SyncSummarizeResponse
	if err := common.ValidateStatusAndUnmarshal(respBody, statusCode, http.StatusOK, &resp); err != nil {
		return nil, err
	}

	logger.Infof("[SUMMARIZE] file summarized (file=%s length=%d summary_len=%d)",
		filepath.Base(filePath), length, len(resp.Summary()))

	return &resp, nil
}

// SummarizeTextExpectingError posts a JSON body and expects a non-200 error response.
func SummarizeTextExpectingError(ctx context.Context, baseURL, text, level string) (*ErrorResponse, int, error) {
	payload, err := json.Marshal(SyncSummarizeRequest{Text: text, Level: level})
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request: %w", err)
	}

	respBody, statusCode, err := doPOST(ctx, syncSummarizeURL(baseURL), bytes.NewBuffer(payload), "application/json")
	if err != nil {
		return nil, statusCode, err
	}

	errResp, parseErr := common.ExpectErrorResponse(respBody, statusCode, http.StatusOK)
	if parseErr != nil {
		return nil, statusCode, parseErr
	}

	return errResp, statusCode, nil
}

// SummarizeFileExpectingError posts a multipart file and expects a non-200 error response.
func SummarizeFileExpectingError(ctx context.Context, baseURL, filePath, level string) (*ErrorResponse, int, error) {
	fields := map[string]string{}
	if level != "" {
		fields["level"] = level
	}

	body, writer, err := common.BuildMultipartBodyWithFields("file", filePath, fields)
	if err != nil {
		return nil, 0, err
	}

	respBody, statusCode, err := doPOST(ctx, syncSummarizeURL(baseURL), body, writer.FormDataContentType())
	if err != nil {
		return nil, statusCode, err
	}

	errResp, parseErr := common.ExpectErrorResponse(respBody, statusCode, http.StatusOK)
	if parseErr != nil {
		return nil, statusCode, parseErr
	}

	return errResp, statusCode, nil
}

// SummarizeRawBody posts an arbitrary body to POST /v1/summarize.
// Used for wrong content-type, malformed JSON, and empty-request error tests.
func SummarizeRawBody(ctx context.Context, baseURL string, body *bytes.Buffer, contentType string) ([]byte, int, error) {
	return doPOST(ctx, syncSummarizeURL(baseURL), body, contentType)
}