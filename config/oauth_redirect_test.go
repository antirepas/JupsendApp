package config

import "testing"

func TestOAuthRedirectHostMismatch(t *testing.T) {
	tests := []struct {
		redirect string
		base     string
		want     bool
	}{
		{"https://app.jupsend.com/auth/google/callback", "http://localhost:8080", true},
		{"https://app.jupsend.com/auth/google/callback", "https://app.jupsend.com", false},
		{"http://localhost:8080/auth/google/callback", "http://localhost:8080", false},
		{"http://127.0.0.1:8080/auth/google/callback", "http://localhost:8080", true},
		{"", "http://localhost:8080", false},
	}
	for _, tc := range tests {
		got := oauthRedirectHostMismatch(tc.redirect, tc.base)
		if got != tc.want {
			t.Fatalf("oauthRedirectHostMismatch(%q, %q) = %v want %v", tc.redirect, tc.base, got, tc.want)
		}
	}
}

func TestPickOAuthRedirectURI(t *testing.T) {
	BaseURL = "http://localhost:8080"
	derived := "http://localhost:8080/auth/google/callback"

	got := pickOAuthRedirectURI("App", "https://app.jupsend.com/auth/google/callback", derived)
	if got != derived {
		t.Fatalf("expected derived localhost URI, got %q", got)
	}

	got = pickOAuthRedirectURI("App", "http://localhost:8080/auth/google/callback", derived)
	if got != "http://localhost:8080/auth/google/callback" {
		t.Fatalf("expected env URI when hosts match, got %q", got)
	}

	got = pickOAuthRedirectURI("App", "", derived)
	if got != derived {
		t.Fatalf("expected derived when env empty, got %q", got)
	}
}
