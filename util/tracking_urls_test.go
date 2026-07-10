package util

import "testing"

func TestNormalizeTrackingBaseURL(t *testing.T) {
	tests := map[string]string{
		"https://app.jupsend.com/":        "https://app.jupsend.com",
		"https://app.jupsend.com/api/v1":  "https://app.jupsend.com",
		"https://app.jupsend.com/api/v1/": "https://app.jupsend.com",
	}
	for in, want := range tests {
		if got := NormalizeTrackingBaseURL(in); got != want {
			t.Fatalf("NormalizeTrackingBaseURL(%q) = %q want %q", in, got, want)
		}
	}
}

func TestShouldSkipLinkTracking(t *testing.T) {
	skip := []string{
		"#section",
		"mailto:hi@example.com",
		"https://app.jupsend.com/api/v1/track/click/abc",
		"/u/token",
	}
	for _, href := range skip {
		if !shouldSkipLinkTracking(href) {
			t.Fatalf("expected skip for %q", href)
		}
	}
	keep := []string{
		"https://example.com/page",
		"http://client.io/demo",
	}
	for _, href := range keep {
		if shouldSkipLinkTracking(href) {
			t.Fatalf("expected track for %q", href)
		}
	}
}

func TestSafeRedirectURL(t *testing.T) {
	ok := map[string]string{
		"https://example.com/path": "https://example.com/path",
		"http://client.io":         "http://client.io",
	}
	for in, want := range ok {
		got, valid := SafeRedirectURL(in)
		if !valid || got != want {
			t.Fatalf("SafeRedirectURL(%q) = %q %v want %q true", in, got, valid, want)
		}
	}
	reject := []string{
		"/settings",
		"example.com",
		"javascript:alert(1)",
		"",
	}
	for _, in := range reject {
		if _, valid := SafeRedirectURL(in); valid {
			t.Fatalf("expected reject for %q", in)
		}
	}
}

func TestTrackClickURL(t *testing.T) {
	got := TrackClickURL("https://app.jupsend.com/api/v1", "abc-123")
	want := "https://app.jupsend.com/api/v1/track/click/abc-123"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
