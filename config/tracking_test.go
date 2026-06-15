package config

import "testing"

func TestTrackingWarningNgrokFree(t *testing.T) {
	w := TrackingWarning("https://abc.ngrok-free.app")
	if w == "" {
		t.Fatal("expected ngrok free warning")
	}
}

func TestTrackingWarningLocalhost(t *testing.T) {
	w := TrackingWarning("http://localhost:8078")
	if w == "" {
		t.Fatal("expected localhost warning")
	}
}

func TestTrackingWarningCustomDomain(t *testing.T) {
	w := TrackingWarning("https://track.example.com")
	if w != "" {
		t.Fatalf("expected no warning, got %q", w)
	}
}
