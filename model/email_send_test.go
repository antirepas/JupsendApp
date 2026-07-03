package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestTryMarkEmailSendSending(t *testing.T) {
	db.OpenTestDB(t)

	userID, err := CreateUser("sending-test@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var contactID, templateID int64
	if err := db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('c@example.com', ?) RETURNING id`, userID).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}

	sendID, err := CreateQueuedEmailSend(userID, templateID, contactID, fmt.Sprintf("track-sending-%d", time.Now().UnixNano()), 0, "", 0)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := TryMarkEmailSendSending(sendID)
	if err != nil || !ok {
		t.Fatalf("expected first mark ok, got ok=%v err=%v", ok, err)
	}

	ok, err = TryMarkEmailSendSending(sendID)
	if err != nil || ok {
		t.Fatalf("expected second mark false, got ok=%v err=%v", ok, err)
	}

	status, err := GetEmailSendDeliveryStatus(sendID)
	if err != nil || status != "sending" {
		t.Fatalf("status=%q err=%v", status, err)
	}
}

func TestListEmailSendsNullSentAt(t *testing.T) {
	db.OpenTestDB(t)

	userID, err := CreateUser("null-sent@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var contactID, templateID int64
	if err := db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('c@example.com', ?) RETURNING id`, userID).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateQueuedEmailSend(userID, templateID, contactID, fmt.Sprintf("track-null-%d", time.Now().UnixNano()), 0, "", 0); err != nil {
		t.Fatal(err)
	}

	items, err := ListEmailSends(userID)
	if err != nil {
		t.Fatalf("ListEmailSends: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 send, got %d", len(items))
	}
	if !items[0].SentAt.IsZero() {
		t.Fatalf("expected zero sent_at for queued send, got %v", items[0].SentAt)
	}
	if items[0].DeliveryStatus != "queued" {
		t.Fatalf("expected queued, got %q", items[0].DeliveryStatus)
	}
}

func TestReconcileEmailSendAlreadySent(t *testing.T) {
	db.OpenTestDB(t)

	userID, err := CreateUser("reconcile-test@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var contactID, templateID int64
	if err := db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('c@example.com', ?) RETURNING id`, userID).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}

	sendID, err := CreateQueuedEmailSend(userID, templateID, contactID, fmt.Sprintf("track-reconcile-%d", time.Now().UnixNano()), 0, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := CreateSendJob(SendJob{UserID: userID, ContactID: contactID, TemplateID: templateID, EmailSendID: sendID})
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkEmailSendSent(sendID, 0, jobID); err != nil {
		t.Fatal(err)
	}
	ok, err := ClaimSendJob(jobID, "tok", 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("claim processing: ok=%v err=%v", ok, err)
	}

	if err := ReconcileEmailSendAlreadySent(sendID, 0, jobID); err != nil {
		t.Fatal(err)
	}
	job, err := GetSendJob(jobID)
	if err != nil || job.Status != "sent" {
		t.Fatalf("expected job sent, got %q err=%v", job.Status, err)
	}
}
