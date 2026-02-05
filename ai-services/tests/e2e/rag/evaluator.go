package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	percentMultiplier = 100
	judgeUserPromptTemplate =
		"QUESTION:\n" +
		"{question}\n" +
		"\n" +
		"GOLDEN ANSWER:\n" +
		"{golden_answer}\n" +
		"\n" +
		"MODEL ANSWER:\n" +
		"{model_answer}\n"
	httpClientTimeout = 4 * time.Minute
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

	client := &http.Client{
		Timeout: httpClientTimeout,
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