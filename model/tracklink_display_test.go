package model

import (
	"errors"
	"strings"
	"testing"
)

func TestRewriteTrackedClicksForDisplay(t *testing.T) {
	prev := lookupOriginalURL
	t.Cleanup(func() { lookupOriginalURL = prev })
	lookupOriginalURL = func(id string) (string, error) {
		if id == "abc123" {
			return "https://example.com/offer?x=1&y=2", nil
		}
		return "", errors.New("not found")
	}

	in := `<p><a href="https://app.example.com/api/v1/track/click/abc123">Offer</a></p>` +
		`<a href="https://app.example.com/api/v1/track/click/missing">Gone</a>` +
		`<a href="https://cdn.example.com/ok">Keep</a>`
	out := RewriteTrackedClicksForDisplay(in)
	if strings.Contains(strings.ToLower(out), "/track/click/") {
		t.Fatalf("tracking url remained: %q", out)
	}
	if !strings.Contains(out, "https://example.com/offer?x=1&amp;y=2") {
		t.Fatalf("original url not restored: %q", out)
	}
	if !strings.Contains(out, `href="#"`) {
		t.Fatalf("unknown id not neutralized: %q", out)
	}
	if !strings.Contains(out, "cdn.example.com/ok") {
		t.Fatalf("real link lost: %q", out)
	}
}

func TestSanitizeHTMLForDisplayStripsClicks(t *testing.T) {
	prev := lookupOriginalURL
	t.Cleanup(func() { lookupOriginalURL = prev })
	lookupOriginalURL = func(string) (string, error) {
		return "", errors.New("not found")
	}
	in := `<a href="http://localhost/api/v1/track/click/xyz">x</a>` +
		`<img src="http://localhost/api/v1/track/open/xyz">`
	out := sanitizeHTMLForDisplay(in)
	if strings.Contains(out, "track/click") || strings.Contains(out, "track/open") {
		t.Fatalf("tracking remained: %q", out)
	}
}
