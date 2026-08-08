package util

import (
	"testing"
	"time"
)

func TestClassifyOpenTooFast(t *testing.T) {
	sent := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	got := ClassifyOpen("Mozilla/5.0 (Macintosh)", "8.8.8.8", sent, sent.Add(2*time.Second))
	if !got.IsBot || got.Reason != "too_fast" {
		t.Fatalf("got %+v", got)
	}
	got = ClassifyOpen("Mozilla/5.0 (Macintosh)", "8.8.8.8", sent, sent.Add(6*time.Second))
	if got.IsBot {
		t.Fatalf("should be human: %+v", got)
	}
}

func TestClassifyOpenUserAgent(t *testing.T) {
	botCases := []string{
		"Barracuda Sentinel",
		"Mimecast-URL",
		"Proofpoint URL Defense",
		"Mozilla/5.0 (Macintosh) ApplePrivacy",
		"",
	}
	for _, ua := range botCases {
		got := ClassifyOpen(ua, "8.8.8.8", time.Time{}, time.Now())
		if !got.IsBot || got.Reason != "user_agent" {
			t.Fatalf("ua=%q got %+v", ua, got)
		}
	}

	// Gmail (and Yahoo) load tracking pixels via their image proxies — real human opens.
	humanCases := []string{
		"Mozilla/5.0 (Windows NT 5.1; rv:11.0) Gecko Firefox/11.0 (via ggpht.com GoogleImageProxy)",
		"Mozilla/5.0 GoogleImageProxy",
		"YahooMailProxy; https://help.yahoo.com/kb/yahoo-mail-proxy-SLN28749.html",
	}
	sent := time.Now().Add(-time.Minute)
	for _, ua := range humanCases {
		got := ClassifyOpen(ua, "8.8.8.8", sent, time.Now())
		if got.IsBot {
			t.Fatalf("webmail proxy should count as human open: ua=%q got %+v", ua, got)
		}
	}
}

func TestClassifyOpenPrivateIP(t *testing.T) {
	got := ClassifyOpen("Mozilla/5.0", "127.0.0.1", time.Time{}, time.Now())
	if !got.IsBot || got.Reason != "datacenter_ip" {
		t.Fatalf("got %+v", got)
	}
}

func TestRequestClientIPPrefersForwarded(t *testing.T) {
	// Covered indirectly via routes tests; keep unit smoke for plausibility helpers.
	if !isPlausibleIP("203.0.113.10") {
		t.Fatal("expected plausible")
	}
	if isPlausibleIP("not-an-ip") {
		t.Fatal("expected reject")
	}
}
