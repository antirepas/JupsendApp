package ai

import (
	"context"
	"fmt"
	"strings"

	"emailtracker.com/config"
)

// transformModel picks a chat model suitable for short template transforms (fit, summarize).
// Reasoning models like gpt-5-nano often return empty content for these tasks.
func transformModel() string {
	primary := strings.TrimSpace(config.OpenAIModel)
	if isReasoningChatModel(primary) {
		if fb := fallbackOpenAIModel(primary); fb != "" {
			return fb
		}
		return "gpt-4.1-mini"
	}
	if primary != "" {
		return primary
	}
	return "gpt-4.1-mini"
}

// CompleteTransform runs fit/summarize-style prompts on a chat model.
func CompleteTransform(ctx context.Context, system, user string) (string, error) {
	c := getCompleter()
	if !isDefaultClient(c) {
		return c.Complete(ctx, system, user)
	}

	apiKey := config.OpenAIAPIKey
	if apiKey == "" {
		return "", fmt.Errorf("AI not configured")
	}

	client := c.(*Client)
	model := transformModel()
	type attempt struct {
		model   string
		payload map[string]interface{}
	}
	attempts := []attempt{{model, buildChatCompletionPayload(model, system, user)}}
	if fb := fallbackOpenAIModel(model); fb != "" && !strings.EqualFold(fb, model) {
		attempts = append(attempts, attempt{fb, buildChatCompletionPayload(fb, system, user)})
	}

	var lastErr error
	for _, a := range attempts {
		text, err := client.doComplete(ctx, apiKey, a.payload)
		if err == nil && text != "" {
			return text, nil
		}
		if err == nil {
			lastErr = fmt.Errorf("openai: empty response")
		} else {
			lastErr = err
		}
		if !shouldRetryOpenAI(lastErr) {
			break
		}
	}
	return "", lastErr
}

func isDefaultClient(c Completer) bool {
	_, ok := c.(*Client)
	return ok
}
