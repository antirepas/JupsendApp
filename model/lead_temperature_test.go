package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestClassifyLeadTemperatureReplyCentric(t *testing.T) {
	rules := DefaultLeadTemperatureRules()
	if got := ClassifyLeadTemperature(rules, CampaignContactEngagementCounts{}); got != LeadTemperatureCold {
		t.Fatalf("empty -> %s", got)
	}
	if got := ClassifyLeadTemperature(rules, CampaignContactEngagementCounts{Replies: 1, NonNegativeReplies: 1}); got != LeadTemperatureWarm {
		t.Fatalf("pending/neutral reply -> %s", got)
	}
	if got := ClassifyLeadTemperature(rules, CampaignContactEngagementCounts{Replies: 1, PositiveReplies: 1, NonNegativeReplies: 1}); got != LeadTemperatureHot {
		t.Fatalf("positive -> %s", got)
	}
	if got := ClassifyLeadTemperature(rules, CampaignContactEngagementCounts{Replies: 1, NegativeReplies: 1}); got != LeadTemperatureCold {
		t.Fatalf("only negative -> %s", got)
	}
	// Opens alone no longer warm/hot
	if got := ClassifyLeadTemperature(rules, CampaignContactEngagementCounts{Opens: 10, Clicks: 5}); got != LeadTemperatureCold {
		t.Fatalf("opens/clicks alone -> %s", got)
	}
}

func TestParseLeadTemperatureRulesMigratesLegacy(t *testing.T) {
	r := ParseLeadTemperatureRulesJSON(`{"warm":{"min_opens":2,"min_clicks":1},"hot":{"min_opens":3,"min_clicks":2,"reply_is_hot":true}}`)
	if r.Hot.MinPositiveReplies != 1 || !r.Warm.AnyNonNegativeReply {
		t.Fatalf("%+v", r)
	}
}

func TestSetConversationReplySentiment(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser(fmt.Sprintf("sent-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	c := Contact{Email: "lead@example.com"}
	contactID, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	msgID, err := InsertConversationMessage(ConversationMessageInput{
		UserID: userID, ContactID: contactID, Direction: ConversationInbound,
		FromEmail: "lead@example.com", ToEmail: "me@example.com", Subject: "Re: hi",
		BodyText: "Interested!", ReplySentiment: ReplySentimentPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := SetConversationReplySentiment(msgID, ReplySentimentPositive, "manual"); err != nil {
		t.Fatal(err)
	}
	got, err := GetConversationMessageForUser(userID, contactID, msgID)
	if err != nil || got.ReplySentiment != ReplySentimentPositive {
		t.Fatalf("%+v %v", got, err)
	}
}
