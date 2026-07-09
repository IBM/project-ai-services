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

// WaitForRAGBackendReady polls ragBaseURL/v1/models until it returns HTTP 200,
// meaning the chat-bot backend has successfully connected to the LLM (vLLM).
// The LLM pod (llm-<slug>) is the last and most resource-heavy pod to start —
// it can take 20-30 minutes on Spyre hardware. This gate ensures we don't start
// the judge container (SetupLLMAsJudge) while the LLM pod is still consuming
// resources during initialisation, which would cause all other pods to crash.
// ragBaseURL is the full base URL, e.g. "https://chat-bot-backend-<slug>.<ip>.nip.io".
func WaitForRAGBackendReady(ctx context.Context, ragBaseURL string, pollInterval time.Duration) error {
	modelsURL := ragBaseURL + "/v1/models"
	attempt := 0

	for {
		attempt++
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
		if err != nil {
			return fmt.Errorf("build /v1/models request: %w", err)
		}

		if token := loadFreshBearerToken(); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := similarityHealthClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				logger.Infof("[RAG] LLM is ready — chat-bot /v1/models returned 200 after %d attempt(s)", attempt)
				return nil
			}
			logger.Infof("[RAG] LLM not ready yet (HTTP %d on /v1/models), attempt %d — retrying in %s",
				resp.StatusCode, attempt, pollInterval)
		} else {
			logger.Infof("[RAG] chat-bot /v1/models unreachable (attempt %d): %v — retrying in %s",
				attempt, err, pollInterval)
		}

		if ctx.Err() != nil {
			return fmt.Errorf("timed out waiting for LLM via %s after %d attempt(s): %w",
				modelsURL, attempt, ctx.Err())
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for LLM via %s after %d attempt(s): %w",
				modelsURL, attempt, ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

// WaitForSimilarityAPIReady polls the similarity-api /health endpoint until it
// returns HTTP 200 or the context deadline is exceeded.
// similarityBaseURL is the full base URL extracted from 'application info' output,
// e.g. "https://similarity-api-<slug>.<ip>.nip.io" (catalog) or "http://<ip>:<port>" (legacy).
func WaitForSimilarityAPIReady(ctx context.Context, similarityBaseURL string, pollInterval time.Duration) error {
	healthURL := similarityBaseURL + "/health"
	attempt := 0

	for {
		attempt++
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if err != nil {
			return fmt.Errorf("build similarity health request: %w", err)
		}

		resp, err := similarityHealthClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				logger.Infof("[RAG] similarity-api healthy after %d attempt(s) — %s", attempt, healthURL)
				return nil
			}
			logger.Infof("[RAG] similarity-api not ready yet (HTTP %d), attempt %d — retrying in %s",
				resp.StatusCode, attempt, pollInterval)
		} else {
			logger.Infof("[RAG] similarity-api unreachable (attempt %d): %v — retrying in %s",
				attempt, err, pollInterval)
		}

		if ctx.Err() != nil {
			return fmt.Errorf("timed out waiting for similarity-api at %s after %d attempt(s): %w",
				healthURL, attempt, ctx.Err())
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for similarity-api at %s after %d attempt(s): %w",
				healthURL, attempt, ctx.Err())
		case <-time.After(pollInterval):
		}
	}
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
	httpClientTimeout = 8 * time.Minute
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
func RunWithRetry(
	ctx context.Context,
	maxRetries int,
	fn func(context.Context) (string, error),
) (string, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err := fn(ctx)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		if errors.Is(err, ErrNonRetriable) {
			return "", err
		}

		// wait before the next attempt
		if attempt < maxRetries {
			time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
		}
	}

	return "", lastErr
}

// AskRAG sends a question to the RAG backend and returns the answer.
func AskRAG(ctx context.Context, baseURL string, question string) (string, error) {
	req := map[string]interface{}{
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

	req := map[string]interface{}{
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

// PostJSON sends a POST request with a JSON body and returns the response body as a string.
func PostJSON(
	ctx context.Context,
	baseURL string,
	path string,
	body map[string]interface{},
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

	// The catalog backend URL uses nip.io with a self-signed certificate.
	// InsecureSkipVerify is required — same pattern as the health-check client
	// in cli/runner.go. This is intentional for e2e test environments only.
	client := &http.Client{
		Timeout: httpClientTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec
			},
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		return "", fmt.Errorf("http request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

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
