package extraction

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	postCallTimeout = 120 * time.Second
	pollInterval    = 5 * time.Second
)

// httpConfigForTimeout returns an HTTP client config with the given timeout.
func httpConfigForTimeout(timeout time.Duration) common.HTTPClientConfig {
	return common.HTTPClientConfig{
		Timeout:            timeout,
		InsecureSkipVerify: true,
		PoolConnections:    false,
	}
}

func healthURL(baseURL string) string {
	return fmt.Sprintf("%s/health", strings.TrimRight(baseURL, "/"))
}

func syncExtractURL(baseURL string) string {
	return fmt.Sprintf("%s/v1/extract", strings.TrimRight(baseURL, "/"))
}

func jobsURL(baseURL string) string {
	return fmt.Sprintf("%s/v1/extract/jobs", strings.TrimRight(baseURL, "/"))
}

func jobURL(baseURL, jobID string) string {
	return fmt.Sprintf("%s/v1/extract/jobs/%s", strings.TrimRight(baseURL, "/"), jobID)
}

func jobResultURL(baseURL, jobID, docID string) string {
	return fmt.Sprintf("%s/v1/extract/jobs/%s/results/%s", strings.TrimRight(baseURL, "/"), jobID, docID)
}

func doGET(ctx context.Context, url string) ([]byte, int, error) {
	return common.DoGET(ctx, url, httpConfigForTimeout(getCallTimeout))
}

func doPOST(ctx context.Context, url string, body *bytes.Buffer, contentType string) ([]byte, int, error) {
	return common.DoPOST(ctx, url, body, contentType, httpConfigForTimeout(postCallTimeout))
}

func doDELETE(ctx context.Context, url string) ([]byte, int, error) {
	return common.DoDELETE(ctx, url, httpConfigForTimeout(getCallTimeout))
}

// ErrorResponse is the error envelope returned by the extract service.
type ErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Status  int    `json:"status"`
	} `json:"error"`
}

// HealthResponse is the body of GET /health.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

// ExtractionExample is a single few-shot example for the LLM prompt.
type ExtractionExample struct {
	Text   string      `json:"text"`
	Output interface{} `json:"output"`
}

// ExtractRequest is the JSON body for POST /v1/extract (synchronous).
type ExtractRequest struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	CustomPrompt string                 `json:"custom_prompt,omitempty"`
	JSONSchema   map[string]interface{} `json:"json_schema"`
	Examples     []ExtractionExample    `json:"examples,omitempty"`
	Text         string                 `json:"text"`
}

// extractDataEnvelope is the inner "data" object of POST /v1/extract.
type extractDataEnvelope struct {
	Extraction map[string]interface{} `json:"extraction"`
	SchemaID   interface{}            `json:"schema_id"`
	Source     map[string]interface{} `json:"source"`
}

// ExtractResponse is the 200 body of POST /v1/extract.
type ExtractResponse struct {
	DataEnvelope extractDataEnvelope    `json:"data"`
	Meta         map[string]interface{} `json:"meta,omitempty"`
	Usage        map[string]interface{} `json:"usage,omitempty"`
}

// GetData returns the extracted fields map.
func (r *ExtractResponse) GetData() map[string]interface{} {
	return r.DataEnvelope.Extraction
}

// JobStatus is the lifecycle state of an async extraction job.
type JobStatus string

const (
	JobStatusAccepted            JobStatus = "accepted"
	JobStatusInProgress          JobStatus = "in_progress"
	JobStatusCompleted           JobStatus = "completed"
	JobStatusCompletedWithErrors JobStatus = "completed_with_errors"
	JobStatusFailed              JobStatus = "failed"
)

// JobDocStatus is the lifecycle state of a single document inside a job.
type JobDocStatus string

const (
	JobDocStatusPending    JobDocStatus = "pending"
	JobDocStatusInProgress JobDocStatus = "in_progress"
	JobDocStatusCompleted  JobDocStatus = "completed"
	JobDocStatusFailed     JobDocStatus = "failed"
)

// JobCreatedResponse is the 202 body of POST /v1/extract/jobs.
type JobCreatedResponse struct {
	JobID     string `json:"job_id"`
	FileCount int    `json:"file_count"`
}

