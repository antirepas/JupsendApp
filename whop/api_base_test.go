package whop

import (
	"testing"

	"emailtracker.com/config"
)

func TestAPIBaseDefault(t *testing.T) {
	config.WhopAPIBase = ""
	if got := apiBase(); got != "https://api.whop.com/api/v1" {
		t.Fatalf("got %q", got)
	}
}

func TestAPIBaseSandbox(t *testing.T) {
	config.WhopAPIBase = "https://sandbox-api.whop.com/api/v1/"
	if got := apiBase(); got != "https://sandbox-api.whop.com/api/v1" {
		t.Fatalf("got %q", got)
	}
}
