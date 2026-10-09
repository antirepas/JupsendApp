package util

import (
	"fmt"
	"mime"
	"strings"
	"time"
	"unicode"
)

const mimeBoundary = "jupsend-boundary-123"

// encodeMIMEHeader encodes a header value with RFC 2047 when it contains
// non-ASCII (or CR/LF). Plain ASCII subjects stay untouched for readability.
func encodeMIMEHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	for _, r := range s {
		if r > unicode.MaxASCII {
			return mime.QEncoding.Encode("utf-8", s)
		}
	}
	return s
}

func formatFromHeader(email, fromName string) string {
	email = strings.TrimSpace(email)
	fromName = strings.TrimSpace(fromName)
	if fromName == "" {
		return email
	}
	encoded := encodeMIMEHeader(fromName)
	if encoded != fromName {
		return fmt.Sprintf("%s <%s>", encoded, email)
	}
	if strings.ContainsAny(fromName, `<>@"\,`) || strings.Contains(fromName, " ") {
		return fmt.Sprintf(`"%s" <%s>`, strings.ReplaceAll(fromName, `"`, `\"`), email)
	}
	return fmt.Sprintf("%s <%s>", fromName, email)
}

// BuildMultipartEmail builds a multipart/alternative RFC 2822 message.
func BuildMultipartEmail(from, fromName, to, subject, plainBody, htmlBody string, meta SendMeta) []byte {
	fromHeader := formatFromHeader(from, fromName)
	subjectHeader := encodeMIMEHeader(subject)

	headers := "From: " + fromHeader + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subjectHeader + "\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"" + mimeBoundary + "\"\r\n"

	if meta.MessageID != "" {
		headers += "Message-ID: " + meta.MessageID + "\r\n"
	}
	if meta.InReplyTo != "" {
		headers += "In-Reply-To: " + meta.InReplyTo + "\r\n"
	}
	if meta.References != "" {
		headers += "References: " + meta.References + "\r\n"
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
