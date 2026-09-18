package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestGetMailboxSendRates(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser(fmt.Sprintf("mb-rates-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	smtpID, err := UpsertInboxKitSMTPAccount(userID, "from@example.com", "smtp.gmail.com", "587", "from@example.com", "pass-aaaa-aaaa-aaaa", "From", "ik-rates", true, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	otherSMTP, err := UpsertInboxKitSMTPAccount(userID, "other@example.com", "smtp.gmail.com", "587", "other@example.com", "pass-bbbb-bbbb-bbbb", "Other", "ik-other", false, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	tpl := Template{Name: "t", Subject: "hi", Body: "body"}
	tplID, err := tpl.SaveTemplate(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := Contact{Email: "lead@example.com"}
	contactID, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}

	makeSent := func(smtp int64, variant, track string) int64 {
		t.Helper()
		sendID, err := CreateQueuedEmailSend(userID, tplID, contactID, track, 0, variant, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := MarkEmailSendSent(sendID, smtp, 0); err != nil {
			t.Fatal(err)
		}
		return sendID
	}

	sendA := makeSent(smtpID, "", fmt.Sprintf("track-a-%d", time.Now().UnixNano()))
	sendB := makeSent(smtpID, "", fmt.Sprintf("track-b-%d", time.Now().UnixNano()))
	_ = makeSent(smtpID, DeliverabilityProbeVariant, fmt.Sprintf("track-probe-%d", time.Now().UnixNano()))
	_ = makeSent(otherSMTP, "", fmt.Sprintf("track-other-%d", time.Now().UnixNano()))

	_, _ = db.Exec(`
		INSERT INTO email_events (email_send_id, tracking_id, event_type, is_bot, created_at)
		VALUES (?, ?, 'open', 0, CURRENT_TIMESTAMP)
	`, sendA, fmt.Sprintf("track-open-%d", sendA))
	_, _ = db.Exec(`
		INSERT INTO email_events (email_send_id, tracking_id, event_type, is_bot, created_at)
		VALUES (?, ?, 'bounce', 0, CURRENT_TIMESTAMP)
	`, sendB, fmt.Sprintf("track-bounce-%d", sendB))
	_, _ = InsertContactEvent(ContactEventInput{
		ContactID: contactID, EmailSendID: sendA, EventType: "REPLY",
	})

	rates := GetMailboxSendRates(userID, smtpID, 30)
	if rates.TotalSends != 2 {
		t.Fatalf("expected 2 sends (probe excluded), got %d", rates.TotalSends)
	}
	if rates.OpenRate <= 0 {
		t.Fatalf("expected open rate > 0, got %v", rates.OpenRate)
	}
	if rates.BounceRate <= 0 {
		t.Fatalf("expected bounce rate > 0, got %v", rates.BounceRate)
	}
	if rates.ReplyRate <= 0 {
		t.Fatalf("expected reply rate > 0, got %v", rates.ReplyRate)
	}
	if rates.HealthLabel != "Insufficient data" {
		t.Fatalf("expected thin sample label, got %q", rates.HealthLabel)
	}
}

func TestMailboxHealthFromRates(t *testing.T) {
	label, _ := mailboxHealthFromRates(MailboxSendRates{PeriodDays: 30, TotalSends: 25, BounceRate: 6, OpenRate: 20})
	if label != "Poor" {
		t.Fatalf("got %q", label)
	}
	label, _ = mailboxHealthFromRates(MailboxSendRates{PeriodDays: 30, TotalSends: 25, BounceRate: 3, OpenRate: 20})
	if label != "Watch" {
		t.Fatalf("got %q", label)
	}
	label, _ = mailboxHealthFromRates(MailboxSendRates{PeriodDays: 30, TotalSends: 25, BounceRate: 0.5, OpenRate: 30})
	if label != "Healthy" {
		t.Fatalf("got %q", label)
	}
}

func TestEnsureDeliverabilityProbeTemplateIdempotent(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser(fmt.Sprintf("probe-tpl-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	id1, err := EnsureDeliverabilityProbeTemplate(userID)
	if err != nil || id1 == 0 {
		t.Fatalf("id1=%d err=%v", id1, err)
	}
	id2, err := EnsureDeliverabilityProbeTemplate(userID)
	if err != nil || id2 != id1 {
		t.Fatalf("want %d got %d err=%v", id1, id2, err)
	}
}
