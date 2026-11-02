package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
)

const (
	getCallTimeout  = 10 * time.Second
	postCallTimeout = 120 * time.Second // translation calls can be slow
	pollInterval    = 5 * time.Second   // interval between job status polls
)

// ── Fixture path helpers ──────────────────────────────────────────────────────

// fixturePath returns the absolute path to a file in the translation/docs fixture directory.
func fixturePath(name string) string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}

	return filepath.Join(filepath.Dir(filename), "docs", name)
}

// GetGermanTXTPath returns the absolute path to german_text.txt.
func GetGermanTXTPath() string { return fixturePath("german_text.txt") }

// GetTestMDPath returns the absolute path to test.md.
func GetTestMDPath() string { return fixturePath("test.md") }

// GetPDFPath returns the absolute path to document.pdf (unsupported type fixture).
func GetPDFPath() string { return fixturePath("document.pdf") }

// GetDOCXPath returns the absolute path to document.docx (unsupported type fixture).
func GetDOCXPath() string { return fixturePath("document.docx") }

// ── Response types ────────────────────────────────────────────────────────────

// JobCreatedResponse is the 202 body returned when a translation job is accepted.
type JobCreatedResponse struct {
	JobID string `json:"job_id"`
}

// ChunkingInfo holds chunking progress details from job_metadata.
type ChunkingInfo struct {
	ChunkCount int    `json:"chunk_count"`
	Phase      string `json:"phase"`
}

// JobMetadata holds translation job progress counters.
type JobMetadata struct {
	Chunking ChunkingInfo `json:"chunking"`
	Phase    string       `json:"phase"`
}

// JobDetailResponse is the body returned by GET /v1/translate/jobs/{job_id}.
type JobDetailResponse struct {
	JobID       string           `json:"job_id"`
	JobName     *string          `json:"job_name,omitempty"`
	Status      common.JobStatus `json:"status"`
	SubmittedAt string           `json:"submitted_at"`
	CompletedAt *string          `json:"completed_at"`
	Error       *string          `json:"error"`
	Metadata    *JobMetadata     `json:"job_metadata,omitempty"`
}

// TranslationData is the nested "data" object in a translation response.
type TranslationData struct {
	Translation         string `json:"translation"`
	SourceLanguage      string `json:"source_language"`
	TargetLanguage      string `json:"target_language"`
	OriginalWordCount   int    `json:"original_word_count"`
	TranslatedWordCount int    `json:"translated_word_count"`
}

// UsageInfo contains token usage counters.
type UsageInfo struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// JobsListResponse is the body returned by GET /v1/translate/jobs.
type JobsListResponse struct {
	Pagination common.PaginationInfo `json:"pagination"`
	Data       []JobDetailResponse   `json:"data"`
}

// HealthCheckResponse is the body returned by GET /health.
type HealthCheckResponse struct {
	Status string `json:"status"`
}

// ErrorResponse is the translate-service error envelope.
// Unlike common.ErrorResponse, the translate API returns a string error code
// (e.g. "EMPTY_INPUT") and a numeric HTTP status:
// {"error":{"code":"EMPTY_INPUT","message":"...","status":400}}.
type ErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Status  int    `json:"status"`
	} `json:"error,omitempty"`
}

// SyncTranslateRequest is the JSON body for POST /v1/translate.
type SyncTranslateRequest struct {
	Text           string `json:"text"`
	SourceLanguage string `json:"source_language,omitempty"`
	TargetLanguage string `json:"target_language"`
}

// SyncTranslateResponse is the full 200 response from POST /v1/translate.
type SyncTranslateResponse struct {
	Data  TranslationData `json:"data"`
	Usage UsageInfo       `json:"usage"`
}

// Translation returns the translated text from the response.
func (r *SyncTranslateResponse) Translation() string {
	return r.Data.Translation
}

// ── URL helpers ───────────────────────────────────────────────────────────────

func healthURL(baseURL string) string { return baseURL + "/health" }

func syncTranslateURL(baseURL string) string { return baseURL + "/v1/translate" }

func jobsURL(baseURL string) string { return baseURL + "/v1/translate/jobs" }

func jobURL(baseURL, jobID string) string { return jobsURL(baseURL) + "/" + jobID }

func jobResultURL(baseURL, jobID string) string { return jobURL(baseURL, jobID) + "/result" }

