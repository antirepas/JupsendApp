package util

import (
	"regexp"
	"strings"
)

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

func StripHTML(html string) string {
	text := htmlTagRe.ReplaceAllString(html, "")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	return strings.TrimSpace(text)
}
