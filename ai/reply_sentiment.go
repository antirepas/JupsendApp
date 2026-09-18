package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type ReplySentimentResult struct {
	Sentiment  string  `json:"sentiment"` // positive | negative | neutral
	Confidence float64 `json:"confidence"`
}

const replySentimentSystem = `You classify cold-email replies for B2B outreach.
Return ONLY compact JSON: {"sentiment":"positive"|"negative"|"neutral","confidence":0.0-1.0}
- positive: interested, wants meeting, asks pricing, open to chat, soft yes
- negative: not interested, unsubscribe, remove me, hostile, hard no, wrong person refusing
- neutral: auto-ack without intent, unclear, out-of-office that is not a hard no, questions without intent
Do not invent content. If unsure, use neutral with low confidence.`

// ClassifyReplySentiment uses the configured Completer to label an inbound reply.
func ClassifyReplySentiment(ctx context.Context, subject, body string) (ReplySentimentResult, error) {
	if !Enabled() {
		return ReplySentimentResult{}, fmt.Errorf("AI not configured")
	}
	subject = strings.TrimSpace(subject)
	body = strings.TrimSpace(body)
	if len(body) > 4000 {
		body = body[:4000]
	}
	user := fmt.Sprintf("Subject: %s\n\nBody:\n%s", subject, body)
	raw, err := getCompleter().Complete(ctx, replySentimentSystem, user)
	if err != nil {
		return ReplySentimentResult{}, err
	}
	return ParseReplySentimentJSON(raw)
}

// ParseReplySentimentJSON extracts sentiment from model output (tolerates markdown fences).
func ParseReplySentimentJSON(raw string) (ReplySentimentResult, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ReplySentimentResult{}, fmt.Errorf("empty AI response")
	}
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var out ReplySentimentResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return ReplySentimentResult{}, err
	}
	switch strings.ToLower(strings.TrimSpace(out.Sentiment)) {
	case "positive", "negative", "neutral":
		out.Sentiment = strings.ToLower(strings.TrimSpace(out.Sentiment))
	default:
		return ReplySentimentResult{}, fmt.Errorf("invalid sentiment %q", out.Sentiment)
	}
	if out.Confidence < 0 {
		out.Confidence = 0
	}
	if out.Confidence > 1 {
		out.Confidence = 1
	}
	return out, nil
}
