package model

import (
	"fmt"
	"testing"
	"time"

	"emailtracker.com/db"
)

func TestCampaignMailboxDistributionPlanned(t *testing.T) {
	db.OpenTestDB(t)
	userID, err := CreateUser(fmt.Sprintf("mb-dist-%d@test.com", time.Now().UnixNano()), "hash", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	_, err = UpsertInboxKitSMTPAccount(userID, "a@example.com", "smtp.gmail.com", "587", "a@example.com", "pass-aaaa-aaaa-aaaa", "A", "ik-a", true, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	_, err = UpsertInboxKitSMTPAccount(userID, "b@example.com", "smtp.gmail.com", "587", "b@example.com", "pass-bbbb-bbbb-bbbb", "B", "ik-b", false, 100, "imap.gmail.com", "993")
	if err != nil {
		t.Fatal(err)
	}
	var templateID int64
	if err := db.QueryRow(`INSERT INTO template (name, subject, body, user_id) VALUES ('t','s','b', ?) RETURNING id`, userID).Scan(&templateID); err != nil {
		t.Fatal(err)
	}
	campID, err := CreateCampaign(userID, "dist", templateID, 0, "bulk", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for i := 0; i < 20; i++ {
		c := Contact{Email: fmt.Sprintf("lead%d@x.com", i)}
		cid, err := c.SaveContact(userID, nil)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, cid)
	}
	if err := AddContactsToCampaign(campID, ids); err != nil {
		t.Fatal(err)
	}
	dist, err := GetCampaignMailboxDistribution(userID, campID)
	if err != nil {
		t.Fatal(err)
	}
	if !dist.HasSeats || dist.SeatCount < 2 {
		t.Fatalf("%+v", dist)
	}
	if dist.TotalPlan != 20 {
		t.Fatalf("total=%d", dist.TotalPlan)
	}
	sum := 0
	nonzero := 0
	for _, s := range dist.Seats {
		sum += s.PlannedCount
		if s.PlannedCount > 0 {
			nonzero++
		}
	}
	if sum != 20 {
		t.Fatalf("planned sum=%d", sum)
	}
	if nonzero < 2 {
		t.Fatalf("expected spread, seats=%+v", dist.Seats)
	}
}
