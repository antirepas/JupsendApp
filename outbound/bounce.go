package outbound

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	reFailedRecipients  = regexp.MustCompile(`(?i)X-Failed-Recipients:\s*([^\s\r\n]+)`)
	reFinalRecipient    = regexp.MustCompile(`(?i)Final-Recipient:\s*(?:rfc822;\s*)?([^\s\r\n]+)`)
	reOriginalRecipient = regexp.MustCompile(`(?i)Original-Recipient:\s*(?:rfc822;\s*)?([^\s\r\n]+)`)
	reSendIDHeader      = regexp.MustCompile(`(?i)X-EmailTracker-Send-ID:\s*(\d+)`)
	reSendIDBody        = regexp.MustCompile(`(?i)emailtracker-send-id[:\s]+(\d+)`)
	reMailerDaemon      = regexp.MustCompile(`(?i)(mailer-daemon|postmaster|mail delivery subsystem)`)
	reBounceRecipient   = regexp.MustCompile(`(?i)(?:wasn't delivered to|was not delivered to|could not be delivered to|unable to deliver(?:\s+your)?\s+message to|delivery to the following recipient|the following address|recipient address)\s*:?\s*<?([a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,})>?`)
	reAnyEmail          = regexp.MustCompile(`(?i)\b([a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,})\b`)
)

func IsBounceMessage(from, subject, body string) bool {
	combined := from + " " + subject
	if reMailerDaemon.MatchString(combined) {
		return true
	}
	lower := strings.ToLower(subject + " " + body)
	phrases := []string{
		"delivery status notification",
		"undelivered",
		"undeliverable",
		"delivery failure",
		"mail delivery failed",
		"returned mail",
		"permanent error",
		"address not found",
		"does not exist",
		"no longer exists",
		"wasn't delivered",
		"was not delivered",
		"could not be delivered",
		"unable to receive mail",
		"mailbox unavailable",
		"user unknown",
		"unknown user",
		"no such user",
		"recipient rejected",
		"550 5.1.1",
		"5.1.1",
	}
	for _, p := range phrases {
		if strings.Contains(lower, p) {
			return true
		}
	}
	if reFailedRecipients.MatchString(body) || reFinalRecipient.MatchString(body) {
		return true
	}
	return false
}

func ExtractSendIDFromBounce(body string) int64 {
	if m := reSendIDHeader.FindStringSubmatch(body); len(m) > 1 {
		if id, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			return id
		}
	}
	if m := reSendIDBody.FindStringSubmatch(body); len(m) > 1 {
		if id, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			return id
		}
	}
	return 0
}

func ExtractFailedRecipient(body string) string {
	for _, re := range []*regexp.Regexp{reFailedRecipients, reFinalRecipient, reOriginalRecipient, reBounceRecipient} {
		if m := re.FindStringSubmatch(body); len(m) > 1 {
			return normalizeBounceEmail(m[1])
		}
	}
	// Fallback: first non-system email near bounce wording in the body.
	lower := strings.ToLower(body)
	if !(strings.Contains(lower, "address not found") ||
		strings.Contains(lower, "undeliverable") ||
		strings.Contains(lower, "wasn't delivered") ||
		strings.Contains(lower, "was not delivered") ||
		strings.Contains(lower, "does not exist") ||
		strings.Contains(lower, "no longer exists") ||
		strings.Contains(lower, "delivery status")) {
		return ""
	}
	for _, m := range reAnyEmail.FindAllStringSubmatch(body, -1) {
		if len(m) < 2 {
			continue
		}
		email := normalizeBounceEmail(m[1])
		if email == "" || isSystemBounceAddress(email) {
			continue
		}
		return email
	}
	return ""
}

func normalizeBounceEmail(email string) string {
	email = strings.TrimSpace(email)
	email = strings.Trim(email, "<>")
	email = strings.Trim(email, `"'`)
	return strings.ToLower(strings.TrimSpace(email))
}

func isSystemBounceAddress(email string) bool {
	email = strings.ToLower(email)
	local, domain, ok := strings.Cut(email, "@")
	if !ok {
		return true
	}
	if local == "mailer-daemon" || local == "postmaster" || local == "mail-delivery-subsystem" {
		return true
	}
	if domain == "google.com" && (local == "mailer-daemon" || strings.Contains(local, "mailer")) {
		return true
	}
	return false
}
