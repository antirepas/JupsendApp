package util

import (
	"net/url"
	"strings"

	"emailtracker.com/config"
	"github.com/google/uuid"
)

// NormalizeTrackingBaseURL delegates to config for a single source of truth.
func NormalizeTrackingBaseURL(baseURL string) string {
	return config.NormalizeTrackingBaseURL(baseURL)
}

func GenerateLinkTrackingID() string {
	return uuid.New().String()
}

func TrackOpenURL(baseURL, trackingID string) string {
	base := NormalizeTrackingBaseURL(baseURL)
	if base == "" {
		base = "http://localhost:8080"
	}
	return base + "/api/v1/track/open/" + trackingID
}

func TrackClickURL(baseURL, linkTrackingID string) string {
	base := NormalizeTrackingBaseURL(baseURL)
	if base == "" {
		base = "http://localhost:8080"
	}
	return base + "/api/v1/track/click/" + linkTrackingID
}

func shouldSkipLinkTracking(href string) bool {
	h := strings.TrimSpace(href)
	if h == "" || strings.HasPrefix(h, "#") {
		return true
	}
	lower := strings.ToLower(h)
	switch {
	case strings.HasPrefix(lower, "mailto:"),
		strings.HasPrefix(lower, "tel:"),
		strings.HasPrefix(lower, "javascript:"),
		strings.HasPrefix(lower, "data:"):
		return true
	case strings.Contains(lower, "/track/click/"),
		strings.Contains(lower, "/track/open/"),
		strings.Contains(lower, "/api/v1/u/"),
		strings.HasPrefix(lower, "/u/"):
		return true
	default:
		return false
	}
}

// SafeRedirectURL returns true only for absolute http(s) destinations.
func SafeRedirectURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	return u.String(), true
}
