package outbound

import (
	"errors"
	"testing"
	"time"

	"emailtracker.com/model"
)

func TestClassifySMTPError(t *testing.T) {
	if ClassifySMTPError(errors.New("535 authentication failed")) != ErrorPermanent {
		t.Fatal("expected permanent for auth")
	}
	if ClassifySMTPError(errors.New("550 user unknown")) != ErrorPermanent {
		t.Fatal("expected permanent for 550")
	}
	if ClassifySMTPError(errors.New("connection timeout")) != ErrorTransient {
		t.Fatal("expected transient")
	}
}

func TestBackoffForAttempt(t *testing.T) {
	delay := BackoffForAttempt(1)
	if delay != 15*time.Second {
		t.Fatalf("got %v", delay)
	}
	if BackoffForAttempt(10) != 60*time.Minute {
		t.Fatalf("got %v", BackoffForAttempt(10))
	}
}

func TestEffectiveDailyCap(t *testing.T) {
	acc := model.SMTPAccount{
		WarmupEnabled:         true,
		WarmupDailyCap:        5,
		WarmupTargetDailyCap:  50,
		WarmupIncrementPerDay: 5,
		DailyLimit:            50,
	}
	// Anchor to calendar midnights so the test is stable regardless of wall-clock time of day.
	today := time.Now().UTC()
	start := time.Date(today.Year(), today.Month(), today.Day(), 12, 0, 0, 0, time.UTC).Add(-48 * time.Hour)
	acc.WarmupStartedAt = &start
	cap := EffectiveDailyCap(acc)
	if cap != 15 {
		t.Fatalf("expected 15 got %d", cap)
	}
	acc.WarmupEnabled = false
	if EffectiveDailyCap(acc) != 50 {
		t.Fatal("expected daily limit when warmup off")
	}
}

func TestIsBounceMessage(t *testing.T) {
	if !IsBounceMessage("mailer-daemon@google.com", "Delivery Status Notification", "") {
		t.Fatal("expected bounce")
	}
	body := "X-Failed-Recipients: bad@example.com"
	if !IsBounceMessage("postmaster@", "failed mail", body) {
		t.Fatal("expected bounce from failed recipients")
	}
}

func TestExtractFailedRecipient(t *testing.T) {
	body := "Final-Recipient: rfc822; alice@nowhere.invalid\r\n"
	if got := ExtractFailedRecipient(body); got != "alice@nowhere.invalid" {
		t.Fatalf("got %q", got)
	}
	body2 := "X-Failed-Recipients: bad@example.com\n"
	if got := ExtractFailedRecipient(body2); got != "bad@example.com" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractSendIDFromBounce(t *testing.T) {
	body := "X-EmailTracker-Send-ID: 42\r\n"
	if ExtractSendIDFromBounce(body) != 42 {
		t.Fatal("expected send id 42")
	}
}

func TestIsBounceMessageUndeliverable(t *testing.T) {
	if !IsBounceMessage("mailer-daemon@google.com", "Undeliverable: Hello", "Address not found") {
		t.Fatal("expected bounce")
	}
	if !IsBounceMessage("mailer-daemon@googlemail.com", "Delivery Status Notification (Failure)", "The email account that you tried to reach does not exist") {
		t.Fatal("expected bounce for does not exist")
	}
	if !IsBounceMessage("mailer-daemon@google.com", "Undeliverable", "Your message wasn't delivered to gone@example.com because the address no longer exists") {
		t.Fatal("expected bounce for no longer exists")
	}
}

func TestExtractFailedRecipientGmailBody(t *testing.T) {
	body := "Your message wasn't delivered to gone@example.com because the address couldn't be found"
	if got := ExtractFailedRecipient(body); got != "gone@example.com" {
		t.Fatalf("got %q", got)
	}
	body2 := "Address not found\r\n\r\nsomeone@nowhere.invalid is unable to receive mail"
	if got := ExtractFailedRecipient(body2); got != "someone@nowhere.invalid" {
		t.Fatalf("fallback got %q", got)
	}
}