// BatchDocumentItem describes a single document within a job.
type BatchDocumentItem struct {
	DocID    string `json:"doc_id"`
	Filename string `json:"filename"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

// JobDetailResponse is the body of GET /v1/extract/jobs/{job_id}.
type JobDetailResponse struct {
	JobID          string                 `json:"job_id"`
	JobName        *string                `json:"job_name,omitempty"`
	SchemaID       string                 `json:"schema_id"`
	Status         JobStatus              `json:"status"`
	Documents      []BatchDocumentItem    `json:"documents,omitempty"`
	FileCount      int                    `json:"file_count,omitempty"`
	FilesCompleted int                    `json:"files_completed,omitempty"`
	FilesFailed    int                    `json:"files_failed,omitempty"`
	FilesPending   int                    `json:"files_pending,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	SubmittedAt    string                 `json:"submitted_at"`
	CompletedAt    *string                `json:"completed_at,omitempty"`
	Error          *string                `json:"error,omitempty"`
}

// JobListItem is one entry in GET /v1/extract/jobs.
type JobListItem struct {
	JobID       string    `json:"job_id"`
	JobName     *string   `json:"job_name,omitempty"`
	SchemaID    string    `json:"schema_id"`
	Status      JobStatus `json:"status"`
	FileCount   int       `json:"file_count"`
	SubmittedAt string    `json:"submitted_at"`
	CompletedAt *string   `json:"completed_at,omitempty"`
}

// PaginationInfo holds page metadata for list responses.
type PaginationInfo struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// JobsListResponse is the body of GET /v1/extract/jobs.
type JobsListResponse struct {
	Pagination PaginationInfo `json:"pagination"`
	Data       []JobListItem  `json:"data"`
}

// JobResultResponse is the body of GET /v1/extract/jobs/{job_id}/results/{doc_id}.
type JobResultResponse struct {
	DataEnvelope extractDataEnvelope    `json:"data"`
	Status       string                 `json:"status"`
	Meta         map[string]interface{} `json:"meta,omitempty"`
	Usage        map[string]interface{} `json:"usage,omitempty"`
}

// GetData returns the extracted fields map.
func (r *JobResultResponse) GetData() map[string]interface{} {
	return r.DataEnvelope.Extraction
}

// HealthCheck calls GET /health and returns an error if the service is not up.
func HealthCheck(ctx context.Context, baseURL string) error {
	respBody, statusCode, err := doGET(ctx, healthURL(baseURL))
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}

	if statusCode != http.StatusOK {
		return fmt.Errorf("health check returned %d: %s", statusCode, string(respBody))
	}

	var resp HealthResponse
	if jsonErr := json.Unmarshal(respBody, &resp); jsonErr != nil {
		logger.Infof("[EXTRACT] health check passed (non-JSON body)")

		return nil
	}

	logger.Infof("[EXTRACT] health check passed (status=%q)", resp.Status)

	return nil
}

// Extract calls POST /v1/extract (synchronous).
// Returns the parsed ExtractResponse on HTTP 200.
func Extract(ctx context.Context, baseURL string, req ExtractRequest) (*ExtractResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	respBody, statusCode, err := doPOST(ctx, syncExtractURL(baseURL), bytes.NewBuffer(payload), "application/json")
	if err != nil {
		return nil, err
	}

	var resp ExtractResponse
	if err := common.ValidateStatusAndUnmarshal(respBody, statusCode, http.StatusOK, &resp); err != nil {
		return nil, err
	}

	logger.Infof("[EXTRACT] sync extraction succeeded (fields=%d)", len(resp.GetData()))

	return &resp, nil
}

// ExtractExpectingError calls POST /v1/extract and expects a non-200 response.
func ExtractExpectingError(ctx context.Context, baseURL string, req ExtractRequest) (*ErrorResponse, int, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request: %w", err)
	}

	respBody, statusCode, err := doPOST(ctx, syncExtractURL(baseURL), bytes.NewBuffer(payload), "application/json")
	if err != nil {
		return nil, statusCode, err
	}

	if statusCode == http.StatusOK {
		return nil, statusCode, fmt.Errorf("unexpected success with status code %d: %s", statusCode, string(respBody))
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		return nil, statusCode, fmt.Errorf("failed to parse error response (status %d): %w", statusCode, err)
	}

	return &errResp, statusCode, nil
}

// ExtractRawBody posts an arbitrary body to POST /v1/extract.
func ExtractRawBody(ctx context.Context, baseURL string, body *bytes.Buffer, contentType string) ([]byte, int, error) {
	return doPOST(ctx, syncExtractURL(baseURL), body, contentType)
}

