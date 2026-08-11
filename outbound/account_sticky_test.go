package outbound

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
	"emailtracker.com/model"
)

func TestResolveSendAccountForContactSticky(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := model.CreateUser(fmt.Sprintf("sticky-mb-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.UpsertInboxKitSMTPAccount(userID, "a@example.com", "smtp.gmail.com", "587", "a@example.com", "pass-aaaa-aaaa-aaaa", "A", "ik-a", true, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.UpsertInboxKitSMTPAccount(userID, "b@example.com", "smtp.gmail.com", "587", "b@example.com", "pass-bbbb-bbbb-bbbb", "B", "ik-b", false, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}

	c1 := model.Contact{Email: "c1@lead.com"}
	contact1, err := c1.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	c2 := model.Contact{Email: "c2@lead.com"}
	contact2, err := c2.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}

	acc1, err := ResolveSendAccountForContact(userID, contact1)
	if err != nil {
		t.Fatal(err)
	}
	acc1b, err := ResolveSendAccountForContact(userID, contact1)
	if err != nil {
		t.Fatal(err)
	}
	if acc1.ID != acc1b.ID {
		t.Fatalf("sticky mismatch: %d vs %d", acc1.ID, acc1b.ID)
	}

	seen := map[int64]bool{}
	for _, cid := range []int64{contact1, contact2, contact1 + 10, contact2 + 20, contact1 + 30} {
		acc, err := ResolveSendAccountForContact(userID, cid)
		if err != nil {
			t.Fatal(err)
		}
		seen[acc.ID] = true
	}
	if len(seen) < 2 {
		t.Fatalf("expected spread across seats, got %v", seen)
	}
}

func TestResolveSendAccountForContactSkipsOverCap(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := model.CreateUser(fmt.Sprintf("sticky-cap-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	idA, err := model.UpsertInboxKitSMTPAccount(userID, "a@example.com", "smtp.gmail.com", "587", "a@example.com", "pass-aaaa-aaaa-aaaa", "A", "ik-a", true, 5, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	idB, err := model.UpsertInboxKitSMTPAccount(userID, "b@example.com", "smtp.gmail.com", "587", "b@example.com", "pass-bbbb-bbbb-bbbb", "B", "ik-b", false, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	// Exhaust A's daily limit.
	if _, err := db.Exec(`UPDATE smtp_accounts SET sends_today = 5, daily_limit = 5, warmup_enabled = 0 WHERE id = ?`, idA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE smtp_accounts SET warmup_enabled = 0 WHERE id = ?`, idB); err != nil {
		t.Fatal(err)
	}

	// Pick a contactID that hashes to A first (idA is lower, ready ordered by id).
	ready, err := model.ListSendReadyAccountsForUser(userID)
	if err != nil || len(ready) < 2 {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
	var contactID int64
	for i := int64(1); i < 50; i++ {
		if int(i%int64(len(ready))) == 0 && ready[0].ID == idA {
			contactID = i
			break
		}
	}
	if contactID == 0 {
		t.Fatal("could not find contact hashing to first seat")
	}
	acc, err := ResolveSendAccountForContact(userID, contactID)
	if err != nil {
		t.Fatal(err)
	}
	if acc.ID != idB {
		t.Fatalf("expected skip to B (%d), got %d", idB, acc.ID)
	}
}

func TestComputeCombinedWarmupProgress(t *testing.T) {
	empty := ComputeCombinedWarmupProgress(nil)
	if empty.HasAccount {
		t.Fatal("empty should have no account")
	}

	start := time.Now()
	a := model.SMTPAccount{
		ID: 1, Status: "active", FromEmail: "a@x.com", SMTPHost: "smtp.gmail.com",
		SMTPUser: "a", SMTPPassword: "p", MailboxSource: model.MailboxSourceInboxKit,
		WarmupEnabled: true, WarmupDailyCap: 20, WarmupTargetDailyCap: 100,
		WarmupIncrementPerDay: 20, DailyLimit: 100, SendsToday: 5, WarmupStartedAt: &start,
	}
	b := model.SMTPAccount{
		ID: 2, Status: "active", FromEmail: "b@x.com", SMTPHost: "smtp.gmail.com",
		SMTPUser: "b", SMTPPassword: "p", MailboxSource: model.MailboxSourceInboxKit,
		WarmupEnabled: true, WarmupDailyCap: 20, WarmupTargetDailyCap: 100,
		WarmupIncrementPerDay: 20, DailyLimit: 100, SendsToday: 10, WarmupStartedAt: &start,
	}
	p := ComputeCombinedWarmupProgress([]model.SMTPAccount{a, b})
	if !p.HasAccount || p.MailboxCount != 2 {
		t.Fatalf("%+v", p)
	}
	if p.SendsToday != 15 {
		t.Fatalf("sends=%d", p.SendsToday)
	}
	if p.TodayCap != 40 {
		t.Fatalf("cap=%d want 40 (day0 start caps)", p.TodayCap)
	}
	if p.CombinedLabel == "" {
		t.Fatal("expected combined label")
	}
}
