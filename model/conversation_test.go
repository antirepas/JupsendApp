package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestSaveEmailSendRenderedContent(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("snap@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var contactID, templateID int64
	if err := db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('lead@example.com', ?) RETURNING id`, userID).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','Hello','body', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	sendID, err := CreateQueuedEmailSend(userID, templateID, contactID, fmt.Sprintf("track-snap-%d", time.Now().UnixNano()), 0, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveEmailSendRenderedContent(sendID, "Hello Alice", "<p>Hi</p>", "Hi"); err != nil {
		t.Fatal(err)
	}
	detail, err := GetEmailSendDetail(sendID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.RenderedSubject != "Hello Alice" || detail.RenderedHTML != "<p>Hi</p>" || detail.RenderedText != "Hi" {
		t.Fatalf("rendered snapshot mismatch: %+v", detail)
	}
}

func TestConversationInboundAndThreadOrder(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("conv@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var contactID int64
	if err := db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('lead@example.com', ?) RETURNING id`, userID).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	acctID, err := CreateSMTPAccountForUser(userID, SMTPAccount{
		Name: "mbox", SMTPHost: "smtp.test", SMTPPort: "587", SMTPUser: "u", SMTPPassword: "p",
		FromEmail: "me@test.com", Status: "active", DailyLimit: 50, PerMinuteLimit: 10, MinSecondsBetweenSends: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now().Add(-2 * time.Hour)
	t1 := time.Now().Add(-1 * time.Hour)
	_, err = InsertConversationMessage(ConversationMessageInput{
		UserID: userID, ContactID: contactID, SMTPAccountID: acctID, EmailSendID: 99,
		Direction: ConversationOutbound, FromEmail: "me@test.com", ToEmail: "lead@example.com",
		Subject: "Outreach", BodyText: "Hello", MessageID: "<out-1@test>", OccurredAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = InsertConversationMessage(ConversationMessageInput{
		UserID: userID, ContactID: contactID, SMTPAccountID: acctID, EmailSendID: 99,
		Direction: ConversationInbound, FromEmail: "lead@example.com", ToEmail: "me@test.com",
		Subject: "Re: Outreach", BodyText: "Interested!", BodyHTML: "<p>Interested!</p>",
		MessageID: "<in-1@client>", InReplyTo: "<out-1@test>", OccurredAt: t1,
	})
	if err != nil {
		t.Fatal(err)
	}

	msgs, err := ListContactConversation(userID, contactID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Direction != ConversationOutbound || msgs[1].Direction != ConversationInbound {
		t.Fatalf("order wrong: %s then %s", msgs[0].Direction, msgs[1].Direction)
	}
	if !HasInboundConversation(userID, contactID) {
		t.Fatal("expected inbound")
	}
	inbound, err := LatestInboundMessage(userID, contactID)
	if err != nil {
		t.Fatal(err)
	}
	if inbound.MessageID != "<in-1@client>" {
		t.Fatalf("message id %q", inbound.MessageID)
	}
}

func TestLatestSMTPAccountForContactPinsMailbox(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("pin@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var contactID, templateID int64
	if err := db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('lead@example.com', ?) RETURNING id`, userID).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	acctA, err := CreateSMTPAccountForUser(userID, SMTPAccount{
		Name: "a", SMTPHost: "smtp.test", SMTPPort: "587", SMTPUser: "a", SMTPPassword: "p",
		FromEmail: "a@test.com", Status: "active", DailyLimit: 50, PerMinuteLimit: 10, MinSecondsBetweenSends: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	acctB, err := CreateSMTPAccountForUser(userID, SMTPAccount{
		Name: "b", SMTPHost: "smtp.test", SMTPPort: "587", SMTPUser: "b", SMTPPassword: "p",
		FromEmail: "b@test.com", Status: "active", DailyLimit: 50, PerMinuteLimit: 10, MinSecondsBetweenSends: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	sendID, err := CreateQueuedEmailSend(userID, templateID, contactID, fmt.Sprintf("track-pin-%d", time.Now().UnixNano()), 0, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkEmailSendSent(sendID, acctB, 0); err != nil {
		t.Fatal(err)
	}
	// Older send from A should not win.
	oldID, err := CreateQueuedEmailSend(userID, templateID, contactID, fmt.Sprintf("track-pin-old-%d", time.Now().UnixNano()), 0, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE email_sends SET delivery_status='sent', sent_at=?, smtp_account_id=? WHERE id=?`,
		time.Now().Add(-24*time.Hour), acctA, oldID); err != nil {
		t.Fatal(err)
	}

	got, err := LatestSMTPAccountForContact(userID, contactID)
	if err != nil {
		t.Fatal(err)
	}
	if got != acctB {
		t.Fatalf("want account %d, got %d", acctB, got)
	}
}

func TestListContactConversationMergesLegacySnapshots(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("merge@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var contactID, templateID int64
	if err := db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('lead@example.com', ?) RETURNING id`, userID).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','Template Subj','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	sendID, err := CreateQueuedEmailSend(userID, templateID, contactID, fmt.Sprintf("track-merge-%d", time.Now().UnixNano()), 0, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkEmailSendSent(sendID, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := SaveEmailSendRenderedContent(sendID, "Rendered Subj", "<p>Body</p>", "Body"); err != nil {
		t.Fatal(err)
	}

	msgs, err := ListContactConversation(userID, contactID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 merged outbound, got %d", len(msgs))
	}
	if msgs[0].Subject != "Rendered Subj" || msgs[0].Direction != ConversationOutbound {
		t.Fatalf("unexpected msg: %+v", msgs[0])
	}
}

func TestConversationMessageIDDedupe(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser("dedupe-conv@example.com", "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	var contactID int64
	if err := db.QueryRow(`INSERT INTO contact (email, user_id) VALUES ('lead@example.com', ?) RETURNING id`, userID).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	in := ConversationMessageInput{
		UserID: userID, ContactID: contactID, Direction: ConversationInbound,
		FromEmail: "lead@example.com", ToEmail: "me@test.com", Subject: "Hi",
		BodyText: "x", MessageID: "<same@id>",
	}
	id1, err := InsertConversationMessage(in)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := InsertConversationMessage(in)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("expected dedupe same id, got %d and %d", id1, id2)
	}
}
