package e2e

// mcp_translate_extract_test.go — MCP sidecar flows for the translate and extract
// templates. The specs are registered inside the "MCP Sidecar Tests" context in
// e2e_suite_test.go, next to the chat/digitize/similarity/summarize sidecar specs.
//
// Tool names are the FastAPI auto-generated operationIds (the translate and extract
// services do not set explicit ones); the sidecar exposes only operations tagged "MCP".
//
// Argument shapes accepted by the sidecar (mcp/internal/tool/provider.go):
//   - request body → "prototype" for create_* operations, "data" otherwise
//   - path params  → top-level keys (e.g. "job_id")
//
// Multipart form values go through the sidecar's base64 sniffing: any string that is
// valid standard base64 is uploaded as a *file part*. Short alphanumeric values whose
// length is a multiple of four (for example "auto") would therefore be mangled into
// files, so these specs omit source_language when they want auto-detection.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/cli"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
	"github.com/project-ai-services/ai-services/tests/e2e/extraction"
	"github.com/project-ai-services/ai-services/tests/e2e/translation"
)

const (
	mcpTranslateToolSync   = "sync_translate_v1_translate_post"
	mcpTranslateToolCreate = "create_translation_job_v1_translate_jobs_post"
	mcpTranslateToolJob    = "get_job_v1_translate_jobs__job_id__get"
	mcpTranslateToolResult = "get_job_result_v1_translate_jobs__job_id__result_get"

	mcpExtractToolSync   = "extract_sync_v1_extract_post"
	mcpExtractToolCreate = "create_extract_job_v1_extract_jobs_post"
	mcpExtractToolJob    = "get_extract_job_v1_extract_jobs__job_id__get"
	mcpExtractToolResult = "get_document_result_v1_extract_jobs__job_id__results__doc_id__get"

	mcpInfoTimeout     = 2 * time.Minute
	mcpSyncTimeout     = 5 * time.Minute
	mcpShortTimeout    = 1 * time.Minute
	mcpJobTimeout      = 15 * time.Minute
	mcpJobPollInterval = 10 * time.Second
	mcpCleanupTimeout  = 2 * time.Minute
)

// mcpTranslateTools lists the tools the translate sidecar must register.
var mcpTranslateTools = []string{
	mcpTranslateToolSync, mcpTranslateToolCreate, mcpTranslateToolJob, mcpTranslateToolResult,
}

// mcpExtractTools lists the tools the extract sidecar must register.
var mcpExtractTools = []string{
	mcpExtractToolSync, mcpExtractToolCreate, mcpExtractToolJob, mcpExtractToolResult,
}

// ── helpers ───────────────────────────────────────────────────────────────────

// mcpApplicationInfo returns the current 'application info' output, failing the spec on error.
func mcpApplicationInfo(label string) string {
	infoCtx, infoCancel := context.WithTimeout(context.Background(), mcpInfoTimeout)
	defer infoCancel()

	out, err := cli.ApplicationInfo(infoCtx, cfg, appName, appRuntime)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[%s] application info failed", label)

	return out
}

// mcpRequireTools asserts that the sidecar at mcpURL registered every tool in want.
func mcpRequireTools(mcpURL, label string, want []string) {
	gomega.Expect(mcpURL).NotTo(gomega.BeEmpty(),
		"[MCP] %s MCP URL not found in application info — sidecar may not be deployed", label)

	tools, err := mcpToolsForEndpoint(mcpEnsureEndpoint(mcpURL))
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[MCP] %s: tools/list handshake failed", label)

	registered := make([]string, 0, len(tools))
	for _, tool := range tools {
		if name, ok := tool["name"].(string); ok {
			registered = append(registered, name)
		}
	}

	for _, name := range want {
		gomega.Expect(registered).To(gomega.ContainElement(name),
			"[MCP] %s: expected tool %q is not registered", label, name)
	}
}

// mcpCallRaw invokes a tool and returns content[0].text without inspecting the upstream body.
func mcpCallRaw(callCtx context.Context, endpoint, tool string, args map[string]any) (string, error) {
	raw, err := mcpCallTool(callCtx, endpoint, tool, args)
	if err != nil {
		return "", err
	}

	return mcpParseToolText(raw)
}

