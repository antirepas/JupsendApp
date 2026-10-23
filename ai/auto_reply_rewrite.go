package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const maxAutoReplyRewriteAttempts = 1

var htmlTagRe = regexp.MustCompile(`(?is)<[^>]*>`)

func plainFromHTML(html string) string {
	s := htmlTagRe.ReplaceAllString(html, " ")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

// AutoReplyPrepareResult is verify (+ optional rewrite) outcome for watcher auto-replies.
type AutoReplyPrepareResult struct {
	Send      bool
	Subject   string
	BodyHTML  string
	Rewritten bool
	Attempts  int // rewrite attempts used
	Note      string
	Verify    ReplyVerifyResult
}

// PrepareAutoReply verifies a rendered template reply; if Jev skips, rewrites once with OpenAI and re-verifies.
func PrepareAutoReply(ctx context.Context, inboundSubject, inboundBody, proposedSubject, proposedBodyHTML, intent string) (AutoReplyPrepareResult, error) {
	plain := plainFromHTML(proposedBodyHTML)
	out := AutoReplyPrepareResult{
		Subject:  proposedSubject,
		BodyHTML: proposedBodyHTML,
	}

	if !JEVConfigured() {
		out.Send = true
		return out, nil
	}

	vr, err := VerifyAutoReply(ctx, inboundSubject, inboundBody, proposedSubject, plain, intent)
	if err != nil {
		return out, err
	}
	out.Verify = vr
	if vr.Decision == "send" {
		out.Send = true
		return out, nil
	}

	skipReason := vr.Reason
	if skipReason == "" {
		skipReason = FormatReplyVerifySkipReason(vr, intent)
	}

	for attempt := 1; attempt <= maxAutoReplyRewriteAttempts; attempt++ {
		out.Attempts = attempt
		rewrittenSubj, rewrittenHTML, rerr := RewriteAutoReply(ctx, inboundSubject, inboundBody, proposedSubject, proposedBodyHTML, intent, skipReason)
		if rerr != nil {
			out.Note = fmt.Sprintf("[Auto-reply skipped] %s Rewrite failed: %s", skipReason, rerr.Error())
			return out, nil
		}
		out.Rewritten = true
		out.Subject = rewrittenSubj
		out.BodyHTML = rewrittenHTML
		plain2 := plainFromHTML(rewrittenHTML)

		vr2, verr := VerifyAutoReply(ctx, inboundSubject, inboundBody, rewrittenSubj, plain2, intent)
		if verr != nil {
			out.Verify = vr
			out.Note = fmt.Sprintf("[Auto-reply skipped] %s Rewrote once, but re-verify failed: %s", skipReason, verr.Error())
			return out, nil
		}
		out.Verify = vr2
		if vr2.Decision == "send" {
			out.Send = true
			out.Note = fmt.Sprintf("[Auto-reply] Rewrote after Jev feedback, then approved. Original issue: %s", skipReason)
			return out, nil
		}
		skipReason = vr2.Reason
		if skipReason == "" {
			skipReason = FormatReplyVerifySkipReason(vr2, intent)
		}
	}

	out.Note = fmt.Sprintf("[Auto-reply skipped] %s Tried rewriting with OpenAI; Jev still would not send.", skipReason)
	return out, nil
}

type rewrittenAutoReply struct {
	Subject  string `json:"subject"`
	BodyHTML string `json:"body_html"`
	BodyText string `json:"body_text"`
}

// RewriteAutoReply uses OpenAI to adapt the template reply so it answers the inbound message.
func RewriteAutoReply(ctx context.Context, inboundSubject, inboundBody, proposedSubject, proposedBodyHTML, intent, skipReason string) (subject, bodyHTML string, err error) {
	system := `You rewrite cold-outreach auto-replies so they clearly answer the prospect's inbound message.
Return ONLY compact JSON: {"subject":"...","body_html":"..."}.
Rules:
- Directly acknowledge what they said (buying interest, question, objection, etc.).
- Keep it short (2–5 short paragraphs or a few <p> tags), professional, and human.
- Include a light, appropriate next step (e.g. offer times / ask one clarifying question).
- Do not invent pricing, product claims, or calendar links that were not in the draft.
- Prefer improving the draft; keep useful phrasing from it when it still fits.
- body_html must be simple HTML (<p>, <br>, <a> only). No markdown fences.`

	user := fmt.Sprintf(`Intent: %s

Inbound subject: %s
Inbound body:
%s

Draft subject: %s
Draft body (HTML):
%s

Why the draft was rejected:
%s

Rewrite the draft so it addresses the inbound message and would pass verification.`,
		strings.TrimSpace(intent),
		strings.TrimSpace(inboundSubject),
		trimRunes(inboundBody, 2500),
		strings.TrimSpace(proposedSubject),
		trimRunes(proposedBodyHTML, 2500),
		strings.TrimSpace(skipReason),
	)

	raw, err := CompleteTransform(ctx, system, user)
	if err != nil {
		return "", "", err
	}
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var parsed rewrittenAutoReply
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		// Best-effort: treat whole response as body if JSON fails.
		if strings.Contains(raw, "<") {
			return proposedSubject, raw, nil
		}
		return "", "", fmt.Errorf("rewrite parse: %w", err)
	}
	subj := strings.TrimSpace(parsed.Subject)
	if subj == "" {
		subj = strings.TrimSpace(proposedSubject)
	}
	body := strings.TrimSpace(parsed.BodyHTML)
	if body == "" && strings.TrimSpace(parsed.BodyText) != "" {
		body = "<p>" + strings.ReplaceAll(strings.TrimSpace(parsed.BodyText), "\n\n", "</p><p>") + "</p>"
	}
	if body == "" {
		return "", "", fmt.Errorf("rewrite returned empty body")
	}
	return subj, body, nil
}

func trimRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
