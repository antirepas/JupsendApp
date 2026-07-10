package ai

import "strings"

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
		payload["max_completion_tokens"] = 512
		payload["reasoning_effort"] = "minimal"
		return payload
	}
	payload["max_tokens"] = 512
	payload["temperature"] = 0.7
	return payload
}
