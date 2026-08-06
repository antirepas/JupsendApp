package util

import (
	"regexp"
	"strings"
)

// Tracking artifacts embedded in stored HTML must never fire when staff view
// a send/conversation preview in the app.
var (
	trackingPixelImgRe = regexp.MustCompile(`(?is)<img\b[^>]*\bsrc\s*=\s*["'][^"']*/track/open/[^"']*["'][^>]*/?>`)
	trackingPixelBgRe  = regexp.MustCompile(`(?is)background-image\s*:\s*url\(\s*['"]?[^)'"]*/track/open/[^)'"]*['"]?\s*\)\s*;?`)
	// Hidden wrapper divs that only exist to host the pixel (InjectTrackingPixel).
	trackingPixelDivRe = regexp.MustCompile(`(?is)<div[^>]*(?:aria-hidden\s*=\s*["']true["']|max-height\s*:\s*0|display\s*:\s*none)[^>]*>\s*(?:<img\b[^>]*\bsrc\s*=\s*["'][^"']*/track/open/[^"']*["'][^>]*/?>)?\s*</div>`)
	// Click-tracking hrefs — neutralized so preview clicks do not record engagement.
	trackingClickHrefRe = regexp.MustCompile(`(?is)(\bhref\s*=\s*)(["'])([^"']*/track/click/[^"']*)(["'])`)
)

// StripTrackingForDisplay removes open-tracking pixels and neutralizes click-tracking
// links so in-app previews do not record false opens/clicks.
func StripTrackingForDisplay(html string) string {
	if html == "" {
		return html
	}
	lower := strings.ToLower(html)
	out := html
	if strings.Contains(lower, "/track/open/") {
		out = trackingPixelDivRe.ReplaceAllString(out, "")
		out = trackingPixelImgRe.ReplaceAllString(out, "")
		out = trackingPixelBgRe.ReplaceAllString(out, "")
	}
	if strings.Contains(strings.ToLower(out), "/track/click/") {
		out = trackingClickHrefRe.ReplaceAllString(out, `${1}${2}#${4}`)
	}
	return out
}

// StripOpenTrackingForDisplay removes open-tracking pixels from HTML.
// Prefer StripTrackingForDisplay for previews (opens + clicks).
func StripOpenTrackingForDisplay(html string) string {
	return StripTrackingForDisplay(html)
}
