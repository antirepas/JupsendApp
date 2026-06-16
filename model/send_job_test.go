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
