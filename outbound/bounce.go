package outbound

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	reFailedRecipients = regexp.MustCompile(`(?i)X-Failed-Recipients:\s*([^\s\r\n]+)`)
	reSendIDHeader       = regexp.MustCompile(`(?i)X-EmailTracker-Send-ID:\s*(\d+)`)
	reSendIDBody         = regexp.MustCompile(`(?i)emailtracker-send-id[:\s]+(\d+)`)
	reMailerDaemon       = regexp.MustCompile(`(?i)(mailer-daemon|postmaster|mail delivery subsystem)`)
)

func IsBounceMessage(from, subject, body string) bool {
	combined := from + " " + subject
	if reMailerDaemon.MatchString(combined) {
		return true
	}
	lower := strings.ToLower(subject)
	if strings.Contains(lower, "delivery status notification") ||
		strings.Contains(lower, "undelivered") ||
		strings.Contains(lower, "delivery failure") ||
		strings.Contains(lower, "mail delivery failed") {
		return true
	}
	if reFailedRecipients.MatchString(body) {
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
	if m := reFailedRecipients.FindStringSubmatch(body); len(m) > 1 {
		return strings.Trim(m[1], "<>")
	}
	return ""
}
