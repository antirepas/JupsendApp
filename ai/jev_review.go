package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// TemplateReviewInput is what the template editor sends for a Jev check.
// BodyPlain should already be HTML-stripped (avoid importing util from ai).
type TemplateReviewInput struct {
	Subject      string
	BodyPlain    string
	TemplateName string
	SenderEmail  string
	SenderName   string
	Variables    []string
	Product      string // optional product/offer label
}

// TemplateReviewCheck is one noul check for the UI.
type TemplateReviewCheck struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	Yes   float64 `json:"yes"` // P(yes)
}

// TemplateReviewChoice is a choice answer for the UI.
type TemplateReviewChoice struct {
	ID            string             `json:"id"`
	Label         string             `json:"label"`
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// TemplateReviewResult is the API payload for the modal.
type TemplateReviewResult struct {
	Model   string                 `json:"model"`
	Decision TemplateReviewChoice  `json:"decision"`
	Checks  []TemplateReviewCheck  `json:"checks"`
	PrimaryIssue TemplateReviewChoice `json:"primary_issue"`
	ProblemArea  TemplateReviewChoice `json:"problem_area"`
	RecommendedFix TemplateReviewChoice `json:"recommended_fix"`
	Usage   map[string]int         `json:"usage"`
}

var reQuestionSentence = regexp.MustCompile(`(?s)([^.!?\n]{8,}?\?)`)

// BuildTemplateReviewState builds Jev state without prospect contact fields —
// this is a template-quality review, not a per-lead review.
func BuildTemplateReviewState(in TemplateReviewInput) map[string]interface{} {
	body := strings.TrimSpace(in.BodyPlain)
	subject := strings.TrimSpace(in.Subject)
	cta := extractCTA(body)
	product := strings.TrimSpace(in.Product)
	if product == "" {
		product = strings.TrimSpace(in.TemplateName)
	}
	if product == "" {
		product = "cold outreach offer"
	}
	from := strings.TrimSpace(in.SenderEmail)
	senderName := strings.TrimSpace(in.SenderName)

	return map[string]interface{}{
		"email": map[string]interface{}{
			"subject":     subject,
			"from":        from,
			"sender_name": senderName,
			"body":        body,
			"cta":         cta,
		},
		"template": map[string]interface{}{
			"name":              strings.TrimSpace(in.TemplateName),
			"variables":         in.Variables,
			"has_placeholders":  len(in.Variables) > 0,
			"channel":           "cold_email",
			"is_template_draft": true,
		},
		"offer": map[string]interface{}{
			"product":       product,
			"sales_stage":   "cold_outreach",
			"price_mentioned": strings.Contains(strings.ToLower(body+" "+subject), "$") ||
				strings.Contains(strings.ToLower(body), "€") ||
				strings.Contains(strings.ToLower(body), "invoice") ||
				strings.Contains(strings.ToLower(body), "pricing"),
		},
		"evaluation_policy": map[string]interface{}{
			"goal": "Maximize relevant, credible, and appropriate cold outreach templates; do not simply reward persuasive writing.",
			"rules": []string{
				"This is a reusable email template — {{placeholders}} are intentional, not missing data.",
				"Evaluate the offer framing and requested commitment, not just wording polish.",
				"Never treat a cold prospect as ready to pay merely because the problem sounds relevant.",
				"Distinguish a clear CTA from a commercially appropriate CTA for cold outreach.",
				"Do not invent company-specific problems; flag fake or unsupported personalization claims written as facts.",
				"Flag inconsistent sender identities, unsupported claims, and misleading personalization copy.",
				"Treat missing critical commercial context as uncertainty (manual_review), not as proof the email is good.",
				"Prefer revise when issues are fixable; reserve do_not_send for deceptive or fundamentally inappropriate copy.",
			},
		},
	}
}

func TemplateReviewQuestions() map[string]interface{} {
	return map[string]interface{}{
		"decision": map[string]interface{}{
			"type": "choice",
			"instructions": `Choose the single best action for this cold email template.
Evaluate relevance framing, credibility, personalization placeholders,
value proposition, CTA, and sales stage together.

A clearly worded CTA does not automatically make a template appropriate to send.
For cold outreach, requesting payment or an invoice before purchase agreement
is a material issue. Prefer revise when the problem is fixable.
Placeholders like {{name}} or {{company}} are expected in templates.`,
			"criteria": map[string]string{
				"send":          "The template is credible, appropriately staged, and ready to use. No material issues remain.",
				"revise":        "The template has potential but contains fixable issues involving relevance framing, personalization, value proposition, CTA, trust, or requested commitment.",
				"do_not_send":   "The offer or email is deceptive, materially misleading, or fundamentally inappropriate.",
				"manual_review": "Essential commercial context is missing or the arrangement cannot be evaluated reliably.",
			},
		},
		"placeholders_ok": map[string]interface{}{
			"type": "noul",
			"instructions": `Are template placeholders used in a natural way (e.g. {{name}}, {{company}})
without looking broken, doubled, or like unfinished draft notes? Empty placeholders
in a template draft are acceptable; nonsense tokens are not.`,
		},
		"personalization_ok": map[string]interface{}{
			"type": "noul",
			"instructions": `Does the template set up accurate, evidence-friendly personalization
(via placeholders or clearly marked optional lines) rather than fabricating
specific facts about a prospect as if verified?`,
		},
		"offer_relevant": map[string]interface{}{
			"type": "noul",
			"instructions": `Is the offer framed so it could plausibly be relevant to a typical
recipient of this template, without claiming verified company-specific pain?`,
		},
		"value_proposition_clear": map[string]interface{}{
			"type": "noul",
			"instructions": `Does the email explain what the offer does and why it may be valuable,
without relying only on vague benefits or unsupported promises?`,
		},
		"cta_clear": map[string]interface{}{
			"type": "noul",
			"instructions": `Is the requested next action understandable and easy to respond to?
Evaluate clarity separately from whether the action itself is appropriate.`,
		},
		"commitment_appropriate": map[string]interface{}{
			"type": "noul",
			"instructions": `Is the requested commitment proportionate to cold outreach?
A cold prospect who has not agreed to buy should not be asked to pay an invoice
or make a large financial commitment.`,
		},
		"price_framing_appropriate": map[string]interface{}{
			"type": "noul",
			"instructions": `If pricing is discussed, is it appropriate for cold outreach context?
If pricing is not mentioned, answer yes unless the copy implies a premature payment ask.`,
		},
		"sales_sequence_appropriate": map[string]interface{}{
			"type": "noul",
			"instructions": `Is this an appropriate cold-outreach step (permission, interest check,
or light next step) rather than jumping to purchase or payment?`,
		},
		"credible": map[string]interface{}{
			"type": "noul",
			"instructions": `Are claims specific, credible, and appropriately qualified?
Flag unsupported guarantees or assumptions presented as hard facts.`,
		},
		"sender_consistent": map[string]interface{}{
			"type": "noul",
			"instructions": `Is the sender identity consistent across From address, body, and signature?
Flag obvious mismatches that would undermine trust. Missing signature alone is not fatal.`,
		},
		"ai_slop": map[string]interface{}{
			"type": "noul",
			"instructions": `Does the email sound generic, formulaic, overly polished, repetitive,
or unnatural? Judge the writing itself — yes means it feels like AI slop.`,
		},
		"primary_issue": map[string]interface{}{
			"type": "choice",
			"instructions": `What is the single most important reason this template should not be sent as written?
Select the most material issue. Placeholders alone are not an issue.`,
			"criteria": map[string]string{
				"none":                   "No material issue; the template is ready to send.",
				"weak_subject":           "The subject is misleading, vague, or unlikely to attract relevant attention.",
				"weak_personalization":   "Personalization is fabricated, generic, or poorly structured for real contact data.",
				"weak_value_proposition": "The benefit or business value is unclear, weak, or unsupported.",
				"offer_mismatch":         "The offer framing does not fit a plausible cold recipient.",
				"premature_price":        "Price or payment is introduced without adequate cold-outreach context.",
				"premature_cta":          "The CTA asks for a commitment inappropriate for cold outreach.",
				"credibility_issue":      "Questionable claims, trust issues, or inconsistent sender identity.",
				"unnatural_writing":      "The writing sounds generic, awkward, or unnatural.",
				"missing_information":    "Essential information is missing to make a reliable judgment.",
			},
		},
		"problem_area": map[string]interface{}{
			"type": "choice",
			"instructions": `Which part of the email most needs changing to resolve the primary issue?`,
			"criteria": map[string]string{
				"subject":           "The subject line.",
				"opening":           "The greeting or opening sentence.",
				"personalization":   "The prospect-specific details or assumptions.",
				"value_proposition": "The explanation of the offer and its benefits.",
				"price":             "The stated price, payment terms, or price framing.",
				"cta":               "The requested next step or call to action.",
				"signature":         "The sender name or signature.",
				"offer":             "The underlying offer itself.",
				"multiple_parts":    "Several parts need changes.",
				"none":              "No change is necessary.",
			},
		},
		"recommended_fix": map[string]interface{}{
			"type": "choice",
			"instructions": `What type of change would best address the primary issue?`,
			"criteria": map[string]string{
				"rewrite_subject":         "Rewrite the subject to be more specific and relevant.",
				"improve_personalization": "Use accurate, evidence-based personalization structure.",
				"clarify_value":           "Explain the concrete benefit and how the offer works.",
				"justify_price":           "Provide appropriate context for any price mention — or remove it.",
				"lower_commitment":        "Replace a premature purchase/payment ask with a lower-friction next step.",
				"improve_credibility":     "Resolve unsupported claims or inconsistent identity details.",
				"rewrite_copy":            "Rewrite awkward or generic language.",
				"change_offer":            "Reconsider the offer framing itself.",
				"gather_information":      "Obtain missing information before deciding.",
				"no_change":               "No material correction is needed.",
			},
		},
	}
}

func extractCTA(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	matches := reQuestionSentence.FindAllString(body, -1)
	if len(matches) > 0 {
		return strings.TrimSpace(matches[len(matches)-1])
	}
	lines := strings.Split(body, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.EqualFold(line, "best,") || strings.EqualFold(line, "thanks,") || strings.EqualFold(line, "regards,") {
			continue
		}
		if len(line) > 12 {
			return line
		}
	}
	return ""
}

var noulCheckMeta = []struct {
	ID    string
	Label string
}{
	{"placeholders_ok", "Placeholders look natural"},
	{"personalization_ok", "Personalization is honest"},
	{"offer_relevant", "Offer framing is relevant"},
	{"value_proposition_clear", "Value proposition is clear"},
	{"cta_clear", "CTA is clear"},
	{"commitment_appropriate", "Commitment fits cold outreach"},
	{"price_framing_appropriate", "Price framing is appropriate"},
	{"sales_sequence_appropriate", "Sales step is appropriate"},
	{"credible", "Claims are credible"},
	{"sender_consistent", "Sender identity is consistent"},
	{"ai_slop", "Sounds like AI slop"}, // high yes = bad
}

// ReviewTemplate runs Jev on a template draft and shapes the UI payload.
func ReviewTemplate(ctx context.Context, client *JevClient, in TemplateReviewInput) (*TemplateReviewResult, error) {
	if client == nil {
		client = NewJevClient()
	}
	state := BuildTemplateReviewState(in)
	questions := TemplateReviewQuestions()
	resp, err := client.Evaluate(ctx, state, questions)
	if err != nil {
		return nil, err
	}
	out := &TemplateReviewResult{
		Model: resp.Model,
		Usage: map[string]int{
			"input_tokens":  resp.Usage.InputTokens,
			"output_tokens": resp.Usage.OutputTokens,
		},
	}
	if raw, ok := resp.Answers["decision"]; ok {
		if c, err := DecodeJevChoice(raw); err == nil {
			out.Decision = TemplateReviewChoice{
				ID: "decision", Label: "Decision", Choice: c.Choice,
				Confidence: c.Confidence, Probabilities: c.Probabilities,
			}
		}
	}
	for _, meta := range noulCheckMeta {
		raw, ok := resp.Answers[meta.ID]
		if !ok {
			continue
		}
		n, err := DecodeJevNoul(raw)
		if err != nil {
			continue
		}
		out.Checks = append(out.Checks, TemplateReviewCheck{ID: meta.ID, Label: meta.Label, Yes: n.Noul})
	}
	out.PrimaryIssue = decodeChoiceLabeled(resp.Answers, "primary_issue", "Primary issue")
	out.ProblemArea = decodeChoiceLabeled(resp.Answers, "problem_area", "Problem area")
	out.RecommendedFix = decodeChoiceLabeled(resp.Answers, "recommended_fix", "Recommended fix")
	if out.Decision.Choice == "" {
		return nil, fmt.Errorf("jev returned no decision")
	}
	return out, nil
}

func decodeChoiceLabeled(answers map[string]json.RawMessage, id, label string) TemplateReviewChoice {
	raw, ok := answers[id]
	if !ok {
		return TemplateReviewChoice{ID: id, Label: label}
	}
	c, err := DecodeJevChoice(raw)
	if err != nil {
		return TemplateReviewChoice{ID: id, Label: label}
	}
	return TemplateReviewChoice{
		ID: id, Label: label, Choice: c.Choice,
		Confidence: c.Confidence, Probabilities: c.Probabilities,
	}
}
