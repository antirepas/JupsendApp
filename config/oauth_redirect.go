package config

import (
	"log"
	"net/url"
	"os"
	"strings"
)

func applyGoogleOAuthRedirectURIs() {
	gmailDerived := strings.TrimRight(BaseURL, "/") + "/settings/gmail/callback"
	appDerived := strings.TrimRight(BaseURL, "/") + "/auth/google/callback"

	envGmail := strings.TrimSpace(os.Getenv("GOOGLE_OAUTH_REDIRECT_URI"))
	envApp := strings.TrimSpace(os.Getenv("GOOGLE_APP_OAUTH_REDIRECT_URI"))

	GoogleOAuthRedirectURI = pickOAuthRedirectURI("Gmail", envGmail, gmailDerived)
	GoogleAppOAuthRedirectURI = pickOAuthRedirectURI("App sign-in", envApp, appDerived)
}

func pickOAuthRedirectURI(label, envValue, derived string) string {
	if envValue == "" {
		return derived
	}
	if oauthRedirectHostMismatch(envValue, BaseURL) {
		log.Printf(
			"config: %s OAuth redirect URI %q does not match BASE_URL %q — using %q for local/dev",
			label,
			envValue,
			BaseURL,
			derived,
		)
		return derived
	}
	return envValue
}

func oauthRedirectHostMismatch(redirectURI, baseURL string) bool {
	redirectHost := urlHost(redirectURI)
	baseHost := urlHost(baseURL)
	if redirectHost == "" || baseHost == "" {
		return false
	}
	if isLoopbackHost(baseHost) != isLoopbackHost(redirectHost) {
		return true
	}
	return !strings.EqualFold(redirectHost, baseHost)
}

func urlHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(host)
	name := host
	if h, _, ok := strings.Cut(host, ":"); ok {
		name = h
	}
	switch name {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	}
	return strings.HasPrefix(name, "127.")
}
