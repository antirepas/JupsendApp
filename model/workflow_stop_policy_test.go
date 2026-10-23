package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestShouldBlockWorkflowSendStopOnReply(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser(fmt.Sprintf("stop-reply-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var contactID, templateID int64
	if err := db.QueryRow(`INSERT INTO contact (email, user_id, replied_at) VALUES ('r@example.com', ?, ?) RETURNING id`, userID, time.Now()).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	campID, err := CreateCampaign(userID, "c", templateID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE campaigns SET stop_on_reply = TRUE WHERE id = ?`, campID); err != nil {
		t.Fatal(err)
	}

	block, reason := ShouldBlockWorkflowSend(campID, contactID)
	if !block || reason == "" {
		t.Fatalf("outreach should block after reply, block=%v reason=%q", block, reason)
	}
	block, reason = ShouldBlockReplyWatcherSend(campID, contactID)
	if block {
		t.Fatalf("reply watcher must not be blocked by stop-on-reply, reason=%q", reason)
	}
}
