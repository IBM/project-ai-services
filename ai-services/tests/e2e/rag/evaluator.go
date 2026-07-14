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

// RAG transport tuning constants — values are explained in sharedRAGClient below.
const (
	similarityHealthTimeout  = 10 * time.Second       //nolint:mnd
	ragMaxIdleConnsPerHost   = 4                      //nolint:mnd
	ragMaxConnsPerHost       = 8                      //nolint:mnd
	ragIdleConnTimeout       = 90 * time.Second       //nolint:mnd
	ragResponseHeaderTimeout = 90 * time.Second       //nolint:mnd
	ragDialTimeout           = 15 * time.Second       //nolint:mnd
	ragDialKeepAlive         = 30 * time.Second       //nolint:mnd
	ragRetryBackoffBase      = 200 * time.Millisecond //nolint:mnd
)

// similarityHealthClient skips TLS verification to support both plain http:// (legacy podman) and https:// nip.io self-signed certs (catalog).
var similarityHealthClient = &http.Client{
	Timeout: similarityHealthTimeout,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec
		},
	},
}

// sharedRAGClient pools TCP connections for all RAG/Judge requests; timeout exceeds per-question deadline so ctx cancellation fires first; ResponseHeaderTimeout guards dead keep-alive sockets.
var sharedRAGClient = &http.Client{
	Timeout: httpClientTimeout,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec
		},
		MaxIdleConnsPerHost:   ragMaxIdleConnsPerHost,
		MaxConnsPerHost:       ragMaxConnsPerHost,
		IdleConnTimeout:       ragIdleConnTimeout,
		ResponseHeaderTimeout: ragResponseHeaderTimeout,
		DialContext: (&net.Dialer{
			Timeout:   ragDialTimeout,
			KeepAlive: ragDialKeepAlive,
		}).DialContext,
	},
}

// waitForEndpointReady polls targetURL until HTTP 200 or ctx is done, using callbacks to format per-attempt log messages.
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

// WaitForRAGBackendReady polls ragBaseURL/v1/models until HTTP 200, ensuring the LLM is fully up before starting the judge container.
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

// WaitForSimilarityAPIReady polls similarityBaseURL/health until HTTP 200 or ctx deadline is exceeded.
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

// catalogClientNew wraps catalogClient.New() as a variable to allow test overrides and avoid import cycles.
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

	// httpClientTimeout: set longer than perQuestionTimeout so context cancellation always fires first.
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

// RunWithRetry executes fn with exponential back-off; stops on context cancellation or ErrNonRetriable.
func RunWithRetry(
	ctx context.Context,
	maxRetries int,
	fn func(context.Context) (string, error),
) (string, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
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

		if ctx.Err() != nil {
			return "", fmt.Errorf("context cancelled after attempt %d: %w", attempt+1, ctx.Err())
		}

		if errors.Is(err, ErrNonRetriable) {
			return "", err
		}

		if attempt < maxRetries {
			backoff := time.Duration(attempt+1) * ragRetryBackoffBase
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

// buildJudgeUserPrompt builds the user prompt for the judge LLM.
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

// loadFreshBearerToken returns the catalog access token, auto-refreshing if expired.
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

	const refreshSkew = 30 * time.Second
	exp, jwtErr := jwtTokenExpiry(creds.AccessToken)
	if jwtErr != nil || time.Until(exp) < refreshSkew {
		catalogClient, clientErr := catalogClientNew()
		if clientErr != nil {
			logger.Warningf("[RAG] could not refresh catalog token: %v", clientErr)

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

// buildPostJSONRequest marshals body into an *http.Request with Content-Type and Bearer token.
func buildPostJSONRequest(ctx context.Context, baseURL, path string, body map[string]any) (*http.Request, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewBuffer(b))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if token := loadFreshBearerToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return req, nil
}

// handlePostJSONResponse drains and closes the response body, returning it as a string.
func handlePostJSONResponse(ctx context.Context, resp *http.Response, baseURL, path string, elapsed time.Duration) (string, error) {
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
			return "", fmt.Errorf("retriable http status %d: %s", resp.StatusCode, string(responseBody))
		}

		return "", fmt.Errorf("%w: http status %d", ErrNonRetriable, resp.StatusCode)
	}

	_ = ctx // ctx is kept in the signature for future use (e.g. trace propagation)

	return string(responseBody), nil
}

// PostJSON sends a JSON POST request using the shared HTTP client and returns the response body.
func PostJSON(
	ctx context.Context,
	baseURL string,
	path string,
	body map[string]any,
) (string, error) {
	req, err := buildPostJSONRequest(ctx, baseURL, path, body)
	if err != nil {
		return "", err
	}

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
		if ctx.Err() != nil {
			logger.Errorf("[RAG][http] POST %s%s — cancelled after %s: context=%v",
				baseURL, path, elapsed.Round(time.Millisecond), ctx.Err())

			return "", fmt.Errorf("request cancelled: %w", ctx.Err())
		}
		logger.Errorf("[RAG][http] POST %s%s — transport error after %s: %v",
			baseURL, path, elapsed.Round(time.Millisecond), err)

		return "", fmt.Errorf("http request failed: %w", err)
	}

	return handlePostJSONResponse(ctx, resp, baseURL, path, elapsed)
}

// extractAssistantContent extracts the assistant message content from a chat completion response.
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
