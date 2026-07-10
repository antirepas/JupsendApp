package config

import "strings"

// NormalizeTrackingBaseURL strips trailing slashes and accidental /api/v1 suffixes.
func NormalizeTrackingBaseURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	base = strings.TrimSuffix(base, "/api/v1")
	return base
}

// TrackingWarning explains when open pixels are unlikely to work for the current BASE_URL.
func TrackingWarning(baseURL string) string {
	lower := strings.ToLower(strings.TrimSpace(baseURL))
	if lower == "" {
		return "Set BASE_URL to a public HTTPS URL so email clients can load tracking pixels."
	}
	if strings.Contains(lower, "localhost") || strings.Contains(lower, "127.0.0.1") {
		return "BASE_URL points to localhost. Mail clients on other devices cannot reach it, so opens will not be tracked."
	}
	if strings.Contains(lower, "ngrok-free.app") || strings.Contains(lower, "ngrok-free.dev") {
		return "Gmail open tracking does not work with ngrok free URLs. Google's image proxy is shown ngrok's warning page instead of your pixel, so the request never reaches this app. Use a paid ngrok static domain, Cloudflare Tunnel, or deploy to a real host. In Gmail you must also click Display images."
	}
	return ""
}
