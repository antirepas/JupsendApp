package ai

import (
	"context"
	"encoding/json"
	"fmt"
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

// BuildReplyVerifyState builds Jev state for auto-reply verification.
func BuildReplyVerifyState(inboundSubject, inboundBody, proposedSubject, proposedBody, intent string) map[string]interface{} {
	trim := func(s string, n int) string {
		s = strings.TrimSpace(s)
		r := []rune(s)
		if len(r) > n {
			return string(r[:n]) + "…"
		}
		return s
	}
	return map[string]interface{}{
		"inbound": map[string]interface{}{
			"subject": trim(inboundSubject, 300),
			"body":    trim(inboundBody, 2500),
			"intent":  intent,
		},
		"proposed_reply": map[string]interface{}{
			"subject": trim(proposedSubject, 300),
			"body":    trim(proposedBody, 2500),
		},
		"evaluation_policy": map[string]interface{}{
			"goal": "Decide whether this fixed template reply is an appropriate, grammatical answer to the inbound message.",
			"rules": []string{
				"The reply does not need to address every detail, but must not ignore a clear question or request.",
				"Grammar and clarity must be acceptable for professional email.",
				"Commitment must stay appropriate for cold outreach follow-up.",
				"If the template is clearly mismatched to the intent (e.g. pricing ask answered with a breakup email), skip.",
			},
		},
	}
}

func ReplyVerifyQuestions() map[string]interface{} {
	return map[string]interface{}{
		"decision": map[string]interface{}{
			"type":         "choice",
			"instructions": `Should this proposed reply be sent as an automatic response?`,
			"criteria": map[string]string{
				"send": "The reply appropriately and grammatically answers the inbound message.",
				"skip": "The reply is mismatched, unclear, ungrammatical, or inappropriate to send automatically.",
			},
		},
		"answers_inbound": map[string]interface{}{
			"type":         "noul",
			"instructions": `Does the proposed reply substantively address the inbound message given its intent?`,
		},
		"grammatically_ok": map[string]interface{}{
			"type":         "noul",
			"instructions": `Is the proposed reply grammatically clear and professional?`,
		},
		"appropriate_commitment": map[string]interface{}{
			"type":         "noul",
			"instructions": `Is the requested next step proportionate and appropriate?`,
		},
		"primary_gap": map[string]interface{}{
			"type":         "choice",
			"instructions": `If decision is skip, what is the single main reason? If decision is send, pick other.`,
			"criteria": map[string]string{
				"does_not_answer":  "Ignores a clear question, request, or buying signal in the inbound message.",
				"wrong_intent":     "Tone or content mismatches the classified intent.",
				"grammar":          "Unclear, broken, or unprofessional writing.",
				"bad_commitment":   "Asks for too much or an inappropriate next step for cold outreach.",
				"generic_template": "Reads like an unrelated canned template that does not fit this reply.",
				"other":            "Another issue not listed above.",
			},
		},
	}
}

// ReplyVerifyResult is the verify-layer outcome.
type ReplyVerifyResult struct {
	Decision               string  `json:"decision"` // send | skip
	Confidence             float64 `json:"confidence"`
	Model                  string  `json:"model"`
	PrimaryGap             string  `json:"primary_gap"`
	AnswersInbound         float64 `json:"answers_inbound"`
	GrammaticallyOK        float64 `json:"grammatically_ok"`
	AppropriateCommitment  float64 `json:"appropriate_commitment"`
	Reason                 string  `json:"reason"` // human-readable skip explanation
}

// VerifyAutoReply runs Jev to gate an automatic template reply.
func VerifyAutoReply(ctx context.Context, inboundSubject, inboundBody, proposedSubject, proposedBody, intent string) (ReplyVerifyResult, error) {
	if !JEVConfigured() {
		return ReplyVerifyResult{}, fmt.Errorf("Jev is not configured")
	}
	client := NewJevClient()
	resp, err := client.Evaluate(ctx, BuildReplyVerifyState(inboundSubject, inboundBody, proposedSubject, proposedBody, intent), ReplyVerifyQuestions())
	if err != nil {
		return ReplyVerifyResult{}, err
	}
	out := ReplyVerifyResult{Model: resp.Model, Decision: "skip"}
	if raw, ok := resp.Answers["decision"]; ok {
		if c, err := DecodeJevChoice(raw); err == nil {
			choice := strings.ToLower(strings.TrimSpace(c.Choice))
			if choice == "send" || choice == "skip" {
				out.Decision = choice
			}
			out.Confidence = c.Confidence
		}
	}
	if raw, ok := resp.Answers["primary_gap"]; ok {
		if c, err := DecodeJevChoice(raw); err == nil {
			out.PrimaryGap = strings.ToLower(strings.TrimSpace(c.Choice))
		}
	}
	out.AnswersInbound = decodeNoulScore(resp.Answers, "answers_inbound")
	out.GrammaticallyOK = decodeNoulScore(resp.Answers, "grammatically_ok")
	out.AppropriateCommitment = decodeNoulScore(resp.Answers, "appropriate_commitment")
	out.Reason = FormatReplyVerifySkipReason(out, intent)
	return out, nil
}

func decodeNoulScore(answers map[string]json.RawMessage, key string) float64 {
	raw, ok := answers[key]
	if !ok {
		return 0
	}
	n, err := DecodeJevNoul(raw)
	if err != nil {
		return 0
	}
	return n.Noul
}

// FormatReplyVerifySkipReason builds a concise explanation for conversation notes / UI.
func FormatReplyVerifySkipReason(vr ReplyVerifyResult, intent string) string {
	if vr.Decision == "send" {
		return ""
	}
	gap := strings.TrimSpace(vr.PrimaryGap)
	gapLabel := map[string]string{
		"does_not_answer":  "does not address what they wrote",
		"wrong_intent":     "mismatches the classified intent",
		"grammar":          "has clarity/grammar issues",
		"bad_commitment":   "asks for an inappropriate next step",
		"generic_template": "reads like a mismatched canned template",
		"other":            "did not pass verification",
	}[gap]
	if gapLabel == "" {
		gapLabel = "did not pass verification"
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("Jev skipped auto-reply (intent=%s, conf=%.0f%%): %s", intent, vr.Confidence*100, gapLabel))
	var scores []string
	if vr.AnswersInbound > 0 {
		scores = append(scores, fmt.Sprintf("answers inbound %.0f%%", vr.AnswersInbound*100))
	}
	if vr.GrammaticallyOK > 0 {
		scores = append(scores, fmt.Sprintf("grammar %.0f%%", vr.GrammaticallyOK*100))
	}
	if vr.AppropriateCommitment > 0 {
		scores = append(scores, fmt.Sprintf("commitment %.0f%%", vr.AppropriateCommitment*100))
	}
	if len(scores) > 0 {
		parts = append(parts, strings.Join(scores, ", "))
	}
	return strings.Join(parts, ". ")
}
