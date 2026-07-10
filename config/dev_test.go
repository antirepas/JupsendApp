package config

import "testing"

func TestIsLocalDev(t *testing.T) {
	BaseURL = "http://localhost:8080"
	if !IsLocalDev() {
		t.Fatal("expected local dev for localhost BASE_URL")
	}
	BaseURL = "https://app.jupsend.com"
	if IsLocalDev() {
		t.Fatal("expected prod for public BASE_URL")
	}
}
