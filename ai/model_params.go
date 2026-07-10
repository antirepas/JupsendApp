package ai

import (
	"strings"

	"emailtracker.com/config"
)

// isReasoningChatModel reports whether the model rejects legacy sampling params
// (temperature, max_tokens) used by GPT-4 style chat models.
func isReasoningChatModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return false
	}
	if strings.Contains(m, "chat") {
		return false
	}
	switch {
	case strings.HasPrefix(m, "o1"),
		strings.HasPrefix(m, "o3"),
		strings.HasPrefix(m, "o4"),
		strings.HasPrefix(m, "gpt-5"):
		return true
	default:
		return false
	}
}

func buildChatCompletionPayload(model, system, user string) map[string]interface{} {
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	if isReasoningChatModel(model) {
		payload["max_completion_tokens"] = 2048
		payload["reasoning_effort"] = "minimal"
		return payload
	}
	payload["max_tokens"] = 512
	payload["temperature"] = 0.7
	return payload
}

// buildReasoningFallbackPayload retries without optional params some models reject.
func buildReasoningFallbackPayload(model, system, user string) map[string]interface{} {
	return map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"max_completion_tokens": 2048,
	}
}

func fallbackOpenAIModel(primary string) string {
	fallback := strings.TrimSpace(config.OpenAIFallbackModel)
	if fallback == "" {
		fallback = "gpt-4.1-mini"
	}
	if strings.EqualFold(fallback, primary) {
		return ""
	}
	return fallback
}

func shouldRetryOpenAI(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "openai api 4") ||
		strings.Contains(msg, "unsupported") ||
		strings.Contains(msg, "empty response")
}
