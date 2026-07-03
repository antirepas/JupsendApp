package model

import (
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestClaimSendJob(t *testing.T) {
	db.OpenTestDB(t)

	userID, err := CreateUser("claim-test@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}

	var contactID, templateID int64
	err = db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('claim-test@example.com', ?) RETURNING id`, userID).Scan(&contactID)
	if err != nil {
		t.Fatal(err)
	}
	err = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)
	if err != nil {
		t.Fatal(err)
	}

	jobID, err := CreateSendJob(SendJob{
		UserID:     userID,
		ContactID:  contactID,
		TemplateID: templateID,
	})
	if err != nil {
		t.Fatal(err)
	}

	ok, err := ClaimSendJob(jobID, "token-1", 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("expected claim success, ok=%v err=%v", ok, err)
	}

	ok, err = ClaimSendJob(jobID, "token-2", 30*time.Second)
	if err != nil || ok {
		t.Fatalf("expected second claim to fail, ok=%v err=%v", ok, err)
	}
}

func TestReleaseStaleJobsReconciled(t *testing.T) {
	db.OpenTestDB(t)

	userID, err := CreateUser("stale-test@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var contactID, templateID int64
	if err := db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('stale@example.com', ?) RETURNING id`, userID).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}

	mkStaleJob := func(deliveryStatus string) int64 {
		t.Helper()
		sendID, err := CreateQueuedEmailSend(userID, templateID, contactID, "track-"+deliveryStatus, 0, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if deliveryStatus == "sent" {
			_ = MarkEmailSendSent(sendID, 0, 0)
		} else if deliveryStatus == "sending" {
			_, _ = TryMarkEmailSendSending(sendID)
		}
		jobID, err := CreateSendJob(SendJob{UserID: userID, ContactID: contactID, TemplateID: templateID, EmailSendID: sendID})
		if err != nil {
			t.Fatal(err)
		}
		expired := time.Now().Add(-1 * time.Minute)
		_, err = db.Exec(`
			UPDATE send_jobs SET status='processing', lock_token='stale', lock_expires_at=?, claimed_at=?
			WHERE id=?
		`, expired, expired, jobID)
		if err != nil {
			t.Fatal(err)
		}
		return jobID
	}

	sentJob := mkStaleJob("sent")
	sendingJob := mkStaleJob("sending")
	queuedJob := mkStaleJob("queued")

	if err := ReleaseStaleJobsReconciled(); err != nil {
		t.Fatal(err)
	}

	sent, err := GetSendJob(sentJob)
	if err != nil || sent.Status != "sent" {
		t.Fatalf("sent job: status=%q err=%v", sent.Status, err)
	}

	dead, err := GetSendJob(sendingJob)
	if err != nil || dead.Status != "dead" {
		t.Fatalf("sending job: status=%q err=%v", dead.Status, err)
	}
	sendingStatus, _ := GetEmailSendDeliveryStatus(dead.EmailSendID)
	if sendingStatus != "failed" {
		t.Fatalf("sending email status=%q", sendingStatus)
	}

	pending, err := GetSendJob(queuedJob)
	if err != nil || pending.Status != "pending" {
		t.Fatalf("queued job: status=%q err=%v", pending.Status, err)
	}
	if pending.LastError != "worker timeout" {
		t.Fatalf("queued last_error=%q", pending.LastError)
	}
}
