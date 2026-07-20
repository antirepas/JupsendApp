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
	start := time.Now().Add(-48 * time.Hour)
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
}
