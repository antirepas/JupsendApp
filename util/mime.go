package util

import (
	"fmt"
	"time"
)

const mimeBoundary = "jupsend-boundary-123"

// BuildMultipartEmail builds a multipart/alternative RFC 2822 message.
func BuildMultipartEmail(from, fromName, to, subject, plainBody, htmlBody string, meta SendMeta) []byte {
	fromHeader := from
	if fromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", fromName, from)
	}

	headers := "From: " + fromHeader + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"" + mimeBoundary + "\"\r\n"

	if meta.MessageID != "" {
		headers += "Message-ID: " + meta.MessageID + "\r\n"
	}
	if meta.EmailTrackerSendID != "" {
		headers += "X-EmailTracker-Send-ID: " + meta.EmailTrackerSendID + "\r\n"
	}

	return []byte(headers +
		"\r\n" +
		"--" + mimeBoundary + "\r\n" +
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" +
		plainBody + "\r\n" +
		"--" + mimeBoundary + "\r\n" +
		"Content-Type: text/html; charset=\"UTF-8\"\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" +
		htmlBody + "\r\n" +
		"--" + mimeBoundary + "--\r\n",
	)
}
