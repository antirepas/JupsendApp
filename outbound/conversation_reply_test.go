package outbound

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
	"emailtracker.com/model"
)

func TestHandleReplyStoresInboundAndMarksReplied(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := model.CreateUser("imap-reply@example.com", "hash", "http://localhost")
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
	sendID, err := model.CreateQueuedEmailSend(userID, templateID, contactID, fmt.Sprintf("track-reply-%d", time.Now().UnixNano()), 0, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	acctID, err := model.CreateSMTPAccountForUser(userID, model.SMTPAccount{
		Name: "mbox", SMTPHost: "smtp.test", SMTPPort: "587", SMTPUser: "u", SMTPPassword: "p",
		FromEmail: "me@test.com", Status: "active", DailyLimit: 50, PerMinuteLimit: 10, MinSecondsBetweenSends: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.MarkEmailSendSent(sendID, acctID, 0); err != nil {
		t.Fatal(err)
	}

	handleReply(userID, ReplyMatch{
		ContactID:   contactID,
		EmailSendID: sendID,
		TrackingID:  "x",
	}, inboxMessage{
		From:      "lead@example.com",
		Subject:   "Re: Hello",
		Body:      "Content-Type: text/plain\r\n\r\nYes I'm interested",
		MessageID: "<reply-msg@client>",
		InReplyTo: "<orig@test>",
	}, acctID, "me@test.com")

	msgs, err := model.ListConversationMessages(userID, contactID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Direction != model.ConversationInbound {
		t.Fatalf("expected 1 inbound, got %+v", msgs)
	}
	if msgs[0].BodyText == "" && msgs[0].BodyHTML == "" {
		t.Fatal("expected parsed body")
	}
	if msgs[0].MessageID != "<reply-msg@client>" {
		t.Fatalf("message id %q", msgs[0].MessageID)
	}

	var repliedAt *time.Time
	var rt sql.NullTime
	if err := db.QueryRow(`SELECT replied_at FROM contact WHERE id = ?`, contactID).Scan(&rt); err != nil {
		t.Fatal(err)
	}
	if rt.Valid {
		repliedAt = &rt.Time
	}
	if repliedAt == nil {
		t.Fatal("expected contact marked replied")
	}
}

func TestManualReplyResolvesPinnedAccount(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := model.CreateUser("pin-reply@example.com", "hash", "http://localhost")
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
	acctPinned, err := model.CreateSMTPAccountForUser(userID, model.SMTPAccount{
		Name: "pinned", SMTPHost: "smtp.test", SMTPPort: "587", SMTPUser: "p", SMTPPassword: "p",
		FromEmail: "pinned@test.com", Status: "active", DailyLimit: 50, PerMinuteLimit: 10, MinSecondsBetweenSends: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.CreateSMTPAccountForUser(userID, model.SMTPAccount{
		Name: "other", SMTPHost: "smtp.test", SMTPPort: "587", SMTPUser: "o", SMTPPassword: "p",
		FromEmail: "other@test.com", Status: "active", DailyLimit: 50, PerMinuteLimit: 10, MinSecondsBetweenSends: 1,
		IsDefault: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sendID, err := model.CreateQueuedEmailSend(userID, templateID, contactID, fmt.Sprintf("track-pinr-%d", time.Now().UnixNano()), 0, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.MarkEmailSendSent(sendID, acctPinned, 0); err != nil {
		t.Fatal(err)
	}
	_, _ = model.InsertConversationMessage(model.ConversationMessageInput{
		UserID: userID, ContactID: contactID, SMTPAccountID: acctPinned, EmailSendID: sendID,
		Direction: model.ConversationInbound, FromEmail: "lead@example.com", ToEmail: "pinned@test.com",
		Subject: "Re: Hello", BodyText: "Hi", MessageID: "<inbound-pin@client>",
	})

	got, err := model.LatestSMTPAccountForContact(userID, contactID)
	if err != nil {
		t.Fatal(err)
	}
	if got != acctPinned {
		t.Fatalf("want pinned %d got %d", acctPinned, got)
	}
	inbound, err := model.LatestInboundMessage(userID, contactID)
	if err != nil {
		t.Fatal(err)
	}
	inReplyTo := inbound.MessageID
	if inReplyTo != "<inbound-pin@client>" {
		t.Fatalf("In-Reply-To source %q", inReplyTo)
	}
}
