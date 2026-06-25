package util

import "testing"

func TestValidateEmailSyntax(t *testing.T) {
	ok, _ := ValidateEmail("alice@example.com")
	if !ok {
		t.Fatal("expected valid email")
	}
	ok, reason := ValidateEmail("not-an-email")
	if ok {
		t.Fatal("expected invalid")
	}
	if reason == "" {
		t.Fatal("expected reason")
	}
}

func TestValidateEmailKnownDomain(t *testing.T) {
	ok, _ := ValidateEmail("test@gmail.com")
	if !ok {
		t.Fatal("expected gmail.com to have MX")
	}
}
