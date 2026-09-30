package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestListSendReadyAccountsForCampaignAllowlist(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser(fmt.Sprintf("camp-smtp-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	idA, err := UpsertInboxKitSMTPAccount(userID, "a@example.com", "smtp.gmail.com", "587", "a@example.com", "pass-aaaa-aaaa-aaaa", "A", "ik-a", true, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	idB, err := UpsertInboxKitSMTPAccount(userID, "b@example.com", "smtp.gmail.com", "587", "b@example.com", "pass-bbbb-bbbb-bbbb", "B", "ik-b", false, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	var templateID int64
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	campID, err := CreateCampaign(userID, "smtp-allow", templateID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Empty allowlist → all ready seats.
	ready, err := ListSendReadyAccountsForCampaign(userID, campID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) < 2 {
		t.Fatalf("expected all seats, got %d", len(ready))
	}

	if err := SetCampaignSMTPAccounts(campID, userID, []int64{idA}); err != nil {
		t.Fatal(err)
	}
	ready, err = ListSendReadyAccountsForCampaign(userID, campID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 || ready[0].ID != idA {
		t.Fatalf("allowlist filter: %+v", ready)
	}

	ids, err := ListCampaignSMTPAccountIDs(campID)
	if err != nil || len(ids) != 1 || ids[0] != idA {
		t.Fatalf("ids=%v err=%v", ids, err)
	}

	// Distribution only plans on allowlisted seat.
	var contactIDs []int64
	for i := 0; i < 10; i++ {
		c := Contact{Email: fmt.Sprintf("lead%d@x.com", i)}
		cid, err := c.SaveContact(userID, nil)
		if err != nil {
			t.Fatal(err)
		}
		contactIDs = append(contactIDs, cid)
	}
	if err := AddContactsToCampaign(campID, contactIDs); err != nil {
		t.Fatal(err)
	}
	dist, err := GetCampaignMailboxDistribution(userID, campID)
	if err != nil {
		t.Fatal(err)
	}
	if dist.SeatCount != 1 {
		t.Fatalf("seatCount=%d seats=%+v", dist.SeatCount, dist.Seats)
	}
	if dist.TotalPlan != 10 || dist.Seats[0].PlannedCount != 10 || dist.Seats[0].SMTPAccountID != idA {
		t.Fatalf("dist=%+v", dist)
	}

	// Clearing allowlist restores all seats.
	if err := SetCampaignSMTPAccounts(campID, userID, nil); err != nil {
		t.Fatal(err)
	}
	ready, err = ListSendReadyAccountsForCampaign(userID, campID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) < 2 {
		t.Fatalf("cleared allowlist should restore all, got %d (b=%d)", len(ready), idB)
	}
}
