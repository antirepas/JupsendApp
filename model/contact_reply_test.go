package model

import (
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestFindRecentSendByRecipientEmail(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("bounce-resolve@test.com", "hash", "http://localhost")
	c := Contact{Email: "bounced@example.com"}
	cid, _ := c.SaveContact(userID, nil)
	var templateID int64
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)
	tracking := "track-bounce-1"
	_, err := db.Exec(`
		INSERT INTO email_sends (user_id, contact_id, template_id, tracking_id, sent_at, variant, delivery_status)
		VALUES (?, ?, ?, ?, ?, 'A', 'sent')
	`, userID, cid, templateID, tracking, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	gotCID, gotTrack, gotUID, err := FindRecentSendByRecipientEmail("bounced@example.com", 14)
	if err != nil {
		t.Fatal(err)
	}
	if gotCID != cid || gotTrack != tracking || gotUID != userID {
		t.Fatalf("got cid=%d track=%q uid=%d", gotCID, gotTrack, gotUID)
	}
}
