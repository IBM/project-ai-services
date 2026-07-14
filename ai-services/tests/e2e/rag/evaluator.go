package rag

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogConfig "github.com/project-ai-services/ai-services/internal/pkg/catalog/config"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// similarityHealthClient skips TLS verification so it works with both plain
// http:// (legacy podman HOST_IP:PORT) and https:// nip.io self-signed certs
// (catalog path). Same pattern as PostJSON's transport.
var similarityHealthClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec
		},
	},
}

// sharedRAGClient is a single, long-lived HTTP client used for all RAG and
// Judge PostJSON requests. Reusing one client (and its underlying Transport)
// allows TCP connections to be kept alive and reused across questions,
// preventing the connection-pool exhaustion that caused client.Do to hang
// indefinitely when hundreds of short-lived transports were created — one per
// call — and hammered the same upstream without ever releasing sockets.
//
// Configuration rationale:
//   - Timeout: 10 min — deliberately LONGER than the per-question context
//     deadline (perQuestionTimeout in e2e_suite_test.go, currently 8 min) so
//     the context cancellation always fires first. This makes the error
//     deterministic ("context deadline exceeded") rather than a silent client
//     Timeout that obscures the real cause.
//   - MaxIdleConnsPerHost: 4 — enough to reuse sockets for sequential
//     RAG + Judge calls without opening a fresh TCP connection every time.
//   - MaxConnsPerHost: 8 — hard cap so we never saturate a single vLLM or
//     RAG process with too many parallel connections.
//   - IdleConnTimeout: 90 s — recycle idle connections quickly so stale ones
//     are not reused after a long LLM inference pause.
//   - ResponseHeaderTimeout: 90 s — guards against dead keep-alive sockets
//     that are silently dropped by Caddy/nip.io after TLS idle expiry.
//     When the transport reuses a dead socket, the kernel send buffer accepts
//     the outgoing request bytes without error, then blocks waiting for
//     response headers that will never arrive.  http.Client.Timeout does NOT
//     catch this because it only starts ticking after the request body is
//     sent.  ResponseHeaderTimeout fires if no response header byte is
//     received within 90 s of the request being written.
//     NOTE: for vLLM/RAG this is safe — the server sends HTTP 200 headers
//     immediately before starting token generation; the body (token stream)
//     is not subject to ResponseHeaderTimeout.
//   - DialContext timeout: 15 s — bounds TCP connect + TLS handshake so
//     that a network partition on connect does not hang indefinitely.
var sharedRAGClient = &http.Client{
	Timeout: httpClientTimeout,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec
		},
		MaxIdleConnsPerHost:   4,
		MaxConnsPerHost:       8,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 90 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	},
}

// waitForEndpointReady polls targetURL until it returns HTTP 200 or ctx is done.
// onReady / onNotReady / onUnreachable format the per-attempt log messages.
// This eliminates the near-identical loop bodies in WaitForRAGBackendReady and
// WaitForSimilarityAPIReady.
func waitForEndpointReady(
	ctx context.Context,
	client *http.Client,
	targetURL string,
	pollInterval time.Duration,
	prepareReq func(*http.Request),
	onReady func(int),
	onNotReady func(int, int, time.Duration),
	onUnreachable func(int, error, time.Duration),
	onTimeout func(string, int) error,
) error {
	for attempt := 1; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if err != nil {
			return fmt.Errorf("build request for %s: %w", targetURL, err)
		}

		if prepareReq != nil {
			prepareReq(req)
		}

		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				onReady(attempt)

				return nil
			}
			onNotReady(resp.StatusCode, attempt, pollInterval)
		} else {
			onUnreachable(attempt, err, pollInterval)
		}

		if ctx.Err() != nil {
			return onTimeout(targetURL, attempt)
		}

		select {
		case <-ctx.Done():
			return onTimeout(targetURL, attempt)
		case <-time.After(pollInterval):
		}
	}
}

