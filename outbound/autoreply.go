package outbound

import (
	"regexp"
	"strings"
)

var (
	reAutoSubmitted = regexp.MustCompile(`(?im)^Auto-Submitted:\s*(.+)$`)
	rePrecedence    = regexp.MustCompile(`(?im)^Precedence:\s*(.+)$`)
	reXAutoReply    = regexp.MustCompile(`(?im)^X-(?:Autoreply|Auto-Response|Autorespond):\s*(.+)$`)
)

// IsAutoReplyMessage detects vacation / helpdesk / ticket auto-replies that should
// not count as human engagement.
func IsAutoReplyMessage(from, subject, body string) bool {
	headerBlob := body
	if i := strings.Index(body, "\r\n\r\n"); i > 0 && i < 8000 {
		headerBlob = body[:i]
	} else if i := strings.Index(body, "\n\n"); i > 0 && i < 8000 {
		headerBlob = body[:i]
	}
	combinedHeaders := from + "\n" + subject + "\n" + headerBlob

	if m := reAutoSubmitted.FindStringSubmatch(combinedHeaders); len(m) > 1 {
		v := strings.ToLower(strings.TrimSpace(m[1]))
		if v != "" && v != "no" {
			return true
		}
	}
	if m := rePrecedence.FindStringSubmatch(combinedHeaders); len(m) > 1 {
		v := strings.ToLower(strings.TrimSpace(m[1]))
		if v == "bulk" || v == "junk" || v == "list" || v == "auto_reply" || v == "autoreply" {
			return true
		}
	}
	if m := reXAutoReply.FindStringSubmatch(combinedHeaders); len(m) > 1 {
		v := strings.ToLower(strings.TrimSpace(m[1]))
		if v != "" && v != "no" {
			return true
		}
	}

	subj := strings.ToLower(strings.TrimSpace(subject))
	subjHints := []string{
		"out of office",
		"out-of-office",
		"automatic reply",
		"auto reply",
		"auto-reply",
		"autoreply",
		"auto response",
		"auto-response",
		"abwesenheit",
		"absence du bureau",
	}
	for _, h := range subjHints {
		if strings.Contains(subj, h) {
			return true
		}
	}

	lowerBody := strings.ToLower(body)
	bodyHints := []string{
		"this is an automatic reply",
		"this is an automated reply",
		"automated response",
		"auto-generated",
		"our team will get back to you",
		"thanks for contacting us",
		"thank you for contacting us",
		"we have received your message",
		"we'll get back to you as soon as",
		"we will get back to you as soon as",
	}
	for _, h := range bodyHints {
		if strings.Contains(lowerBody, h) {
			return true
		}
	}
	return false
}
