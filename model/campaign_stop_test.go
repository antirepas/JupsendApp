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
	job, err := GetSendJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "failed" {
		t.Fatalf("job status=%q want failed", job.Status)
	}
	if !CampaignIsStopped(campID) {
		t.Fatal("CampaignIsStopped should be true")
	}
	if ComputeDisplayStatus(camp.Status, nil, false) != "stopped" {
		t.Fatal("display status should be stopped")
	}
}
