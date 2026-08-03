package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestListContactTimeline(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser("timeline@test.com", "hash", "http://localhost")
	var templateID int64
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('Intro','Hello there','body', ?) RETURNING id`, userID).Scan(&templateID)

	c := Contact{Email: "lead@test.com"}
	cid, _ := c.SaveContact(userID, nil)
	campaignID, _ := CreateCampaign(userID, "Spring outreach", templateID, 0, "bulk", 0, "", "")
	sendID, err := enqueueTestSend(userID, templateID, cid, campaignID)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	_, err = InsertContactEvent(ContactEventInput{
		ContactID:   cid,
		CampaignID:  campaignID,
		EmailSendID: sendID,
		EventType:   "SEND",
		OccurredAt:  now.Add(-3 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = InsertContactEvent(ContactEventInput{
		ContactID:   cid,
		CampaignID:  campaignID,
		EmailSendID: sendID,
		EventType:   "OPEN",
		OccurredAt:  now.Add(-2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = InsertContactEvent(ContactEventInput{
		ContactID:   cid,
		CampaignID:  campaignID,
		EmailSendID: sendID,
		EventType:   "CLICK",
		OccurredAt:  now.Add(-1 * time.Hour),
		Metadata:    map[string]interface{}{"clicked_url": "https://example.com/pricing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = InsertContactEvent(ContactEventInput{
		ContactID:   cid,
		EmailSendID: sendID,
		EventType:   "REPLY",
		OccurredAt:  now,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Duplicate open from email_events should collapse with contact_events open.
	_, err = db.Exec(`
		INSERT INTO email_events (email_send_id, tracking_id, event_type, created_at)
		VALUES (?, ?, 'open', ?)
	`, sendID, fmt.Sprintf("track-timeline-%d", cid), now.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	items, err := ListContactTimeline(userID, cid, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 4 {
		t.Fatalf("expected at least send/open/click/reply, got %d: %+v", len(items), items)
	}

	types := map[string]int{}
	for _, it := range items {
		types[it.Type]++
		if it.Type == "click" && it.URL != "https://example.com/pricing" {
			t.Fatalf("click url=%q", it.URL)
		}
		if it.Type == "open" && it.CampaignName != "Spring outreach" {
			t.Fatalf("open campaign=%q", it.CampaignName)
		}
	}
	if types["send"] < 1 || types["open"] < 1 || types["click"] < 1 || types["reply"] < 1 {
		t.Fatalf("missing event types: %+v", types)
	}
	// Deduped: not two opens for the same minute.
	if types["open"] > 1 {
		t.Fatalf("expected open deduped, got %d", types["open"])
	}
	if !items[0].At.After(items[len(items)-1].At) && !items[0].At.Equal(items[len(items)-1].At) {
		t.Fatal("expected newest-first order")
	}
	if items[0].Type != "reply" {
		t.Fatalf("newest should be reply, got %s", items[0].Type)
	}
}
