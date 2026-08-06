package util

import (
	"strings"
	"testing"
)

func TestStripOpenTrackingForDisplay(t *testing.T) {
	in := `<p>Hi</p>` +
		`<img src="https://app.example.com/api/v1/track/open/abc" width="1" height="1" alt="" />` +
		`<div aria-hidden="true" style="width:1px;height:1px;max-height:0;overflow:hidden;line-height:1px;background-image:url('https://app.example.com/api/v1/track/open/abc');"></div>` +
		`<div style="display:none;max-height:0;overflow:hidden;"><img src="https://app.example.com/api/v1/track/open/abc" width="1" height="1" alt="" /></div>` +
		`<img src="https://cdn.example.com/logo.png" alt="logo">`
	out := StripOpenTrackingForDisplay(in)
	if strings.Contains(strings.ToLower(out), "/track/open/") {
		t.Fatalf("tracking pixel remained: %q", out)
	}
	if !strings.Contains(out, "logo.png") {
		t.Fatalf("real image stripped: %q", out)
	}
	if !strings.Contains(out, "<p>Hi</p>") {
		t.Fatalf("content lost: %q", out)
	}
}

func TestSanitizeHTMLForDisplayStripsTrackingPixel(t *testing.T) {
	in := `<p>ok</p><img src="http://localhost:8080/api/v1/track/open/xyz">`
	out := SanitizeHTMLForDisplay(in)
	if strings.Contains(out, "track/open") {
		t.Fatalf("pixel remained: %q", out)
	}
}

func TestStripTrackingForDisplayNeutralizesClicks(t *testing.T) {
	in := `<p><a href="https://app.example.com/api/v1/track/click/abc123">Go</a></p>` +
		`<a href="https://cdn.example.com/real">Keep</a>`
	out := StripTrackingForDisplay(in)
	if strings.Contains(strings.ToLower(out), "/track/click/") {
		t.Fatalf("click tracking remained: %q", out)
	}
	if !strings.Contains(out, `href="#"`) {
		t.Fatalf("expected neutralized href: %q", out)
	}
	if !strings.Contains(out, "cdn.example.com/real") {
		t.Fatalf("real link stripped: %q", out)
	}
}

func TestSanitizeHTMLForDisplayStripsClickTracking(t *testing.T) {
	in := `<a href="http://localhost:8080/api/v1/track/click/xyz">Click</a>`
	out := SanitizeHTMLForDisplay(in)
	if strings.Contains(out, "track/click") {
		t.Fatalf("click tracking remained: %q", out)
	}
}
