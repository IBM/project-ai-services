package rag

import (
	"context"
	"errors"
	"strings"
)

var ErrInvalidJudgeResponse = errors.New("invalid judge response format")

// JudgeSystemPrompt defines the strict evaluation instructions provided to the judge LLM.
const judgeSystemPrompt =
  "YOU ARE AN AUTOMATED ANSWER VERIFIER.\n" +
  "YOUR TASK IS FACT VERIFICATION, NOT QUALITY JUDGMENT.\n" +
  "\n" +
  "You evaluate a MODEL ANSWER using ONLY the provided GOLDEN ANSWER.\n" +
  "You MUST NOT use outside knowledge.\n" +
  "You MUST NOT add new facts, expectations, or requirements beyond the GOLDEN ANSWER.\n" +
  "\n" +
  "INPUTS:\n" +
  "- QUESTION\n" +
  "- GOLDEN ANSWER (defines ALL required facts)\n" +
  "- MODEL ANSWER\n" +
  "\n" +
  "EVALUATION RULES (FOLLOW STRICTLY):\n" +
  "1. Identify the required facts using ONLY what is explicitly stated in the GOLDEN ANSWER.\n" +
  "2. Do NOT require facts that are implied, assumed, or commonly known but not explicitly stated.\n" +
  "3. If the GOLDEN ANSWER lists multiple details or examples, the MODEL ANSWER is acceptable\n" +
  "   if it correctly covers the main idea or purpose, even if some specific numbers, formats,\n" +
  "   versions, examples, or implementation details are missing.\n" +
  "4. Check whether EACH required fact (at the correct level of detail) is present and correct\n" +
  "   in the MODEL ANSWER.\n" +
  "   - Different wording or structure is acceptable.\n" +
  "   - Extra correct information MUST be ignored.\n" +
  "   - Extra incorrect information must be ignored unless it directly contradicts a required fact.\n" +
  "\n" +
  "VERDICT LOGIC:\n" +
  "- YES: the MODEL ANSWER correctly covers the required facts or main concepts from the GOLDEN ANSWER.\n" +
  "- NO: a required fact or core concept from the GOLDEN ANSWER is missing, incorrect,\n" +
  "      contradicted, or explicitly denied.\n" +
  "\n" +
  "IMPORTANT CONSTRAINTS:\n" +
  "- DO NOT penalize extra information, additional explanation, or deeper technical detail.\n" +
  "- DO NOT require the MODEL ANSWER to mention every example, specification, number,\n" +
  "  technology name, or configuration listed in the GOLDEN ANSWER.\n" +
  "- DO NOT judge quality, style, completeness, or helpfulness.\n" +
  "- If a required fact or concept is unclear in the MODEL ANSWER, treat it as missing.\n" +
  "\n" +
  "FAILURE HANDLING:\n" +
  "If you are unsure, confused, or cannot confidently verify all required facts, output:\n" +
  "VERDICT: NO\n" +
  "REASON: One or more required facts are missing or unclear.\n" +
  "\n" +
  "LANGUAGE:\n" +
  "- Output MUST be in English only.\n" +
  "\n" +
  "OUTPUT FORMAT (STRICT – NO EXCEPTIONS):\n" +
  "- Output EXACTLY two lines.\n" +
  "- No explanations, no markdown, no bullets, no extra text.\n" +
  "\n" +
  "MANDATORY FORMAT:\n" +
  "VERDICT: YES or NO\n" +
  "REASON: one short sentence stating the missing or incorrect required fact, or confirming full coverage\n";

func AskJudgeWithFormatRetry(
	ctx context.Context,
	maxRetries int,
	judgeBaseURL string,
	question string,
	ragAns string,
	goldenAns string,
) (verdict string, reason string, err error) {
	var lastErr error

	for attempt := 0; attempt <= 1; attempt++ {
		raw, err := RunWithRetry(ctx, maxRetries, func(ctx context.Context) (string, error) {
			return AskJudge(ctx, judgeBaseURL, question, ragAns, goldenAns)
		})

		if err != nil {
			// Infra / timeout / non-retriable error
			return "", "", err
		}

		verdict, reason, err = ParseJudgeResponse(raw)
		if err == nil {
			return verdict, reason, nil
		}

		if !errors.Is(err, ErrInvalidJudgeResponse) {
			return "", "", err
		}

		// Invalid format → retry once
		lastErr = err
	}

	return "", "", lastErr
}

// ParseJudgeResponse extracts the verdict and reason from the judge output. The response must contain both VERDICT and REASON fields.
func ParseJudgeResponse(resp string) (verdict string, reason string, err error) {
	var foundVerdict, foundReason bool

	for _, line := range strings.Split(resp, "\n") {
		clean := strings.Trim(strings.TrimSpace(line), "*#- ")

		if clean == "" {
			continue
		}

		lower := strings.ToLower(clean)

		switch {
			case strings.HasPrefix(lower, "verdict:"):
				value := strings.TrimSpace(clean[len("VERDICT:"):])
				verdict = strings.ToUpper(value)
				foundVerdict = true

			case strings.HasPrefix(lower, "reason:"):
				reason = strings.TrimSpace(clean[len("REASON:"):])
				foundReason = true
			}
	}

	if !foundVerdict || !foundReason || (verdict != "YES" && verdict != "NO") {
		return "", "", ErrInvalidJudgeResponse
	}

	return verdict, reason, nil
}