// WaitForRAGBackendReady polls ragBaseURL/v1/models until it returns HTTP 200,
// meaning the chat-bot backend has successfully connected to the LLM (vLLM).
// The LLM pod (llm-<slug>) is the last and most resource-heavy pod to start —
// it can take 20-30 minutes on Spyre hardware. This gate ensures we don't start
// the judge container (SetupLLMAsJudge) while the LLM pod is still consuming
// resources during initialisation, which would cause all other pods to crash.
// ragBaseURL is the full base URL, e.g. "https://chat-bot-backend-<slug>.<ip>.nip.io".
func WaitForRAGBackendReady(ctx context.Context, ragBaseURL string, pollInterval time.Duration) error {
	modelsURL := ragBaseURL + "/v1/models"

	return waitForEndpointReady(ctx, similarityHealthClient, modelsURL, pollInterval,
		func(req *http.Request) {
			if token := loadFreshBearerToken(); token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
		},
		func(attempt int) {
			logger.Infof("[RAG] LLM is ready — chat-bot /v1/models returned 200 after %d attempt(s)", attempt)
		},
		func(code, attempt int, interval time.Duration) {
			logger.Infof("[RAG] LLM not ready yet (HTTP %d on /v1/models), attempt %d — retrying in %s", code, attempt, interval)
		},
		func(attempt int, err error, interval time.Duration) {
			logger.Infof("[RAG] chat-bot /v1/models unreachable (attempt %d): %v — retrying in %s", attempt, err, interval)
		},
		func(url string, attempt int) error {
			return fmt.Errorf("timed out waiting for LLM via %s after %d attempt(s): %w", url, attempt, ctx.Err())
		},
	)
}

// WaitForSimilarityAPIReady polls the similarity-api /health endpoint until it
// returns HTTP 200 or the context deadline is exceeded.
// similarityBaseURL is the full base URL extracted from 'application info' output,
// e.g. "https://similarity-api-<slug>.<ip>.nip.io" (catalog) or "http://<ip>:<port>" (legacy).
func WaitForSimilarityAPIReady(ctx context.Context, similarityBaseURL string, pollInterval time.Duration) error {
	healthURL := similarityBaseURL + "/health"

	return waitForEndpointReady(ctx, similarityHealthClient, healthURL, pollInterval,
		nil,
		func(attempt int) {
			logger.Infof("[RAG] similarity-api healthy after %d attempt(s) — %s", attempt, healthURL)
		},
		func(code, attempt int, interval time.Duration) {
			logger.Infof("[RAG] similarity-api not ready yet (HTTP %d), attempt %d — retrying in %s", code, attempt, interval)
		},
		func(attempt int, err error, interval time.Duration) {
			logger.Infof("[RAG] similarity-api unreachable (attempt %d): %v — retrying in %s", attempt, err, interval)
		},
		func(url string, attempt int) error {
			return fmt.Errorf("timed out waiting for similarity-api at %s after %d attempt(s): %w", url, attempt, ctx.Err())
		},
	)
}

// catalogClientNew is a thin wrapper around catalogClient.New() so it can be
// referenced as a variable in tests and avoids a direct import cycle.
var catalogClientNew = func() (interface{ AccessToken() string }, error) {
	return catalogClient.New()
}

const (
	percentMultiplier       = 100
	judgeUserPromptTemplate = "QUESTION:\n" +
		"{question}\n" +
		"\n" +
		"GOLDEN ANSWER:\n" +
		"{golden_answer}\n" +
		"\n" +
		"MODEL ANSWER:\n" +
		"{model_answer}\n"

	// httpClientTimeout is the http.Client.Timeout on sharedRAGClient.
	// It is set deliberately LONGER than the per-question context deadline
	// (perQuestionTimeout in e2e_suite_test.go, currently 8 min) so the
	// context always fires first and the error is deterministic.
	httpClientTimeout = 10 * time.Minute
)

var ErrNonRetriable = errors.New("non-retriable error")

type ChatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type EvalResult struct {
	Question string
	Passed   bool
	Details  string
}

func isRetriableStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		(code >= 500 && code <= 599)
}