func jobResultDownloadURL(baseURL, jobID string) string {
	return jobResultURL(baseURL, jobID) + "/download"
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func doGET(ctx context.Context, url string) ([]byte, int, error) {
	return common.DoGET(ctx, url, common.HTTPConfigForTimeout(getCallTimeout))
}

func doPOST(ctx context.Context, url string, body *bytes.Buffer, contentType string) ([]byte, int, error) {
	return common.DoPOST(ctx, url, body, contentType, common.HTTPConfigForTimeout(postCallTimeout))
}

// expectError parses body as an ErrorResponse. It returns an error when statusCode
// equals successStatus, because the caller expected the request to fail.
func expectError(body []byte, statusCode, successStatus int) (*ErrorResponse, error) {
	if statusCode == successStatus {
		return nil, fmt.Errorf("unexpected success with status code %d: %s", statusCode, string(body))
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(body, &errResp); err != nil {
		return nil, fmt.Errorf("failed to parse error response (status %d): %w, body: %s", statusCode, err, string(body))
	}

	return &errResp, nil
}

// ── Service operations ────────────────────────────────────────────────────────

// HealthCheck calls GET /health and returns an error unless the service reports status "ok".
func HealthCheck(ctx context.Context, baseURL string) error {
	health, err := common.GetJSON[HealthCheckResponse](ctx, healthURL(baseURL), common.HTTPConfigForTimeout(getCallTimeout))
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	if health.Status != "ok" {
		return fmt.Errorf("unexpected health status %q", health.Status)
	}

	logger.Infof("[TRANSLATE] health check passed")

	return nil
}

// ── Sync translate — POST /v1/translate ──────────────────────────────────────

// postSync posts a sync translation request. An empty sourceLang is omitted from the body.
func postSync(ctx context.Context, baseURL, text, sourceLang, targetLang string) ([]byte, int, error) {
	payload, err := json.Marshal(SyncTranslateRequest{Text: text, SourceLanguage: sourceLang, TargetLanguage: targetLang})
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request: %w", err)
	}

	return doPOST(ctx, syncTranslateURL(baseURL), bytes.NewBuffer(payload), "application/json")
}

// TranslateText calls POST /v1/translate and expects 200.
func TranslateText(ctx context.Context, baseURL, text, sourceLang, targetLang string) (*SyncTranslateResponse, error) {
	respBody, statusCode, err := postSync(ctx, baseURL, text, sourceLang, targetLang)
	if err != nil {
		return nil, err
	}

	var resp SyncTranslateResponse
	if err := common.ValidateStatusAndUnmarshal(respBody, statusCode, http.StatusOK, &resp); err != nil {
		return nil, err
	}

	logger.Infof("[TRANSLATE] text translated (source=%s target=%s len=%d)", sourceLang, targetLang, len(resp.Translation()))

	return &resp, nil
}

// TranslateTextExpectingError calls POST /v1/translate and expects a non-200 error response.
func TranslateTextExpectingError(ctx context.Context, baseURL, text, sourceLang, targetLang string) (*ErrorResponse, int, error) {
	respBody, statusCode, err := postSync(ctx, baseURL, text, sourceLang, targetLang)
	if err != nil {
		return nil, statusCode, err
	}

	errResp, err := expectError(respBody, statusCode, http.StatusOK)

	return errResp, statusCode, err
}

// TranslateRawBody posts an arbitrary body to POST /v1/translate.
// Used for malformed JSON and missing-field tests that bypass the typed request.
func TranslateRawBody(ctx context.Context, baseURL string, body *bytes.Buffer, contentType string) ([]byte, int, error) {
	return doPOST(ctx, syncTranslateURL(baseURL), body, contentType)
}

// ── Async jobs — /v1/translate/jobs ──────────────────────────────────────────

// postJob uploads filePath as a multipart job. Empty language and job_name values are omitted.
func postJob(ctx context.Context, baseURL, filePath, sourceLang, targetLang, jobName string) ([]byte, int, error) {
	fields := map[string]string{
		"source_language": sourceLang,
		"target_language": targetLang,
		"job_name":        jobName,
	}
	for key, val := range fields {
		if val == "" {
			delete(fields, key)
		}
	}

	body, writer, err := common.BuildMultipartBodyWithFields("file", filePath, fields)
	if err != nil {
		return nil, 0, err
	}

	return doPOST(ctx, jobsURL(baseURL), body, writer.FormDataContentType())
}

// CreateJob submits an async translation job from a file and expects 202.
func CreateJob(ctx context.Context, baseURL, filePath, sourceLang, targetLang, jobName string) (*JobCreatedResponse, error) {
	respBody, statusCode, err := postJob(ctx, baseURL, filePath, sourceLang, targetLang, jobName)
	if err != nil {
		return nil, err
	}

	var jobResp JobCreatedResponse
	if err := common.ValidateStatusAndUnmarshal(respBody, statusCode, http.StatusAccepted, &jobResp); err != nil {
		return nil, err
	}

	logger.Infof("[TRANSLATE] job created: %s", jobResp.JobID)

	return &jobResp, nil
}

// CreateJobExpectingError submits a file job and expects a non-202 error response.
func CreateJobExpectingError(ctx context.Context, baseURL, filePath, sourceLang, targetLang, jobName string) (*ErrorResponse, int, error) {
	respBody, statusCode, err := postJob(ctx, baseURL, filePath, sourceLang, targetLang, jobName)
	if err != nil {
		return nil, statusCode, err
	}

	errResp, err := expectError(respBody, statusCode, http.StatusAccepted)

	return errResp, statusCode, err
}

// GetJobDetail fetches job details from GET /v1/translate/jobs/{id} and expects 200.
func GetJobDetail(ctx context.Context, baseURL, jobID string) (*JobDetailResponse, error) {
	return common.GetJSON[JobDetailResponse](ctx, jobURL(baseURL, jobID), common.HTTPConfigForTimeout(getCallTimeout))
}

// GetJobDetailRaw fetches GET /v1/translate/jobs/{id} and returns raw (body, statusCode, error).
// Used for tests that need to assert non-200 status codes directly.
func GetJobDetailRaw(ctx context.Context, baseURL, jobID string) ([]byte, int, error) {
	return doGET(ctx, jobURL(baseURL, jobID))
}

// GetJobResultRaw fetches GET /v1/translate/jobs/{id}/result and returns raw bytes and status.
// Used for tests asserting 202 (in_progress), 404, etc.
func GetJobResultRaw(ctx context.Context, baseURL, jobID string) ([]byte, int, error) {
	return doGET(ctx, jobResultURL(baseURL, jobID))
}

// GetJobResultDownloadRaw fetches GET /v1/translate/jobs/{id}/result/download and returns raw bytes and status.
func GetJobResultDownloadRaw(ctx context.Context, baseURL, jobID string) ([]byte, int, error) {
	return doGET(ctx, jobResultDownloadURL(baseURL, jobID))
}

// ListJobs calls GET /v1/translate/jobs with pagination and an optional status filter, and expects 200.
func ListJobs(ctx context.Context, baseURL string, limit, offset int, status string) (*JobsListResponse, error) {
	listURL := common.AppendQueryParams(jobsURL(baseURL), map[string]string{
		"limit":         strconv.Itoa(limit),
		"offset":        strconv.Itoa(offset),
		"status_filter": status,
	})

	return common.GetJSON[JobsListResponse](ctx, listURL, common.HTTPConfigForTimeout(getCallTimeout))
}

// ListJobsRaw calls GET /v1/translate/jobs with a status filter and returns raw bytes and status.
// Used for invalid filter tests.
func ListJobsRaw(ctx context.Context, baseURL, status string) ([]byte, int, error) {
	return doGET(ctx, common.AppendQueryParams(jobsURL(baseURL), map[string]string{"status_filter": status}))
}

// WaitForJobCompletion polls GET /v1/translate/jobs/{id} until the job reaches a terminal state or timeout.
func WaitForJobCompletion(ctx context.Context, baseURL, jobID string, timeout time.Duration) (*JobDetailResponse, error) {
	return common.WaitForJob(ctx, jobID,
		common.JobPollConfig{Timeout: timeout, Interval: pollInterval, LogPrefix: "TRANSLATE"},
		func(ctx context.Context) (*JobDetailResponse, error) { return GetJobDetail(ctx, baseURL, jobID) },
		func(d *JobDetailResponse) common.JobState { return common.JobState{Status: d.Status, Error: d.Error} },
	)
}
