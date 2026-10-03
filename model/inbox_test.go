package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestListInboxThreadsGroupsByContactAndUnread(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser(fmt.Sprintf("inbox-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")

	c1 := Contact{Email: "alice@example.com"}
	cid1, _ := c1.SaveContact(userID, nil)
	c2 := Contact{Email: "bob@example.com"}
	cid2, _ := c2.SaveContact(userID, nil)

	_, err := InsertConversationMessage(ConversationMessageInput{
		UserID: userID, ContactID: cid1, Direction: ConversationOutbound,
		FromEmail: "me@test.com", ToEmail: "alice@example.com", Subject: "Hello",
		BodyText: "Outreach", OccurredAt: time.Now().Add(-2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = InsertConversationMessage(ConversationMessageInput{
		UserID: userID, ContactID: cid1, Direction: ConversationInbound,
		FromEmail: "alice@example.com", ToEmail: "me@test.com", Subject: "Re: Hello",
		BodyText: "Yes interested", ReplySentiment: ReplySentimentPositive,
		OccurredAt: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = InsertConversationMessage(ConversationMessageInput{
		UserID: userID, ContactID: cid2, Direction: ConversationInbound,
		FromEmail: "bob@example.com", ToEmail: "me@test.com", Subject: "Re: Ping",
		BodyText: "Not now", ReplySentiment: ReplySentimentNegative,
		OccurredAt: time.Now().Add(-30 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	all, err := ListInboxThreads(userID, InboxFolderAll, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("all threads=%d want 2", len(all))
	}
	if all[0].ContactID != cid2 {
		t.Fatalf("expected newest first contact=%d got %d", cid2, all[0].ContactID)
	}
	if !all[0].Unread || !all[1].Unread {
		t.Fatalf("expected both unread before mark-read")
	}
	if CountInboxUnread(userID) != 2 {
		t.Fatalf("unread count=%d want 2", CountInboxUnread(userID))
	}

	if err := MarkInboxThreadRead(userID, cid1); err != nil {
		t.Fatal(err)
	}
	if CountInboxUnread(userID) != 1 {
		t.Fatalf("unread after mark=%d want 1", CountInboxUnread(userID))
	}

	unread, err := ListInboxThreads(userID, InboxFolderUnread, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(unread) != 1 || unread[0].ContactID != cid2 {
		t.Fatalf("unread threads=%v", unread)
	}
}

func TestListInboxThreadsInterestedFilter(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser(fmt.Sprintf("inbox-int-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")

	hot := Contact{Email: "hot@example.com"}
	hotID, _ := hot.SaveContact(userID, nil)
	cold := Contact{Email: "cold@example.com"}
	coldID, _ := cold.SaveContact(userID, nil)

	_, _ = InsertConversationMessage(ConversationMessageInput{
		UserID: userID, ContactID: hotID, Direction: ConversationInbound,
		FromEmail: "hot@example.com", ToEmail: "me@test.com", Subject: "Re: hi",
		BodyText: "Let's talk", ReplySentiment: ReplySentimentPositive,
	})
	_, _ = InsertConversationMessage(ConversationMessageInput{
		UserID: userID, ContactID: coldID, Direction: ConversationInbound,
		FromEmail: "cold@example.com", ToEmail: "me@test.com", Subject: "Re: hi",
		BodyText: "No thanks", ReplySentiment: ReplySentimentNegative,
	})

	list, err := ListInboxThreads(userID, InboxFolderInterested, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ContactID != hotID {
		t.Fatalf("interested=%v want only hot", list)
	}
}

func TestListInboxThreadsOpenClickAggregates(t *testing.T) {
	db.OpenTestDB(t)
	userID, _ := CreateUser(fmt.Sprintf("inbox-eng-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	var templateID int64
	_ = db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID)
	c := Contact{Email: "eng@example.com"}
	cid, _ := c.SaveContact(userID, nil)
	campaignID, _ := CreateCampaign(userID, "Camp", templateID, 0, "bulk", 0, "", "")
	sendID, err := enqueueTestSend(userID, templateID, cid, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = InsertConversationMessage(ConversationMessageInput{
		UserID: userID, ContactID: cid, EmailSendID: sendID, Direction: ConversationInbound,
		FromEmail: "eng@example.com", ToEmail: "me@test.com", Subject: "Re: offer",
		BodyText: "Interesting", ReplySentiment: ReplySentimentNeutral,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`
		INSERT INTO email_events (email_send_id, tracking_id, event_type, is_bot, created_at)
		VALUES (?, ?, 'open', 0, CURRENT_TIMESTAMP), (?, ?, 'click', 0, CURRENT_TIMESTAMP)
	`, sendID, fmt.Sprintf("o-%d", cid), sendID, fmt.Sprintf("c-%d", cid))

	threads, err := ListInboxThreads(userID, InboxFolderAll, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads=%d", len(threads))
	}
	if threads[0].OpenCount < 1 || threads[0].ClickCount < 1 {
		t.Fatalf("opens=%d clicks=%d", threads[0].OpenCount, threads[0].ClickCount)
	}

	opened, err := ListInboxThreads(userID, InboxFolderOpened, "", 50)
	if err != nil || len(opened) != 1 {
		t.Fatalf("opened=%v err=%v", opened, err)
	}
	clicked, err := ListInboxThreads(userID, InboxFolderClicked, "", 50)
	if err != nil || len(clicked) != 1 {
		t.Fatalf("clicked=%v err=%v", clicked, err)
	}

	detail, err := GetInboxThread(userID, cid)
	if err != nil {
		t.Fatal(err)
	}
	if detail.OpenCount < 1 || detail.ClickCount < 1 {
		t.Fatalf("detail opens=%d clicks=%d", detail.OpenCount, detail.ClickCount)
	}
	if CountInboxUnread(userID) != 0 {
		t.Fatalf("GetInboxThread should mark read, unread=%d", CountInboxUnread(userID))
	}
}
