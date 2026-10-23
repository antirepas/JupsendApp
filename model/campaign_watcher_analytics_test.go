package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestSentimentFromReplyIntentMatchesWatchers(t *testing.T) {
	if got := SentimentFromReplyIntent("interested", "negative"); got != ReplySentimentPositive {
		t.Fatalf("interested → %q want positive", got)
	}
	if got := SentimentFromReplyIntent("unsubscribe", "positive"); got != ReplySentimentNegative {
		t.Fatalf("unsubscribe → %q want negative", got)
	}
	if got := SentimentFromReplyIntent("", "positive"); got != ReplySentimentPositive {
		t.Fatalf("empty intent falls back to sentiment, got %q", got)
	}
}

func TestGetCampaignWatcherAnalyticsIntentFunnel(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser(fmt.Sprintf("watch-an-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var contactID, templateID int64
	if err := db.QueryRow(`INSERT INTO contact (email, user_id, last_reply_intent, last_reply_sentiment) VALUES ('w@example.com', ?, 'interested', 'positive') RETURNING id`, userID).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	campID, err := CreateCampaign(userID, "watch analytics", templateID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = AddContactsToCampaign(campID, []int64{contactID})
	sendID, err := CreateQueuedEmailSend(userID, templateID, contactID, fmt.Sprintf("trk-%d", time.Now().UnixNano()), campID, "A", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`UPDATE email_sends SET delivery_status = 'sent', sent_at = ? WHERE id = ?`, time.Now(), sendID)
	_, _ = InsertContactEvent(ContactEventInput{
		ContactID: contactID, EmailSendID: sendID, CampaignID: campID, EventType: "REPLY",
	})

	wa := GetCampaignWatcherAnalytics(campID)
	if wa.Sent != 1 || wa.Replied != 1 {
		t.Fatalf("sent=%d replied=%d", wa.Sent, wa.Replied)
	}
	if wa.PositiveFromIntent != 1 {
		t.Fatalf("positive from intent=%d want 1", wa.PositiveFromIntent)
	}
	found := false
	for _, in := range wa.Intents {
		if in.Intent == "interested" && in.Replies == 1 && in.Sentiment == ReplySentimentPositive {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected interested intent row, got %+v", wa.Intents)
	}

	pos, neg, neu, pend := countCampaignReplySentiments(campID, "")
	if pos != 1 || neg != 0 || neu != 0 || pend != 0 {
		t.Fatalf("count sentiments pos=%d neg=%d neu=%d pend=%d", pos, neg, neu, pend)
	}
}
