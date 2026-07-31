package routes

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatSMTPProbeErrorBlocked(t *testing.T) {
	err := formatSMTPProbeError("smtp.gmail.com", "465", errors.New("dial tcp: i/o timeout"))
	if err == nil || !strings.Contains(err.Error(), "may be blocked") {
		t.Fatalf("got %v", err)
	}
}

func TestFormatSMTPProbeErrorAuth(t *testing.T) {
	err := formatSMTPProbeError("smtp.gmail.com", "465", errors.New("535 Username and Password not accepted"))
	if err == nil || !strings.Contains(err.Error(), "SMTP auth") {
		t.Fatalf("got %v", err)
	}
}
