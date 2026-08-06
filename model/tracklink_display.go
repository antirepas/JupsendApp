package model

import (
	"html"
	"regexp"
	"strings"
)

var trackedClickHrefRe = regexp.MustCompile(`(?is)(\bhref\s*=\s*)(["'])([^"']*/track/click/)([^"'?#/]+)([^"']*)(["'])`)

// lookupOriginalURL resolves a tracked click id to its destination.
// Overridable in tests.
var lookupOriginalURL = GetOriginalURL

// RewriteTrackedClicksForDisplay replaces click-tracking URLs with the original
// destination so in-app previews show real links without hitting /track/click.
// Unknown tracking ids become href="#".
func RewriteTrackedClicksForDisplay(body string) string {
	if body == "" || !strings.Contains(strings.ToLower(body), "/track/click/") {
		return body
	}
	return trackedClickHrefRe.ReplaceAllStringFunc(body, func(match string) string {
		sub := trackedClickHrefRe.FindStringSubmatch(match)
		if len(sub) < 7 {
			return `href="#"`
		}
		prefix, q1, id, q2 := sub[1], sub[2], sub[4], sub[6]
		orig, err := lookupOriginalURL(strings.TrimSpace(id))
		if err != nil || strings.TrimSpace(orig) == "" {
			return prefix + q1 + "#" + q2
		}
		return prefix + q1 + html.EscapeString(orig) + q2
	})
}
