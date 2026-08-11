package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/config"
	"emailtracker.com/db"
)

func TestSMTPAccountIsSendReady(t *testing.T) {
	oauth := SMTPAccount{
		Status:            "active",
		AuthType:          AuthTypeGoogleOAuth,
		OAuthRefreshToken: "enc",
		GoogleEmail:       "user@gmail.com",
		SMTPHost:          "smtp.gmail.com",
	}
	if !oauth.IsSendReady() {
		t.Fatal("oauth account should be send ready")
	}
	inactive := oauth
	inactive.Status = "inactive"
	if inactive.IsSendReady() {
		t.Fatal("inactive account should not be send ready")
	}
	noGmail := SMTPAccount{Status: "active", SMTPHost: "smtp.gmail.com"}
	if noGmail.IsSendReady() {
		t.Fatal("account without gmail should not be send ready")
	}
}

func TestListSendReadyExcludesSharedWhenPersonalSeatsExist(t *testing.T) {
	db.OpenTestDB(t)
	config.SMTPHost = "smtp.example.com"
	config.SMTPUser = "shared@example.com"
	config.SMTPPass = "shared-pass-xxxx"
	config.SMTPFrom = "shared@example.com"
	config.SMTPPort = "587"

	userID, err := CreateUser(fmt.Sprintf("ready-filter-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureFreeSharedMailbox(userID); err != nil {
		t.Fatal(err)
	}
	personalID, err := UpsertInboxKitSMTPAccount(userID, "seat@example.com", "smtp.gmail.com", "587", "seat@example.com", "pass-aaaa-aaaa-aaaa", "Seat", "ik-seat", true, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	// Keep shared active even though personal seats exist (simulates leftover Free seat).
	if _, err := db.Exec(`UPDATE smtp_accounts SET status='active' WHERE user_id=? AND mailbox_source=?`, userID, MailboxSourceShared); err != nil {
		t.Fatal(err)
	}

	ready, err := ListSendReadyAccountsForUser(userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 || ready[0].ID != personalID {
		t.Fatalf("expected only personal seat %d, got %+v", personalID, ready)
	}
}

func TestListSendReadyKeepsOnlyOutreachLinkedSeats(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser(fmt.Sprintf("ready-link-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	linkedID, err := UpsertInboxKitSMTPAccount(userID, "linked@example.com", "smtp.gmail.com", "587", "linked@example.com", "pass-aaaa-aaaa-aaaa", "L", "ik-l", true, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	orphanID, err := UpsertInboxKitSMTPAccount(userID, "orphan@example.com", "smtp.gmail.com", "587", "orphan@example.com", "pass-bbbb-bbbb-bbbb", "O", "ik-o", false, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	_, err = UpsertOutreachMailbox(OutreachMailbox{
		UserID: userID, SMTPAccountID: linkedID, Email: "linked@example.com",
		Status: "ready", Platform: "GOOGLE", InboxkitMailboxID: "ik-l",
	})
	if err != nil {
		t.Fatal(err)
	}

	ready, err := ListSendReadyAccountsForUser(userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 || ready[0].ID != linkedID {
		t.Fatalf("expected only linked %d (orphan %d), got %+v", linkedID, orphanID, idsOf(ready))
	}
}

func idsOf(accounts []SMTPAccount) []int64 {
	out := make([]int64, len(accounts))
	for i, a := range accounts {
		out[i] = a.ID
	}
	return out
}
