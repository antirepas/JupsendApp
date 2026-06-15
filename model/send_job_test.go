package model

import (
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestClaimSendJobIdempotent(t *testing.T) {
	db.Prepare()
	defer db.DB.Close()

	var contactID int64
	err := db.DB.QueryRow(`INSERT INTO contact (email) VALUES ('claim-test@example.com') RETURNING id`).Scan(&contactID)
	if err != nil {
		t.Fatalf("contact: %v", err)
	}
	var templateID int64
	err = db.DB.QueryRow(`INSERT INTO template (name, subject, body) VALUES ('t','s','b') RETURNING id`).Scan(&templateID)
	if err != nil {
		t.Fatalf("template: %v", err)
	}

	jobID, err := CreateSendJob(SendJob{
		UserID:     1,
		ContactID:  contactID,
		TemplateID: templateID,
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	ok1, err := ClaimSendJob(jobID, "token-a", time.Minute)
	if err != nil || !ok1 {
		t.Fatalf("first claim failed ok=%v err=%v", ok1, err)
	}
	ok2, err := ClaimSendJob(jobID, "token-b", time.Minute)
	if err != nil || ok2 {
		t.Fatalf("second claim should fail ok=%v err=%v", ok2, err)
	}
}
