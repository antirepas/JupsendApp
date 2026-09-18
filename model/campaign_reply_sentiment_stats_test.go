package model

import (
	"testing"

	"emailtracker.com/db"
)

func TestCountCampaignReplySentiments(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("sent-stats@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var templateID int64
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	campID, err := CreateCampaign(userID, "Sentiment camp", templateID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cPos, err := (&Contact{Email: "pos-sent@example.com"}).SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	cNeg, err := (&Contact{Email: "neg-sent@example.com"}).SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = AddContactsToCampaign(campID, []int64{cPos, cNeg})

	sendPos, err := enqueueTestSend(userID, templateID, cPos, campID)
	if err != nil {
		t.Fatal(err)
	}
	sendNeg, err := enqueueTestSend(userID, templateID, cNeg, campID)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = InsertContactEvent(ContactEventInput{ContactID: cPos, EmailSendID: sendPos, EventType: "REPLY"})
	_, _ = InsertContactEvent(ContactEventInput{ContactID: cNeg, EmailSendID: sendNeg, EventType: "REPLY"})
	_, _ = db.Exec(`UPDATE contact SET last_reply_sentiment = 'positive' WHERE id = ?`, cPos)
	_, _ = db.Exec(`UPDATE contact SET last_reply_sentiment = 'negative' WHERE id = ?`, cNeg)

	pos, neg, neu, pend := countCampaignReplySentiments(campID, "")
	if pos != 1 || neg != 1 || neu != 0 || pend != 0 {
		t.Fatalf("pos=%d neg=%d neu=%d pend=%d", pos, neg, neu, pend)
	}

	a := CampaignAnalytics{CampaignID: campID, Overview: CampaignOverview{ContactCount: 2}, Contacts: []ContactEngagementRow{
		{SendID: sendPos}, {SendID: sendNeg},
	}}
	fillOverview(&a)
	if a.Overview.PositiveReplies != 1 || a.Overview.NegativeReplies != 1 {
		t.Fatalf("overview %+v", a.Overview)
	}
	if a.Overview.PositiveReplyRate <= 0 || a.Overview.NegativeReplyRate <= 0 {
		t.Fatalf("rates pos=%.1f neg=%.1f", a.Overview.PositiveReplyRate, a.Overview.NegativeReplyRate)
	}
}
