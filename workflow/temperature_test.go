package workflow

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
	"emailtracker.com/model"
)

func TestTemperatureConditionExecutor(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := model.CreateUser(fmt.Sprintf("wf-temp-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var tmplID int64
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&tmplID); err != nil {
		t.Fatal(err)
	}
	c := model.Contact{Email: fmt.Sprintf("c-%d@ex.com", time.Now().UnixNano())}
	contactID, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	campID, err := model.CreateCampaign(userID, "Camp", tmplID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = model.SetCampaignTemperatureRules(campID, userID, model.DefaultLeadTemperatureRules())

	var sendID int64
	err = db.QueryRow(`
		INSERT INTO email_sends (user_id, contact_id, template_id, campaign_id, tracking_id, sent_at, delivery_status)
		VALUES (?, ?, ?, ?, ?, ?, 'sent') RETURNING id
	`, userID, contactID, tmplID, campID, fmt.Sprintf("trk-%d", time.Now().UnixNano()), time.Now()).Scan(&sendID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.InsertContactEvent(model.ContactEventInput{
		ContactID: contactID, EmailSendID: sendID, EventType: "REPLY",
		Metadata: map[string]interface{}{"sentiment": "neutral", "sentiment_source": "manual"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.InsertConversationMessage(model.ConversationMessageInput{
		UserID: userID, ContactID: contactID, EmailSendID: sendID, Direction: model.ConversationInbound,
		FromEmail: c.Email, ToEmail: "me@example.com", Subject: "Re: hi",
		BodyText: "Thanks", ReplySentiment: model.ReplySentimentNeutral,
	})
	if err != nil {
		t.Fatal(err)
	}

	camp := campID
	inst := model.WorkflowInstance{ContactID: contactID, CampaignID: &camp}
	ex := TemperatureConditionExecutor{}
	res, err := ex.Execute(ExecutionContext{
		Instance: inst,
		Node:     model.WorkflowNode{NodeKey: "temp", NodeType: "condition_temperature"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.NextEdgeType != model.LeadTemperatureWarm {
		t.Fatalf("edge=%q", res.NextEdgeType)
	}
}
