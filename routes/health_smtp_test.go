package routes

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatSMTPProbeErrorBlocked(t *testing.T) {
	err := formatSMTPProbeError("smtp.gmail.com", "465", "me@x.com (manual)", errors.New("dial tcp: i/o timeout"))
	if err == nil || !strings.Contains(err.Error(), "may be blocked") {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "me@x.com") {
		t.Fatalf("should name account: %v", err)
	}
}

func TestFormatSMTPProbeErrorAuth(t *testing.T) {
	err := formatSMTPProbeError("smtp.gmail.com", "465", "me@x.com (inboxkit)", errors.New("535 Username and Password not accepted"))
	if err == nil || !strings.Contains(err.Error(), "App Password") {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "without spaces") {
		t.Fatalf("should mention spaces: %v", err)
	}
}
