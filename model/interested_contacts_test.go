package model

import (
	"fmt"
	"testing"

	"emailtracker.com/db"
)

func TestListInterestedContactsScoring(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("interested@test.com", "hash", "http://localhost")
	var templateID int64
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)

	c := Contact{Email: "hot@test.com"}
	cid, _ := c.SaveContact(userID, nil)
	campaignID, _ := CreateCampaign(userID, "Camp", templateID, 0, "bulk", 0, "", "")

	sendID, err := enqueueTestSend(userID, templateID, cid, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = InsertContactEvent(ContactEventInput{
		ContactID:   cid,
		CampaignID:  campaignID,
		EmailSendID: sendID,
		EventType:   "REPLY",
		DedupeKey:   fmt.Sprintf("test-reply-%d", cid),
	})
	if err != nil {
		t.Fatal(err)
	}

	list, err := ListInterestedContacts(userID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("expected interested contact")
	}
	if list[0].Tier != "hot" {
		t.Fatalf("tier=%q", list[0].Tier)
	}
}

func enqueueTestSend(userID, templateID, contactID, campaignID int64) (int64, error) {
	row := db.QueryRow(`
		INSERT INTO email_sends (user_id, contact_id, template_id, campaign_id, tracking_id, sent_at, variant, delivery_status)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, 'A', 'sent') RETURNING id
	`, userID, contactID, templateID, campaignID, fmt.Sprintf("track-test-%d", contactID))
	var id int64
	err := row.Scan(&id)
	return id, err
}
