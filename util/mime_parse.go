package util

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
)

// ParsedMIME holds decoded text parts from a raw RFC822 message.
type ParsedMIME struct {
	Text string
	HTML string
}

// ParseMIMEBody extracts text/plain and text/html from a raw email (headers optional).
func ParseMIMEBody(raw string) ParsedMIME {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ParsedMIME{}
	}
	head := strings.ToLower(raw[:min(800, len(raw))])
	hasMIMEHeaders := strings.Contains(head, "content-type:") ||
		strings.HasPrefix(head, "mime-version:") ||
		strings.Contains(head, "\nmime-version:") ||
		strings.Contains(head, "delivered-to:") ||
		strings.Contains(head, "return-path:") ||
		strings.HasPrefix(head, "received:") ||
		strings.Contains(head, "\nreceived:")
	if !hasMIMEHeaders {
		if strings.Contains(head, "<html") || strings.Contains(raw, "<div") {
			return ParsedMIME{HTML: raw, Text: StripHTML(raw)}
		}
		return ParsedMIME{Text: raw}
	}
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		// Body-only multipart or plain.
		if strings.Contains(strings.ToLower(raw), "content-type:") {
			return parseMIMEEntity(raw, "", "")
		}
		return ParsedMIME{Text: raw}
	}
	ct := msg.Header.Get("Content-Type")
	cte := msg.Header.Get("Content-Transfer-Encoding")
	body, _ := io.ReadAll(msg.Body)
	return parseMIMEEntity(string(body), ct, cte)
}

func parseMIMEEntity(body, contentType, transferEncoding string) ParsedMIME {
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ParsedMIME{Text: decodeTransfer(body, transferEncoding)}
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return ParsedMIME{Text: body}
		}
		return parseMultipart(body, boundary)
	}
	decoded := decodeTransfer(body, transferEncoding)
	switch {
	case mediaType == "text/html":
		return ParsedMIME{HTML: decoded, Text: StripHTML(decoded)}
	default:
		return ParsedMIME{Text: decoded}
	}
}

func parseMultipart(body, boundary string) ParsedMIME {
	var out ParsedMIME
	r := multipart.NewReader(strings.NewReader(body), boundary)
	for {
		part, err := r.NextPart()
		if err != nil {
			break
		}
		ct := part.Header.Get("Content-Type")
		mediaType, _, _ := mime.ParseMediaType(ct)
		raw, _ := io.ReadAll(part)
		decoded := decodeTransfer(string(raw), part.Header.Get("Content-Transfer-Encoding"))
		switch {
		case mediaType == "text/plain" && out.Text == "":
			out.Text = decoded
		case mediaType == "text/html" && out.HTML == "":
			out.HTML = decoded
		case strings.HasPrefix(mediaType, "multipart/"):
			_, params, _ := mime.ParseMediaType(ct)
			if b := params["boundary"]; b != "" {
				nested := parseMultipart(decoded, b)
				if out.Text == "" {
					out.Text = nested.Text
				}
				if out.HTML == "" {
					out.HTML = nested.HTML
				}
			}
		}
		_ = part.Close()
	}
	if out.Text == "" && out.HTML != "" {
		out.Text = StripHTML(out.HTML)
	}
	return out
}

func decodeTransfer(body, encoding string) string {
	encoding = strings.ToLower(strings.TrimSpace(encoding))
	switch encoding {
	case "base64":
		cleaned := strings.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, body)
		b, err := base64.StdEncoding.DecodeString(cleaned)
		if err != nil {
			return body
		}
		return string(b)
	case "quoted-printable":
		b, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader([]byte(body))))
		if err != nil {
			return body
		}
		return string(b)
	default:
		return body
	}
}

// SanitizeHTMLForDisplay strips scripts, event handlers, and tracking pixels/links
// for safe in-app embedding (so previews do not fire false opens).
func SanitizeHTMLForDisplay(html string) string {
	if html == "" {
		return ""
	}
	html = StripTrackingForDisplay(html)
	lower := strings.ToLower(html)
	// Remove script/style blocks (best-effort).
	for {
		start := strings.Index(lower, "<script")
		if start < 0 {
			break
		}
		end := strings.Index(lower[start:], "</script>")
		if end < 0 {
			html = html[:start]
			break
		}
		end = start + end + len("</script>")
		html = html[:start] + html[end:]
		lower = strings.ToLower(html)
	}
	for {
		start := strings.Index(lower, "<style")
		if start < 0 {
			break
		}
		end := strings.Index(lower[start:], "</style>")
		if end < 0 {
			html = html[:start]
			break
		}
		end = start + end + len("</style>")
		html = html[:start] + html[end:]
		lower = strings.ToLower(html)
	}
	html = strings.ReplaceAll(html, "javascript:", "")
	return html
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// LooksLikeMangledEmailBody detects raw MIME / undecoded quoted-printable stored as body text.
func LooksLikeMangledEmailBody(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	lower := strings.ToLower(s[:min(400, len(s))])
	if strings.Contains(lower, "content-transfer-encoding:") ||
		strings.Contains(lower, "delivered-to:") ||
		strings.Contains(lower, "return-path:") ||
		strings.HasPrefix(lower, "received:") {
		return true
	}
	// Common QP artifacts when CTE was not decoded.
	if strings.Contains(s, "=20") || strings.Contains(s, "=\r\n") || strings.Contains(s, "=\n") {
		return true
	}
	return false
}

// RepairEmailBody re-parses a mangled stored body into clean text/html when possible.
func RepairEmailBody(bodyText, bodyHTML string) (text, html string) {
	raw := bodyHTML
	if LooksLikeMangledEmailBody(bodyText) && (raw == "" || !LooksLikeMangledEmailBody(raw) || len(bodyText) > len(raw)) {
		raw = bodyText
	}
	if raw == "" {
		raw = bodyText
	}
	if !LooksLikeMangledEmailBody(raw) && !LooksLikeMangledEmailBody(bodyHTML) {
		return bodyText, bodyHTML
	}
	// Prefer the more header-like blob.
	candidate := raw
	if LooksLikeMangledEmailBody(bodyHTML) && len(bodyHTML) >= len(candidate) {
		candidate = bodyHTML
	}
	if LooksLikeMangledEmailBody(bodyText) && strings.Contains(strings.ToLower(bodyText), "content-type:") {
		candidate = bodyText
	}
	parsed := ParseMIMEBody(candidate)
	if parsed.HTML == "" && parsed.Text == "" {
		// Last resort: decode QP on the blob as-is.
		decoded := decodeTransfer(candidate, "quoted-printable")
		if decoded != candidate {
			if strings.Contains(strings.ToLower(decoded), "<html") || strings.Contains(decoded, "<div") || strings.Contains(decoded, "<p") {
				return StripHTML(decoded), decoded
			}
			return decoded, ""
		}
		return bodyText, bodyHTML
	}
	text, html = parsed.Text, parsed.HTML
	if text == "" && html != "" {
		text = StripHTML(html)
	}
	return text, html
}