// createTempTextFile writes text to a temporary .txt file and returns its path.
func createTempTextFile(text string) (string, error) {
	f, err := os.CreateTemp("", "extract-text-*.txt")
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

// buildJobCreateURL builds POST /v1/extract/jobs with optional query params.
func buildJobCreateURL(baseURL, jobName string) string {
	url := jobsURL(baseURL)
	if jobName != "" {
		url += fmt.Sprintf("?job_name=%s", jobName)
	}

	return url
}

// CreateJobWithText writes text to a temp file and submits it as an async job.
// req supplies the schema, name, custom_prompt and examples — its Text field is ignored.
func CreateJobWithText(ctx context.Context, baseURL string, req ExtractRequest, jobName string) (*JobCreatedResponse, error) {
	tmpFile, err := createTempTextFile(req.Text)
	if err != nil {
		return nil, err
	}

	defer func() { _ = os.Remove(tmpFile) }()

	return CreateJobWithFile(ctx, baseURL, req, tmpFile, jobName)
}

// buildJobFormFields converts an ExtractRequest into the multipart form fields of the jobs API.
func buildJobFormFields(req ExtractRequest) (map[string]string, error) {
	jsonSchemaBytes, err := json.Marshal(req.JSONSchema)
	if err != nil {
		return nil, fmt.Errorf("marshal json_schema: %w", err)
	}

	fields := map[string]string{
		"json_schema": string(jsonSchemaBytes),
	}

	for key, val := range map[string]string{
		"name":          req.Name,
		"description":   req.Description,
		"custom_prompt": req.CustomPrompt,
	} {
		if val != "" {
			fields[key] = val
		}
	}

	if len(req.Examples) > 0 {
		examplesBytes, err := json.Marshal(req.Examples)
		if err != nil {
			return nil, fmt.Errorf("marshal examples: %w", err)
		}

		fields["examples"] = string(examplesBytes)
	}

	return fields, nil
}

// CreateJobWithFile submits an async extraction job from a file path.
// req supplies the schema metadata; filePath is the document to extract from.
func CreateJobWithFile(ctx context.Context, baseURL string, req ExtractRequest, filePath, jobName string) (*JobCreatedResponse, error) {
	fields, err := buildJobFormFields(req)
	if err != nil {
		return nil, err
	}

	logger.Infof("[EXTRACT] async job form fields: json_schema=%s name=%s", fields["json_schema"], req.Name)

	body, writer, err := common.BuildMultipartBodyWithFields("files", filePath, fields)
	if err != nil {
		return nil, err
	}

	respBody, statusCode, err := doPOST(ctx, buildJobCreateURL(baseURL, jobName), body, writer.FormDataContentType())
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusAccepted {
		return nil, fmt.Errorf("unexpected status %d: %s", statusCode, string(respBody))
	}

	var jobResp JobCreatedResponse
	if err := json.Unmarshal(respBody, &jobResp); err != nil {
		return nil, fmt.Errorf("failed to parse job created response: %w", err)
	}

	logger.Infof("[EXTRACT] async job created: %s (files=%d)", jobResp.JobID, jobResp.FileCount)

	return &jobResp, nil
}

// GetJobDetail fetches job details from GET /v1/extract/jobs/{job_id}.
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
		return nil, fmt.Errorf("failed to parse job detail: %w", err)
	}

	return &detail, nil
}

// WaitForJobCompletion polls GET /v1/extract/jobs/{job_id} until the job reaches
// a terminal state (completed, completed_with_errors, failed) or timeout.
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
				logger.Warningf("[EXTRACT] get job detail failed: %v", err)

				continue
			}

			logger.Infof("[EXTRACT] job %s status: %s", jobID, detail.Status)

			switch detail.Status {
			case JobStatusCompleted, JobStatusCompletedWithErrors:
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

// GetJobResult fetches per-document result from GET /v1/extract/jobs/{job_id}/results/{doc_id}.
func GetJobResult(ctx context.Context, baseURL, jobID, docID string) (*JobResultResponse, error) {
	respBody, statusCode, err := doGET(ctx, jobResultURL(baseURL, jobID, docID))
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusAccepted {
		return nil, fmt.Errorf("document %s still processing (202)", docID)
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", statusCode, string(respBody))
	}

	var result JobResultResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse job result: %w", err)
	}

	return &result, nil
}

// ListJobs calls GET /v1/extract/jobs with optional filters.
func ListJobs(ctx context.Context, baseURL string, limit, offset int, status, schemaName string) (*JobsListResponse, error) {
	url := fmt.Sprintf("%s?limit=%d&offset=%d", jobsURL(baseURL), limit, offset)
	if status != "" {
		url += fmt.Sprintf("&status=%s", status)
	}

	if schemaName != "" {
		url += fmt.Sprintf("&schema_name=%s", schemaName)
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
		return nil, fmt.Errorf("failed to parse jobs list: %w", err)
	}

	return &list, nil
}

