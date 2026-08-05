package util

import "testing"

func TestNormalizeGmailSMTPPort(t *testing.T) {
	if got := NormalizeGmailSMTPPort("smtp.gmail.com", "465"); got != "587" {
		t.Fatalf("gmail 465 → 587, got %q", got)
	}
	if got := NormalizeGmailSMTPPort("smtp.gmail.com", ""); got != "587" {
		t.Fatalf("gmail empty → 587, got %q", got)
	}
	if got := NormalizeGmailSMTPPort("smtp.gmail.com", "587"); got != "587" {
		t.Fatalf("gmail 587 stays, got %q", got)
	}
	if got := NormalizeGmailSMTPPort("smtp.office365.com", "465"); got != "465" {
		t.Fatalf("non-gmail 465 stays, got %q", got)
	}
}

func TestAlternateSMTPPort(t *testing.T) {
	p, tls := alternateSMTPPort("465")
	if p != "587" || tls {
		t.Fatalf("465 alt = %s tls=%v", p, tls)
	}
	p, tls = alternateSMTPPort("587")
	if p != "465" || !tls {
		t.Fatalf("587 alt = %s tls=%v", p, tls)
	}
}

func TestShouldTryAlternateSMTPPort(t *testing.T) {
	if !shouldTryAlternateSMTPPort(errString("read tcp … i/o timeout")) {
		t.Fatal("timeout should retry")
	}
	if shouldTryAlternateSMTPPort(errString("535 BadCredentials")) {
		t.Fatal("auth errors should not trigger port swap")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
