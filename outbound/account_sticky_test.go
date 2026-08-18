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
	// Exhaust A's daily limit (stamp reset_at so EnsureDailyCounterReset won't wipe sends_today).
	today := time.Now().Format("2006-01-02")
	if _, err := db.Exec(`UPDATE smtp_accounts SET sends_today = 5, daily_limit = 5, warmup_enabled = 0, sends_today_reset_at = ? WHERE id = ?`, today, idA); err != nil {
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

func TestResolveSendAccountStaysStickyWhenOverCap(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := model.CreateUser(fmt.Sprintf("sticky-pin-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	idA, err := model.UpsertInboxKitSMTPAccount(userID, "pin-a@example.com", "smtp.gmail.com", "587", "pin-a@example.com", "pass-aaaa-aaaa-aaaa", "A", "ik-pin-a", true, 5, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	idB, err := model.UpsertInboxKitSMTPAccount(userID, "pin-b@example.com", "smtp.gmail.com", "587", "pin-b@example.com", "pass-bbbb-bbbb-bbbb", "B", "ik-pin-b", false, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	_ = idB
	today := time.Now().Format("2006-01-02")
	if _, err := db.Exec(`UPDATE smtp_accounts SET warmup_enabled = 0, sends_today_reset_at = ? WHERE id IN (?, ?)`, today, idA, idB); err != nil {
		t.Fatal(err)
	}

	c := model.Contact{Email: "pinned@lead.com"}
	contactID, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Record a prior send from A so sticky pins this contact.
	if _, err := db.Exec(`
		INSERT INTO email_sends (user_id, contact_id, template_id, tracking_id, smtp_account_id, delivery_status, sent_at)
		VALUES (?, ?, 0, ?, ?, 'sent', NOW())
	`, userID, contactID, fmt.Sprintf("pin-%d", time.Now().UnixNano()), idA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE smtp_accounts SET sends_today = 5, daily_limit = 5 WHERE id = ?`, idA); err != nil {
		t.Fatal(err)
	}

	acc, err := ResolveSendAccountForContact(userID, contactID)
	if err != nil {
		t.Fatal(err)
	}
	if acc.ID != idA {
		t.Fatalf("expected stay on pinned A (%d) even over cap, got %d", idA, acc.ID)
	}
}

func TestResolveSendAccountStaysStickyWhenSeatFilteredFromReady(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := model.CreateUser(fmt.Sprintf("sticky-filter-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	idA, err := model.UpsertInboxKitSMTPAccount(userID, "keep-a@example.com", "smtp.gmail.com", "587", "keep-a@example.com", "pass-aaaa-aaaa-aaaa", "A", "ik-keep-a", true, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	idGmail, err := model.UpsertInboxKitSMTPAccount(userID, "personal@gmail.com", "smtp.gmail.com", "587", "personal@gmail.com", "pass-gggg-gggg-gggg", "Gmail", "ik-gmail", false, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	c := model.Contact{Email: "filter-pin@lead.com"}
	contactID, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO email_sends (user_id, contact_id, template_id, tracking_id, smtp_account_id, delivery_status, sent_at)
		VALUES (?, ?, 0, ?, ?, 'sent', NOW())
	`, userID, contactID, fmt.Sprintf("filt-%d", time.Now().UnixNano()), idA); err != nil {
		t.Fatal(err)
	}
	// Simulate A missing from ready filter by marking it inactive credentials-wise while still active status —
	// force ListSendReady to exclude A by clearing password, but GetSMTPAccount still returns active row.
	// Instead: deactivate A in a way IsSendReady fails, then sticky must still return A via GetSMTPAccount.
	if _, err := db.Exec(`UPDATE smtp_accounts SET smtp_password = '' WHERE id = ?`, idA); err != nil {
		t.Fatal(err)
	}
	ready, err := model.ListSendReadyAccountsForUser(userID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range ready {
		if a.ID == idA {
			t.Fatal("A should not be in ready set")
		}
	}
	if len(ready) == 0 || ready[0].ID != idGmail && !containsAccount(ready, idGmail) {
		// gmail should still be ready
		if !containsAccount(ready, idGmail) {
			t.Fatalf("expected gmail in ready, got %+v", ready)
		}
	}

	acc, err := ResolveSendAccountForContact(userID, contactID)
	if err != nil {
		t.Fatal(err)
	}
	if acc.ID != idA {
		t.Fatalf("must stay on sticky A (%d), not switch to ready seat (got %d)", idA, acc.ID)
	}
}

func containsAccount(ready []model.SMTPAccount, id int64) bool {
	for _, a := range ready {
		if a.ID == id {
			return true
		}
	}
	return false
}

func TestResolveAccountForJobUsesPinnedSMTP(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := model.CreateUser(fmt.Sprintf("sticky-job-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	idA, err := model.UpsertInboxKitSMTPAccount(userID, "job-a@example.com", "smtp.gmail.com", "587", "job-a@example.com", "pass-aaaa-aaaa-aaaa", "A", "ik-job-a", true, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.UpsertInboxKitSMTPAccount(userID, "job-b@example.com", "smtp.gmail.com", "587", "job-b@example.com", "pass-bbbb-bbbb-bbbb", "B", "ik-job-b", false, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	c := model.Contact{Email: "job-pin@lead.com"}
	contactID, err := c.SaveContact(userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	job := model.SendJob{UserID: userID, ContactID: contactID, SMTPAccountID: idA}
	acc, err := ResolveAccountForJob(job)
	if err != nil {
		t.Fatal(err)
	}
	if acc.ID != idA {
		t.Fatalf("got %d want %d", acc.ID, idA)
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
