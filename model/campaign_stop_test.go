package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
)

func TestApplyPlanLimitsPreservesSendsToday(t *testing.T) {
	db.OpenTestDB(t)
	config.SMTPHost = "smtp.example.com"
	config.SMTPUser = "shared@example.com"
	config.SMTPPass = "pass"
	config.SMTPFrom = "shared@example.com"
	config.SMTPPort = "587"

	userID, err := CreateUser(fmt.Sprintf("limits-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyPlanLimitsToUser(userID, PlanTierFree); err != nil {
		t.Fatal(err)
	}
	acct, err := GetSendReadyAccountForUser(userID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE smtp_accounts SET sends_today = 7, last_send_at = ? WHERE id = ?`, time.Now(), acct.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := ApplyPlanLimitsToUser(userID, PlanTierFree); err != nil {
		t.Fatal(err)
	}
	acct, err = GetSendReadyAccountForUser(userID)
	if err != nil {
		t.Fatal(err)
	}
	if acct.SendsToday != 7 {
		t.Fatalf("sends_today reset unexpectedly: got %d want 7", acct.SendsToday)
	}
	if acct.DailyLimit != 10 {
		t.Fatalf("daily_limit=%d want 10", acct.DailyLimit)
	}
}

func TestStopCampaignCancelsQueuedJobs(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser(fmt.Sprintf("stop-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var contactID, templateID int64
	if err := db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('stop@example.com', ?) RETURNING id`, userID).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	campID, err := CreateCampaign(userID, "c", templateID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = AddContactsToCampaign(campID, []int64{contactID})

	sendID, err := CreateQueuedEmailSend(userID, templateID, contactID, fmt.Sprintf("track-stop-%d", time.Now().UnixNano()), campID, "A", 0)
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := CreateSendJob(SendJob{
		UserID: userID, ContactID: contactID, TemplateID: templateID,
		CampaignID: campID, Variant: "A", EmailSendID: sendID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = MarkCampaignSending(campID)

	result, err := StopCampaign(campID, userID)
	if err != nil {
		t.Fatal(err)
	}
	if result.CancelledJobs != 1 {
		t.Fatalf("cancelled jobs=%d want 1", result.CancelledJobs)
	}

	camp, err := GetCampaignForUser(campID, userID)
	if err != nil {
		t.Fatal(err)
	}
	if camp.Status != "stopped" || camp.IsSending {
		t.Fatalf("status=%q isSending=%v", camp.Status, camp.IsSending)
	}
	if _, err := GetSendJob(jobID); err == nil {
		t.Fatal("expected send job to be deleted on stop")
	}
	var sendLeft int
	_ = db.QueryRow(`SELECT COUNT(*) FROM email_sends WHERE id = ?`, sendID).Scan(&sendLeft)
	if sendLeft != 0 {
		t.Fatalf("expected email_send deleted, still have %d", sendLeft)
	}
	if !CampaignIsStopped(campID) {
		t.Fatal("CampaignIsStopped should be true")
	}
	if ComputeDisplayStatus(camp.Status, nil, false) != "stopped" {
		t.Fatal("display status should be stopped")
	}
}

func TestContinueCampaignReopensStopped(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser(fmt.Sprintf("continue-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var contactID, templateID int64
	if err := db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('continue@example.com', ?) RETURNING id`, userID).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	campID, err := CreateCampaign(userID, "c", templateID, 0, "sequence", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = AddContactsToCampaign(campID, []int64{contactID})
	instID, created, err := CreateSequenceInstance(campID, contactID, "A")
	if err != nil || !created {
		t.Fatalf("create sequence instance: id=%d created=%v err=%v", instID, created, err)
	}
	_, _ = db.Exec(`UPDATE campaign_sequence_instances SET step_index = 1, status = 'waiting' WHERE id = ?`, instID)
	_ = MarkCampaignSent(campID)

	if _, err := StopCampaign(campID, userID); err != nil {
		t.Fatal(err)
	}
	var status string
	_ = db.QueryRow(`SELECT status FROM campaign_sequence_instances WHERE id = ?`, instID).Scan(&status)
	if status != "cancelled" {
		t.Fatalf("instance status=%q want cancelled", status)
	}

	if err := ContinueCampaign(campID, userID); err != nil {
		t.Fatal(err)
	}
	camp, err := GetCampaignForUser(campID, userID)
	if err != nil {
		t.Fatal(err)
	}
	if camp.Status != "draft" {
		t.Fatalf("campaign status=%q want draft", camp.Status)
	}
	var step int
	_ = db.QueryRow(`SELECT status, step_index FROM campaign_sequence_instances WHERE id = ?`, instID).Scan(&status, &step)
	if status != "active" || step != 1 {
		t.Fatalf("instance status=%q step=%d want active at step 1", status, step)
	}
}

func TestContactIDsWithSuccessfulSendInCampaign(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser(fmt.Sprintf("alreadysent-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var contactID, templateID int64
	if err := db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('sent@example.com', ?) RETURNING id`, userID).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	campID, err := CreateCampaign(userID, "c", templateID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	sendID, err := CreateQueuedEmailSend(userID, templateID, contactID, fmt.Sprintf("track-sent-%d", time.Now().UnixNano()), campID, "A", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`UPDATE email_sends SET delivery_status = 'sent', sent_at = ? WHERE id = ?`, time.Now(), sendID)

	got, err := ContactIDsWithSuccessfulSendInCampaign(campID)
	if err != nil {
		t.Fatal(err)
	}
	if !got[contactID] {
		t.Fatalf("expected contact %d marked already sent: %+v", contactID, got)
	}
}