// DeleteJob deletes a single job by ID. Accepts 200 or 204.
func DeleteJob(ctx context.Context, baseURL, jobID string) error {
	_, statusCode, err := doDELETE(ctx, jobURL(baseURL, jobID))
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("delete job returned unexpected status %d", statusCode)
	}

	logger.Infof("[EXTRACT] deleted job: %s", jobID)

	return nil
}

// DeleteAllJobs deletes all jobs (requires ?confirm=true).
func DeleteAllJobs(ctx context.Context, baseURL string) error {
	_, statusCode, err := doDELETE(ctx, jobsURL(baseURL)+"?confirm=true")
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("delete all jobs returned unexpected status %d", statusCode)
	}

	logger.Infof("[EXTRACT] all jobs deleted")

	return nil
}

// SubmitAndVerifyJob creates an async job from text, waits for completion, fetches
// the result for the first completed document, and returns both.
func SubmitAndVerifyJob(
	ctx context.Context,
	baseURL string,
	req ExtractRequest,
	jobName string,
	jobTimeout time.Duration,
) (*JobDetailResponse, *JobResultResponse, error) {
	jobResp, err := CreateJobWithText(ctx, baseURL, req, jobName)
	if err != nil {
		return nil, nil, fmt.Errorf("create job: %w", err)
	}

	time.Sleep(2 * time.Second) //nolint:mnd

	detail, err := WaitForJobCompletion(ctx, baseURL, jobResp.JobID, jobTimeout)
	if err != nil {
		return detail, nil, fmt.Errorf("wait for job %s: %w", jobResp.JobID, err)
	}

	for _, doc := range detail.Documents {
		if doc.Status == string(JobDocStatusCompleted) {
			result, resultErr := GetJobResult(ctx, baseURL, jobResp.JobID, doc.DocID)
			if resultErr != nil {
				return detail, nil, fmt.Errorf("get result for doc %s: %w", doc.DocID, resultErr)
			}

			return detail, result, nil
		}
	}

	return detail, nil, fmt.Errorf("job %s completed but no document has status=completed", jobResp.JobID)
}

// ResponseContainsField reports whether resp.GetData() has the given key with a
// non-nil, non-empty-string value.
func ResponseContainsField(resp *ExtractResponse, field string) bool {
	if resp == nil {
		return false
	}

	val, ok := resp.GetData()[field]
	if !ok || val == nil {
		return false
	}

	if s, isStr := val.(string); isStr {
		return s != ""
	}

	return true
}

// AllSchemaFieldsPresent reports whether every key in expectedFields appears in
// resp.GetData(). Returns the first missing field name and false when any is absent.
func AllSchemaFieldsPresent(resp *ExtractResponse, expectedFields []string) (string, bool) {
	if resp == nil {
		return "<nil response>", false
	}

	for _, f := range expectedFields {
		if _, ok := resp.GetData()[f]; !ok {
			return f, false
		}
	}

	return "", true
}

// JobResultContainsField reports whether result.GetData() has the given key.
func JobResultContainsField(result *JobResultResponse, field string) bool {
	if result == nil {
		return false
	}

	val, ok := result.GetData()[field]
	if !ok || val == nil {
		return false
	}

	if s, isStr := val.(string); isStr {
		return s != ""
	}

	return true
}

// AllJobResultFieldsPresent checks all expectedFields exist in result.GetData().
func AllJobResultFieldsPresent(result *JobResultResponse, expectedFields []string) (string, bool) {
	if result == nil {
		return "<nil result>", false
	}

	for _, f := range expectedFields {
		if _, ok := result.GetData()[f]; !ok {
			return f, false
		}
	}

	return "", true
}

// ShippingUpdateSchema returns the json_schema for shipping-update-v4.
func ShippingUpdateSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"sender_info": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":    map[string]interface{}{"type": "string"},
					"phone":   map[string]interface{}{"type": "string"},
					"email":   map[string]interface{}{"type": "string"},
					"company": map[string]interface{}{"type": "string"},
				},
				"required": []string{"name", "phone", "email", "company"},
			},
			"shipping_date":         map[string]interface{}{"type": "string"},
			"tracking_number":       map[string]interface{}{"type": "string"},
			"handling_instructions": map[string]interface{}{"type": "string"},
			"items": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"weight":      map[string]interface{}{"type": "string"},
						"weight_unit": map[string]interface{}{"type": "string"},
						"size":        map[string]interface{}{"type": []string{"string", "null"}},
						"type":        map[string]interface{}{"type": "string"},
					},
					"required": []string{"weight", "weight_unit", "size", "type"},
				},
			},
		},
		"required": []string{"sender_info", "shipping_date", "tracking_number", "items"},
	}
}

