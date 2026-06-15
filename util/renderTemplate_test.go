package util

import (
	"strings"
	"testing"
)

func TestInjectTrackingPixelAfterWrap(t *testing.T) {
	body := InjectTrackingPixel(WrapHTMLBody("<p>Hello</p>"), "track-123")
	lower := strings.ToLower(body)
	if !strings.Contains(body, "/api/v1/track/open/track-123") {
		t.Fatal("expected tracking pixel URL in body")
	}
	if !strings.Contains(lower, "<img") {
		t.Fatal("expected img tag")
	}
	idxBody := strings.LastIndex(lower, "</body>")
	idxImg := strings.Index(lower, "<img")
	if idxImg <= 0 || idxBody <= 0 || idxImg >= idxBody {
		t.Fatalf("pixel should appear before </body>, img@%d body@%d", idxImg, idxBody)
	}
}
