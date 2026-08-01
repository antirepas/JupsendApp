package util

import (
	"html"
	"regexp"
	"strings"
)

var (
	htmlChunkRe = regexp.MustCompile(`(?s)(<[^>]+>)|([^<]+)`)
	bareURLRe   = regexp.MustCompile(`(?i)\bhttps?://[^\s<>"']+`)
	hrefAttrRe  = regexp.MustCompile(`(?i)href\s*=\s*("([^"]*)"|'([^']*)')`)
)

// AutolinkBareURLs wraps plain http(s) URLs in text nodes with <a href> tags.
// Email clients otherwise auto-link those URLs and clicks bypass tracking rewrite.
func AutolinkBareURLs(htmlBody string) string {
	if htmlBody == "" || !bareURLRe.MatchString(htmlBody) {
		return htmlBody
	}
	return htmlChunkRe.ReplaceAllStringFunc(htmlBody, func(chunk string) string {
		if chunk == "" || chunk[0] == '<' {
			return chunk
		}
		return bareURLRe.ReplaceAllStringFunc(chunk, wrapBareURL)
	})
}

func wrapBareURL(raw string) string {
	url, trail := splitTrailingURLPunct(raw)
	if url == "" || shouldSkipLinkTracking(url) {
		return raw
	}
	if _, ok := SafeRedirectURL(url); !ok {
		return raw
	}
	return `<a href="` + html.EscapeString(url) + `">` + url + `</a>` + trail
}

func splitTrailingURLPunct(raw string) (url, trail string) {
	url = raw
	for len(url) > 0 {
		r := rune(url[len(url)-1])
		switch r {
		case '.', ',', ';', ':', '!', '?':
			trail = string(r) + trail
			url = url[:len(url)-1]
		case ')':
			if strings.Count(url, "(") >= strings.Count(url, ")") {
				return url, trail
			}
			trail = ")" + trail
			url = url[:len(url)-1]
		default:
			return url, trail
		}
	}
	return url, trail
}

// normalizeTrackableURL unescapes HTML entities and adds https:// for www. hosts.
func normalizeTrackableURL(raw string) (string, bool) {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	if raw == "" {
		return "", false
	}
	if !strings.Contains(raw, "://") && strings.HasPrefix(strings.ToLower(raw), "www.") {
		raw = "https://" + raw
	}
	return SafeRedirectURL(raw)
}
