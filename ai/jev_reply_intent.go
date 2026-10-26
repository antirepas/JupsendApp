package ai

import (
	"context"
	"strings"
)

// ReplyIntentResult is the Jev (or fallback) classification of an inbound reply.
type ReplyIntentResult struct {
	Intent     string  `json:"intent"`
	Sentiment  string  `json:"sentiment"`
	Confidence float64 `json:"confidence"`
	Model      string  `json:"model"`
}

func BuildReplyIntentState(subject, body string) map[string]interface{} {
	body = strings.TrimSpace(body)
	if len(body) > 4000 {
		body = body[:4000]
	}
	return map[string]interface{}{
		"reply": map[string]interface{}{
			"subject": strings.TrimSpace(subject),
			"body":    body,
			"channel": "cold_email_inbound",
		},
		"evaluation_policy": map[string]interface{}{
			"goal": "Classify the prospect's intent so outreach automation can respond appropriately.",
			"rules": []string{
				"Pick the single best intent; prefer specific intents over vague ones.",
				"OOO / auto-replies are ooo, not interested.",
				"Unsubscribe / remove me / stop emailing is unsubscribe.",
				"Wrong person / left company / not the right contact is wrong_person.",
				"Soft delays like 'circle back next quarter' are not_now.",
				"Asking for price, rates, or packages is pricing.",
				"Asking to meet, book time, or hop on a call is meeting_ask.",
				"Introducing someone else or asking for a referral intro is referral.",
				"Clear interest without a meeting/pricing ask is interested.",
			},
		},
	}
}

func ReplyIntentQuestions() map[string]interface{} {
	return map[string]interface{}{
		"intent": map[string]interface{}{
			"type":         "choice",
			"instructions": `Classify this inbound cold-email reply into exactly one intent.`,
			"criteria": map[string]string{
				"interested":   "Positive interest or soft yes without a concrete meeting/pricing ask.",
				"meeting_ask":  "Wants a call, meeting, or demo time.",
				"pricing":      "Asks about price, cost, packages, or commercial terms.",
				"referral":     "Points to another person or offers an introduction.",
				"not_now":      "Not interested right now / defer / maybe later — not a hard unsubscribe.",
				"ooo":          "Out-of-office or automated vacation reply.",
				"unsubscribe":  "Asks to stop emailing, unsubscribe, or remove from list.",
				"wrong_person": "Wrong contact, left company, or not the decision-maker refusing further mail.",
			},
		},
	}
}

var knownReplyIntents = map[string]bool{
	"interested": true, "not_now": true, "ooo": true, "unsubscribe": true,
	"wrong_person": true, "referral": true, "meeting_ask": true, "pricing": true,
}

func normalizeIntent(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if knownReplyIntents[s] {
		return s
	}
	return ""
}

func intentToSentiment(intent string) string {
	switch normalizeIntent(intent) {
	case "interested", "meeting_ask", "pricing", "referral":
		return "positive"
	case "not_now", "unsubscribe", "wrong_person":
		return "negative"
	default:
		return "neutral"
	}
}

// ClassifyReplyIntent runs Jev when configured; otherwise falls back to Completer sentiment→intent.
func ClassifyReplyIntent(ctx context.Context, subject, body string) (ReplyIntentResult, error) {
	if JEVConfigured() {
		client := NewJevClient()
		resp, err := client.Evaluate(ctx, BuildReplyIntentState(subject, body), ReplyIntentQuestions())
		if err == nil {
			raw, ok := resp.Answers["intent"]
			if ok {
				if c, err := DecodeJevChoice(raw); err == nil {
					intent := normalizeIntent(c.Choice)
					if intent == "" {
						intent = "ooo"
					}
					return ReplyIntentResult{
						Intent:     intent,
						Sentiment:  intentToSentiment(intent),
						Confidence: c.Confidence,
						Model:      resp.Model,
					}, nil
				}
			}
		}
		// Fall through to Completer on Jev failure.
	}
	return classifyReplyIntentFallback(ctx, subject, body)
}

func classifyReplyIntentFallback(ctx context.Context, subject, body string) (ReplyIntentResult, error) {
	sent, err := ClassifyReplySentiment(ctx, subject, body)
	if err != nil {
		return ReplyIntentResult{}, err
	}
	intent := "ooo"
	switch sent.Sentiment {
	case "positive":
		intent = "interested"
	case "negative":
		intent = "not_now"
		lower := strings.ToLower(subject + " " + body)
		if strings.Contains(lower, "unsub") || strings.Contains(lower, "remove me") || strings.Contains(lower, "stop email") {
			intent = "unsubscribe"
		}
		if strings.Contains(lower, "wrong person") || strings.Contains(lower, "no longer") {
			intent = "wrong_person"
		}
	case "neutral":
		lower := strings.ToLower(subject + " " + body)
		if strings.Contains(lower, "out of office") || strings.Contains(lower, "ooo") || strings.Contains(lower, "away from") {
			intent = "ooo"
		} else {
			intent = "not_now"
		}
	}
	return ReplyIntentResult{
		Intent:     intent,
		Sentiment:  sent.Sentiment,
		Confidence: sent.Confidence,
		Model:      "completer-fallback",
	}, nil
}