// mcpCallText invokes a tool and additionally fails when the upstream service answered with an
// error envelope (the sidecar does not check the upstream HTTP status).
func mcpCallText(callCtx context.Context, endpoint, tool string, args map[string]any) (string, error) {
	text, err := mcpCallRaw(callCtx, endpoint, tool, args)
	if err != nil {
		return "", err
	}

	if err := mcpUpstreamError(text); err != nil {
		return "", err
	}

	return text, nil
}

// mcpUpstreamStatus returns error.status from a {"error":{"status":N}} envelope, or 0 when absent.
func mcpUpstreamStatus(text string) int {
	var probe struct {
		Error struct {
			Status int `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &probe); err != nil {
		return 0
	}

	return probe.Error.Status
}

// mcpExpectUpstreamError calls a tool that must be rejected by the upstream service and
// returns the error envelope text. wantStatus 0 accepts any 4xx status.
func mcpExpectUpstreamError(endpoint, tool string, args map[string]any, wantStatus int, label string) string {
	callCtx, cancel := context.WithTimeout(ctx, mcpShortTimeout)
	defer cancel()

	text, err := mcpCallRaw(callCtx, endpoint, tool, args)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[%s] %s: tool call failed at the MCP layer", label, tool)
	gomega.Expect(mcpUpstreamError(text)).To(gomega.HaveOccurred(),
		"[%s] %s: expected an upstream error envelope, got: %.300s", label, tool, text)

	status := mcpUpstreamStatus(text)
	if wantStatus != 0 {
		gomega.Expect(status).To(gomega.Equal(wantStatus),
			"[%s] %s: unexpected upstream status in: %.300s", label, tool, text)
	} else {
		gomega.Expect(status).To(gomega.And(
			gomega.BeNumerically(">=", http.StatusBadRequest),
			gomega.BeNumerically("<", http.StatusInternalServerError)),
			"[%s] %s: expected a 4xx upstream status in: %.300s", label, tool, text)
	}

	logger.Infof("[%s] %s rejected as expected (status=%d)", label, tool, status)

	return text
}

// mcpRequireToken fails the spec when no catalog token is available; the sidecars run with
// --auth-passthrough so every tools/call must carry one.
func mcpRequireToken(label string) {
	_, err := mcpBearerToken()
	gomega.Expect(err).NotTo(gomega.HaveOccurred(),
		"[%s] sidecars use --auth-passthrough, a catalog token is required", label)
}

// mcpRequireEndpoint turns the URL found in application info into a /mcp endpoint, failing when absent.
func mcpRequireEndpoint(mcpURL, label string) string {
	gomega.Expect(mcpURL).NotTo(gomega.BeEmpty(),
		"[%s] MCP URL not found in application info — sidecar may not be deployed", label)

	return mcpEnsureEndpoint(mcpURL)
}

// mcpExpectHealthy asserts GET <base>/health on a sidecar returns the expected MCP health body.
func mcpExpectHealthy(mcpURL, label string) {
	gomega.Expect(mcpURL).NotTo(gomega.BeEmpty(),
		"[MCP] %s MCP URL not found in application info — sidecar may not be deployed", label)

	healthURL := strings.TrimSuffix(mcpURL, "/mcp") + "/health"

	healthCtx, healthCancel := context.WithTimeout(context.Background(), mcpRequestTimeout)
	defer healthCancel()

	req, err := http.NewRequestWithContext(healthCtx, http.MethodGet, healthURL, nil)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[MCP] failed to build request for %s", healthURL)

	resp, err := mcpHTTPClient().Do(req)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[MCP] GET %s failed", healthURL)

	defer func() { _ = resp.Body.Close() }()

	gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK), "[MCP] %s /health returned HTTP %d", label, resp.StatusCode)

	var body struct {
		Status    string `json:"status"`
		Transport string `json:"transport"`
	}
	gomega.Expect(json.NewDecoder(resp.Body).Decode(&body)).To(gomega.Succeed(),
		"[MCP] %s /health response body could not be decoded", label)
	gomega.Expect(body.Status).To(gomega.Equal("ok"), "[MCP] %s sidecar health status is not 'ok'", label)
	gomega.Expect(body.Transport).To(gomega.Equal("streamable-http"),
		"[MCP] %s sidecar transport is not 'streamable-http'", label)

	logger.Infof("[MCP] %s sidecar healthy at %s", label, healthURL)
}

// ── translate specs ───────────────────────────────────────────────────────────

// mcpTranslateSpecs registers the translate MCP sidecar specs. It is nested inside
// translationSpecs, so it only runs for the translate template, after the app is created,
// the catalog session is refreshed and the translate API is healthy.
func mcpTranslateSpecs() {
	ginkgo.BeforeEach(func() {
		if appRuntime != "podman" {
			ginkgo.Skip("[MCP] MCP sidecar tests are podman-only — skipping for runtime: " + appRuntime)
		}
	})

	ginkgo.It("translate MCP sidecar /health returns ok",
		ginkgo.Label("spyre-dependent", "mcp-health"), func() {
			mcpExpectHealthy(cli.ExtractTranslateMCPURL(mcpApplicationInfo("MCP")), "translate")
		})

	ginkgo.It("translate MCP sidecar tools/list returns the translate tools",
		ginkgo.Label("spyre-dependent", "mcp-tools"), func() {
			mcpRequireTools(cli.ExtractTranslateMCPURL(mcpApplicationInfo("MCP")), "translate", mcpTranslateTools)
		})

	ginkgo.Context("MCP translate flow: sync translation → async job → result",
		ginkgo.Label("spyre-dependent", "mcp-flow"), ginkgo.Ordered, mcpTranslateFlowSpecs)
}

// ── translate flow ────────────────────────────────────────────────────────────

// mcpTranslateFlowSpecs drives the translate service through its MCP sidecar only:
// synchronous translation, an asynchronous file job, and the error paths of both.
func mcpTranslateFlowSpecs() {
	const (
		label      = "MCP-TRANSLATE"
		germanText = "Der Vertrag tritt am 1. Januar 2025 in Kraft."
	)

	// Written by earlier specs and read by later ones; ginkgo.Ordered guarantees sequential execution.
	var (
		endpoint string
		jobID    string
	)

	ginkgo.BeforeAll(func() {
		mcpRequireToken(label)
		endpoint = mcpRequireEndpoint(cli.ExtractTranslateMCPURL(mcpApplicationInfo(label)), label)
		logger.Infof("[%s] endpoint resolved — %s", label, endpoint)
	})

	ginkgo.It("should translate text synchronously via the translate MCP sidecar", func() {
		callCtx, cancel := context.WithTimeout(ctx, mcpSyncTimeout)
		defer cancel()

		text, err := mcpCallText(callCtx, endpoint, mcpTranslateToolSync, map[string]any{
			"data": map[string]any{
				"text":            germanText,
				"source_language": "German",
				"target_language": "English",
			},
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[%s] %s failed", label, mcpTranslateToolSync)

		var resp translation.SyncTranslateResponse
		gomega.Expect(json.Unmarshal([]byte(text), &resp)).To(gomega.Succeed(),
			"[%s] could not unmarshal sync translation from: %s", label, text)

		gomega.Expect(resp.Translation()).NotTo(gomega.BeEmpty())
		gomega.Expect(resp.Translation()).NotTo(gomega.Equal(germanText), "translation should differ from the input")
		gomega.Expect(resp.Translation()).To(gomega.ContainSubstring("2025"))
		gomega.Expect(resp.Data.TargetLanguage).To(gomega.Equal("english"))
		gomega.Expect(resp.Usage.TotalTokens).To(gomega.BeNumerically(">", 0))

		logger.Infof("[%s] sync translation: %s", label, resp.Translation())
	})

	ginkgo.It("should reject identical source and target languages with 400", func() {
		text := mcpExpectUpstreamError(endpoint, mcpTranslateToolSync, map[string]any{
			"data": map[string]any{
				"text":            "How are you?",
				"source_language": "English",
				"target_language": "English",
			},
		}, http.StatusBadRequest, label)

		gomega.Expect(strings.ToLower(text)).To(gomega.ContainSubstring("differ"))
	})

	ginkgo.It("should submit a file translation job via the translate MCP sidecar", func() {
		fileBytes, err := os.ReadFile(translation.GetGermanTXTPath())
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[%s] could not read the German fixture", label)

		callCtx, cancel := context.WithTimeout(ctx, mcpShortTimeout)
		defer cancel()

		text, err := mcpCallText(callCtx, endpoint, mcpTranslateToolCreate, map[string]any{
			"prototype": map[string]any{
				"file":            mcpBase64File(fileBytes),
				"source_language": "German",
				"target_language": "English",
				"job_name":        "e2e-mcp-translate",
			},
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[%s] %s failed", label, mcpTranslateToolCreate)

		var created translation.JobCreatedResponse
		gomega.Expect(json.Unmarshal([]byte(text), &created)).To(gomega.Succeed(),
			"[%s] could not unmarshal JobCreatedResponse from: %s", label, text)
		gomega.Expect(created.JobID).NotTo(gomega.BeEmpty(), "[%s] response missing job_id: %s", label, text)

		jobID = created.JobID
		logger.Infof("[%s] translation job submitted — job_id=%s", label, jobID)
	})

	ginkgo.It("should complete the translation job, checked via the translate MCP sidecar", func() {
		gomega.Expect(jobID).NotTo(gomega.BeEmpty(), "[%s] jobID not set — the submit step must pass first", label)

		pollCtx, pollCancel := context.WithTimeout(ctx, mcpJobTimeout)
		defer pollCancel()

		var last translation.JobDetailResponse

		// Stop on any terminal state so a failed job fails fast; transient poll errors return "".
		gomega.Eventually(func() string {
			text, err := mcpCallText(pollCtx, endpoint, mcpTranslateToolJob, map[string]any{"job_id": jobID})
			if err != nil {
				logger.Warningf("[%s] job poll error: %v", label, err)

				return ""
			}

			var detail translation.JobDetailResponse
			if err := json.Unmarshal([]byte(text), &detail); err != nil {
				logger.Warningf("[%s] job poll unexpected body: %s", label, text)

				return ""
			}

			last = detail
			logger.Infof("[%s] job %s status: %s", label, jobID, detail.Status)

			return string(detail.Status)
		}, mcpJobTimeout, mcpJobPollInterval).Should(
			gomega.BeElementOf("completed", "completed_with_errors", "failed"),
			"[%s] translation job %s did not reach a terminal state in time", label, jobID)

		gomega.Expect(string(last.Status)).To(gomega.Equal("completed"),
			"[%s] translation job %s ended as %q (error=%v)", label, jobID, last.Status, last.Error)
	})

	ginkgo.It("should return the translated text for the finished job via the translate MCP sidecar", func() {
		gomega.Expect(jobID).NotTo(gomega.BeEmpty(), "[%s] jobID not set — the submit step must pass first", label)

		callCtx, cancel := context.WithTimeout(ctx, mcpShortTimeout)
		defer cancel()

		text, err := mcpCallText(callCtx, endpoint, mcpTranslateToolResult, map[string]any{"job_id": jobID})
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[%s] %s failed", label, mcpTranslateToolResult)

		var resp translation.SyncTranslateResponse
		gomega.Expect(json.Unmarshal([]byte(text), &resp)).To(gomega.Succeed(),
			"[%s] could not unmarshal job result from: %.300s", label, text)
		gomega.Expect(resp.Translation()).NotTo(gomega.BeEmpty(), "[%s] job result has an empty translation", label)
		gomega.Expect(resp.Data.TargetLanguage).To(gomega.Equal("english"))

		logger.Infof("[%s] job result (first 200 chars): %.200s", label, resp.Translation())
	})

	ginkgo.It("should return 404 for an unknown translation job", func() {
		mcpExpectUpstreamError(endpoint, mcpTranslateToolJob,
			map[string]any{"job_id": uuid.NewString()}, http.StatusNotFound, label)
	})
}

// ── extract specs ─────────────────────────────────────────────────────────────

// mcpExtractSpecs registers the extract MCP sidecar specs. It is nested inside the
// "Entity Extraction Tests" context, so it only runs for the extract template, after the
// app is created, the catalog session is refreshed and the extract API is healthy.
func mcpExtractSpecs() {
	ginkgo.BeforeEach(func() {
		if appRuntime != "podman" {
			ginkgo.Skip("[MCP] MCP sidecar tests are podman-only — skipping for runtime: " + appRuntime)
		}
	})

	ginkgo.It("extract MCP sidecar /health returns ok",
		ginkgo.Label("spyre-dependent", "mcp-health"), func() {
			mcpExpectHealthy(cli.ExtractExtractMCPURL(mcpApplicationInfo("MCP")), "extract")
		})

	ginkgo.It("extract MCP sidecar tools/list returns the extract tools",
		ginkgo.Label("spyre-dependent", "mcp-tools"), func() {
			mcpRequireTools(cli.ExtractExtractMCPURL(mcpApplicationInfo("MCP")), "extract", mcpExtractTools)
		})

	ginkgo.Context("MCP extract flow: sync extraction → async job → per-document result",
		ginkgo.Label("spyre-dependent", "mcp-flow"), ginkgo.Ordered, mcpExtractFlowSpecs)
}

// ── extract flow ──────────────────────────────────────────────────────────────

// mcpExtractFlowSpecs drives the extract service through its MCP sidecar only:
// synchronous extraction, an asynchronous file job, per-document results and the error paths.
func mcpExtractFlowSpecs() {
	const label = "MCP-EXTRACT"

	var (
		endpoint       string
		extractRESTURL string
		jobID          string
		docID          string
		schemaJSON     string
	)

	// expectShippingFields asserts the extraction output carries the required fields and
	// that the FedEx tracking number was actually read from the text.
	expectShippingFields := func(data map[string]interface{}) {
		missing, ok := common.AllFieldsPresent(data, []string{"sender_info", "shipping_date", "tracking_number", "items"})
		gomega.Expect(ok).To(gomega.BeTrue(), "[%s] expected field %q in the extraction output", label, missing)
		gomega.Expect(fmt.Sprintf("%v", data["tracking_number"])).To(gomega.ContainSubstring("FX987654321"))

		items, castOK := data["items"].([]interface{})
		gomega.Expect(castOK).To(gomega.BeTrue(), "[%s] items must be an array", label)
		gomega.Expect(items).NotTo(gomega.BeEmpty(), "[%s] items array must not be empty", label)
	}

	ginkgo.BeforeAll(func() {
		mcpRequireToken(label)

		infoOutput := mcpApplicationInfo(label)
		endpoint = mcpRequireEndpoint(cli.ExtractExtractMCPURL(infoOutput), label)
		extractRESTURL = cli.ExtractExtractURL(infoOutput)

		schemaBytes, err := json.Marshal(extraction.ShippingUpdateSchema())
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[%s] could not marshal the shipping schema", label)
		schemaJSON = string(schemaBytes)

		logger.Infof("[%s] endpoint resolved — %s", label, endpoint)
	})

	// Remove the job created by the async specs through the REST API (best effort);
	// the sidecar only exposes read and create operations.
	ginkgo.AfterAll(func() {
		if jobID == "" || extractRESTURL == "" {
			return
		}

		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), mcpCleanupTimeout)
		defer cleanCancel()

		if err := extraction.DeleteJob(cleanCtx, extractRESTURL, jobID); err != nil {
			logger.Warningf("[%s] job %s cleanup failed (non-fatal): %v", label, jobID, err)
		}
	})

	ginkgo.It("should extract entities synchronously via the extract MCP sidecar", func() {
		callCtx, cancel := context.WithTimeout(ctx, mcpSyncTimeout)
		defer cancel()

		text, err := mcpCallText(callCtx, endpoint, mcpExtractToolSync, map[string]any{
			"data": map[string]any{
				"text":        extraction.FedExShippingText,
				"json_schema": extraction.ShippingUpdateSchema(),
			},
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[%s] %s failed", label, mcpExtractToolSync)

		var resp extraction.ExtractResponse
		gomega.Expect(json.Unmarshal([]byte(text), &resp)).To(gomega.Succeed(),
			"[%s] could not unmarshal ExtractResponse from: %.300s", label, text)

		expectShippingFields(resp.GetData())
		logger.Infof("[%s] sync extraction: tracking=%v", label, resp.GetData()["tracking_number"])
	})

	ginkgo.It("should reject a sync extraction that supplies no schema source with 4xx", func() {
		mcpExpectUpstreamError(endpoint, mcpExtractToolSync, map[string]any{
			"data": map[string]any{"text": extraction.FedExShippingText},
		}, 0, label)
	})

	ginkgo.It("should submit an extraction job via the extract MCP sidecar", func() {
		callCtx, cancel := context.WithTimeout(ctx, mcpShortTimeout)
		defer cancel()

		// "files" is a multipart array; each entry is base64 so the sidecar rebuilds a file part
		// (named files-N.txt from the content sniffing). json_schema is a JSON *string* form field.
		text, err := mcpCallText(callCtx, endpoint, mcpExtractToolCreate, map[string]any{
			"prototype": map[string]any{
				"files":       []string{mcpBase64File([]byte(extraction.FedExShippingText))},
				"json_schema": schemaJSON,
				"job_name":    "e2e-mcp-extract",
			},
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[%s] %s failed", label, mcpExtractToolCreate)

		var created extraction.JobCreatedResponse
		gomega.Expect(json.Unmarshal([]byte(text), &created)).To(gomega.Succeed(),
			"[%s] could not unmarshal JobCreatedResponse from: %s", label, text)
		gomega.Expect(created.JobID).NotTo(gomega.BeEmpty(), "[%s] response missing job_id: %s", label, text)
		gomega.Expect(created.FileCount).To(gomega.Equal(1))

		jobID = created.JobID
		logger.Infof("[%s] extraction job submitted — job_id=%s", label, jobID)
	})

	ginkgo.It("should complete the extraction job, checked via the extract MCP sidecar", func() {
		gomega.Expect(jobID).NotTo(gomega.BeEmpty(), "[%s] jobID not set — the submit step must pass first", label)

		pollCtx, pollCancel := context.WithTimeout(ctx, mcpJobTimeout)
		defer pollCancel()

		var last extraction.JobDetailResponse

		gomega.Eventually(func() string {
			text, err := mcpCallText(pollCtx, endpoint, mcpExtractToolJob, map[string]any{"job_id": jobID})
			if err != nil {
				logger.Warningf("[%s] job poll error: %v", label, err)

				return ""
			}

			var detail extraction.JobDetailResponse
			if err := json.Unmarshal([]byte(text), &detail); err != nil {
				logger.Warningf("[%s] job poll unexpected body: %s", label, text)

				return ""
			}

			last = detail
			logger.Infof("[%s] job %s status: %s", label, jobID, detail.Status)

			return string(detail.Status)
		}, mcpJobTimeout, mcpJobPollInterval).Should(
			gomega.BeElementOf("completed", "completed_with_errors", "failed"),
			"[%s] extraction job %s did not reach a terminal state in time", label, jobID)

		gomega.Expect(string(last.Status)).To(gomega.Equal("completed"),
			"[%s] extraction job %s ended as %q (error=%v, documents=%+v)", label, jobID, last.Status, last.Error, last.Documents)
		gomega.Expect(last.Documents).NotTo(gomega.BeEmpty(), "[%s] completed job lists no documents", label)

		docID = last.Documents[0].DocID
		gomega.Expect(docID).NotTo(gomega.BeEmpty(), "[%s] first document has no doc_id", label)
	})

	ginkgo.It("should return the per-document result via the extract MCP sidecar", func() {
		gomega.Expect(docID).NotTo(gomega.BeEmpty(), "[%s] docID not set — the completion step must pass first", label)

		callCtx, cancel := context.WithTimeout(ctx, mcpShortTimeout)
		defer cancel()

		text, err := mcpCallText(callCtx, endpoint, mcpExtractToolResult, map[string]any{
			"job_id": jobID,
			"doc_id": docID,
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "[%s] %s failed", label, mcpExtractToolResult)

		var resp extraction.JobResultResponse
		gomega.Expect(json.Unmarshal([]byte(text), &resp)).To(gomega.Succeed(),
			"[%s] could not unmarshal JobResultResponse from: %.300s", label, text)

		expectShippingFields(resp.GetData())
		logger.Infof("[%s] document %s result: tracking=%v", label, docID, resp.GetData()["tracking_number"])
	})

	ginkgo.It("should return 404 for an unknown extraction job", func() {
		mcpExpectUpstreamError(endpoint, mcpExtractToolJob,
			map[string]any{"job_id": uuid.NewString()}, http.StatusNotFound, label)
	})

	ginkgo.It("should return 404 for an unknown document in an unknown job", func() {
		mcpExpectUpstreamError(endpoint, mcpExtractToolResult, map[string]any{
			"job_id": uuid.NewString(),
			"doc_id": uuid.NewString(),
		}, http.StatusNotFound, label)
	})
}