// RunWithRetry executes the provided function with retries upon failure.
// Each attempt receives the same parent context so that the parent deadline
// governs the total budget. Before each attempt the parent is checked — if it
// is already cancelled the loop exits immediately, preventing retries on a
// dead context. Back-off sleeps also respect context cancellation so they
// don't block past the parent deadline.
func RunWithRetry(
	ctx context.Context,
	maxRetries int,
	fn func(context.Context) (string, error),
) (string, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Guard: do not start a new attempt on an already-cancelled parent.
		// This is the key fix for the "retries inherit a cancelled context"
		// bug: when the per-question context (child of specCtx) expires, every
		// subsequent fn(ctx) call would immediately return context.Canceled.
		// Exiting here surfaces the real error instead of a misleading retry loop.
		if ctx.Err() != nil {
			return "", fmt.Errorf("parent context cancelled before attempt %d: %w", attempt+1, ctx.Err())
		}

		if attempt > 0 {
			logger.Infof("[RAG][retry] attempt %d/%d — previous error: %v", attempt+1, maxRetries+1, lastErr)
		}

		resp, err := fn(ctx)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		// If the parent context expired during the call, stop immediately.
		if ctx.Err() != nil {
			return "", fmt.Errorf("context cancelled after attempt %d: %w", attempt+1, ctx.Err())
		}

		if errors.Is(err, ErrNonRetriable) {
			return "", err
		}

		// Exponential back-off: 200 ms, 400 ms, 600 ms …
		// The select ensures we don't block past the parent deadline.
		if attempt < maxRetries {
			backoff := time.Duration(attempt+1) * 200 * time.Millisecond
			logger.Infof("[RAG][retry] waiting %s before next attempt", backoff)
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("context cancelled during back-off after attempt %d: %w", attempt+1, ctx.Err())
			case <-time.After(backoff):
			}
		}
	}

	return "", lastErr
}

// AskRAG sends a question to the RAG backend and returns the answer.
func AskRAG(ctx context.Context, baseURL string, question string) (string, error) {
	req := map[string]any{
		"model": "ibm-granite/granite-3.3-8b-instruct",
		"messages": []map[string]string{
			{"role": "user", "content": question},
		},
		"temperature": 0,
	}

	raw, err := PostJSON(ctx, baseURL, "/v1/chat/completions", req)
	if err != nil {
		return "", err
	}

	return extractAssistantContent(raw)
}

// buildJudgeUserPrompt constructs the user prompt for the judge LLM.
func buildJudgeUserPrompt(question, goldenAns, ragAns string) string {
	prompt := judgeUserPromptTemplate
	prompt = strings.ReplaceAll(prompt, "{question}", question)
	prompt = strings.ReplaceAll(prompt, "{golden_answer}", goldenAns)
	prompt = strings.ReplaceAll(prompt, "{model_answer}", ragAns)

	return prompt
}

