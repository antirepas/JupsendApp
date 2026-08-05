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
	if err == nil || !strings.Contains(err.Error(), "InboxKit") {
		t.Fatalf("got %v", err)
	}
}

func TestFormatSMTPProbeErrorManualWorkspace(t *testing.T) {
	err := formatSMTPProbeError("smtp.gmail.com", "587", "starter@tryjupsend.com (manual)", errors.New("535 Username and Password not accepted"))
	if err == nil || !strings.Contains(err.Error(), "Pull credentials from InboxKit") {
		t.Fatalf("got %v", err)
	}
}

func TestFormatSMTPProbeErrorPersonalGmail(t *testing.T) {
	err := formatSMTPProbeError("smtp.gmail.com", "587", "me@gmail.com", errors.New("535 Username and Password not accepted"))
	if err == nil || !strings.Contains(err.Error(), "App Password") {
		t.Fatalf("got %v", err)
	}
}
