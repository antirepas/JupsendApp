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
	// Ensure we can parse as a mail message even if Body alone was stored.
	if !strings.Contains(strings.ToLower(raw[:min(200, len(raw))]), "content-type:") &&
		!strings.HasPrefix(strings.ToLower(raw), "mime-version:") {
		if strings.Contains(strings.ToLower(raw), "<html") || strings.Contains(raw, "<div") {
			return ParsedMIME{HTML: raw, Text: StripHTML(raw)}
		}
		return ParsedMIME{Text: raw}
	}
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		// Body-only multipart or plain.
		if strings.Contains(strings.ToLower(raw), "content-type:") {
			return parseMIMEEntity(raw, "")
		}
		return ParsedMIME{Text: raw}
	}
	ct := msg.Header.Get("Content-Type")
	body, _ := io.ReadAll(msg.Body)
	return parseMIMEEntity(string(body), ct)
}

func parseMIMEEntity(body, contentType string) ParsedMIME {
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ParsedMIME{Text: body}
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return ParsedMIME{Text: body}
		}
		return parseMultipart(body, boundary)
	}
	decoded := decodeTransfer(body, "")
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

// SanitizeHTMLForDisplay strips scripts and event handlers for safe embedding.
func SanitizeHTMLForDisplay(html string) string {
	if html == "" {
		return ""
	}
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
