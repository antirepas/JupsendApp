package util

import (
	"html"
	"regexp"
	"strings"
)

var (
	htmlTagRe      = regexp.MustCompile(`<[^>]*>`)
	htmlBlockBreak = regexp.MustCompile(`(?i)</(p|div|h[1-6]|li|tr|blockquote|section|article)>`)
	htmlBR         = regexp.MustCompile(`(?i)<br\s*/?>`)
)

func StripHTML(s string) string {
	text := htmlTagRe.ReplaceAllString(s, "")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	return strings.TrimSpace(text)
}

// HTMLToPlainPreview turns email HTML into readable plain text for UI previews
// (paragraph breaks preserved; tags and entities removed).
func HTMLToPlainPreview(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = htmlBR.ReplaceAllString(s, "\n")
	s = htmlBlockBreak.ReplaceAllString(s, "\n")
	s = StripHTML(s)
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
