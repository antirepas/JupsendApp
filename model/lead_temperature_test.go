package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestClassifyLeadTemperatureDefaults(t *testing.T) {
	rules := DefaultLeadTemperatureRules()
	if got := ClassifyLeadTemperature(rules, CampaignContactEngagementCounts{}); got != LeadTemperatureCold {
		t.Fatalf("empty -> %s", got)
	}
	if got := ClassifyLeadTemperature(rules, CampaignContactEngagementCounts{Opens: 2, Clicks: 1}); got != LeadTemperatureWarm {
		t.Fatalf("warm threshold -> %s", got)
	}
	if got := ClassifyLeadTemperature(rules, CampaignContactEngagementCounts{Opens: 3, Clicks: 2}); got != LeadTemperatureHot {
		t.Fatalf("hot threshold -> %s", got)
	}
	if got := ClassifyLeadTemperature(rules, CampaignContactEngagementCounts{Opens: 0, Clicks: 0, Replies: 1}); got != LeadTemperatureHot {
		t.Fatalf("reply -> %s", got)
	}
	// Warm is not hot when below hot mins even with many opens alone.
	if got := ClassifyLeadTemperature(rules, CampaignContactEngagementCounts{Opens: 10, Clicks: 0}); got != LeadTemperatureCold {
		t.Fatalf("opens without clicks -> %s", got)
	}
}

func TestClassifyLeadTemperatureReplyOff(t *testing.T) {
	rules := DefaultLeadTemperatureRules()
	rules.Hot.ReplyIsHot = false
	if got := ClassifyLeadTemperature(rules, CampaignContactEngagementCounts{Replies: 1}); got != LeadTemperatureCold {
		t.Fatalf("reply ignored -> %s", got)
	}
}

func TestResolveLeadTemperatureCampaignIsolationAndBots(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser(fmt.Sprintf("temp-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var tmplID int64
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&tmplID); err != nil {
		t.Fatal(err)
	}
	c := Contact{Email: fmt.Sprintf("lead-%d@example.com", time.Now().UnixNano())}
	contactID, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	campA, err := CreateCampaign(userID, "A", tmplID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	campB, err := CreateCampaign(userID, "B", tmplID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := SetCampaignTemperatureRules(campA, userID, DefaultLeadTemperatureRules()); err != nil {
		t.Fatal(err)
	}

	sendA, err := insertTestCampaignSend(userID, contactID, tmplID, campA, "trk-a-"+fmt.Sprint(time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	sendB, err := insertTestCampaignSend(userID, contactID, tmplID, campB, "trk-b-"+fmt.Sprint(time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}

	// Bot opens on A must not count.
	for i := 0; i < 5; i++ {
		if _, err := db.Exec(`
			INSERT INTO email_events (email_send_id, tracking_id, event_type, is_bot, bot_reason, created_at)
			VALUES (?, ?, 'open', 1, 'scanner', ?)
		`, sendA, fmt.Sprintf("bot-%d-%d", sendA, i), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	// Engagement on campaign B must not affect A.
	for i := 0; i < 3; i++ {
		if _, err := db.Exec(`
			INSERT INTO email_events (email_send_id, tracking_id, event_type, is_bot, created_at)
			VALUES (?, ?, 'open', 0, ?)
		`, sendB, fmt.Sprintf("b-open-%d-%d", sendB, i), time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`
			INSERT INTO email_events (email_send_id, tracking_id, event_type, is_bot, created_at)
			VALUES (?, ?, 'click', 0, ?)
		`, sendB, fmt.Sprintf("b-click-%d-%d", sendB, i), time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	tier, err := ResolveLeadTemperature(campA, contactID)
	if err != nil {
		t.Fatal(err)
	}
	if tier != LeadTemperatureCold {
		t.Fatalf("expected cold (bots + other campaign ignored), got %s", tier)
	}

	// Human opens + click on A → warm.
	for i := 0; i < 2; i++ {
		if _, err := db.Exec(`
			INSERT INTO email_events (email_send_id, tracking_id, event_type, is_bot, created_at)
			VALUES (?, ?, 'open', 0, ?)
		`, sendA, fmt.Sprintf("a-open-%d-%d", sendA, i), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`
		INSERT INTO email_events (email_send_id, tracking_id, event_type, is_bot, created_at)
		VALUES (?, ?, 'click', 0, ?)
	`, sendA, fmt.Sprintf("a-click-%d", sendA), time.Now()); err != nil {
		t.Fatal(err)
	}
	tier, err = ResolveLeadTemperature(campA, contactID)
	if err != nil {
		t.Fatal(err)
	}
	if tier != LeadTemperatureWarm {
		t.Fatalf("expected warm, got %s", tier)
	}

	// Reply → hot.
	if _, err := InsertContactEvent(ContactEventInput{
		ContactID:   contactID,
		EmailSendID: sendA,
		EventType:   "REPLY",
	}); err != nil {
		t.Fatal(err)
	}
	tier, err = ResolveLeadTemperature(campA, contactID)
	if err != nil {
		t.Fatal(err)
	}
	if tier != LeadTemperatureHot {
		t.Fatalf("expected hot from reply, got %s", tier)
	}
}

func insertTestCampaignSend(userID, contactID, templateID, campaignID int64, trackingID string) (int64, error) {
	var id int64
	err := db.QueryRow(`
		INSERT INTO email_sends (user_id, contact_id, template_id, campaign_id, tracking_id, sent_at, delivery_status)
		VALUES (?, ?, ?, ?, ?, ?, 'sent') RETURNING id
	`, userID, contactID, templateID, campaignID, trackingID, time.Now()).Scan(&id)
	return id, err
}

func TestParseLeadTemperatureRulesJSONDefaults(t *testing.T) {
	r := ParseLeadTemperatureRulesJSON("")
	if r.Warm.MinOpens != 2 || r.Hot.MinClicks != 2 || !r.Hot.ReplyIsHot {
		t.Fatalf("%+v", r)
	}
	r2 := ParseLeadTemperatureRulesJSON(`{"warm":{"min_opens":4,"min_clicks":2},"hot":{"min_opens":5,"min_clicks":3,"reply_is_hot":false}}`)
	if r2.Warm.MinOpens != 4 || r2.Hot.ReplyIsHot {
		t.Fatalf("%+v", r2)
	}
}