// ShippingUpdateExample returns the canonical DHL few-shot example.
func ShippingUpdateExample() ExtractionExample {
	return ExtractionExample{
		Text: "Subject: Shipping Update for Your Package\nDear Mr. John Doe,\n" +
			"Please find below the shipping details for your package: " +
			"Tracking Number: #ABCD1234 Shipping Carrier: DHL Express " +
			"Ship Date: February 22, 2023 Estimated Delivery Date: March 1, 2023 " +
			"Package Details: Weight: 5.6 pounds  Package Type: Parcel \n " +
			"Handling Instructions: Handle with Care and Keep Refrigerated \n" +
			"Contact Information: \nName: Emily Rodriguez \n" +
			"Email: erodriguez@dhl.com \nPhone Number: 1-800-123-4567\n" +
			"If you have any questions or concerns about your package, please don't hesitate to contact us. " +
			"Thank you for choosing DHL Express. Best regards, Emily Rodriguez DHL Express Customer Service",
		Output: map[string]interface{}{
			"sender_info": map[string]interface{}{
				"name":    "Emily Rodriguez",
				"phone":   "1-800-123-4567",
				"email":   "erodriguez@dhl.com",
				"company": "DHL Express",
			},
			"shipping_date":   "22-02-2023",
			"tracking_number": "ABCD1234",
			"items": []interface{}{
				map[string]interface{}{
					"weight":      "5.6",
					"weight_unit": "lb",
					"size":        "6X5",
					"type":        "parcel",
				},
			},
		},
	}
}

// FedExShippingText is the FedEx multi-item shipping email (2 items: Box + Parcel).
const FedExShippingText = "Subject: International Shipping Confirmation\n" +
	"Hello Sarah,\n" +
	"Your shipment has been scheduled for dispatch with FedEx International.\n" +
	"Tracking Number: FX987654321\n" +
	"Shipping Date: March 15, 2023\n" +
	"Sender Information:\n" +
	"Name: Michael Anderson\n" +
	"Phone: +1-415-555-0198\n" +
	"Email: michael.anderson@example.com\n" +
	"Company: Global Trade Solutions\n" +
	"Package Details:\n" +
	"Item 1:\n" +
	"Weight: 12.5 kilograms\n" +
	"Weight Unit: kg\n" +
	"Size: 40X30X25\n" +
	"Type: Box\n" +
	"Item 2:\n" +
	"Weight: 2 kilograms\n" +
	"Weight Unit: kg\n" +
	"Size: 20X15X10\n" +
	"Type: Parcel\n" +
	"Handling Instructions:\n" +
	"Keep dry and protect from direct sunlight.\n" +
	"Please contact Global Trade Solutions if you have any questions regarding this shipment.\n" +
	"Regards,\n" +
	"Michael Anderson\n" +
	"Global Trade Solutions"

// NewShippingUpdateRequest builds a complete ExtractRequest for shipping-update-v4.
func NewShippingUpdateRequest(text string) ExtractRequest {
	return ExtractRequest{
		Name:        "shipping-update-v4",
		Description: "Extracts core invoice fields",
		CustomPrompt: "Following properties need to be extracted:\n" +
			" 1. sender_info: name, phone, email, company \n" +
			" 2. shipping_date\n" +
			" 3. tracking_number\n" +
			" 4. items to be shipped - weight, weight_unit, size, type, \n" +
			" 5. handling_instructions\n" +
			" Return N/A for a value that is unavailable",
		JSONSchema: ShippingUpdateSchema(),
		Examples:   []ExtractionExample{ShippingUpdateExample()},
		Text:       text,
	}
}

// GetTestTXTPath returns the absolute path to sample_txt.txt in ingestion/docs.
func GetTestTXTPath() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}

	return filepath.Join(filepath.Dir(filepath.Dir(filename)), "ingestion", "docs", "sample_txt.txt")
}

// GetTestPDFPath returns the absolute path to test_doc.pdf in ingestion/docs.
func GetTestPDFPath() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}

	return filepath.Join(filepath.Dir(filepath.Dir(filename)), "ingestion", "docs", "test_doc.pdf")
}
