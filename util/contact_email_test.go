package util

import "testing"

func TestResolveImportEmailSemicolonRanking(t *testing.T) {
	got, ok := ResolveImportEmail(" support@acme.com ; hello@acme.com ; contact@acme.com ")
	if !ok {
		t.Fatal("expected ok")
	}
	if got != "hello@acme.com" {
		t.Fatalf("got %q want hello@acme.com", got)
	}
}

func TestResolveImportEmailNormalizeDedupe(t *testing.T) {
	got, ok := ResolveImportEmail("Hello@Acme.com;HELLO@acme.com")
	if !ok {
		t.Fatal("expected ok")
	}
	if got != "hello@acme.com" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveImportEmailSingle(t *testing.T) {
	got, ok := ResolveImportEmail("  Person@Example.COM  ")
	if !ok {
		t.Fatal("expected ok")
	}
	if got != "person@example.com" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveImportEmailInvalid(t *testing.T) {
	if _, ok := ResolveImportEmail("not-an-email"); ok {
		t.Fatal("expected invalid")
	}
}

func TestRankPreferredEmailSupportVsOther(t *testing.T) {
	got := RankPreferredEmail([]string{"support@x.com", "sales@x.com"})
	if got != "support@x.com" {
		t.Fatalf("got %q want support@x.com", got)
	}
}
