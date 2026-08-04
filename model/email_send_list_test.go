package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestIsCancelledSend(t *testing.T) {
	if !IsCancelledSend("failed", "failed", CancelledCampaignStopMsg) {
		t.Fatal("legacy stop message should count as cancelled")
	}
	if !IsCancelledSend("cancelled", "", "") {
		t.Fatal("cancelled delivery status")
	}
	if IsCancelledSend("failed", "failed", "smtp timeout") {
		t.Fatal("real failure should not look cancelled")
	}
	if IsCancelledSend("sent", "", "") {
		t.Fatal("sent is not cancelled")
	}
}

func TestClearCancelledSendsLeavesDelivered(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser(fmt.Sprintf("clear-sends-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	var contactID, templateID int64
	_ = db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('c@test.com', ?) RETURNING id`, userID).Scan(&contactID)
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)

	cancelledID, err := CreateQueuedEmailSend(userID, templateID, contactID, "track-cancel-1", 0, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := CreateSendJob(SendJob{
		UserID: userID, ContactID: contactID, TemplateID: templateID, EmailSendID: cancelledID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`UPDATE email_sends SET delivery_status='failed', send_job_id=? WHERE id=?`, jobID, cancelledID)
	_, _ = db.Exec(`UPDATE send_jobs SET status='failed', last_error=? WHERE id=?`, CancelledCampaignStopMsg, jobID)

	sentID, err := CreateQueuedEmailSend(userID, templateID, contactID, "track-sent-1", 0, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`UPDATE email_sends SET delivery_status='sent', sent_at=CURRENT_TIMESTAMP WHERE id=?`, sentID)

	failedID, err := CreateQueuedEmailSend(userID, templateID, contactID, "track-fail-1", 0, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	failJob, err := CreateSendJob(SendJob{
		UserID: userID, ContactID: contactID, TemplateID: templateID, EmailSendID: failedID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`UPDATE email_sends SET delivery_status='failed', send_job_id=? WHERE id=?`, failJob, failedID)
	_, _ = db.Exec(`UPDATE send_jobs SET status='failed', last_error='smtp timeout' WHERE id=?`, failJob)

	n, err := ClearCancelledSends(userID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("cleared=%d want 1", n)
	}

	var leftCancel, leftSent, leftFail int
	_ = db.QueryRow(`SELECT COUNT(*) FROM email_sends WHERE id=?`, cancelledID).Scan(&leftCancel)
	_ = db.QueryRow(`SELECT COUNT(*) FROM email_sends WHERE id=?`, sentID).Scan(&leftSent)
	_ = db.QueryRow(`SELECT COUNT(*) FROM email_sends WHERE id=?`, failedID).Scan(&leftFail)
	if leftCancel != 0 {
		t.Fatal("cancelled row should be deleted")
	}
	if leftSent != 1 {
		t.Fatal("sent row must remain")
	}
	if leftFail != 1 {
		t.Fatal("real failed row must remain")
	}
}

func TestListEmailSendsFilteredExcludesCancelledByDefault(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser(fmt.Sprintf("list-sends-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	var contactID, templateID int64
	_ = db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('lead@test.com', ?) RETURNING id`, userID).Scan(&contactID)
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('Intro','Hi','b', ?) RETURNING id`, userID).Scan(&templateID)
	campID, _ := CreateCampaign(userID, "Spring", templateID, 0, "bulk", 0, "", "")

	sentID, _ := CreateQueuedEmailSend(userID, templateID, contactID, "track-ok", campID, "A", 0)
	_, _ = db.Exec(`UPDATE email_sends SET delivery_status='sent', sent_at=CURRENT_TIMESTAMP WHERE id=?`, sentID)

	cancelID, _ := CreateQueuedEmailSend(userID, templateID, contactID, "track-cxl", campID, "A", 0)
	jobID, _ := CreateSendJob(SendJob{
		UserID: userID, ContactID: contactID, TemplateID: templateID, CampaignID: campID, EmailSendID: cancelID,
	})
	_, _ = db.Exec(`UPDATE email_sends SET delivery_status='failed', send_job_id=? WHERE id=?`, jobID, cancelID)
	_, _ = db.Exec(`UPDATE send_jobs SET status='failed', last_error=? WHERE id=?`, CancelledCampaignStopMsg, jobID)

	page, err := ListEmailSendsFiltered(userID, SendListFilter{PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range page.Items {
		if it.ID == cancelID {
			t.Fatal("default list should hide cancelled")
		}
	}
	foundSent := false
	for _, it := range page.Items {
		if it.ID == sentID {
			foundSent = true
		}
	}
	if !foundSent {
		t.Fatal("expected sent row in default inbox")
	}

	cancelledPage, err := ListEmailSendsFiltered(userID, SendListFilter{Status: "cancelled", PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	foundCancel := false
	for _, it := range cancelledPage.Items {
		if it.ID == cancelID {
			foundCancel = true
			if !it.IsCancelled() {
				t.Fatal("expected IsCancelled")
			}
			if !it.CanDelete() {
				t.Fatal("cancelled should be deletable")
			}
		}
	}
	if !foundCancel {
		t.Fatal("status=cancelled should include cancelled row")
	}

	counts, err := CountEmailSendsSummary(userID)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Cancelled < 1 {
		t.Fatalf("cancelled count=%d", counts.Cancelled)
	}
}

func TestDeleteEmailSendForUserSkipsSent(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser(fmt.Sprintf("del-send-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	var contactID, templateID int64
	_ = db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('d@test.com', ?) RETURNING id`, userID).Scan(&contactID)
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)

	sentID, _ := CreateQueuedEmailSend(userID, templateID, contactID, "track-del-sent", 0, "", 0)
	_, _ = db.Exec(`UPDATE email_sends SET delivery_status='sent', sent_at=CURRENT_TIMESTAMP WHERE id=?`, sentID)
	ok, err := DeleteEmailSendForUser(userID, sentID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("must not delete sent")
	}

	queuedID, _ := CreateQueuedEmailSend(userID, templateID, contactID, "track-del-q", 0, "", 0)
	ok, err = DeleteEmailSendForUser(userID, queuedID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("queued should delete")
	}
}
