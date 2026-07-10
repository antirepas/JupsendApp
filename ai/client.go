package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"emailtracker.com/config"
)

const preservePlaceholders = "Never remove or rename {{placeholder}} tokens in the text."

type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

type Client struct {
	apiKey string
	model  string
	http   *http.Client
}

var (
	DefaultCompleter Completer = &Client{
		http: &http.Client{Timeout: 45 * time.Second},
	}
	completerMu sync.RWMutex
)

func SetDefaultCompleter(c Completer) {
	completerMu.Lock()
	defer completerMu.Unlock()
	DefaultCompleter = c
}

func getCompleter() Completer {
	completerMu.RLock()
	defer completerMu.RUnlock()
	return DefaultCompleter
}

func Enabled() bool {
	return config.AIEnabled()
}

func (c *Client) Complete(ctx context.Context, system, user string) (string, error) {
	apiKey := config.OpenAIAPIKey
	primary := config.OpenAIModel
	if apiKey == "" {
		return "", fmt.Errorf("AI not configured")
	}

	type attempt struct {
		model   string
		payload map[string]interface{}
	}
	attempts := []attempt{
		{primary, buildChatCompletionPayload(primary, system, user)},
	}
	if isReasoningChatModel(primary) {
		attempts = append(attempts, attempt{primary, buildReasoningFallbackPayload(primary, system, user)})
	}
	if fb := fallbackOpenAIModel(primary); fb != "" {
		attempts = append(attempts, attempt{fb, buildChatCompletionPayload(fb, system, user)})
	}

	var lastErr error
	for _, a := range attempts {
		if a.model == "" {
			continue
		}
		text, err := c.doComplete(ctx, apiKey, a.payload)
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

func (c *Client) doComplete(ctx context.Context, apiKey string, payload map[string]interface{}) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai api %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("openai: empty response")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

func Complete(ctx context.Context, system, user string) (string, error) {
	return getCompleter().Complete(ctx, system, user)
}

func RewritePrompt(action string) string {
	switch action {
	case "shorten":
		return "You shorten email snippets. " + preservePlaceholders + " Return only the rewritten text."
	case "soften":
		return "You soften the tone of email snippets. " + preservePlaceholders + " Return only the rewritten text."
	case "direct":
		return "You make email snippets more direct and concise. " + preservePlaceholders + " Return only the rewritten text."
	case "grammar":
		return "You fix grammar and spelling in email snippets. " + preservePlaceholders + " Return only the rewritten text."
	default:
		return "Rewrite the snippet. " + preservePlaceholders + " Return only the rewritten text."
	}
}

func SoftenBodyPrompt() string {
	return "Soften the tone of this email body HTML while keeping structure and links. " + preservePlaceholders + " Return only the HTML body."
}

func SubjectAlternativesPrompt() string {
	return "Suggest 2-3 alternative email subject lines. " + preservePlaceholders + " Return a JSON array of strings only, no markdown."
}

func PersonalizationHintPrompt() string {
	return "Suggest one optional improvement for this cold email draft. " +
		preservePlaceholders + " " +
		"Do not require specific placeholders like {{company}}. " +
		"If the draft is already fine, respond with exactly: SKIP. " +
		"One sentence only, no quotes."
}

func ToneCheckPrompt() string {
	return `Analyze the tone of this email. Reply with JSON only: {"tone":"formal|casual|neutral","message":"one short sentence for the user"}`
}

func FitPrompt() string {
	return "You rewrite a company or product description so it fits grammatically at ___ in the sentence context. " +
		"Return ONLY the words that replace ___ — not the full sentence, and not a standalone pitch. " +
		"The inserted phrase must read naturally when you read the full sentence aloud. " +
		"Drop product-category labels (e.g. B2B SaaS, platform, bootstrapped) unless the grammar slot requires them. " +
		"Never repeat helping/helps if the context already contains helps. " +
		"Keep 4–15 words. Preserve who they serve and the core outcome. No quotes or explanation."
}

func SummarizePrompt(maxWords int) string {
	return fmt.Sprintf(
		"Summarize the following text to at most %d words. Return only the summary, no quotes or explanation.",
		maxWords,
	)
}