// AskJudge sends the evaluation prompt to the judge service and returns the judge's response.
func AskJudge(
	ctx context.Context,
	judgeBaseURL string,
	question string,
	ragAns string,
	goldenAns string,
) (string, error) {
	userPrompt := buildJudgeUserPrompt(question, goldenAns, ragAns)

	req := map[string]any{
		"model": Model,
		"messages": []map[string]string{
			{"role": "system", "content": judgeSystemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0,
	}

	raw, err := PostJSON(ctx, judgeBaseURL, "/v1/chat/completions", req)
	if err != nil {
		return "", err
	}

	return extractAssistantContent(raw)
}

// loadFreshBearerToken returns the current catalog access token, refreshing it
// via the stored refresh token if the access token is missing or expired.
// This ensures requests succeed even when the 15-min access TTL has elapsed
// during long-running setup steps (model download, vLLM startup, etc.).
func loadFreshBearerToken() string {
	creds, err := catalogConfig.Load()
	if err != nil {
		logger.Warningf("[RAG] could not load catalog credentials: %v", err)
		return ""
	}

	if creds.AccessToken == "" {
		logger.Warningf("[RAG] catalog credentials loaded but access token is empty")
		return ""
	}

	// Check if the token has already expired or will expire imminently (within 30s).
	// If so, use the catalog client which auto-refreshes via the refresh token.
	const refreshSkew = 30 * time.Second
	exp, jwtErr := jwtTokenExpiry(creds.AccessToken)
	if jwtErr != nil || time.Until(exp) < refreshSkew {
		// Token expired or unparseable — use catalog client to get a fresh token.
		// catalog/client.New() loads credentials, refreshes if needed, and saves them.
		catalogClient, clientErr := catalogClientNew()
		if clientErr != nil {
			logger.Warningf("[RAG] could not refresh catalog token: %v", clientErr)
			// Fall through and use whatever token we have.
			return creds.AccessToken
		}
		return catalogClient.AccessToken()
	}

	return creds.AccessToken
}

// jwtTokenExpiry decodes the exp claim from a JWT without verifying the signature.
func jwtTokenExpiry(token string) (time.Time, error) {
	const jwtPartCount = 3
	parts := strings.Split(token, ".")
	if len(parts) != jwtPartCount {
		return time.Time{}, fmt.Errorf("malformed JWT")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, err
	}

	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Time{}, fmt.Errorf("no exp claim")
	}

	return time.Unix(claims.Exp, 0), nil
}

// PostJSON sends a POST request with a JSON body and returns the response body
// as a string. It uses the package-level sharedRAGClient so that TCP
// connections are kept alive and reused across calls, avoiding the connection-
// pool exhaustion that occurred when a brand-new *http.Transport was allocated
// on every call.
//
// Debug logging emits request start time, context deadline, HTTP status, and
// elapsed duration so that slow or hanging requests are visible without waiting
// for the full suite timeout.
func PostJSON(
	ctx context.Context,
	baseURL string,
	path string,
	body map[string]any,
) (string, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		baseURL+path,
		bytes.NewBuffer(b),
	)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// The chat-bot backend requires a Bearer token (catalog access token).
	// Use a fresh token — auto-refreshes if the 15-min TTL has elapsed.
	if token := loadFreshBearerToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	// Debug: log start time and remaining budget so slow requests are
	// immediately visible in the test output.
	start := time.Now()
	if deadline, ok := ctx.Deadline(); ok {
		logger.Infof("[RAG][http] POST %s%s — deadline in %s",
			baseURL, path, time.Until(deadline).Round(time.Second))
	} else {
		logger.Infof("[RAG][http] POST %s%s — no deadline", baseURL, path)
	}

	resp, err := sharedRAGClient.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		// Distinguish context cancellation from a transport-level error.
		// A cancelled context means the per-question budget expired; a
		// transport error means a network or server-side failure.
		if ctx.Err() != nil {
			logger.Errorf("[RAG][http] POST %s%s — cancelled after %s: context=%v",
				baseURL, path, elapsed.Round(time.Millisecond), ctx.Err())
			return "", fmt.Errorf("request cancelled: %w", ctx.Err())
		}
		logger.Errorf("[RAG][http] POST %s%s — transport error after %s: %v",
			baseURL, path, elapsed.Round(time.Millisecond), err)
		return "", fmt.Errorf("http request failed: %w", err)
	}

	// Always drain and close the body so the underlying TCP connection is
	// returned to the pool. Closing without a full drain leaves the connection
	// in a half-read state and permanently removes it from the pool, leaking
	// file descriptors and starving future requests of reusable sockets.
	// The defer runs after io.ReadAll below; io.Copy on an already-exhausted
	// reader is a cheap no-op that guarantees the body is fully consumed even
	// if io.ReadAll returns early on a partial read.
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	logger.Infof("[RAG][http] POST %s%s → HTTP %d in %s",
		baseURL, path, resp.StatusCode, elapsed.Round(time.Millisecond))

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if isRetriableStatus(resp.StatusCode) {
			return "", fmt.Errorf(
				"retriable http status %d: %s",
				resp.StatusCode,
				string(responseBody),
			)
		}

		return "", fmt.Errorf("%w: http status %d", ErrNonRetriable, resp.StatusCode)
	}

	return string(responseBody), nil
}

// extractAssistantContent extracts assistant text from raw JSON response.
func extractAssistantContent(raw string) (string, error) {
	var resp ChatCompletionResponse

	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return "", fmt.Errorf("failed to parse chat completion response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices returned in chat completion response")
	}

	content := resp.Choices[0].Message.Content
	if content == "" {
		return "", fmt.Errorf("empty assistant content in chat completion response")
	}

	return content, nil
}

// PrintValidationSummary prints a summary of validation results.
func PrintValidationSummary(results []EvalResult, accuracy float64) {
	logger.Infof("-------------------------------------------")
	logger.Infof("RAG Golden Dataset Validation Results")
	logger.Infof("-------------------------------------------")
	logger.Infof("Total Prompts: %d", len(results))
	logger.Infof("Accuracy: %.2f%%", accuracy*percentMultiplier)

	for _, r := range results {
		if !r.Passed {
			logger.Infof(
				"[FAIL] %s | %s",
				r.Question,
				r.Details,
			)
		}
	}
}
