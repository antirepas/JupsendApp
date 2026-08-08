package config

import (
	"strings"
	"testing"
)

func TestWithSupportContact(t *testing.T) {
	if SupportEmail != "akupstas9@gmail.com" {
		t.Fatalf("SupportEmail=%q", SupportEmail)
	}
	got := WithSupportContact("Something failed")
	if !strings.Contains(got, SupportEmail) {
		t.Fatalf("missing support email: %q", got)
	}
	again := WithSupportContact(got)
	if again != got {
		t.Fatalf("should not duplicate: %q vs %q", again, got)
	}
}
