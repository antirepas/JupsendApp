package outbound

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	reFailedRecipients = regexp.MustCompile(`(?i)X-Failed-Recipients:\s*([^\s\r\n]+)`)
	reFinalRecipient   = regexp.MustCompile(`(?i)Final-Recipient:\s*(?:rfc822;\s*)?([^\s\r\n]+)`)
	reOriginalRecipient = regexp.MustCompile(`(?i)Original-Recipient:\s*(?:rfc822;\s*)?([^\s\r\n]+)`)
	reSendIDHeader     = regexp.MustCompile(`(?i)X-EmailTracker-Send-ID:\s*(\d+)`)
	reSendIDBody       = regexp.MustCompile(`(?i)emailtracker-send-id[:\s]+(\d+)`)
	reMailerDaemon     = regexp.MustCompile(`(?i)(mailer-daemon|postmaster|mail delivery subsystem)`)
)

func IsBounceMessage(from, subject, body string) bool {
	combined := from + " " + subject
	if reMailerDaemon.MatchString(combined) {
		return true
	}
	lower := strings.ToLower(subject + " " + body)
	if strings.Contains(lower, "delivery status notification") ||
		strings.Contains(lower, "undelivered") ||
		strings.Contains(lower, "undeliverable") ||
		strings.Contains(lower, "delivery failure") ||
		strings.Contains(lower, "mail delivery failed") ||
		strings.Contains(lower, "returned mail") ||
		strings.Contains(lower, "permanent error") ||
		strings.Contains(lower, "address not found") ||
		strings.Contains(lower, "does not exist") {
		return true
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
	for _, re := range []*regexp.Regexp{reFailedRecipients, reFinalRecipient, reOriginalRecipient} {
		if m := re.FindStringSubmatch(body); len(m) > 1 {
			return strings.Trim(strings.TrimSpace(m[1]), "<>")
		}
	}
	return ""
}
